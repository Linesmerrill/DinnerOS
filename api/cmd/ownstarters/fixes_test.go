package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func TestEditIngredientsKeepsNamedLinesAndAddsNewOnes(t *testing.T) {
	r := recipes.Recipe{Ingredients: []recipes.RecipeIngredient{
		{IngredientID: "rice", Name: "Jasmine Rice", Amounts: []recipes.Amount{{Servings: 2, Quantity: "3/4", Unit: "cup"}}},
		{IngredientID: "cuke", Name: "Mini Cucumber", Amounts: []recipes.Amount{{Servings: 2, Quantity: "2", Unit: "count"}}},
		{IngredientID: "salt", Name: "Salt", PantryStaple: true},
	}}
	got := editIngredients(r, []FixIngredient{
		{Name: "jasmine rice"},
		{Name: "Snap Peas", Amounts: []FixAmount{{Servings: 2, Quantity: "4", Unit: "oz"}}},
		{Name: "Salt"},
	})
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3 (the cucumber is dropped)", len(got))
	}
	if got[0].IngredientID != "rice" || got[0].Name != "Jasmine Rice" || len(got[0].Amounts) != 1 || got[0].Amounts[0].Quantity != "3/4" {
		t.Errorf("kept line = %+v", got[0])
	}
	if got[1].IngredientID != "" || got[1].Name != "Snap Peas" || got[1].Amounts[0].Unit != "oz" {
		t.Errorf("new line = %+v", got[1])
	}
	if !got[2].PantryStaple || got[2].IngredientID != "salt" {
		t.Errorf("staple = %+v", got[2])
	}
}

