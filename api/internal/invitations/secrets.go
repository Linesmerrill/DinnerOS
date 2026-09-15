package invitations

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const (
	tokenBytes = 32
	// TokenLength is the length of an encoded token (unpadded base64url).
	TokenLength = 43
	// CodeLength is the number of significant characters in an invite code.
	CodeLength = 10
	// codeAlphabet is Crockford base32 (no I, L, O, U). 32 symbols divide 256
	// evenly, so masking a random byte to 5 bits is unbiased.
	codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

// newToken returns 32 random bytes as unpadded base64url.
func newToken(random io.Reader) (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := io.ReadFull(random, b); err != nil {
		return "", fmt.Errorf("invitations: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newCode returns CodeLength random characters from codeAlphabet (50 bits),
// without separators.
func newCode(random io.Reader) (string, error) {
	b := make([]byte, CodeLength)
	if _, err := io.ReadFull(random, b); err != nil {
		return "", fmt.Errorf("invitations: generate code: %w", err)
	}
	for i := range b {
		b[i] = codeAlphabet[b[i]&0x1f]
	}
	return string(b), nil
}

// FormatCode displays a normalized code as XXXXX-XXXXX.
func FormatCode(code string) string {
	if len(code) != CodeLength {
		return code
	}
	return code[:5] + "-" + code[5:]
}

// NormalizeCode turns user input into a canonical code: uppercase, spaces and
// dashes removed, and the commonly confused letters O, I, and L read as 0, 1,
// and 1 (as Crockford base32 specifies). ok is false when the result is not a
// well-formed code.
func NormalizeCode(input string) (code string, ok bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(input) {
		switch r {
		case ' ', '-', '\t', ' ':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		if !strings.ContainsRune(codeAlphabet, r) {
			return "", false
		}
		b.WriteRune(r)
		if b.Len() > CodeLength {
			return "", false
		}
	}
	if b.Len() != CodeLength {
		return "", false
	}
	return b.String(), true
}

// hashSecret returns the hex SHA-256 of a token or normalized code. Both
// secrets have enough entropy that an unsalted fast hash is appropriate, and
// it allows lookup by hash.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
