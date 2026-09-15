package invitations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

func testEmail() HouseholdInvitationEmail {
	return HouseholdInvitationEmail{
		To:            "cat@example.com",
		HouseholdName: "The Lines",
		InviterName:   "Ada",
		Role:          households.RoleMember,
		Code:          "ABCDE-FGHJK",
		AcceptURL:     "dinneros://invite?token=tok_abc-123",
		ExpiresAt:     time.Date(2026, 9, 21, 18, 30, 0, 0, time.UTC),
	}
}

func TestRenderHouseholdInvitation(t *testing.T) {
	msg, err := RenderHouseholdInvitation("Supper Club", testEmail())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Ada invited you to The Lines on Supper Club" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	for _, want := range []string{"ABCDE-FGHJK", `href="dinneros://invite?token=tok_abc-123"`, "Open in the app", "Supper Club", "September 21, 2026", "as a member"} {
		if !strings.Contains(msg.HTML, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	for _, want := range []string{"ABCDE-FGHJK", "dinneros://invite?token=tok_abc-123", "Supper Club", "September 21, 2026"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("Text missing %q", want)
		}
	}
	if strings.Contains(msg.HTML+msg.Text+msg.Subject, "DinnerOS") {
		t.Error("email copy hardcodes DinnerOS instead of using the app name")
	}

	anonymous := testEmail()
	anonymous.InviterName = ""
	anonymous.Role = households.RoleAdmin
	msg, err = RenderHouseholdInvitation("Supper Club", anonymous)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "You're invited to The Lines on Supper Club" || !strings.Contains(msg.Text, "as an admin") {
		t.Errorf("anonymous inviter: subject %q text %q", msg.Subject, msg.Text)
	}
}

func TestRenderHouseholdInvitationEscapesHTML(t *testing.T) {
	e := testEmail()
	e.HouseholdName = `<script>alert("x")</script> & Co`
	e.InviterName = "Eve\"><img src=x onerror=alert(1)>\nBcc: victim@example.com"
	e.AcceptURL = `https://example.com/invite?token=a"><script>alert(2)</script>`
	msg, err := RenderHouseholdInvitation(`App <b>`, e)
	if err != nil {
		t.Fatal(err)
	}
	// The template's own markup legitimately contains `"><`, so check for the
	// injected tags themselves.
	for _, bad := range []string{"<script>", "<img", "<b>", "onerror=alert(1)>"} {
		if strings.Contains(msg.HTML, bad) {
			t.Errorf("HTML contains unescaped %q:\n%s", bad, msg.HTML)
		}
	}
	if !strings.Contains(msg.HTML, "&lt;script&gt;") || !strings.Contains(msg.HTML, "&amp; Co") {
		t.Errorf("HTML does not contain escaped household name:\n%s", msg.HTML)
	}
	if strings.ContainsAny(msg.Subject, "\r\n") {
		t.Errorf("Subject contains a line break: %q", msg.Subject)
	}
}

func TestRenderHouseholdInvitationRejectsUnsafeLinks(t *testing.T) {
	for _, link := range []string{"javascript:alert(1)//?token=", "data:text/html,hi", "/relative?token=x", ""} {
		e := testEmail()
		e.AcceptURL = link
		if _, err := RenderHouseholdInvitation("App", e); err == nil {
			t.Errorf("RenderHouseholdInvitation(%q) error = nil", link)
		}
	}
}

func TestResendProviderSends(t *testing.T) {
	const apiKey = "re_live_secret_key_123"
	var got struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		HTML    string   `json:"html"`
		Text    string   `json:"text"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emails" {
			t.Errorf("request = %s %s, want POST /emails", r.Method, r.URL.Path)
		}
		if h := r.Header.Get("Authorization"); h != "Bearer "+apiKey {
			t.Errorf("Authorization = %q", h)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)
	}))
	defer server.Close()

	p := NewResendProvider(ResendOptions{
		APIKey: apiKey, From: "Supper <invites@example.com>", AppName: "Supper",
		BaseURL: server.URL + "/", HTTPClient: server.Client(),
	})
	if err := p.SendHouseholdInvitation(context.Background(), testEmail()); err != nil {
		t.Fatalf("SendHouseholdInvitation() error = %v", err)
	}
	if got.From != "Supper <invites@example.com>" || len(got.To) != 1 || got.To[0] != "cat@example.com" ||
		!strings.Contains(got.Subject, "The Lines") || !strings.Contains(got.HTML, "ABCDE-FGHJK") || !strings.Contains(got.Text, "ABCDE-FGHJK") {
		t.Errorf("request body = %+v", got)
	}
}

func TestResendProviderErrorsDoNotLeakKey(t *testing.T) {
	const apiKey = "re_live_secret_key_123"
	tests := []struct {
		name       string
		status     int
		body       string
		wantInErr  string
		clientWait time.Duration
	}{
		{"api error echoing key", 422, `{"name":"validation_error","message":"API key re_live_secret_key_123 cannot send from this domain"}`, "status 422", 0},
		{"non-json error", 500, `<html>oops</html>`, "status 500", 0},
		{"unauthorized", 401, `{"name":"missing_api_key","message":"Missing API key"}`, "missing_api_key", 0},
		{"timeout", 200, `{}`, "resend request failed", 50 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.clientWait > 0 {
					select {
					case <-r.Context().Done():
					case <-time.After(2 * time.Second):
					}
					return
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			client := server.Client()
			if tt.clientWait > 0 {
				client.Timeout = tt.clientWait
			}
			p := NewResendProvider(ResendOptions{APIKey: apiKey, From: "a@example.com", AppName: "App", BaseURL: server.URL, HTTPClient: client})
			err := p.SendHouseholdInvitation(context.Background(), testEmail())
			if err == nil || !strings.Contains(err.Error(), tt.wantInErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantInErr)
			}
			if strings.Contains(err.Error(), apiKey) {
				t.Errorf("error leaks API key: %v", err)
			}
		})
	}
}

func TestResendProviderDefaults(t *testing.T) {
	p := NewResendProvider(ResendOptions{APIKey: "k"})
	if p.opts.BaseURL != ResendBaseURL || p.opts.HTTPClient == nil || p.opts.HTTPClient.Timeout != resendTimeout {
		t.Errorf("defaults = %+v", p.opts)
	}
}

func TestLogEmailProvider(t *testing.T) {
	e := testEmail()
	for _, includeCode := range []bool{true, false} {
		var logs bytes.Buffer
		p := NewLogEmailProvider(slog.New(slog.NewJSONHandler(&logs, nil)), includeCode)
		if err := p.SendHouseholdInvitation(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		out := logs.String()
		if !strings.Contains(out, "invitation email suppressed") || !strings.Contains(out, e.To) || !strings.Contains(out, e.HouseholdName) {
			t.Errorf("includeCode=%v: log missing recipient or household: %s", includeCode, out)
		}
		if strings.Contains(out, "tok_abc-123") || strings.Contains(out, "dinneros://") {
			t.Errorf("includeCode=%v: log leaks token or link: %s", includeCode, out)
		}
		if strings.Contains(out, e.Code) != includeCode {
			t.Errorf("includeCode=%v: code in log = %v: %s", includeCode, !includeCode, out)
		}
	}
	if err := NewLogEmailProvider(nil, false).SendHouseholdInvitation(context.Background(), HouseholdInvitationEmail{}); err == nil {
		t.Error("empty recipient error = nil")
	}
}

var errEmailDown = errors.New("resend returned status 503")
