package recipes

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// runIngredientUseContract checks FindIngredientUse against a service whose
// household hh is empty and otherHH is another household.
func runIngredientUseContract(t *testing.T, svc *Service, hh, otherHH string) {
	t.Helper()
	ctx := context.Background()
	garlicOnly := testRecipe("r-garlic", "Garlic Toast")
	garlicOnly.Ingredients = garlicOnly.Ingredients[:1]
	// A recipe that lists garlic twice still counts it once.
	twice := testRecipe("r-twice", "Double Garlic")
	twice.Ingredients = append(twice.Ingredients, twice.Ingredients[0])
	twice.Ingredients[2].SourceIngredientID = "ing-garlic"
	mustImport(t, svc, hh, testFile(testRecipe("r-both", "Tacos"), garlicOnly, twice))
	mustImport(t, svc, otherHH, testFile(testRecipe("r-other", "Elsewhere")))

	found, err := svc.IngredientsByKey(ctx, []string{"garlic", "salt"})
	if err != nil || len(found) != 2 {
		t.Fatalf("IngredientsByKey() = %+v, %v", found, err)
	}
	ids := map[string]string{}
	for _, ing := range found {
		ids[ing.Key] = ing.ID
	}
	garlic, salt := ids["garlic"], ids["salt"]

	uses, err := svc.FindIngredientUse(ctx, hh, []string{salt, "not-an-id", garlic})
	if err != nil {
		t.Fatalf("FindIngredientUse() error = %v", err)
	}
	if len(uses) != 3 || !slices.IsSortedFunc(uses, func(a, b IngredientUse) int {
		if a.RecipeID < b.RecipeID {
			return -1
		}
		return 1
	}) {
		t.Fatalf("uses = %+v, want 3 ordered by recipe ID", uses)
	}
	var counts = map[string]int{}
	both := []string{garlic, salt}
	slices.Sort(both)
	for _, u := range uses {
		for _, id := range u.IngredientIDs {
			counts[id]++
		}
		if len(u.IngredientIDs) == 2 && !reflect.DeepEqual(u.IngredientIDs, both) {
			t.Errorf("use %+v, want sorted IDs %v", u, both)
		}
	}
	if counts[garlic] != 3 || counts[salt] != 2 {
		t.Errorf("recipes per ingredient = %v, want garlic 3 and salt 2", counts)
	}

	if uses, err := svc.FindIngredientUse(ctx, hh, []string{salt}); err != nil || len(uses) != 2 {
		t.Errorf("salt only = %+v, %v", uses, err)
	}
	if uses, err := svc.FindIngredientUse(ctx, hh, nil); err != nil || uses != nil {
		t.Errorf("no ids = %+v, %v", uses, err)
	}
	if uses, err := svc.FindIngredientUse(ctx, bson.NewObjectID().Hex(), []string{garlic}); err != nil || len(uses) != 0 {
		t.Errorf("empty household = %+v, %v", uses, err)
	}
	if _, err := svc.FindIngredientUse(ctx, "", []string{garlic}); err == nil {
		t.Error("no household: error = nil")
	}
}

func TestFindIngredientUse(t *testing.T) {
	runIngredientUseContract(t, NewService(newMemoryStore()), "hh", "other-hh")
}

func TestIntegrationFindIngredientUse(t *testing.T) {
	svc, _, _ := newTestMongoService(t)
	runIngredientUseContract(t, svc, bson.NewObjectID().Hex(), bson.NewObjectID().Hex())
}
