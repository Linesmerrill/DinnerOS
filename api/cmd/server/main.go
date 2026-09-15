// Command server runs the DinnerOS HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/httpapi"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/logging"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/ratelimit"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

const (
	startupTimeout  = 20 * time.Second
	shutdownTimeout = 25 * time.Second // Heroku allows 30s after SIGTERM before SIGKILL.

	jwksFetchTimeout = 5 * time.Second
	// Auth endpoints allow a burst of 10 requests per client IP, refilling one
	// token every 6 seconds.
	authRateBurst = 10
	authRateEvery = 6 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	logger := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("api starting", "config", cfg)
	if cfg.AuthSigningKeyEphemeral {
		logger.Warn("AUTH_TOKEN_SIGNING_KEY is not set; using an ephemeral development key, so sessions end when the process restarts")
	}

	// Fail fast: without its database the API cannot serve anything useful, and
	// a crashing dyno is more visible than a silently degraded one.
	startupCtx, cancelStartup := context.WithTimeout(ctx, startupTimeout)
	defer cancelStartup()

	db, err := mongodb.Connect(startupCtx, mongodb.Config{
		URI:      cfg.MongoURI,
		Database: cfg.MongoDatabase,
		AppName:  cfg.AppName,
	})
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(closeCtx); err != nil {
			logger.Warn("mongodb disconnect failed", "error", err)
		}
	}()

	// Each domain package registers its IndexSets here as it is implemented.
	if err := db.EnsureIndexes(startupCtx, slices.Concat(
		users.Indexes(),
		auth.Indexes(),
	)...); err != nil {
		return err
	}
	cancelStartup()
	logger.Info("mongodb connected", "database", cfg.MongoDatabase)

	authHandler, err := newAuthHandler(cfg, db, logger)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr: cfg.Addr(),
		Handler: httpapi.NewRouter(httpapi.Options{
			Logger:       logger,
			AppName:      cfg.AppName,
			Version:      cfg.Version,
			MaxBodyBytes: cfg.MaxBodyBytes,
			ReadinessChecks: []httpapi.ReadinessCheck{
				{Name: "mongodb", Check: db.Ping},
			},
			APIRoutes: authHandler.Mount,
		}),
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", srv.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		logger.Info("api shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("api stopped")
	return nil
}

// newAuthHandler wires users, sessions, provider verifiers, and the auth HTTP
// handler.
func newAuthHandler(cfg config.Config, db *mongodb.Client, logger *slog.Logger) (*auth.Handler, error) {
	userService := users.NewService(users.NewMongoStore(db.Database()))

	tokens, err := auth.NewTokenService(auth.NewMongoSessionStore(db.Database()), auth.TokenOptions{
		SigningKey: cfg.AuthTokenSigningKey,
		Logger:     logger,
	})
	if err != nil {
		return nil, err
	}

	jwksHTTP := &http.Client{Timeout: jwksFetchTimeout}
	verifiers := map[users.Provider]auth.IdentityVerifier{
		users.ProviderApple: auth.NewAppleVerifier(cfg.AppleBundleID, auth.VerifierOptions{
			Keys: auth.NewJWKSClient(auth.AppleJWKSURL, auth.JWKSOptions{HTTPClient: jwksHTTP}),
		}),
	}
	if cfg.GoogleClientID != "" {
		verifiers[users.ProviderGoogle] = auth.NewGoogleVerifier(cfg.GoogleClientID, auth.VerifierOptions{
			Keys: auth.NewJWKSClient(auth.GoogleJWKSURL, auth.JWKSOptions{HTTPClient: jwksHTTP}),
		})
	} else {
		logger.Warn("GOOGLE_CLIENT_ID is not set; Google sign-in is disabled")
	}

	if cfg.DevLoginEnabled() {
		logger.Warn("development login enabled: POST /api/v1/auth/dev accepts unverified identities")
	}

	return auth.NewHandler(auth.HandlerOptions{
		Service: auth.NewService(auth.ServiceOptions{
			Verifiers: verifiers,
			Users:     userService,
			Tokens:    tokens,
			Logger:    logger,
		}),
		Tokens:          tokens,
		Logger:          logger,
		RateLimit:       ratelimit.New(ratelimit.Options{Burst: authRateBurst, Every: authRateEvery}).Middleware,
		DevLoginEnabled: cfg.DevLoginEnabled(),
	}), nil
}
