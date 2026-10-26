package httpapi

import (
	"context"
	"errors"
	"net/http"

	"destekmasa/internal/auth"
	"destekmasa/internal/tickets"
)

type ThreadService interface {
	Merge(context.Context, auth.User, int64, int64, string) (tickets.ThreadResult, error)
	Split(context.Context, auth.User, int64, tickets.SplitInput, string) (tickets.ThreadResult, error)
	ThreadHistory(context.Context, auth.User, int64, int64, int) ([]tickets.ThreadOperation, error)
}

func registerThreads(mux *http.ServeMux, authentication Authenticator, service ThreadService) {
	for _, kind := range []string{"merge", "split"} {
		mux.HandleFunc("POST /tickets/{id}/"+kind, requireRole(authentication, []string{"team_lead", "admin"}, func(w http.ResponseWriter, r *http.Request) {
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
			var result tickets.ThreadResult
			var err error
			user := currentUser(r)
			if kind == "merge" {
				var input struct {
					TargetID int64 `json:"target_id"`
				}
				if !decodeJSON(w, r, &input) {
					return
				}
				result, err = service.Merge(r.Context(), user, id, input.TargetID, key)
			} else {
				var input tickets.SplitInput
				if !decodeJSON(w, r, &input) {
					return
				}
				result, err = service.Split(r.Context(), user, id, input, key)
			}
			if err != nil {
				threadError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"result": result})
		}))
	}
	mux.HandleFunc("GET /tickets/{id}/operations", requireRole(authentication, nil, func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		before, limit, ok := pageParams(w, r)
		if !ok {
			return
		}
		items, err := service.ThreadHistory(r.Context(), currentUser(r), id, before, limit)
		if err != nil {
			threadError(w, err)
			return
		}
		items, cursor := trimPage(items, limit, func(item tickets.ThreadOperation) int64 { return item.ID })
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": cursor})
	}))
}
func threadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tickets.ErrInvalid):
		writeError(w, 400, "INVALID_INPUT", "Geçerli bir hedef talep veya başlık ve bu talebe ait 1–100 farklı mesaj seçin.")
	case errors.Is(err, tickets.ErrTransition):
		writeError(w, 409, "INVALID_THREAD_OPERATION", "Kapalı veya birleştirilmiş talepte bu işlem yapılamaz.")
	case errors.Is(err, tickets.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Yalnızca lider/admin aynı talep edenin kayıtlarını birleştirebilir veya bölebilir.")
	default:
		ticketError(w, err)
	}
}
