package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestRouter() http.Handler {
	return NewRouter(Options{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		AppName: "DinnerOS",
		Version: "test",
	})
}

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	var body HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := HealthResponse{Status: "ok", Service: "DinnerOS", Version: "test"}
	if body != want {
		t.Errorf("body = %+v, want %+v", body, want)
	}
}

func TestUnknownRouteReturnsJSONError(t *testing.T) {
	tests := []struct {
		name, method, path string
		status             int
		code               string
	}{
		{"not found", http.MethodGet, "/api/v1/nope", http.StatusNotFound, "not_found"},
		{"method not allowed", http.MethodPost, "/health", http.StatusMethodNotAllowed, "method_not_allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newTestRouter().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			var body ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Error.Code != tt.code {
				t.Errorf("error code = %q, want %q", body.Error.Code, tt.code)
			}
		})
	}
}
