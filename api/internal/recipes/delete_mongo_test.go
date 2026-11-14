package recipes

import (
	"context"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestIntegrationDeleteAndRestore(t *testing.T) {
	svc, store, _ := newTestMongoService(t)
	plans := &fakePlans{planned: map[string][]RemovedPlanEntry{}}
	svc.WithPlans(plans)
	ctx := context.Background()
	hh := bson.NewObjectID().Hex()
	mustImport(t, svc, hh, testFile(testRecipe("r1", "Tacos", "2026-W10"), testRecipe("r2", "Noodles")))
	tacos := recipeByName(t, svc, hh, "Tacos")
	plans.planned[tacos.ID] = []RemovedPlanEntry{{Week: "2026-W47", Day: "thu", Servings: 2, Note: "extra salsa", Origin: "manual"}}

	if _, err := svc.Delete(ctx, hh, "u1", tacos.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetRecipe(ctx, hh, tacos.ID)
	if err != nil || stored.DeletedAt.IsZero() || len(stored.RemovedPlanEntries) != 1 || stored.RemovedPlanEntries[0].Note != "extra salsa" {
		t.Fatalf("stored after delete = %+v, %v", stored, err)
	}
	if page, _ := svc.List(ctx, hh, ListQuery{}); len(page.Items) != 1 || page.Items[0].Name != "Noodles" {
		t.Errorf("list = %+v", page.Items)
	}
	for name, list := range map[string]func(context.Context, string) ([]Recipe, error){"catalog": svc.Catalog, "menu catalog": svc.MenuCatalog} {
		if got, err := list(ctx, hh); err != nil || !slices.Equal(names(got), []string{"Noodles"}) {
			t.Errorf("%s = %v, %v", name, names(got), err)
		}
	}
	if after, _ := store.ListRecipesAfter(ctx, hh, "", 10); !slices.Equal(names(after), []string{"Noodles"}) {
		t.Errorf("list after = %v", names(after))
	}
	if keys, _ := store.LibraryCatalogKeys(ctx, hh, []string{stored.CatalogKey}); len(keys) != 0 {
		t.Errorf("library keys = %v", keys)
	}
	if uses, _ := store.FindIngredientUse(ctx, hh, []string{stored.Ingredients[0].IngredientID}); len(uses) != 1 || uses[0].RecipeID == tacos.ID {
		t.Errorf("ingredient use = %+v", uses)
	}
	// History still reads it, and a re-import keeps it deleted.
	if got, _ := store.GetRecipes(ctx, hh, []string{tacos.ID}); len(got) != 1 {
		t.Errorf("get recipes = %v", names(got))
	}
	if ids, _ := store.ExistingRecipeIDs(ctx, hh, []string{tacos.ID}); len(ids) != 1 {
		t.Errorf("existing ids = %v", ids)
	}
	mustImport(t, svc, hh, testFile(testRecipe("r1", "Tacos", "2026-W10", "2026-W20"), testRecipe("r2", "Noodles")))
	if again, _ := store.GetRecipe(ctx, hh, tacos.ID); again.DeletedAt.IsZero() || again.TimesOrdered != 2 {
		t.Errorf("after re-import = deleted %v, ordered %d", again.DeletedAt, again.TimesOrdered)
	}

	back, err := svc.Restore(ctx, hh, "u1", tacos.ID)
	if err != nil || !back.DeletedAt.IsZero() || back.RemovedPlanEntries != nil {
		t.Fatalf("restore = %+v, %v", back, err)
	}
	if len(plans.restored) != 1 || plans.restored[0].Day != "thu" {
		t.Errorf("restored meals = %+v", plans.restored)
	}
	if page, _ := svc.List(ctx, hh, ListQuery{}); len(page.Items) != 2 {
		t.Errorf("list after restore = %+v", page.Items)
	}
	if _, err := store.SetRecipeDeleted(ctx, hh, bson.NewObjectID().Hex(), time.Now(), nil, time.Now()); err != ErrNotFound {
		t.Errorf("missing recipe = %v; want ErrNotFound", err)
	}
	if _, err := store.SetRecipeDeleted(ctx, bson.NewObjectID().Hex(), tacos.ID, time.Now(), nil, time.Now()); err != ErrNotFound {
		t.Errorf("other household = %v; want ErrNotFound", err)
	}
}
