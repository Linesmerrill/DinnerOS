// Package config loads API configuration from environment variables.
//
// Every setting has a documented placeholder in the repository's .env.example.
// Load never reads the process environment directly; it receives a getenv
// function so tests can supply values deterministically.
package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"regexp"
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
	// OperatorUserIDs are the users who run DinnerOS and may read data across
	// households, such as store request totals (OPERATOR_USER_IDS, a comma
	// list of user IDs). Empty means nobody.
	OperatorUserIDs []string
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
	// InviteURLBase is the invitation link in emails, before the token. The
	// token is appended as a "#token=" fragment, or directly when the base ends
	// in "=" (the legacy "dinneros://invite?token=" form).
	InviteURLBase string
	// WebsiteURL is the public website, e.g. "https://dinneros.app". When set,
	// the API's home and privacy pages redirect there. Empty serves them here.
	WebsiteURL string

	// AppleTeamID is the Apple Developer Team ID. With AppleBundleID it forms
	// the app ID in apple-app-site-association. Empty disables that file.
	AppleTeamID string
	// AppURLScheme is the iOS app's custom URL scheme. The invitation landing
	// page links to <scheme>://invite?token=... to open the app.
	AppURLScheme string

	// WalmartImpact wraps Walmart cart handoff links in Impact affiliate
	// tracking links when all three IDs are set. Empty (the default) leaves
	// links untracked; no Walmart setting is required.
	WalmartImpact WalmartImpact
	// Kroger connects a household's Kroger-family store account (Fry's by
	// default) to fill its cart directly. Off unless the client ID and
	// secret are set.
	Kroger Kroger
	// Plus is DinnerOS Plus: off (PLUS_ENABLED=false, the default) shows no
	// paywall and opens everything. The RevenueCat webhook needs
	// REVENUECAT_WEBHOOK_SECRET.
	Plus Plus
	// Instacart makes Instacart shopping list pages from a week's list when
	// INSTACART_ENABLED is true and INSTACART_API_KEY is set.
	Instacart Instacart

	// StarterRecipesHouseholdID is the household whose recipes are copied
	// into every new household as a starter library. Empty (the default)
	// disables it.
	StarterRecipesHouseholdID string

	// APNs signs and addresses Apple push notifications. Unset (the default
	// in development) makes push a logged no-op.
	APNs APNs
}

// APNs holds the token-based APNs credentials (docs/deployment.md#push-notifications).
// The one key signs for both the sandbox and production gateways.
type APNs struct {
	KeyID  string
	TeamID string
	// Topic is the app's bundle ID.
	Topic string
	// PrivateKey is the parsed .p8 (ES256) key. Never log it.
	PrivateKey *ecdsa.PrivateKey
}

// Enabled reports whether APNs credentials are configured.
func (a APNs) Enabled() bool { return a.PrivateKey != nil }

// WalmartImpact holds the Impact affiliate IDs for Walmart links
// (docs/shopping-providers.md#credentials). They are identifiers, not
// secrets.
type WalmartImpact struct {
	PublisherID string
	AdID        string
	CampaignID  string
}

// Enabled reports whether affiliate tracking is configured.
func (w WalmartImpact) Enabled() bool { return w.PublisherID != "" }

// Kroger holds the Kroger Public API application
// (docs/shopping-providers.md#kroger-public-api). ClientSecret is a secret:
// never log it.
type Kroger struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the OAuth callback registered with the application.
	RedirectURL string
	// APIBase is the API origin: production, or certification for testing.
	APIBase string
	// Banner is the default Kroger store family ("frys").
	Banner string
	// Banners are the other families to offer (KROGER_BANNERS, a comma list
	// or "all"); a household's family follows the store it picks. Empty
	// offers Banner alone.
	Banners []string
}

// Enabled reports whether the Kroger application is configured.
func (k Kroger) Enabled() bool { return k.ClientID != "" && k.ClientSecret != "" }

// Plus holds DinnerOS Plus settings.
type Plus struct {
	Enabled bool
	// WebhookSecret is the Authorization header value set on the RevenueCat
	// webhook. A secret: never log it. Empty turns the webhook off.
	WebhookSecret string
	// TestHouseholds are household IDs Plus is on for while Enabled is false
	// (PLUS_TEST_HOUSEHOLDS, comma-separated), for trying purchases.
	TestHouseholds []string
}

