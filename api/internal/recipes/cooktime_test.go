package recipes

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestCookMinutes covers the shapes seen in real imported catalogs, where the
// source's prep and total fields are inconsistent or swapped.
func TestCookMinutes(t *testing.T) {
	tests := []struct {
		name        string
		prep, total int
		want        int
	}{
		{"consistent", 10, 30, 30},
		{"total below prep (rigatoni shape)", 20, 5, 20},
		{"total below prep (tacos shape)", 30, 10, 30},
		{"prep only (garlic bread shape)", 15, 0, 15},
		{"total only", 0, 25, 25},
		{"equal", 20, 20, 20},
		{"unknown", 0, 0, 0},
		{"negative values are ignored", -5, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CookMinutes(tt.prep, tt.total); got != tt.want {
				t.Errorf("CookMinutes(%d, %d) = %d, want %d", tt.prep, tt.total, got, tt.want)
			}
			r := Recipe{PrepMinutes: tt.prep, TotalMinutes: tt.total}
			s := RecipeSummary{PrepMinutes: tt.prep, TotalMinutes: tt.total}
			if r.CookMinutes() != tt.want || s.CookMinutes() != tt.want {
				t.Errorf("Recipe/RecipeSummary.CookMinutes() = %d/%d, want %d", r.CookMinutes(), s.CookMinutes(), tt.want)
			}
		})
	}
}

func TestCatalog(t *testing.T) {
	runCatalogContract(t, NewService(newMemoryStore()), "66e5a1f2c3b4a5d6e7f80a01", "66e5a1f2c3b4a5d6e7f80b01")
}

func TestIntegrationCatalog(t *testing.T) {
	svc, _, _ := newTestMongoService(t)
	runCatalogContract(t, svc, bson.NewObjectID().Hex(), bson.NewObjectID().Hex())
}

func runCatalogContract(t *testing.T, svc *Service, hh, other string) {
	t.Helper()
	ctx := context.Background()
	swapped := testRecipe("r-swapped", "Rigatoni")
	swapped.PrepMinutes, swapped.TotalMinutes = 20, 5
	prepOnly := testRecipe("r-prep", "Garlic Bread")
	prepOnly.PrepMinutes, prepOnly.TotalMinutes, prepOnly.IsAddon = 15, 0, true
	mustImport(t, svc, hh, testFile(swapped, prepOnly))
	mustImport(t, svc, other, testFile(testRecipe("r-other", "Stew")))

	list, err := svc.Catalog(ctx, hh)
	if err != nil || len(list) != 2 {
		t.Fatalf("Catalog() = %d recipes, %v; want the household's 2", len(list), err)
	}
	if list[0].ID > list[1].ID {
		t.Error("Catalog() is not ordered by ID")
	}
	byName := map[string]Recipe{}
	for _, r := range list {
		byName[r.Name] = r
	}
	rigatoni, bread := byName["Rigatoni"], byName["Garlic Bread"]
	if rigatoni.CookMinutes() != 20 || bread.CookMinutes() != 15 || !bread.IsAddon {
		t.Errorf("cook minutes = %d, %d (addon %v); want 20, 15", rigatoni.CookMinutes(), bread.CookMinutes(), bread.IsAddon)
	}
	if len(rigatoni.Steps) != 0 || len(rigatoni.Nutrition) != 0 {
		t.Error("Catalog() should leave out steps and nutrition")
	}
	if len(rigatoni.Ingredients) != 2 || rigatoni.Ingredients[0].Name == "" || len(rigatoni.Cuisines) == 0 || len(rigatoni.Servings) == 0 {
		t.Errorf("Catalog() dropped fields the recommender needs: %+v", rigatoni)
	}

	page, err := svc.List(ctx, hh, ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range page.Items {
		if want := byName[s.Name].CookMinutes(); s.CookMinutes() != want {
			t.Errorf("summary %q CookMinutes() = %d, want %d", s.Name, s.CookMinutes(), want)
		}
	}
	if _, err := svc.Catalog(ctx, ""); err == nil {
		t.Error("Catalog() accepted an empty household")
	}
}
