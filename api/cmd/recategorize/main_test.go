package main

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func TestRecategorize(t *testing.T) {
	ctx := context.Background()
	coll := mongotest.Client(t).Database().Collection(recipes.IngredientsCollection)
	_, err := coll.InsertMany(ctx, []any{
		bson.D{{Key: "name", Value: "Parsnip"}, {Key: "category", Value: "other"}, {Key: "categoryConfident", Value: false}},
		bson.D{{Key: "name", Value: "Mystery Ingredient XYZ"}, {Key: "category", Value: "other"}, {Key: "categoryConfident", Value: false}},
		// A confident category (possibly set by a person) is never changed,
		// even if the rules disagree.
		bson.D{{Key: "name", Value: "Salt"}, {Key: "category", Value: "pantry"}, {Key: "categoryConfident", Value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	dry, err := recategorize(ctx, coll, false)
	if err != nil || dry.checked != 2 || len(dry.changes) != 1 {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if n, _ := coll.CountDocuments(ctx, bson.D{{Key: "categoryConfident", Value: false}}); n != 2 {
		t.Fatalf("dry run wrote changes: %d unconfident left", n)
	}

	if _, err := recategorize(ctx, coll, true); err != nil {
		t.Fatal(err)
	}
	category := func(name string) (string, bool) {
		var doc struct {
			Category          string `bson:"category"`
			CategoryConfident bool   `bson:"categoryConfident"`
		}
		if err := coll.FindOne(ctx, bson.D{{Key: "name", Value: name}}).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		return doc.Category, doc.CategoryConfident
	}
	if c, ok := category("Parsnip"); c != "produce" || !ok {
		t.Errorf("Parsnip = %s/%v, want produce/true", c, ok)
	}
	if c, ok := category("Mystery Ingredient XYZ"); c != "other" || ok {
		t.Errorf("unknown = %s/%v, want unchanged other/false", c, ok)
	}
	if c, ok := category("Salt"); c != "pantry" || !ok {
		t.Errorf("confident Salt = %s/%v, want untouched pantry/true", c, ok)
	}
}

// Pantry items added before build 122 as "Frozen Peas" sat under Produce.
func TestFrozenPantryItemsMoveToFrozen(t *testing.T) {
	ctx := context.Background()
	coll := mongotest.Client(t).Database().Collection(pantry.ItemsCollection)
	_, err := coll.InsertMany(ctx, []any{
		bson.D{{Key: "displayName", Value: "Frozen Peas"}, {Key: "category", Value: "produce"}, {Key: "quantity", Value: "1"}},
		bson.D{{Key: "displayName", Value: "frozen shrimp"}, {Key: "category", Value: "meat"}},
		bson.D{{Key: "displayName", Value: "Vanilla Ice Cream"}, {Key: "category", Value: "dairy"}},
		bson.D{{Key: "displayName", Value: "Frozen Spinach"}, {Key: "category", Value: "frozen"}},
		bson.D{{Key: "displayName", Value: "Peas"}, {Key: "category", Value: "produce"}},
		bson.D{{Key: "displayName", Value: "Unfrozen Butter"}, {Key: "category", Value: "dairy"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	category := func(name string) string {
		t.Helper()
		var doc struct {
			Category string `bson:"category"`
		}
		if err := coll.FindOne(ctx, bson.D{{Key: "displayName", Value: name}}).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		return doc.Category
	}

	dry, err := frozenPantry(ctx, coll, false)
	if err != nil || len(dry.changes) != 3 {
		t.Fatalf("dry run = %+v, %v; want 3 changes", dry, err)
	}
	if category("Frozen Peas") != "produce" {
		t.Fatal("dry run wrote a change")
	}
	if _, err := frozenPantry(ctx, coll, true); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"Frozen Peas": "frozen", "frozen shrimp": "frozen", "Vanilla Ice Cream": "frozen",
		"Frozen Spinach": "frozen", "Peas": "produce", "Unfrozen Butter": "dairy",
	} {
		if got := category(name); got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	var peas struct {
		Quantity string `bson:"quantity"`
	}
	_ = coll.FindOne(ctx, bson.D{{Key: "displayName", Value: "Frozen Peas"}}).Decode(&peas)
	if peas.Quantity != "1" {
		t.Errorf("quantity = %q, want it untouched", peas.Quantity)
	}
	again, err := frozenPantry(ctx, coll, false)
	if err != nil || len(again.changes) != 0 {
		t.Errorf("second run = %+v, %v; want nothing left", again, err)
	}
}
