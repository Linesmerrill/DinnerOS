// Package config loads API configuration from environment variables.
//
// Every setting has a documented placeholder in the repository's .env.example.
// Load never reads the process environment directly; it receives a getenv
// function so tests can supply values deterministically.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
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
	// RecipeImportMaxBytes replaces MaxBodyBytes on the recipe import route.
	RecipeImportMaxBytes int64

	MongoURI      string
	MongoDatabase string

	// AuthTokenSigningKey signs DinnerOS access tokens (HS256). Never log it.
	AuthTokenSigningKey []byte
	// AuthSigningKeyEphemeral is true when no key was configured in
	// development and a random per-process key was generated instead.
	AuthSigningKeyEphemeral bool
	// AppleBundleID is the expected audience of Sign in with Apple tokens.
	AppleBundleID string
	// GoogleClientID is the expected audience of Google ID tokens. Empty
	// disables Google sign-in.
	GoogleClientID string
	// AuthDevLoginEnabled requests the unverified development sign-in route.
	// Use DevLoginEnabled, which also requires the development environment.
	AuthDevLoginEnabled bool

	// EmailProvider is EmailProviderResend or EmailProviderLog.
	EmailProvider string
	// ResendAPIKey authenticates to Resend. Never log it.
	ResendAPIKey string
	// EmailFrom is the sender address, e.g. "DinnerOS <invites@example.com>".
	EmailFrom string
	// InviteURLBase is prepended to an invitation token to form the link in
	// invitation emails.
	InviteURLBase string
}

// Email providers.
const (
	EmailProviderResend = "resend"
	EmailProviderLog    = "log"
)

// DefaultInviteURLBase opens the iOS app's custom URL scheme. Universal links
// can replace it later without a code change.
const DefaultInviteURLBase = "dinneros://invite?token="

// DefaultRecipeImportMaxBytes fits a full recipe order history (about 1000
// recipes, 10–15 MB) with room to grow.
const DefaultRecipeImportMaxBytes = 32 << 20

// Default and minimum values for authentication settings.
const (
	DefaultAppleBundleID   = "com.linesmerrill.dinneros"
	MinSigningKeyBytes     = 32
	ephemeralSigningKeyLen = 32
)

// IsProduction reports whether the API is running in production.
func (c Config) IsProduction() bool { return c.Env == Production }

// DevLoginEnabled reports whether POST /api/v1/auth/dev should be mounted.
// It is true only in development with AUTH_DEV_LOGIN_ENABLED=true.
func (c Config) DevLoginEnabled() bool {
	return c.Env == Development && c.AuthDevLoginEnabled
}

// Addr is the TCP listen address for the HTTP server.
func (c Config) Addr() string { return ":" + strconv.Itoa(c.Port) }

