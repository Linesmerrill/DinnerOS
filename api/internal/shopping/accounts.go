package shopping

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file is a household's signed-in account with a provider that writes
// carts itself (Kroger): connecting it through the provider's own sign-in
// page, keeping its tokens encrypted, refreshing them, and disconnecting
// (decision 649).
//
// One member connects for the household, the way one Walmart cart serves
// everyone: whoever sends the week's list fills that account's cart.

var (
	// ErrNotConnected means the provider needs a member to sign in to it
	// before DinnerOS can fill the cart.
	ErrNotConnected = errors.New("shopping: provider account not connected")
	// ErrBadConnectState means a sign-in came back with a state DinnerOS
	// didn't issue, or one that expired.
	ErrBadConnectState = errors.New("shopping: sign-in state is invalid or expired")
	// ErrCartUnavailable means the provider failed to add to the cart;
	// nothing was sent, and trying again may work.
	ErrCartUnavailable = errors.New("shopping: provider cart unavailable")
)

// connectStateTTL is how long a member has to finish signing in.
const connectStateTTL = 15 * time.Minute

// ProviderAccount is a household's connection to a provider account. Tokens
// are stored encrypted (tokenBox); this type holds them decrypted.
type ProviderAccount struct {
	HouseholdID string
	Provider    providers.Key
	Token       providers.CustomerToken
	ConnectedBy string
	ConnectedAt time.Time
	UpdatedAt   time.Time
}

// Connection is what members see of a provider account: whether one is
// connected, by whom, and when. Never the tokens.
type Connection struct {
	Provider    providers.Key
	Connected   bool
	ConnectedBy string
	ConnectedAt time.Time
}

// tokenBox encrypts provider tokens at rest (AES-256-GCM) and signs sign-in
// states (HMAC-SHA256), with separate keys derived from one secret.
type tokenBox struct {
	aead     cipher.AEAD
	stateKey []byte
}

// newTokenBox derives the keys from secret (the server's signing key). A nil
// box means connected providers are off.
func newTokenBox(secret []byte) (*tokenBox, error) {
	if len(secret) == 0 {
		return nil, nil
	}
	derive := func(label string) []byte {
		m := hmac.New(sha256.New, secret)
		m.Write([]byte("dinneros/shopping/" + label))
		return m.Sum(nil)
	}
	block, err := aes.NewCipher(derive("provider-tokens"))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &tokenBox{aead: aead, stateKey: derive("connect-state")}, nil
}

func (b *tokenBox) seal(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (b *tokenBox) open(sealed string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", errors.New("shopping: stored token is unreadable")
	}
	plain, err := b.aead.Open(nil, raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():], nil)
	if err != nil {
		return "", errors.New("shopping: stored token is unreadable")
	}
	return string(plain), nil
}

// connectState is what a sign-in carries through the provider and back.
type connectState struct {
	HouseholdID string        `json:"h"`
	UserID      string        `json:"u"`
	Provider    providers.Key `json:"p"`
	Expires     int64         `json:"e"`
	Nonce       string        `json:"n"`
}

func (b *tokenBox) signState(st connectState) (string, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	st.Nonce = base64.RawURLEncoding.EncodeToString(nonce)
	raw, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	m := hmac.New(sha256.New, b.stateKey)
	m.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

func (b *tokenBox) readState(s string, now time.Time) (connectState, error) {
	body, sig, ok := strings.Cut(s, ".")
	if !ok {
		return connectState{}, ErrBadConnectState
	}
	m := hmac.New(sha256.New, b.stateKey)
	m.Write([]byte(body))
	want := base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return connectState{}, ErrBadConnectState
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return connectState{}, ErrBadConnectState
	}
	var st connectState
	if err := json.Unmarshal(raw, &st); err != nil || now.Unix() > st.Expires {
		return connectState{}, ErrBadConnectState
	}
	return st, nil
}

// accountProvider returns the enabled provider named key that members sign
// in to.
func (s *Service) accountProvider(key string) (providers.GroceryProvider, providers.CustomerAccount, error) {
	p, err := s.Provider(key)
	if err != nil {
		return nil, nil, err
	}
	account, ok := p.(providers.CustomerAccount)
	if !ok || s.tokens == nil || s.members == nil {
		return nil, nil, invalid("%s doesn't use a sign-in", p.Name())
	}
	return p, account, nil
}

// Connection reports whether the household connected a provider account.
func (s *Service) Connection(ctx context.Context, householdID, provider string) (Connection, error) {
	if householdID == "" {
		return Connection{}, errHouseholdRequired
	}
	p, _, err := s.accountProvider(provider)
	if err != nil {
		return Connection{}, err
	}
	a, err := s.store.GetProviderAccount(ctx, householdID, p.Key())
	if errors.Is(err, ErrNotFound) {
		return Connection{Provider: p.Key()}, nil
	}
	if err != nil {
		return Connection{}, fmt.Errorf("get provider account: %w", err)
	}
	return Connection{Provider: p.Key(), Connected: true, ConnectedBy: a.ConnectedBy, ConnectedAt: a.ConnectedAt}, nil
}

