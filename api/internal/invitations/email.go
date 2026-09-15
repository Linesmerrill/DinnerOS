package invitations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// EmailProvider sends transactional email. Implementations: ResendProvider
// (production) and LogEmailProvider (development and tests).
type EmailProvider interface {
	SendHouseholdInvitation(ctx context.Context, email HouseholdInvitationEmail) error
}

// HouseholdInvitationEmail is everything needed to write an invitation email.
type HouseholdInvitationEmail struct {
	To            string
	HouseholdName string
	// InviterName may be empty when the inviter has no display name.
	InviterName string
	Role        households.Role
	// Code is the display form (XXXXX-XXXXX).
	Code string
	// AcceptURL opens the app's accept flow. It contains the token.
	AcceptURL string
	ExpiresAt time.Time
}

// RenderedEmail is a ready-to-send message.
type RenderedEmail struct {
	Subject string
	HTML    string
	Text    string
}

// SafeLinkURL reports whether raw is acceptable as a link in an email: an
// absolute URL whose scheme cannot run script.
func SafeLinkURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "javascript", "vbscript", "data", "file":
		return false
	}
	return true
}

var invitationHTML = template.Must(template.New("invitation").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Subject}}</title></head>
<body style="margin:0;padding:24px 12px;background:#f6f2ee;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;color:#231f1b;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;background:#ffffff;border-radius:14px;">
<tr><td style="padding:32px 28px;">
<p style="margin:0 0 8px;font-size:14px;font-weight:600;color:#c4561f;">{{.AppName}}</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;">Join {{.HouseholdName}}</h1>
<p style="margin:0 0 24px;font-size:16px;line-height:1.5;">{{.Intro}}</p>
<p style="margin:0 0 8px;font-size:15px;">Your invite code:</p>
<p style="margin:0 0 24px;padding:16px;background:#fbeee5;border-radius:10px;text-align:center;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:28px;font-weight:700;letter-spacing:3px;">{{.Code}}</p>
<p style="margin:0 0 24px;text-align:center;"><a href="{{.AcceptURL}}" style="display:inline-block;padding:12px 24px;background:#c4561f;color:#ffffff;border-radius:10px;font-size:16px;font-weight:600;text-decoration:none;">Open in the app</a></p>
<p style="margin:0 0 8px;font-size:14px;line-height:1.5;color:#5f5750;">If the button doesn't work, open {{.AppName}}, choose to join a household, and enter the code.</p>
<p style="margin:0;font-size:13px;line-height:1.5;color:#8a8179;">This invitation expires on {{.Expires}}. If you weren't expecting it, you can ignore this email.</p>
</td></tr>
</table>
</td></tr></table>
</body>
</html>
`))

// RenderHouseholdInvitation builds the subject, HTML, and plain-text bodies.
// Every interpolated value is escaped in the HTML body.
func RenderHouseholdInvitation(appName string, e HouseholdInvitationEmail) (RenderedEmail, error) {
	if !SafeLinkURL(e.AcceptURL) {
		return RenderedEmail{}, errors.New("invitations: accept URL must be an absolute URL with a safe scheme")
	}
	appName = oneLine(appName)
	household := oneLine(e.HouseholdName)
	inviter := oneLine(e.InviterName)

	var subject, intro string
	if inviter != "" {
		subject = fmt.Sprintf("%s invited you to %s on %s", inviter, household, appName)
		intro = fmt.Sprintf("%s invited you to join %s on %s as %s.", inviter, household, appName, roleNoun(e.Role))
	} else {
		subject = fmt.Sprintf("You're invited to %s on %s", household, appName)
		intro = fmt.Sprintf("You've been invited to join %s on %s as %s.", household, appName, roleNoun(e.Role))
	}
	expires := e.ExpiresAt.UTC().Format("January 2, 2006")

	var html bytes.Buffer
	err := invitationHTML.Execute(&html, map[string]any{
		"Subject":       subject,
		"AppName":       appName,
		"HouseholdName": household,
		"Intro":         intro,
		"Code":          e.Code,
		// template.URL keeps custom schemes such as dinneros:// (which
		// html/template would otherwise replace); SafeLinkURL vetted it above.
		// The value is still HTML-attribute escaped.
		"AcceptURL": template.URL(e.AcceptURL), //nolint:gosec // validated by SafeLinkURL
		"Expires":   expires,
	})
	if err != nil {
		return RenderedEmail{}, fmt.Errorf("invitations: render email: %w", err)
	}

	text := fmt.Sprintf("%s\n\nYour invite code: %s\n\nOpen in the app: %s\n\nOr open %s, choose to join a household, and enter the code.\n\nThis invitation expires on %s. If you weren't expecting it, you can ignore this email.\n",
		intro, e.Code, e.AcceptURL, appName, expires)

	return RenderedEmail{Subject: subject, HTML: html.String(), Text: text}, nil
}

func roleNoun(r households.Role) string {
	switch r {
	case households.RoleAdmin:
		return "an admin"
	case households.RoleMember:
		return "a member"
	default:
		return string(r)
	}
}

// oneLine collapses whitespace (including newlines) so values are safe in a
// subject line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
