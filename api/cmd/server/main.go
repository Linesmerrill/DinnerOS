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

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/applinks"
	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot/baseline"
	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/httpapi"
	"github.com/Linesmerrill/DinnerOS/api/internal/invitations"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/logging"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/ratelimit"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
	"github.com/Linesmerrill/DinnerOS/api/internal/shopping"
	"github.com/Linesmerrill/DinnerOS/api/internal/substitutes"
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
	// Accepting invitations uses the same budget as sign-in: invite codes are
	// 50 bits, so guessing needs far more attempts than this allows.
	inviteAcceptRateBurst = 10
	inviteAcceptRateEvery = 6 * time.Second
	// Creating invitations sends email; limit bursts from one client.
	inviteCreateRateBurst = 20
	inviteCreateRateEvery = 30 * time.Second
	// A recipe import uploads a household's whole order history (10–15 MB), so
	// that one route extends the server's ReadTimeout and WriteTimeout.
	recipeImportTimeout = 5 * time.Minute
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
		households.Indexes(),
		invitations.Indexes(),
		recipes.Indexes(),
		planning.Indexes(),
		pantry.Indexes(),
		notifications.Indexes(),
		substitutes.Indexes(),
		shopping.Indexes(),
		behaviorIndexes(),
		recommendations.Indexes(),
	)...); err != nil {
		return err
	}
	// The curated specialty ingredients ship in the binary; syncing is
	// idempotent and writes only what changed.
	substitutesStore := substitutes.NewMongoStore(db.Database())
	if err := substitutes.EnsureSeed(startupCtx, substitutesStore, logger); err != nil {
		return err
	}
	cancelStartup()
	logger.Info("mongodb connected", "database", cfg.MongoDatabase)

	userService := users.NewService(users.NewMongoStore(db.Database()))
	tokens, err := auth.NewTokenService(auth.NewMongoSessionStore(db.Database()), auth.TokenOptions{
		SigningKey: cfg.AuthTokenSigningKey,
		Logger:     logger,
	})
	if err != nil {
		return err
	}

	authHandler := newAuthHandler(cfg, userService, tokens, logger)
	householdService, householdHandler, invitationHandler := newHouseholdHandlers(cfg, db, userService, tokens, logger)
	recipeService := recipes.NewService(recipes.NewMongoStore(db.Database()))
	// The recipe service is the pantry's view of the global ingredient catalog
	// and of cooked recipes. The pantry decides grocery list statuses, deducts
	// cooked recipes (an events listener), and raises low-stock notifications;
	// reading notifications first runs its time-based low-stock check.
	notificationService := notifications.NewService(notifications.ServiceOptions{
		Store:  notifications.NewMongoStore(db.Database()),
		Logger: logger,
	})
	pantryStore := pantry.NewMongoStore(db.Database())
	pantryService := pantry.NewService(pantryStore, recipeService).WithUsage(pantry.UsageOptions{
		Store:    pantryStore,
		Recipes:  recipeService,
		Notifier: notificationService,
		Logger:   logger,
	})
	notificationService.SetRefresher(pantryService)
	// Specialty ingredients read the catalog, recipe use, and batch stock;
	// grocery lists apply the household's choices, and cook deductions match
	// specialty aliases and packet sizes through them.
	substitutesService := substitutes.NewService(substitutes.ServiceOptions{
		Store:   substitutesStore,
		Catalog: recipeService,
		Recipes: recipeService,
		Pantry:  pantryService,
		Logger:  logger,
	})
	pantryService.SetKeyResolver(substitutesService)
	behavior := newBehavior(db, recipeService, userService, householdService, tokens, logger, pantryService.CookedListener())
	recipeHandler := recipes.NewHandler(recipes.HandlerOptions{
		Service:        recipeService,
		Ratings:        behavior.ratings,
		Events:         behavior.events,
		Authorizer:     householdService,
		Tokens:         tokens,
		Logger:         logger,
		ImportMaxBytes: cfg.RecipeImportMaxBytes,
		ImportTimeout:  recipeImportTimeout,
	})
	planService := planning.NewService(planning.NewMongoStore(db.Database()), recipeService).
		WithPantry(pantryService).WithSpecialties(substitutesService).WithEvents(behavior.events, logger)
	planHandler := planning.NewHandler(planning.HandlerOptions{
		Service:    planService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})
	// Autopilot runs the local baseline provider. A remote provider can
	// replace it behind the same autopilot.RecommendationProvider interface.
	autopilotHandler := recommendations.NewHandler(recommendations.HandlerOptions{
		Service: recommendations.NewService(recommendations.ServiceOptions{
			Store:      recommendations.NewMongoStore(db.Database()),
			Provider:   baseline.New(baseline.Options{}),
			Households: householdService,
			Recipes:    recipeService,
			Ratings:    behavior.ratings,
			Events:     behavior.events,
			Plans:      planService,
			Pantry:     pantryService,
			Logger:     logger,
		}),
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})
	// Shopping hands the week's list (as planning builds it) to a provider and
	// records confirmed orders as pantry purchases. Walmart needs no keys in
	// Phase 8a; the Impact IDs only wrap its links when set.
	shoppingHandler := shopping.NewHandler(shopping.HandlerOptions{
		Service: shopping.NewService(shopping.ServiceOptions{
			Store:     shopping.NewMongoStore(db.Database()),
			Providers: providers.NewRegistry(newWalmartProvider(cfg)),
			Grocery:   planService,
			Catalog:   recipeService,
			Pantry:    pantryService,
			Events:    behavior.events,
			Logger:    logger,
		}),
		Pantry:     pantryService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})
	pantryHandler := pantry.NewHandler(pantry.HandlerOptions{
		Service:    pantryService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})
	specialtyHandler := substitutes.NewHandler(substitutes.HandlerOptions{
		Service:    substitutesService,
		Pantry:     pantryService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})
	notificationHandler := notifications.NewHandler(notifications.HandlerOptions{
		Service:    notificationService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})

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
			WebRoutes: applinks.NewHandler(applinks.Options{
				AppName:       cfg.AppName,
				AppURLScheme:  cfg.AppURLScheme,
				AppleTeamID:   cfg.AppleTeamID,
				AppleBundleID: cfg.AppleBundleID,
			}).Mount,
			APIRoutes: func(r chi.Router) {
				authHandler.Mount(r)
				householdHandler.Mount(r)
				invitationHandler.Mount(r)
				recipeHandler.Mount(r)
				planHandler.Mount(r)
				autopilotHandler.Mount(r)
				pantryHandler.Mount(r)
				specialtyHandler.Mount(r)
				shoppingHandler.Mount(r)
				notificationHandler.Mount(r)
				behavior.ratingHandler.Mount(r)
				behavior.eventHandler.Mount(r)
			},
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

