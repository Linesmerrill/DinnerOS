package config

import (
	"crypto/ecdsa"
	"errors"
	"strings"
)

// SIWA is the Sign in with Apple key the API uses to revoke a user's Apple
// sign-in when they delete their account (decision 750). Off unless
// SIWA_KEY_ID, SIWA_TEAM_ID, and SIWA_PRIVATE_KEY are all set.
type SIWA struct {
	KeyID  string
	TeamID string
	// ClientID is SIWA_CLIENT_ID, by default APPLE_BUNDLE_ID: the audience
	// Apple identity tokens are verified against.
	ClientID string
	// PrivateKey is the parsed .p8 (ES256) key. Never log it.
	PrivateKey *ecdsa.PrivateKey
	// Problem says why a partly set or bad key was left off. Never the key.
	Problem string
}

// Enabled reports whether a Sign in with Apple key is configured.
func (s SIWA) Enabled() bool { return s.PrivateKey != nil }

// logState is what the startup config log shows: never the key or its ID.
func (s SIWA) logState() string {
	if s.Enabled() {
		return "configured"
	}
	if s.Problem != "" {
		return "not configured: " + s.Problem
	}
	return "not configured"
}

// loadSIWA reads SIWA_* into cfg. cfg.AppleBundleID must already be set.
// A partly set or bad key never stops the API: Heroku restarts it after each
// config change, so setting the three values one at a time would otherwise
// take it down. Revocation stays off and the startup log says why, never
// with the key.
func loadSIWA(cfg *Config, get func(key, fallback string) string) []error {
	keyID, teamID, rawKey := get("SIWA_KEY_ID", ""), get("SIWA_TEAM_ID", ""), get("SIWA_PRIVATE_KEY", "")
	if keyID == "" && teamID == "" && rawKey == "" {
		return nil
	}
	var errs []error
	if !appleTeamIDPattern.MatchString(keyID) {
		errs = append(errs, errors.New("SIWA_KEY_ID must be the 10-character key ID of the Sign in with Apple key"))
	}
	if !appleTeamIDPattern.MatchString(teamID) {
		errs = append(errs, errors.New("SIWA_TEAM_ID must be the 10-character Apple team ID"))
	}
	key, err := parseP8Key(rawKey, "SIWA_PRIVATE_KEY")
	if err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		cfg.SIWA = SIWA{Problem: strings.Join(msgs, "; ")}
		return nil
	}
	cfg.SIWA = SIWA{KeyID: keyID, TeamID: teamID, ClientID: get("SIWA_CLIENT_ID", cfg.AppleBundleID), PrivateKey: key}
	return nil
}
