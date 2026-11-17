package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

const testReviewPassword = "plain-test-password-0123"

func TestLoadReviewAccount(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.ReviewAccount.Enabled() || cfg.ReviewAccount.Problem != "" {
		t.Fatalf("defaults: ReviewAccount = %+v, %v; want off with no problem", cfg.ReviewAccount, err)
	}

	cfg, err = Load(env(map[string]string{"REVIEW_ACCOUNT_EMAIL": " Review@Example.TEST ", "REVIEW_ACCOUNT_PASSWORD": " " + testReviewPassword + " "}))
	if err != nil || !cfg.ReviewAccount.Enabled() {
		t.Fatalf("configured: ReviewAccount = %+v, %v; want on", cfg.ReviewAccount, err)
	}
	if cfg.ReviewAccount.Email != "review@example.test" || cfg.ReviewAccount.Password != testReviewPassword {
		t.Errorf("configured: email %q, password trimmed %v", cfg.ReviewAccount.Email, cfg.ReviewAccount.Password == testReviewPassword)
	}

	for name, vars := range map[string]map[string]string{
		"email only":     {"REVIEW_ACCOUNT_EMAIL": "review@example.test"},
		"password only":  {"REVIEW_ACCOUNT_PASSWORD": testReviewPassword},
		"not an email":   {"REVIEW_ACCOUNT_EMAIL": "review", "REVIEW_ACCOUNT_PASSWORD": testReviewPassword},
		"short password": {"REVIEW_ACCOUNT_EMAIL": "review@example.test", "REVIEW_ACCOUNT_PASSWORD": "short"},
	} {
		// A mistyped pair turns review sign-in off; it never stops the API.
		cfg, err := Load(env(vars))
		if err != nil {
			t.Errorf("%s: Load() error = %v; want the API to start", name, err)
			continue
		}
		if cfg.ReviewAccount.Enabled() || cfg.ReviewAccount.Problem == "" || cfg.ReviewAccount.Password != "" {
			t.Errorf("%s: ReviewAccount = %+v; want off, a problem, and no password kept", name, cfg.ReviewAccount)
		}
		if strings.Contains(cfg.ReviewAccount.Problem, testReviewPassword) {
			t.Errorf("%s: problem names the password", name)
		}
	}
}

func TestLogValueSaysReviewAccountIsConfiguredWithoutValues(t *testing.T) {
	cfg, err := Load(env(map[string]string{"REVIEW_ACCOUNT_EMAIL": "review@example.test", "REVIEW_ACCOUNT_PASSWORD": testReviewPassword}))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)
	out := buf.String()
	if strings.Contains(out, testReviewPassword) || strings.Contains(out, "review@example.test") {
		t.Fatalf("log output leaks the review account: %s", out)
	}
	if !strings.Contains(out, `"reviewAccount":true`) {
		t.Errorf("log output doesn't say the review account is configured: %s", out)
	}
}
