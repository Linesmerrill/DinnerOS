// Command sendreminders runs the push sweep once and exits. Heroku Scheduler
// runs it hourly (docs/deployment.md#push-notifications).
//
// Reminders are derived when notifications are read, so a closed app would
// never produce one. For every household the sweep runs the same refreshers a
// notification read runs — the pantry's low-stock check, the thaw reminder for
// today's frozen ingredients, and the grocery order reminder — and then pushes
// each pending notification to the devices of the
// members who haven't read it. A notification is claimed before it is sent,
// so re-running (or two overlapping runs) never sends it twice.
//
// Without APNS_* configuration it still refreshes, logs that push is off, and
// leaves notifications pending.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/logging"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/shopping"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// runTimeout keeps a stuck run from overlapping the next scheduled one for
// long; a claimed push is never re-sent either way.
const runTimeout = 15 * time.Minute

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
	logger := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat).With("command", "sendreminders")
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	db, err := mongodb.Connect(ctx, mongodb.Config{URI: cfg.MongoURI, Database: cfg.MongoDatabase, AppName: cfg.AppName + "-sendreminders"})
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Close(closeCtx)
	}()
	// The server owns index creation, but a sweep that runs before the first
	// deploy of this version must not scan device_tokens without them.
	if err := db.EnsureIndexes(ctx, slices.Concat(notifications.Indexes(), push.Indexes())...); err != nil {
		return err
	}

	sender, err := newSender(cfg, logger)
	if err != nil {
		return err
	}
	sweeper := newSweeper(db, sender, logger)
	started := time.Now()
	report, err := sweeper.Run(ctx)
	logger.Info("sweep finished",
		"durationMs", time.Since(started).Milliseconds(), "households", report.Households,
		"sent", report.Sent, "skipped", report.Skipped, "failed", report.Failed, "deferred", report.Deferred,
		"deliveries", report.Deliveries, "tokensRemoved", report.TokensRemoved, "pushEnabled", sender != nil)
	return err
}

// newSender returns the APNs client, or nil when APNs isn't configured.
func newSender(cfg config.Config, logger *slog.Logger) (push.Sender, error) {
	if !cfg.APNs.Enabled() {
		logger.Warn("APNS_KEY_ID, APNS_TEAM_ID, and APNS_AUTH_KEY are not set; push is a logged no-op")
		return nil, nil
	}
	return push.NewAPNsClient(push.APNsOptions{
		KeyID: cfg.APNs.KeyID, TeamID: cfg.APNs.TeamID, Topic: cfg.APNs.Topic, PrivateKey: cfg.APNs.PrivateKey,
	})
}

// newSweeper wires the refreshers exactly as cmd/server wires them for
// notification reads: the pantry's low-stock check, the thaw reminder, then
// the order reminder. Refresh uses only the stores, the recipe catalog,
// households, and the notifier, so nothing request-scoped is needed.
func newSweeper(db *mongodb.Client, sender push.Sender, logger *slog.Logger) *push.Sweeper {
	database := db.Database()
	notificationStore := notifications.NewMongoStore(database)
	notificationService := notifications.NewService(notifications.ServiceOptions{Store: notificationStore, Logger: logger})
	householdStore := households.NewMongoStore(database)
	householdService := households.NewService(households.ServiceOptions{
		Store: householdStore, Users: users.NewService(users.NewMongoStore(database)), Logger: logger,
	})
	recipeService := recipes.NewService(recipes.NewMongoStore(database))
	pantryStore := pantry.NewMongoStore(database)
	pantryService := pantry.NewService(pantryStore, recipeService).WithUsage(pantry.UsageOptions{
		Store: pantryStore, Recipes: recipeService, Notifier: notificationService, Logger: logger,
	})
	shoppingService := shopping.NewService(shopping.ServiceOptions{
		Store: shopping.NewMongoStore(database), Households: householdService, Notifier: notificationService, Logger: logger,
	})
	// The thaw reminder needs the week's plan, its recipes, and the freezer;
	// it builds no grocery list, so the planner's pantry, specialty, and skip
	// sources are deliberately absent.
	planService := planning.NewService(planning.NewMongoStore(database), recipeService).
		WithWeekStart(householdService).
		WithFreezer(pantryService, householdService, notificationService)
	notificationService.SetRefresher(pantryService)
	notificationService.AddRefresher(planService)
	notificationService.AddRefresher(shoppingService)
	return push.NewSweeper(push.SweepOptions{
		Households: householdStore,
		Refresher:  notificationService,
		Outbox:     notificationStore,
		Tokens:     push.NewMongoStore(database),
		// A reminder that waited out the night isn't pushed once the week was
		// ordered or the item restocked.
		Relevance: []push.Relevance{shoppingService, pantryService, planService},
		Sender:    sender,
		Logger:    logger,
	})
}
