package recipes

import (
	"context"
	"errors"
	"slices"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newTestMongoService(t *testing.T) (*Service, *MongoStore, *mongo.Database) {
	t.Helper()
	client := mongotest.Client(t)
	for range 2 { // applying indexes twice is a no-op
		if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	store := NewMongoStore(client.Database())
	return NewService(store), store, client.Database()
}

func TestIntegrationIndexes(t *testing.T) {
	_, _, db := newTestMongoService(t)
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
			// Every declared index is named explicitly.
			if name := indexName(t, model); !slices.Contains(names, name) {
				t.Errorf("%s: index %q missing (have %v)", set.Collection, name, names)
			}
		}
	}
}

func indexName(t *testing.T, model mongo.IndexModel) string {
	t.Helper()
	var opts options.IndexOptions
	for _, set := range model.Options.List() {
		if err := set(&opts); err != nil {
			t.Fatal(err)
		}
	}
	if opts.Name == nil {
		t.Fatalf("index %v has no explicit name", model.Keys)
	}
	return *opts.Name
}

func TestIntegrationImport(t *testing.T) {
	svc, store, db := newTestMongoService(t)
	ctx := context.Background()
	hh, otherHH := bson.NewObjectID().Hex(), bson.NewObjectID().Hex()

	bowls := testRecipe("r2", "Bowls", "2026-W11")
	bowls.Ingredients = append(bowls.Ingredients,
		ImportIngredient{SourceIngredientID: "ing-garlic-2", Name: "garlic"},
		ImportIngredient{SourceIngredientID: "ing-zorb", Name: "Zorblax Root"},
	)
	file := testFile(testRecipe("r1", "Tacos", "2026-W10"), bowls)
	file.Review = []ImportReviewItem{
		{SourceRecipeID: "r1", RecipeName: "Tacos", Field: "ingredients.Mystery.unit", Value: "dollop", Reason: "unknown unit"},
		{SourceRecipeID: "r2", RecipeName: "Bowls", Field: "steps", Reason: "recipe has no steps"},
	}

	first := mustImport(t, svc, hh, file)
	if first.Created != 2 || first.IngredientsCreated != 3 || first.ReviewItems != 2 || len(first.Errors) != 0 {
		t.Fatalf("first import = %+v", first)
	}
	// The round trip through BSON must compare equal, or every re-import
	// would rewrite every recipe.
	second := mustImport(t, svc, hh, file)
	if second.Unchanged != 2 || second.Created != 0 || second.Updated != 0 || second.IngredientsCreated != 0 {
		t.Fatalf("second import = %+v, want all unchanged", second)
	}
	count := func(coll string, filter bson.D) int64 {
		n, err := db.Collection(coll).CountDocuments(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	hid, _ := bson.ObjectIDFromHex(hh)
	if n := count(ImportReviewsCollection, bson.D{{Key: "householdId", Value: hid}, {Key: "status", Value: "open"}}); n != 2 {
		t.Errorf("import_reviews = %d, want 2", n)
	}
	if n := count(IngredientsCollection, bson.D{{Key: "categoryConfident", Value: false}}); n != 1 {
		t.Errorf("ingredients needing review = %d, want 1 (zorblax)", n)
	}
	garlic, err := store.FindIngredients(ctx, []SourceRef{{Source: SourceHelloFresh, SourceIngredientID: "ing-garlic-2"}}, nil)
	if err != nil || len(garlic) != 1 || garlic[0].Key != "garlic" || len(garlic[0].SourceRefs) != 2 || garlic[0].Category != "produce" || !garlic[0].CategoryConfident {
		t.Errorf("garlic by source ref = %+v, %v", garlic, err)
	}

	// A new canonical ID that lists the old one as an alias updates in place.
	canonical := testRecipe("canon-r1", "Tacos", "2026-W12")
	canonical.SourceAliases = []string{"r1"}
	if res := mustImport(t, svc, hh, testFile(canonical)); res.Updated != 1 {
		t.Fatalf("alias import = %+v, want 1 updated", res)
	}
	if res := mustImport(t, svc, hh, testFile(canonical)); res.Unchanged != 1 {
		t.Fatalf("alias re-import = %+v, want unchanged", res)
	}
	if n := count(RecipesCollection, bson.D{{Key: "householdId", Value: hid}}); n != 2 {
		t.Errorf("recipes = %d, want 2", n)
	}

	found, err := store.FindRecipesBySourceIDs(ctx, hh, SourceHelloFresh, []string{"r1"})
	if err != nil || len(found) != 1 {
		t.Fatalf("FindRecipesBySourceIDs(alias) = %+v, %v", found, err)
	}
	tacos, err := svc.Get(ctx, hh, found[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if tacos.SourceRecipeID != "canon-r1" || !slices.Equal(tacos.SourceAliases, []string{"r1"}) ||
		!slices.Equal(tacos.OrderWeeks, []string{"2026-W10", "2026-W12"}) || tacos.TimesOrdered != 2 || tacos.LastOrderedWeek != "2026-W12" {
		t.Errorf("tacos identity/history = %+v", tacos)
	}
	if line := tacos.Ingredients[0]; line.Category != "produce" || line.Amounts[0].Quantity != "2" || line.IngredientID != garlic[0].ID {
		t.Errorf("tacos garlic = %+v", line)
	}

	// Exact and float quantities are both stored.
	var raw struct {
		Ingredients []struct {
			Amounts []bson.M `bson:"amounts"`
		} `bson:"ingredients"`
	}
	if err := db.Collection(RecipesCollection).FindOne(ctx, bson.D{{Key: "sourceRecipeId", Value: "canon-r1"}}).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Ingredients) == 0 || len(raw.Ingredients[0].Amounts) == 0 {
		t.Fatalf("stored recipe has no amounts: %+v", raw)
	}
	if amount := raw.Ingredients[0].Amounts[0]; amount["quantity"] != "2" || amount["quantityValue"] != 2.0 {
		t.Errorf("stored amount = %v", amount)
	}

	// GetMany is one household-scoped query plus one catalog query.
	many, err := svc.GetMany(ctx, hh, []string{tacos.ID, "not-an-id", bson.NewObjectID().Hex()})
	if err != nil || len(many) != 1 || many[0].ID != tacos.ID || many[0].Ingredients[0].Category != "produce" {
		t.Errorf("GetMany() = %+v, %v", many, err)
	}
	if others, err := svc.GetMany(ctx, otherHH, []string{tacos.ID}); err != nil || len(others) != 0 {
		t.Errorf("GetMany(other household) = %+v, %v", others, err)
	}

	// Household isolation and error mapping.
	if _, err := svc.Get(ctx, otherHH, tacos.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(other household) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(ctx, hh, "not-an-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(malformed) error = %v, want ErrNotFound", err)
	}
	err = store.SaveRecipes(ctx, hh, []Recipe{{Source: SourceHelloFresh, SourceRecipeID: "canon-r1", Name: "Duplicate"}})
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate SaveRecipes() error = %v, want ErrDuplicate", err)
	}
}

func TestIntegrationListContract(t *testing.T) {
	svc, _, _ := newTestMongoService(t)
	hh := bson.NewObjectID().Hex()
	mustImport(t, svc, hh, listFixture())
	mustImport(t, svc, bson.NewObjectID().Hex(), testFile(testRecipe("r-other", "Another Household Recipe")))
	runListContract(t, svc, hh)
}

func TestIntegrationImportReviews(t *testing.T) {
	svc, _, _ := newTestMongoService(t)
	ctx := context.Background()
	hh, otherHH := bson.NewObjectID().Hex(), bson.NewObjectID().Hex()

	// The stored recipe carries the review item's source ID as an alias, which
	// is the real shape: weekly clones merge under the newest source ID.
	tacos := testRecipe("canon-r1", "Tacos", "2026-W10")
	tacos.SourceAliases = []string{"r1"}
	file := testFile(tacos)
	file.Review = []ImportReviewItem{
		{SourceRecipeID: "r1", RecipeName: "Tacos", Field: "variant", Value: "pork tacos", Reason: "delivered menu variant differs"},
		{SourceRecipeID: "gone", RecipeName: "Ghost", Field: "steps", Reason: "recipe has no steps"},
	}
	if res := mustImport(t, svc, hh, file); res.ReviewItems != 2 {
		t.Fatalf("ReviewItems = %d, want 2", res.ReviewItems)
	}

	items, err := svc.ImportReviews(ctx, hh, ReviewStatusOpen, 0)
	if err != nil {
		t.Fatalf("ImportReviews() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	byField := map[string]ReviewRecord{}
	for _, it := range items {
		byField[it.Field] = it
		if it.Status != ReviewStatusOpen || it.CreatedAt.IsZero() {
			t.Errorf("%s: status = %q, createdAt = %v", it.Field, it.Status, it.CreatedAt)
		}
	}
	// The variant item resolves to its recipe through the alias.
	stored, err := svc.List(ctx, hh, ListQuery{})
	if err != nil || len(stored.Items) != 1 {
		t.Fatalf("List() = %+v, %v", stored.Items, err)
	}
	if got := byField["variant"].RecipeID; got != stored.Items[0].ID {
		t.Errorf("variant recipeId = %q, want %q", got, stored.Items[0].ID)
	}
	// An item whose recipe is not stored carries no ID rather than a wrong one.
	if got := byField["steps"].RecipeID; got != "" {
		t.Errorf("steps recipeId = %q, want empty", got)
	}

	// Items are scoped to the household, and limit caps the page.
	if other, err := svc.ImportReviews(ctx, otherHH, ReviewStatusOpen, 0); err != nil || len(other) != 0 {
		t.Errorf("other household items = %d, %v", len(other), err)
	}
	if one, err := svc.ImportReviews(ctx, hh, "", 1); err != nil || len(one) != 1 {
		t.Errorf("limit=1 items = %d, %v", len(one), err)
	}
}