// ConnectURL returns the provider's sign-in page for the actor to connect
// the household's account. It comes back to CompleteConnection.
func (s *Service) ConnectURL(ctx context.Context, actor households.Membership, provider string) (string, error) {
	if err := authorize(actor, households.PermShoppingEdit); err != nil {
		return "", err
	}
	p, account, err := s.accountProvider(provider)
	if err != nil {
		return "", err
	}
	state, err := s.tokens.signState(connectState{
		HouseholdID: actor.HouseholdID, UserID: actor.UserID, Provider: p.Key(), Expires: s.now().Add(connectStateTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	return account.AuthorizeURL(state), nil
}

// CompleteConnection finishes a sign-in: it checks the state, trades the
// code for tokens, and stores them for the household the state names. The
// member who started it must still be able to edit its shopping.
func (s *Service) CompleteConnection(ctx context.Context, provider, state, code string) (Connection, error) {
	p, account, err := s.accountProvider(provider)
	if err != nil {
		return Connection{}, err
	}
	if s.tokens == nil {
		return Connection{}, ErrBadConnectState
	}
	st, err := s.tokens.readState(state, s.now())
	if err != nil || st.Provider != p.Key() {
		return Connection{}, ErrBadConnectState
	}
	member, err := s.members.GetMembership(ctx, st.HouseholdID, st.UserID)
	if err != nil {
		return Connection{}, ErrBadConnectState
	}
	if err := authorize(member, households.PermShoppingEdit); err != nil {
		return Connection{}, err
	}
	if strings.TrimSpace(code) == "" {
		return Connection{}, invalid("The sign-in didn't finish. Try again.")
	}
	token, err := account.ExchangeCode(ctx, code)
	if err != nil {
		return Connection{}, fmt.Errorf("finish %s sign-in: %w", p.Name(), err)
	}
	now := s.timestamp()
	a := ProviderAccount{HouseholdID: st.HouseholdID, Provider: p.Key(), Token: token, ConnectedBy: st.UserID, ConnectedAt: now, UpdatedAt: now}
	if err := s.putAccount(ctx, a); err != nil {
		return Connection{}, err
	}
	s.logger.InfoContext(ctx, "shopping provider connected", "householdId", st.HouseholdID, "provider", p.Key())
	return Connection{Provider: p.Key(), Connected: true, ConnectedBy: a.ConnectedBy, ConnectedAt: now}, nil
}

// Disconnect forgets the household's provider account. The provider's own
// sign-in stays until the member signs out there.
func (s *Service) Disconnect(ctx context.Context, actor households.Membership, provider string) error {
	if err := authorize(actor, households.PermShoppingEdit); err != nil {
		return err
	}
	p, _, err := s.accountProvider(provider)
	if err != nil {
		return err
	}
	if err := s.store.DeleteProviderAccount(ctx, actor.HouseholdID, p.Key()); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete provider account: %w", err)
	}
	return nil
}

// customerToken returns a current access token for the household's account,
// refreshing and storing it when it's about to expire. ErrNotConnected when
// there is no account, or when the provider refused the refresh (the member
// signs in again; the dead account is forgotten).
func (s *Service) customerToken(ctx context.Context, householdID string, p providers.GroceryProvider) (string, error) {
	account, ok := p.(providers.CustomerAccount)
	if !ok || s.tokens == nil {
		return "", ErrNotConnected
	}
	a, err := s.getAccount(ctx, householdID, p.Key())
	if errors.Is(err, ErrNotFound) {
		return "", ErrNotConnected
	}
	if err != nil {
		return "", err
	}
	if s.now().Add(time.Minute).Before(a.Token.ExpiresAt) {
		return a.Token.AccessToken, nil
	}
	token, err := account.RefreshCustomer(ctx, a.Token.RefreshToken)
	if err != nil {
		if errors.Is(err, providers.ErrCustomerSignedOut) {
			_ = s.store.DeleteProviderAccount(ctx, householdID, p.Key())
			return "", ErrNotConnected
		}
		return "", fmt.Errorf("refresh %s sign-in: %w", p.Name(), err)
	}
	a.Token, a.UpdatedAt = token, s.timestamp()
	if err := s.putAccount(ctx, a); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func (s *Service) putAccount(ctx context.Context, a ProviderAccount) error {
	access, err := s.tokens.seal(a.Token.AccessToken)
	if err != nil {
		return err
	}
	refresh, err := s.tokens.seal(a.Token.RefreshToken)
	if err != nil {
		return err
	}
	if err := s.store.PutProviderAccount(ctx, StoredAccount{
		HouseholdID: a.HouseholdID, Provider: a.Provider, AccessToken: access, RefreshToken: refresh, ExpiresAt: a.Token.ExpiresAt,
		ConnectedBy: a.ConnectedBy, ConnectedAt: a.ConnectedAt, UpdatedAt: a.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("put provider account: %w", err)
	}
	return nil
}

func (s *Service) getAccount(ctx context.Context, householdID string, provider providers.Key) (ProviderAccount, error) {
	stored, err := s.store.GetProviderAccount(ctx, householdID, provider)
	if err != nil {
		return ProviderAccount{}, err
	}
	access, err := s.tokens.open(stored.AccessToken)
	if err != nil {
		return ProviderAccount{}, ErrNotFound // unreadable after a key change: sign in again
	}
	refresh, err := s.tokens.open(stored.RefreshToken)
	if err != nil {
		return ProviderAccount{}, ErrNotFound
	}
	return ProviderAccount{
		HouseholdID: stored.HouseholdID, Provider: stored.Provider,
		Token:       providers.CustomerToken{AccessToken: access, RefreshToken: refresh, ExpiresAt: stored.ExpiresAt},
		ConnectedBy: stored.ConnectedBy, ConnectedAt: stored.ConnectedAt, UpdatedAt: stored.UpdatedAt,
	}, nil
}

// StoredAccount is a ProviderAccount as stored: tokens sealed.
type StoredAccount struct {
	HouseholdID  string
	Provider     providers.Key
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	ConnectedBy  string
	ConnectedAt  time.Time
	UpdatedAt    time.Time
}
