package recipes

import "testing"

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
		{"a meal-kit recipe", Recipe{Source: SourceHelloFresh, SourceRecipeID: "1", Name: "A"}, true},
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
	if !PublicSource("HELLOFRESH ") {
		t.Error("PublicSource is case- or space-sensitive")
	}
	if PublicSource(SourceManual) || PublicSource(SourceUser) || PublicSource("") {
		t.Error("a private source is treated as public")
	}
}
