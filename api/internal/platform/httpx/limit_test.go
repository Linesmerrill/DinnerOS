package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOverrideBodyLimitReplacesGlobalLimit(t *testing.T) {
	decodeRoute := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		if !DecodeJSON(w, r, &v) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	global := LimitBody(16)
	body := func(n int) string { return `{"a":"` + strings.Repeat("x", n) + `"}` }

	tests := []struct {
		name    string
		handler http.Handler
		body    string
		status  int
	}{
		{"global limit applies without override", global(decodeRoute), body(64), 413},
		{"override raises the limit", global(OverrideBodyLimit(256)(decodeRoute)), body(64), 204},
		{"override enforces its own limit", global(OverrideBodyLimit(256)(decodeRoute)), body(512), 413},
		{"override can lower the limit", global(OverrideBodyLimit(8)(decodeRoute)), body(4), 413},
		{"override without a global limit", OverrideBodyLimit(8)(decodeRoute), body(64), 413},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body)))
			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.status, rec.Body.String())
			}
		})
	}
}
