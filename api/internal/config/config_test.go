package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

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
		"APP_ENV":              "production",
		"APP_NAME":             "Renamed",
		"PORT":                 "5000",
		"MONGODB_URI":          "mongodb+srv://user:secret@cluster.example.net",
		"HEROKU_SLUG_COMMIT":   "abc123",
		"CORS_ALLOWED_ORIGINS": " https://a.example , ,https://b.example",
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
	if got := strings.Join(cfg.CORSAllowedOrigins, "|"); got != "https://a.example|https://b.example" {
		t.Errorf("CORSAllowedOrigins = %q", got)
	}
}

func TestLoadProductionRequiresMongoURI(t *testing.T) {
	_, err := Load(env(map[string]string{"APP_ENV": "production"}))
	if err == nil || !strings.Contains(err.Error(), "MONGODB_URI") {
		t.Fatalf("Load() error = %v, want MONGODB_URI requirement", err)
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
