package events

import (
	"context"
	"strings"
	"testing"
)

func TestMealCustomizedValidation(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	ctx := context.Background()
	long := strings.Repeat("x", 201)
	tooMany := make([]CustomizationChange, MaxCustomizationChanges+1)
	for i := range tooMany {
		tooMany[i] = CustomizationChange{IngredientKey: "k", From: "original", To: "double"}
	}
	for _, tc := range []struct {
		name    string
		payload MealCustomized
		wantMsg string
	}{
		{"no entry", MealCustomized{Changes: []CustomizationChange{{IngredientKey: "k", From: "original", To: "double"}}}, "entryId is required"},
		{"no changes", MealCustomized{EntryID: "e1"}, "changes must list"},
		{"too many changes", MealCustomized{EntryID: "e1", Changes: tooMany}, "changes must list"},
		{"change without a key", MealCustomized{EntryID: "e1", Changes: []CustomizationChange{{From: "original", To: "double"}}}, "ingredientKey"},
		{"long key", MealCustomized{EntryID: "e1", Changes: []CustomizationChange{{IngredientKey: long, From: "original", To: "double"}}}, "ingredientKey"},
		{"change without from and to", MealCustomized{EntryID: "e1", Changes: []CustomizationChange{{IngredientKey: "k"}}}, "from and to"},
	} {
		err := svc.Record(ctx, Event{HouseholdID: hhA, UserID: userA, Type: TypeMealCustomized, RecipeID: recipeA, Payload: tc.payload})
		if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("%s: Record() error = %v, want %q", tc.name, err, tc.wantMsg)
		}
	}
	// meal.customized is server-observed and needs a recipe.
	if err := svc.Record(ctx, Event{HouseholdID: hhA, UserID: userA, Type: TypeMealCustomized,
		Payload: MealCustomized{EntryID: "e1", Changes: []CustomizationChange{{IngredientKey: "k", From: "original", To: "double"}}}}); err == nil {
		t.Error("Record() without a recipe succeeded")
	}
	for _, typ := range ClientTypes() {
		if typ == TypeMealCustomized {
			t.Error("clients may send meal.customized")
		}
	}
	if n := len(store.all()); n != 0 {
		t.Errorf("stored %d invalid events", n)
	}

	valid := Event{HouseholdID: hhA, UserID: userA, Type: TypeMealCustomized, RecipeID: recipeA, Week: "2026-W38",
		Payload: MealCustomized{EntryID: "e1", Changes: []CustomizationChange{
			{IngredientKey: "66e5a1f2c3b4a5d6e7f80a12", From: "original", To: "swap:ground-beef"},
			{IngredientKey: "name:shrimp", From: "double", To: "original"},
		}}}
	if err := svc.Record(ctx, valid); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	stored := store.all()
	if len(stored) != 1 {
		t.Fatalf("stored = %+v", stored)
	}
	p, ok := stored[0].Payload.(MealCustomized)
	if !ok || p.EntryID != "e1" || len(p.Changes) != 2 || p.Changes[0].To != "swap:ground-beef" {
		t.Errorf("stored payload = %+v", stored[0].Payload)
	}
	if stored[0].Source != SourceAPI || stored[0].Week != "2026-W38" {
		t.Errorf("stored event = %+v", stored[0])
	}
}

// ChangeFromOriginal is the choice a line has before it's customized.
const ChangeFromOriginal = "original"
