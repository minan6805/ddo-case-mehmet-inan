package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"destekmasa/internal/assignment"
)

type AssignmentService interface {
	Board(context.Context) (assignment.Board, error)
	UpdateExpert(context.Context, int64, int64, assignment.ProfileInput) (assignment.Board, error)
	Retry(context.Context, int64, int64) error
	History(context.Context, int64, int64, int) ([]assignment.History, error)
}

func assignmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, assignment.ErrInvalid):
		writeError(w, 400, "INVALID_EXPERT", "Kapasite, beceri veya vardiya bilgisi geçersiz.")
	case errors.Is(err, assignment.ErrConflict):
		writeError(w, 409, "ASSIGNMENT_CONFLICT", "Uzman bilgisi değişti. Yenileyip tekrar deneyin.")
	case errors.Is(err, assignment.ErrNotFound):
		writeError(w, 404, "NOT_FOUND", "Uzman veya talep bulunamadı.")
	case errors.Is(err, assignment.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Bu atama işlemine yetkiniz yok.")
	default:
		writeError(w, 503, "ASSIGNMENT_UNAVAILABLE", "Atama servisine şu an erişilemiyor.")
	}
}
func registerAssignment(mux *http.ServeMux, authentication Authenticator, service AssignmentService) {
	staff := func(handler http.HandlerFunc) http.HandlerFunc {
		return requireRole(authentication, []string{"specialist", "team_lead", "admin"}, handler)
	}
	manager := func(handler http.HandlerFunc) http.HandlerFunc {
		return requireRole(authentication, []string{"team_lead", "admin"}, handler)
	}
	mux.HandleFunc("GET /assignment/experts", staff(func(w http.ResponseWriter, r *http.Request) {
		board, err := service.Board(r.Context())
		if err != nil {
			assignmentError(w, err)
			return
		}
		writeJSON(w, 200, board)
	}))
	mux.HandleFunc("PUT /assignment/experts/{id}", manager(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var input assignment.ProfileInput
		if !decodeJSON(w, r, &input) {
			return
		}
		actor := currentUser(r)
		board, err := service.UpdateExpert(r.Context(), id, actor.ID, input)
		if err != nil {
			assignmentError(w, err)
			return
		}
		writeJSON(w, 200, board)
	}))
	mux.HandleFunc("PATCH /tickets/{id}/assignment", staff(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		actor := currentUser(r)
		if err := service.Retry(r.Context(), id, actor.ID); err != nil {
			assignmentError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "checked"})
	}))
	mux.HandleFunc("GET /tickets/{id}/assignments", staff(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		before, limit, err := parsePagination(r)
		if err != nil {
			assignmentError(w, assignment.ErrInvalid)
			return
		}
		history, err := service.History(r.Context(), id, before, limit)
		if err != nil {
			assignmentError(w, err)
			return
		}
		cursor := ""
		if len(history) > limit {
			history = history[:limit]
			cursor = strconv.FormatInt(history[len(history)-1].ID, 10)
		}
		writeJSON(w, 200, map[string]any{"items": history, "next_cursor": cursor})
	}))
}
