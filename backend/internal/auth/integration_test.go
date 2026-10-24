package auth_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"destekmasa/internal/auth"
	"destekmasa/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func TestPostgresAuthentication(t *testing.T) {
	url := os.Getenv("INTEGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("set INTEGRATION_DATABASE_URL or run make test-integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	nonce := make([]byte, 8)
	if _, err = rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	schema := "auth_test_" + hex.EncodeToString(nonce)
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	const password = "DemoDestek2026!"
	const email = "calisan@destekmasa.local"
	if err = database.SeedDemo(ctx, pool, password); err != nil {
		t.Fatal(err)
	}
	service, err := auth.New(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	login := func(t *testing.T) auth.Session {
		t.Helper()
		session, err := service.Login(ctx, email, password, "")
		if err != nil {
			t.Fatal(err)
		}
		return session
	}

	t.Run("seed_is_repeatable", func(t *testing.T) {
		if err := database.Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if err := database.SeedDemo(ctx, pool, "OtherPassword2026!"); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 7 {
			t.Fatalf("users: %d", count)
		}
		login(t) // Seed must not overwrite existing password.
	})
	t.Run("password_is_bcrypt_hash", func(t *testing.T) {
		var hash string
		if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email=$1`, email).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		if hash == password || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
			t.Fatal("invalid password storage")
		}
	})
	t.Run("login_and_current_user", func(t *testing.T) {
		session := login(t)
		user, err := service.Current(ctx, session.Token)
		if err != nil || user.Email != email || user.Role != "employee" {
			t.Fatalf("current user: %+v, %v", user, err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND octet_length(token_hash)=32`, user.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Fatal("session not persisted as hash")
		}
	})
	t.Run("wrong_password", func(t *testing.T) {
		_, err := service.Login(ctx, email, "wrong-password", "")
		if !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("unknown_email", func(t *testing.T) {
		_, err := service.Login(ctx, "missing@destekmasa.local", password, "")
		if !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("expired_session", func(t *testing.T) {
		session := login(t)
		// Move both timestamps to preserve the table CHECK constraint.
		if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour' WHERE user_id=$1`, session.User.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Current(ctx, session.Token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("expired session accepted")
		}
	})
	t.Run("login_rotates_previous_session", func(t *testing.T) {
		previous := login(t)
		next, err := service.Login(ctx, email, password, previous.Token)
		if err != nil {
			t.Fatal(err)
		}
		if next.Token == previous.Token {
			t.Fatal("token reused")
		}
		if _, err := service.Current(ctx, previous.Token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("old session valid")
		}
		if _, err := service.Current(ctx, next.Token); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("logout_prevents_replay", func(t *testing.T) {
		session := login(t)
		if err := service.Logout(ctx, session.Token); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Current(ctx, session.Token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("logged-out token accepted")
		}
	})
	t.Run("role_change_is_immediate", func(t *testing.T) {
		session := login(t)
		if _, err := pool.Exec(ctx, `UPDATE users SET role_code='admin' WHERE id=$1`, session.User.ID); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_, err := pool.Exec(ctx, `UPDATE users SET role_code='employee' WHERE id=$1`, session.User.ID)
			if err != nil {
				t.Error(err)
			}
		}()
		user, err := service.Current(ctx, session.Token)
		if err != nil || user.Role != "admin" {
			t.Fatal("stale role used")
		}
	})
	t.Run("audit_is_append_only", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action IN ('auth.login','auth.logout')`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Fatal("audit missing")
		}
		for _, query := range []string{`UPDATE audit_log SET action='tampered'`, `DELETE FROM audit_log`, `TRUNCATE audit_log`} {
			if _, err := pool.Exec(ctx, query); err == nil {
				t.Fatalf("audit mutation allowed: %s", query)
			}
		}
	})
	t.Run("concurrent_logins_have_distinct_tokens", func(t *testing.T) {
		var wg sync.WaitGroup
		sessions := make(chan auth.Session, 4)
		problems := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				session, err := service.Login(ctx, email, password, "")
				if err != nil {
					problems <- err
					return
				}
				sessions <- session
			}()
		}
		wg.Wait()
		close(sessions)
		close(problems)
		for err := range problems {
			t.Error(err)
		}
		tokens := map[string]bool{}
		for session := range sessions {
			if tokens[session.Token] {
				t.Error("duplicate token")
			}
			tokens[session.Token] = true
			if _, err := service.Current(ctx, session.Token); err != nil {
				t.Error(err)
			}
		}
		if len(tokens) != 4 {
			t.Fatalf("valid sessions: %d", len(tokens))
		}
	})
}
