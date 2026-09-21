package planning

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestReplaceEntryRecipe(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	_, tacos := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 4, Note: "extra lime"})

	before, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}

	p, previous, next, err := svc.ReplaceEntryRecipe(ctx, hhAda, testWeek, tacos.ID, recipeSoup, OriginAutopilot)
	if err != nil {
		t.Fatalf("ReplaceEntryRecipe() error = %v", err)
	}
	if previous.RecipeID != recipeTacos || previous.RecipeName != "Beef Tacos" {
		t.Errorf("previous = %+v", previous)
	}
	switch {
	case next.ID != tacos.ID:
		t.Errorf("the swap moved the meal to a new entry: %s", next.ID)
	case next.RecipeID != recipeSoup || next.RecipeName != "Onion Soup":
		t.Errorf("entry = %+v; want the new recipe's snapshot", next)
	case next.Day != Tuesday || next.Servings != 4 || next.Note != "extra lime":
		t.Errorf("the swap changed the day, servings, or note: %+v", next)
	case next.Origin != OriginAutopilot || next.ProposalID != "":
		t.Errorf("origin = %s, proposal = %q", next.Origin, next.ProposalID)
	case len(p.Entries) != 1:
		t.Errorf("the week gained or lost meals: %+v", p.Entries)
	}

	// The grocery list is derived from the plan, so it follows.
	after, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if listSummary(before) == listSummary(after) {
		t.Errorf("the grocery list didn't change with the meal: %s", listSummary(after))
	}
}

// listSummary is a grocery list's items and amounts, for comparing two lists.
func listSummary(l GroceryList) string {
	var out []string
	for _, c := range l.Categories {
		for _, it := range c.Items {
			for _, a := range it.Amounts {
				out = append(out, fmt.Sprintf("%s %v %v", it.Name, a.Quantity, a.Unit))
			}
		}
	}
	slices.Sort(out)
	return strings.Join(out, ", ")
}

func TestReplaceEntryRecipeServingsAndRefusals(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	_, soup := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Day: "wed", Servings: 4})

	// The salad is only authored for 2, so the entry takes the nearest size.
	_, _, next, err := svc.ReplaceEntryRecipe(ctx, hhAda, testWeek, soup.ID, recipeSalad, OriginAutopilot)
	if err != nil {
		t.Fatalf("ReplaceEntryRecipe() error = %v", err)
	}
	if next.Servings != 2 {
		t.Errorf("servings = %d; want the nearest authored size", next.Servings)
	}

	if _, _, _, err := svc.ReplaceEntryRecipe(ctx, hhAda, testWeek, soup.ID, recipeSalad, OriginAutopilot); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("replacing a meal with itself error = %v", err)
	}
	if _, _, _, err := svc.ReplaceEntryRecipe(ctx, hhAda, testWeek, "ffffffffffffffffffffffff", recipeTacos, OriginAutopilot); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown entry error = %v", err)
	}
	if _, _, _, err := svc.ReplaceEntryRecipe(ctx, hhAda, testWeek, soup.ID, recipeBobs, OriginAutopilot); !errors.Is(err, ErrRecipeNotFound) {
		t.Errorf("another household's recipe error = %v", err)
	}
	if _, _, _, err := svc.ReplaceEntryRecipe(ctx, hhAda, testWeek, soup.ID, "", OriginAutopilot); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("missing recipe error = %v", err)
	}

	// A finalized week is locked: the grocery list is already out.
	if _, err := svc.SetStatus(ctx, hhAda, testWeek, "finalized"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.ReplaceEntryRecipe(ctx, hhAda, testWeek, soup.ID, recipeTacos, OriginAutopilot); !errors.Is(err, ErrFinalized) {
		t.Errorf("finalized week error = %v", err)
	}
}

func TestIntegrationReplaceEntryRecipe(t *testing.T) {
	store, db := newTestMongoStore(t)
	svc, _ := newTestService(t, store)
	ctx := context.Background()
	_, tacos := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 2, Note: "extra lime"})
	if _, err := store.UpdateEntry(ctx, hhAda, mustWeek(t, testWeek), tacos.ID, EntryChanges{
		Customizations: &[]Customization{{IngredientKey: "sour-cream", ChoiceID: "double", Label: "2x Sour Cream"}},
	}, testNow); err != nil {
		t.Fatal(err)
	}
	_, previous, next, err := svc.ReplaceEntryRecipe(ctx, hhAda, testWeek, tacos.ID, recipeSoup, OriginAutopilot)
	if err != nil {
		t.Fatalf("ReplaceEntryRecipe() error = %v", err)
	}
	if previous.RecipeID != recipeTacos || next.RecipeID != recipeSoup || next.ID != tacos.ID {
		t.Fatalf("previous = %+v, next = %+v", previous, next)
	}
	if len(next.Customizations) != 0 {
		t.Errorf("the old recipe's customizations survived the swap: %+v", next.Customizations)
	}
	raw := loadRawPlan(t, db, hhAda, testWeek)
	if raw.Entries[0]["recipeId"] != objectID(t, recipeSoup) || raw.Entries[0]["recipeName"] != "Onion Soup" {
		t.Errorf("raw entry = %v", raw.Entries[0])
	}
	if raw.Entries[0]["origin"] != "autopilot" || raw.Entries[0]["note"] != "extra lime" {
		t.Errorf("raw entry = %v", raw.Entries[0])
	}
	for _, field := range []string{"customizations", "recipeImageUrl", "proposalId"} {
		if _, has := raw.Entries[0][field]; has {
			t.Errorf("%s is still stored: %v", field, raw.Entries[0])
		}
	}
	// A finalized week can't be swapped, even at the store.
	if _, err := svc.SetStatus(ctx, hhAda, testWeek, "finalized"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.ReplaceEntryRecipe(ctx, hhAda, testWeek, tacos.ID, recipeTacos, OriginAutopilot); !errors.Is(err, ErrFinalized) {
		t.Errorf("ReplaceEntryRecipe(finalized) error = %v", err)
	}
}

func TestNearestServings(t *testing.T) {
	r := saladRecipe()
	r.Servings = []int{2, 4, 6}
	for _, tc := range []struct{ want, got int }{{2, nearestServings(r, 2)}, {4, nearestServings(r, 4)}, {2, nearestServings(r, 1)}, {6, nearestServings(r, 9)}, {2, nearestServings(r, 3)}} {
		if tc.got != tc.want {
			t.Errorf("nearestServings = %d; want %d", tc.got, tc.want)
		}
	}
	r.Servings = nil
	if got := nearestServings(r, 4); got != 0 {
		t.Errorf("a recipe with no serving sizes = %d", got)
	}
}
