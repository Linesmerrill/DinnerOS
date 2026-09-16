package applinks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/httpapi"
)

func TestStaticPages(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []string
	}{
		{path: HomePath, want: []string{"<title>DinnerOS</title>", `href="/privacy"`, "mailto:" + ContactEmail}},
		{path: PrivacyPath, want: []string{
			"Privacy policy", "Sign in with Apple", "MongoDB Atlas", "Heroku", "Resend",
			"Delete Account", "never sells", "mailto:" + ContactEmail, PrivacyEffectiveDate,
		}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := serve(t, Options{AppName: "DinnerOS"}, tc.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", ct)
			}
			body := rec.Body.String()
			for _, s := range tc.want {
				if !strings.Contains(body, s) {
					t.Errorf("page missing %q", s)
				}
			}
			if strings.Contains(body, "<script") {
				t.Error("static page has a script")
			}
			// The page's only inline style is allowed by hash.
			csp := rec.Header().Get("Content-Security-Policy")
			style := between(t, body, "<style>", "</style>")
			if !strings.Contains(csp, "style-src "+hashSource(style)) || !strings.Contains(csp, "default-src 'none'") {
				t.Errorf("CSP %q does not allow the page style only", csp)
			}
		})
	}
}

// TestHomePageDoesNotShadowRoutes mounts the pages on the real router: "/"
// must match only itself.
func TestHomePageDoesNotShadowRoutes(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Options{
		WebRoutes: NewHandler(Options{AppName: "DinnerOS", AppleTeamID: "TEAM", AppleBundleID: "com.example"}).Mount,
		APIRoutes: func(r chi.Router) {
			r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })
		},
	})
	for _, tc := range []struct {
		path   string
		status int
		want   string
	}{
		{path: "/", status: http.StatusOK, want: "<!doctype html>"},
		{path: "/privacy", status: http.StatusOK, want: "Privacy policy"},
		{path: "/invite", status: http.StatusOK, want: "<!doctype html>"},
		{path: "/health", status: http.StatusOK, want: `"status":"ok"`},
		{path: AASAPath, status: http.StatusOK, want: "applinks"},
		{path: "/api/v1/ping", status: http.StatusOK, want: "pong"},
		{path: "/api/v1/nope", status: http.StatusNotFound, want: "not_found"},
		{path: "/nope", status: http.StatusNotFound, want: "not_found"},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("GET %s = %d %.80q; want %d containing %q", tc.path, rec.Code, rec.Body.String(), tc.status, tc.want)
		}
	}
}
