package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"destekmasa/internal/assignment"
	"destekmasa/internal/auth"
)

type fakeAssignment struct {
	called  bool
	actor   int64
	failure error
}

func (f *fakeAssignment) Board(context.Context) (assignment.Board, error) {
	f.called = true
	return assignment.Board{}, f.failure
}
func (f *fakeAssignment) UpdateExpert(_ context.Context, _ int64, actor int64, _ assignment.ProfileInput) (assignment.Board, error) {
	f.called = true
	f.actor = actor
	return assignment.Board{}, f.failure
}
func (f *fakeAssignment) Retry(_ context.Context, _ int64, actor int64) error {
	f.called = true
	f.actor = actor
	return f.failure
}
func (f *fakeAssignment) History(context.Context, int64, int64, int) ([]assignment.History, error) {
	f.called = true
	return []assignment.History{}, f.failure
}
func assignmentRouter(f *fakeAssignment, role string) http.Handler {
	return NewServicesRouter(Services{Database: pingFunc(func(context.Context) error { return nil }), Authentication: &fakeAuth{user: auth.User{ID: 7, Role: role}}, Assignment: f})
}
func TestAssignmentEndpointsRequireAuthentication(t *testing.T) {
	for _, entry := range []struct{ method, path string }{{"GET", "/assignment/experts"}, {"PUT", "/assignment/experts/1"}, {"GET", "/tickets/1/assignments"}, {"PATCH", "/tickets/1/assignment"}} {
		f := &fakeAssignment{}
		response := authRequest(assignmentRouter(f, "employee"), entry.method, entry.path, `{}`, false)
		if response.Code != 401 || f.called {
			t.Errorf("unprotected %s", entry.path)
		}
	}
}
func TestAssignmentReadAndRetryRoleMatrix(t *testing.T) {
	for _, role := range []string{"employee", "asset_manager", "specialist", "team_lead", "admin"} {
		for _, entry := range []struct{ method, path string }{{"GET", "/assignment/experts"}, {"GET", "/tickets/1/assignments"}, {"PATCH", "/tickets/1/assignment"}} {
			f := &fakeAssignment{}
			response := authRequest(assignmentRouter(f, role), entry.method, entry.path, "", true)
			expected := 200
			if role == "employee" || role == "asset_manager" {
				expected = 403
			}
			if response.Code != expected || f.called != (expected == 200) {
				t.Fatalf("%s %s: %d", role, entry.path, response.Code)
			}
		}
	}
}
func TestExpertMutationLeaderAndAdminOnly(t *testing.T) {
	for _, role := range []string{"employee", "asset_manager", "specialist", "team_lead", "admin"} {
		f := &fakeAssignment{}
		response := authRequest(assignmentRouter(f, role), "PUT", "/assignment/experts/1", `{}`, true)
		expected := 403
		if role == "team_lead" || role == "admin" {
			expected = 200
		}
		if response.Code != expected || f.called != (expected == 200) {
			t.Fatalf("%s: %d", role, response.Code)
		}
		if f.called && f.actor != 7 {
			t.Fatal("wrong authenticated actor")
		}
	}
}
func TestExpertMutationRejectsCrossOrigin(t *testing.T) {
	f := &fakeAssignment{}
	request := httptest.NewRequest("PUT", "/assignment/experts/1", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	response := httptest.NewRecorder()
	assignmentRouter(f, "admin").ServeHTTP(response, request)
	if response.Code != 403 || f.called {
		t.Fatal("foreign origin changed expert")
	}
}
func TestAssignmentHistoryValidatesCursor(t *testing.T) {
	f := &fakeAssignment{}
	response := authRequest(assignmentRouter(f, "admin"), "GET", "/tickets/1/assignments?before=-1", "", true)
	if response.Code != 400 || f.called {
		t.Fatal("invalid history cursor accepted")
	}
}
