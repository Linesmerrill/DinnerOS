package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/logging"
)

type testServer struct {
	mux  *chi.Mux
	logs *bytes.Buffer
}

func newTestServer(t *testing.T, opts Options) testServer {
	t.Helper()
	var logs bytes.Buffer
	opts.Logger = logging.New(&logs, slog.LevelDebug, "json")
	if opts.AppName == "" {
		opts.AppName = "DinnerOS"
	}
	if opts.Version == "" {
		opts.Version = "test"
	}
	return testServer{mux: newMux(opts), logs: &logs}
}

func (s testServer) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t, Options{})
	rec := srv.do(httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	want := HealthResponse{Status: "ok", Service: "DinnerOS", Version: "test"}
	if got := decode[HealthResponse](t, rec); got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}
}

func TestReady(t *testing.T) {
	ok := ReadinessCheck{Name: "mongodb", Check: func(context.Context) error { return nil }}
	failing := ReadinessCheck{Name: "mongodb", Check: func(context.Context) error {
		return errors.New("server selection timeout: secret-host-detail")
	}}

	t.Run("all checks pass", func(t *testing.T) {
		srv := newTestServer(t, Options{ReadinessChecks: []ReadinessCheck{ok}})
		rec := srv.do(httptest.NewRequest(http.MethodGet, "/ready", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := decode[ReadyResponse](t, rec)
		if body.Status != "ready" || body.Checks["mongodb"] != "ok" {
			t.Errorf("body = %+v", body)
		}
	})

	t.Run("failing check returns 503 without details", func(t *testing.T) {
		srv := newTestServer(t, Options{ReadinessChecks: []ReadinessCheck{failing}})
		rec := srv.do(httptest.NewRequest(http.MethodGet, "/ready", nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		raw := rec.Body.String()
		if strings.Contains(raw, "secret-host-detail") {
			t.Errorf("response leaks failure details: %s", raw)
		}
		body := decode[ReadyResponse](t, rec)
		if body.Status != "unavailable" || body.Checks["mongodb"] != "unavailable" {
			t.Errorf("body = %+v", body)
		}
		if !strings.Contains(srv.logs.String(), "readiness check failed") {
			t.Errorf("failure not logged: %s", srv.logs.String())
		}
	})
}

func TestAPIRoutesMountedUnderV1WithMiddleware(t *testing.T) {
	srv := newTestServer(t, Options{APIRoutes: func(r chi.Router) {
		r.Get("/widgets/{id}", func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteJSON(w, http.StatusOK, map[string]string{"id": chi.URLParam(r, "id")})
		})
	}})

	rec := srv.do(httptest.NewRequest(http.MethodGet, "/api/v1/widgets/42", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"42"`) {
		t.Fatalf("status = %d body = %s, want 200 with id", rec.Code, rec.Body.String())
	}
	if rec.Header().Get(RequestIDHeader) == "" {
		t.Error("global middleware not applied to API routes (missing request ID)")
	}
	if !strings.Contains(srv.logs.String(), `"route":"/api/v1/widgets/{id}"`) {
		t.Errorf("request log missing full route pattern: %s", srv.logs.String())
	}

	notFound := srv.do(httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	if notFound.Code != http.StatusNotFound || decode[httpx.ErrorResponse](t, notFound).Error.Code != "not_found" {
		t.Errorf("unknown API route status = %d, want JSON 404", notFound.Code)
	}
}

func TestUnknownRoutesReturnJSONErrors(t *testing.T) {
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
			srv := newTestServer(t, Options{})
			rec := srv.do(httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			body := decode[httpx.ErrorResponse](t, rec)
			if body.Error.Code != tt.code {
				t.Errorf("error code = %q, want %q", body.Error.Code, tt.code)
			}
			if body.Error.RequestID == "" || body.Error.RequestID != rec.Header().Get(RequestIDHeader) {
				t.Errorf("requestId = %q, header = %q", body.Error.RequestID, rec.Header().Get(RequestIDHeader))
			}
		})
	}
}
