package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type Pinger interface{ Ping(context.Context) error }

type APIError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body APIError
	body.Error.Code, body.Error.Message = code, message
	writeJSON(w, status, body)
}

func NewRouter(db Pinger) http.Handler {
	return newRouter(db, nil, AuthOptions{}, nil, nil)
}

func NewAuthenticatedRouter(db Pinger, service Authenticator, options AuthOptions) http.Handler {
	return newRouter(db, service, options, nil, nil)
}

func NewApplicationRouter(db Pinger, service Authenticator, options AuthOptions, ticketService TicketService) http.Handler {
	return newRouter(db, service, options, ticketService, nil)
}

func NewSLAApplicationRouter(db Pinger, service Authenticator, options AuthOptions, ticketService TicketService, engine SLAService) http.Handler {
	return newRouter(db, service, options, ticketService, engine)
}

type Services struct {
	Database       Pinger
	Authentication Authenticator
	AuthOptions    AuthOptions
	Tickets        TicketService
	SLA            SLAService
	Assignment     AssignmentService
	Comments       CommentService
	Email          EmailService
	Threads        ThreadService
	Assets         AssetService
	Simulator      SimulatorService
	Reports        ReportService
	Knowledge      KnowledgeService
	Changes        ChangeService
}

func newRouter(db Pinger, service Authenticator, options AuthOptions, ticketService TicketService, engine SLAService) http.Handler {
	return NewServicesRouter(Services{Database: db, Authentication: service, AuthOptions: options, Tickets: ticketService, SLA: engine})
}
func NewServicesRouter(services Services) http.Handler {
	db, service, options, ticketService, engine := services.Database, services.Authentication, services.AuthOptions, services.Tickets, services.SLA
	mux := http.NewServeMux()
	registerOpenAPI(mux)
	if service != nil {
		registerAuth(mux, service, options)
		if services.Simulator != nil {
			registerSimulator(mux, service, services.Simulator)
		}
		if services.Reports != nil {
			registerReports(mux, service, services.Reports)
		}
		if services.Knowledge != nil {
			registerKnowledge(mux, service, services.Knowledge)
		}
		if services.Changes != nil {
			registerChanges(mux, service, services.Changes)
		}
		if services.Assets != nil {
			registerAssets(mux, service, services.Assets)
		}
		if services.Threads != nil {
			registerThreads(mux, service, services.Threads)
		}
		if services.Email != nil {
			registerEmail(mux, service, services.Email)
		}
		if services.Comments != nil {
			registerComments(mux, service, services.Comments)
		}
		if services.Assignment != nil {
			registerAssignment(mux, service, services.Assignment)
		}
		if engine != nil {
			registerSLA(mux, service, engine)
		}
		if ticketService != nil {
			registerTickets(mux, service, ticketService)
		}
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "Veritabanına erişilemiyor.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Uç nokta bulunamadı.")
	})
	return mux
}
