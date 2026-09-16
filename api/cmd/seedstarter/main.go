// Command seedstarter copies the starter recipe library (the recipes of the
// STARTER_RECIPES_HOUSEHOLD_ID household) into an existing household. The API
// does this in the background for every new household; this command backfills
// households created before the library was configured, or finishes a copy
// that failed. It is a dry run unless -apply is given, and re-running it never
// duplicates recipes. Only recipe content is copied, never order history,
// ratings, plans, pantry, review items, or events.
//
//	MONGODB_URI=... MONGODB_DATABASE=dinneros STARTER_RECIPES_HOUSEHOLD_ID=<sourceId> \
//	  go run ./cmd/seedstarter -household <householdId>          # dry run
//	  go run ./cmd/seedstarter -household <householdId> -apply
//
// -source overrides STARTER_RECIPES_HOUSEHOLD_ID. It reads MONGODB_URI
// (default mongodb://localhost:27017) and MONGODB_DATABASE (default dinneros)
// from the environment and never prints the URI.
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

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "seedstarter:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("seedstarter", flag.ContinueOnError)
	flags.SetOutput(out)
	target := flags.String("household", "", "ID of the existing household to copy the starter recipes into")
	source := flags.String("source", strings.TrimSpace(getenv("STARTER_RECIPES_HOUSEHOLD_ID")), "ID of the starter source household (default STARTER_RECIPES_HOUSEHOLD_ID)")
	apply := flags.Bool("apply", false, "write recipes (default is a dry run)")
	timeout := flags.Duration("timeout", 10*time.Minute, "overall time limit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	*target, *source = strings.ToLower(strings.TrimSpace(*target)), strings.ToLower(strings.TrimSpace(*source))
	if *target == "" || *source == "" {
		flags.Usage()
		return errors.New("-household and a source (-source or STARTER_RECIPES_HOUSEHOLD_ID) are required")
	}
	for name, id := range map[string]string{"-household": *target, "source": *source} {
		if _, err := mongodb.ParseID(id); err != nil {
			return fmt.Errorf("%s must be a 24-character hex household ID", name)
		}
	}
	if *target == *source {
		return errors.New("-household is the starter source household")
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
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-seedstarter"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.WithoutCancel(ctx)) }()

	householdStore := households.NewMongoStore(client.Database())
	for name, id := range map[string]string{"household": *target, "source household": *source} {
		if _, err := householdStore.GetHousehold(ctx, id); err != nil {
			if errors.Is(err, households.ErrNotFound) {
				return fmt.Errorf("%s %s not found in database %q", name, id, database)
			}
			return fmt.Errorf("look up %s: %w", name, err)
		}
	}
	if *apply {
		if err := client.EnsureIndexes(ctx, recipes.Indexes()...); err != nil {
			return err
		}
	}

	started := time.Now()
	res, err := recipes.NewService(recipes.NewMongoStore(client.Database())).CopyStarterRecipes(ctx, *source, *target, *apply)
	verb := "would copy"
	if *apply {
		verb = "copied"
	}
	fmt.Fprintf(out, "household %s in database %q: %s %d of %d starter recipes; %d already present (%s)\n",
		*target, database, verb, res.Copied, res.Source, res.Existing, time.Since(started).Round(time.Millisecond))
	if err != nil {
		return err
	}
	if !*apply && res.Copied > 0 {
		fmt.Fprintln(out, "dry run: re-run with -apply to write")
	}
	return nil
}
