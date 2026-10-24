package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"destekmasa/internal/auth"
	mailinput "destekmasa/internal/email"
	"destekmasa/internal/tickets"
)

type EmailService interface {
	IngestEmail(context.Context, auth.User, []byte) (tickets.EmailReceipt, error)
	Attachments(context.Context, auth.User, int64, int64) ([]tickets.Attachment, error)
	DownloadAttachment(context.Context, auth.User, int64) (tickets.Attachment, error)
}

func registerEmail(mux *http.ServeMux, authentication Authenticator, service EmailService) {
	mux.HandleFunc("POST /email/ingest", requireRole(authentication, []string{"admin", "team_lead"}, func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "message/rfc822" {
			writeError(w, 415, "EMAIL_REQUIRED", "message/rfc822 gövdesi gönderin.")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mailinput.MaxRaw))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, 413, "EMAIL_TOO_LARGE", "E-posta en fazla 4 MiB olabilir.")
			} else {
				writeError(w, 400, "INVALID_EMAIL", "E-posta okunamadı.")
			}
			return
		}
		receipt, err := service.IngestEmail(r.Context(), r.Context().Value(userContextKey{}).(auth.User), raw)
		if err != nil {
			emailError(w, err)
			return
		}
		status := 201
		if receipt.Duplicate {
			status = 200
		}
		writeJSON(w, status, map[string]any{"receipt": receipt})
	}))
	mux.HandleFunc("GET /tickets/{id}/comments/{comment}/attachments", requireRole(authentication, nil, func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		comment, err := strconv.ParseInt(r.PathValue("comment"), 10, 64)
		if err != nil || comment < 1 {
			writeError(w, 400, "INVALID_ID", "Geçerli bir mesaj numarası gönderin.")
			return
		}
		items, err := service.Attachments(r.Context(), r.Context().Value(userContextKey{}).(auth.User), id, comment)
		if err != nil {
			ticketError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	}))
	mux.HandleFunc("GET /attachments/{id}", requireRole(authentication, nil, func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		a, err := service.DownloadAttachment(r.Context(), r.Context().Value(userContextKey{}).(auth.User), id)
		if err != nil {
			ticketError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(a.Data)))
		w.WriteHeader(200)
		_, _ = w.Write(a.Data)
	}))
}
func emailError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tickets.ErrInvalid):
		writeError(w, 400, "INVALID_EMAIL", "E-posta geçersiz veya desteklenmeyen biçimde. Message-ID, gönderen, konu, UTF-8 düz metin ve ek sınırlarını kontrol edin.")
	case errors.Is(err, tickets.ErrConflict):
		writeError(w, 409, "MESSAGE_ID_CONFLICT", "Bu Message-ID farklı içerikle daha önce işlendi.")
	case errors.Is(err, tickets.ErrTransition):
		writeError(w, 409, "TICKET_CLOSED", "Kapalı talebe e-posta yanıtı eklenemez.")
	default:
		ticketError(w, err)
	}
}
