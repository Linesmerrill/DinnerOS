// Command compbeta gives every household that exists now Plus on the house, as
// a thank-you for testing the beta (decision 682). It is a dry run unless
// -apply is given, and running it again changes nothing: a household that
// already has a comp keeps it.
//
// Left out, so they still see the paywall and can buy Plus:
//   - the starter recipe library's household (STARTER_RECIPES_HOUSEHOLD_ID),
//     which holds data, not people;
//   - households in PLUS_TEST_HOUSEHOLDS, kept for trying the purchase;
//   - households given with -exclude, like an App Review demo account's.
//
// Households made after it runs, including any App Review account, are never
// comped: it is a one-time grant.
//
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/compbeta          # dry run
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/compbeta -exclude <id>,<id> -apply
//
// It never prints the URI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "compbeta:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("compbeta", flag.ContinueOnError)
	flags.SetOutput(out)
	apply := flags.Bool("apply", false, "grant the comps (default is a dry run)")
	exclude := flags.String("exclude", "", "comma-separated household IDs to leave out, like an App Review demo household")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	uri := strings.TrimSpace(getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	database := strings.TrimSpace(getenv("MONGODB_DATABASE"))
	if database == "" {
		database = "dinneros"
	}
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-compbeta"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.WithoutCancel(ctx)) }()
	if *apply {
		if err := client.EnsureIndexes(ctx, plus.CompIndexes()...); err != nil {
			return err
		}
	}
	cur, err := client.Database().Collection("households").Find(ctx, bson.D{}, options.Find().SetProjection(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return fmt.Errorf("list households: %w", err)
	}
	var docs []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return fmt.Errorf("read households: %w", err)
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID.Hex())
	}
	skip := map[string]string{}
	add := func(list, why string) {
		for _, id := range strings.Split(list, ",") {
			if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
				skip[id] = why
			}
		}
	}
	add(getenv("STARTER_RECIPES_HOUSEHOLD_ID"), "starter recipe library")
	add(getenv("PLUS_TEST_HOUSEHOLDS"), "PLUS_TEST_HOUSEHOLDS")
	add(*exclude, "-exclude")
	res, err := grant(ctx, plus.NewMongoComps(client.Database()), ids, skip, time.Now().UTC(), *apply)
	verb := "would comp"
	if *apply {
		verb = "comped"
	}
	fmt.Fprintf(out, "database %q: %s %d of %d households; %d already had one; %d left out\n",
		database, verb, res.granted, len(ids), res.existing, len(res.skipped))
	for _, s := range res.skipped {
		fmt.Fprintf(out, "  left out %s (%s)\n", s, skip[s])
	}
	return err
}

type result struct {
	granted, existing int
	skipped           []string
}

// grant gives each household in ids the beta comp, except those in skip. A
// dry run counts what it would grant.
func grant(ctx context.Context, store plus.CompStore, ids []string, skip map[string]string, at time.Time, apply bool) (result, error) {
	var res result
	for _, id := range ids {
		if _, ok := skip[id]; ok {
			res.skipped = append(res.skipped, id)
			continue
		}
		if _, ok, err := store.Get(ctx, id); err != nil {
			return res, err
		} else if ok {
			res.existing++
			continue
		}
		if !apply {
			res.granted++
			continue
		}
		ok, err := store.Grant(ctx, plus.Comp{HouseholdID: id, Reason: plus.CompReasonBeta, GrantedAt: at})
		if err != nil {
			return res, err
		}
		if ok {
			res.granted++
		} else {
			res.existing++
		}
	}
	return res, nil
}
