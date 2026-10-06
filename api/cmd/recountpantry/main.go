// Command recountpantry catches a household's pantry up with the meals it
// cooked, after the way cooking is counted improves (decision 630). Each
// recorded meal is replayed with today's matching and only what its record
// missed is deducted (pantry.Service.RecountCooked); a cooked meal with no
// record at all is deducted as the server would (ApplyCooked). An item a
// person set the amount of after a meal already accounts for it and is left
// alone. A meal cooked with a customization (a swapped protein) is reported
// and skipped: this command doesn't load customizations, and won't guess.
//
//	MONGODB_URI=... go run ./cmd/recountpantry -household <id> -since 2026-09-27          # dry run
//	MONGODB_URI=... go run ./cmd/recountpantry -household <id> -since 2026-09-27 -apply
//
// It reads MONGODB_URI (default mongodb://localhost:27017) and MONGODB_DATABASE
// (default dinneros) from the environment and never prints the URI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/skips"
	"github.com/Linesmerrill/DinnerOS/api/internal/substitutes"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "recountpantry:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("recountpantry", flag.ContinueOnError)
	flags.SetOutput(out)
	household := flags.String("household", "", "household ID (required)")
	sinceText := flags.String("since", "", "first day of meals to recount, YYYY-MM-DD (required)")
	apply := flags.Bool("apply", false, "write changes (default is a dry run)")
	oldSwapText := flags.String("old-texmex-before", "", "meals cooked before this RFC 3339 time used the old Tex-Mex Paste swap (1 tsp base, 5 tsp tomato paste)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	since, err := time.Parse(time.DateOnly, *sinceText)
	if *household == "" || err != nil {
		return fmt.Errorf("-household and -since YYYY-MM-DD are required")
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
	client, err := mongodb.Connect(connectCtx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-recountpantry"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.Background()) }()
	var oldSwap time.Time
	if *oldSwapText != "" {
		if oldSwap, err = time.Parse(time.RFC3339, *oldSwapText); err != nil {
			return fmt.Errorf("-old-texmex-before: %w", err)
		}
	}
	return recount(ctx, client.Database(), *household, since, oldSwap, *apply, out)
}

func recount(ctx context.Context, db *mongo.Database, householdID string, since, oldSwap time.Time, apply bool, out io.Writer) error {
	logger := slog.New(slog.DiscardHandler)
	catalogService := catalog.NewService(catalog.ServiceOptions{Store: catalog.NewMongoStore(db)})
	recipeService := recipes.NewService(recipes.NewMongoStore(db)).WithCatalog(catalogService)
	catalogService.SetLibrary(recipeService)
	pantryStore := pantry.NewMongoStore(db)
	svc := pantry.NewService(pantryStore, recipeService).WithUsage(pantry.UsageOptions{Store: pantryStore, Recipes: recipeService, Logger: logger})
	subs := substitutes.NewService(substitutes.ServiceOptions{
		Store: substitutes.NewMongoStore(db), Catalog: recipeService, Recipes: recipeService, Pantry: svc, Logger: logger,
	})
	swaps := &asCooked{inner: subs}
	svc.SetKeyResolver(subs).SetCookSpecialties(swaps)
	svc.SetLeftOut(skips.NewService(skips.NewMongoStore(db), recipeService, logger).WithRecipes(recipeService))

	customized, err := customizedEntries(ctx, db, householdID)
	if err != nil {
		return err
	}
	verb := "would deduct"
	if apply {
		verb = "deducted"
	}

	usages, err := pantryStore.ListCookUsageSince(ctx, householdID, since)
	if err != nil {
		return err
	}
	recorded := map[string]bool{}
	for _, u := range usages {
		recorded[u.SourceKey] = true
		name := recipeName(ctx, recipeService, householdID, u.RecipeID)
		if customized[u.EntryID] {
			fmt.Fprintf(out, "%s  %s: skipped, cooked with a customization\n", u.OccurredAt.Format(time.DateOnly), name)
			continue
		}
		swaps.old = u.OccurredAt.Before(oldSwap)
		res, err := svc.RecountCooked(ctx, u, apply)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if len(res.Added) == 0 {
			fmt.Fprintf(out, "%s  %s: nothing missed\n", u.OccurredAt.Format(time.DateOnly), name)
			continue
		}
		for _, l := range res.Added {
			fmt.Fprintf(out, "%s  %s: %s %s %s %s of %s\n", u.OccurredAt.Format(time.DateOnly), name, verb, l.Deducted, l.TrackingUnit, estimatedNote(l), l.Ingredient)
		}
		if apply {
			if err := pantryStore.ReplaceCookUsageLines(ctx, householdID, u.ID, res.Lines); err != nil {
				return fmt.Errorf("%s: save record: %w", name, err)
			}
		}
	}

	// Meals cooked with nothing deducted at all have no record.
	cooked, err := events.NewMongoStore(db).List(ctx, events.Query{HouseholdID: householdID, Types: []events.Type{events.TypeRecipeCooked}, Since: since})
	if err != nil {
		return err
	}
	for _, e := range cooked {
		p, _ := e.Payload.(events.RecipeCooked)
		if p.EntryID == "" || recorded["entry:"+p.EntryID] {
			continue
		}
		name := recipeName(ctx, recipeService, householdID, e.RecipeID)
		if customized[p.EntryID] {
			fmt.Fprintf(out, "%s  %s: skipped, cooked with a customization\n", e.OccurredAt.Format(time.DateOnly), name)
			continue
		}
		if !apply {
			fmt.Fprintf(out, "%s  %s: no record; would deduct it now\n", e.OccurredAt.Format(time.DateOnly), name)
			continue
		}
		swaps.old = e.OccurredAt.Before(oldSwap)
		usage, applied, err := svc.ApplyCooked(ctx, pantry.CookedMeal{
			HouseholdID: householdID, UserID: e.UserID, RecipeID: e.RecipeID, EntryID: p.EntryID,
			ClientEventID: e.ClientEventID, Servings: p.Servings, OccurredAt: e.OccurredAt,
		})
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if !applied {
			fmt.Fprintf(out, "%s  %s: nothing in the pantry to deduct\n", e.OccurredAt.Format(time.DateOnly), name)
		}
		for _, l := range usage.Lines {
			if l.Deducted != "" {
				fmt.Fprintf(out, "%s  %s: deducted %s %s %s of %s\n", e.OccurredAt.Format(time.DateOnly), name, l.Deducted, l.TrackingUnit, estimatedNote(l), l.Ingredient)
			}
		}
	}
	return nil
}

func estimatedNote(l pantry.CookLine) string {
	if l.Estimated {
		return "(estimated)"
	}
	return ""
}

func recipeName(ctx context.Context, r *recipes.Service, householdID, id string) string {
	if rec, err := r.Get(ctx, householdID, id); err == nil {
		return rec.Name
	}
	return id
}

// customizedEntries are the household's plan entries cooked with a
// customization.
func customizedEntries(ctx context.Context, db *mongo.Database, householdID string) (map[string]bool, error) {
	hid, err := bson.ObjectIDFromHex(householdID)
	if err != nil {
		return nil, fmt.Errorf("household: %w", err)
	}
	cur, err := db.Collection("weekly_plans").Find(ctx, bson.D{{Key: "householdId", Value: hid}})
	if err != nil {
		return nil, err
	}
	var plans []struct {
		Entries []struct {
			ID             bson.ObjectID `bson:"_id"`
			Customizations []bson.Raw    `bson:"customizations"`
		} `bson:"entries"`
	}
	if err := cur.All(ctx, &plans); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, p := range plans {
		for _, e := range p.Entries {
			if len(e.Customizations) > 0 {
				out[e.ID.Hex()] = true
			}
		}
	}
	return out, nil
}

// asCooked is the household's specialty choices as they were when a meal was
// cooked: before decision 624 the Tex-Mex Paste swap was 1 tsp of chipotle
// base and 5 tsp of tomato paste, with no chili spices.
type asCooked struct {
	inner pantry.CookSpecialties
	old   bool
}

func (a *asCooked) GrocerySpecialties(ctx context.Context, householdID string, lines []grocery.Line) (grocery.Specialties, error) {
	specs, err := a.inner.GrocerySpecialties(ctx, householdID, lines)
	if err != nil || !a.old {
		return specs, err
	}
	for _, sp := range specs {
		if sp == nil || sp.ID != "tex-mex-paste" || sp.Choice == nil || sp.Choice.Type != grocery.ChoiceStoreAlternative {
			continue
		}
		var kept []grocery.Component
		for _, c := range sp.Choice.Components {
			switch c.Name {
			case "Smoky Chipotle Bouillon Base":
				kept = append(kept, c)
			case "Tomato Paste":
				five := ingredients.NewQuantity(5, 1)
				c.Quantity = &five
				kept = append(kept, c)
			}
		}
		choice := *sp.Choice
		choice.Components = kept
		sp.Choice = &choice
	}
	return specs, nil
}
