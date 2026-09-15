// Package httpapi wires the HTTP router, cross-cutting middleware, and the
// JSON response conventions shared by every domain handler.
package httpapi

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse is the envelope for every non-2xx JSON response.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes an API error. Code is a stable, machine-readable
// identifier; Message is human-readable and may change.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes a JSON error envelope.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorResponse{Error: ErrorDetail{Code: code, Message: message}})
}
