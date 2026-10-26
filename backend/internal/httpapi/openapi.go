package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.json
var openAPIDocument []byte

func registerOpenAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openAPIDocument)
	})
}
