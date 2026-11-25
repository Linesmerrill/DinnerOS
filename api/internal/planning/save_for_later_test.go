package planning

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
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

// Two meals sharing an ingredient never each say to save some for the other
// (build 122, Hark's review item 4): the week's earlier meal keeps some back,
// and the later one just uses its own share.
func TestSaveForLaterOnlyKeepsForLaterMeals(t *testing.T) {
	ctx := context.Background()
	svc, _ := settleService(t)
	svc.events = nil
	mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeTacos, Day: "mon", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeSoup, Day: "thu", Servings: 2})

	first, err := svc.SaveForLater(ctx, hhAda, recipeTacos)
	if err != nil {
		t.Fatal(err)
	}
	if got := notesByName(first)["Garlic"].Text; got != "Use 2 cloves. Save 3 cloves for Onion Soup on Thursday." {
		t.Errorf("tacos garlic = %q", got)
	}
	later, err := svc.SaveForLater(ctx, hhAda, recipeSoup)
	if err != nil {
		t.Fatal(err)
	}
	if len(later) != 0 {
		t.Errorf("soup notes = %+v, want none: Monday's tacos come first", later)
	}
}

// An ingredient left out of the meal being cooked, just this once or for
// this dish, is nothing to save from: the meal doesn't use it (build 123,
// Merrill's iPad review). A later meal that uses it still gets all of it,
// and its own notes still say what to keep for the meals after it.
func TestSaveForLaterLeavesOutWhatThisMealSkips(t *testing.T) {
	ctx := context.Background()
	svc, _ := settleService(t)
	svc.events = nil
	svc.WithSkips(&fakeSkips{recipes: map[string]map[string]bool{recipeTacos: {ingOnion: true}}})
	mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeTacos, Day: "mon", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeSoup, Day: "thu", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeSalad, Day: "fri", Servings: 2})

	tacos, err := svc.SaveForLater(ctx, hhAda, recipeTacos)
	if err != nil {
		t.Fatal(err)
	}
	got := notesByName(tacos)
	if n, ok := got["Yellow Onion"]; ok {
		t.Errorf("tacos skip the onion, but say %q", n.Text)
	}
	if got["Garlic"].Text != "Use 2 cloves. Save 3 cloves for Onion Soup on Thursday." {
		t.Errorf("garlic = %q", got["Garlic"].Text)
	}
	soup, err := svc.SaveForLater(ctx, hhAda, recipeSoup)
	if err != nil {
		t.Fatal(err)
	}
	if got := notesByName(soup)["Yellow Onion"].Text; got != "Use 8 oz. Save ½ Yellow Onion for Chicken Salad on Friday." {
		t.Errorf("soup onion = %q, want the soup's whole share and the salad's", got)
	}
}

// Skipped for every dish isn't used by this meal or any other, so there's
// nothing to save. Only not bought this week is still at home and cooked
// with, so it still has to last.
func TestSaveForLaterSkipsWhatEveryDishLeavesOut(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		scope grocery.SkipScope
		want  string
	}{
		{grocery.SkipAlways, ""},
		{grocery.SkipThisWeek, "Use 2 cloves. Save 3 cloves for Onion Soup on Thursday."},
	} {
		svc, _ := settleService(t)
		svc.events = nil
		svc.WithSkips(&fakeSkips{byWeek: map[string]grocery.SkipSet{"2026-W47": {ingGarlic: tc.scope}}})
		mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeTacos, Day: "mon", Servings: 2})
		mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeSoup, Day: "thu", Servings: 2})
		notes, err := svc.SaveForLater(ctx, hhAda, recipeTacos)
		if err != nil {
			t.Fatal(err)
		}
		if got := notesByName(notes)["Garlic"].Text; got != tc.want {
			t.Errorf("%s skip: garlic = %q, want %q", tc.scope, got, tc.want)
		}
	}
}

