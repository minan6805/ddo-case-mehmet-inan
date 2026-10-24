package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestHealthChecksDatabaseWithDeadline(t *testing.T) {
	called := false
	router := NewRouter(pingFunc(func(ctx context.Context) error {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("database ping must have a bounded deadline")
		}
		return nil
	}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if !called {
		t.Fatal("health endpoint did not check database")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["database"] != "ok" || body["status"] != "ok" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestHealthFailureDoesNotLeakDatabaseDetails(t *testing.T) {
	router := NewRouter(pingFunc(func(context.Context) error { return errors.New("secret database connection detail") }))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
	var body APIError
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "DATABASE_UNAVAILABLE" {
		t.Fatalf("unexpected body: %+v", body)
	}
	if body.Error.Message != "Veritabanına erişilemiyor." {
		t.Fatal("internal error exposed")
	}
}

func TestHealthPropagatesRequestCancellation(t *testing.T) {
	router := NewRouter(pingFunc(func(ctx context.Context) error {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("request cancellation not propagated")
		}
		return ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/health", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestUnknownEndpointReturnsConsistentError(t *testing.T) {
	router := NewRouter(pingFunc(func(context.Context) error { t.Fatal("unexpected database call"); return nil }))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
	var body APIError
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "NOT_FOUND" {
		t.Fatalf("unexpected body: %+v", body)
	}
}
