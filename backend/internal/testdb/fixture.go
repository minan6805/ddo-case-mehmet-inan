// Package testdb provides isolated PostgreSQL fixtures for integration tests only.
package testdb

import (
	"context"
	"crypto/rand"
	"destekmasa/internal/auth"
	"destekmasa/internal/database"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"regexp"
	"testing"
	"time"
)

func New(t *testing.T, prefix string) (context.Context, *pgxpool.Pool, map[string]auth.User) {
	t.Helper()
	url := os.Getenv("INTEGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("run make test-integration")
	}
	if !regexp.MustCompile(`^[a-z_]+$`).MatchString(prefix) {
		t.Fatal("invalid fixture prefix")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	random := make([]byte, 8)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := prefix + "_" + hex.EncodeToString(random)
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = database.SeedDemo(ctx, pool, "DemoDestek2026!"); err != nil {
		t.Fatal(err)
	}
	users := map[string]auth.User{}
	rows, err := pool.Query(ctx, `SELECT id,email,display_name,role_code FROM users`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var u auth.User
		if err = rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role); err != nil {
			t.Fatal(err)
		}
		users[u.Email] = u
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, users
}
