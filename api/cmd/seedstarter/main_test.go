package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// starterRecipeCount is about the size of the owner's library.
const starterRecipeCount = 460

// seedSource creates a source household with n recipes shaped like imported
// HelloFresh ones, each with order history.
func seedSource(t *testing.T, ctx context.Context, db *mongo.Database, svc *households.Service, n int) string {
	t.Helper()
	source, _, err := svc.Create(ctx, bson.NewObjectID().Hex(), households.CreateInput{Name: "Source", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	store := recipes.NewMongoStore(db)
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.UpsertIngredients(ctx, []recipes.Ingredient{{Key: "garlic", Name: "Garlic", Category: "produce", CreatedAt: now, UpdatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	ings, err := store.FindIngredients(ctx, nil, []string{"garlic"})
	if err != nil || len(ings) != 1 {
		t.Fatalf("garlic = %v, %v", ings, err)
	}
	list := make([]recipes.Recipe, 0, n)
	for i := range n {
		r := recipes.Recipe{
			Source: recipes.SourceHelloFresh, SourceRecipeID: fmt.Sprintf("hf-%04d", i), SourceAliases: []string{fmt.Sprintf("hf-alias-%04d", i)},
			SourceURL: "https://www.hellofresh.com/recipes/x", Name: fmt.Sprintf("Recipe %d", i), Headline: "with rice", Description: strings.Repeat("Tasty. ", 40),
			ImageURL: "https://img.hellofresh.com/recipe.jpg", Servings: []int{2, 4}, PrepMinutes: 10, TotalMinutes: 35, Difficulty: 1,
			Cuisines: []string{"Asian"}, Tags: []string{"Quick"}, Utensils: []string{"Pan"}, Allergens: []string{"Soy"},
			Nutrition:  []recipes.Nutrient{{Name: "Calories", Amount: 650, Unit: "kcal"}, {Name: "Protein", Amount: 32, Unit: "g"}},
			OrderWeeks: []string{"2026-W30", "2026-W34"}, TimesOrdered: 2, LastOrderedWeek: "2026-W34",
			CreatedAt: now, UpdatedAt: now,
		}
		for j := range 12 {
			r.Ingredients = append(r.Ingredients, recipes.RecipeIngredient{IngredientID: ings[0].ID, Name: fmt.Sprintf("Garlic %d", j), Amounts: []recipes.Amount{
				{Servings: 2, Quantity: "1/2", Unit: "clove"}, {Servings: 4, Quantity: "1", Unit: "clove"},
			}})
		}
		for j := range 6 {
			r.Steps = append(r.Steps, recipes.Step{Index: j + 1, Text: strings.Repeat("Stir well. ", 10), ImageURL: "https://img.hellofresh.com/step.jpg"})
		}
		list = append(list, r)
	}
	if err := store.SaveRecipes(ctx, source.ID, list); err != nil {
		t.Fatal(err)
	}
	return source.ID
}

func count(t *testing.T, ctx context.Context, db *mongo.Database, householdID string) int64 {
	t.Helper()
	hid, _ := bson.ObjectIDFromHex(householdID)
	n, err := db.Collection(recipes.RecipesCollection).CountDocuments(ctx, bson.D{{Key: "householdId", Value: hid}})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIntegrationStarterLibrary(t *testing.T) {
	ctx := context.Background()
	client := mongotest.Client(t)
	db := client.Database()
	if err := client.EnsureIndexes(ctx, append(recipes.Indexes(), households.Indexes()...)...); err != nil {
		t.Fatal(err)
	}
	plain := households.NewService(households.ServiceOptions{Store: households.NewMongoStore(db)})
	sourceID := seedSource(t, ctx, db, plain, starterRecipeCount)
	recipeService := recipes.NewService(recipes.NewMongoStore(db))

	// Source set: creating a household copies the library in the background.
	// A long hold keeps the assertion below deterministic on a slow machine.
	starter := recipes.NewStarter(recipes.StarterOptions{Service: recipeService, SourceHouseholdID: sourceID, Hold: time.Minute})
	withStarter := households.NewService(households.ServiceOptions{Store: households.NewMongoStore(db), OnCreated: starter})
	started := time.Now()
	h, _, err := withStarter.Create(ctx, bson.NewObjectID().Hex(), households.CreateInput{Name: "Friends", TimeZone: "UTC"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	t.Logf("household create (holding for the copy of %d recipes) returned in %s", starterRecipeCount, time.Since(started))
	// Within the hold, the recipes are there when creation returns.
	if got := count(t, ctx, db, h.ID); got != starterRecipeCount {
		t.Fatalf("copied %d recipes, want %d", got, starterRecipeCount)
	}
	if err := starter.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	// Content is copied; order history is not.
	catalog, err := recipeService.Catalog(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := recipeService.Get(ctx, h.ID, catalog[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.HouseholdID != h.ID || first.SourceRecipeID != "hf-0000" || len(first.SourceAliases) != 1 || first.ImageURL == "" ||
		first.Description == "" || len(first.Steps) != 6 || first.Steps[0].ImageURL == "" || len(first.Ingredients) != 12 ||
		first.Ingredients[0].Category != "produce" || len(first.Ingredients[0].Amounts) != 2 || len(first.Nutrition) != 2 ||
		first.Difficulty != 1 || len(first.Allergens) != 1 || len(first.Servings) != 2 {
		t.Errorf("copied recipe content = %+v", first)
	}
	if first.OrderWeeks != nil || first.TimesOrdered != 0 || first.LastOrderedWeek != "" {
		t.Errorf("copied order history: weeks %v, times %d, last %q", first.OrderWeeks, first.TimesOrdered, first.LastOrderedWeek)
	}
	if n := count(t, ctx, db, sourceID); n != starterRecipeCount {
		t.Errorf("source now has %d recipes", n)
	}

	env := func(key string) string {
		switch key {
		case "MONGODB_URI":
			return os.Getenv("MONGODB_TEST_URI")
		case "MONGODB_DATABASE":
			return db.Name()
		case "STARTER_RECIPES_HOUSEHOLD_ID":
			return sourceID
		}
		return ""
	}

	// Re-running (the backfill command) does not duplicate.
	var out bytes.Buffer
	if err := run(ctx, []string{"-household", h.ID, "-apply"}, env, &out); err != nil {
		t.Fatalf("re-run error = %v\n%s", err, out.String())
	}
	if got := count(t, ctx, db, h.ID); got != starterRecipeCount || !strings.Contains(out.String(), fmt.Sprintf("copied 0 of %d starter recipes; %d already present", starterRecipeCount, starterRecipeCount)) {
		t.Fatalf("re-run left %d recipes:\n%s", got, out.String())
	}

	// Source unset: creation works and copies nothing.
	off := recipes.NewStarter(recipes.StarterOptions{Service: recipeService})
	without := households.NewService(households.ServiceOptions{Store: households.NewMongoStore(db), OnCreated: off})
	empty, _, err := without.Create(ctx, bson.NewObjectID().Hex(), households.CreateInput{Name: "Empty", TimeZone: "UTC"})
	if err != nil {
		t.Fatalf("Create() without source error = %v", err)
	}
	if err := off.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if got := count(t, ctx, db, empty.ID); got != 0 {
		t.Fatalf("without source: %d recipes", got)
	}

	// Backfill: dry run writes nothing, -apply copies, a second -apply is a no-op.
	out.Reset()
	if err := run(ctx, []string{"-household", empty.ID}, env, &out); err != nil {
		t.Fatalf("dry run error = %v", err)
	}
	if got := count(t, ctx, db, empty.ID); got != 0 || !strings.Contains(out.String(), fmt.Sprintf("would copy %d of %d", starterRecipeCount, starterRecipeCount)) {
		t.Fatalf("dry run wrote %d:\n%s", got, out.String())
	}
	for range 2 {
		out.Reset()
		if err := run(ctx, []string{"-household", empty.ID, "-apply"}, env, &out); err != nil {
			t.Fatalf("apply error = %v", err)
		}
		t.Log(strings.TrimSpace(out.String()))
		if got := count(t, ctx, db, empty.ID); got != starterRecipeCount {
			t.Fatalf("apply left %d recipes:\n%s", got, out.String())
		}
	}

	// Operator mistakes are refused.
	for name, args := range map[string][]string{
		"no household":   nil,
		"bad id":         {"-household", "nope"},
		"source itself":  {"-household", sourceID},
		"missing target": {"-household", "0123456789abcdef01234567"},
	} {
		if err := run(ctx, args, env, &out); err == nil {
			t.Errorf("%s: error = nil", name)
		}
	}
}