// newAuthHandler wires provider verifiers and the auth HTTP handler.
func newAuthHandler(cfg config.Config, userService *users.Service, tokens *auth.TokenService, logger *slog.Logger) *auth.Handler {
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
	})
}

// newHouseholdHandlers wires the households and invitations modules. The
// household service is also the Authorizer for other household-scoped routes.
func newHouseholdHandlers(cfg config.Config, db *mongodb.Client, userService *users.Service, tokens *auth.TokenService, logger *slog.Logger) (*households.Service, *households.Handler, *invitations.Handler) {
	householdService := households.NewService(households.ServiceOptions{
		Store:  households.NewMongoStore(db.Database()),
		Users:  userService,
		Logger: logger,
	})
	invitationService := invitations.NewService(invitations.ServiceOptions{
		Store:         invitations.NewMongoStore(db.Database()),
		Households:    householdService,
		Users:         userService,
		Email:         newEmailProvider(cfg, logger),
		AcceptURLBase: cfg.InviteURLBase,
		Logger:        logger,
	})

	householdHandler := households.NewHandler(households.HandlerOptions{
		Service: householdService,
		Tokens:  tokens,
		Logger:  logger,
	})
	invitationHandler := invitations.NewHandler(invitations.HandlerOptions{
		Service:         invitationService,
		Authorizer:      householdService,
		Tokens:          tokens,
		Logger:          logger,
		AcceptRateLimit: ratelimit.New(ratelimit.Options{Burst: inviteAcceptRateBurst, Every: inviteAcceptRateEvery}).Middleware,
		CreateRateLimit: ratelimit.New(ratelimit.Options{Burst: inviteCreateRateBurst, Every: inviteCreateRateEvery}).Middleware,
	})
	return householdService, householdHandler, invitationHandler
}

// newWalmartProvider returns the Walmart provider, with Impact affiliate
// links when WALMART_IMPACT_* is configured.
func newWalmartProvider(cfg config.Config) *providers.Walmart {
	opts := providers.WalmartOptions{}
	if w := cfg.WalmartImpact; w.Enabled() {
		opts.Affiliate = &providers.ImpactAffiliate{PublisherID: w.PublisherID, AdID: w.AdID, CampaignID: w.CampaignID}
	}
	return providers.NewWalmart(opts)
}

// newEmailProvider picks the EmailProvider named by EMAIL_PROVIDER. Config
// validation guarantees production uses Resend.
func newEmailProvider(cfg config.Config, logger *slog.Logger) invitations.EmailProvider {
	if cfg.EmailProvider == config.EmailProviderResend {
		return invitations.NewResendProvider(invitations.ResendOptions{
			APIKey:  cfg.ResendAPIKey,
			From:    cfg.EmailFrom,
			AppName: cfg.AppName,
		})
	}
	includeCode := cfg.Env == config.Development
	logger.Warn("EMAIL_PROVIDER=log: invitation emails are logged, not sent", "logsInviteCodes", includeCode)
	return invitations.NewLogEmailProvider(logger, includeCode)
}
