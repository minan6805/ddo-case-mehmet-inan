package httpapi

import (
	"context"
	"destekmasa/internal/assets"
	"destekmasa/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeAssets struct{ called bool }

func (f *fakeAssets) Owners(context.Context, auth.User, int64, int) ([]auth.User, error) {
	f.called = true
	return []auth.User{}, nil
}
func (f *fakeAssets) List(context.Context, auth.User, int64, int, string) ([]assets.Asset, error) {
	f.called = true
	return []assets.Asset{}, nil
}
func (f *fakeAssets) Detail(context.Context, auth.User, int64) (assets.Detail, error) {
	f.called = true
	return assets.Detail{}, nil
}
func (f *fakeAssets) Create(context.Context, auth.User, assets.Input, string) (assets.Asset, error) {
	f.called = true
	return assets.Asset{ID: 1}, nil
}
func (f *fakeAssets) Update(context.Context, auth.User, int64, assets.Input) (assets.Asset, error) {
	f.called = true
	return assets.Asset{ID: 1}, nil
}
func (f *fakeAssets) Dependency(context.Context, auth.User, int64, int64, bool) error {
	f.called = true
	return nil
}
func (f *fakeAssets) Impact(context.Context, auth.User, []int64) (assets.Impact, error) {
	f.called = true
	return assets.Impact{}, nil
}
func assetRouter(f *fakeAssets, role string) http.Handler {
	return NewServicesRouter(Services{Authentication: &fakeAuth{user: auth.User{ID: 7, Role: role}}, Assets: f})
}
func TestAssetHTTPAuthorizationAndInputs(t *testing.T) {
	for _, path := range []string{"/assets", "/assets/1", "/assets/1/impact"} {
		for _, cookie := range []bool{false, true} {
			f := &fakeAssets{}
			w := authRequest(assetRouter(f, "employee"), "GET", path, "", cookie)
			want := 401
			if cookie {
				want = 403
			}
			if w.Code != want || f.called {
				t.Fatalf("%s %d", path, w.Code)
			}
		}
	}
	for _, role := range []string{"employee", "specialist", "team_lead"} {
		for _, method := range []string{"PUT", "DELETE"} {
			f := &fakeAssets{}
			w := authRequest(assetRouter(f, role), method, "/assets/1/dependencies/2", "", true)
			if w.Code != 403 || f.called {
				t.Fatalf("role %s %d", role, w.Code)
			}
		}
	}
	for _, path := range []string{"/assets?limit=0", "/assets?before=bad", "/assets/0/impact"} {
		f := &fakeAssets{}
		w := authRequest(assetRouter(f, "admin"), "GET", path, "", true)
		if w.Code != 400 || f.called {
			t.Fatalf("input %s %d", path, w.Code)
		}
	}
	f := &fakeAssets{}
	w := authRequest(assetRouter(f, "admin"), "POST", "/assets", `{}`, true)
	if w.Code != 400 || f.called {
		t.Fatal("missing key accepted")
	}
	r := httptest.NewRequest("POST", "/assets", strings.NewReader(`{"code":"SRV-1","name":"Sunucu","kind":"server","state":"active"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "test")
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
	w = httptest.NewRecorder()
	assetRouter(f, "asset_manager").ServeHTTP(w, r)
	if w.Code != 201 || !f.called {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
}
