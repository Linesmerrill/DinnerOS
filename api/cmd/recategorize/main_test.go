package main

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

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
