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
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
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
	case "normalize":
		err = runNormalize(logger, os.Args[2:])
	case "variants":
		err = runVariants(logger, os.Args[2:])
	case "cards":
		err = runCards(ctx, logger, os.Args[2:])
	case "capture":
		err = runCapture(logger, os.Args[2:])
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
  fetch       save public recipe page data for every recipe in the order history
  normalize   convert raw recipes into the DinnerOS import format
  variants    list delivered variants that still need an account capture
  cards       fetch and parse printed recipe cards for delivered variants and step-less pages
  capture     save account captures ({deliveredId: recipe} JSON) to data/raw/delivered
`)
}

func runCapture(logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	inPath := fs.String("in", "-", "captures JSON file, or - for stdin")
	rawDir := fs.String("raw", "data/raw", "directory containing raw recipe files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	in := os.Stdin
	if *inPath != "-" {
		f, err := os.Open(*inPath)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	var captures map[string]json.RawMessage
	if err := json.NewDecoder(in).Decode(&captures); err != nil {
		return fmt.Errorf("decode captures: %w", err)
	}
	n, err := SaveAccountCaptures(*rawDir, captures, time.Now())
	if err != nil {
		return err
	}
	logger.Info("account captures saved", "count", n, "dir", filepath.Join(*rawDir, "delivered"))
	return nil
}

func runVariants(logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("variants", flag.ContinueOnError)
	historyPath := fs.String("history", "data/order-history.json", "order history exported from the browser")
	rawDir := fs.String("raw", "data/raw", "directory containing raw recipe files")
	outPath := fs.String("out", "data/variants-pending.json", "pending variant list to write")
	if err := fs.Parse(args); err != nil {
		return err
	}

	history, err := LoadHistory(*historyPath)
	if err != nil {
		return err
	}
	raws, err := LoadRawRecipes(*rawDir)
	if err != nil {
		return err
	}
	pending, err := PendingVariants(raws, history)
	if err != nil {
		return err
	}
	if err := writeJSONAtomic(*outPath, pending); err != nil {
		return err
	}
	logger.Info("pending variants", "count", len(pending), "out", *outPath)
	return nil
}

func runNormalize(logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("normalize", flag.ContinueOnError)
	historyPath := fs.String("history", "data/order-history.json", "order history exported from the browser")
	rawDir := fs.String("raw", "data/raw", "directory containing raw recipe files")
	outPath := fs.String("out", "data/import/recipes.json", "normalized import file to write")
	if err := fs.Parse(args); err != nil {
		return err
	}

	history, err := LoadHistory(*historyPath)
	if err != nil {
		return err
	}
	raws, err := LoadRawRecipes(*rawDir)
	if err != nil {
		return err
	}
	file, err := Normalize(raws, history, time.Now())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		return err
	}
	if err := writeJSONAtomic(*outPath, file); err != nil {
		return err
	}
	logger.Info("normalized", "rawFiles", len(raws), "recipes", len(file.Recipes), "reviewItems", len(file.Review), "out", *outPath)
	return nil
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
