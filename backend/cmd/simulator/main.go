package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"destekmasa/internal/simulator"
	"github.com/jackc/pgx/v5/pgxpool"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func run() error {
	seed, err := strconv.ParseInt(env("SIMULATOR_SEED", "42"), 10, 64)
	if err != nil {
		return simulator.ErrInvalid
	}
	count, err := strconv.Atoi(env("SIMULATOR_COUNT", "800"))
	if err != nil {
		return simulator.ErrInvalid
	}
	seconds, err := strconv.Atoi(env("SIMULATOR_DAY_SECONDS", "120"))
	if err != nil {
		return simulator.ErrInvalid
	}
	c := simulator.Config{Name: env("SIMULATOR_RUN", "demo-v1"), Day: env("SIMULATOR_DAY", "2026-10-26"), Seed: seed, Count: count, DaySeconds: seconds}
	if err = c.Validate(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, "")
	if err != nil {
		return fmt.Errorf("database configuration invalid")
	}
	defer pool.Close()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = pool.Ping(startup); err != nil {
		return fmt.Errorf("database unavailable")
	}
	result, err := simulator.NewRunner(pool).Run(ctx, c)
	if result.ID != 0 {
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil && err == nil {
			return encodeErr
		}
	}
	return err
}
func main() {
	if err := run(); err != nil {
		message := "Simülasyon tamamlanamadı; durum ekranını kontrol edip aynı ayarlarla yeniden çalıştırın."
		switch err {
		case simulator.ErrInvalid:
			message = "Simülatör ayarları geçersiz."
		case simulator.ErrConflict:
			message = "Çalışma kimliği veya mevcut veri planla çakışıyor. Durum ekranını ve ayarları kontrol edin."
		case simulator.ErrRunning:
			message = "Başka bir simülatör çalışıyor."
		case simulator.ErrProfileChanged:
			message = "Uzman profili dışarıdan değişti; simülatör değişikliklerinizi ezmedi."
		}
		fmt.Fprintln(os.Stderr, message)
		os.Exit(1)
	}
}
