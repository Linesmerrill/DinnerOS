package config

import (
	"bytes"
	"crypto/elliptic"
	"log/slog"
	"strings"
	"testing"
)

func TestLoadReferrals(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.Referrals != (Referrals{QualifyDays: 14, MinDinners: 1, MinRecipes: 1, MaxPer30Days: 3}) || cfg.DeviceCheck.Enabled() {
		t.Fatalf("defaults = %+v %v, %v", cfg.Referrals, cfg.DeviceCheck.Enabled(), err)
	}
	cfg, err = Load(env(map[string]string{
		"REFERRAL_QUALIFY_DAYS": "7", "REFERRAL_MIN_DINNERS": "2", "REFERRAL_MIN_RECIPES": "1", "REFERRAL_MAX_PER_30_DAYS": "5",
	}))
	if err != nil || cfg.Referrals != (Referrals{QualifyDays: 7, MinDinners: 2, MinRecipes: 1, MaxPer30Days: 5}) {
		t.Errorf("set = %+v, %v", cfg.Referrals, err)
	}
	for _, bad := range []map[string]string{{"REFERRAL_QUALIFY_DAYS": "0"}, {"REFERRAL_MIN_RECIPES": "three"}, {"REFERRAL_MAX_PER_30_DAYS": "-1"}} {
		if _, err := Load(env(bad)); err == nil || !strings.Contains(err.Error(), "REFERRAL_") {
			t.Errorf("%v: error = %v", bad, err)
		}
	}
}

func TestLoadDeviceCheck(t *testing.T) {
	p8 := generatedP8(t, elliptic.P256())
	// The APNs key is the default.
	cfg, err := Load(env(map[string]string{"APNS_KEY_ID": "ABC123DEFG", "APNS_TEAM_ID": "6VTPDG2HNK", "APNS_AUTH_KEY": p8}))
	if err != nil || !cfg.DeviceCheck.Enabled() || cfg.DeviceCheck.KeyID != "ABC123DEFG" || cfg.DeviceCheck.TeamID != "6VTPDG2HNK" {
		t.Fatalf("from APNs = %+v, %v", cfg.DeviceCheck, err)
	}
	// Its own key wins.
	own := generatedP8(t, elliptic.P256())
	cfg, err = Load(env(map[string]string{
		"APNS_KEY_ID": "ABC123DEFG", "APNS_TEAM_ID": "6VTPDG2HNK", "APNS_AUTH_KEY": p8,
		"DEVICECHECK_KEY_ID": "ZZZ999YYYX", "DEVICECHECK_PRIVATE_KEY": own,
	}))
	if err != nil || cfg.DeviceCheck.KeyID != "ZZZ999YYYX" || cfg.DeviceCheck.TeamID != "6VTPDG2HNK" || cfg.DeviceCheck.PrivateKey.Equal(cfg.APNs.PrivateKey) {
		t.Errorf("own key = %+v, %v", cfg.DeviceCheck.KeyID, err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)
	if body := strings.Split(strings.TrimSpace(own), "\n")[1]; strings.Contains(buf.String(), body[:20]) || !strings.Contains(buf.String(), `"deviceCheckEnabled":true`) {
		t.Errorf("log = %s", buf.String())
	}
	_, err = Load(env(map[string]string{"DEVICECHECK_KEY_ID": "ZZZ999YYYX"}))
	if err == nil || !strings.Contains(err.Error(), "DEVICECHECK_") || strings.Contains(err.Error(), "PRIVATE KEY-") {
		t.Errorf("partial = %v", err)
	}
	_, err = Load(env(map[string]string{"DEVICECHECK_KEY_ID": "ZZZ999YYYX", "DEVICECHECK_TEAM_ID": "6VTPDG2HNK", "DEVICECHECK_PRIVATE_KEY": "nope"}))
	if err == nil || !strings.Contains(err.Error(), "DEVICECHECK_PRIVATE_KEY") {
		t.Errorf("bad key = %v", err)
	}
}
