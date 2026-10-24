package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"destekmasa/internal/assets"
	"destekmasa/internal/auth"
	"destekmasa/internal/tickets"
)

type AssetService interface {
	Owners(context.Context, auth.User, int64, int) ([]auth.User, error)
	List(context.Context, auth.User, int64, int, string) ([]assets.Asset, error)
	Detail(context.Context, auth.User, int64) (assets.Detail, error)
	Create(context.Context, auth.User, assets.Input, string) (assets.Asset, error)
	Update(context.Context, auth.User, int64, assets.Input) (assets.Asset, error)
	Dependency(context.Context, auth.User, int64, int64, bool) error
	Impact(context.Context, auth.User, []int64) (assets.Impact, error)
}

func assetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, assets.ErrInvalid):
		writeError(w, 400, "INVALID_ASSET", "Varlık kodu, adı, türü, durumu veya bağlantısı geçersiz.")
	case errors.Is(err, assets.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Bu varlık işlemi için yetkiniz yok.")
	case errors.Is(err, assets.ErrNotFound):
		writeError(w, 404, "NOT_FOUND", "Varlık bulunamadı.")
	case errors.Is(err, assets.ErrConflict):
		writeError(w, 409, "ASSET_CONFLICT", "Kod/işlem anahtarı kullanılıyor veya kayıt değişti. Yenileyip tekrar deneyin.")
	case errors.Is(err, assets.ErrLimit):
		writeError(w, 409, "GRAPH_LIMIT", "Graf desteklenen büyüklük sınırını aşıyor.")
	default:
		writeError(w, 503, "ASSET_UNAVAILABLE", "Varlık servisine şu an erişilemiyor.")
	}
}
func pageParams(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	limit := 20
	var before int64
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
	}
	if err != nil || limit < 1 || limit > 100 {
		writeError(w, 400, "INVALID_PAGINATION", "Sayfa boyutu 1–100 arasında olmalı.")
		return 0, 0, false
	}
	if v := r.URL.Query().Get("before"); v != "" {
		before, err = strconv.ParseInt(v, 10, 64)
		if err != nil || before < 1 {
			writeError(w, 400, "INVALID_PAGINATION", "Sayfa imleci geçersiz.")
			return 0, 0, false
		}
	}
	return before, limit, true
}
func registerAssets(mux *http.ServeMux, authentication Authenticator, service AssetService) {
	read := func(h http.HandlerFunc) http.HandlerFunc {
		return requireRole(authentication, []string{"specialist", "team_lead", "asset_manager", "admin"}, h)
	}
	write := func(h http.HandlerFunc) http.HandlerFunc {
		return requireRole(authentication, []string{"asset_manager", "admin"}, h)
	}
	actor := func(r *http.Request) auth.User { return r.Context().Value(userContextKey{}).(auth.User) }
	mux.HandleFunc("GET /assets/owners", write(func(w http.ResponseWriter, r *http.Request) {
		before, limit, ok := pageParams(w, r)
		if !ok {
			return
		}
		items, err := service.Owners(r.Context(), actor(r), before, limit)
		if err != nil {
			assetError(w, err)
			return
		}
		cursor := ""
		if len(items) > limit {
			items = items[:limit]
			cursor = strconv.FormatInt(items[len(items)-1].ID, 10)
		}
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": cursor})
	}))

	mux.HandleFunc("GET /assets", read(func(w http.ResponseWriter, r *http.Request) {
		before, limit, ok := pageParams(w, r)
		if !ok {
			return
		}
		items, err := service.List(r.Context(), actor(r), before, limit, r.URL.Query().Get("kind"))
		if err != nil {
			assetError(w, err)
			return
		}
		cursor := ""
		if len(items) > limit {
			items = items[:limit]
			cursor = strconv.FormatInt(items[len(items)-1].ID, 10)
		}
		writeJSON(w, 200, assets.Page{Items: items, NextCursor: cursor})
	}))
	mux.HandleFunc("POST /assets", write(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !tickets.ValidKey(key) {
			writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "1–128 karakterlik Idempotency-Key başlığı gönderin.")
			return
		}
		var input assets.Input
		if !decodeJSON(w, r, &input) {
			return
		}
		a, err := service.Create(r.Context(), actor(r), input, key)
		if err != nil {
			assetError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"asset": a})
	}))
	mux.HandleFunc("GET /assets/{id}", read(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		a, err := service.Detail(r.Context(), actor(r), id)
		if err != nil {
			assetError(w, err)
			return
		}
		writeJSON(w, 200, a)
	}))
	mux.HandleFunc("PUT /assets/{id}", write(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var input assets.Input
		if !decodeJSON(w, r, &input) {
			return
		}
		a, err := service.Update(r.Context(), actor(r), id, input)
		if err != nil {
			assetError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"asset": a})
	}))
	for _, method := range []string{"PUT", "DELETE"} {
		mux.HandleFunc(method+" /assets/{id}/dependencies/{dependent}", write(func(w http.ResponseWriter, r *http.Request) {
			if !sameOrigin(w, r) {
				return
			}
			id, ok := pathID(w, r)
			if !ok {
				return
			}
			dependent, err := strconv.ParseInt(r.PathValue("dependent"), 10, 64)
			if err != nil || dependent < 1 {
				writeError(w, 400, "INVALID_ID", "Geçerli bir bağımlı varlık numarası gönderin.")
				return
			}
			if err = service.Dependency(r.Context(), actor(r), id, dependent, method == "PUT"); err != nil {
				assetError(w, err)
				return
			}
			writeJSON(w, 200, map[string]string{"status": "ok"})
		}))
	}
	mux.HandleFunc("GET /assets/{id}/impact", read(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		impact, err := service.Impact(r.Context(), actor(r), []int64{id})
		if err != nil {
			assetError(w, err)
			return
		}
		writeJSON(w, 200, impact)
	}))
}
