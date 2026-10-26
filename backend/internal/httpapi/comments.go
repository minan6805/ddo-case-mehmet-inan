package httpapi

import (
	"context"
	"errors"
	"net/http"

	"destekmasa/internal/auth"
	"destekmasa/internal/tickets"
)

type CommentService interface {
	Comments(context.Context, auth.User, int64, int64, int) ([]tickets.Comment, error)
	AddComment(context.Context, auth.User, int64, tickets.CommentInput, string) (tickets.Comment, error)
}

func registerComments(mux *http.ServeMux, authentication Authenticator, service CommentService) {
	mux.HandleFunc("GET /tickets/{id}/comments", requireRole(authentication, nil, func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		before, limit, ok := pageParams(w, r)
		if !ok {
			return
		}
		items, err := service.Comments(r.Context(), currentUser(r), id, before, limit)
		if err != nil {
			commentError(w, err)
			return
		}
		items, cursor := trimPage(items, limit, func(item tickets.Comment) int64 { return item.ID })
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": cursor})
	}))
	mux.HandleFunc("POST /tickets/{id}/comments", requireRole(authentication, nil, func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !tickets.ValidKey(key) {
			writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "1–128 karakterlik Idempotency-Key başlığı gönderin.")
			return
		}
		var input tickets.CommentInput
		if !decodeJSON(w, r, &input) {
			return
		}
		c, err := service.AddComment(r.Context(), currentUser(r), id, input, key)
		if err != nil {
			commentError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"comment": c})
	}))
}

func commentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tickets.ErrInvalid):
		writeError(w, 400, "INVALID_INPUT", "Mesaj 1–10000 karakter olmalı; görünürlük external veya internal olmalı.")
	case errors.Is(err, tickets.ErrTransition):
		writeError(w, 409, "TICKET_CLOSED", "Kapalı talebe yeni mesaj eklenemez.")
	default:
		ticketError(w, err)
	}
}
