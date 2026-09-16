package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Message is one push to one device.
type Message struct {
	Token       string
	Environment Environment
	Title       string
	Body        string
	// ThreadID groups related alerts in Notification Center.
	ThreadID string
	// CollapseID replaces an earlier alert with the same ID (at most 64 bytes).
	CollapseID string
	// Expiration is when APNs stops trying to deliver. Zero: deliver once
	// or not at all.
	Expiration time.Time
	// Data is added to the payload beside "aps" for the app to read.
	Data map[string]any
}

// Sender delivers one push. APNsClient is the real one; tests use a fake.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// ErrDeviceTokenInvalid (wrapped in *APNsError) means APNs will never accept
// this token again: BadDeviceToken or 410 Unregistered. Its registration
// should be deleted.
var ErrDeviceTokenInvalid = errors.New("push: device token is no longer valid")

// APNsError is a rejected push.
type APNsError struct {
	Status int
	// Reason is APNs' reason string, such as "BadDeviceToken".
	Reason string
}

func (e *APNsError) Error() string {
	return fmt.Sprintf("apns: %d %s", e.Status, e.Reason)
}

// Is reports ErrDeviceTokenInvalid for the rejections that end a token.
func (e *APNsError) Is(target error) bool {
	return target == ErrDeviceTokenInvalid &&
		(e.Status == http.StatusGone || e.Reason == "BadDeviceToken" || e.Reason == "Unregistered")
}

// APNs gateways.
const (
	APNsProductionURL = "https://api.push.apple.com"
	APNsSandboxURL    = "https://api.sandbox.push.apple.com"
)

// providerTokenLifetime is how long a signed provider token is reused. APNs
// rejects tokens older than an hour and throttles ones refreshed more often
// than every 20 minutes.
const providerTokenLifetime = 45 * time.Minute

// APNsOptions configures an APNsClient.
type APNsOptions struct {
	KeyID      string
	TeamID     string
	Topic      string
	PrivateKey *ecdsa.PrivateKey
	// HTTPClient must speak HTTP/2 (APNs requires it). Default: a client
	// whose transport negotiates HTTP/2 over TLS, with a 15 s timeout.
	HTTPClient *http.Client
	// ProductionURL and SandboxURL override the gateways, for tests.
	ProductionURL string
	SandboxURL    string
	Now           func() time.Time
}

// APNsClient sends pushes with token-based (ES256 JWT) authentication.
type APNsClient struct {
	opts APNsOptions

	mu       sync.Mutex
	token    string
	signedAt time.Time
}

var _ Sender = (*APNsClient)(nil)

// NewAPNsClient returns a client. It fails without a key, key ID, team ID,
// and topic.
func NewAPNsClient(opts APNsOptions) (*APNsClient, error) {
	if opts.PrivateKey == nil || opts.KeyID == "" || opts.TeamID == "" || opts.Topic == "" {
		return nil, errors.New("apns: key, key ID, team ID, and topic are required")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{ForceAttemptHTTP2: true, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 90 * time.Second},
		}
	}
	if opts.ProductionURL == "" {
		opts.ProductionURL = APNsProductionURL
	}
	if opts.SandboxURL == "" {
		opts.SandboxURL = APNsSandboxURL
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &APNsClient{opts: opts}, nil
}

// providerToken returns the cached JWT, signing a new one when the cached
// one is older than providerTokenLifetime or force is set.
func (c *APNsClient) providerToken(force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.opts.Now()
	if !force && c.token != "" && now.Sub(c.signedAt) < providerTokenLifetime {
		return c.token, nil
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"iss": c.opts.TeamID, "iat": now.Unix()})
	t.Header["kid"] = c.opts.KeyID
	signed, err := t.SignedString(c.opts.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("apns: sign provider token: %w", err)
	}
	c.token, c.signedAt = signed, now
	return signed, nil
}

type apsAlert struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

type apsPayload struct {
	Alert    apsAlert `json:"alert"`
	Sound    string   `json:"sound"`
	ThreadID string   `json:"thread-id,omitempty"`
}

// Payload builds the JSON body APNs receives for m.
func Payload(m Message) ([]byte, error) {
	body := map[string]any{}
	for k, v := range m.Data {
		body[k] = v
	}
	body["aps"] = apsPayload{Alert: apsAlert{Title: m.Title, Body: m.Body}, Sound: "default", ThreadID: m.ThreadID}
	return json.Marshal(body)
}

// Send implements Sender. A provider token APNs reports expired is re-signed
// and the push retried once.
func (c *APNsClient) Send(ctx context.Context, m Message) error {
	base := c.opts.ProductionURL
	switch m.Environment {
	case EnvironmentProduction:
	case EnvironmentSandbox:
		base = c.opts.SandboxURL
	default:
		return fmt.Errorf("apns: unknown environment %q", m.Environment)
	}
	payload, err := Payload(m)
	if err != nil {
		return fmt.Errorf("apns: payload: %w", err)
	}
	err = c.send(ctx, base, m, payload, false)
	var apnsErr *APNsError
	if errors.As(err, &apnsErr) && apnsErr.Reason == "ExpiredProviderToken" {
		err = c.send(ctx, base, m, payload, true)
	}
	return err
}

func (c *APNsClient) send(ctx context.Context, base string, m Message, payload []byte, forceToken bool) error {
	token, err := c.providerToken(forceToken)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/3/device/"+m.Token, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("apns: request: %w", err)
	}
	req.Header.Set("authorization", "bearer "+token)
	req.Header.Set("apns-topic", c.opts.Topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("content-type", "application/json")
	if m.CollapseID != "" {
		req.Header.Set("apns-collapse-id", m.CollapseID)
	}
	expiration := int64(0)
	if !m.Expiration.IsZero() {
		expiration = m.Expiration.Unix()
	}
	req.Header.Set("apns-expiration", strconv.FormatInt(expiration, 10))

	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		// Never include the URL: it holds the device token.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("apns: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	return &APNsError{Status: resp.StatusCode, Reason: body.Reason}
}
