package invitations

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

func TestNewTokenFormatAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		token, err := newToken(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(token) != TokenLength {
			t.Fatalf("len(token) = %d, want %d", len(token), TokenLength)
		}
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(raw) != 32 {
			t.Fatalf("token %q is not 32 bytes of base64url: %v", token, err)
		}
		if seen[token] {
			t.Fatal("duplicate token")
		}
		seen[token] = true
	}
}

var codePattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{10}$`)

func TestNewCodeFormatAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		code, err := newCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if !codePattern.MatchString(code) {
			t.Fatalf("code %q has wrong format", code)
		}
		if strings.ContainsAny(code, "ILOU") {
			t.Fatalf("code %q contains an ambiguous letter", code)
		}
		if seen[code] {
			t.Fatal("duplicate code")
		}
		seen[code] = true

		if normalized, ok := NormalizeCode(FormatCode(code)); !ok || normalized != code {
			t.Fatalf("NormalizeCode(FormatCode(%q)) = %q, %v", code, normalized, ok)
		}
	}
}

func TestNewCodeMapsBytesToAlphabet(t *testing.T) {
	// Only the low 5 bits of each byte matter, so every symbol is equally likely.
	code, err := newCode(bytes.NewReader([]byte{0, 9, 10, 31, 32, 42, 255, 17, 18, 100}))
	if err != nil {
		t.Fatal(err)
	}
	if code != "09AZ0AZHJ4" {
		t.Errorf("newCode() = %q, want 09AZ0AZHJ4", code)
	}
	if _, err := newCode(bytes.NewReader([]byte{1, 2})); err == nil {
		t.Error("newCode(short reader) error = nil")
	}
}

func TestFormatCode(t *testing.T) {
	if got := FormatCode("ABCDE12345"); got != "ABCDE-12345" {
		t.Errorf("FormatCode() = %q", got)
	}
}

func TestNormalizeCode(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"ABCDE-FGHJK", "ABCDEFGHJK", true},
		{"abcde-fghjk", "ABCDEFGHJK", true},
		{"  ab cde - fg hjk ", "ABCDEFGHJK", true},
		{"ABCDEFGHJK", "ABCDEFGHJK", true},
		{"oo1il-23456", "00111-23456"[:5] + "23456", true},
		{"ABCDE FGHJK", "ABCDEFGHJK", true},
		{"ABCDE-FGHJ", "", false},   // too short
		{"ABCDE-FGHJKM", "", false}, // too long
		{"ABCDE-FGHJU", "", false},  // U is not in the alphabet
		{"ABCDE_FGHJK", "", false},
		{"", "", false},
		{"ÀBCDE-FGHJK", "", false},
	}
	for _, tt := range tests {
		got, ok := NormalizeCode(tt.input)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeCode(%q) = %q, %v; want %q, %v", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestHashSecret(t *testing.T) {
	a, b := hashSecret("secret-a"), hashSecret("secret-b")
	if len(a) != 64 || a == b || a != hashSecret("secret-a") {
		t.Errorf("hashSecret() = %q, %q", a, b)
	}
	if strings.Contains(a, "secret") {
		t.Error("hash contains the secret")
	}
	// Known SHA-256 vector.
	if got := hashSecret("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("hashSecret(abc) = %s", got)
	}
}
