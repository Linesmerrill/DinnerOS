// Package auth verifies identity-provider tokens (Sign in with Apple, Sign in
// with Google) and issues DinnerOS access and refresh tokens.
//
// Provider tokens are verified and then discarded. Tokens, identity tokens, and
// nonces are never logged or stored; refresh tokens are persisted only as
// SHA-256 hashes.
package auth

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// Provider token issuers.
const (
	AppleIssuer = "https://appleid.apple.com"
)

// GoogleIssuers are the issuer values Google uses in ID tokens.
var GoogleIssuers = []string{"accounts.google.com", "https://accounts.google.com"}

// providerClockSkew tolerates small clock differences with Apple and Google.
const providerClockSkew = time.Minute

// ErrInvalidIdentityToken means a provider token failed verification. The
// wrapped error describes why and is safe to log; it never contains the token.
var ErrInvalidIdentityToken = errors.New("auth: invalid identity token")

// IdentityVerifier verifies a provider identity token and returns the identity
// it asserts. This is the AuthProvider seam from docs/architecture.md.
//
// nonce is the raw nonce the client generated for this sign-in; verifiers that
// do not use nonces accept an empty string.
//
// Errors wrap ErrInvalidIdentityToken when the token is not acceptable, or
// ErrKeysUnavailable when the provider's keys could not be fetched.
type IdentityVerifier interface {
	Verify(ctx context.Context, token, nonce string) (users.VerifiedIdentity, error)
}

// KeySource resolves a signing key by key ID. *JWKSClient implements it.
type KeySource interface {
	Key(ctx context.Context, kid string) (*rsa.PublicKey, error)
}

// providerClaims are the claims DinnerOS reads from Apple and Google tokens.
type providerClaims struct {
	jwt.RegisteredClaims
	Email         string       `json:"email"`
	EmailVerified flexibleBool `json:"email_verified"`
	Nonce         string       `json:"nonce"`
	Name          string       `json:"name"`
}

// flexibleBool accepts a JSON boolean or the strings "true"/"false". Apple has
// historically sent email_verified as a string.
type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch t := v.(type) {
	case bool:
		*b = flexibleBool(t)
	case string:
		*b = flexibleBool(strings.EqualFold(t, "true"))
	case nil:
		*b = false
	default:
		return fmt.Errorf("email_verified: unexpected type %T", v)
	}
	return nil
}

// oidcVerifier holds the verification shared by Apple and Google.
type oidcVerifier struct {
	provider users.Provider
	keys     KeySource
	issuers  []string
	audience string
	now      func() time.Time
	// checkNonce validates the nonce claim against the client's raw nonce.
	checkNonce func(claim, raw string) error
}

func (v *oidcVerifier) verify(ctx context.Context, token, nonce string) (users.VerifiedIdentity, error) {
	if token == "" {
		return users.VerifiedIdentity{}, fmt.Errorf("%w: token is empty", ErrInvalidIdentityToken)
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(providerClockSkew),
		jwt.WithTimeFunc(v.now),
	)

	var claims providerClaims
	var keyErr error
	_, err := parser.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("token has no kid header")
		}
		key, err := v.keys.Key(ctx, kid)
		if err != nil {
			keyErr = err
			return nil, err
		}
		return key, nil
	})
	if errors.Is(keyErr, ErrKeysUnavailable) {
		return users.VerifiedIdentity{}, keyErr
	}
	if err != nil {
		return users.VerifiedIdentity{}, fmt.Errorf("%w: %w", ErrInvalidIdentityToken, err)
	}

	switch {
	case claims.IssuedAt == nil:
		return users.VerifiedIdentity{}, fmt.Errorf("%w: iat is required", ErrInvalidIdentityToken)
	case !slices.Contains(v.issuers, claims.Issuer):
		return users.VerifiedIdentity{}, fmt.Errorf("%w: %w", ErrInvalidIdentityToken, jwt.ErrTokenInvalidIssuer)
	case claims.Subject == "":
		return users.VerifiedIdentity{}, fmt.Errorf("%w: sub is required", ErrInvalidIdentityToken)
	}
	if err := v.checkNonce(claims.Nonce, nonce); err != nil {
		return users.VerifiedIdentity{}, fmt.Errorf("%w: %w", ErrInvalidIdentityToken, err)
	}

	identity := users.VerifiedIdentity{
		Provider:    v.provider,
		Subject:     claims.Subject,
		DisplayName: claims.Name,
	}
	// Email is trusted only when the provider says it verified it.
	if claims.Email != "" && bool(claims.EmailVerified) {
		identity.Email = strings.ToLower(claims.Email)
		identity.EmailVerified = true
	}
	return identity, nil
}

// VerifierOptions holds dependencies shared by provider verifiers. Keys is
// required; Now defaults to time.Now.
type VerifierOptions struct {
	Keys KeySource
	Now  func() time.Time
}

func (o VerifierOptions) now() func() time.Time {
	if o.Now == nil {
		return time.Now
	}
	return o.Now
}

// AppleVerifier verifies Sign in with Apple identity tokens.
type AppleVerifier struct{ v oidcVerifier }

// NewAppleVerifier returns a verifier for tokens issued to bundleID.
func NewAppleVerifier(bundleID string, opts VerifierOptions) *AppleVerifier {
	return &AppleVerifier{v: oidcVerifier{
		provider:   users.ProviderApple,
		keys:       opts.Keys,
		issuers:    []string{AppleIssuer},
		audience:   bundleID,
		now:        opts.now(),
		checkNonce: checkAppleNonce,
	}}
}

// Verify implements IdentityVerifier. Apple requires a nonce: the token's
// nonce claim must equal the hex SHA-256 of the raw nonce.
func (a *AppleVerifier) Verify(ctx context.Context, token, nonce string) (users.VerifiedIdentity, error) {
	identity, err := a.v.verify(ctx, token, nonce)
	// Apple tokens carry no name; the client supplies it separately.
	identity.DisplayName = ""
	return identity, err
}

func checkAppleNonce(claim, raw string) error {
	if raw == "" {
		return errors.New("nonce is required")
	}
	sum := sha256.Sum256([]byte(raw))
	want := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(claim)), []byte(want)) != 1 {
		return errors.New("nonce mismatch")
	}
	return nil
}

// GoogleVerifier verifies Google Sign-In ID tokens.
type GoogleVerifier struct{ v oidcVerifier }

// NewGoogleVerifier returns a verifier for tokens issued to clientID.
func NewGoogleVerifier(clientID string, opts VerifierOptions) *GoogleVerifier {
	return &GoogleVerifier{v: oidcVerifier{
		provider:   users.ProviderGoogle,
		keys:       opts.Keys,
		issuers:    GoogleIssuers,
		audience:   clientID,
		now:        opts.now(),
		checkNonce: checkGoogleNonce,
	}}
}

// Verify implements IdentityVerifier. The nonce is optional for Google; when
// the client supplies one, the token's nonce claim must equal it exactly.
func (g *GoogleVerifier) Verify(ctx context.Context, token, nonce string) (users.VerifiedIdentity, error) {
	return g.v.verify(ctx, token, nonce)
}

func checkGoogleNonce(claim, raw string) error {
	if raw == "" {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(claim), []byte(raw)) != 1 {
		return errors.New("nonce mismatch")
	}
	return nil
}
