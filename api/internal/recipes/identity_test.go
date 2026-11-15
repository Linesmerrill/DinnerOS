package recipes

import (
	"strings"
	"testing"
)

func TestCatalogKeyUsesTheSourcesOwnIdentity(t *testing.T) {
	ada := Recipe{Source: SourceHelloFresh, SourceRecipeID: "hf-42", Name: "Pork Tacos", HouseholdID: "a"}
	bob := Recipe{Source: "HelloFresh", SourceRecipeID: "hf-42", Name: "Pork Tacos (2 servings)", HouseholdID: "b"}
	if got, want := CatalogKey(ada), "hellofresh:hf-42"; got != want {
		t.Fatalf("CatalogKey = %q; want %q", got, want)
	}
	if CatalogKey(ada) != CatalogKey(bob) {
		t.Errorf("two households' copies of one HelloFresh recipe got different keys: %q and %q", CatalogKey(ada), CatalogKey(bob))
	}
}

func TestCatalogKeyFallsBackToNameAndIngredients(t *testing.T) {
	typed := func(name string, ingredientNames ...string) Recipe {
		r := Recipe{Source: SourceManual, SourceRecipeID: "whatever", Name: name}
		for _, n := range ingredientNames {
			r.Ingredients = append(r.Ingredients, RecipeIngredient{Name: n})
		}
		return r
	}
	base := typed("Grandma's Chili", "Kidney Beans", "Ground Beef")
	sameRecipe := typed("  GRANDMA'S CHILI!  ", "ground beef", "kidney beans")
	differentIngredients := typed("Grandma's Chili", "Black Beans", "Ground Beef")

	if CatalogKey(base) == "" {
		t.Fatal("a typed recipe got no catalog key")
	}
	if CatalogKey(base) != CatalogKey(sameRecipe) {
		t.Error("the same typed recipe, written differently, got two keys")
	}
	if CatalogKey(base) == CatalogKey(differentIngredients) {
		t.Error("two different recipes with the same name share a key")
	}
	// A typed recipe's source ID must not be part of its identity: two
	// households typing the same recipe generate different IDs.
	other := base
	other.SourceRecipeID = "a-different-generated-id"
	if CatalogKey(base) != CatalogKey(other) {
		t.Error("a generated source ID leaked into a typed recipe's identity")
	}
}

func TestCatalogKeyOfATypedRecipeCoversItsWholeContent(t *testing.T) {
	brownies := func(steps []string, lines ...[3]string) Recipe {
		r := Recipe{Source: SourceManual, SourceRecipeID: "generated", Name: "Brownies"}
		for _, l := range lines {
			r.Ingredients = append(r.Ingredients, RecipeIngredient{Name: l[0], Amounts: []Amount{{Servings: 2, Quantity: l[1], Unit: l[2]}}})
		}
		for i, s := range steps {
			r.Steps = append(r.Steps, Step{Index: i + 1, Text: s})
		}
		return r
	}
	butter, cocoa := [3]string{"Butter", "1/2", "cup"}, [3]string{"Cocoa", "1/3", "cup"}
	jane := brownies([]string{"Melt the butter.", "Bake 20 minutes."}, butter, cocoa)
	for _, tc := range []struct {
		name string
		r    Recipe
		same bool
	}{
		{"the same recipe, written differently", brownies([]string{"melt the  butter.", "BAKE 20 minutes."}, cocoa, [3]string{"butter", "0.5", "Cup"}), true},
		{"different steps", brownies([]string{"Melt the butter.", "Bake 35 minutes."}, butter, cocoa), false},
		{"steps in another order", brownies([]string{"Bake 20 minutes.", "Melt the butter."}, butter, cocoa), false},
		{"a different amount", brownies([]string{"Melt the butter.", "Bake 20 minutes."}, [3]string{"Butter", "1", "cup"}, cocoa), false},
		{"a different unit", brownies([]string{"Melt the butter.", "Bake 20 minutes."}, [3]string{"Butter", "1/2", "tbsp"}, cocoa), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CatalogKey(tc.r) == CatalogKey(jane); got != tc.same {
				t.Errorf("same key = %v; want %v", got, tc.same)
			}
			// The key from before decision 697 saw only the name and the
			// ingredient names.
			if LegacyCatalogKey(tc.r) != LegacyCatalogKey(jane) {
				t.Error("the legacy key changed with steps or amounts")
			}
		})
	}
	if !strings.HasPrefix(CatalogKey(jane), "fp2:") || !strings.HasPrefix(LegacyCatalogKey(jane), "fp:") {
		t.Errorf("keys = %q, %q", CatalogKey(jane), LegacyCatalogKey(jane))
	}
	hf := Recipe{Source: SourceHelloFresh, SourceRecipeID: "hf-1", Name: "Tacos", Steps: []Step{{Index: 1, Text: "Cook"}}}
	if CatalogKey(hf) != "hellofresh:hf-1" || LegacyCatalogKey(hf) != "" {
		t.Errorf("an importer's recipe is keyed %q, legacy %q", CatalogKey(hf), LegacyCatalogKey(hf))
	}
}

