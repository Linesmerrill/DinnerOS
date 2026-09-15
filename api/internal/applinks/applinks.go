// Package applinks serves the unversioned, browser-facing routes that make
// invitation links work without the app installed:
//
//   - GET /invite, a small landing page. Invitation emails link to
//     /invite#token=<token>. The token stays in the URL fragment, which
//     browsers never send, so it can't reach the server or Heroku's router
//     logs. The page's script reads it, links to the app's custom URL scheme,
//     and previews the invitation through POST /api/v1/invitations/preview.
//   - GET /.well-known/apple-app-site-association, which lets the iOS app
//     claim /invite as a universal link, so the link opens the app directly
//     when it's installed.
package applinks

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Paths served by Handler.
const (
	InvitePath = "/invite"
	AASAPath   = "/.well-known/apple-app-site-association"
)

// TestFlightURL is TestFlight on the App Store. DinnerOS is distributed
// through TestFlight while it's in private testing.
const TestFlightURL = "https://apps.apple.com/app/testflight/id899247664"

const (
	defaultAppName   = "DinnerOS"
	defaultURLScheme = "dinneros"
)

var urlSchemePattern = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

// Options configures a Handler.
type Options struct {
	// AppName is shown on the landing page. Default "DinnerOS".
	AppName string
	// AppURLScheme is the iOS app's custom URL scheme. Default "dinneros".
	AppURLScheme string
	// AppleTeamID and AppleBundleID form the app ID in
	// apple-app-site-association. The file is not served unless both are set.
	AppleTeamID   string
	AppleBundleID string
}

// Handler serves the landing page and apple-app-site-association. Both are
// rendered once, at construction.
type Handler struct {
	page []byte
	csp  string
	// aasa is nil when the app ID isn't configured.
	aasa []byte
}

// NewHandler returns a Handler.
func NewHandler(opts Options) *Handler {
	appName := strings.Join(strings.Fields(opts.AppName), " ")
	if appName == "" {
		appName = defaultAppName
	}
	scheme := strings.ToLower(opts.AppURLScheme)
	if !urlSchemePattern.MatchString(scheme) {
		scheme = defaultURLScheme
	}

	var page bytes.Buffer
	err := invitePage.Execute(&page, map[string]any{
		"AppName":       appName,
		"Scheme":        scheme,
		"TestFlightURL": TestFlightURL,
		// Both are constants in this file, hashed into the CSP below.
		"Style":  template.CSS(inviteStyle), //nolint:gosec // constant
		"Script": template.JS(inviteScript), //nolint:gosec // constant
	})
	if err != nil {
		// The template and its inputs are fixed; this is a programming error.
		panic(fmt.Sprintf("applinks: render invite page: %v", err))
	}

	h := &Handler{page: page.Bytes(), csp: contentSecurityPolicy()}
	if opts.AppleTeamID != "" && opts.AppleBundleID != "" {
		h.aasa = encodeAppSiteAssociation(opts.AppleTeamID + "." + opts.AppleBundleID)
	}
	return h
}

// Mount registers the routes on r, which is expected to be the root router.
func (h *Handler) Mount(r chi.Router) {
	r.Get(InvitePath, h.serveInvitePage)
	if h.aasa != nil {
		r.Get(AASAPath, h.serveAASA)
	}
}

func (h *Handler) serveInvitePage(w http.ResponseWriter, _ *http.Request) {
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Content-Security-Policy", h.csp)
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.page)
}

func (h *Handler) serveAASA(w http.ResponseWriter, _ *http.Request) {
	header := w.Header()
	header.Set("Content-Type", "application/json")
	header.Set("Cache-Control", "public, max-age=3600")
	header.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.aasa)
}

// --- apple-app-site-association ---------------------------------------------

// appSiteAssociation is the modern (iOS 13+) applinks format.
type appSiteAssociation struct {
	AppLinks appLinks `json:"applinks"`
}

type appLinks struct {
	Details []appLinkDetail `json:"details"`
}

type appLinkDetail struct {
	AppIDs     []string           `json:"appIDs"`
	Components []appLinkComponent `json:"components"`
}

type appLinkComponent struct {
	Path    string `json:"/"`
	Comment string `json:"comment,omitempty"`
}

func encodeAppSiteAssociation(appID string) []byte {
	b, err := json.Marshal(appSiteAssociation{AppLinks: appLinks{Details: []appLinkDetail{{
		AppIDs: []string{appID},
		Components: []appLinkComponent{{
			Path:    InvitePath,
			Comment: "Household invitation links; the token is in the fragment",
		}},
	}}}})
	if err != nil {
		panic(fmt.Sprintf("applinks: encode apple-app-site-association: %v", err))
	}
	return b
}

// --- Landing page ---------------------------------------------------------------

