package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"destekmasa/internal/auth"
	"destekmasa/internal/simulator"
)

type fakeSimulator struct {
	called  bool
	failure error
}

func (f *fakeSimulator) List(context.Context, auth.User, int64, int) ([]simulator.Run, error) {
	f.called = true
	return []simulator.Run{}, f.failure
}
func (f *fakeSimulator) Get(context.Context, auth.User, int64) (simulator.Run, error) {
	f.called = true
	return simulator.Run{ID: 1}, f.failure
}
func simulatorRouter(f *fakeSimulator, role string) http.Handler {
	return NewServicesRouter(Services{Authentication: &fakeAuth{user: auth.User{ID: 7, Role: role}}, Simulator: f})
}
func TestSimulatorHTTPAccessAndErrors(t *testing.T) {
	for _, path := range []string{"/simulations", "/simulations/1"} {
		for _, role := range []string{"employee", "specialist", "asset_manager"} {
			for _, cookie := range []bool{false, true} {
				f := &fakeSimulator{}
				w := authRequest(simulatorRouter(f, role), "GET", path, "", cookie)
				want := 401
				if cookie {
					want = 403
				}
				if w.Code != want || f.called {
					t.Fatalf("%s %s %d", path, role, w.Code)
				}
			}
		}
	}
	for _, path := range []string{"/simulations?limit=101", "/simulations?before=bad", "/simulations/nope"} {
		f := &fakeSimulator{}
		w := authRequest(simulatorRouter(f, "admin"), "GET", path, "", true)
		if w.Code != 400 || f.called {
			t.Fatalf("%s %d", path, w.Code)
		}
	}
	for _, role := range []string{"team_lead", "admin"} {
		f := &fakeSimulator{}
		w := authRequest(simulatorRouter(f, role), "GET", "/simulations", "", true)
		if w.Code != 200 || !f.called {
			t.Fatal(w.Code)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{simulator.ErrNotFound, 404}, {simulator.ErrForbidden, 403}, {simulator.ErrInvalid, 400}, {errors.New("database details must not leak"), 503}} {
		f := &fakeSimulator{failure: tc.err}
		w := authRequest(simulatorRouter(f, "admin"), "GET", "/simulations/1", "", true)
		if w.Code != tc.status {
			t.Fatal(w.Code)
		}
	}
}
