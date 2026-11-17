package config

import (
	"bytes"
	"crypto/elliptic"
	"log/slog"
	"strings"
	"testing"
)

func TestLoadSIWA(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.SIWA.Enabled() {
		t.Fatalf("defaults: enabled = %v, err = %v", cfg.SIWA.Enabled(), err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)
	if !strings.Contains(buf.String(), `"signInWithAppleRevocation":"not configured"`) {
		t.Errorf("log output = %s", buf.String())
	}

	p8 := generatedP8(t, elliptic.P256())
	cfg, err = Load(env(map[string]string{
		"SIWA_KEY_ID": "SIWAKEY123", "SIWA_TEAM_ID": "6VTPDG2HNK",
		"SIWA_PRIVATE_KEY": strings.ReplaceAll(p8, "\n", `\n`),
	}))
	if err != nil || !cfg.SIWA.Enabled() {
		t.Fatalf("configured: enabled = %v, err = %v", cfg.SIWA.Enabled(), err)
	}
	if cfg.SIWA.KeyID != "SIWAKEY123" || cfg.SIWA.TeamID != "6VTPDG2HNK" || cfg.SIWA.ClientID != DefaultAppleBundleID {
		t.Errorf("SIWA = %+v", cfg.SIWA)
	}
	buf.Reset()
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)
	body := strings.Split(strings.TrimSpace(p8), "\n")[1]
	if !strings.Contains(buf.String(), `"signInWithAppleRevocation":"configured"`) ||
		strings.Contains(buf.String(), body[:20]) || strings.Contains(buf.String(), "SIWAKEY123") {
		t.Errorf("log output = %s", buf.String())
	}

	// The client ID follows APPLE_BUNDLE_ID unless SIWA_CLIENT_ID is set.
	cfg, err = Load(env(map[string]string{
		"SIWA_KEY_ID": "SIWAKEY123", "SIWA_TEAM_ID": "6VTPDG2HNK", "SIWA_PRIVATE_KEY": p8,
		"APPLE_BUNDLE_ID": "com.example.app",
	}))
	if err != nil || cfg.SIWA.ClientID != "com.example.app" {
		t.Errorf("bundle default: %+v, %v", cfg.SIWA.ClientID, err)
	}
	cfg, err = Load(env(map[string]string{
		"SIWA_KEY_ID": "SIWAKEY123", "SIWA_TEAM_ID": "6VTPDG2HNK", "SIWA_PRIVATE_KEY": p8,
		"SIWA_CLIENT_ID": "com.example.other",
	}))
	if err != nil || cfg.SIWA.ClientID != "com.example.other" {
		t.Errorf("override: %+v, %v", cfg.SIWA.ClientID, err)
	}
}

func TestLoadSIWARejectsPartialOrBadValues(t *testing.T) {
	p8 := generatedP8(t, elliptic.P256())
	for name, vars := range map[string]map[string]string{
		"key ID only":   {"SIWA_KEY_ID": "SIWAKEY123"},
		"no key":        {"SIWA_KEY_ID": "SIWAKEY123", "SIWA_TEAM_ID": "6VTPDG2HNK"},
		"bad team":      {"SIWA_KEY_ID": "SIWAKEY123", "SIWA_TEAM_ID": "nope", "SIWA_PRIVATE_KEY": p8},
		"not a PEM":     {"SIWA_KEY_ID": "SIWAKEY123", "SIWA_TEAM_ID": "6VTPDG2HNK", "SIWA_PRIVATE_KEY": "not a key"},
		"wrong curve":   {"SIWA_KEY_ID": "SIWAKEY123", "SIWA_TEAM_ID": "6VTPDG2HNK", "SIWA_PRIVATE_KEY": generatedP8(t, elliptic.P384())},
		"bad key ID":    {"SIWA_KEY_ID": "short", "SIWA_TEAM_ID": "6VTPDG2HNK", "SIWA_PRIVATE_KEY": p8},
		"private only":  {"SIWA_PRIVATE_KEY": p8},
		"team ID alone": {"SIWA_TEAM_ID": "6VTPDG2HNK"},
	} {
		_, err := Load(env(vars))
		if err == nil || !strings.Contains(err.Error(), "SIWA_") {
			t.Errorf("%s: err = %v", name, err)
			continue
		}
		if strings.Contains(err.Error(), "PRIVATE KEY-") {
			t.Errorf("%s: error includes the key", name)
		}
	}
}
