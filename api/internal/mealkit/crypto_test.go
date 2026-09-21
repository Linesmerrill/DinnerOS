package mealkit

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestSealAndOpenTokensRoundTrips(t *testing.T) {
	c := testCipher(t)
	in := Tokens{AccessToken: "access-value", RefreshToken: "refresh-value", ExpiresAt: time.Now().UTC().Truncate(time.Second)}

	env, err := c.SealTokens(in)
	if err != nil {
		t.Fatalf("SealTokens() error = %v", err)
	}
	if env.Empty() || env.KeyID == "" || len(env.Key) == 0 {
		t.Fatalf("envelope = %+v", env)
	}
	// Neither the tokens nor the key-encryption key appear in what is stored.
	for _, secret := range [][]byte{[]byte(in.AccessToken), []byte(in.RefreshToken), testKey} {
		if bytes.Contains(env.Ciphertext, secret) || bytes.Contains(env.Key, secret) {
			t.Errorf("a secret is readable in the stored envelope")
		}
	}
	if env.KeyID == base64.StdEncoding.EncodeToString(testKey) || strings.Contains(env.KeyID, "0123456789") {
		t.Errorf("keyId leaks key material: %q", env.KeyID)
	}

	out, err := c.OpenTokens(env)
	if err != nil {
		t.Fatalf("OpenTokens() error = %v", err)
	}
	if out.AccessToken != in.AccessToken || out.RefreshToken != in.RefreshToken || !out.ExpiresAt.Equal(in.ExpiresAt) {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestSealTokensUsesAFreshDataKeyEachTime(t *testing.T) {
	c := testCipher(t)
	a, _ := c.SealTokens(Tokens{AccessToken: "same"})
	b, _ := c.SealTokens(Tokens{AccessToken: "same"})
	if bytes.Equal(a.Ciphertext, b.Ciphertext) || bytes.Equal(a.Key, b.Key) {
		t.Error("two seals of the same tokens produced the same bytes; the data key is not fresh")
	}
}

func TestOpenTokensRefusesAnotherKeyAndTampering(t *testing.T) {
	c := testCipher(t)
	env, err := c.SealTokens(Tokens{AccessToken: "access-value"})
	if err != nil {
		t.Fatalf("SealTokens() error = %v", err)
	}

	other, err := NewCipher([]byte("ffffffffffffffffffffffffffffffff"))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	if _, err := other.OpenTokens(env); err == nil {
		t.Error("a different key decrypted the envelope")
	} else if strings.Contains(err.Error(), "access-value") {
		t.Errorf("the error quotes the secret: %v", err)
	}

	tampered := env
	tampered.Ciphertext = append([]byte(nil), env.Ciphertext...)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 0xff
	if _, err := c.OpenTokens(tampered); err == nil {
		t.Error("a tampered ciphertext decrypted")
	}
	if _, err := c.OpenTokens(Envelope{}); err == nil {
		t.Error("an empty envelope decrypted")
	}
}

func TestNewCipherRefusesAShortKeyWithoutQuotingIt(t *testing.T) {
	_, err := NewCipher([]byte("too-short"))
	if err == nil {
		t.Fatal("NewCipher() accepted a short key")
	}
	if strings.Contains(err.Error(), "too-short") {
		t.Errorf("the error quotes the key: %v", err)
	}
}

func TestParseKeyAcceptsBase64AndRawBytes(t *testing.T) {
	raw := make([]byte, 48)
	for i := range raw {
		raw[i] = byte(i)
	}
	for name, encoded := range map[string]string{
		"standard": base64.StdEncoding.EncodeToString(raw),
		"raw url":  base64.RawURLEncoding.EncodeToString(raw),
	} {
		got, err := ParseKey(encoded)
		if err != nil || !bytes.Equal(got, raw) {
			t.Errorf("ParseKey(%s) = %v, %v", name, got, err)
		}
	}
	// A passphrase with characters base64 cannot hold, so it is taken raw.
	long := strings.Repeat("correct horse! ", 4)
	if got, err := ParseKey(long); err != nil || string(got) != long {
		t.Errorf("ParseKey(passphrase) = %v, %v", got, err)
	}
	if _, err := ParseKey("short"); err == nil {
		t.Error("ParseKey accepted a short value")
	} else if strings.Contains(err.Error(), "short") && strings.Contains(err.Error(), `"short"`) {
		t.Errorf("the error quotes the value: %v", err)
	}
	if _, err := ParseKey(""); err == nil {
		t.Error("ParseKey accepted an empty value")
	}
}
