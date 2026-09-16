package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// testSigningKey is 48 random-looking bytes, base64-encoded.
const testSigningKey = "q83vEjRWeJq83vEjRWeJq83vEjRWeJq83vEjRWeJq83vEjRWeJq83vEjRWeJq83v"

// testResendKey is a fake Resend API key.
const testResendKey = "re_test_0123456789abcdef"

func TestLoadShopping(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.WalmartImpact.Enabled() {
		t.Fatalf("defaults: WalmartImpact = %+v, %v", cfg.WalmartImpact, err)
	}
	all := map[string]string{"WALMART_IMPACT_PUBLISHER_ID": "1234567", "WALMART_IMPACT_AD_ID": "565706", "WALMART_IMPACT_CAMPAIGN_ID": "9383"}
	cfg, err = Load(env(all))
	if err != nil || !cfg.WalmartImpact.Enabled() || cfg.WalmartImpact.CampaignID != "9383" {
		t.Fatalf("configured: WalmartImpact = %+v, %v", cfg.WalmartImpact, err)
	}
	for name, values := range map[string]map[string]string{
		"partial":     {"WALMART_IMPACT_PUBLISHER_ID": "1234567"},
		"non-numeric": {"WALMART_IMPACT_PUBLISHER_ID": "abc", "WALMART_IMPACT_AD_ID": "565706", "WALMART_IMPACT_CAMPAIGN_ID": "9383"},
	} {
		if _, err := Load(env(values)); err == nil || !strings.Contains(err.Error(), "WALMART_IMPACT_") {
			t.Errorf("%s: Load() error = %v", name, err)
		}
	}
}

func TestLoadStarterRecipes(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.StarterRecipesHouseholdID != "" {
		t.Fatalf("defaults: StarterRecipesHouseholdID = %q, %v", cfg.StarterRecipesHouseholdID, err)
	}
	cfg, err = Load(env(map[string]string{"STARTER_RECIPES_HOUSEHOLD_ID": " 6AA940907895FF325B652912 "}))
	if err != nil || cfg.StarterRecipesHouseholdID != "6aa940907895ff325b652912" {
		t.Fatalf("configured: StarterRecipesHouseholdID = %q, %v", cfg.StarterRecipesHouseholdID, err)
	}
	if _, err := Load(env(map[string]string{"STARTER_RECIPES_HOUSEHOLD_ID": "Lines"})); err == nil || !strings.Contains(err.Error(), "STARTER_RECIPES_HOUSEHOLD_ID") {
		t.Errorf("invalid: Load() error = %v", err)
	}
}

func TestLoadEmailDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.EmailProvider != EmailProviderLog || cfg.InviteURLBase != DefaultInviteURLBase || cfg.EmailFrom != "" {
		t.Errorf("EmailProvider = %q InviteURLBase = %q EmailFrom = %q", cfg.EmailProvider, cfg.InviteURLBase, cfg.EmailFrom)
	}

	cfg, err = Load(env(map[string]string{"RESEND_API_KEY": testResendKey, "EMAIL_FROM": "DinnerOS <invites@example.com>"}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.EmailProvider != EmailProviderResend || cfg.ResendAPIKey != testResendKey {
		t.Errorf("with key: EmailProvider = %q, want resend by default", cfg.EmailProvider)
	}
}

func TestLoadEmail(t *testing.T) {
	prod := func(extra map[string]string) map[string]string {
		values := map[string]string{
			"APP_ENV":                "production",
			"MONGODB_URI":            "mongodb+srv://cluster.example.net",
			"AUTH_TOKEN_SIGNING_KEY": testSigningKey,
		}
		for k, v := range extra {
			values[k] = v
		}
		return values
	}
	tests := []struct {
		name         string
		env          map[string]string
		wantErr      string
		wantProvider string
	}{
		{name: "explicit log with key", env: map[string]string{"EMAIL_PROVIDER": "log", "RESEND_API_KEY": testResendKey}, wantProvider: EmailProviderLog},
		{name: "provider is case-insensitive", env: map[string]string{"EMAIL_PROVIDER": "LOG"}, wantProvider: EmailProviderLog},
		{name: "resend without key", env: map[string]string{"EMAIL_PROVIDER": "resend", "EMAIL_FROM": "a@example.com"}, wantErr: "RESEND_API_KEY is required"},
		{name: "resend without from", env: map[string]string{"RESEND_API_KEY": testResendKey}, wantErr: "EMAIL_FROM is required"},
		{name: "unknown provider", env: map[string]string{"EMAIL_PROVIDER": "smtp"}, wantErr: "EMAIL_PROVIDER must be"},
		{name: "invalid from", env: map[string]string{"EMAIL_FROM": "not an address"}, wantErr: "EMAIL_FROM must be"},
		{name: "custom invite URL", env: map[string]string{"APP_INVITE_URL_BASE": "https://example.com/invite?token="}, wantProvider: EmailProviderLog},
		{name: "relative invite URL", env: map[string]string{"APP_INVITE_URL_BASE": "/invite?token="}, wantErr: "APP_INVITE_URL_BASE"},
		{name: "script invite URL", env: map[string]string{"APP_INVITE_URL_BASE": "javascript:alert(1)//"}, wantErr: "APP_INVITE_URL_BASE"},
		{name: "production resend", env: prod(map[string]string{"RESEND_API_KEY": testResendKey, "EMAIL_FROM": "invites@example.com"}), wantProvider: EmailProviderResend},
		{name: "production defaults to log without key", env: prod(nil), wantErr: "EMAIL_PROVIDER must be resend in production"},
		{name: "production explicit log", env: prod(map[string]string{"EMAIL_PROVIDER": "log", "RESEND_API_KEY": testResendKey, "EMAIL_FROM": "a@example.com"}), wantErr: "EMAIL_PROVIDER must be resend in production"},
		{name: "production resend without from", env: prod(map[string]string{"RESEND_API_KEY": testResendKey}), wantErr: "EMAIL_FROM is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(env(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), testResendKey) {
					t.Errorf("error leaks the Resend key: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.EmailProvider != tt.wantProvider {
				t.Errorf("EmailProvider = %q, want %q", cfg.EmailProvider, tt.wantProvider)
			}
		})
	}
}

func TestLogValueOmitsResendKey(t *testing.T) {
	cfg, err := Load(env(map[string]string{"RESEND_API_KEY": testResendKey, "EMAIL_FROM": "DinnerOS <invites@example.com>"}))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)
	out := buf.String()
	if strings.Contains(out, testResendKey) {
		t.Fatalf("log output leaks Resend key: %s", out)
	}
	if !strings.Contains(out, `"emailProvider":"resend"`) {
		t.Errorf("log output missing email provider: %s", out)
	}
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AppName != "DinnerOS" {
		t.Errorf("AppName = %q, want DinnerOS", cfg.AppName)
	}
	if cfg.Env != Development {
		t.Errorf("Env = %q, want %q", cfg.Env, Development)
	}
	if cfg.Port != 8080 || cfg.Addr() != ":8080" {
		t.Errorf("Port = %d Addr = %q, want 8080 :8080", cfg.Port, cfg.Addr())
	}
	if cfg.LogFormat != LogFormatText {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, LogFormatText)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.MongoURI != "mongodb://localhost:27017" || cfg.MongoDatabase != "dinneros" {
		t.Errorf("Mongo = %q/%q, want local defaults", cfg.MongoURI, cfg.MongoDatabase)
	}
	if cfg.MaxBodyBytes != 1<<20 {
		t.Errorf("MaxBodyBytes = %d, want %d", cfg.MaxBodyBytes, 1<<20)
	}
	if cfg.RecipeImportMaxBytes != 32<<20 {
		t.Errorf("RecipeImportMaxBytes = %d, want %d", cfg.RecipeImportMaxBytes, 32<<20)
	}
	if cfg.Version != "dev" {
		t.Errorf("Version = %q, want dev", cfg.Version)
	}
}

func TestLoadProduction(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"APP_ENV":                "production",
		"APP_NAME":               "Renamed",
		"PORT":                   "5000",
		"MONGODB_URI":            "mongodb+srv://user:secret@cluster.example.net",
		"AUTH_TOKEN_SIGNING_KEY": testSigningKey,
		"RESEND_API_KEY":         testResendKey,
		"EMAIL_FROM":             "Renamed <invites@example.com>",
		"HEROKU_BUILD_COMMIT":    "abc123",
		"HEROKU_SLUG_COMMIT":     "ignored-when-build-commit-set",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.IsProduction() {
		t.Error("IsProduction() = false, want true")
	}
	if cfg.LogFormat != LogFormatJSON {
		t.Errorf("LogFormat = %q, want json default in production", cfg.LogFormat)
	}
	if cfg.AppName != "Renamed" || cfg.Port != 5000 || cfg.Version != "abc123" {
		t.Errorf("got AppName=%q Port=%d Version=%q", cfg.AppName, cfg.Port, cfg.Version)
	}
}