func countShare(t *testing.T, recipeID, recipeName, quantity string) grocery.Share {
	t.Helper()
	q, err := ingredients.ParseQuantity(quantity)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := ingredients.LookupUnit("count")
	if err != nil {
		t.Fatal(err)
	}
	return grocery.Share{Source: grocery.Source{RecipeID: recipeID, RecipeName: recipeName}, Amounts: []grocery.Amount{{Quantity: q, Unit: unit}}}
}

// A count with no unit reads the way a person says it (build 123, Merrill's
// iPad review): a meal kit's pack of Crispy Fried Onions is "1 of the 2
// packs", never "1 Crispy Fried Onions"; one pack split is "half the pack"
// and "the other half"; a container in the name is its own word; and things
// counted one by one take the right singular or plural.
func TestSaveForLaterCountsReadNaturally(t *testing.T) {
	meals := mealOrder{"a": {day: Monday, order: 0}, "b": {day: Wednesday, order: 1}, "c": {day: Thursday, order: 2}}
	later := []string{"Pork Bowls", "Fish Tacos"}
	for _, tc := range []struct {
		name  string
		mine  string
		later []string
		text  string
		short string
	}{
		{"Crispy Fried Onions", "1", []string{"1"}, "Use 1 of the 2 packs. Save 1 pack for Pork Bowls on Wednesday.", "Save 1 pack for Pork Bowls"},
		{"Crispy Fried Onions", "1", []string{"2"}, "Use 1 of the 3 packs. Save 2 packs for Pork Bowls on Wednesday.", "Save 2 packs for Pork Bowls"},
		{"Crispy Fried Onions", "1/2", []string{"1/2"}, "Use half the pack. Save the other half for Pork Bowls on Wednesday.", "Save the other half for Pork Bowls"},
		{"Crispy Fried Onions", "1", []string{"1/2"}, "Use 1 pack. Save half a pack for Pork Bowls on Wednesday.", "Save half a pack for Pork Bowls"},
		{"Crispy Fried Onions", "1", []string{"1", "1"}, "Use 1 of the 3 packs. Save 1 pack for Pork Bowls on Wednesday and 1 pack for Fish Tacos on Thursday.", "Save some for later meals"},
		{"Marinara Cup", "1", []string{"1"}, "Use 1 of the 2 cups. Save 1 cup for Pork Bowls on Wednesday.", "Save 1 cup for Pork Bowls"},
		{"Apricot Jam", "1", []string{"1"}, "Use 1 of the 2 packs. Save 1 pack for Pork Bowls on Wednesday.", "Save 1 pack for Pork Bowls"},
		{"Lemon", "1", []string{"2"}, "Use 1 Lemon. Save 2 Lemons for Pork Bowls on Wednesday.", "Save 2 Lemons for Pork Bowls"},
		{"Scallions", "1", []string{"2"}, "Use 1 Scallion. Save 2 Scallions for Pork Bowls on Wednesday.", "Save 2 Scallions for Pork Bowls"},
		{"Yellow Onion", "1/2", []string{"1/2"}, "Use ½ Yellow Onion. Save ½ Yellow Onion for Pork Bowls on Wednesday.", "Save ½ Yellow Onion for Pork Bowls"},
		{"Roma Tomato", "1", []string{"3"}, "Use 1 Roma Tomato. Save 3 Roma Tomatoes for Pork Bowls on Wednesday.", "Save 3 Roma Tomatoes for Pork Bowls"},
	} {
		item := grocery.Item{IngredientKey: "k", Name: tc.name, Shares: []grocery.Share{countShare(t, "a", "Stir-Fry Tacos", tc.mine)}}
		for i, q := range tc.later {
			item.Shares = append(item.Shares, countShare(t, string(rune('b'+i)), later[i], q))
		}
		note, ok := saveForLaterNote(item, "a", meals, nil)
		if !ok {
			t.Errorf("%s: no note", tc.name)
			continue
		}
		if note.Text != tc.text || note.Short != tc.short {
			t.Errorf("%s %s+%v:\n got %q / %q\nwant %q / %q", tc.name, tc.mine, tc.later, note.Text, note.Short, tc.text, tc.short)
		}
	}
}
