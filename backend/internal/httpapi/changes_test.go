package httpapi

import (
	"context"
	"destekmasa/internal/auth"
	"destekmasa/internal/changes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeChanges struct {
	called  bool
	actor   auth.User
	failure error
}

func (f *fakeChanges) Create(_ context.Context, u auth.User, _ changes.Input, _ string) (changes.Change, error) {
	f.called = true
	f.actor = u
	return changes.Change{ID: 1}, f.failure
}
func (f *fakeChanges) Update(context.Context, auth.User, int64, changes.Input) (changes.Change, error) {
	f.called = true
	return changes.Change{}, f.failure
}
func (f *fakeChanges) Get(context.Context, auth.User, int64) (changes.Change, error) {
	f.called = true
	return changes.Change{}, f.failure
}
func (f *fakeChanges) List(context.Context, auth.User, int64, int, string) ([]changes.Change, error) {
	f.called = true
	return []changes.Change{}, f.failure
}
func (f *fakeChanges) Action(_ context.Context, u auth.User, _ int64, _ changes.ActionInput, _ string) (changes.Change, error) {
	f.called = true
	f.actor = u
	return changes.Change{}, f.failure
}
func (f *fakeChanges) Preview(context.Context, auth.User, int64) (changes.Preview, error) {
	f.called = true
	return changes.Preview{}, f.failure
}
func (f *fakeChanges) History(context.Context, auth.User, int64, int64, int) ([]changes.Event, error) {
	f.called = true
	return []changes.Event{}, f.failure
}
func changeRouter(f *fakeChanges, role string) http.Handler {
	return NewServicesRouter(Services{Authentication: &fakeAuth{user: auth.User{ID: 7, Role: role}}, Changes: f})
}
func TestChangeHTTPAuthorizationAndInputGuards(t *testing.T) {
	for _, path := range []string{"/changes", "/changes/1", "/changes/1/impact", "/changes/1/history"} {
		for _, cookie := range []bool{false, true} {
			f := &fakeChanges{}
			w := authRequest(changeRouter(f, "employee"), "GET", path, "", cookie)
			want := 401
			if cookie {
				want = 403
			}
			if w.Code != want || f.called {
				t.Fatalf("%s %d", path, w.Code)
			}
		}
	}
	for _, path := range []string{"/changes", "/changes/1/actions"} {
		f := &fakeChanges{}
		w := authRequest(changeRouter(f, "admin"), "POST", path, `{}`, true)
		if w.Code != 400 || f.called {
			t.Fatal("missing key accepted")
		}
	}
	for _, body := range []string{`{"action":"approve","requester_id":99}`, `{"action":"approve","approved_by":99}`} {
		f := &fakeChanges{}
		r := httptest.NewRequest("POST", "/changes/1/actions", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "test")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
		w := httptest.NewRecorder()
		changeRouter(f, "team_lead").ServeHTTP(w, r)
		if w.Code != 400 || f.called {
			t.Fatal("spoof accepted")
		}
	}
	f := &fakeChanges{}
	r := httptest.NewRequest("POST", "/changes/1/actions", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "test")
	r.Header.Set("Origin", "https://foreign.example")
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
	w := httptest.NewRecorder()
	changeRouter(f, "team_lead").ServeHTTP(w, r)
	if w.Code != 403 || f.called {
		t.Fatal("foreign origin allowed")
	}
}
func TestChangeHTTPConflictHasConsistentErrorAndDetails(t *testing.T) {
	f := &fakeChanges{failure: &changes.ConflictError{Items: []changes.Conflict{{ID: 42, Title: "Çakışan plan"}}}}
	r := httptest.NewRequest("POST", "/changes/1/actions", strings.NewReader(`{"action":"approve","note":"CAB kararı","version":2}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "test")
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
	w := httptest.NewRecorder()
	changeRouter(f, "team_lead").ServeHTTP(w, r)
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Conflicts []changes.Conflict `json:"conflicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 409 || body.Error.Code != "CHANGE_OVERLAP" || len(body.Conflicts) != 1 || body.Conflicts[0].ID != 42 || f.actor.ID != 7 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
