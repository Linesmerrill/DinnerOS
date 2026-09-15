// Command seedspecialties syncs the curated specialty ingredients embedded in
// the API (internal/substitutes/seed) into a database and reports which
// catalog ingredients they flag. The API also syncs on startup, so this is for
// previewing a seed change against a copy of production data, or applying it
// without a deploy. Syncing is idempotent: unchanged specialties aren't
// written, and removed ones are retired, never deleted.
//
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/seedspecialties          # dry run
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/seedspecialties -apply
//
// It reads MONGODB_URI (default mongodb://localhost:27017) and MONGODB_DATABASE
// (default dinneros) from the environment and never prints the URI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/substitutes"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "seedspecialties:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("seedspecialties", flag.ContinueOnError)
	flags.SetOutput(out)
	apply := flags.Bool("apply", false, "write changes (default is a dry run)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	uri := getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	database := getenv("MONGODB_DATABASE")
	if database == "" {
		database = "dinneros"
	}

	connectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, err := mongodb.Connect(connectCtx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-seedspecialties"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.Background()) }()
	if *apply {
		if err := client.EnsureIndexes(ctx, substitutes.Indexes()...); err != nil {
			return err
		}
	}

	rep, err := sync(ctx, client.Database(), *apply, time.Now().UTC().Truncate(time.Millisecond))
	if err != nil {
		return err
	}
	for _, line := range rep.lines {
		fmt.Fprintln(out, line)
	}
	verb := "would"
	if *apply {
		verb = "did"
	}
	fmt.Fprintf(out, "seed version %d: %s create %d, update %d, retire %d; %d unchanged in database %q; %d of %d specialty ingredients match catalog ingredients\n",
		rep.version, verb, len(rep.res.Created), len(rep.res.Updated), len(rep.res.Retired), len(rep.res.Unchanged), database, rep.matched, rep.total)
	return nil
}

type report struct {
	version int
	res     substitutes.SyncResult
	lines   []string
	matched int
	total   int
}

// sync syncs the embedded seed into db and describes each specialty: what the
// sync does with it and which catalog ingredients it flags.
func sync(ctx context.Context, db *mongo.Database, apply bool, now time.Time) (report, error) {
	seed, err := substitutes.LoadSeed()
	if err != nil {
		return report{}, err
	}
	res, err := substitutes.SyncSeed(ctx, substitutes.NewMongoStore(db), seed, apply, now)
	if err != nil {
		return report{}, err
	}
	rep := report{version: seed.Version, res: res, total: len(seed.Specialties)}
	catalog := recipes.NewService(recipes.NewMongoStore(db))
	action := func(id string) string {
		switch {
		case slices.Contains(res.Created, id):
			return "create"
		case slices.Contains(res.Updated, id):
			return "update"
		}
		return "same"
	}
	for _, sp := range seed.Specialties {
		found, err := catalog.IngredientsByKey(ctx, append([]string{sp.Key}, sp.AliasKeys...))
		if err != nil {
			return report{}, fmt.Errorf("look up catalog: %w", err)
		}
		names := make([]string, 0, len(found))
		for _, ing := range found {
			names = append(names, ing.Name)
		}
		slices.Sort(names)
		match := "no catalog ingredient yet"
		if len(names) > 0 {
			rep.matched++
			match = "catalog: " + strings.Join(names, ", ")
		}
		rep.lines = append(rep.lines, fmt.Sprintf("  %-6s  %-32s  %s", action(sp.ID), sp.ID, match))
	}
	for _, id := range res.Retired {
		rep.lines = append(rep.lines, fmt.Sprintf("  %-6s  %s", "retire", id))
	}
	return rep, nil
}
