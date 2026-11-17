// Package siwa keeps a Sign in with Apple refresh token for each Apple user
// and revokes it when the account is deleted, as Apple requires of apps that
// offer Sign in with Apple (App Review guideline 5.1.1(v)).
//
// The app sends the authorization code Apple gives it at sign-in. The server
// exchanges it at Apple's token endpoint for a refresh token, stores that
// encrypted, and on account deletion posts it to Apple's revoke endpoint. A
// user who signed in before codes were sent confirms with Apple again at
// deletion, and the code from that is exchanged and revoked the same way.
//
// Nothing here blocks sign-in or deletion: every Apple failure is logged
// (status and Apple's error code only) and dropped. Codes, tokens, and the
// client secret are never logged.
package siwa

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DefaultBaseURL is Apple's Sign in with Apple server. The client secret's
// audience is always this, whatever base URL requests go to.
const DefaultBaseURL = "https://appleid.apple.com"

// clientSecretAudience is the aud claim Apple requires in a client secret.
const clientSecretAudience = "https://appleid.apple.com"

// clientSecretLifetime is how long a client secret is valid. Apple allows up
// to six months; one is made for each call, so a few minutes is plenty.
const clientSecretLifetime = 5 * time.Minute

// requestTimeout bounds one call to Apple when the caller's context has no
// deadline of its own.
const requestTimeout = 10 * time.Second

// Credentials are the Sign in with Apple key and the IDs it signs for.
type Credentials struct {
	// TeamID is the Apple Developer team ID (the client secret's iss).
	TeamID string
	// KeyID is the 10-character ID of the Sign in with Apple key (kid).
	KeyID string
	// ClientID is the app's bundle ID (the client secret's sub), the same
	// audience identity tokens are verified against.
	ClientID string
	// PrivateKey is the key's .p8, parsed. Never log it.
	PrivateKey *ecdsa.PrivateKey
}

// ClientOptions configures a Client. All fields are optional.
type ClientOptions struct {
	// BaseURL replaces DefaultBaseURL, for tests.
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

// Client calls Apple's token and revoke endpoints.
type Client struct {
	creds   Credentials
	baseURL string
	http    *http.Client
	now     func() time.Time
}

// NewClient returns a Client for creds.
func NewClient(creds Credentials, opts ClientOptions) *Client {
	c := &Client{creds: creds, baseURL: strings.TrimRight(opts.BaseURL, "/"), http: opts.HTTPClient, now: opts.Now}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: requestTimeout}
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// ClientSecret returns a fresh client secret: an ES256 JWT signed with the
// key, as Apple's token and revoke endpoints require.
func (c *Client) ClientSecret() (string, error) {
	now := c.now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
		Issuer:    c.creds.TeamID,
		Subject:   c.creds.ClientID,
		Audience:  jwt.ClaimStrings{clientSecretAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(clientSecretLifetime)),
	})
	token.Header["kid"] = c.creds.KeyID
	secret, err := token.SignedString(c.creds.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("siwa: sign client secret: %w", err)
	}
	return secret, nil
}

// AppleError is an error answer from Apple. It holds only the status and
// Apple's error code, so it is safe to log.
type AppleError struct {
	Endpoint string
	Status   int
	Code     string
}

func (e *AppleError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("siwa: apple %s answered %d", e.Endpoint, e.Status)
	}
	return fmt.Sprintf("siwa: apple %s answered %d %s", e.Endpoint, e.Status, e.Code)
}

// ExchangeCode trades an authorization code from the app for a refresh token.
func (c *Client) ExchangeCode(ctx context.Context, code string) (string, error) {
	body, err := c.post(ctx, "token", url.Values{
		"code":       {code},
		"grant_type": {"authorization_code"},
	})
	if err != nil {
		return "", err
	}
	var answer struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return "", errors.New("siwa: apple token answer is not JSON")
	}
	if answer.RefreshToken == "" {
		return "", errors.New("siwa: apple token answer has no refresh token")
	}
	return answer.RefreshToken, nil
}

// RevokeRefreshToken tells Apple to revoke the token, which ends the app's
// Sign in with Apple authorization for that user.
func (c *Client) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	_, err := c.post(ctx, "revoke", url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
	})
	return err
}

// post sends form to /auth/<endpoint> with the client ID and a fresh secret.
func (c *Client) post(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	secret, err := c.ClientSecret()
	if err != nil {
		return nil, err
	}
	form.Set("client_id", c.creds.ClientID)
	form.Set("client_secret", secret)
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/auth/"+endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("siwa: build %s request: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		// The URL holds no secrets; the form isn't part of the error.
		return nil, fmt.Errorf("siwa: apple %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("siwa: read apple %s answer: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		var answer struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &answer)
		return nil, &AppleError{Endpoint: endpoint, Status: resp.StatusCode, Code: safeCode(answer.Error)}
	}
	return body, nil
}

// safeCode keeps Apple's error code only if it looks like one
// ("invalid_grant"), so nothing else from the answer reaches a log.
func safeCode(s string) string {
	if len(s) == 0 || len(s) > 40 {
		return ""
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '_' {
			return ""
		}
	}
	return s
}