// contentSecurityPolicy allows only the page's own inline style and script
// (by hash) and fetches to this origin.
func contentSecurityPolicy() string {
	return strings.Join([]string{
		"default-src 'none'",
		"script-src '" + sha256Source(inviteScript) + "'",
		"style-src '" + sha256Source(inviteStyle) + "'",
		"connect-src 'self'",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'none'",
	}, "; ")
}

func sha256Source(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

// The page uses no inline style attributes, which the CSP would block.
var invitePage = template.Must(template.New("invite").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta name="robots" content="noindex">
<title>Join a household on {{.AppName}}</title>
<style>{{.Style}}</style>
</head>
<body data-app-name="{{.AppName}}" data-scheme="{{.Scheme}}">
<main>
<p class="brand">{{.AppName}}</p>
<h1 id="title">You're invited to {{.AppName}}</h1>
<p id="lead" class="lead">Someone invited you to join their household on {{.AppName}}.</p>
<p id="missing" class="notice" hidden>This link is missing its invitation. Open the link in your invitation email again, or enter your invite code in the app.</p>
<a id="open" class="button" href="#" hidden>Open in {{.AppName}}</a>
<div id="help">
<h2>Don't have {{.AppName}} yet?</h2>
<p class="note">{{.AppName}} is in private testing.</p>
<ol>
<li>Install <a href="{{.TestFlightURL}}">TestFlight</a> from the App Store.</li>
<li>Accept the TestFlight invite email to install {{.AppName}}.</li>
<li>Come back to this page and tap <strong>Open in {{.AppName}}</strong>.</li>
</ol>
<h2>Or enter your invite code</h2>
<p class="note">Open {{.AppName}}, choose to join a household, and enter the invite code from your invitation email.</p>
</div>
<noscript><p class="fine">Turn on JavaScript to open this invitation, or enter your invite code in the app.</p></noscript>
</main>
<script>{{.Script}}</script>
</body>
</html>
`))

// inviteStyle matches the invitation email.
const inviteStyle = `
*{box-sizing:border-box}
[hidden]{display:none!important}
body{margin:0;padding:24px 16px;background:#f6f2ee;color:#231f1b;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;line-height:1.5;-webkit-text-size-adjust:100%}
main{max-width:480px;margin:0 auto;padding:32px 24px;background:#fff;border-radius:14px}
a{color:#c4561f}
.brand{margin:0 0 8px;font-size:14px;font-weight:600;color:#c4561f}
h1{margin:0 0 12px;font-size:24px;line-height:1.25}
h2{margin:28px 0 8px;font-size:17px}
p,ol{margin:0 0 12px;font-size:16px}
ol{padding-left:22px}
li{margin-bottom:6px}
.lead{margin-bottom:24px}
.notice{padding:12px 14px;background:#fbeee5;border-radius:10px}
.note{color:#5f5750;font-size:15px}
.fine{margin:24px 0 0;color:#8a8179;font-size:13px}
.button{display:block;margin:0 0 8px;padding:14px 24px;background:#c4561f;color:#fff;border-radius:10px;font-size:17px;font-weight:600;text-align:center;text-decoration:none}
`

// inviteScript reads the token from the fragment, links to the app, and
// previews the invitation. It writes only textContent, never HTML, and never
// sends the token anywhere but the preview request body.
const inviteScript = `
(() => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const appName = document.body.dataset.appName;
  const scheme = document.body.dataset.scheme;
  const token = (new URLSearchParams(window.location.hash.slice(1)).get("token") || "").trim();

  if (!token) {
    $("title").textContent = "Join a household on " + appName;
    $("lead").hidden = true;
    $("missing").hidden = false;
    return;
  }

  const open = $("open");
  open.href = scheme + "://invite?token=" + encodeURIComponent(token);
  open.hidden = false;

  const roleNoun = (role) => (role === "admin" ? "an admin" : role === "member" ? "a member" : role);

  const showInvalid = () => {
    $("title").textContent = "This invitation is no longer valid";
    $("lead").textContent =
      "It may have expired, been revoked, or already been used. Ask the person who invited you to send a new invitation.";
    open.hidden = true;
    $("help").hidden = true;
  };

  const showPreview = (preview) => {
    $("title").textContent = "Join " + preview.householdName + " on " + appName;
    let lead = (preview.inviterName ? preview.inviterName + " invited you" : "You're invited") +
      " to join as " + roleNoun(preview.role) + ".";
    const expires = new Date(preview.expiresAt);
    if (!Number.isNaN(expires.getTime())) {
      lead += " This invitation expires on " +
        expires.toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" }) + ".";
    }
    $("lead").textContent = lead;
  };

  fetch("/api/v1/invitations/preview", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token }),
    cache: "no-store",
    credentials: "omit",
    referrerPolicy: "no-referrer",
  })
    .then((response) => {
      if (response.status === 400 || response.status === 404) {
        showInvalid();
      } else if (response.ok) {
        return response.json().then(showPreview);
      }
      // Rate limited or unavailable: keep the generic page and the Open button.
    })
    .catch(() => {});
})();
`
