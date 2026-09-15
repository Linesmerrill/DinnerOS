package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/requestid"
)

func TestWriteErrorIncludesRequestID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(requestid.With(req.Context(), "req-abcdef12"))
	rec := httptest.NewRecorder()

	WriteError(rec, req, http.StatusConflict, "conflict", "already exists")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := ErrorDetail{Code: "conflict", Message: "already exists", RequestID: "req-abcdef12"}
	if body.Error != want {
		t.Errorf("error = %+v, want %+v", body.Error, want)
	}
}

type payload struct {
	Name     string `json:"name"`
	Servings int    `json:"servings"`
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		limit       int64
		wantOK      bool
		wantStatus  int
		wantMessage string
	}{
		{name: "valid", body: `{"name":"Tacos","servings":2}`, wantOK: true},
		{name: "valid with trailing whitespace", body: "{\"name\":\"Tacos\"}\n  ", wantOK: true},
		{name: "empty", body: "", wantStatus: 400, wantMessage: "must not be empty"},
		{name: "malformed", body: `{"name":`, wantStatus: 400, wantMessage: "valid JSON"},
		{name: "unknown field", body: `{"name":"Tacos","isAdmin":true}`, wantStatus: 400, wantMessage: `unknown field "isAdmin"`},
		{name: "wrong type", body: `{"servings":"two"}`, wantStatus: 400, wantMessage: `"servings"`},
		{name: "trailing data", body: `{"name":"a"} {"name":"b"}`, wantStatus: 400, wantMessage: "single JSON value"},
		{name: "too large", body: `{"name":"` + strings.Repeat("x", 100) + `"}`, limit: 32, wantStatus: 413, wantMessage: "32 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			if tt.limit > 0 {
				req.Body = http.MaxBytesReader(rec, req.Body, tt.limit)
			}

			var dst payload
			ok := DecodeJSON(rec, req, &dst)

			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (response %s)", ok, tt.wantOK, rec.Body.String())
			}
			if tt.wantOK {
				return
			}
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if !strings.Contains(body.Error.Message, tt.wantMessage) {
				t.Errorf("message = %q, want it to contain %q", body.Error.Message, tt.wantMessage)
			}
		})
	}
}