// Instacart holds the Instacart Developer Platform key. The key is a
// secret: never log it.
type Instacart struct {
	// On is INSTACART_ENABLED: Instacart is offered only when it's true and
	// the key is set. Off by default while there is no approved key.
	On     bool
	APIKey string
	// APIBase is the API origin: production, or development for a
	// development key.
	APIBase string
	// Affiliate says the key is tied to the Impact account, so Instacart
	// adds affiliate tracking to page links and the app shows the
	// disclosure (INSTACART_AFFILIATE=true).
	Affiliate bool
}

// Enabled reports whether Instacart is configured.
func (i Instacart) Enabled() bool { return i.On && i.APIKey != "" }

// DefaultInstacartAPIBase is the Instacart Developer Platform's production
// origin. A development key uses https://connect.dev.instacart.tools.
const DefaultInstacartAPIBase = "https://connect.instacart.com"

// DefaultKrogerAPIBase is the Kroger Public API's production origin.
const DefaultKrogerAPIBase = "https://api.kroger.com"

// Email providers.
const (
	EmailProviderResend = "resend"
	EmailProviderLog    = "log"
)

// DefaultInviteURLBase is the invitation landing page, which the iOS app
// claims as a universal link (see GET /.well-known/apple-app-site-association).
// The token goes in the URL fragment, so it never reaches the server or its
// router logs.
const DefaultInviteURLBase = "https://api.tlps.dev/invite"

// DefaultAppURLScheme matches APP_URL_SCHEME in ios/Config/Shared.xcconfig.
const DefaultAppURLScheme = "dinneros"

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
		slog.String("appleTeamId", c.AppleTeamID),
		slog.String("appURLScheme", c.AppURLScheme),
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
		slog.Bool("walmartAffiliateLinks", c.WalmartImpact.Enabled()),
		slog.Bool("instacart", c.Instacart.Enabled()),
		slog.Bool("plus", c.Plus.Enabled),
		slog.Bool("revenueCatWebhook", c.Plus.WebhookSecret != ""),
		slog.Bool("instacartAffiliateLinks", c.Instacart.Enabled() && c.Instacart.Affiliate),
		slog.String("starterRecipesHouseholdId", c.StarterRecipesHouseholdID),
		slog.Bool("apnsEnabled", c.APNs.Enabled()),
		slog.String("apnsKeyId", c.APNs.KeyID),
		slog.String("apnsTopic", c.APNs.Topic),
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
		Version:         get("APP_VERSION", get("HEROKU_BUILD_COMMIT", get("HEROKU_SLUG_COMMIT", "dev"))),
		MongoDatabase:   get("MONGODB_DATABASE", "dinneros"),
		OperatorUserIDs: commaList(get("OPERATOR_USER_IDS", "")),
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
	errs = append(errs, loadShopping(&cfg, get)...)

	cfg.StarterRecipesHouseholdID = strings.ToLower(get("STARTER_RECIPES_HOUSEHOLD_ID", ""))
	if id := cfg.StarterRecipesHouseholdID; id != "" && !objectIDPattern.MatchString(id) {
		errs = append(errs, fmt.Errorf("STARTER_RECIPES_HOUSEHOLD_ID must be a 24-character hex household ID, got %q", id))
	}
	errs = append(errs, loadAPNs(&cfg, get)...)

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// loadAPNs reads the APNs credentials into cfg. cfg.AppleBundleID must
// already be set. APNS_KEY_ID, APNS_TEAM_ID, and APNS_AUTH_KEY are all set or
// all empty; error messages never include the key.
func loadAPNs(cfg *Config, get func(key, fallback string) string) []error {
	keyID, teamID, rawKey := get("APNS_KEY_ID", ""), get("APNS_TEAM_ID", ""), get("APNS_AUTH_KEY", "")
	if keyID == "" && teamID == "" && rawKey == "" {
		return nil
	}
	var errs []error
	if !appleTeamIDPattern.MatchString(keyID) {
		errs = append(errs, errors.New("APNS_KEY_ID must be the 10-character key ID of the APNs auth key"))
	}
	if !appleTeamIDPattern.MatchString(teamID) {
		errs = append(errs, errors.New("APNS_TEAM_ID must be the 10-character Apple team ID"))
	}
	key, err := ParseAPNsKey(rawKey)
	if err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return append(errs, errors.New("APNS_KEY_ID, APNS_TEAM_ID, and APNS_AUTH_KEY must all be set, or all be empty"))
	}
	cfg.APNs = APNs{KeyID: keyID, TeamID: teamID, Topic: get("APNS_TOPIC", cfg.AppleBundleID), PrivateKey: key}
	return nil
}