func TestCatalogOrigin(t *testing.T) {
	r := Recipe{HouseholdID: "h1", Source: SourceManual, SourceRecipeID: "generated-1", Name: "Brownies"}
	edited := r
	edited.Name, edited.Steps = "Fudgy Brownies", []Step{{Index: 1, Text: "Bake longer"}}
	if CatalogOrigin(r) == "" || CatalogOrigin(r) != CatalogOrigin(edited) {
		t.Errorf("an edit changed the origin: %q, %q", CatalogOrigin(r), CatalogOrigin(edited))
	}
	if strings.Contains(CatalogOrigin(r), "h1") || len(CatalogOrigin(r)) != 64 {
		t.Errorf("origin %q is not an opaque hash", CatalogOrigin(r))
	}
	other := r
	other.HouseholdID = "h2"
	if CatalogOrigin(other) == CatalogOrigin(r) {
		t.Error("two households' recipes share an origin")
	}
	if byID := (Recipe{HouseholdID: "h1", ID: "r1", Source: SourceImport, Name: "Soup"}); CatalogOrigin(byID) == "" {
		t.Error("a recipe with only its own ID has no origin")
	}
	for _, none := range []Recipe{
		{HouseholdID: "h1", Source: SourceHelloFresh, SourceRecipeID: "hf-1", Name: "Tacos"},
		{Source: SourceManual, SourceRecipeID: "generated-1", Name: "Brownies"},
		{HouseholdID: "h1", Source: SourceImport, Name: "Soup"},
	} {
		if got := CatalogOrigin(none); got != "" {
			t.Errorf("CatalogOrigin(%+v) = %q; want none", none, got)
		}
	}
}

func TestCatalogKeyIsEmptyWithoutAName(t *testing.T) {
	if got := CatalogKey(Recipe{Source: SourceManual, SourceRecipeID: "x"}); got != "" {
		t.Errorf("CatalogKey of a nameless recipe = %q; want empty", got)
	}
}

func TestPublicSourceAndPublishable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		recipe Recipe
		want   bool
	}{
		// A meal kit's recipes never reach the catalog, shared or not (decision 672).
		{"a meal-kit recipe", Recipe{Source: SourceHelloFresh, SourceRecipeID: "1", Name: "A"}, false},
		{"a meal-kit recipe the household shared", Recipe{Source: SourceHelloFresh, SourceRecipeID: "1", Name: "A", SharedToCatalog: true}, true},
		{"a meal-kit recipe the household kept private", Recipe{Source: SourceHelloFresh, SourceRecipeID: "1", Name: "A"}, false},
		{"an import the household shared", Recipe{Source: SourceImport, SourceRecipeID: "1", Name: "A", SharedToCatalog: true}, true},
		{"a typed recipe", Recipe{Source: SourceManual, Name: "A"}, false},
		{"a pasted page", Recipe{Source: SourceUser, Name: "A"}, false},
		{"an operator import of unknown provenance", Recipe{Source: SourceImport, SourceRecipeID: "1", Name: "A"}, false},
		{"a typed recipe the household shared", Recipe{Source: SourceManual, Name: "A", SharedToCatalog: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Publishable(tc.recipe); got != tc.want {
				t.Errorf("Publishable = %v; want %v", got, tc.want)
			}
		})
	}
	if PublicSource(SourceHelloFresh) || PublicSource("HELLOFRESH ") {
		t.Error("a meal kit's recipes would reach the catalog without the household sharing them")
	}
	if CatalogWithheld(SourceHelloFresh) || len(WithheldSources()) != 0 {
		t.Error("a household's shared meal-kit imports are still withheld")
	}
	if len(PublicSources()) != 0 {
		t.Error("some source is published without a household opting in")
	}
	if PublicSource(SourceManual) || PublicSource(SourceUser) || PublicSource("") {
		t.Error("a private source is treated as public")
	}
}
