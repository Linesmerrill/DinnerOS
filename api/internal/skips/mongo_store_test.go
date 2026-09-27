package skips

import (
	"context"
	"errors"
	"slices"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
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

// A skip stored before recipe skips existed has no recipeId field at all. It
// is still the household-wide skip for its ingredient: setting it again
// replaces it instead of colliding with it, once the old index is gone.
func TestIntegrationLegacySkipWithoutRecipeIDIsReplaced(t *testing.T) {
	ctx := context.Background()
	client := mongotest.Client(t)
	db := client.Database()
	// The collection as the previous release left it: the old unique index,
	// and a document without the field.
	if _, err := db.Collection(Collection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "ingredientKey", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("householdId_ingredientKey_unique"),
	}); err != nil {
		t.Fatal(err)
	}
	legacy := bson.D{
		{Key: "_id", Value: mustOID(t, "66e5a1f2c3b4a5d6e7f80098")}, {Key: "householdId", Value: mustOID(t, testHousehold)},
		{Key: "ingredientKey", Value: "name:cilantro"}, {Key: "key", Value: "cilantro"}, {Key: "name", Value: "Cilantro"},
		{Key: "scope", Value: "week"}, {Key: "week", Value: "2026-W38"},
		{Key: "createdBy", Value: mustOID(t, testUser)}, {Key: "createdAt", Value: testNow},
		{Key: "updatedBy", Value: mustOID(t, testUser)}, {Key: "updatedAt", Value: testNow},
	}
	if _, err := db.Collection(Collection).InsertOne(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureIndexes(ctx, Indexes()...); err != nil {
		t.Fatalf("EnsureIndexes() over the old index error = %v", err)
	}
	store := NewMongoStore(db)
	replaced, created, err := store.PutSkip(ctx, cilantro(testHousehold, ScopeAlways, ""))
	if err != nil || created || replaced.ID != "66e5a1f2c3b4a5d6e7f80098" || replaced.Scope != ScopeAlways {
		t.Fatalf("PutSkip() over a legacy skip = %+v, created %v, %v; want it replaced", replaced, created, err)
	}
	curry := cilantro(testHousehold, ScopeRecipe, "")
	curry.RecipeID, curry.RecipeName = "66e5a1f2c3b4a5d6e7f81001", "Thai Coconut Curry Chicken"
	if _, created, err := store.PutSkip(ctx, curry); err != nil || !created {
		t.Fatalf("PutSkip(recipe) beside a legacy skip = created %v, %v; the old index must be gone", created, err)
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
