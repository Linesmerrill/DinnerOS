// Package httpx holds the HTTP conventions every handler shares: JSON
// responses, the error envelope, and strict request decoding. Domain packages
// import it; it imports no domain code.
package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/requestid"
)

// ErrorResponse is the envelope for every non-2xx JSON response.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes an API error. Code is a stable, machine-readable
// identifier; Message is human-readable and may change.
type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the JSON error envelope, including the request ID so a
// client-reported error can be matched to server logs.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteJSON(w, status, ErrorResponse{Error: ErrorDetail{
		Code:      code,
		Message:   message,
		RequestID: requestid.From(r.Context()),
	}})
}
