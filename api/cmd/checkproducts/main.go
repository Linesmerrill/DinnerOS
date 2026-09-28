// Command checkproducts re-checks households' saved Walmart products once and
// exits. Heroku Scheduler runs it daily (docs/deployment.md#product-checks).
//
// Walmart retires and renumbers items, and a saved item ID that no longer
// exists would silently fail to go into the cart. A run reads saved products
// whose last check is more than a day old — never-checked first, then oldest
// first — and checks at most PRODUCT_CHECKS_PER_RUN distinct items (default
// 200), one request at a time, 2.5 s apart plus jitter, with an honest
// User-Agent. A 403 or 429 stops the run at once and pauses every check (this
// sweep's and the API's) for 24 or 6 hours. A product that has gone gets its
// household one notification, which the hourly /sendreminders pushes.
//
// Products shared by several households are checked once per run. Nothing
// about a household is sent to Walmart: the request is the public product
// page.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/logging"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
	"github.com/Linesmerrill/DinnerOS/api/internal/shopping"
)

// runTimeout bounds a run: the default cap takes about ten minutes, and a run
// cut off here loses nothing, because each result is saved as it arrives.
const runTimeout = 45 * time.Minute

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
	logger := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat).With("command", "checkproducts")
	slog.SetDefault(logger)
	perRun, err := perRunCap(os.Getenv("PRODUCT_CHECKS_PER_RUN"))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	db, err := mongodb.Connect(ctx, mongodb.Config{URI: cfg.MongoURI, Database: cfg.MongoDatabase, AppName: cfg.AppName + "-checkproducts"})
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Close(closeCtx)
	}()
	// The server owns index creation, but a run before the first deploy of
	// this version must not scan saved products without the sweep's index.
	if err := db.EnsureIndexes(ctx, slices.Concat(shopping.Indexes(), notifications.Indexes())...); err != nil {
		return err
	}

	database := db.Database()
	checker := shopping.NewSweepChecker()
	service := shopping.NewService(shopping.ServiceOptions{
		Store:     shopping.NewMongoStore(database),
		Providers: providers.NewRegistry(providers.NewWalmart(providers.WalmartOptions{})),
		Notifier: notifications.NewService(notifications.ServiceOptions{
			Store: notifications.NewMongoStore(database), Logger: logger,
		}),
		Checker: checker,
		Logger:  logger,
	})
	started := time.Now()
	report, err := service.SweepProducts(ctx, providers.KeyWalmart, shopping.SweepOptions{Max: perRun})
	logger.Info("product check run finished",
		"durationMs", time.Since(started).Milliseconds(), "cap", perRun, "candidates", report.Candidates,
		"checked", report.Checked, "found", report.Found, "gone", report.Gone, "unknown", report.Unknown,
		"newlyGone", report.NewlyGone, "notified", report.Notified, "requests", checker.Requests(),
		"stopped", report.Stopped, "paused", report.Paused)
	if report.Stopped {
		logger.Warn("Walmart refused or throttled a check; the run stopped and checks are paused")
	}
	return err
}

// perRunCap reads PRODUCT_CHECKS_PER_RUN: empty is the default.
func perRunCap(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return shopping.DefaultSweepMax, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 || n > shopping.MaxSweepMax {
		return 0, fmt.Errorf("PRODUCT_CHECKS_PER_RUN must be an integer between 1 and %d, got %q", shopping.MaxSweepMax, v)
	}
	return n, nil
}
