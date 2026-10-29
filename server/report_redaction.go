package server

import (
	"net/http"
	"strconv"
)

func exportRedaction(w http.ResponseWriter, r *http.Request) (bool, bool) {
	value := r.URL.Query().Get("redact")
	if value == "" {
		return false, true
	}
	redacted, err := strconv.ParseBool(value)
	if err != nil {
		writeErr(w, 400, "redact must be a boolean")
		return false, false
	}
	return redacted, true
}
