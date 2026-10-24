package httpapi

import (
	"errors"
	"net/http"
	"strconv"
)

var (
	errPageLimit  = errors.New("invalid page limit")
	errPageCursor = errors.New("invalid page cursor")
)

func parsePagination(r *http.Request) (before int64, limit int, err error) {
	limit = 20
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err != nil || limit < 1 || limit > 100 {
		return 0, 0, errPageLimit
	}
	if value := r.URL.Query().Get("before"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before < 1 {
			return 0, 0, errPageCursor
		}
	}
	return before, limit, nil
}

func pageParams(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	before, limit, err := parsePagination(r)
	if err != nil {
		message := "Sayfa boyutu 1–100 arasında olmalı."
		if errors.Is(err, errPageCursor) {
			message = "Sayfa imleci geçersiz."
		}
		writeError(w, 400, "INVALID_PAGINATION", message)
		return 0, 0, false
	}
	return before, limit, true
}
