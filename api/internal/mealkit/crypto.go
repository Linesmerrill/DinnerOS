package mealkit

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// MinKeyBytes is the shortest accepted RECIPE_IMPORT_ENCRYPTION_KEY. AES-256
// wants exactly 32; a longer key is hashed down to 32 so a generated
// `openssl rand -base64 48` value works.
const MinKeyBytes = 32

// Envelope is one encrypted secret at rest: a random per-secret data key
// (DEK), itself encrypted with the configured key-encryption key (KEK), plus
// the ciphertext the DEK protects. Rotating the KEK re-wraps the small DEK
// rather than re-encrypting every secret.
//
// Nothing in here is a secret on its own: without the KEK the envelope is
// undecryptable, which is why it is safe to keep in MongoDB.
type Envelope struct {
	// KeyID identifies the KEK that wrapped Key, so a rotation can tell which
	// envelopes still need re-wrapping. It is a hash prefix, not key material.
	KeyID string
	// Key is the DEK, encrypted with the KEK (nonce prefixed).
	Key []byte
	// Ciphertext is the payload, encrypted with the DEK (nonce prefixed).
	Ciphertext []byte
}

// Empty reports whether the envelope holds nothing.
func (e Envelope) Empty() bool { return len(e.Ciphertext) == 0 }

// Cipher seals and opens envelopes with one key-encryption key.
type Cipher struct {
	aead  cipher.AEAD
	keyID string
	rand  io.Reader
}

// NewCipher returns a Cipher for a raw key of at least MinKeyBytes. A key of
// any accepted length is reduced to a 32-byte AES key with SHA-256, so the
// caller does not have to produce exactly 32 bytes.
//
// The error never contains the key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) < MinKeyBytes {
		return nil, fmt.Errorf("mealkit: encryption key must be at least %d bytes, got %d", MinKeyBytes, len(key))
	}
	sum := sha256.Sum256(key)
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("mealkit: encryption key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("mealkit: encryption key: %w", err)
	}
	// A separate hash, so the stored ID is not the AES key itself.
	id := sha256.Sum256(append([]byte("dinneros-mealkit-kek-id:"), key...))
	return &Cipher{aead: aead, keyID: hex.EncodeToString(id[:6]), rand: rand.Reader}, nil
}

// ParseKey decodes a configured key. It accepts base64 (padded or not,
// standard or URL-safe) and falls back to the raw bytes, so both
// `openssl rand -base64 48` and a long passphrase work. The error never
// contains the value.
func ParseKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, errors.New("encryption key is empty")
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(raw); err == nil && len(key) >= MinKeyBytes {
			return key, nil
		}
	}
	if len(raw) >= MinKeyBytes {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("encryption key must decode to at least %d bytes; generate one with `openssl rand -base64 48`", MinKeyBytes)
}

// KeyID is the identifier this Cipher stamps on envelopes it seals.
func (c *Cipher) KeyID() string { return c.keyID }

// storedTokens is the JSON form of Tokens inside an envelope. It exists only
// between Seal and Open.
type storedTokens struct {
	AccessToken  string    `json:"a"`
	RefreshToken string    `json:"r"`
	ExpiresAt    time.Time `json:"e"`
}

// SealTokens encrypts tokens under a fresh data key.
func (c *Cipher) SealTokens(t Tokens) (Envelope, error) {
	plaintext, err := json.Marshal(storedTokens(t))
	if err != nil {
		return Envelope{}, errors.New("mealkit: encode tokens")
	}
	dek := make([]byte, 32)
	if _, err := io.ReadFull(c.rand, dek); err != nil {
		return Envelope{}, errors.New("mealkit: generate data key")
	}
	inner, err := newAEAD(dek)
	if err != nil {
		return Envelope{}, err
	}
	ciphertext, err := seal(inner, c.rand, plaintext)
	if err != nil {
		return Envelope{}, err
	}
	wrapped, err := seal(c.aead, c.rand, dek)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{KeyID: c.keyID, Key: wrapped, Ciphertext: ciphertext}, nil
}

// OpenTokens decrypts an envelope. A wrong or rotated key, or any tampering,
// fails; the error never contains ciphertext or key material.
func (c *Cipher) OpenTokens(e Envelope) (Tokens, error) {
	if e.Empty() {
		return Tokens{}, errors.New("mealkit: no stored tokens")
	}
	dek, err := open(c.aead, e.Key)
	if err != nil {
		return Tokens{}, errors.New("mealkit: stored tokens cannot be decrypted with the configured RECIPE_IMPORT_ENCRYPTION_KEY")
	}
	inner, err := newAEAD(dek)
	if err != nil {
		return Tokens{}, err
	}
	plaintext, err := open(inner, e.Ciphertext)
	if err != nil {
		return Tokens{}, errors.New("mealkit: stored tokens are corrupt")
	}
	var stored storedTokens
	if err := json.Unmarshal(plaintext, &stored); err != nil {
		return Tokens{}, errors.New("mealkit: stored tokens are corrupt")
	}
	return Tokens(stored), nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("mealkit: data key is not usable")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("mealkit: data key is not usable")
	}
	return aead, nil
}

// seal returns nonce||ciphertext.
func seal(aead cipher.AEAD, random io.Reader, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(random, nonce); err != nil {
		return nil, errors.New("mealkit: generate nonce")
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func open(aead cipher.AEAD, data []byte) ([]byte, error) {
	if len(data) < aead.NonceSize() {
		return nil, errors.New("short ciphertext")
	}
	nonce, body := data[:aead.NonceSize()], data[aead.NonceSize():]
	return aead.Open(nil, nonce, body, nil)
}
