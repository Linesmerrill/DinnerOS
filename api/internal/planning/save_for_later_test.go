package planning

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func notesByName(notes []recipes.SaveForLater) map[string]recipes.SaveForLater {
	out := map[string]recipes.SaveForLater{}
	for _, n := range notes {
		out[n.Name] = n
	}
	return out
}

// Cooking Monday's tacos says how much of what the week bought goes in and
// what to keep for the meals still to come (decision 663): the garlic and
// sour cream Thursday's soup needs, the half onion Friday's salad needs.
// Salt is a staple and needs no note.
func TestSaveForLaterNamesWhatLaterMealsNeed(t *testing.T) {
	ctx := context.Background()
	svc, _ := settleService(t)
	mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeTacos, Day: "mon", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeSoup, Day: "thu", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeSalad, Day: "fri", Servings: 2})

	notes, err := svc.SaveForLater(ctx, hhAda, recipeTacos)
	if err != nil {
		t.Fatal(err)
	}
	got := notesByName(notes)
	want := map[string]string{
		"Garlic":       "Use 2 cloves. Save 3 cloves for Onion Soup on Thursday.",
		"Sour Cream":   "Use 2 Tbsp. Save ¼ cup for Onion Soup on Thursday.",
		"Yellow Onion": "Use ½ Yellow Onion. Save 8 oz for Onion Soup on Thursday and ½ Yellow Onion for Chicken Salad on Friday.",
	}
	if len(got) != len(want) {
		t.Fatalf("notes = %+v, want %d", notes, len(want))
	}
	for name, text := range want {
		if got[name].Text != text {
			t.Errorf("%s: %q, want %q", name, got[name].Text, text)
		}
	}
	if s := got["Garlic"].Short; s != "Save 3 cloves for Onion Soup" {
		t.Errorf("garlic short = %q", s)
	}
	if s := got["Yellow Onion"].Short; s != "Save some for later meals" {
		t.Errorf("onion short = %q", s)
	}
	if keys := got["Garlic"].Keys; len(keys) != 1 || keys[0] != ingGarlic {
		t.Errorf("garlic keys = %v", keys)
	}
}

// A meal someone already made or said they didn't make needs nothing kept,
// and a recipe that isn't planned near now has no notes.
func TestSaveForLaterIgnoresAnsweredMeals(t *testing.T) {
	ctx := context.Background()
	svc, log := settleService(t)
	mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 2})
	_, soup := mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeSoup, Day: "mon", Servings: 2})
	_, salad := mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeSalad, Day: "fri", Servings: 2})
	log.answer(soup.ID, true, time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC))
	log.answer(salad.ID, false, time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC))

	notes, err := svc.SaveForLater(ctx, hhAda, recipeTacos)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %+v, want none", notes)
	}
	notes, err = svc.SaveForLater(ctx, hhAda, recipeBobs)
	if err != nil || len(notes) != 0 {
		t.Errorf("unplanned recipe: %+v, %v", notes, err)
	}
}

// Last week's meal cooked late still keeps some for last week's other meals,
// and a recipe planned twice uses one meal's amount at a time.
func TestSaveForLaterFindsTheMealsWeekAndCooksOneAtATime(t *testing.T) {
	ctx := context.Background()
	svc, _ := settleService(t)
	svc.events = nil // nothing settled: the weeks keep their meals unanswered
	mustAdd(t, svc, hhAda, userAda, "2026-W46", NewEntry{RecipeID: recipeTacos, Day: "fri", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W46", NewEntry{RecipeID: recipeTacos, Day: "sat", Servings: 2})

	notes, err := svc.SaveForLater(ctx, hhAda, recipeTacos)
	if err != nil {
		t.Fatal(err)
	}
	// The second tacos night is the same recipe: it isn't a later meal to
	// save for, and this night's amount is one night's.
	if len(notes) != 0 {
		t.Errorf("notes = %+v, want none", notes)
	}

	mustAdd(t, svc, hhAda, userAda, "2026-W46", NewEntry{RecipeID: recipeSalad, Day: "sun", Servings: 2})
	notes, err = svc.SaveForLater(ctx, hhAda, recipeTacos)
	if err != nil {
		t.Fatal(err)
	}
	onion := notesByName(notes)["Yellow Onion"]
	if !strings.HasPrefix(onion.Text, "Use ½ Yellow Onion.") || !strings.Contains(onion.Text, "Chicken Salad on Sunday") {
		t.Errorf("onion = %q", onion.Text)
	}
}
