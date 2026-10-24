package httpapi

import (
	"context"
	"destekmasa/internal/assets"
	"destekmasa/internal/auth"
	"destekmasa/internal/changes"
	"destekmasa/internal/tickets"
	"errors"
	"net/http"
	"strconv"
)

type ChangeService interface {
	Create(context.Context, auth.User, changes.Input, string) (changes.Change, error)
	Update(context.Context, auth.User, int64, changes.Input) (changes.Change, error)
	Get(context.Context, auth.User, int64) (changes.Change, error)
	List(context.Context, auth.User, int64, int, string) ([]changes.Change, error)
	Action(context.Context, auth.User, int64, changes.ActionInput, string) (changes.Change, error)
	Preview(context.Context, auth.User, int64) (changes.Preview, error)
	History(context.Context, auth.User, int64, int64, int) ([]changes.Event, error)
}

func changeError(w http.ResponseWriter, err error) {
	var conflict *changes.ConflictError
	if errors.As(err, &conflict) {
		writeJSON(w, 409, map[string]any{"error": map[string]string{"code": "CHANGE_OVERLAP", "message": "Zaman penceresi onaylı/devam eden bir değişiklikle çakışıyor. Planı veya eski graf onayını güncelleyin."}, "conflicts": conflict.Items})
		return
	}
	switch {
	case errors.Is(err, changes.ErrInvalid):
		writeError(w, 400, "INVALID_CHANGE", "Başlık, plan, geri dönüş planı, risk, zaman aralığı, varlık seçimi veya işlem notu geçersiz.")
	case errors.Is(err, changes.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Yalnız plan sahibi/admin düzenleyebilir; CAB kararı bağımsız bir lider/admin gerektirir.")
	case errors.Is(err, changes.ErrNotFound):
		writeError(w, 404, "NOT_FOUND", "Değişiklik bulunamadı.")
	case errors.Is(err, changes.ErrConflict):
		writeError(w, 409, "CHANGE_CONFLICT", "Kayıt değişti veya işlem anahtarı farklı içerikle kullanıldı. Yenileyin.")
	case errors.Is(err, changes.ErrTransition):
		writeError(w, 409, "INVALID_CHANGE_TRANSITION", "Bu durum geçişi yapılamaz veya uygulama zamanı planlanan pencere dışında.")
	case errors.Is(err, changes.ErrStaleGraph):
		writeError(w, 409, "STALE_GRAPH", "Varlık grafı değişti. Planı taslağa alıp etkiyi yeniden hesaplatın ve CAB onayını yenileyin.")
	case errors.Is(err, assets.ErrInvalid), errors.Is(err, assets.ErrNotFound), errors.Is(err, assets.ErrLimit):
		assetError(w, err)
	default:
		writeError(w, 503, "CHANGE_UNAVAILABLE", "Değişiklik servisine şu an erişilemiyor.")
	}
}
func registerChanges(mux *http.ServeMux, authentication Authenticator, service ChangeService) {
	protect := func(h http.HandlerFunc) http.HandlerFunc {
		return requireRole(authentication, []string{"specialist", "team_lead", "asset_manager", "admin"}, h)
	}
	actor := func(r *http.Request) auth.User { return r.Context().Value(userContextKey{}).(auth.User) }
	mux.HandleFunc("GET /changes", protect(func(w http.ResponseWriter, r *http.Request) {
		before, limit, ok := pageParams(w, r)
		if !ok {
			return
		}
		items, err := service.List(r.Context(), actor(r), before, limit, r.URL.Query().Get("status"))
		if err != nil {
			changeError(w, err)
			return
		}
		cursor := ""
		if len(items) > limit {
			items = items[:limit]
			cursor = strconv.FormatInt(items[len(items)-1].ID, 10)
		}
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": cursor})
	}))
	mux.HandleFunc("POST /changes", protect(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !tickets.ValidKey(key) {
			writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key başlığı gönderin.")
			return
		}
		var input changes.Input
		if !decodeJSON(w, r, &input) {
			return
		}
		c, err := service.Create(r.Context(), actor(r), input, key)
		if err != nil {
			changeError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"change": c})
	}))
	mux.HandleFunc("GET /changes/{id}", protect(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		c, err := service.Get(r.Context(), actor(r), id)
		if err != nil {
			changeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"change": c})
	}))
	mux.HandleFunc("PUT /changes/{id}", protect(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var input changes.Input
		if !decodeJSON(w, r, &input) {
			return
		}
		c, err := service.Update(r.Context(), actor(r), id, input)
		if err != nil {
			changeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"change": c})
	}))
	mux.HandleFunc("POST /changes/{id}/actions", protect(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !tickets.ValidKey(key) {
			writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key başlığı gönderin.")
			return
		}
		var input changes.ActionInput
		if !decodeJSON(w, r, &input) {
			return
		}
		c, err := service.Action(r.Context(), actor(r), id, input, key)
		if err != nil {
			changeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"change": c})
	}))
	mux.HandleFunc("GET /changes/{id}/impact", protect(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		preview, err := service.Preview(r.Context(), actor(r), id)
		if err != nil {
			changeError(w, err)
			return
		}
		writeJSON(w, 200, preview)
	}))
	mux.HandleFunc("GET /changes/{id}/history", protect(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		before, limit, ok := pageParams(w, r)
		if !ok {
			return
		}
		items, err := service.History(r.Context(), actor(r), id, before, limit)
		if err != nil {
			changeError(w, err)
			return
		}
		cursor := ""
		if len(items) > limit {
			items = items[:limit]
			cursor = strconv.FormatInt(items[len(items)-1].ID, 10)
		}
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": cursor})
	}))
}
