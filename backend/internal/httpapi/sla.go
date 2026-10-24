package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"destekmasa/internal/sla"
	"destekmasa/internal/tickets"
)

type SLAService interface {
	Settings(context.Context) (sla.Settings, error)
	Ticket(context.Context, int64, bool, int64) (sla.Snapshot, error)
	UpdateCalendar(context.Context, int64, sla.Calendar) (sla.Calendar, error)
	UpdatePolicy(context.Context, int64, sla.Policy) (sla.Policy, error)
	Alerts(context.Context, int64, int) ([]sla.Alert, error)
}

func slaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sla.ErrInvalid):
		writeError(w, 400, "INVALID_SLA", "SLA süresi veya takvim bilgisi geçersiz.")
	case errors.Is(err, sla.ErrConflict):
		writeError(w, 409, "SLA_VERSION_CONFLICT", "Ayarlar başka bir işlemde değişti. Yenileyip tekrar deneyin.")
	case errors.Is(err, sla.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Bu talebin SLA bilgisine erişiminiz yok.")
	case errors.Is(err, sla.ErrNotFound):
		writeError(w, 404, "NOT_FOUND", "Talep bulunamadı.")
	default:
		writeError(w, 503, "SLA_UNAVAILABLE", "SLA bilgisi şu an alınamıyor.")
	}
}
func registerSLA(mux *http.ServeMux, authentication Authenticator, service SLAService) {
	staff := func(handler http.HandlerFunc) http.HandlerFunc {
		return requireRole(authentication, []string{"specialist", "team_lead", "admin"}, handler)
	}
	admin := func(handler http.HandlerFunc) http.HandlerFunc {
		return requireRole(authentication, []string{"admin"}, handler)
	}
	mux.HandleFunc("GET /tickets/{id}/sla", requireRole(authentication, nil, func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		user := currentUser(r)
		result, err := service.Ticket(r.Context(), user.ID, tickets.IsStaff(user.Role), id)
		if err != nil {
			slaError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"sla": result})
	}))
	mux.HandleFunc("GET /sla/settings", staff(func(w http.ResponseWriter, r *http.Request) {
		settings, err := service.Settings(r.Context())
		if err != nil {
			slaError(w, err)
			return
		}
		writeJSON(w, 200, settings)
	}))
	mux.HandleFunc("PUT /sla/calendar", admin(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		var input sla.Calendar
		if !decodeJSON(w, r, &input) {
			return
		}
		result, err := service.UpdateCalendar(r.Context(), currentUser(r).ID, input)
		if err != nil {
			slaError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"calendar": result})
	}))
	mux.HandleFunc("PUT /sla/policies/{category}/{priority}", admin(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		var input struct {
			ResponseMinutes   int   `json:"response_minutes"`
			ResolutionMinutes int   `json:"resolution_minutes"`
			Version           int64 `json:"version"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		policy := sla.Policy{Category: r.PathValue("category"), Priority: r.PathValue("priority"), ResponseMinutes: input.ResponseMinutes, ResolutionMinutes: input.ResolutionMinutes, Version: input.Version}
		result, err := service.UpdatePolicy(r.Context(), currentUser(r).ID, policy)
		if err != nil {
			slaError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"policy": result})
	}))
	mux.HandleFunc("GET /sla/alerts", staff(func(w http.ResponseWriter, r *http.Request) {
		before, limit, err := parsePagination(r)
		if err != nil {
			slaError(w, sla.ErrInvalid)
			return
		}
		alerts, err := service.Alerts(r.Context(), before, limit)
		if err != nil {
			slaError(w, err)
			return
		}
		cursor := ""
		if len(alerts) > limit {
			alerts = alerts[:limit]
			cursor = strconv.FormatInt(alerts[len(alerts)-1].ID, 10)
		}
		writeJSON(w, 200, map[string]any{"items": alerts, "next_cursor": cursor})
	}))
	// Read-only calculator: controlled timestamps never change production clocks or ticket data.
	mux.HandleFunc("POST /sla/preview", staff(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(w, r) {
			return
		}
		var input struct {
			Calendar          sla.Calendar      `json:"calendar"`
			History           []sla.StatusEvent `json:"history"`
			AsOf              time.Time         `json:"as_of"`
			ResponseMinutes   int64             `json:"response_minutes"`
			ResolutionMinutes int64             `json:"resolution_minutes"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.ResponseMinutes < 1 || input.ResponseMinutes > 43200 || input.ResolutionMinutes < 1 || input.ResolutionMinutes > 43200 || len(input.History) > 1000 {
			slaError(w, sla.ErrInvalid)
			return
		}
		result, err := sla.Evaluate(input.Calendar, input.History, input.AsOf, input.ResponseMinutes*60, input.ResolutionMinutes*60)
		if err != nil {
			slaError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"sla": result})
	}))
}
