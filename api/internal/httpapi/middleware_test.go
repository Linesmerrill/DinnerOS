package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/requestid"
)

func TestRequestID(t *testing.T) {
	generated := regexp.MustCompile(`^[0-9a-f]{24}$`)

	tests := []struct {
		name     string
		incoming string
		wantSame bool
	}{
		{name: "generated when absent"},
		{name: "valid incoming preserved", incoming: "3f1c2b8e-9d4a-4c55-8a51-1e2f3a4b5c6d", wantSame: true},
		{name: "invalid incoming replaced", incoming: "bad id\nwith newline"},
		{name: "too short replaced", incoming: "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, Options{})
			var seen string
			srv.mux.Get("/echo", func(w http.ResponseWriter, r *http.Request) {
				seen = requestid.From(r.Context())
			})

			req := httptest.NewRequest(http.MethodGet, "/echo", nil)
			if tt.incoming != "" {
				req.Header.Set(RequestIDHeader, tt.incoming)
			}
			rec := srv.do(req)

			header := rec.Header().Get(RequestIDHeader)
			if header == "" || header != seen {
				t.Fatalf("header = %q, context = %q; want equal and non-empty", header, seen)
			}
			if tt.wantSame && header != tt.incoming {
				t.Errorf("header = %q, want incoming %q", header, tt.incoming)
			}
			if !tt.wantSame && !generated.MatchString(header) {
				t.Errorf("header = %q, want generated 24-char hex ID", header)
			}
		})
	}
}

func TestRecovererReturnsJSON500AndLogs(t *testing.T) {
	srv := newTestServer(t, Options{})
	srv.mux.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("kaboom") })

	rec := srv.do(httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := decode[httpx.ErrorResponse](t, rec)
	if body.Error.Code != "internal" || strings.Contains(body.Error.Message, "kaboom") {
		t.Errorf("error = %+v, want generic internal error", body.Error)
	}
	logs := srv.logs.String()
	if !strings.Contains(logs, "panic recovered") || !strings.Contains(logs, "kaboom") {
		t.Errorf("panic not logged: %s", logs)
	}
	if !strings.Contains(logs, `"status":500`) {
		t.Errorf("request log missing status 500: %s", logs)
	}
}

func TestRequestLoggerLogsRoutePatternNotRawPath(t *testing.T) {
	srv := newTestServer(t, Options{})
	srv.mux.Get("/invitations/{token}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	rec := srv.do(httptest.NewRequest(http.MethodGet, "/invitations/super-secret-token?code=also-secret", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	logs := srv.logs.String()
	if strings.Contains(logs, "super-secret-token") || strings.Contains(logs, "also-secret") {
		t.Fatalf("logs leak path or query: %s", logs)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs)), &entry); err != nil {
		t.Fatalf("expected a single JSON log line, got %q: %v", logs, err)
	}
	checks := map[string]any{
		"msg":       "http request",
		"method":    "GET",
		"route":     "/invitations/{token}",
		"status":    float64(204),
		"requestId": rec.Header().Get(RequestIDHeader),
	}
	for key, want := range checks {
		if entry[key] != want {
			t.Errorf("log %s = %v, want %v", key, entry[key], want)
		}
	}
	if _, ok := entry["latencyMs"]; !ok {
		t.Error("log missing latencyMs")
	}
}

func TestMaxBodyBytesEnforced(t *testing.T) {
	srv := newTestServer(t, Options{MaxBodyBytes: 16})
	srv.mux.Post("/echo", func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		if !httpx.DecodeJSON(w, r, &v) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	small := srv.do(httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":1}`)))
	if small.Code != http.StatusNoContent {
		t.Errorf("small body status = %d, want 204", small.Code)
	}

	large := srv.do(httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":"`+strings.Repeat("x", 64)+`"}`)))
	if large.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("large body status = %d, want 413", large.Code)
	}
}
