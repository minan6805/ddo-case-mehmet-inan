package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"destekmasa/internal/auth"
	mailinput "destekmasa/internal/email"
	"destekmasa/internal/tickets"
)

type fakeEmail struct {
	called    bool
	actor     auth.User
	duplicate bool
	failure   error
}

func (f *fakeEmail) IngestEmail(_ context.Context, u auth.User, _ []byte) (tickets.EmailReceipt, error) {
	f.called = true
	f.actor = u
	return tickets.EmailReceipt{TicketID: 42, Duplicate: f.duplicate}, f.failure
}
func (f *fakeEmail) Attachments(context.Context, auth.User, int64, int64) ([]tickets.Attachment, error) {
	f.called = true
	return []tickets.Attachment{}, f.failure
}
func (f *fakeEmail) DownloadAttachment(context.Context, auth.User, int64) (tickets.Attachment, error) {
	f.called = true
	return tickets.Attachment{ID: 1, Filename: "rapor ş.html", Data: []byte("<script>bad()</script>")}, f.failure
}
func emailRouter(f *fakeEmail, role string) http.Handler {
	return NewServicesRouter(Services{Authentication: &fakeAuth{user: auth.User{ID: 7, Role: role}}, Email: f})
}
func TestEmailHTTPGuards(t *testing.T) {
	for _, test := range []struct {
		name, role, content, origin, body string
		cookie                            bool
		status                            int
	}{
		{name: "anonymous", role: "admin", status: 401},
		{name: "employee", role: "employee", cookie: true, status: 403},
		{name: "specialist", role: "specialist", cookie: true, status: 403},
		{name: "asset manager", role: "asset_manager", cookie: true, status: 403},
		{name: "wrong mime", role: "admin", content: "application/json", cookie: true, status: 415},
		{name: "cross origin", role: "admin", content: "message/rfc822", origin: "https://foreign.example", cookie: true, status: 403},
		{name: "oversized", role: "admin", content: "message/rfc822", body: strings.Repeat("a", mailinput.MaxRaw+1), cookie: true, status: 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &fakeEmail{}
			r := httptest.NewRequest("POST", "/email/ingest", strings.NewReader(test.body))
			r.Header.Set("Content-Type", test.content)
			if test.cookie {
				r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
			}
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			w := httptest.NewRecorder()
			emailRouter(f, test.role).ServeHTTP(w, r)
			if w.Code != test.status || f.called {
				t.Fatalf("%d %s called=%v", w.Code, w.Body.String(), f.called)
			}
		})
	}
}
func TestEmailHTTPReceiptsAndErrors(t *testing.T) {
	for _, test := range []struct {
		failure   error
		duplicate bool
		status    int
	}{{nil, false, 201}, {nil, true, 200}, {tickets.ErrInvalid, false, 400}, {tickets.ErrConflict, false, 409}, {tickets.ErrForbidden, false, 403}, {tickets.ErrTransition, false, 409}} {
		f := &fakeEmail{failure: test.failure, duplicate: test.duplicate}
		r := httptest.NewRequest("POST", "/email/ingest", strings.NewReader("raw"))
		r.Header.Set("Content-Type", "message/rfc822")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
		w := httptest.NewRecorder()
		emailRouter(f, "team_lead").ServeHTTP(w, r)
		if w.Code != test.status || f.actor.ID != 7 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
func TestAttachmentHTTPDownloadAndAuthentication(t *testing.T) {
	for _, path := range []string{"/attachments/1", "/tickets/1/comments/2/attachments"} {
		f := &fakeEmail{}
		w := authRequest(emailRouter(f, "employee"), "GET", path, "", false)
		if w.Code != 401 || f.called {
			t.Fatalf("anonymous %s: %d", path, w.Code)
		}
	}
	f := &fakeEmail{}
	w := authRequest(emailRouter(f, "employee"), "GET", "/attachments/1", "", true)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" || w.Body.String() != "<script>bad()</script>" {
		t.Fatalf("download %d %v", w.Code, w.Header())
	}
	f = &fakeEmail{failure: tickets.ErrNotFound}
	w = authRequest(emailRouter(f, "employee"), "GET", "/attachments/1", "", true)
	if w.Code != 404 {
		t.Fatalf("access %d", w.Code)
	}
}
