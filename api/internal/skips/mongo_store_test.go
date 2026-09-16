package skips

import (
	"context"
	"errors"
	"slices"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newTestMongoClient(t *testing.T) *mongodb.Client {
	t.Helper()
	client := mongotest.Client(t)
	for range 2 { // applying indexes twice is a no-op
		if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	return client
}

func TestIntegrationIndexes(t *testing.T) {
	db := newTestMongoClient(t).Database()
	ctx := context.Background()
	for _, set := range Indexes() {
		specs, err := db.Collection(set.Collection).Indexes().ListSpecifications(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, s := range specs {
			names = append(names, s.Name)
		}
		for _, model := range set.Indexes {
			var opts options.IndexOptions
			for _, apply := range model.Options.List() {
				if err := apply(&opts); err != nil {
					t.Fatal(err)
				}
			}
			if opts.Name == nil || !slices.Contains(names, *opts.Name) {
				t.Errorf("%s: index %v missing (have %v)", set.Collection, model.Keys, names)
			}
		}
	}
}

func TestIntegrationStoreContract(t *testing.T) {
	runStoreContract(t, NewMongoStore(newTestMongoClient(t).Database()))
}

// The unique index is what keeps one ingredient to one skip, so a second skip
// for the same ingredient must be impossible even when the store is bypassed.
func TestIntegrationOneSkipPerIngredient(t *testing.T) {
	ctx := context.Background()
	db := newTestMongoClient(t).Database()
	store := NewMongoStore(db)
	if _, _, err := store.PutSkip(ctx, cilantro(testHousehold, ScopeAlways, "")); err != nil {
		t.Fatalf("PutSkip() error = %v", err)
	}
	_, err := db.Collection(Collection).InsertOne(ctx, skipDoc{
		ID: mustOID(t, "66e5a1f2c3b4a5d6e7f80099"), HouseholdID: mustOID(t, testHousehold),
		IngredientKey: "name:cilantro", Key: "cilantro", Name: "Cilantro", Scope: string(ScopeWeek),
		CreatedBy: mustOID(t, testUser), CreatedAt: testNow, UpdatedBy: mustOID(t, testUser), UpdatedAt: testNow,
	})
	if !errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		t.Fatalf("a second skip for one ingredient was accepted: %v", err)
	}
}

func mustOID(t *testing.T, hex string) bson.ObjectID {
	t.Helper()
	oid, err := mongodb.ParseID(hex)
	if err != nil {
		t.Fatalf("ParseID(%q) error = %v", hex, err)
	}
	return oid
}
