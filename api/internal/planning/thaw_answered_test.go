package planning

import (
	"context"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
)

// swapTo replaces every line of a customized entry's meal with key: the
// protein the member swapped in.
type swapTo struct{ key, name string }

func (s swapTo) CustomizeGrocery(_ context.Context, _ string, entries []Entry, sel []grocery.RecipeSelection) ([]grocery.RecipeSelection, error) {
	for i, e := range entries {
		if len(e.Customizations) == 0 {
			continue
		}
		for j := range sel[i].Lines {
			sel[i].Lines[j].IngredientKey, sel[i].Lines[j].Name = s.key, s.name
		}
	}
	return sel, nil
}

// A meal already marked cooked needs nothing thawed: a "take it out"
// reminder after dinner was made is noise (decision 636).
func TestThawSkipsACookedMeal(t *testing.T) {
	svc, notifier := thawService(t, nil, frozenChicken())
	_, entry := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "tue", Servings: 2})
	entryID := entry.ID
	svc.WithOutcomes(&fakeOutcomes{list: []events.Event{{Type: events.TypeRecipeCooked, Payload: events.RecipeCooked{EntryID: entryID}}}})
	due, err := svc.ThawDue(context.Background(), hhAda)
	if err != nil || len(due.Items) != 0 {
		t.Fatalf("due = %+v, %v; want nothing for a cooked meal", due.Items, err)
	}
	if err := svc.Refresh(context.Background(), hhAda); err != nil || len(notifier.created) != 0 {
		t.Errorf("refresh created %+v, %v", notifier.created, err)
	}
}

// A swapped protein is the bag to thaw: the card's thighs were swapped for
// the chicken breast strips in the freezer (decision 636).
func TestThawFollowsAProteinSwap(t *testing.T) {
	strips := frozenChicken()
	strips.Item.ID, strips.Item.DisplayName, strips.Item.IngredientID = "66e5a1f2c3b4a5d6e7f84002", "Chicken Breast Strips", "ing-strips"
	strips.Keys = []string{"ing-strips"}
	svc, _ := thawService(t, nil, strips)
	svc.WithCustomizations(swapTo{key: "ing-strips", name: "Chicken Breast Strips"})
	_, entry := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "tue", Servings: 2})
	entryID := entry.ID
	if _, err := svc.SetEntryCustomizations(context.Background(), hhAda, testWeek, entryID, []Customization{{IngredientKey: ingChicken, ChoiceID: "swap:chicken-breast-strips", Label: "Chicken Breast Strips"}}); err != nil {
		t.Fatal(err)
	}
	due, err := svc.ThawDue(context.Background(), hhAda)
	if err != nil || len(due.Items) != 1 || due.Items[0].Name != "Chicken Breast Strips" {
		t.Fatalf("due = %+v, %v; want the swapped-in strips", due.Items, err)
	}
}

// A bag that had to go in this morning gets a reminder in the morning, and
// none once it's too late to thaw by dinner: "Move it to the fridge this
// morning" at 7 PM is wrong (decision 636).
func TestThawReminderStopsWhenItsTooLate(t *testing.T) {
	big := frozenChicken()
	big.Item.Quantity, big.Item.Portions = "96", 1
	big.Thaw = pantry.ThawFor(big.Item)
	svc, _ := thawService(t, nil, big)
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "tue", Servings: 2})

	// 8 AM in Denver: past the move-by time, so "this morning".
	svc.now = func() time.Time { return time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC) }
	due, err := svc.ThawDue(context.Background(), hhAda)
	if err != nil || len(due.Items) != 1 || !due.Items[0].Overnight {
		t.Fatalf("morning due = %+v, %v; want the bag, this morning", due.Items, err)
	}
	// 7 PM: too late to thaw by dinner.
	svc.now = func() time.Time { return time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC) }
	if due, err := svc.ThawDue(context.Background(), hhAda); err != nil || len(due.Items) != 0 {
		t.Errorf("evening due = %+v, %v; want nothing", due.Items, err)
	}
}
