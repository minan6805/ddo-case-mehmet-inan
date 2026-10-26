package httpapi

import (
	"context"
	"errors"
	"net/http"

	"destekmasa/internal/auth"
	"destekmasa/internal/simulator"
)

type SimulatorService interface {
	List(context.Context, auth.User, int64, int) ([]simulator.Run, error)
	Get(context.Context, auth.User, int64) (simulator.Run, error)
}

func simulatorError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, simulator.ErrInvalid):
		writeError(w, 400, "INVALID_SIMULATION", "Geçerli çalışma numarası ve sayfalama bilgisi gönderin.")
	case errors.Is(err, simulator.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Simülasyon durumunu yalnız lider ve yönetici görebilir.")
	case errors.Is(err, simulator.ErrNotFound):
		writeError(w, 404, "NOT_FOUND", "Simülasyon bulunamadı.")
	default:
		writeError(w, 503, "SIMULATOR_UNAVAILABLE", "Simülasyon durumu alınamadı.")
	}
}
func registerSimulator(mux *http.ServeMux, authentication Authenticator, service SimulatorService) {
	protect := func(h http.HandlerFunc) http.HandlerFunc {
		return requireRole(authentication, []string{"team_lead", "admin"}, h)
	}
	mux.HandleFunc("GET /simulations", protect(func(w http.ResponseWriter, r *http.Request) {
		before, limit, ok := pageParams(w, r)
		if !ok {
			return
		}
		items, err := service.List(r.Context(), currentUser(r), before, limit)
		if err != nil {
			simulatorError(w, err)
			return
		}
		items, cursor := trimPage(items, limit, func(item simulator.Run) int64 { return item.ID })
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": cursor})
	}))
	mux.HandleFunc("GET /simulations/{id}", protect(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		item, err := service.Get(r.Context(), currentUser(r), id)
		if err != nil {
			simulatorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"run": item})
	}))
}
