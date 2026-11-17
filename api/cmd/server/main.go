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

	"github.com/Linesmerrill/DinnerOS/api/internal/account"
	"github.com/Linesmerrill/DinnerOS/api/internal/applinks"
	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot/baseline"
	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/companions"
	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/cooking"
	"github.com/Linesmerrill/DinnerOS/api/internal/customize"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/httpapi"
	"github.com/Linesmerrill/DinnerOS/api/internal/instacart"
	"github.com/Linesmerrill/DinnerOS/api/internal/invitations"
	"github.com/Linesmerrill/DinnerOS/api/internal/kroger"
	"github.com/Linesmerrill/DinnerOS/api/internal/menu"
	"github.com/Linesmerrill/DinnerOS/api/internal/nextsteps"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/photosearch"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/logging"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/ratelimit"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
	"github.com/Linesmerrill/DinnerOS/api/internal/sameitems"
	"github.com/Linesmerrill/DinnerOS/api/internal/shelflife"
	"github.com/Linesmerrill/DinnerOS/api/internal/shopping"
	"github.com/Linesmerrill/DinnerOS/api/internal/siwa"
	"github.com/Linesmerrill/DinnerOS/api/internal/skips"
	"github.com/Linesmerrill/DinnerOS/api/internal/substitutes"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

