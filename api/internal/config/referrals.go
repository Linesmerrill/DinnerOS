package config

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Referrals are the thresholds a referral must meet before both households
// get their free month (decision 698).
type Referrals struct {
	// QualifyDays is REFERRAL_QUALIFY_DAYS: the setup window after the code.
	QualifyDays int
	// MinDinners and MinRecipes are REFERRAL_MIN_DINNERS and
	// REFERRAL_MIN_RECIPES: the setup.
	MinDinners int
	MinRecipes int
	// MaxPer30Days is REFERRAL_MAX_PER_30_DAYS: one sharer's qualified
	// referrals in any 30 days.
	MaxPer30Days int
}

// Referral defaults.
const (
	DefaultReferralQualifyDays  = 14
	DefaultReferralMinDinners   = 1
	DefaultReferralMinRecipes   = 3
	DefaultReferralMaxPer30Days = 3
)

// DeviceCheck holds the key that signs Apple DeviceCheck requests. It
// defaults to the APNs key (DEVICECHECK_* unset); the key must have
// DeviceCheck enabled in the developer account.
type DeviceCheck struct {
	KeyID  string
	TeamID string
	// PrivateKey is the parsed .p8 (ES256) key. Never log it.
	PrivateKey *ecdsa.PrivateKey
}

// Enabled reports whether a DeviceCheck key is configured.
func (d DeviceCheck) Enabled() bool { return d.PrivateKey != nil }

// LoadDeviceCheck reads only the DeviceCheck key, for tools that need no
// other configuration (cmd/devicecheckprobe).
func LoadDeviceCheck(getenv func(string) string) (DeviceCheck, error) {
	get := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	var cfg Config
	if err := errors.Join(loadReferrals(&cfg, get)...); err != nil {
		return DeviceCheck{}, err
	}
	return cfg.DeviceCheck, nil
}

// loadReferrals reads REFERRAL_* and DEVICECHECK_* into cfg.
func loadReferrals(cfg *Config, get func(key, fallback string) string) []error {
	var errs []error
	ints := []struct {
		name     string
		fallback int
		min      int
		out      *int
	}{
		{"REFERRAL_QUALIFY_DAYS", DefaultReferralQualifyDays, 1, &cfg.Referrals.QualifyDays},
		{"REFERRAL_MIN_DINNERS", DefaultReferralMinDinners, 0, &cfg.Referrals.MinDinners},
		{"REFERRAL_MIN_RECIPES", DefaultReferralMinRecipes, 0, &cfg.Referrals.MinRecipes},
		{"REFERRAL_MAX_PER_30_DAYS", DefaultReferralMaxPer30Days, 1, &cfg.Referrals.MaxPer30Days},
	}
	for _, v := range ints {
		raw := get(v.name, strconv.Itoa(v.fallback))
		n, err := strconv.Atoi(raw)
		if err != nil || n < v.min || n > 1000 {
			errs = append(errs, fmt.Errorf("%s must be a whole number from %d to 1000, got %q", v.name, v.min, raw))
			n = v.fallback
		}
		*v.out = n
	}

	keyID := get("DEVICECHECK_KEY_ID", get("APNS_KEY_ID", ""))
	teamID := get("DEVICECHECK_TEAM_ID", get("APNS_TEAM_ID", ""))
	rawKey := get("DEVICECHECK_PRIVATE_KEY", get("APNS_AUTH_KEY", ""))
	if keyID == "" && teamID == "" && rawKey == "" {
		return errs
	}
	var keyErrs []error
	if !appleTeamIDPattern.MatchString(keyID) {
		keyErrs = append(keyErrs, errors.New("DEVICECHECK_KEY_ID must be the 10-character key ID"))
	}
	if !appleTeamIDPattern.MatchString(teamID) {
		keyErrs = append(keyErrs, errors.New("DEVICECHECK_TEAM_ID must be the 10-character Apple team ID"))
	}
	key, err := ParseAPNsKey(rawKey)
	if err != nil {
		keyErrs = append(keyErrs, errors.New(strings.Replace(err.Error(), "APNS_AUTH_KEY", "DEVICECHECK_PRIVATE_KEY", 1)))
	}
	if len(keyErrs) > 0 {
		return append(append(errs, keyErrs...), errors.New("DEVICECHECK_KEY_ID, DEVICECHECK_TEAM_ID, and DEVICECHECK_PRIVATE_KEY (or the APNS_* they default to) must all be set, or all be empty"))
	}
	cfg.DeviceCheck = DeviceCheck{KeyID: keyID, TeamID: teamID, PrivateKey: key}
	return errs
}
