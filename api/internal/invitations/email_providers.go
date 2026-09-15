package invitations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ResendBaseURL is the Resend API origin.
const ResendBaseURL = "https://api.resend.com"

const (
	resendTimeout      = 10 * time.Second
	maxResendErrorBody = 4 << 10
)

// ResendOptions configures a ResendProvider.
type ResendOptions struct {
	// APIKey is the Resend API key. It is sent only in the Authorization
	// header and never logged or included in errors.
	APIKey string
	// From is the sender, e.g. "DinnerOS <invites@example.com>".
	From string
	// AppName is used in email copy.
	AppName string
	// BaseURL overrides ResendBaseURL (tests).
	BaseURL string
	// HTTPClient overrides the default client, which times out after 10s.
	HTTPClient *http.Client
}

// ResendProvider sends email through the Resend HTTP API.
type ResendProvider struct {
	opts ResendOptions
}

var _ EmailProvider = (*ResendProvider)(nil)

// NewResendProvider returns a ResendProvider.
func NewResendProvider(opts ResendOptions) *ResendProvider {
	if opts.BaseURL == "" {
		opts.BaseURL = ResendBaseURL
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: resendTimeout}
	}
	return &ResendProvider{opts: opts}
}

type resendEmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

// SendHouseholdInvitation implements EmailProvider.
func (p *ResendProvider) SendHouseholdInvitation(ctx context.Context, e HouseholdInvitationEmail) error {
	msg, err := RenderHouseholdInvitation(p.opts.AppName, e)
	if err != nil {
		return err
	}
	body, err := json.Marshal(resendEmailRequest{
		From: p.opts.From, To: []string{e.To}, Subject: msg.Subject, HTML: msg.HTML, Text: msg.Text,
	})
	if err != nil {
		return fmt.Errorf("invitations: encode resend request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.opts.BaseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invitations: build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.opts.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("invitations: resend request failed: %s", p.redact(err.Error()))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResendErrorBody))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	detail := ""
	var apiErr struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	if json.Unmarshal(respBody, &apiErr) == nil && (apiErr.Name != "" || apiErr.Message != "") {
		detail = strings.TrimSpace(apiErr.Name + ": " + apiErr.Message)
	}
	if detail == "" {
		return fmt.Errorf("invitations: resend returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("invitations: resend returned status %d: %s", resp.StatusCode, p.redact(detail))
}

// redact removes the API key from s in case an upstream message echoes it.
func (p *ResendProvider) redact(s string) string {
	if p.opts.APIKey == "" {
		return s
	}
	return strings.ReplaceAll(s, p.opts.APIKey, "[REDACTED]")
}

// LogEmailProvider logs that an email would have been sent instead of sending
// it. It is for development and tests only; configuration refuses it in
// production.
type LogEmailProvider struct {
	logger      *slog.Logger
	includeCode bool
}

var _ EmailProvider = (*LogEmailProvider)(nil)

// NewLogEmailProvider returns a LogEmailProvider. includeCode adds the invite
// code to the log line so a developer can accept the invitation; set it only
// in development. The token and accept link are never logged.
func NewLogEmailProvider(logger *slog.Logger, includeCode bool) *LogEmailProvider {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &LogEmailProvider{logger: logger, includeCode: includeCode}
}

// SendHouseholdInvitation implements EmailProvider.
func (p *LogEmailProvider) SendHouseholdInvitation(ctx context.Context, e HouseholdInvitationEmail) error {
	if e.To == "" {
		return errors.New("invitations: recipient is required")
	}
	attrs := []any{
		"to", e.To,
		"household", e.HouseholdName,
		"role", string(e.Role),
		"expiresAt", e.ExpiresAt.UTC(),
	}
	if p.includeCode {
		attrs = append(attrs, "code", e.Code)
	}
	p.logger.InfoContext(ctx, "invitation email suppressed (EMAIL_PROVIDER=log)", attrs...)
	return nil
}
