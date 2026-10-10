// Command mergeingredients folds catalog ingredients imported under a
// variant's name into the ingredient they are the same as
// (ingredients.SameIngredientKey, decision 640): "Yellow Onion" into
// "Onion". Recipes, the published catalog, pantry items, saved products, and
// Walmart hand-offs that point at the variant point at the kept ingredient
// instead; the kept one gains the variant's source references, so a later
// import resolves there too; and the variant is deleted.
//
// A household with a saved product for both keeps the kept ingredient's and
// drops the variant's. Recipe text is untouched: a step still says "yellow
// onion".
//
// It is a dry run unless -apply is given, and re-running it is safe.
//
//	MONGODB_URI=... go run ./cmd/mergeingredients          # dry run
//	MONGODB_URI=... go run ./cmd/mergeingredients -apply
//
// It reads MONGODB_URI (default mongodb://localhost:27017) and MONGODB_DATABASE
// (default dinneros) from the environment and never prints the URI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mergeingredients:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("mergeingredients", flag.ContinueOnError)
	flags.SetOutput(out)
	apply := flags.Bool("apply", false, "write changes (default is a dry run)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	uri := strings.TrimSpace(getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	database := strings.TrimSpace(getenv("MONGODB_DATABASE"))
	if database == "" {
		database = "dinneros"
	}
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-mergeingredients"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.Background()) }()
	m := merger{db: client.Database(), apply: *apply, out: out}

	variants := ingredients.SameIngredientVariants()
	names := make([]string, 0, len(variants))
	for v := range variants {
		names = append(names, v)
	}
	slices.Sort(names)
	for _, v := range names {
		if err := m.merge(ctx, v, variants[v]); err != nil {
			return fmt.Errorf("%s: %w", v, err)
		}
	}
	if !*apply {
		fmt.Fprintln(out, "dry run: nothing written; run with -apply to merge")
	}
	return nil
}

type merger struct {
	db    *mongo.Database
	apply bool
	out   io.Writer
}

type catalogIngredient struct {
	ID         bson.ObjectID `bson:"_id"`
	Key        string        `bson:"key"`
	SourceRefs []bson.M      `bson:"sourceRefs"`
}

func (m merger) find(ctx context.Context, key string) (*catalogIngredient, error) {
	var ing catalogIngredient
	err := m.db.Collection("ingredients").FindOne(ctx, bson.M{"key": key}).Decode(&ing)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ing, nil
}

