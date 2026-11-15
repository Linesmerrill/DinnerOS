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