func TestTheCheckedInFixesLoad(t *testing.T) {
	fixes, err := loadFixes(filepath.Join("fixes", "build-122.json"))
	if err != nil {
		t.Fatal(err)
	}
	later, err := loadFixes(filepath.Join("fixes", "build-123.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixes = append(fixes, later...)
	for _, f := range fixes {
		text := strings.ToLower(f.Headline + " " + f.Description + " " + strings.Join(f.Steps, " "))
		// Ginger Chicken's photo shows snap peas and peppers; the enchiladas are chicken, not beef.
		for _, gone := range []string{"cucumber", "peanut", "beef", "—"} {
			if strings.Contains(text, gone) {
				t.Errorf("%s still says %q", f.CatalogKey, gone)
			}
		}
	}
}

func TestIntegrationFixes(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	db := client.Database()
	starter, home, edited := bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID()
	starterRecipe := bson.NewObjectID()
	key := "hellofresh:fix-a"
	steps := bson.A{bson.D{{Key: "index", Value: 1}, {Key: "text", Value: "Smash the cucumbers."}}}
	doc := func(id, hh bson.ObjectID, steps bson.A) bson.D {
		return bson.D{{Key: "_id", Value: id}, {Key: "householdId", Value: hh}, {Key: "source", Value: "hellofresh"},
			{Key: "sourceRecipeId", Value: "fix-a"}, {Key: "catalogKey", Value: key}, {Key: "name", Value: "Bowls"},
			{Key: "sourceUrl", Value: "https://old.example/r"}, {Key: "headline", Value: "old"}, {Key: "servings", Value: bson.A{2}},
			{Key: "ingredients", Value: bson.A{bson.D{{Key: "ingredientId", Value: bson.NewObjectID()}, {Key: "name", Value: "Mini Cucumber"},
				{Key: "amounts", Value: bson.A{bson.D{{Key: "servings", Value: 2}, {Key: "quantity", Value: "2"}, {Key: "unit", Value: "count"}}}}}}},
			{Key: "steps", Value: steps}}
	}
	if _, err := db.Collection("recipes").InsertMany(ctx, []any{
		doc(starterRecipe, starter, steps),
		doc(bson.NewObjectID(), home, steps),
		doc(bson.NewObjectID(), edited, bson.A{bson.D{{Key: "index", Value: 1}, {Key: "text", Value: "My way."}}}),
	}); err != nil {
		t.Fatal(err)
	}
	fixes := []Fix{{CatalogKey: key, Name: "Snap Pea Bowls", Headline: "new", Description: "With snap peas.",
		Ingredients: []FixIngredient{{Name: "Snap Peas", Amounts: []FixAmount{{Servings: 2, Quantity: "4", Unit: "oz"}}}},
		Steps:       []string{"Cook the snap peas."}, Cuisines: []string{"Mexican"}}}
	raw, _ := json.Marshal(fixes)
	path := filepath.Join(t.TempDir(), "fixes.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"MONGODB_URI": os.Getenv("MONGODB_TEST_URI"), "MONGODB_DATABASE": db.Name(), "STARTER_RECIPES_HOUSEHOLD_ID": starter.Hex()}
	getenv := func(k string) string { return env[k] }
	var out bytes.Buffer
	if err := run(ctx, []string{"-fixes", path, "-apply"}, getenv, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "updated text of 1 starters and 1 copies (1 edited copies left alone)") {
		t.Errorf("run = %q", out.String())
	}
	type rec struct {
		Name        string `bson:"name"`
		Headline    string `bson:"headline"`
		SourceURL   string `bson:"sourceUrl"`
		Ingredients []struct {
			Name string `bson:"name"`
		} `bson:"ingredients"`
		Steps    []step   `bson:"steps"`
		Cuisines []string `bson:"cuisines"`
		EditedAt any      `bson:"editedAt"`
	}
	for _, hh := range []bson.ObjectID{starter, home} {
		var r rec
		if err := db.Collection("recipes").FindOne(ctx, bson.D{{Key: "householdId", Value: hh}}).Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.Name != "Snap Pea Bowls" || r.Headline != "new" || r.SourceURL != "" || len(r.Ingredients) != 1 ||
			r.Ingredients[0].Name != "Snap Peas" || r.Steps[0].Text != "Cook the snap peas." || r.EditedAt != nil ||
			len(r.Cuisines) != 1 || r.Cuisines[0] != "Mexican" {
			t.Errorf("fixed recipe = %+v", r)
		}
	}
	var e rec
	_ = db.Collection("recipes").FindOne(ctx, bson.D{{Key: "householdId", Value: edited}}).Decode(&e)
	if e.Steps[0].Text != "My way." || e.Name != "Bowls" || len(e.Cuisines) != 0 {
		t.Errorf("an edited copy changed: %+v", e)
	}
	// Running it again changes nothing.
	out.Reset()
	if err := run(ctx, []string{"-fixes", path}, getenv, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "would update text of 0 starters and 0 copies") {
		t.Errorf("second run = %q", out.String())
	}
}

func TestSameCuisines(t *testing.T) {
	cases := []struct {
		have, want []string
		same       bool
	}{
		{[]string{"North American"}, nil, true},
		{[]string{"Mexican"}, []string{"Mexican"}, true},
		{[]string{"North American"}, []string{"Mexican"}, false},
		{nil, []string{"Mexican"}, false},
	}
	for _, c := range cases {
		if got := sameCuisines(c.have, c.want); got != c.same {
			t.Errorf("sameCuisines(%v, %v) = %v", c.have, c.want, got)
		}
	}
}

// Ginger Chicken led with Ponzu Sauce and Chopped Chicken Breast, the only two
// rows above the fold, while its words promise snap peas, red pepper, rice,
// scallions, sesame and chili flakes (build 123, Hark's review item 11). The
// build 123 fix leads with what the words name, and keeps every line.
func TestGingerChickenLeadsWithWhatItsWordsName(t *testing.T) {
	old, err := loadFixes(filepath.Join("fixes", "build-122.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixes, err := loadFixes(filepath.Join("fixes", "build-123.json"))
	if err != nil || len(fixes) != 1 {
		t.Fatalf("fixes = %d, %v", len(fixes), err)
	}
	f := fixes[0]
	if f.CatalogKey != old[0].CatalogKey || f.Description != old[0].Description || strings.Join(f.Steps, "|") != strings.Join(old[0].Steps, "|") {
		t.Error("build 123 should only reorder build 122's Ginger Chicken")
	}
	var names, before []string
	for _, l := range f.Ingredients {
		names = append(names, l.Name)
	}
	for _, l := range old[0].Ingredients {
		before = append(before, l.Name)
	}
	if len(names) != len(before) {
		t.Fatalf("lines = %v, want the same %d as before", names, len(before))
	}
	for _, n := range before {
		if !slices.Contains(names, n) {
			t.Errorf("%s is gone", n)
		}
	}
	lead := strings.ToLower(strings.Join(names[:8], " "))
	for _, word := range []string{"chicken", "snap peas", "red bell pepper", "rice", "ginger", "scallions", "sesame seeds", "chili flakes"} {
		if !strings.Contains(lead, word) {
			t.Errorf("the first 8 lines %v don't name %q", names[:8], word)
		}
	}
	if names[0] != "Chopped Chicken Breast" || names[1] != "Snap Peas" {
		t.Errorf("leads with %v", names[:2])
	}
}

func TestSameIngredientsIsInOrder(t *testing.T) {
	var target fixTarget
	for _, n := range []string{"Ponzu Sauce", "Chicken"} {
		target.Ingredients = append(target.Ingredients, struct {
			Name string `bson:"name"`
		}{n})
	}
	if sameIngredients(target, []FixIngredient{{Name: "Chicken"}, {Name: "Ponzu Sauce"}}) {
		t.Error("another order counted as the same")
	}
	if !sameIngredients(target, []FixIngredient{{Name: "ponzu sauce"}, {Name: "Chicken"}}) {
		t.Error("the same order, other case, counted as different")
	}
	if !sameIngredients(target, nil) {
		t.Error("a fix without ingredients leaves them alone")
	}
}
