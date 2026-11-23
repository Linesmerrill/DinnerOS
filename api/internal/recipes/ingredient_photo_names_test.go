package recipes

import (
	"context"
	"slices"
	"testing"
)

func TestPhotoBaseDropsLeadingDescriptors(t *testing.T) {
	for in, want := range map[string]string{
		"diced chicken thighs":             "chicken thighs",
		"baby spinach":                     "spinach",
		"boneless skinless chicken thighs": "chicken thighs",
		"bone in chicken thighs":           "chicken thighs",
		"sliced cremini mushrooms":         "cremini mushrooms",
		// Ground beef and crushed tomatoes are their own things.
		"ground beef":        "ground beef",
		"crushed tomatoes":   "crushed tomatoes",
		"frozen spinach":     "frozen spinach",
		"baby":               "baby",
		"spinach":            "spinach",
		"salt and pepper":    "salt and pepper",
		"chicken diced fine": "chicken diced fine",
	} {
		if got := photoBase(in); got != want {
			t.Errorf("photoBase(%q) = %q, want %q", in, got, want)
		}
	}
	keys := photoKeys("diced chicken thighs")
	for _, want := range []string{"diced chicken thigh", "chicken thighs", "chicken thigh"} {
		if !slices.Contains(keys, want) {
			t.Errorf("photoKeys(diced chicken thighs) = %v, missing %q", keys, want)
		}
	}
	if slices.Contains(keys, "diced chicken thighs") {
		t.Error("photoKeys lists the key itself")
	}
	if keys := photoKeys("tomatoes"); !slices.Contains(keys, "tomato") {
		t.Errorf("photoKeys(tomatoes) = %v, missing tomato", keys)
	}
}

func TestIngredientsWithoutAPhotoBorrowThePlainIngredients(t *testing.T) {
	const spinach, thighs, mushrooms = "https://img.example/spinach.png", "https://img.example/thighs.png", "https://img.example/mushrooms.png"
	store := newMemoryStore()
	store.ingredients = []Ingredient{
		{ID: "a1", Key: "baby spinach", Name: "Baby Spinach"},
		{ID: "a2", Key: "spinach", Name: "Spinach", ImageURL: spinach},
		{ID: "a3", Key: "diced chicken thighs", Name: "Diced Chicken Thighs"},
		// Only another cut has a photo: it stands in.
		{ID: "a4", Key: "boneless chicken thighs", Name: "Boneless Chicken Thighs", ImageURL: thighs},
		{ID: "a5", Key: "sliced mushroom", Name: "Sliced Mushroom"},
		{ID: "a6", Key: "mushrooms", Name: "Mushrooms", ImageURL: mushrooms},
		{ID: "a7", Key: "ground beef", Name: "Ground Beef"},
		{ID: "a8", Key: "beef", Name: "Beef", ImageURL: "https://img.example/beef.png"},
		{ID: "a9", Key: "frozen spinach", Name: "Frozen Spinach"},
	}
	s := NewService(store)
	got, err := s.IngredientsByID(context.Background(), []string{"a1", "a3", "a5", "a7", "a9"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a1": spinach, "a3": thighs, "a5": mushrooms, "a7": "", "a9": ""}
	for _, ing := range got {
		if ing.ImageURL != want[ing.ID] {
			t.Errorf("%s: photo %q, want %q", ing.Name, ing.ImageURL, want[ing.ID])
		}
	}

	// By key too, which the protein choices and the pantry use.
	byKey, err := s.IngredientsByKey(context.Background(), []string{"baby spinach"})
	if err != nil || len(byKey) != 1 || byKey[0].ImageURL != spinach {
		t.Errorf("IngredientsByKey(baby spinach) = %+v, %v", byKey, err)
	}

	// The recipe page's ingredient rows.
	store.recipes = append(store.recipes, Recipe{ID: "r1", HouseholdID: "h1", Name: "Pesto Bow Tie Pasta",
		Ingredients: []RecipeIngredient{{IngredientID: "a1", Name: "Baby Spinach"}}})
	r, err := s.Get(context.Background(), "h1", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Ingredients[0].ImageURL != spinach {
		t.Errorf("Baby Spinach on the recipe page: %q, want Spinach's photo", r.Ingredients[0].ImageURL)
	}
}

// With no chicken thigh photo in the catalog at all (its only one is a dish,
// dropped), chicken thighs show raw chicken rather than the fallback glyph.
func TestChickenThighsWithNoPhotoShowRawChicken(t *testing.T) {
	store := newMemoryStore()
	store.ingredients = []Ingredient{{ID: "b1", Key: "diced chicken thighs", Name: "Diced Chicken Thighs"}}
	got, err := NewService(store).IngredientsByID(context.Background(), []string{"b1"})
	if err != nil || len(got) != 1 || got[0].ImageURL != rawChickenPhoto {
		t.Errorf("Diced Chicken Thighs = %+v, %v; want the raw chicken photo", got, err)
	}
	if IngredientPhoto(rawChickenPhoto) != rawChickenPhoto {
		t.Error("the raw chicken stand-in is itself dropped or swapped")
	}
}
