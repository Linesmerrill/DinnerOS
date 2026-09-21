// Command importmealkit runs the meal-kit recipe import worker once and
// exits. Heroku Scheduler runs it every ten minutes
// (docs/meal-kit-import.md#operating-the-worker).
//
// A run claims runnable jobs with an atomic find-and-modify under a lease,
// fetches at most MEAL_KIT_RECIPES_PER_RUN recipe pages per job, hands them to
// the ordinary recipe import pipeline, checkpoints as it goes, and leaves the
// rest for the next run. Two runs that overlap are safe: a job is claimed
// exactly once, and a claimed job's writes are conditional on the lease, so a
// run whose job was canceled (the member stopped it) or taken over stops
// without touching the library.
//
// Without MEAL_KIT_IMPORT_ENABLED=true it logs that the feature is off and
// exits 0, so a scheduled job on a deployment that hasn't enabled it is
// harmless.
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
	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	mealkitsources "github.com/Linesmerrill/DinnerOS/api/internal/mealkit/sources"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/logging"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// runTimeout keeps a stuck run from outliving the next scheduled one. It is
// shorter than mealkit.DefaultLease is long, so a run that is cut off here
// still holds its lease until after it is gone, and the next run takes the
// job over from its checkpoint rather than racing this one.
const runTimeout = 6 * time.Minute

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
	logger := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat).With("command", "importmealkit")
	slog.SetDefault(logger)

	if !cfg.MealKitImport.Active() {
		logger.Info("meal-kit recipe import is off; nothing to do",
			"enabled", cfg.MealKitImport.Enabled)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	db, err := mongodb.Connect(ctx, mongodb.Config{URI: cfg.MongoURI, Database: cfg.MongoDatabase, AppName: cfg.AppName + "-importmealkit"})
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Close(closeCtx)
	}()
	// The server owns index creation, but a worker that runs before the first
	// deploy of this version must not scan the queue without them.
	if err := db.EnsureIndexes(ctx, slices.Concat(mealkit.Indexes(), recipes.Indexes())...); err != nil {
		return err
	}

	worker, err := newWorker(cfg, db, logger)
	if err != nil {
		return err
	}
	started := time.Now()
	report, err := worker.Run(ctx)
	logger.Info("meal-kit import run finished",
		"durationMs", time.Since(started).Milliseconds(),
		"claimed", report.Claimed, "succeeded", report.Succeeded,
		"requeued", report.Requeued, "dead", report.Dead, "gone", report.Gone,
		"recipes", report.Recipes, "recipeFailures", report.Failures)
	return err
}

// newWorker wires the queue, the sources, and the seam into the recipe library
// exactly as cmd/server wires the enqueue side. There is no key to load: this
// worker reads public recipe pages and holds no credential.
func newWorker(cfg config.Config, db *mongodb.Client, logger *slog.Logger) (*mealkit.Worker, error) {
	database := db.Database()
	store := mealkit.NewMongoStore(database)
	srcs := mealkitsources.All(mealkitsources.Options{
		HelloFreshBaseURL: cfg.MealKitImport.HelloFreshBaseURL, Logger: logger,
	})
	notifier := notifications.NewService(notifications.ServiceOptions{
		Store: notifications.NewMongoStore(database), Logger: logger,
	})
	service := mealkit.NewService(mealkit.ServiceOptions{
		Store: store, Enabled: cfg.MealKitImport.Active(), Sources: srcs,
		Notifier: notifier, Logger: logger,
	})
	return mealkit.NewWorker(mealkit.WorkerOptions{
		Store:   store,
		Service: service,
		// The seam into the library: fetched recipes go through the same
		// import the file and HTTP paths use (docs/import-format.md).
		Publisher:     mealkit.ServicePublisher{Service: recipes.NewService(recipes.NewMongoStore(database))},
		Sources:       srcs,
		Notifier:      notifier,
		Logger:        logger,
		RecipesPerRun: cfg.MealKitImport.RecipesPerRun,
	}), nil
}
