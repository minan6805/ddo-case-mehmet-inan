package httpapi

import (
	"context"
	"errors"
	"net/http"

	"destekmasa/internal/auth"
	"destekmasa/internal/reports"
)

type ReportService interface {
	Overview(context.Context, auth.User, reports.Filter) (reports.Report, error)
}

func registerReports(mux *http.ServeMux, authentication Authenticator, service ReportService) {
	mux.HandleFunc("GET /reports", requireRole(authentication, []string{"team_lead", "admin"}, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		result, err := service.Overview(r.Context(), currentUser(r), reports.Filter{From: q.Get("from"), To: q.Get("to"), Category: q.Get("category")})
		if err != nil {
			switch {
			case errors.Is(err, reports.ErrInvalid):
				writeError(w, 400, "INVALID_REPORT_FILTER", "Geçerli başlangıç/bitiş tarihleri ve kategori seçin. Tarih aralığı en çok 366 gün olabilir.")
			case errors.Is(err, reports.ErrForbidden):
				writeError(w, 403, "FORBIDDEN", "Raporları yalnız lider ve yönetici görebilir.")
			case errors.Is(err, reports.ErrLimit):
				writeError(w, 409, "REPORT_LIMIT", "Rapor en fazla 500 uzmanı destekler.")
			default:
				writeError(w, 503, "REPORT_UNAVAILABLE", "Rapor şu an hazırlanamadı.")
			}
			return
		}
		writeJSON(w, 200, result)
	}))
}
