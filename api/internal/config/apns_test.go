package config

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"strings"
	"testing"
)

// generatedP8 returns a throwaway key in .p8 form. Keys are generated per
// test run; none is ever stored in the repository.
func generatedP8(t *testing.T, curve elliptic.Curve) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestLoadAPNs(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.APNs.Enabled() {
		t.Fatalf("defaults: APNs enabled = %v, err = %v", cfg.APNs.Enabled(), err)
	}

	p8 := generatedP8(t, elliptic.P256())
	all := map[string]string{"APNS_KEY_ID": "ABC123DEFG", "APNS_TEAM_ID": "6VTPDG2HNK", "APNS_AUTH_KEY": p8}
	cfg, err = Load(env(all))
	if err != nil || !cfg.APNs.Enabled() {
		t.Fatalf("configured: enabled = %v, err = %v", cfg.APNs.Enabled(), err)
	}
	if cfg.APNs.Topic != DefaultAppleBundleID || cfg.APNs.KeyID != "ABC123DEFG" || cfg.APNs.TeamID != "6VTPDG2HNK" {
		t.Errorf("APNs = %+v", cfg.APNs)
	}

	// A single-line value with escaped newlines parses too.
	escaped := map[string]string{"APNS_KEY_ID": "ABC123DEFG", "APNS_TEAM_ID": "6VTPDG2HNK", "APNS_TOPIC": "com.example.app",
		"APNS_AUTH_KEY": strings.ReplaceAll(p8, "\n", `\n`)}
	cfg, err = Load(env(escaped))
	if err != nil || !cfg.APNs.Enabled() || cfg.APNs.Topic != "com.example.app" {
		t.Fatalf("escaped: APNs = %+v, err = %v", cfg.APNs, err)
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)
	body := strings.Split(strings.TrimSpace(p8), "\n")[1]
	if strings.Contains(buf.String(), body[:20]) || !strings.Contains(buf.String(), `"apnsEnabled":true`) {
		t.Errorf("log output = %s", buf.String())
	}

	for name, values := range map[string]map[string]string{
		"partial":   {"APNS_KEY_ID": "ABC123DEFG"},
		"not pem":   {"APNS_KEY_ID": "ABC123DEFG", "APNS_TEAM_ID": "6VTPDG2HNK", "APNS_AUTH_KEY": "not a key"},
		"wrong key": {"APNS_KEY_ID": "ABC123DEFG", "APNS_TEAM_ID": "6VTPDG2HNK", "APNS_AUTH_KEY": generatedP8(t, elliptic.P384())},
		"bad ids":   {"APNS_KEY_ID": "short", "APNS_TEAM_ID": "6VTPDG2HNK", "APNS_AUTH_KEY": p8},
	} {
		_, err := Load(env(values))
		if err == nil || !strings.Contains(err.Error(), "APNS_") {
			t.Errorf("%s: Load() error = %v", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "PRIVATE KEY") {
			t.Errorf("%s: error leaks the key: %v", name, err)
		}
	}
}
