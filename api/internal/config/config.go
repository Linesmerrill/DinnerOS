// Package config loads API configuration from environment variables.
//
// Every setting has a documented placeholder in the repository's .env.example.
// Load never reads the process environment directly; it receives a getenv
// function so tests can supply values deterministically.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
)

// Environment is the deployment environment the API is running in.
type Environment string

// Supported environments.
const (
	Development Environment = "development"
	Production  Environment = "production"
)

// Log output formats.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// Config is the fully validated API configuration.
type Config struct {
	AppName      string
	Env          Environment
	Version      string
	Port         int
	LogLevel     slog.Level
	LogFormat    string
	MaxBodyBytes int64

	MongoURI      string
	MongoDatabase string
}

// IsProduction reports whether the API is running in production.
func (c Config) IsProduction() bool { return c.Env == Production }

// Addr is the TCP listen address for the HTTP server.
func (c Config) Addr() string { return ":" + strconv.Itoa(c.Port) }

// LogValue implements slog.LogValuer. Credentials embedded in connection
// strings are never included.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("appName", c.AppName),
		slog.String("env", string(c.Env)),
		slog.String("version", c.Version),
		slog.Int("port", c.Port),
		slog.String("logLevel", c.LogLevel.String()),
		slog.String("logFormat", c.LogFormat),
		slog.String("mongoURI", RedactURI(c.MongoURI)),
		slog.String("mongoDatabase", c.MongoDatabase),
	)
}

// Load reads and validates configuration. All problems are reported together.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	get := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}

	cfg := Config{
		AppName: get("APP_NAME", "DinnerOS"),
		// Heroku exposes the deployed commit as HEROKU_BUILD_COMMIT (dyno metadata)
		// or HEROKU_SLUG_COMMIT (buildpack builds). APP_VERSION overrides both.
		Version:       get("APP_VERSION", get("HEROKU_BUILD_COMMIT", get("HEROKU_SLUG_COMMIT", "dev"))),
		MongoDatabase: get("MONGODB_DATABASE", "dinneros"),
	}

	switch env := Environment(get("APP_ENV", string(Development))); env {
	case Development, Production:
		cfg.Env = env
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be %q or %q, got %q", Development, Production, env))
	}

	port, err := strconv.Atoi(get("PORT", "8080"))
	if err != nil || port < 1 || port > 65535 {
		errs = append(errs, fmt.Errorf("PORT must be an integer between 1 and 65535, got %q", getenv("PORT")))
	}
	cfg.Port = port

	if err := cfg.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	defaultFormat := LogFormatText
	if cfg.IsProduction() {
		defaultFormat = LogFormatJSON
	}
	switch format := get("LOG_FORMAT", defaultFormat); format {
	case LogFormatText, LogFormatJSON:
		cfg.LogFormat = format
	default:
		errs = append(errs, fmt.Errorf("LOG_FORMAT must be %q or %q, got %q", LogFormatText, LogFormatJSON, format))
	}

	maxBody, err := strconv.ParseInt(get("HTTP_MAX_BODY_BYTES", "1048576"), 10, 64)
	if err != nil || maxBody <= 0 {
		errs = append(errs, fmt.Errorf("HTTP_MAX_BODY_BYTES must be a positive integer, got %q", getenv("HTTP_MAX_BODY_BYTES")))
	}
	cfg.MaxBodyBytes = maxBody

	// Production must be pointed at a real database explicitly; development
	// falls back to the docker-compose MongoDB.
	if cfg.IsProduction() {
		cfg.MongoURI = get("MONGODB_URI", "")
		if cfg.MongoURI == "" {
			errs = append(errs, errors.New("MONGODB_URI is required in production"))
		}
	} else {
		cfg.MongoURI = get("MONGODB_URI", "mongodb://localhost:27017")
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// RedactURI removes any password from a URI so it is safe to log.
func RedactURI(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable>"
	}
	return u.Redacted()
}
