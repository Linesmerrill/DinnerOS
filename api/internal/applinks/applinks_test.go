package applinks

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func serve(t *testing.T, opts Options, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	NewHandler(opts).Mount(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// between returns the text between the first open and the next close.
func between(t *testing.T, s, open, close string) string {
	t.Helper()
	_, after, ok := strings.Cut(s, open)
	if !ok {
		t.Fatalf("%q not found", open)
	}
	inner, _, ok := strings.Cut(after, close)
	if !ok {
		t.Fatalf("%q not found after %q", close, open)
	}
	return inner
}

func hashSource(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func TestInvitePageHeaders(t *testing.T) {
	rec := serve(t, Options{AppName: "DinnerOS"}, "/invite")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	want := map[string]string{
		"Content-Type":           "text/html; charset=utf-8",
		"Cache-Control":          "no-store",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestInvitePageCSPAllowsOnlyItsOwnInlineCode(t *testing.T) {
	rec := serve(t, Options{}, "/invite")
	csp := rec.Header().Get("Content-Security-Policy")
	body := rec.Body.String()

	directives := map[string]string{}
	for _, d := range strings.Split(csp, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(d), " ")
		directives[name] = value
	}
	script := between(t, body, "<script>", "</script>")
	style := between(t, body, "<style>", "</style>")
	want := map[string]string{
		"default-src":     "'none'",
		"script-src":      hashSource(script),
		"style-src":       hashSource(style),
		"connect-src":     "'self'",
		"base-uri":        "'none'",
		"form-action":     "'none'",
		"frame-ancestors": "'none'",
	}
	for name, value := range want {
		if directives[name] != value {
			t.Errorf("CSP %s = %q, want %q (CSP %q)", name, directives[name], value, csp)
		}
	}
	if strings.Contains(csp, "unsafe") {
		t.Errorf("CSP allows unsafe sources: %q", csp)
	}
	// Inline style attributes and event handlers would be blocked by the CSP.
	for _, forbidden := range []string{" style=", " onclick=", " onload="} {
		if strings.Contains(body, forbidden) {
			t.Errorf("page uses %q, which the CSP blocks", forbidden)
		}
	}
	if strings.Count(body, "<script") != 1 || strings.Count(body, "<style") != 1 {
		t.Error("page must have exactly one inline script and style, both hashed")
	}
}

func TestInvitePageContent(t *testing.T) {
	body := serve(t, Options{AppName: "Supper Club", AppURLScheme: "supper"}, "/invite").Body.String()
	for _, want := range []string{
		`data-app-name="Supper Club"`,
		`data-scheme="supper"`,
		"Open in Supper Club",
		`href="` + TestFlightURL + `"`,
		"Supper Club is in private testing",
		"Accept the TestFlight invite email",
		"Or enter your invite code",
		"invite code from your invitation email",
		`window.location.hash`,
		`"/api/v1/invitations/preview"`,
		`"://invite?token=" + encodeURIComponent(token)`,
		"This invitation is no longer valid",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, "DinnerOS") {
		t.Error("page hardcodes DinnerOS instead of using the app name")
	}
	if strings.Contains(body, "innerHTML") {
		t.Error("script must write text, not HTML")
	}
}

func TestInvitePageEscapesAppNameAndRejectsBadSchemes(t *testing.T) {
	body := serve(t, Options{AppName: `Pie <script>alert(1)</script> "Co"`, AppURLScheme: "javascript:alert(1)//"}, "/invite").Body.String()
	if strings.Contains(body, "<script>alert(1)") {
		t.Error("app name is not escaped")
	}
	if !strings.Contains(body, `data-scheme="dinneros"`) {
		t.Errorf("invalid scheme not replaced by the default: %s", between(t, body, "<body", ">"))
	}
}

func TestAppSiteAssociation(t *testing.T) {
	rec := serve(t, Options{AppleTeamID: "ABCDE12345", AppleBundleID: "com.example.dinner"}, AASAPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 without a redirect", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var file struct {
		AppLinks struct {
			Apps    []string `json:"apps"`
			Details []struct {
				AppIDs     []string            `json:"appIDs"`
				Components []map[string]string `json:"components"`
			} `json:"details"`
		} `json:"applinks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &file); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	details := file.AppLinks.Details
	if len(details) != 1 || len(details[0].AppIDs) != 1 || details[0].AppIDs[0] != "ABCDE12345.com.example.dinner" {
		t.Fatalf("details = %+v", details)
	}
	if len(details[0].Components) != 1 || details[0].Components[0]["/"] != "/invite" {
		t.Errorf("components = %+v, want one matching /invite", details[0].Components)
	}
	if strings.Contains(rec.Body.String(), "paths") {
		t.Errorf("uses the legacy paths format: %s", rec.Body.String())
	}
}

func TestAppSiteAssociationRequiresAppID(t *testing.T) {
	for _, opts := range []Options{{}, {AppleBundleID: "com.example.dinner"}, {AppleTeamID: "ABCDE12345"}} {
		if rec := serve(t, opts, AASAPath); rec.Code != http.StatusNotFound {
			t.Errorf("%+v: status = %d, want 404", opts, rec.Code)
		}
	}
	// The landing page works either way.
	if rec := serve(t, Options{}, InvitePath); rec.Code != http.StatusOK {
		t.Errorf("invite page status = %d", rec.Code)
	}
}