// ParseAPNsKey parses the contents of an APNs .p8 file: a PKCS #8 P-256
// private key in PEM. Escaped newlines (a literal backslash-n, as a
// single-line config value may hold them) are accepted.
func ParseAPNsKey(raw string) (*ecdsa.PrivateKey, error) {
	if raw == "" {
		return nil, errors.New("APNS_AUTH_KEY is required: the full contents of the .p8 file")
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(raw, `\n`, "\n")))
	if block == nil {
		return nil, errors.New("APNS_AUTH_KEY must be the PEM contents of the .p8 file, including the BEGIN and END lines")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("APNS_AUTH_KEY is not a valid PKCS #8 private key")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("APNS_AUTH_KEY must be a P-256 (ES256) key")
	}
	return key, nil
}

// objectIDPattern matches a MongoDB ObjectID in hex.
var objectIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

// impactIDPattern matches one numeric Impact identifier.
var impactIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

// loadShopping reads shopping provider settings into cfg. The Walmart Impact
// IDs are all set or all empty.
func loadShopping(cfg *Config, get func(key, fallback string) string) []error {
	cfg.WalmartImpact = WalmartImpact{
		PublisherID: get("WALMART_IMPACT_PUBLISHER_ID", ""),
		AdID:        get("WALMART_IMPACT_AD_ID", ""),
		CampaignID:  get("WALMART_IMPACT_CAMPAIGN_ID", ""),
	}
	var errs []error
	w := cfg.WalmartImpact
	if (w.PublisherID != "" || w.AdID != "" || w.CampaignID != "") &&
		(!impactIDPattern.MatchString(w.PublisherID) || !impactIDPattern.MatchString(w.AdID) || !impactIDPattern.MatchString(w.CampaignID)) {
		cfg.WalmartImpact = WalmartImpact{}
		errs = append(errs, errors.New("WALMART_IMPACT_PUBLISHER_ID, WALMART_IMPACT_AD_ID, and WALMART_IMPACT_CAMPAIGN_ID must all be set to numeric Impact IDs, or all be empty"))
	}

	cfg.Kroger = Kroger{
		ClientID:     strings.TrimSpace(get("KROGER_CLIENT_ID", "")),
		ClientSecret: strings.TrimSpace(get("KROGER_CLIENT_SECRET", "")),
		RedirectURL:  strings.TrimSpace(get("KROGER_REDIRECT_URL", "")),
		APIBase:      strings.TrimRight(strings.TrimSpace(get("KROGER_API_BASE", DefaultKrogerAPIBase)), "/"),
		Banner:       strings.ToLower(strings.TrimSpace(get("KROGER_BANNER", "frys"))),
		Banners:      krogerBanners(get("KROGER_BANNERS", "")),
	}
	k := cfg.Kroger
	switch {
	case k.ClientID == "" && k.ClientSecret == "":
	case k.ClientID == "" || k.ClientSecret == "":
		cfg.Kroger = Kroger{}
		errs = append(errs, errors.New("KROGER_CLIENT_ID and KROGER_CLIENT_SECRET must both be set, or both be empty"))
	case !strings.HasPrefix(k.RedirectURL, "https://") && !(cfg.Env == Development && strings.HasPrefix(k.RedirectURL, "http://")):
		cfg.Kroger = Kroger{}
		errs = append(errs, errors.New("KROGER_REDIRECT_URL must be the https callback registered with the Kroger application, e.g. https://api.tlps.dev/api/v1/shopping/kroger/callback"))
	case !strings.HasPrefix(k.APIBase, "https://"):
		cfg.Kroger = Kroger{}
		errs = append(errs, errors.New("KROGER_API_BASE must be an https origin"))
	}

	cfg.Instacart = Instacart{
		APIKey:  strings.TrimSpace(get("INSTACART_API_KEY", "")),
		APIBase: strings.TrimRight(strings.TrimSpace(get("INSTACART_API_BASE", DefaultInstacartAPIBase)), "/"),
	}
	cfg.Plus.WebhookSecret = strings.TrimSpace(get("REVENUECAT_WEBHOOK_SECRET", ""))
	if s := cfg.Plus.WebhookSecret; s != "" && len(s) < 24 {
		cfg.Plus.WebhookSecret = ""
		errs = append(errs, errors.New("REVENUECAT_WEBHOOK_SECRET must be at least 24 characters (openssl rand -hex 24)"))
	}
	for _, id := range strings.Split(get("PLUS_TEST_HOUSEHOLDS", ""), ",") {
		if id = strings.ToLower(strings.TrimSpace(id)); id == "" {
			continue
		}
		if !objectIDPattern.MatchString(id) {
			errs = append(errs, fmt.Errorf("PLUS_TEST_HOUSEHOLDS must be household IDs separated by commas, got %q", id))
			continue
		}
		cfg.Plus.TestHouseholds = append(cfg.Plus.TestHouseholds, id)
	}
	for _, flag := range []struct {
		name string
		out  *bool
	}{{"INSTACART_ENABLED", &cfg.Instacart.On}, {"INSTACART_AFFILIATE", &cfg.Instacart.Affiliate}, {"PLUS_ENABLED", &cfg.Plus.Enabled}} {
		switch strings.ToLower(strings.TrimSpace(get(flag.name, ""))) {
		case "", "false", "0":
		case "true", "1":
			*flag.out = true
		default:
			errs = append(errs, errors.New(flag.name+" must be true or false"))
		}
	}
	// A local mock of Instacart is http, in development only.
	if cfg.Instacart.Enabled() && !strings.HasPrefix(cfg.Instacart.APIBase, "https://") &&
		!(cfg.Env == Development && strings.HasPrefix(cfg.Instacart.APIBase, "http://localhost")) {
		cfg.Instacart = Instacart{}
		errs = append(errs, errors.New("INSTACART_API_BASE must be an https origin"))
	}
	return errs
}

