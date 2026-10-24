package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"destekmasa/internal/auth"
	"destekmasa/internal/tickets"
)

type TicketService interface {
	Categories(context.Context) ([]tickets.Category, error)
	Create(context.Context, auth.User, tickets.CreateInput, string) (tickets.Ticket, error)
	Get(context.Context, auth.User, int64) (tickets.Ticket, error)
	List(context.Context, auth.User, tickets.ListOptions) ([]tickets.Ticket, error)
	UpdateStatus(context.Context, auth.User, int64, string) (tickets.Ticket, error)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, 415, "JSON_REQUIRED", "JSON gövdesi gönderilmelidir.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err != nil {
		writeError(w, 400, "INVALID_INPUT", "İstek alanları geçersiz.")
		return false
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		writeError(w, 400, "INVALID_INPUT", "Tek bir JSON nesnesi gönderilmelidir.")
		return false
	}
	return true
}
func ticketError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tickets.ErrInvalid):
		writeError(w, 400, "INVALID_INPUT", "Başlık, açıklama, kategori veya öncelik geçersiz.")
	case errors.Is(err, tickets.ErrNotFound):
		writeError(w, 404, "NOT_FOUND", "Talep bulunamadı.")
	case errors.Is(err, tickets.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Bu talep işlemi için yetkiniz yok.")
	case errors.Is(err, tickets.ErrConflict):
		writeError(w, 409, "IDEMPOTENCY_CONFLICT", "Aynı işlem anahtarı farklı içerikle kullanılamaz.")
	case errors.Is(err, tickets.ErrTransition):
		writeError(w, 409, "INVALID_TRANSITION", "Bu durum geçişine izin verilmiyor.")
	default:
		writeError(w, 503, "TICKET_UNAVAILABLE", "Talep servisine şu an erişilemiyor.")
	}
}
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, 400, "INVALID_ID", "Geçerli bir talep numarası gönderin.")
		return 0, false
	}
	return id, true
}
func registerTickets(mux *http.ServeMux, authentication Authenticator, service TicketService) {
	protect := func(handler http.HandlerFunc) http.HandlerFunc { return requireRole(authentication, nil, handler) }
	mux.HandleFunc("GET /ticket-categories", protect(func(w http.ResponseWriter, r *http.Request) {
		categories, err := service.Categories(r.Context())
		if err != nil {
			ticketError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": categories})
	}))
	mux.HandleFunc("POST /tickets", protect(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !tickets.ValidKey(key) {
			writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "1–128 karakterlik Idempotency-Key başlığı gönderin.")
			return
		}
		var input tickets.CreateInput
		if !decodeJSON(w, r, &input) {
			return
		}
		ticket, err := service.Create(r.Context(), currentUser(r), input, key)
		if err != nil {
			ticketError(w, err)
			return
		}
		w.Header().Set("Location", "/tickets/"+strconv.FormatInt(ticket.ID, 10))
		writeJSON(w, 201, map[string]any{"ticket": ticket})
	}))
	mux.HandleFunc("GET /tickets", protect(func(w http.ResponseWriter, r *http.Request) {
		before, limit, ok := pageParams(w, r)
		if !ok {
			return
		}
		options := tickets.ListOptions{Before: before, Limit: limit}
		options.Status = r.URL.Query().Get("status")
		options.Queue = r.URL.Query().Get("queue")
		items, err := service.List(r.Context(), currentUser(r), options)
		if err != nil {
			ticketError(w, err)
			return
		}
		cursor := ""
		if len(items) > options.Limit {
			items = items[:options.Limit]
			cursor = strconv.FormatInt(items[len(items)-1].ID, 10)
		}
		writeJSON(w, 200, tickets.Page{Items: items, NextCursor: cursor})
	}))
	mux.HandleFunc("GET /tickets/{id}", protect(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		ticket, err := service.Get(r.Context(), currentUser(r), id)
		if err != nil {
			ticketError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ticket": ticket})
	}))
	mux.HandleFunc("PATCH /tickets/{id}/status", requireRole(authentication, []string{"specialist", "team_lead", "admin"}, func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var input struct {
			Status string `json:"status"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		ticket, err := service.UpdateStatus(r.Context(), currentUser(r), id, input.Status)
		if err != nil {
			ticketError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ticket": ticket})
	}))
}
