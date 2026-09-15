// Command hellofresh imports the account owner's HelloFresh order history into
// DinnerOS. It is an offline tool, not part of the API runtime.
//
// Stages:
//
//	fetch      read data/order-history.json (exported from the owner's signed-in
//	           browser session) and save each ordered recipe's public page data
//	           to data/raw/recipes/<id>.json. Idempotent and resumable.
//
// All output lives under data/, which is git-ignored.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	var err error
	switch os.Args[1] {
	case "fetch":
		err = runFetch(ctx, logger, os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: hellofresh <command> [flags]

commands:
  fetch   save public recipe page data for every recipe in the order history
`)
}

func runFetch(ctx context.Context, logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	historyPath := fs.String("history", "data/order-history.json", "order history exported from the browser")
	outDir := fs.String("out", "data/raw", "directory for raw recipe files")
	delay := fs.Duration("delay", 2500*time.Millisecond, "minimum delay between requests")
	limit := fs.Int("limit", 0, "fetch at most this many new recipes (0 = no limit)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	history, err := LoadHistory(*historyPath)
	if err != nil {
		return err
	}
	recipes := history.UniqueRecipes()
	logger.Info("order history loaded", "weeks", len(history.Weeks), "uniqueRecipes", len(recipes))

	fetcher := NewFetcher(logger)
	fetcher.Delay = *delay
	fetcher.Limit = *limit

	stats, err := fetcher.FetchAll(ctx, recipes, *outDir)
	logger.Info("fetch finished", "fetched", stats.Fetched, "skipped", stats.Skipped, "failed", len(stats.Failures))
	return err
}