// LogValue implements slog.LogValuer. Credentials embedded in connection
// strings, the token signing key, and the Resend API key are never included.
func (c Config) LogValue() slog.Value {
	signingKey := "configured"
	if c.AuthSigningKeyEphemeral {
		signingKey = "ephemeral"
	}
	return slog.GroupValue(
		slog.String("authSigningKey", signingKey),
		slog.String("appleBundleId", c.AppleBundleID),
		slog.Bool("googleSignInEnabled", c.GoogleClientID != ""),
		slog.Bool("devLoginEnabled", c.DevLoginEnabled()),
		slog.String("emailProvider", c.EmailProvider),
		slog.String("emailFrom", c.EmailFrom),
		slog.String("inviteURLBase", c.InviteURLBase),
		slog.String("appName", c.AppName),
		slog.String("env", string(c.Env)),
		slog.String("version", c.Version),
		slog.Int("port", c.Port),
		slog.String("logLevel", c.LogLevel.String()),
		slog.String("logFormat", c.LogFormat),
		slog.Int64("maxBodyBytes", c.MaxBodyBytes),
		slog.Int64("recipeImportMaxBytes", c.RecipeImportMaxBytes),
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

	importMax, err := strconv.ParseInt(get("RECIPE_IMPORT_MAX_BYTES", strconv.Itoa(DefaultRecipeImportMaxBytes)), 10, 64)
	if err != nil || importMax <= 0 {
		errs = append(errs, fmt.Errorf("RECIPE_IMPORT_MAX_BYTES must be a positive integer, got %q", getenv("RECIPE_IMPORT_MAX_BYTES")))
	}
	cfg.RecipeImportMaxBytes = importMax

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

	errs = append(errs, loadAuth(&cfg, get)...)
	errs = append(errs, loadEmail(&cfg, get)...)

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// loadAuth reads authentication settings into cfg. cfg.Env must already be set.
func loadAuth(cfg *Config, get func(key, fallback string) string) []error {
	var errs []error

	cfg.AppleBundleID = get("APPLE_BUNDLE_ID", DefaultAppleBundleID)
	cfg.GoogleClientID = get("GOOGLE_CLIENT_ID", "")

	devLogin, err := strconv.ParseBool(get("AUTH_DEV_LOGIN_ENABLED", "false"))
	if err != nil {
		errs = append(errs, errors.New("AUTH_DEV_LOGIN_ENABLED must be true or false"))
	}
	cfg.AuthDevLoginEnabled = devLogin
	if devLogin && cfg.IsProduction() {
		errs = append(errs, errors.New("AUTH_DEV_LOGIN_ENABLED must not be true in production"))
	}

	// Error messages never include the key itself.
	switch raw := get("AUTH_TOKEN_SIGNING_KEY", ""); {
	case raw == "" && cfg.IsProduction():
		errs = append(errs, errors.New("AUTH_TOKEN_SIGNING_KEY is required in production"))
	case raw == "":
		key := make([]byte, ephemeralSigningKeyLen)
		if _, err := rand.Read(key); err != nil {
			errs = append(errs, fmt.Errorf("AUTH_TOKEN_SIGNING_KEY: generate ephemeral key: %w", err))
		}
		cfg.AuthTokenSigningKey = key
		cfg.AuthSigningKeyEphemeral = true
	default:
		key, err := decodeSigningKey(raw)
		if err != nil {
			errs = append(errs, err)
		}
		cfg.AuthTokenSigningKey = key
	}
	return errs
}

// loadEmail reads email and invitation settings into cfg. cfg.Env must
// already be set. Error messages never include the API key.
func loadEmail(cfg *Config, get func(key, fallback string) string) []error {
	var errs []error

	cfg.ResendAPIKey = get("RESEND_API_KEY", "")
	cfg.EmailFrom = get("EMAIL_FROM", "")
	defaultProvider := EmailProviderLog
	if cfg.ResendAPIKey != "" {
		defaultProvider = EmailProviderResend
	}
	cfg.EmailProvider = strings.ToLower(get("EMAIL_PROVIDER", defaultProvider))

	switch cfg.EmailProvider {
	case EmailProviderResend:
		if cfg.ResendAPIKey == "" {
			errs = append(errs, errors.New("RESEND_API_KEY is required when EMAIL_PROVIDER=resend"))
		}
		if cfg.EmailFrom == "" {
			errs = append(errs, errors.New("EMAIL_FROM is required when EMAIL_PROVIDER=resend"))
		}
	case EmailProviderLog:
		if cfg.IsProduction() {
			errs = append(errs, errors.New("EMAIL_PROVIDER must be resend in production (set RESEND_API_KEY and EMAIL_FROM)"))
		}
	default:
		errs = append(errs, fmt.Errorf("EMAIL_PROVIDER must be %q or %q, got %q", EmailProviderResend, EmailProviderLog, cfg.EmailProvider))
	}

	if cfg.EmailFrom != "" {
		if _, err := mail.ParseAddress(cfg.EmailFrom); err != nil {
			errs = append(errs, fmt.Errorf("EMAIL_FROM must be an address like %q, got %q", "DinnerOS <invites@example.com>", cfg.EmailFrom))
		}
	}

	cfg.InviteURLBase = get("APP_INVITE_URL_BASE", DefaultInviteURLBase)
	if u, err := url.Parse(cfg.InviteURLBase); err != nil || u.Scheme == "" || unsafeLinkScheme(u.Scheme) {
		errs = append(errs, fmt.Errorf("APP_INVITE_URL_BASE must be an absolute URL such as %q", DefaultInviteURLBase))
	}
	return errs
}

func unsafeLinkScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "javascript", "vbscript", "data", "file":
		return true
	}
	return false
}

// decodeSigningKey accepts standard or URL-safe base64, padded or not, and
// requires at least MinSigningKeyBytes of key material.
func decodeSigningKey(raw string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		key, err := enc.DecodeString(raw)
		if err != nil {
			continue
		}
		if len(key) < MinSigningKeyBytes {
			return nil, fmt.Errorf("AUTH_TOKEN_SIGNING_KEY must decode to at least %d bytes (got %d); generate one with `openssl rand -base64 48`", MinSigningKeyBytes, len(key))
		}
		return key, nil
	}
	return nil, errors.New("AUTH_TOKEN_SIGNING_KEY must be base64-encoded; generate one with `openssl rand -base64 48`")
}

// RedactURI removes any password from a URI so it is safe to log.
func RedactURI(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable>"
	}
	return u.Redacted()
}
