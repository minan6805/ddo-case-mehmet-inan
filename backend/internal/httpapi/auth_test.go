package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"destekmasa/internal/auth"
)

type fakeAuth struct {
	user       auth.User
	loginErr   error
	currentErr error
	logoutErr  error
	called     bool
	email      string
	password   string
	oldToken   string
}

func (f *fakeAuth) Login(_ context.Context, email, password, previous string) (auth.Session, error) {
	f.called = true
	f.email, f.password, f.oldToken = email, password, previous
	return auth.Session{User: f.user, Token: "random-test-token", ExpiresAt: time.Now().Add(time.Hour)}, f.loginErr
}
func (f *fakeAuth) Current(context.Context, string) (auth.User, error) {
	f.called = true
	return f.user, f.currentErr
}
func (f *fakeAuth) Logout(context.Context, string) error { f.called = true; return f.logoutErr }
func authRouter(f *fakeAuth) http.Handler {
	return NewAuthenticatedRouter(pingFunc(func(context.Context) error { return nil }), f, AuthOptions{})
}
func authRequest(handler http.Handler, method, path, body string, cookie bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie {
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test-token"})
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestLoginSetsPrivateCookieAndNormalizesEmail(t *testing.T) {
	f := &fakeAuth{user: auth.User{ID: 1, Email: "user@example.com", Role: "employee"}}
	response := authRequest(authRouter(f), "POST", "/auth/login", `{"email":" USER@example.com ","password":"test-password"}`, true)
	if response.Code != 200 {
		t.Fatalf("status: %d", response.Code)
	}
	if f.email != "user@example.com" || f.password != "test-password" || f.oldToken != "test-token" {
		t.Fatal("login parameters not forwarded correctly")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("session cookie missing")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge <= 0 {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	if strings.Contains(response.Body.String(), "random-test-token") || strings.Contains(response.Body.String(), "test-password") {
		t.Fatal("credential leaked in response")
	}
}

func TestLoginRejectsInvalidJSONBeforeAuthentication(t *testing.T) {
	cases := []string{
		`{`, `null`, `{}`, `{"email":"not-email","password":"x"}`,
		`{"email":"a@b.local","password":""}`,
		`{"email":"a@b.local","password":"x","role":"admin"}`,
		`{"email":"a@b.local","password":"x"} {}`,
		`{"email":"a@b.local","password":"` + strings.Repeat("a", 73) + `"}`,
		`{"email":"a@b.local","password":"` + strings.Repeat("a", 5000) + `"}`,
	}
	for _, body := range cases {
		t.Run(body[:min(len(body), 60)], func(t *testing.T) {
			f := &fakeAuth{}
			response := authRequest(authRouter(f), "POST", "/auth/login", body, false)
			if response.Code != 400 || f.called {
				t.Fatalf("invalid input reached auth or wrong status: %d", response.Code)
			}
		})
	}
}

func TestLoginRequiresJSON(t *testing.T) {
	f := &fakeAuth{}
	request := httptest.NewRequest("POST", "/auth/login", strings.NewReader("email=a&password=b"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	authRouter(f).ServeHTTP(response, request)
	if response.Code != 415 || f.called {
		t.Fatal("non-JSON login accepted")
	}
}

func TestLoginInvalidCredentialsReturns401WithoutCookie(t *testing.T) {
	f := &fakeAuth{loginErr: auth.ErrUnauthenticated}
	response := authRequest(authRouter(f), "POST", "/auth/login", `{"email":"user@example.com","password":"wrong"}`, false)
	if response.Code != 401 || len(response.Result().Cookies()) != 0 {
		t.Fatal("invalid credentials accepted")
	}
}

func TestAuthBackendFailureIs503WithoutInternalDetails(t *testing.T) {
	f := &fakeAuth{currentErr: errors.New("secret database password")}
	response := authRequest(authRouter(f), "GET", "/auth/me", "", true)
	if response.Code != 503 || strings.Contains(response.Body.String(), "secret") {
		t.Fatal("backend failure mishandled")
	}
}

func TestMissingSessionReturns401(t *testing.T) {
	f := &fakeAuth{}
	response := authRequest(authRouter(f), "GET", "/auth/me", "", false)
	if response.Code != 401 || f.called {
		t.Fatal("missing session accepted")
	}
}

func TestInvalidOrExpiredSessionReturns401(t *testing.T) {
	f := &fakeAuth{currentErr: auth.ErrUnauthenticated}
	response := authRequest(authRouter(f), "GET", "/auth/me", "", true)
	if response.Code != 401 {
		t.Fatal("invalid session accepted")
	}
}

func TestRoleAuthorizationForEveryRole(t *testing.T) {
	for _, role := range []string{"employee", "specialist", "team_lead", "asset_manager", "admin"} {
		t.Run(role, func(t *testing.T) {
			f := &fakeAuth{user: auth.User{ID: 1, Role: role}}
			response := authRequest(authRouter(f), "GET", "/admin/overview", "", true)
			expected := 403
			if role == "admin" {
				expected = 200
			}
			if response.Code != expected {
				t.Fatalf("status: %d, expected: %d", response.Code, expected)
			}
		})
	}
}

func TestMeReturnsAuthenticatedUser(t *testing.T) {
	f := &fakeAuth{user: auth.User{ID: 9, Email: "user@example.com", Role: "employee"}}
	response := authRequest(authRouter(f), "GET", "/auth/me", "", true)
	var body struct {
		User auth.User `json:"user"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || body.User.ID != 9 {
		t.Fatal("wrong current user")
	}
}

func TestLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	f := &fakeAuth{}
	response := authRequest(authRouter(f), "POST", "/auth/logout", "", true)
	cookies := response.Result().Cookies()
	if response.Code != 204 || !f.called || len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("logout did not revoke and clear session")
	}
}

func TestCrossOriginWritesRejectedBeforeAuth(t *testing.T) {
	for _, path := range []string{"/auth/login", "/auth/logout"} {
		t.Run(path, func(t *testing.T) {
			f := &fakeAuth{}
			request := httptest.NewRequest("POST", "http://localhost:3000"+path, strings.NewReader(`{"email":"user@example.com","password":"password"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "https://attacker.example")
			response := httptest.NewRecorder()
			authRouter(f).ServeHTTP(response, request)
			if response.Code != 403 || f.called {
				t.Fatal("cross-origin write accepted")
			}
		})
	}
}

func TestSameOriginWriteAllowedWithPort(t *testing.T) {
	f := &fakeAuth{}
	request := httptest.NewRequest("POST", "http://localhost:3000/auth/login", strings.NewReader(`{"email":"user@example.com","password":"password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()
	authRouter(f).ServeHTTP(response, request)
	if response.Code != 200 || !f.called {
		t.Fatal("valid same-origin write rejected")
	}
}

func TestSecureCookieOption(t *testing.T) {
	f := &fakeAuth{}
	router := NewAuthenticatedRouter(pingFunc(func(context.Context) error { return nil }), f, AuthOptions{SecureCookie: true})
	response := authRequest(router, "POST", "/auth/login", `{"email":"user@example.com","password":"password"}`, false)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatal("secure cookie option not applied")
	}
}
