package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func TestIntegrationMergeIngredients(t *testing.T) {
	ctx := context.Background()
	client := mongotest.Client(t)
	db := client.Database()
	onion, yellow, red := bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID()
	both, onlyVariant := bson.NewObjectID(), bson.NewObjectID()
	insert := func(coll string, docs ...any) {
		t.Helper()
		if _, err := db.Collection(coll).InsertMany(ctx, docs); err != nil {
			t.Fatal(err)
		}
	}
	insert("ingredients",
		bson.M{"_id": onion, "key": "onion", "name": "Onion", "sourceRefs": bson.A{bson.M{"source": "hellofresh", "sourceIngredientId": "a"}}},
		bson.M{"_id": yellow, "key": "yellow onion", "name": "Yellow Onion", "sourceRefs": bson.A{bson.M{"source": "hellofresh", "sourceIngredientId": "b"}}},
		bson.M{"_id": red, "key": "red onion", "name": "Red Onion"},
	)
	insert("recipes", bson.M{"_id": bson.NewObjectID(), "name": "Soup", "ingredients": bson.A{
		bson.M{"ingredientId": yellow.Hex(), "name": "Yellow Onion"},
		bson.M{"ingredientId": red.Hex(), "name": "Red Onion"},
	}})
	insert("recipe_catalog", bson.M{"_id": bson.NewObjectID(), "name": "Soup", "ingredients": bson.A{
		bson.M{"ingredientId": yellow, "name": "Yellow Onion"},
	}})
	insert("pantry_items", bson.M{"_id": bson.NewObjectID(), "householdId": both, "key": "yellow onion", "ingredientId": yellow})
	insert("shopping_product_preferences",
		bson.M{"_id": bson.NewObjectID(), "householdId": both, "provider": "walmart", "ingredientKey": onion.Hex(), "productId": "1"},
		bson.M{"_id": bson.NewObjectID(), "householdId": both, "provider": "walmart", "ingredientKey": yellow.Hex(), "productId": "1"},
		bson.M{"_id": bson.NewObjectID(), "householdId": onlyVariant, "provider": "walmart", "ingredientKey": yellow.Hex(), "productId": "2"},
	)
	insert("shopping_handoffs", bson.M{"_id": bson.NewObjectID(), "householdId": both, "week": "2026-W42",
		"lines": bson.A{bson.M{"id": "l1", "ingredientKey": yellow.Hex()}, bson.M{"id": "l2", "ingredientKey": red.Hex()}}})

	env := func(key string) string {
		switch key {
		case "MONGODB_URI":
			return os.Getenv("MONGODB_TEST_URI")
		case "MONGODB_DATABASE":
			return db.Name()
		}
		return ""
	}
	count := func(coll string, filter bson.M) int64 {
		t.Helper()
		n, err := db.Collection(coll).CountDocuments(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	var out bytes.Buffer
	if err := run(ctx, nil, env, &out); err != nil {
		t.Fatalf("dry run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "yellow onion") || !strings.Contains(out.String(), "dry run") || count("ingredients", bson.M{"_id": yellow}) != 1 {
		t.Fatalf("dry run wrote, or said nothing:\n%s", out.String())
	}

	out.Reset()
	if err := run(ctx, []string{"-apply"}, env, &out); err != nil {
		t.Fatalf("apply: %v\n%s", err, out.String())
	}
	checks := []struct {
		what string
		got  int64
		want int64
	}{
		{"variant deleted", count("ingredients", bson.M{"_id": yellow}), 0},
		{"red onion kept", count("ingredients", bson.M{"_id": red}), 1},
		{"kept has both refs", count("ingredients", bson.M{"_id": onion, "sourceRefs.sourceIngredientId": bson.M{"$all": bson.A{"a", "b"}}}), 1},
		{"recipe points at onion", count("recipes", bson.M{"ingredients.ingredientId": onion.Hex()}), 1},
		{"recipe keeps red onion", count("recipes", bson.M{"ingredients.ingredientId": red.Hex()}), 1},
		{"catalog points at onion", count("recipe_catalog", bson.M{"ingredients.ingredientId": onion}), 1},
		{"no references left", count("recipes", bson.M{"ingredients.ingredientId": yellow.Hex()}) + count("recipe_catalog", bson.M{"ingredients.ingredientId": yellow}), 0},
		{"pantry item relinked", count("pantry_items", bson.M{"ingredientId": onion}), 1},
		{"duplicate saved product dropped", count("shopping_product_preferences", bson.M{"householdId": both}), 1},
		{"only saved product moved", count("shopping_product_preferences", bson.M{"householdId": onlyVariant, "ingredientKey": onion.Hex()}), 1},
		{"handoff line rekeyed", count("shopping_handoffs", bson.M{"lines.ingredientKey": onion.Hex()}), 1},
		{"handoff keeps red onion", count("shopping_handoffs", bson.M{"lines.ingredientKey": red.Hex()}), 1},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: %d, want %d\n%s", c.what, c.got, c.want, out.String())
		}
	}

	// Running it again finds nothing to do.
	out.Reset()
	if err := run(ctx, []string{"-apply"}, env, &out); err != nil || strings.Contains(out.String(), "→") {
		t.Errorf("second run: %v\n%s", err, out.String())
	}
}
