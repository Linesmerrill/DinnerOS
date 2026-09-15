package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// testSigningKey is 48 random-looking bytes, base64-encoded.
const testSigningKey = "q83vEjRWeJq83vEjRWeJq83vEjRWeJq83vEjRWeJq83vEjRWeJq83vEjRWeJq83v"

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
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want validation errors")
	}
	for _, key := range []string{"APP_ENV", "PORT", "LOG_LEVEL", "LOG_FORMAT", "HTTP_MAX_BODY_BYTES"} {
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
