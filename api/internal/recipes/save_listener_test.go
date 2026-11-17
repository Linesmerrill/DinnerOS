package recipes

import (
	"context"
	"testing"
)

type saveCounter map[string]int

func (s saveCounter) RecipesSaved(_ context.Context, householdID string, n int) { s[householdID] += n }

// Typing or importing a recipe tells the listener; saving the same one again
// doesn't, and neither does the starter library.
func TestSavingRecipesTellsTheListener(t *testing.T) {
	store := newMemoryStore()
	heard := saveCounter{}
	service := NewService(store).WithSaveListener(heard)
	if _, err := service.CreateFromDraft(t.Context(), hhAda, ParseText(pastedRecipe), false); err != nil {
		t.Fatal(err)
	}
	if heard[hhAda] != 1 {
		t.Fatalf("heard %d, want 1", heard[hhAda])
	}
	if _, err := service.CreateFromDraft(t.Context(), hhAda, ParseText(pastedRecipe), false); err != nil {
		t.Fatal(err)
	}
	if heard[hhAda] != 2 {
		t.Errorf("heard %d after a second typed recipe, want 2", heard[hhAda])
	}
	// The starter library copies hhAda's recipes into hhBob.
	if _, err := service.CopyStarterRecipes(t.Context(), hhAda, hhBob, true); err != nil {
		t.Fatal(err)
	}
	if heard[hhBob] != 0 {
		t.Errorf("starter recipes counted: %d", heard[hhBob])
	}
}

// A recipe picked from Shared Recipes counts once: adding it again, or
// getting a deleted copy back, does not count again (decision 707).
func TestAddingFromSharedRecipesTellsTheListenerOnce(t *testing.T) {
	ctx := t.Context()
	heard := saveCounter{}
	service, _, _ := newSharingService(t)
	service.WithSaveListener(heard)
	source := importOne(t, service, hhAda, SourceHelloFresh, "hf-1", "Harissa Chicken")
	heard[hhAda] = 0
	content := source
	content.ID, content.HouseholdID = "", ""

	stored, created, err := service.AddFromCatalog(ctx, hhBob, content)
	if err != nil || !created {
		t.Fatalf("AddFromCatalog = created %v, %v", created, err)
	}
	if heard[hhBob] != 1 {
		t.Fatalf("heard %d after a new copy, want 1", heard[hhBob])
	}
	if _, _, err := service.AddFromCatalog(ctx, hhBob, content); err != nil {
		t.Fatal(err)
	}
	if heard[hhBob] != 1 {
		t.Errorf("heard %d after adding the same recipe again, want 1", heard[hhBob])
	}
	if _, err := service.Delete(ctx, hhBob, "user-bob", stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, created, err := service.AddFromCatalog(ctx, hhBob, content); err != nil || !created {
		t.Fatalf("add after delete = created %v, %v", created, err)
	}
	if heard[hhBob] != 1 {
		t.Errorf("heard %d after getting a deleted copy back, want 1", heard[hhBob])
	}
	// A recipe the household already imported itself doesn't count again.
	if _, _, err := service.AddFromCatalog(ctx, hhAda, content); err != nil {
		t.Fatal(err)
	}
	if heard[hhAda] != 0 {
		t.Errorf("heard %d for a recipe the household already had, want 0", heard[hhAda])
	}
}