// merge folds the variant catalog ingredient into the kept one.
func (m merger) merge(ctx context.Context, variantKey, keptKey string) error {
	variant, err := m.find(ctx, variantKey)
	if err != nil || variant == nil {
		return err
	}
	kept, err := m.find(ctx, keptKey)
	if err != nil {
		return err
	}
	if kept == nil {
		fmt.Fprintf(m.out, "%s: no %q in the catalog to merge into; left alone\n", variantKey, keptKey)
		return nil
	}
	vID, kID := variant.ID, kept.ID
	vHex, kHex := vID.Hex(), kID.Hex()
	fmt.Fprintf(m.out, "%s (%s) → %s (%s)\n", variantKey, vHex, keptKey, kHex)

	// Recipes and the published catalog store the ID as a string in some
	// documents and an ObjectID in others; both forms move, keeping their type.
	for _, coll := range []string{"recipes", "recipe_catalog"} {
		for _, pair := range [][2]any{{vHex, kHex}, {vID, kID}} {
			n, err := m.updateMany(ctx, coll,
				bson.M{"ingredients.ingredientId": pair[0]},
				bson.M{"$set": bson.M{"ingredients.$[x].ingredientId": pair[1]}},
				options.UpdateMany().SetArrayFilters([]any{bson.M{"x.ingredientId": pair[0]}}))
			if err != nil {
				return fmt.Errorf("%s: %w", coll, err)
			}
			if n > 0 {
				fmt.Fprintf(m.out, "  %s: %d\n", coll, n)
			}
		}
	}

	n, err := m.updateMany(ctx, "pantry_items", bson.M{"ingredientId": vID}, bson.M{"$set": bson.M{"ingredientId": kID}})
	if err != nil {
		return fmt.Errorf("pantry_items: %w", err)
	}
	if n > 0 {
		fmt.Fprintf(m.out, "  pantry_items: %d\n", n)
	}

	if err := m.mergePreferences(ctx, vHex, kHex); err != nil {
		return err
	}

	for _, field := range []string{"lines", "excluded"} {
		n, err := m.updateMany(ctx, "shopping_handoffs",
			bson.M{field + ".ingredientKey": vHex},
			bson.M{"$set": bson.M{field + ".$[x].ingredientKey": kHex}},
			options.UpdateMany().SetArrayFilters([]any{bson.M{"x.ingredientKey": vHex}}))
		if err != nil {
			return fmt.Errorf("shopping_handoffs %s: %w", field, err)
		}
		if n > 0 {
			fmt.Fprintf(m.out, "  shopping_handoffs %s: %d\n", field, n)
		}
	}

	if len(variant.SourceRefs) > 0 {
		fmt.Fprintf(m.out, "  source refs moved: %d\n", len(variant.SourceRefs))
		if m.apply {
			refs := make([]any, len(variant.SourceRefs))
			for i, r := range variant.SourceRefs {
				refs[i] = r
			}
			if _, err := m.db.Collection("ingredients").UpdateByID(ctx, kID,
				bson.M{"$addToSet": bson.M{"sourceRefs": bson.M{"$each": refs}}}); err != nil {
				return fmt.Errorf("move source refs: %w", err)
			}
		}
	}
	fmt.Fprintf(m.out, "  catalog ingredient %q deleted\n", variantKey)
	if m.apply {
		if _, err := m.db.Collection("ingredients").DeleteOne(ctx, bson.M{"_id": vID}); err != nil {
			return fmt.Errorf("delete variant: %w", err)
		}
	}
	return nil
}

// mergePreferences moves saved products from the variant to the kept
// ingredient. A household that saved both keeps the kept ingredient's.
func (m merger) mergePreferences(ctx context.Context, vHex, kHex string) error {
	coll := m.db.Collection("shopping_product_preferences")
	cur, err := coll.Find(ctx, bson.M{"ingredientKey": vHex})
	if err != nil {
		return fmt.Errorf("shopping_product_preferences: %w", err)
	}
	var prefs []struct {
		ID          bson.ObjectID `bson:"_id"`
		HouseholdID bson.ObjectID `bson:"householdId"`
		Provider    string        `bson:"provider"`
	}
	if err := cur.All(ctx, &prefs); err != nil {
		return fmt.Errorf("shopping_product_preferences: %w", err)
	}
	moved, dropped := 0, 0
	for _, p := range prefs {
		n, err := coll.CountDocuments(ctx, bson.M{"householdId": p.HouseholdID, "provider": p.Provider, "ingredientKey": kHex})
		if err != nil {
			return fmt.Errorf("shopping_product_preferences: %w", err)
		}
		if n > 0 {
			dropped++
			if m.apply {
				if _, err := coll.DeleteOne(ctx, bson.M{"_id": p.ID}); err != nil {
					return fmt.Errorf("drop duplicate saved product: %w", err)
				}
			}
			continue
		}
		moved++
		if m.apply {
			if _, err := coll.UpdateByID(ctx, p.ID, bson.M{"$set": bson.M{"ingredientKey": kHex}}); err != nil {
				return fmt.Errorf("move saved product: %w", err)
			}
		}
	}
	if moved+dropped > 0 {
		fmt.Fprintf(m.out, "  saved products: %d moved, %d duplicates dropped\n", moved, dropped)
	}
	return nil
}

// updateMany counts the matching documents, and updates them with -apply.
func (m merger) updateMany(ctx context.Context, coll string, filter, update bson.M, opts ...options.Lister[options.UpdateManyOptions]) (int64, error) {
	c := m.db.Collection(coll)
	if !m.apply {
		return c.CountDocuments(ctx, filter)
	}
	res, err := c.UpdateMany(ctx, filter, update, opts...)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