func TestLoadProductionRequiresMongoURI(t *testing.T) {
	_, err := Load(env(map[string]string{"APP_ENV": "production"}))
	for _, key := range []string{"MONGODB_URI", "AUTH_TOKEN_SIGNING_KEY"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("Load() error = %v, want %s requirement", err, key)
		}
	}
}

func TestLoadReportsAllInvalidValues(t *testing.T) {
	_, err := Load(env(map[string]string{
		"APP_ENV":             "staging",
		"PORT":                "99999",
		"LOG_LEVEL":           "loud",
		"LOG_FORMAT":          "xml",
		"HTTP_MAX_BODY_BYTES": "-1",

		"RECIPE_IMPORT_MAX_BYTES": "lots",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want validation errors")
	}
	for _, key := range []string{"APP_ENV", "PORT", "LOG_LEVEL", "LOG_FORMAT", "HTTP_MAX_BODY_BYTES", "RECIPE_IMPORT_MAX_BYTES"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q does not mention %s", err, key)
		}
	}
}

func TestLogValueRedactsMongoCredentials(t *testing.T) {
	cfg, err := Load(env(map[string]string{"MONGODB_URI": "mongodb+srv://user:hunter2@cluster.example.net/db"}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)

	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("log output leaked password: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "cluster.example.net") {
		t.Errorf("log output missing host: %s", buf.String())
	}
}

func TestLoadAuthDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppleBundleID != DefaultAppleBundleID || cfg.GoogleClientID != "" {
		t.Errorf("AppleBundleID = %q GoogleClientID = %q", cfg.AppleBundleID, cfg.GoogleClientID)
	}
	if !cfg.AuthSigningKeyEphemeral || len(cfg.AuthTokenSigningKey) < MinSigningKeyBytes {
		t.Errorf("development without a key: ephemeral=%v len=%d, want generated key", cfg.AuthSigningKeyEphemeral, len(cfg.AuthTokenSigningKey))
	}
	other, _ := Load(env(nil))
	if bytes.Equal(cfg.AuthTokenSigningKey, other.AuthTokenSigningKey) {
		t.Error("ephemeral keys are not random")
	}
	if cfg.AuthDevLoginEnabled || cfg.DevLoginEnabled() {
		t.Error("dev login enabled by default")
	}
}

func TestLoadAuthSigningKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantLen int
		wantErr string
	}{
		{name: "std base64 48 bytes", key: testSigningKey, wantLen: 48},
		{name: "raw url base64 32 bytes", key: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", wantLen: 32},
		{name: "too short", key: "c2hvcnQta2V5", wantErr: "at least 32 bytes"},
		{name: "not base64", key: "this is not base64!!", wantErr: "base64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(env(map[string]string{"AUTH_TOKEN_SIGNING_KEY": tt.key}))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), tt.key) {
					t.Errorf("error leaks the key: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if len(cfg.AuthTokenSigningKey) != tt.wantLen || cfg.AuthSigningKeyEphemeral {
				t.Errorf("key len = %d ephemeral = %v, want %d configured", len(cfg.AuthTokenSigningKey), cfg.AuthSigningKeyEphemeral, tt.wantLen)
			}
		})
	}
}

