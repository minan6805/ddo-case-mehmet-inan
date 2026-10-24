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

type fakeTickets struct {
	called  bool
	options tickets.ListOptions
	actor   auth.User
	key     string
	failure error
}

func (f *fakeTickets) Categories(context.Context) ([]tickets.Category, error) {
	f.called = true
	return []tickets.Category{{Code: "general", Label: "Genel"}}, f.failure
}
func (f *fakeTickets) Create(_ context.Context, user auth.User, _ tickets.CreateInput, key string) (tickets.Ticket, error) {
	f.called = true
	f.actor = user
	f.key = key
	return tickets.Ticket{ID: 42}, f.failure
}
func (f *fakeTickets) Get(_ context.Context, user auth.User, _ int64) (tickets.Ticket, error) {
	f.called = true
	f.actor = user
	return tickets.Ticket{ID: 42}, f.failure
}
func (f *fakeTickets) List(_ context.Context, user auth.User, options tickets.ListOptions) ([]tickets.Ticket, error) {
	f.called = true
	f.actor = user
	f.options = options
	return []tickets.Ticket{}, f.failure
}
func (f *fakeTickets) UpdateStatus(_ context.Context, user auth.User, _ int64, _ string) (tickets.Ticket, error) {
	f.called = true
	f.actor = user
	return tickets.Ticket{ID: 42}, f.failure
}
func ticketRouter(f *fakeTickets, role string) http.Handler {
	return NewApplicationRouter(pingFunc(func(context.Context) error { return nil }), &fakeAuth{user: auth.User{ID: 7, Role: role}}, AuthOptions{}, f)
}
func TestTicketEndpointsRequireAuthentication(t *testing.T) {
	for _, endpoint := range []struct{ method, path string }{{"GET", "/tickets"}, {"POST", "/tickets"}, {"GET", "/tickets/1"}, {"PATCH", "/tickets/1/status"}, {"GET", "/ticket-categories"}} {
		f := &fakeTickets{}
		response := authRequest(ticketRouter(f, "employee"), endpoint.method, endpoint.path, "", false)
		if response.Code != 401 || f.called {
			t.Errorf("%s %s allowed without auth", endpoint.method, endpoint.path)
		}
	}
}
func TestTicketCreateRequiresIdempotencyKey(t *testing.T) {
	f := &fakeTickets{}
	response := authRequest(ticketRouter(f, "employee"), "POST", "/tickets", `{"title":"Test"}`, true)
	if response.Code != 400 || f.called {
		t.Fatal("create accepted without key")
	}
}
func TestTicketCreateUsesAuthenticatedActor(t *testing.T) {
	f := &fakeTickets{}
	request := httptest.NewRequest("POST", "/tickets", strings.NewReader(`{"title":"Test","description":"Açıklama","category":"general","priority":"normal"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "test-key")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test-token"})
	response := httptest.NewRecorder()
	ticketRouter(f, "employee").ServeHTTP(response, request)
	if response.Code != 201 || f.actor.ID != 7 || f.key != "test-key" || response.Header().Get("Location") != "/tickets/42" {
		t.Fatalf("creation: %d %+v", response.Code, f)
	}
}
func TestTicketCreateRejectsRequesterSpoofing(t *testing.T) {
	f := &fakeTickets{}
	request := httptest.NewRequest("POST", "/tickets", strings.NewReader(`{"title":"Test","requester_id":999}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "test-key")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test-token"})
	response := httptest.NewRecorder()
	ticketRouter(f, "employee").ServeHTTP(response, request)
	if response.Code != 400 || f.called {
		t.Fatal("requester spoofing accepted")
	}
}
func TestTicketStatusRestrictedToStaff(t *testing.T) {
	for _, role := range []string{"employee", "asset_manager"} {
		f := &fakeTickets{}
		response := authRequest(ticketRouter(f, role), "PATCH", "/tickets/1/status", `{"status":"in_progress"}`, true)
		if response.Code != 403 || f.called {
			t.Errorf("role %s changed status", role)
		}
	}
}
func TestTicketPaginationValidation(t *testing.T) {
	for _, query := range []string{"limit=0", "limit=101", "limit=bad", "before=-1", "before=bad", "before=0"} {
		f := &fakeTickets{}
		response := authRequest(ticketRouter(f, "employee"), "GET", "/tickets?"+query, "", true)
		if response.Code != 400 || f.called {
			t.Errorf("invalid query accepted: %s", query)
		}
	}
}
func TestTicketErrorHTTPStatuses(t *testing.T) {
	for _, entry := range []struct {
		err    error
		status int
	}{{tickets.ErrForbidden, 403}, {tickets.ErrNotFound, 404}, {tickets.ErrInvalid, 400}, {tickets.ErrConflict, 409}, {tickets.ErrTransition, 409}} {
		response := httptest.NewRecorder()
		ticketError(response, entry.err)
		if response.Code != entry.status {
			t.Errorf("error %v: %d", entry.err, response.Code)
		}
	}
}
func TestTicketIDMustBePositive(t *testing.T) {
	for _, id := range []string{"0", "-1", "abc", "999999999999999999999999"} {
		f := &fakeTickets{}
		response := authRequest(ticketRouter(f, "employee"), "GET", "/tickets/"+id, "", true)
		if response.Code != 400 || f.called {
			t.Errorf("invalid ID accepted: %s", id)
		}
	}
}
