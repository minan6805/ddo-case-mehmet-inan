package httpapi

import (
	"context"
	"destekmasa/internal/auth"
	"destekmasa/internal/tickets"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeThreads struct {
	called  string
	failure error
}

func (f *fakeThreads) Merge(context.Context, auth.User, int64, int64, string) (tickets.ThreadResult, error) {
	f.called = "merge"
	return tickets.ThreadResult{TargetID: 2}, f.failure
}
func (f *fakeThreads) Split(context.Context, auth.User, int64, tickets.SplitInput, string) (tickets.ThreadResult, error) {
	f.called = "split"
	return tickets.ThreadResult{TargetID: 2}, f.failure
}
func (f *fakeThreads) ThreadHistory(context.Context, auth.User, int64, int64, int) ([]tickets.ThreadOperation, error) {
	f.called = "history"
	return []tickets.ThreadOperation{}, f.failure
}
func threadRouter(f *fakeThreads, role string) http.Handler {
	return NewServicesRouter(Services{Authentication: &fakeAuth{user: auth.User{ID: 7, Role: role}}, Threads: f})
}
func TestThreadHTTPGuardsAndDispatch(t *testing.T) {
	for _, kind := range []string{"merge", "split"} {
		for _, role := range []string{"employee", "asset_manager", "specialist"} {
			f := &fakeThreads{}
			w := authRequest(threadRouter(f, role), "POST", "/tickets/1/"+kind, `{}`, true)
			if w.Code != 403 || f.called != "" {
				t.Fatalf("%s/%s %d", kind, role, w.Code)
			}
		}
		for _, role := range []string{"team_lead", "admin"} {
			f := &fakeThreads{}
			r := httptest.NewRequest("POST", "/tickets/1/"+kind, strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", "test")
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
			w := httptest.NewRecorder()
			threadRouter(f, role).ServeHTTP(w, r)
			if w.Code != 200 || f.called != kind {
				t.Fatalf("dispatch %d %s", w.Code, f.called)
			}
		}
		f := &fakeThreads{}
		w := authRequest(threadRouter(f, "admin"), "POST", "/tickets/1/"+kind, `{}`, true)
		if w.Code != 400 || f.called != "" {
			t.Fatal("missing key accepted")
		}
		f = &fakeThreads{}
		w = authRequest(threadRouter(f, "admin"), "POST", "/tickets/1/"+kind, `{}`, false)
		if w.Code != 401 || f.called != "" {
			t.Fatal("anonymous accepted")
		}
	}
	f := &fakeThreads{}
	w := authRequest(threadRouter(f, "employee"), "GET", "/tickets/1/operations?before=bad", "", true)
	if w.Code != 400 || f.called != "" {
		t.Fatal("bad cursor accepted")
	}
	f = &fakeThreads{}
	w = authRequest(threadRouter(f, "employee"), "GET", "/tickets/1/operations", "", true)
	if w.Code != 200 || f.called != "history" {
		t.Fatal("history unavailable")
	}
}