func TestLoadDevLogin(t *testing.T) {
	prod := map[string]string{
		"APP_ENV":                "production",
		"MONGODB_URI":            "mongodb+srv://cluster.example.net",
		"AUTH_TOKEN_SIGNING_KEY": testSigningKey,
		"RESEND_API_KEY":         testResendKey,
		"EMAIL_FROM":             "invites@example.com",
	}

	cfg, err := Load(env(map[string]string{"AUTH_DEV_LOGIN_ENABLED": "true"}))
	if err != nil || !cfg.DevLoginEnabled() {
		t.Errorf("development: DevLoginEnabled = %v, err = %v; want true", cfg.DevLoginEnabled(), err)
	}

	prod["AUTH_DEV_LOGIN_ENABLED"] = "true"
	if _, err := Load(env(prod)); err == nil || !strings.Contains(err.Error(), "AUTH_DEV_LOGIN_ENABLED must not be true in production") {
		t.Errorf("production with dev login: error = %v, want rejection", err)
	}

	prod["AUTH_DEV_LOGIN_ENABLED"] = "false"
	cfg, err = Load(env(prod))
	if err != nil || cfg.DevLoginEnabled() {
		t.Errorf("production: DevLoginEnabled = %v, err = %v", cfg.DevLoginEnabled(), err)
	}

	if _, err := Load(env(map[string]string{"AUTH_DEV_LOGIN_ENABLED": "maybe"})); err == nil || !strings.Contains(err.Error(), "AUTH_DEV_LOGIN_ENABLED") {
		t.Errorf("invalid bool: error = %v", err)
	}
}

func TestLoadAuthProviders(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"APPLE_BUNDLE_ID":  "com.example.app",
		"GOOGLE_CLIENT_ID": "123-abc.apps.googleusercontent.com",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppleBundleID != "com.example.app" || cfg.GoogleClientID != "123-abc.apps.googleusercontent.com" {
		t.Errorf("got %q / %q", cfg.AppleBundleID, cfg.GoogleClientID)
	}
}

func TestLogValueOmitsSigningKey(t *testing.T) {
	cfg, err := Load(env(map[string]string{"AUTH_TOKEN_SIGNING_KEY": testSigningKey}))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)
	out := buf.String()
	if strings.Contains(out, testSigningKey) || strings.Contains(out, string(cfg.AuthTokenSigningKey)) {
		t.Fatalf("log output leaks signing key: %s", out)
	}
	if !strings.Contains(out, `"authSigningKey":"configured"`) {
		t.Errorf("log output missing signing key status: %s", out)
	}
}

func TestLoadAppLinks(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.InviteURLBase != "https://api.tlps.dev/invite" || cfg.AppURLScheme != "dinneros" || cfg.AppleTeamID != "" {
		t.Errorf("defaults: InviteURLBase = %q AppURLScheme = %q AppleTeamID = %q", cfg.InviteURLBase, cfg.AppURLScheme, cfg.AppleTeamID)
	}

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		check   func(Config) bool
	}{
		{name: "team ID", env: map[string]string{"APPLE_TEAM_ID": "ABCDE12345"}, check: func(c Config) bool { return c.AppleTeamID == "ABCDE12345" }},
		{name: "lowercase team ID", env: map[string]string{"APPLE_TEAM_ID": "abcde12345"}, wantErr: "APPLE_TEAM_ID"},
		{name: "short team ID", env: map[string]string{"APPLE_TEAM_ID": "ABC"}, wantErr: "APPLE_TEAM_ID"},
		{name: "scheme is lowercased", env: map[string]string{"APP_URL_SCHEME": "Supper"}, check: func(c Config) bool { return c.AppURLScheme == "supper" }},
		{name: "https scheme", env: map[string]string{"APP_URL_SCHEME": "https"}, wantErr: "APP_URL_SCHEME"},
		{name: "script scheme", env: map[string]string{"APP_URL_SCHEME": "javascript"}, wantErr: "APP_URL_SCHEME"},
		{name: "malformed scheme", env: map[string]string{"APP_URL_SCHEME": "1app://"}, wantErr: "APP_URL_SCHEME"},
		{name: "invite base with query", env: map[string]string{"APP_INVITE_URL_BASE": "https://example.com/invite?utm=email"}, check: func(c Config) bool { return c.InviteURLBase == "https://example.com/invite?utm=email" }},
		{name: "invite base with fragment", env: map[string]string{"APP_INVITE_URL_BASE": "https://example.com/invite#join"}, wantErr: "must not contain a fragment"},
		{name: "legacy scheme prefix", env: map[string]string{"APP_INVITE_URL_BASE": "dinneros://invite?token="}, check: func(c Config) bool { return c.InviteURLBase == "dinneros://invite?token=" }},
		{name: "explicit fragment prefix", env: map[string]string{"APP_INVITE_URL_BASE": "https://example.com/invite#token="}, check: func(Config) bool { return true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(env(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !tt.check(cfg) {
				t.Errorf("unexpected config: %+v", cfg)
			}
		})
	}
}
