// Command publishcatalog backfills the global recipe catalog from a
// household's existing recipes.
//
// Recipes stored before the catalog existed have no catalogKey and were never
// published, so this command gives them their key (by re-saving them
// unchanged) and publishes the ones from a known public source. Recipes a
// household typed or pasted are left alone unless the household shared them.
// It is a dry run unless -apply is given, and re-running it is safe.
//
//	MONGODB_URI=... MONGODB_DATABASE=dinneros \
//	  go run ./cmd/publishcatalog -household <householdId>          # dry run
//	  go run ./cmd/publishcatalog -household <householdId> -apply
//
// It reads MONGODB_URI (default mongodb://localhost:27017) and
// MONGODB_DATABASE (default dinneros) and never prints the URI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "publishcatalog:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("publishcatalog", flag.ContinueOnError)
	flags.SetOutput(out)
	household := flags.String("household", "", "ID of the household whose recipes to key and publish")
	apply := flags.Bool("apply", false, "write recipes and catalog entries (default is a dry run)")
	timeout := flags.Duration("timeout", 10*time.Minute, "overall time limit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	*household = strings.ToLower(strings.TrimSpace(*household))
	if *household == "" {
		flags.Usage()
		return errors.New("-household is required")
	}
	if _, err := mongodb.ParseID(*household); err != nil {
		return errors.New("-household must be a 24-character hex household ID")
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	uri := strings.TrimSpace(getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	database := strings.TrimSpace(getenv("MONGODB_DATABASE"))
	if database == "" {
		database = "dinneros"
	}
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-publishcatalog"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.WithoutCancel(ctx)) }()

	if _, err := households.NewMongoStore(client.Database()).GetHousehold(ctx, *household); err != nil {
		if errors.Is(err, households.ErrNotFound) {
			return fmt.Errorf("household %s not found in database %q", *household, database)
		}
		return fmt.Errorf("look up household: %w", err)
	}
	if *apply {
		if err := client.EnsureIndexes(ctx, append(recipes.Indexes(), catalog.Indexes()...)...); err != nil {
			return err
		}
	}

	catalogService := catalog.NewService(catalog.ServiceOptions{Store: catalog.NewMongoStore(client.Database())})
	service := recipes.NewService(recipes.NewMongoStore(client.Database())).WithCatalog(catalogService)
	catalogService.SetLibrary(service)

	started := time.Now()
	res, err := service.Backfill(ctx, *household, *apply)
	verb := "would key"
	if *apply {
		verb = "keyed"
	}
	fmt.Fprintf(out, "household %s in database %q: read %d recipes; %s %d; %d publishable, %d catalog entries written (%s)\n",
		*household, database, res.Read, verb, res.Keyed, res.Publishable, res.Published, time.Since(started).Round(time.Millisecond))
	if err != nil {
		return err
	}
	if !*apply && (res.Keyed > 0 || res.Publishable > 0) {
		fmt.Fprintln(out, "dry run: re-run with -apply to write")
	}
	return nil
}