// loadAuth reads authentication settings into cfg. cfg.Env must already be set.
func loadAuth(cfg *Config, get func(key, fallback string) string) []error {
	var errs []error

	cfg.AppleBundleID = get("APPLE_BUNDLE_ID", DefaultAppleBundleID)
	cfg.AppleTeamID = get("APPLE_TEAM_ID", "")
	if cfg.AppleTeamID != "" && !appleTeamIDPattern.MatchString(cfg.AppleTeamID) {
		errs = append(errs, fmt.Errorf("APPLE_TEAM_ID must be 10 uppercase letters or digits, got %q", cfg.AppleTeamID))
	}
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

	cfg.WebsiteURL = strings.TrimSuffix(get("WEBSITE_URL", ""), "/")
	if cfg.WebsiteURL != "" {
		if u, err := url.Parse(cfg.WebsiteURL); err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			errs = append(errs, fmt.Errorf("WEBSITE_URL must be an https origin such as %q", "https://dinneros.app"))
		}
	}

	cfg.InviteURLBase = get("APP_INVITE_URL_BASE", DefaultInviteURLBase)
	if u, err := url.Parse(cfg.InviteURLBase); err != nil || u.Scheme == "" || unsafeLinkScheme(u.Scheme) {
		errs = append(errs, fmt.Errorf("APP_INVITE_URL_BASE must be an absolute URL such as %q", DefaultInviteURLBase))
	} else if strings.Contains(cfg.InviteURLBase, "#") && !strings.HasSuffix(cfg.InviteURLBase, "=") {
		// The token is appended as "#token=...", so a base can't have its own fragment.
		errs = append(errs, fmt.Errorf("APP_INVITE_URL_BASE must not contain a fragment; use a URL such as %q", DefaultInviteURLBase))
	}

	cfg.AppURLScheme = strings.ToLower(get("APP_URL_SCHEME", DefaultAppURLScheme))
	switch s := cfg.AppURLScheme; {
	case !urlSchemePattern.MatchString(s), unsafeLinkScheme(s), s == "http", s == "https":
		errs = append(errs, fmt.Errorf("APP_URL_SCHEME must be a custom URL scheme such as %q, got %q", DefaultAppURLScheme, s))
	}
	return errs
}

var (
	appleTeamIDPattern = regexp.MustCompile(`^[A-Z0-9]{10}$`)
	// urlSchemePattern is RFC 3986's scheme syntax, lowercased.
	urlSchemePattern = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)
)

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

// krogerBanners reads KROGER_BANNERS: "all" for every known family, or
// comma-separated keys. Unknown keys are dropped by the provider.
func krogerBanners(v string) []string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return nil
	}
	if v == "all" {
		return []string{"all"}
	}
	var out []string
	for _, k := range strings.Split(v, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// commaList splits a comma-separated setting, dropping blanks.
func commaList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
