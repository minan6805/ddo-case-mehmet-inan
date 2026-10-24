package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"destekmasa/internal/auth"
	"destekmasa/internal/tickets"
)

type fakeComments struct {
	called  bool
	actor   auth.User
	failure error
}

func (f *fakeComments) Comments(_ context.Context, u auth.User, _ int64, _ int64, _ int) ([]tickets.Comment, error) {
	f.called = true
	f.actor = u
	return []tickets.Comment{}, f.failure
}
func (f *fakeComments) AddComment(_ context.Context, u auth.User, _ int64, _ tickets.CommentInput, _ string) (tickets.Comment, error) {
	f.called = true
	f.actor = u
	return tickets.Comment{ID: 12}, f.failure
}
func commentRouter(f *fakeComments) http.Handler {
	return NewServicesRouter(Services{Authentication: &fakeAuth{user: auth.User{ID: 7, Role: "employee"}}, Comments: f})
}
func TestCommentHTTPGuards(t *testing.T) {
	for _, test := range []struct {
		name, method, path, body, key, origin string
		cookie                                bool
		status                                int
	}{
		{name: "anonymous read", method: "GET", path: "/tickets/1/comments", status: 401},
		{name: "anonymous write", method: "POST", path: "/tickets/1/comments", status: 401},
		{name: "missing key", method: "POST", path: "/tickets/1/comments", cookie: true, status: 400},
		{name: "invalid id", method: "GET", path: "/tickets/0/comments", cookie: true, status: 400},
		{name: "invalid limit", method: "GET", path: "/tickets/1/comments?limit=101", cookie: true, status: 400},
		{name: "invalid cursor", method: "GET", path: "/tickets/1/comments?before=0", cookie: true, status: 400},
		{name: "spoofed author", method: "POST", path: "/tickets/1/comments", body: `{"body":"x","visibility":"external","author_id":999}`, key: "test", cookie: true, status: 400},
		{name: "cross origin", method: "POST", path: "/tickets/1/comments", body: `{"body":"x","visibility":"external"}`, key: "test", origin: "https://foreign.example", cookie: true, status: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &fakeComments{}
			r := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", test.key)
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			if test.cookie {
				r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
			}
			w := httptest.NewRecorder()
			commentRouter(f).ServeHTTP(w, r)
			if w.Code != test.status || f.called {
				t.Fatalf("status %d body %s called %v", w.Code, w.Body.String(), f.called)
			}
		})
	}
}
func TestCommentHTTPActorAndErrors(t *testing.T) {
	for _, test := range []struct {
		failure error
		status  int
	}{{nil, 201}, {tickets.ErrForbidden, 403}, {tickets.ErrConflict, 409}, {tickets.ErrTransition, 409}, {tickets.ErrNotFound, 404}} {
		f := &fakeComments{failure: test.failure}
		r := httptest.NewRequest("POST", "/tickets/1/comments", strings.NewReader(`{"body":"test","visibility":"external"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "test")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
		w := httptest.NewRecorder()
		commentRouter(f).ServeHTTP(w, r)
		if w.Code != test.status || f.actor.ID != 7 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