const (
	startupTimeout  = 20 * time.Second
	shutdownTimeout = 25 * time.Second // Heroku allows 30s after SIGTERM before SIGKILL.

	jwksFetchTimeout = 5 * time.Second
	// Auth endpoints allow a burst of 10 requests per client IP, refilling one
	// token every 6 seconds.
	authRateEvery = 6 * time.Second
	// Accepting invitations uses the same budget as sign-in: invite codes are
	// 50 bits, so guessing needs far more attempts than this allows.
	inviteAcceptRateBurst = 10
	inviteAcceptRateEvery = 6 * time.Second
	// Creating invitations sends email; limit bursts from one client.
	inviteCreateRateBurst = 20
	inviteCreateRateEvery = 30 * time.Second
	// Checking a referral code as it is typed, and changing it, allow a
	// burst of 30 per user, refilling one a second. Changes are also capped
	// at plus.CodeChangeLimit a month.
	referralCodeRateBurst = 30
	referralCodeRateEvery = time.Second
	// The public invite preview, per address: enough to open a few links,
	// too few to walk the code space.
	invitePreviewRateBurst = 20
	invitePreviewRateEvery = 3 * time.Second
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

	if err := db.EnsureIndexes(startupCtx, indexSets()...); err != nil {
		return err
	}
	// The curated specialty ingredients ship in the binary; syncing is
	// idempotent and writes only what changed.
	substitutesStore := substitutes.NewMongoStore(db.Database())
	if err := substitutes.EnsureSeed(startupCtx, substitutesStore, logger); err != nil {
		return err
	}
	// Shared Recipes groups copies of one recipe by fingerprint (decision
	// 689). Entries stored before it, or under older rules, get theirs here;
	// once all are current this reads one index and writes nothing. A failure
	// is not fatal: an entry without a fingerprint is shown on its own.
	if n, err := catalog.NewMongoStore(db.Database()).BackfillFingerprints(startupCtx); err != nil {
		logger.Warn("shared recipe fingerprint backfill failed", "error", err, "updated", n)
	} else if n > 0 {
		logger.Info("shared recipe fingerprints backfilled", "updated", n)
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

	// Sign in with Apple revocation on account deletion (decision 750).
	appleRevocation, err := newAppleRevocation(cfg, db.Database(), logger)
	if err != nil {
		return err
	}
	authHandler := newAuthHandler(cfg, userService, tokens, appleRevocation, logger)
	// The global recipe catalog is a separate collection with no household in
	// it. Recipes flow one way: an import publishes public-source recipes
	// into it, and a household copies an entry into its own library.
	catalogService := catalog.NewService(catalog.ServiceOptions{Store: catalog.NewMongoStore(db.Database())}).
		WithReports(catalog.NewMongoReports(db.Database())).
		// Hiding a cook and operator reviews (decision 744).
		WithModeration(catalog.NewMongoBlocks(db.Database()), catalog.NewMongoReviews(db.Database())).
		// The rows at the top of Shared Recipes (decisions 710 and 711).
		WithSaves(catalog.NewMongoSaves(db.Database())).
		WithRatings(catalog.NewMongoRatings(db.Database())).
		WithCollections(catalog.NewMongoCollections(db.Database()))
	recipeService := recipes.NewService(recipes.NewMongoStore(db.Database())).WithCatalog(catalogService).
		WithPhotos(recipes.NewMongoStore(db.Database()))
	// Photo search for recipes runs here so the Unsplash key never ships in
	// the app; it stays off until UNSPLASH_ACCESS_KEY is set. Credits are read
	// either way, so photos picked earlier keep theirs.
	recipeService.WithPhotoCredits(recipes.NewMongoStore(db.Database()))
	if cfg.UnsplashAccessKey != "" {
		recipeService.WithPhotoSearch(photosearch.NewUnsplash(photosearch.UnsplashOptions{
			AccessKey: cfg.UnsplashAccessKey, APIBase: cfg.UnsplashAPIBase,
		}))
	} else {
		logger.Info("UNSPLASH_ACCESS_KEY is not set; recipe photo search is off")
	}
	catalogService.SetLibrary(recipeService)
	// New households get a copy of STARTER_RECIPES_HOUSEHOLD_ID's recipes in
	// the background, so friends who sign up don't start with an empty menu.
	starter := recipes.NewStarter(recipes.StarterOptions{
		Service:           recipeService,
		SourceHouseholdID: cfg.StarterRecipesHouseholdID,
		Logger:            logger,
	})
	if !starter.Enabled() {
		logger.Info("STARTER_RECIPES_HOUSEHOLD_ID is not set; new households start with no recipes")
	}
	householdService, householdHandler, invitationHandler := newHouseholdHandlers(cfg, db, userService, tokens, starter, logger)
	// Sharing recipes with other households is each household's choice, off
	// until it opts in; turning it on shares what it already has.
	recipeService.WithSharing(householdService)
	householdService.SetSharingListener(recipeService)
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
	pantryService.SetKeyResolver(substitutesService).SetCookSpecialties(substitutesService)
	behavior := newBehavior(db, recipeService, userService, householdService, tokens, logger, pantryService.CookedListener())
	// A strategy change is a household preference signal, like an Autopilot
	// one, so it is recorded server-side.
	substitutesService.WithEvents(behavior.events)
	// Ingredients the household never wants bought (cilantro, for a household
	// that tastes soap in it), or leaves out of one recipe. Grocery lists hold
	// them back instead of asking anyone to buy them; the catalog links a skip
	// made from free text to the same ingredient reached through the catalog.
	// Cook deductions skip what a recipe is cooked without.
	skipsService := skips.NewService(skips.NewMongoStore(db.Database()), recipeService, logger).WithRecipes(recipeService)
	// Names the household says are one thing to buy ("Chipotle Powder" is
	// "Chipotle Chili Powder"): one line, one product (decision 661). A skip
	// under either name covers both when the dish is cooked.
	sameItemsService := sameitems.NewService(sameitems.NewMongoStore(db.Database()), recipeService)
	recipeLeftOut := sameitems.LeftOut{Skips: skipsService, Same: sameItemsService}
	pantryService.SetLeftOut(recipeLeftOut)
	planService := planning.NewService(planning.NewMongoStore(db.Database()), recipeService).
		WithPantry(pantryService).WithSpecialties(substitutesService).WithSkips(skipsService).WithSameItems(sameItemsService).
		WithEvents(behavior.events, logger).WithWeekStart(householdService).
		WithFreezer(pantryService, householdService, notificationService).WithOutcomes(behavior.events)
	householdService.WithWeekStartListener(planService)
	// Deleting a recipe takes it off this week and later weeks, and Undo puts
	// those meals back (decision 695).
	recipeService.WithPlans(planService)
	// A frozen item needed by one of today's meals is a reminder derived on
	// read, like the pantry's low-stock check and the weekly order reminder.
	notificationService.AddRefresher(planService)
	planHandler := planning.NewHandler(planning.HandlerOptions{
		Service:    planService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
		Outcomes:   behavior.events,
	})
	skipHandler := skips.NewHandler(skips.HandlerOptions{
		Service:    skipsService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})
	plusService := plus.NewService(plus.NewMongoStore(db.Database()), cfg.Plus.Enabled).
		WithReferrals(plus.NewMongoFreeTime(db.Database()), plus.NewMongoReferrals(db.Database())).
		WithTestHouseholds(cfg.Plus.TestHouseholds).
		WithBilling(plus.NewMongoBilling(db.Database())).
		WithComps(plus.NewMongoComps(db.Database()))
	// A referral pays only once the new household is real and set up
	// (decision 698): memberships feed the identity history and DeviceCheck,
	// planned dinners and saved recipes feed the setup, and every one of
	// them can qualify it.
	plusService.WithQualify(plus.QualifyOptions{
		Rules: plus.ReferralRules{
			QualifyDays: cfg.Referrals.QualifyDays, MinDinners: cfg.Referrals.MinDinners,
			MinRecipes: cfg.Referrals.MinRecipes, MaxPer30Days: cfg.Referrals.MaxPer30Days,
		},
		Setup: plus.NewMongoSetup(db.Database()), History: plus.NewMongoHistory(db.Database()),
		Devices: plus.NewMongoDevices(db.Database()), Locks: plus.NewMongoLocks(db.Database()),
		Households: householdService, Identities: userService, Notifier: notificationService,
		DeviceCheck: newDeviceChecker(cfg, logger),
		// Simulators can't make DeviceCheck tokens.
		AllowWithoutToken: cfg.Env == config.Development,
		Logger:            logger,
	})
	householdService.SetMemberListener(plusService)
	planService.WithDinnerListener(plusService)
	recipeService.WithSaveListener(plusService)
	migrateReferrals(ctx, db, plusService, logger)
	requirePlus := plus.Require(plusService, logger)
	plusHandler := plus.NewHandler(plus.HandlerOptions{
		Service:    plusService,
		Authorizer: householdService, Tokens: tokens, WebhookSecret: cfg.Plus.WebhookSecret,
		Operators: cfg.OperatorUserIDs, Logger: logger,
		CodeRateLimit: ratelimit.New(ratelimit.Options{
			Burst: referralCodeRateBurst, Every: referralCodeRateEvery, Message: "Slow down a moment, then try again.",
		}).MiddlewareBy(plus.RateLimitKey),
		InviteRateLimit: ratelimit.New(ratelimit.Options{
			Burst: invitePreviewRateBurst, Every: invitePreviewRateEvery, Message: "Slow down a moment, then try again.",
		}).Middleware,
		MemberName: func(ctx context.Context, userID string) string {
			u, err := userService.GetUser(ctx, userID)
			if err != nil {
				return ""
			}
			return u.DisplayName
		},
	})
	sameItemsHandler := sameitems.NewHandler(sameitems.HandlerOptions{
		Service:    sameItemsService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})
	// Autopilot runs the local baseline provider. A remote provider can
	// replace it behind the same autopilot.RecommendationProvider interface.
	autopilotService := recommendations.NewService(recommendations.ServiceOptions{
		Store:      recommendations.NewMongoStore(db.Database()),
		Provider:   baseline.New(baseline.Options{}),
		Households: householdService,
		Recipes:    recipeService,
		Ratings:    behavior.ratings,
		Events:     behavior.events,
		Plans:      planService,
		Pantry:     pantryService,
		Logger:     logger,
	})
	// Companions (decisions 715-719): things that go with dinner. Autopilot
	// offers them in the week's suggestions and puts the ones set to Add It
	// for Me on the list; the household's add-on recipes become companions
	// the first time they're read, and its pairing rules say what they go
	// with.
	companionService := companions.NewService(companions.Options{
		Store: companions.NewMongoStore(db.Database()), Recipes: recipeService, Hints: autopilotService,
		CanonicalCuisine: recommendations.CanonicalCuisine, CuisineLabel: recommendations.CuisineLabel,
		MealCategory: recommendations.IsMealCategory, Logger: logger,
	})
	autopilotService.WithCompanions(companionService)
	companionHandler := companions.NewHandler(companions.HandlerOptions{
		Service: companionService, Authorizer: householdService, Tokens: tokens, Logger: logger,
	})
	// Grocery items accepted as Autopilot pairings join the week's list (and
	// so the shopping handoff).
	planService.WithExtras(autopilotService)
	// "Try something else" ranks the catalog with the same taste profile
	// Autopilot plans a week from, rather than a second recommender.
	catalogService.SetTaste(autopilotService)
	catalogHandler := catalog.NewHandler(catalog.HandlerOptions{
		Service:    catalogService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
		Credits:    recipeService,
		Operators:  cfg.OperatorUserIDs,
		// Each new report of a shared recipe is emailed (decision 744).
		ReportAlerts:  reportAlerter(cfg),
		ReportAlertTo: cfg.ReportAlertEmail,
	})
	autopilotHandler := recommendations.NewHandler(recommendations.HandlerOptions{
		RequirePlus: requirePlus,
		Service:     autopilotService,
		Authorizer:  householdService,
		Tokens:      tokens,
		Logger:      logger,
	})
	// Meal customizations (swap or double a protein) filter choices by the
	// household's Autopilot restrictions. Grocery lists and cook deductions
	// apply the chosen customizations.
	customizeService := customize.NewService(customize.ServiceOptions{
		Plans:    planService,
		Recipes:  recipeService,
		Catalog:  recipeService,
		Profiles: autopilotService,
		Events:   behavior.events,
		Logger:   logger,
	})
	planService.WithCustomizations(customizeService)
	pantryService.SetCookAdjuster(customizeService)
	// Recommended best-by dates, from the USDA FoodKeeper library and the
	// entries added since.
	shelfLifeService := shelflife.NewService(shelflife.Options{
		Store: shelflife.NewMongoStore(db.Database()), Households: householdService, Logger: logger,
	})
	pantryService.SetShelfLife(shelfLifeService)
	customizeHandler := customize.NewHandler(customize.HandlerOptions{
		Service:    customizeService,
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})
	// Recipe summaries label cook times with the household's Autopilot bands,
	// and cooking instructions read with the household's specialty choices.
	recipeHandler := recipes.NewHandler(recipes.HandlerOptions{
		Service:     recipeService,
		Specialties: substitutesService,
		LeftOut:     recipeLeftOut,
		Swaps:       customizeService,
		// What a meal shares with the rest of its week, to keep some of.
		SaveForLater: planService,
		// What reads wrong in members' recipes is logged to be fixed.
		Findings:       recipes.NewMongoStore(db.Database()),
		Ratings:        behavior.ratings,
		Events:         behavior.events,
		TimeBands:      autopilotService,
		Authorizer:     householdService,
		Tokens:         tokens,
		Logger:         logger,
		ImportMaxBytes: cfg.RecipeImportMaxBytes,
		ImportTimeout:  recipeImportTimeout,
	})
	// The menu composes recipes, ratings, plans, Autopilot preferences, and
	// cooked events into the Menu screen. It stores nothing.
	menuHandler := menu.NewHandler(menu.HandlerOptions{
		Service: menu.NewService(menu.Options{
			Recipes:    recipeService,
			Ratings:    behavior.ratings,
			Plans:      planService,
			Autopilot:  autopilotService,
			Events:     behavior.events,
			Households: householdService,
		}),
		Authorizer: householdService,
		Tokens:     tokens,
		Logger:     logger,
	})
	// Shopping hands the week's list (as planning builds it) to a provider and
	// records confirmed orders as pantry purchases. Walmart needs no keys in
	// Phase 8a; the Impact IDs only wrap its links when set.
	shoppingService := shopping.NewService(shopping.ServiceOptions{
		Store:      shopping.NewMongoStore(db.Database()),
		Providers:  shoppingProviders(cfg, logger),
		Grocery:    planService,
		Catalog:    recipeService,
		Pantry:     pantryService,
		Frozen:     pantryService,
		Freezer:    pantryService,
		Leftovers:  autopilotService,
		Plans:      planService,
		Households: householdService,
		Notifier:   notificationService,
		Events:     behavior.events,
		// A signed-in store account (Kroger) checks who finished the
		// sign-in, and its tokens are sealed with keys derived from the
		// signing key.
		Members:     householdService,
		TokenSecret: cfg.AuthTokenSigningKey,
		Logger:      logger,
	})
	plusService.WithStats(newPlusStats(db.Database(), shoppingService))

	// The weekly order reminder is derived when notifications are read, the
	// way the pantry's low-stock check is: no scheduler and nothing to drift.
	notificationService.AddRefresher(shoppingService)
	// Items a member added by hand join the week's list beside pairings.
	planService.WithExtras(shoppingService)
	shoppingHandler := shopping.NewHandler(shopping.HandlerOptions{
		RequirePlus:  requirePlus,
		Service:      shoppingService,
		Pantry:       pantryService,
		Authorizer:   householdService,
		Households:   householdService,
		Tokens:       tokens,
		AppURLScheme: cfg.AppURLScheme,
		Operators:    cfg.OperatorUserIDs,
		Logger:       logger,
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
	// Device tokens for push. Sending is cmd/sendreminders' job (Heroku
	// Scheduler), so the web dyno needs no APNs credentials to start.
	// DELETE /me: account deletion reaches every module's purge through
	// newAccountService.
	accountHandler := account.NewHandler(account.HandlerOptions{
		Service: newAccountService(db.Database(), householdService, logger),
		Tokens:  tokens,
		Logger:  logger,
		Apple:   appleRevocation,
	})
	// Next steps (decisions 736-739): what the household should do next, as
	// red dots in the app. Read on request from the services above.
	nextStepsHandler := nextsteps.NewHandler(nextsteps.HandlerOptions{
		Service: nextsteps.NewService(nextsteps.Registry(nextsteps.Sources{
			Plus: plusService, Autopilot: autopilotService, Imports: recipeService,
		}), logger),
		Authorizer: householdService, Tokens: tokens, Logger: logger,
	})
	deviceTokenHandler := push.NewHandler(push.HandlerOptions{
		Service: push.NewService(push.NewMongoStore(db.Database()), logger),
		Tokens:  tokens,
		Logger:  logger,
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
				WebsiteURL:    cfg.WebsiteURL,
			}).Mount,
			APIRoutes: func(r chi.Router) {
				authHandler.Mount(r)
				householdHandler.Mount(r)
				invitationHandler.Mount(r)
				recipeHandler.Mount(r)
				companionHandler.Mount(r)
				catalogHandler.Mount(r)
				planHandler.Mount(r)
				skipHandler.Mount(r)
				sameItemsHandler.Mount(r)
				plusHandler.Mount(r)
				customizeHandler.Mount(r)
				autopilotHandler.Mount(r)
				menuHandler.Mount(r)
				pantryHandler.Mount(r)
				specialtyHandler.Mount(r)
				shoppingHandler.Mount(r)
				notificationHandler.Mount(r)
				deviceTokenHandler.Mount(r)
				accountHandler.Mount(r)
				behavior.ratingHandler.Mount(r)
				cooking.NewHandler(cooking.NewStore(db.Database()), householdService, tokens, logger).Mount(r)
				shelflife.NewHandler(shelfLifeService, householdService, tokens, logger).Mount(r)
				behavior.eventHandler.Mount(r)
				nextStepsHandler.Mount(r)
				// UI tests give their own new household Plus (decision 738).
				if cfg.DevLoginEnabled() {
					plus.NewDevCompHandler(plus.NewMongoComps(db.Database()), householdService, tokens, logger).Mount(r)
				}
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
	// Referrals that expire, wait for a sharer's 30-day slot, or met setup
	// with no event are handled here: at start and every hour, well inside
	// the 24 hours the copy promises. Every step is claimed, so two dynos
	// sweeping at once pay nothing twice.
	go plusService.RunSweeps(ctx, plus.SweepEvery)

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
	// A copy cut off here is logged; cmd/seedstarter finishes it.
	if err := starter.Wait(shutdownCtx); err != nil {
		logger.Warn("starter recipe copies still running at shutdown", "error", err)
	}
	logger.Info("api stopped")
	return nil
}

// indexSets returns every module's indexes. Each domain package registers its
// IndexSets here as it is implemented.
func indexSets() []mongodb.IndexSet {
	return slices.Concat(
		users.Indexes(),
		auth.Indexes(),
		siwa.Indexes(),
		cooking.Indexes(),
		shelflife.Indexes(),
		households.Indexes(),
		invitations.Indexes(),
		recipes.Indexes(),
		catalog.Indexes(),
		catalog.ReportIndexes(),
		catalog.ModerationIndexes(),
		catalog.SaveIndexes(),
		planning.Indexes(),
		pantry.Indexes(),
		notifications.Indexes(),
		push.Indexes(),
		substitutes.Indexes(),
		shopping.Indexes(),
		skips.Indexes(),
		sameitems.Indexes(),
		companions.Indexes(),
		plus.Indexes(),
		plus.ReferralIndexes(),
		plus.BillingIndexes(),
		plus.CompIndexes(),
		plus.QualifyIndexes(),
		behaviorIndexes(),
		recommendations.Indexes(),
	)
}

// newAuthHandler wires provider verifiers and the auth HTTP handler.
func newAuthHandler(cfg config.Config, userService *users.Service, tokens *auth.TokenService, appleCodes auth.AppleCodeSaver, logger *slog.Logger) *auth.Handler {
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
		RateLimit:       ratelimit.New(ratelimit.Options{Burst: cfg.AuthRateBurst, Every: authRateEvery}).Middleware,
		DevLoginEnabled: cfg.DevLoginEnabled(),
		AppleCodes:      appleCodes,
	})
}

// newHouseholdHandlers wires the households and invitations modules. The
// household service is also the Authorizer for other household-scoped routes.
func newHouseholdHandlers(cfg config.Config, db *mongodb.Client, userService *users.Service, tokens *auth.TokenService, starter *recipes.Starter, logger *slog.Logger) (*households.Service, *households.Handler, *invitations.Handler) {
	householdService := households.NewService(households.ServiceOptions{
		Store:     households.NewMongoStore(db.Database()),
		Users:     userService,
		OnCreated: starter,
		Logger:    logger,
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

// shoppingProviders returns the enabled shopping providers: Walmart always,
// and the Kroger family (Fry's by default) when KROGER_CLIENT_ID and
// KROGER_CLIENT_SECRET are set, and Instacart when INSTACART_API_KEY is.
func shoppingProviders(cfg config.Config, logger *slog.Logger) *providers.Registry {
	ps := []providers.GroceryProvider{newWalmartProvider(cfg)}
	if k := cfg.Kroger; k.Enabled() {
		banners := k.Banners
		if slices.Contains(banners, "all") {
			banners = providers.KrogerBannerKeys()
		}
		p := kroger.New(kroger.Options{
			ClientID: k.ClientID, ClientSecret: k.ClientSecret, RedirectURL: k.RedirectURL, APIBase: k.APIBase,
			Banner: k.Banner, Banners: banners,
		})
		ps = append(ps, p)
		var keys []string
		for _, b := range p.Banners() {
			keys = append(keys, b.Key)
		}
		logger.Info("shopping provider enabled", "provider", providers.KeyKroger, "banner", k.Banner, "banners", keys)
	}
	if i := cfg.Instacart; i.Enabled() {
		ps = append(ps, instacart.New(instacart.Options{APIKey: i.APIKey, APIBase: i.APIBase, Affiliate: i.Affiliate}))
		logger.Info("shopping provider enabled", "provider", providers.KeyInstacart, "affiliate", i.Affiliate)
	}
	return providers.NewRegistry(ps...)
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

// reportAlerter sends report emails through Resend, or is nil when email
// isn't sent through Resend or no REPORT_ALERT_EMAIL is set.
func reportAlerter(cfg config.Config) catalog.ReportAlerter {
	if cfg.EmailProvider != config.EmailProviderResend || cfg.ReportAlertEmail == "" {
		return nil
	}
	return invitations.NewResendProvider(invitations.ResendOptions{
		APIKey: cfg.ResendAPIKey, From: cfg.EmailFrom, AppName: cfg.AppName,
	})
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
