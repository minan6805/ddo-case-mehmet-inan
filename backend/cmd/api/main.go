package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"destekmasa/internal/assets"
	"destekmasa/internal/assignment"
	"destekmasa/internal/auth"
	"destekmasa/internal/changes"
	"destekmasa/internal/sla"
	"destekmasa/internal/tickets"
	"time"

	"destekmasa/internal/database"
	"destekmasa/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := run(logger); err != nil {
		// Database errors can contain connection details; do not log raw configuration.
		logger.Error("application stopped", "reason", err.Error())
		os.Exit(1)
	}
}

func healthcheck() error {
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(env("HEALTHCHECK_URL", "http://127.0.0.1:8080/health"))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy")
	}
	return nil
}

func startWorker(ctx context.Context, logger *slog.Logger, name string, interval time.Duration, work func(context.Context) error) <-chan struct{} {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := work(check)
			cancel()
			if err != nil && ctx.Err() == nil {
				logger.Error("background check failed", "worker", name)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return finished
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(startup, "") // pgx reads standard PG* environment variables.
	if err != nil {
		return fmt.Errorf("invalid database configuration")
	}
	defer pool.Close()
	if err = pool.Ping(startup); err != nil {
		return fmt.Errorf("database connection failed")
	}
	if err = database.Migrate(startup, pool); err != nil {
		logger.Error("migration failed; inspect schema and migration files")
		return fmt.Errorf("database migration failed")
	}
	demoEnabled, err := strconv.ParseBool(env("DEMO_SEED", "false"))
	if err != nil {
		return fmt.Errorf("invalid DEMO_SEED")
	}
	if demoEnabled {
		if err = database.SeedDemo(startup, pool, os.Getenv("DEMO_PASSWORD")); err != nil {
			return fmt.Errorf("demo seed failed; check DEMO_PASSWORD and database schema")
		}
	}
	ttl, err := time.ParseDuration(env("SESSION_TTL", "8h"))
	if err != nil {
		return fmt.Errorf("invalid SESSION_TTL")
	}
	secure, err := strconv.ParseBool(env("COOKIE_SECURE", "false"))
	if err != nil {
		return fmt.Errorf("invalid COOKIE_SECURE")
	}
	authService, err := auth.New(pool, ttl)
	if err != nil {
		return fmt.Errorf("invalid authentication configuration")
	}
	sweepInterval, err := time.ParseDuration(env("SLA_SWEEP_INTERVAL", "10s"))
	if err != nil || sweepInterval < time.Second || sweepInterval > time.Minute {
		return fmt.Errorf("invalid SLA_SWEEP_INTERVAL")
	}
	batch, err := strconv.Atoi(env("SLA_SWEEP_BATCH", "200"))
	if err != nil || batch < 1 || batch > 1000 {
		return fmt.Errorf("invalid SLA_SWEEP_BATCH")
	}
	assignmentInterval, err := time.ParseDuration(env("ASSIGNMENT_SWEEP_INTERVAL", "10s"))
	if err != nil || assignmentInterval < time.Second || assignmentInterval > time.Minute {
		return fmt.Errorf("invalid ASSIGNMENT_SWEEP_INTERVAL")
	}
	assignmentBatch, err := strconv.Atoi(env("ASSIGNMENT_SWEEP_BATCH", "100"))
	if err != nil || assignmentBatch < 1 || assignmentBatch > 1000 {
		return fmt.Errorf("invalid ASSIGNMENT_SWEEP_BATCH")
	}
	slaEngine := sla.New(pool, nil)
	routingEngine := assignment.New(pool, nil)
	slaDone := startWorker(ctx, logger, "SLA", sweepInterval, func(check context.Context) error { _, err := slaEngine.Sweep(check, batch); return err })
	assignmentDone := startWorker(ctx, logger, "assignment", assignmentInterval, func(check context.Context) error { _, err := routingEngine.Sweep(check, assignmentBatch); return err })
	defer func() { stop(); <-slaDone; <-assignmentDone }()
	ticketService := tickets.NewWithEngines(pool, slaEngine, routingEngine)
	router := httpapi.NewServicesRouter(httpapi.Services{
		Database: pool, Authentication: authService, AuthOptions: httpapi.AuthOptions{SecureCookie: secure},
		Comments: ticketService, Email: ticketService, Threads: ticketService, Assets: assets.New(pool), Changes: changes.New(pool, nil),
		Tickets: ticketService, SLA: slaEngine, Assignment: routingEngine,
	})
	server := &http.Server{
		Addr: env("HTTP_ADDR", ":8080"), Handler: router,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	errors := make(chan error, 1)
	go func() { errors <- server.ListenAndServe() }()
	logger.Info("api ready", "address", server.Addr)
	select {
	case err = <-errors:
		if err != http.ErrServerClosed {
			return fmt.Errorf("http server failed")
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
