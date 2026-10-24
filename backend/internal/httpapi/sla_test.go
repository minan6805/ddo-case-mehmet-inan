package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"destekmasa/internal/auth"
	"destekmasa/internal/sla"
)

type fakeSLA struct {
	called  bool
	userID  int64
	staff   bool
	failure error
}

func (f *fakeSLA) Settings(context.Context) (sla.Settings, error) {
	f.called = true
	return sla.Settings{}, f.failure
}
func (f *fakeSLA) Ticket(_ context.Context, userID int64, staff bool, _ int64) (sla.Snapshot, error) {
	f.called = true
	f.userID = userID
	f.staff = staff
	return sla.Snapshot{}, f.failure
}
func (f *fakeSLA) UpdateCalendar(context.Context, int64, sla.Calendar) (sla.Calendar, error) {
	f.called = true
	return sla.Calendar{}, f.failure
}
func (f *fakeSLA) UpdatePolicy(context.Context, int64, sla.Policy) (sla.Policy, error) {
	f.called = true
	return sla.Policy{}, f.failure
}
func (f *fakeSLA) Alerts(context.Context, int64, int) ([]sla.Alert, error) {
	f.called = true
	return []sla.Alert{}, f.failure
}
func slaRouter(f *fakeSLA, role string) http.Handler {
	return NewSLAApplicationRouter(pingFunc(func(context.Context) error { return nil }), &fakeAuth{user: auth.User{ID: 7, Role: role}}, AuthOptions{}, nil, f)
}
func TestSLAEndpointsRequireAuthentication(t *testing.T) {
	for _, path := range []string{"/tickets/1/sla", "/sla/settings", "/sla/alerts"} {
		f := &fakeSLA{}
		response := authRequest(slaRouter(f, "employee"), "GET", path, "", false)
		if response.Code != 401 || f.called {
			t.Fatalf("unprotected endpoint %s", path)
		}
	}
}
func TestSLASettingsAndAlertsStaffOnly(t *testing.T) {
	for _, role := range []string{"employee", "asset_manager", "specialist", "team_lead", "admin"} {
		for _, path := range []string{"/sla/settings", "/sla/alerts"} {
			f := &fakeSLA{}
			response := authRequest(slaRouter(f, role), "GET", path, "", true)
			allowed := role == "specialist" || role == "team_lead" || role == "admin"
			expected := 403
			if allowed {
				expected = 200
			}
			if response.Code != expected || f.called != allowed {
				t.Fatalf("%s %s: %d", role, path, response.Code)
			}
		}
	}
}
func TestSLASettingsMutationAdminOnly(t *testing.T) {
	for _, role := range []string{"employee", "asset_manager", "specialist", "team_lead", "admin"} {
		for _, path := range []string{"/sla/calendar", "/sla/policies/general/normal"} {
			f := &fakeSLA{}
			response := authRequest(slaRouter(f, role), "PUT", path, `{}`, true)
			expected := 403
			if role == "admin" {
				expected = 200
			}
			if response.Code != expected || f.called != (role == "admin") {
				t.Fatalf("%s %s: %d", role, path, response.Code)
			}
		}
	}
}
func TestSLADetailPassesAuthenticatedOwnership(t *testing.T) {
	f := &fakeSLA{failure: sla.ErrForbidden}
	response := authRequest(slaRouter(f, "employee"), "GET", "/tickets/1/sla", "", true)
	if response.Code != 403 || f.userID != 7 || f.staff {
		t.Fatal("ownership context incorrect")
	}
}
func TestSLACalendarRejectsCrossOriginBeforeMutation(t *testing.T) {
	f := &fakeSLA{}
	request := httptest.NewRequest("PUT", "/sla/calendar", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	response := httptest.NewRecorder()
	slaRouter(f, "admin").ServeHTTP(response, request)
	if response.Code != 403 || f.called {
		t.Fatal("cross-origin calendar write allowed")
	}
}
func TestSLAPreviewIsReadOnlyAndUsesExplicitTimes(t *testing.T) {
	f := &fakeSLA{}
	body := `{"calendar":{"timezone":"Europe/Istanbul","start_minute":510,"end_minute":1080,"version":1,"holidays":[]},"history":[{"status":"open","at":"2026-10-23T17:30:00+03:00"}],"as_of":"2026-10-23T17:30:00+03:00","response_minutes":60,"resolution_minutes":480}`
	response := authRequest(slaRouter(f, "specialist"), "POST", "/sla/preview", body, true)
	if response.Code != 200 || f.called || !strings.Contains(response.Body.String(), "2026-10-26T06:00:00Z") {
		t.Fatalf("preview: %d %s", response.Code, response.Body.String())
	}
}
