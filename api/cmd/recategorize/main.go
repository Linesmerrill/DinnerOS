// Command recategorize re-runs ingredients.Categorize on catalog ingredients
// that were stored without a confident grocery category. Imports never
// overwrite a stored category, so this is how new categorizer rules reach
// existing data. Confident categories (including any set by a person) are never
// touched.
//
// -pantry instead moves pantry items whose name says they're frozen ("Frozen
// Peas") to the Frozen category. Items added before build 122 took the food's
// category and sat under Produce. Nothing else about an item changes.
//
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/recategorize          # dry run
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/recategorize -apply
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/recategorize -pantry          # dry run
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
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "recategorize:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("recategorize", flag.ContinueOnError)
	flags.SetOutput(out)
	apply := flags.Bool("apply", false, "write changes (default is a dry run)")
	pantryItems := flags.Bool("pantry", false, "move pantry items named \"Frozen …\" to Frozen instead")
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
	client, err := mongodb.Connect(connectCtx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-recategorize"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.Background()) }()

	verb := "would update"
	if *apply {
		verb = "updated"
	}
	if *pantryItems {
		res, err := frozenPantry(ctx, client.Database().Collection(pantry.ItemsCollection), *apply)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s %d of %d pantry items named as frozen in database %q\n",
			verb, len(res.changes), res.checked, database)
		return nil
	}
	res, err := recategorize(ctx, client.Database().Collection(recipes.IngredientsCollection), *apply)
	if err != nil {
		return err
	}
	for _, c := range res.changes {
		fmt.Fprintf(out, "  %-40s → %s\n", c.name, c.category)
	}
	fmt.Fprintf(out, "%s %d of %d uncategorized ingredients in database %q; %d still need review\n",
		verb, len(res.changes), res.checked, database, res.checked-len(res.changes))
	return nil
}

type change struct {
	id       bson.ObjectID
	name     string
	category string
}

type result struct {
	checked int
	changes []change
}

func recategorize(ctx context.Context, coll *mongo.Collection, apply bool) (result, error) {
	cur, err := coll.Find(ctx, bson.D{{Key: "categoryConfident", Value: false}})
	if err != nil {
		return result{}, err
	}
	var docs []struct {
		ID   bson.ObjectID `bson:"_id"`
		Name string        `bson:"name"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return result{}, err
	}

	res := result{checked: len(docs)}
	for _, d := range docs {
		category, confident := ingredients.Categorize(d.Name)
		if !confident {
			continue
		}
		res.changes = append(res.changes, change{id: d.ID, name: d.Name, category: category})
	}
	if !apply {
		return res, nil
	}
	for _, c := range res.changes {
		// The filter re-checks categoryConfident so a category a person set in
		// the meantime is not overwritten.
		_, err := coll.UpdateOne(ctx,
			bson.D{{Key: "_id", Value: c.id}, {Key: "categoryConfident", Value: false}},
			bson.D{{Key: "$set", Value: bson.D{
				{Key: "category", Value: c.category},
				{Key: "categoryConfident", Value: true},
				{Key: "updatedAt", Value: time.Now().UTC()},
			}}})
		if err != nil {
			return res, fmt.Errorf("update %s: %w", c.name, err)
		}
	}
	return res, nil
}

// frozenPantry finds pantry items whose name puts them in the Frozen category
// (ingredients.Categorize: "Frozen Peas", "Ice Cream") but that sit in
// another, and with apply moves them. Item names are people's own, so it
// prints only counts.
func frozenPantry(ctx context.Context, coll *mongo.Collection, apply bool) (result, error) {
	cur, err := coll.Find(ctx, bson.D{
		{Key: "category", Value: bson.D{{Key: "$ne", Value: ingredients.CategoryFrozen}}},
		{Key: "displayName", Value: bson.Regex{Pattern: `frozen|ice cream`, Options: "i"}},
	})
	if err != nil {
		return result{}, err
	}
	var docs []struct {
		ID   bson.ObjectID `bson:"_id"`
		Name string        `bson:"displayName"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return result{}, err
	}
	res := result{checked: len(docs)}
	for _, d := range docs {
		if category, confident := ingredients.Categorize(d.Name); confident && category == ingredients.CategoryFrozen {
			res.changes = append(res.changes, change{id: d.ID, name: d.Name, category: category})
		}
	}
	if !apply {
		return res, nil
	}
	for _, c := range res.changes {
		_, err := coll.UpdateOne(ctx,
			bson.D{{Key: "_id", Value: c.id}, {Key: "category", Value: bson.D{{Key: "$ne", Value: c.category}}}},
			bson.D{{Key: "$set", Value: bson.D{
				{Key: "category", Value: c.category},
				{Key: "updatedAt", Value: time.Now().UTC()},
			}}})
		if err != nil {
			return res, fmt.Errorf("update pantry item %s: %w", c.id.Hex(), err)
		}
	}
	return res, nil
}
