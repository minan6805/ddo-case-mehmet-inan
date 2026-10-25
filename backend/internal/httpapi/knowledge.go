package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"destekmasa/internal/auth"
	"destekmasa/internal/knowledge"
	"destekmasa/internal/tickets"
)

type KnowledgeService interface {
	List(context.Context, auth.User, knowledge.Filter) ([]knowledge.Article, error)
	Detail(context.Context, auth.User, int64) (knowledge.Article, error)
	Create(context.Context, auth.User, knowledge.Input, string) (knowledge.Article, error)
	Update(context.Context, auth.User, int64, knowledge.Input) (knowledge.Article, error)
}

func knowledgeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, knowledge.ErrInvalid):
		writeError(w, 400, "INVALID_ARTICLE", "Başlık, içerik, kategori veya yayın geçişi geçersiz. Yayımlanmış içeriği değiştirmek için önce taslağa alın.")
	case errors.Is(err, knowledge.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Bu makaleyi düzenleme veya yayımlama yetkiniz yok.")
	case errors.Is(err, knowledge.ErrNotFound):
		writeError(w, 404, "NOT_FOUND", "Makale bulunamadı.")
	case errors.Is(err, knowledge.ErrConflict):
		writeError(w, 409, "ARTICLE_CONFLICT", "Makale değişti veya işlem anahtarı farklı içerikle kullanıldı. Yenileyip tekrar deneyin.")
	default:
		writeError(w, 503, "KNOWLEDGE_UNAVAILABLE", "Bilgi bankasına şu an erişilemiyor.")
	}
}
func registerKnowledge(mux *http.ServeMux, authentication Authenticator, service KnowledgeService) {
	read := func(h http.HandlerFunc) http.HandlerFunc { return requireRole(authentication, nil, h) }
	write := func(h http.HandlerFunc) http.HandlerFunc {
		return requireRole(authentication, []string{"specialist", "team_lead", "admin"}, h)
	}
	mux.HandleFunc("GET /knowledge", read(func(w http.ResponseWriter, r *http.Request) {
		before, limit, ok := pageParams(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		items, err := service.List(r.Context(), currentUser(r), knowledge.Filter{Before: before, Limit: limit, Query: q.Get("q"), Category: q.Get("category"), Status: q.Get("status")})
		if err != nil {
			knowledgeError(w, err)
			return
		}
		cursor := ""
		if len(items) > limit {
			items = items[:limit]
			cursor = strconv.FormatInt(items[len(items)-1].ID, 10)
		}
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": cursor})
	}))
	mux.HandleFunc("GET /knowledge/{id}", read(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		a, err := service.Detail(r.Context(), currentUser(r), id)
		if err != nil {
			knowledgeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"article": a})
	}))
	mux.HandleFunc("POST /knowledge", write(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !tickets.ValidKey(key) {
			writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "1–128 karakterlik Idempotency-Key başlığı gönderin.")
			return
		}
		var in knowledge.Input
		if !decodeJSON(w, r, &in) {
			return
		}
		a, err := service.Create(r.Context(), currentUser(r), in, key)
		if err != nil {
			knowledgeError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"article": a})
	}))
	mux.HandleFunc("PUT /knowledge/{id}", write(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in knowledge.Input
		if !decodeJSON(w, r, &in) {
			return
		}
		a, err := service.Update(r.Context(), currentUser(r), id, in)
		if err != nil {
			knowledgeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"article": a})
	}))
}
