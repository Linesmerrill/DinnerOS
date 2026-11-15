package plus

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// DeviceChecker is Apple DeviceCheck's two bits per device. Bit 0 means
// "this device is tied to a household" (decision 698).
type DeviceChecker interface {
	// Query reports whether the device's bit 0 is set.
	Query(ctx context.Context, p households.DeviceProof) (tied bool, err error)
	// Tie sets bit 0.
	Tie(ctx context.Context, p households.DeviceProof) error
}

// DeviceCheck servers.
const (
	DeviceCheckProduction  = "https://api.devicecheck.apple.com"
	DeviceCheckDevelopment = "https://api.development.devicecheck.apple.com"
)

// DeviceCheckOptions configures the DeviceCheck client.
type DeviceCheckOptions struct {
	KeyID  string
	TeamID string
	// PrivateKey is the .p8 (ES256) key. Never log it.
	PrivateKey *ecdsa.PrivateKey
	// ProductionURL and DevelopmentURL override the servers, for tests.
	ProductionURL, DevelopmentURL string
	HTTPClient                    *http.Client
	Now                           func() time.Time
}

// DeviceCheckClient calls Apple's DeviceCheck server with a signed token.
type DeviceCheckClient struct {
	opts DeviceCheckOptions

	mu       sync.Mutex
	token    string
	issuedAt time.Time
}

var _ DeviceChecker = (*DeviceCheckClient)(nil)

// NewDeviceCheckClient returns a client.
func NewDeviceCheckClient(opts DeviceCheckOptions) *DeviceCheckClient {
	if opts.ProductionURL == "" {
		opts.ProductionURL = DeviceCheckProduction
	}
	if opts.DevelopmentURL == "" {
		opts.DevelopmentURL = DeviceCheckDevelopment
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &DeviceCheckClient{opts: opts}
}

// DeviceCheckError is a response Apple refused: its status and short text.
type DeviceCheckError struct {
	Status int
	Text   string
}

func (e *DeviceCheckError) Error() string {
	return fmt.Sprintf("devicecheck: %d %s", e.Status, e.Text)
}

// jwt is the provider token, reused for 50 minutes (Apple accepts an hour).
func (c *DeviceCheckClient) jwt() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.opts.Now()
	if c.token != "" && now.Sub(c.issuedAt) < 50*time.Minute {
		return c.token, nil
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"iss": c.opts.TeamID, "iat": now.Unix()})
	t.Header["kid"] = c.opts.KeyID
	signed, err := t.SignedString(c.opts.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("devicecheck: sign token: %w", err)
	}
	c.token, c.issuedAt = signed, now
	return signed, nil
}

func transactionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Call posts body to path (query_two_bits or update_two_bits) and returns
// Apple's status and body.
func (c *DeviceCheckClient) Call(ctx context.Context, development bool, path string, body map[string]any) (int, []byte, error) {
	base := c.opts.ProductionURL
	if development {
		base = c.opts.DevelopmentURL
	}
	token, err := c.jwt()
	if err != nil {
		return 0, nil, err
	}
	body["transaction_id"] = transactionID()
	body["timestamp"] = c.opts.Now().UnixMilli()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/"+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("devicecheck: %w", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, out, nil
}

// ShortText is Apple's error text trimmed to one short line, safe to log.
func ShortText(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Query implements DeviceChecker. A device never set answers 200 with
// "Failed to find bit state", which is not tied.
func (c *DeviceCheckClient) Query(ctx context.Context, p households.DeviceProof) (bool, error) {
	status, body, err := c.Call(ctx, p.Development, "query_two_bits", map[string]any{"device_token": p.Token})
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, &DeviceCheckError{Status: status, Text: ShortText(body)}
	}
	var bits struct {
		Bit0 bool `json:"bit0"`
	}
	if err := json.Unmarshal(body, &bits); err != nil {
		// "Failed to find bit state": never set.
		return false, nil
	}
	return bits.Bit0, nil
}

// Tie implements DeviceChecker.
func (c *DeviceCheckClient) Tie(ctx context.Context, p households.DeviceProof) error {
	status, body, err := c.Call(ctx, p.Development, "update_two_bits", map[string]any{"device_token": p.Token, "bit0": true, "bit1": false})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return &DeviceCheckError{Status: status, Text: ShortText(body)}
	}
	return nil
}

// MemoryDeviceCheck is a DeviceChecker that keeps bits in memory by token,
// so one token is one device. For tests.
type MemoryDeviceCheck struct {
	mu   sync.Mutex
	bits map[string]bool
	// Queries and Ties count calls.
	Queries, Ties int
	// Err fails every call when set.
	Err error
}

var _ DeviceChecker = (*MemoryDeviceCheck)(nil)

// NewMemoryDeviceCheck returns an empty one.
func NewMemoryDeviceCheck() *MemoryDeviceCheck { return &MemoryDeviceCheck{bits: map[string]bool{}} }

// Query implements DeviceChecker.
func (m *MemoryDeviceCheck) Query(_ context.Context, p households.DeviceProof) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Queries++
	if m.Err != nil {
		return false, m.Err
	}
	return m.bits[p.Token], nil
}

// Tie implements DeviceChecker.
func (m *MemoryDeviceCheck) Tie(_ context.Context, p households.DeviceProof) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Ties++
	if m.Err != nil {
		return m.Err
	}
	m.bits[p.Token] = true
	return nil
}
