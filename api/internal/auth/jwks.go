package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// Provider JWKS endpoints.
const (
	AppleJWKSURL  = "https://appleid.apple.com/auth/keys"
	GoogleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"
)

const (
	defaultJWKSTTL             = time.Hour
	defaultJWKSMinRefetch      = time.Minute
	defaultJWKSFetchTimeout    = 5 * time.Second
	maxJWKSResponseBytes       = 1 << 20
	minRSAKeyBits              = 2048
	maxRSAPublicExponentLength = 4 // bytes; real keys use 65537
)

var (
	// ErrUnknownKey means the token's key ID is not in the provider's key set.
	ErrUnknownKey = errors.New("auth: unknown signing key")
	// ErrKeysUnavailable means the provider's key set could not be fetched.
	ErrKeysUnavailable = errors.New("auth: provider signing keys unavailable")
)

// JWKSClient fetches and caches a provider's RSA signing keys.
//
// Keys are cached for TTL. A token whose kid is not cached triggers a refetch
// (the provider may have rotated keys), but at most once per MinRefetch
// interval, so a stream of tokens with bogus key IDs cannot turn into a fetch
// storm against the provider. If a refresh fails, cached keys keep working.
type JWKSClient struct {
	url        string
	httpClient *http.Client
	ttl        time.Duration
	minRefetch time.Duration
	now        func() time.Time

	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey
	fetchedAt   time.Time // last successful fetch
	attemptedAt time.Time // last fetch attempt, successful or not
}

// JWKSOptions configures a JWKSClient. Zero values use defaults.
type JWKSOptions struct {
	HTTPClient *http.Client  // default: client with a 5s timeout
	TTL        time.Duration // default: 1h
	MinRefetch time.Duration // default: 1m
	Now        func() time.Time
}

// NewJWKSClient returns a client for the JWKS document at url.
func NewJWKSClient(url string, opts JWKSOptions) *JWKSClient {
	c := &JWKSClient{
		url:        url,
		httpClient: opts.HTTPClient,
		ttl:        opts.TTL,
		minRefetch: opts.MinRefetch,
		now:        opts.Now,
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: defaultJWKSFetchTimeout}
	}
	if c.ttl <= 0 {
		c.ttl = defaultJWKSTTL
	}
	if c.minRefetch <= 0 {
		c.minRefetch = defaultJWKSMinRefetch
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// Key returns the RSA public key with the given key ID.
func (c *JWKSClient) Key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	key, cached := c.keys[kid]
	fresh := !c.fetchedAt.IsZero() && now.Sub(c.fetchedAt) < c.ttl
	if cached && fresh {
		return key, nil
	}

	// Refetch when the cache is stale or the kid is unknown, but never more
	// than once per minRefetch. Holding the lock while fetching collapses
	// concurrent refreshes into one request.
	canFetch := c.attemptedAt.IsZero() || now.Sub(c.attemptedAt) >= c.minRefetch
	if canFetch {
		if err := c.refresh(ctx, now); err != nil {
			if cached {
				return key, nil // serve the stale key rather than failing sign-in
			}
			return nil, err
		}
		key, cached = c.keys[kid]
	}

	switch {
	case cached:
		return key, nil
	case c.fetchedAt.IsZero():
		return nil, ErrKeysUnavailable // never fetched successfully; recent attempt failed
	default:
		return nil, ErrUnknownKey
	}
}

func (c *JWKSClient) refresh(ctx context.Context, now time.Time) error {
	c.attemptedAt = now
	keys, err := c.fetch(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
	}
	c.keys = keys
	c.fetchedAt = now
	return nil
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (c *JWKSClient) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks fetch: unexpected status %d", resp.StatusCode)
	}

	var doc jwksDocument
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJWKSResponseBytes)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("jwks decode: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		pub, err := k.rsaPublicKey()
		if err != nil {
			continue // skip malformed keys; others may still be usable
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("jwks: no usable RSA signing keys")
	}
	return keys, nil
}

func (k jwk) rsaPublicKey() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("jwk n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("jwk e: %w", err)
	}
	if len(eBytes) == 0 || len(eBytes) > maxRSAPublicExponentLength {
		return nil, errors.New("jwk e: invalid length")
	}
	n := new(big.Int).SetBytes(nBytes)
	if n.BitLen() < minRSAKeyBits {
		return nil, errors.New("jwk n: key too small")
	}
	e := int(new(big.Int).SetBytes(eBytes).Int64())
	if e < 3 || e%2 == 0 {
		return nil, errors.New("jwk e: invalid exponent")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}
