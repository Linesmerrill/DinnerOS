package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var errTrailingData = errors.New("trailing data after JSON object")

// DecodeJSON strictly decodes a single JSON value from the request body into
// dst. Unknown fields and trailing data are rejected. On failure it writes an
// appropriate error response and returns false; callers just return.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	err := dec.Decode(dst)
	if err == nil && dec.More() {
		err = errTrailingData
	}
	if err == nil {
		return true
	}

	var (
		maxBytesErr *http.MaxBytesError
		typeErr     *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &maxBytesErr):
		WriteError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("request body must not exceed %d bytes", maxBytesErr.Limit))
	case errors.Is(err, io.EOF):
		WriteError(w, r, http.StatusBadRequest, "invalid_request", "request body must not be empty")
	case errors.Is(err, errTrailingData):
		WriteError(w, r, http.StatusBadRequest, "invalid_request", "request body must contain a single JSON value")
	case errors.As(err, &typeErr):
		WriteError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("field %q has the wrong type", typeErr.Field))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		WriteError(w, r, http.StatusBadRequest, "invalid_request", strings.TrimPrefix(err.Error(), "json: "))
	default:
		WriteError(w, r, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
	}
	return false
}
