package config

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

// MinReviewAccountPasswordLength is the shortest REVIEW_ACCOUNT_PASSWORD the
// API accepts. The password is typed by a person, so it isn't huge, but it is
// long enough that guessing it inside the sign-in rate limit is hopeless.
const MinReviewAccountPasswordLength = 16

// ReviewAccount is the one account App Review signs in to with an email and
// a password (REVIEW_ACCOUNT_EMAIL, REVIEW_ACCOUNT_PASSWORD; decision 752).
// Both are secrets in practice: the repo is public, so they live only in the
// server's config, and neither is ever logged.
type ReviewAccount struct {
	// Email is lowercased and trimmed.
	Email string
	// Password is trimmed. Never log it.
	Password string
	// Problem says why a half-set or invalid pair leaves the account off. It
	// names variables, never values. Empty when the pair is fine or unset.
	Problem string
}

// Enabled reports whether review sign-in is on: both variables are set and
// valid.
func (r ReviewAccount) Enabled() bool {
	return r.Email != "" && r.Password != "" && r.Problem == ""
}

// loadReviewAccount reads the review account. A missing or invalid pair
// turns the feature off rather than stopping the API: production must keep
// serving everyone else if the variables are mistyped.
func loadReviewAccount(cfg *Config, get func(key, fallback string) string) {
	email := strings.ToLower(get("REVIEW_ACCOUNT_EMAIL", ""))
	password := get("REVIEW_ACCOUNT_PASSWORD", "")
	r := ReviewAccount{Email: email, Password: password}
	switch {
	case email == "" && password == "":
	case email == "" || password == "":
		r.Problem = "REVIEW_ACCOUNT_EMAIL and REVIEW_ACCOUNT_PASSWORD must both be set; review sign-in is off"
	case !validAddress(email):
		r.Problem = "REVIEW_ACCOUNT_EMAIL is not an email address; review sign-in is off"
	case utf8.RuneCountInString(password) < MinReviewAccountPasswordLength:
		r.Problem = "REVIEW_ACCOUNT_PASSWORD must be at least 16 characters; review sign-in is off"
	}
	if r.Problem != "" {
		r.Password = ""
	}
	cfg.ReviewAccount = r
}

func validAddress(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && len(s) <= 254
}
