package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"destekmasa/internal/auth"
)

const sessionCookie = "destekmasa_session"

type Authenticator interface {
	Login(context.Context, string, string, string) (auth.Session, error)
	Current(context.Context, string) (auth.User, error)
	Logout(context.Context, string) error
}

type AuthOptions struct{ SecureCookie bool }

type userContextKey struct{}

func sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setSessionCookie(w http.ResponseWriter, value string, expires time.Time, secure bool, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: value, Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge,
	})
}

// SameSite=Strict plus origin validation protects cookie-authenticated writes.
// Requests without Origin are supported for non-browser verification clients.
func sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, http.StatusForbidden, "ORIGIN_REJECTED", "Bu kaynaktan işlem yapılamaz.")
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Host != r.Host || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			writeError(w, http.StatusForbidden, "ORIGIN_REJECTED", "Bu kaynaktan işlem yapılamaz.")
			return false
		}
	}
	return true
}

func decodeLogin(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "JSON_REQUIRED", "JSON gövdesi gönderilmelidir.")
		return "", "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err = decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Geçerli bir e-posta ve parola gönderin.")
		return "", "", false
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Tek bir JSON nesnesi gönderilmelidir.")
		return "", "", false
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || len(input.Password) == 0 || len(input.Password) > 72 {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Geçerli bir e-posta ve parola gönderin.")
		return "", "", false
	}
	return email, input.Password, true
}

func authError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrUnauthenticated) {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "E-posta veya parola hatalı ya da oturum geçersiz.")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "Giriş servisine şu an erişilemiyor.")
}

func requireRole(service Authenticator, roles []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := sessionToken(r)
		if token == "" {
			authError(w, auth.ErrUnauthenticated)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		user, err := service.Current(ctx, token)
		if err != nil {
			authError(w, err)
			return
		}
		if len(roles) > 0 {
			allowed := false
			for _, role := range roles {
				if role == user.Role {
					allowed = true
					break
				}
			}
			if !allowed {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "Bu işlem için yetkiniz yok.")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(ctx, userContextKey{}, user)))
	}
}

func registerAuth(mux *http.ServeMux, service Authenticator, options AuthOptions) {
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		email, password, ok := decodeLogin(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		session, err := service.Login(ctx, email, password, sessionToken(r))
		if err != nil {
			authError(w, err)
			return
		}
		setSessionCookie(w, session.Token, session.ExpiresAt, options.SecureCookie, int(time.Until(session.ExpiresAt).Seconds()))
		writeJSON(w, http.StatusOK, map[string]any{"user": session.User})
	})
	mux.HandleFunc("GET /auth/me", requireRole(service, nil, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"user": r.Context().Value(userContextKey{})})
	}))
	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		err := service.Logout(ctx, sessionToken(r))
		if err != nil {
			authError(w, err)
			return
		}
		setSessionCookie(w, "", time.Unix(1, 0), options.SecureCookie, -1)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /admin/overview", requireRole(service, []string{"admin"}, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "Yönetici erişimi doğrulandı."})
	}))
}

// currentUser is only called by handlers wrapped in requireRole.
func currentUser(r *http.Request) auth.User {
	return r.Context().Value(userContextKey{}).(auth.User)
}
