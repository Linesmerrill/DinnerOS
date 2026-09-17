package planning

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// fakeCustomizations records what the planner passes and applies apply to
// each selection.
type fakeCustomizations struct {
	entries    []Entry
	selections []grocery.RecipeSelection
	calls      int
	err        error
	apply      func(sel grocery.RecipeSelection, e Entry) grocery.RecipeSelection
}

func (f *fakeCustomizations) CustomizeGrocery(_ context.Context, _ string, entries []Entry, selections []grocery.RecipeSelection) ([]grocery.RecipeSelection, error) {
	f.calls++
	f.entries, f.selections = entries, selections
	if f.err != nil {
		return nil, f.err
	}
	out := slices.Clone(selections)
	if f.apply != nil {
		for i := range out {
			out[i] = f.apply(out[i], entries[i])
		}
	}
	return out, nil
}

func TestSetEntryCustomizations(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	_, entry := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	list := []Customization{{IngredientKey: ingOnion, ChoiceID: "swap:ground-beef", Label: "Ground Beef"}}
	p, err := svc.SetEntryCustomizations(ctx, hhAda, testWeek, entry.ID, list)
	if err != nil {
		t.Fatalf("SetEntryCustomizations() error = %v", err)
	}
	if got := p.Entries[0].Customizations; !slices.Equal(got, list) {
		t.Errorf("stored = %+v", got)
	}
	// The wire form carries them.
	if resp := NewEntryResponse(p, p.Entries[0]); len(resp.Customizations) != 1 || resp.Customizations[0].ChoiceID != "swap:ground-beef" {
		t.Errorf("entry response = %+v", resp.Customizations)
	}

	// An empty list resets the meal, and an uncustomized entry omits the field.
	p, err = svc.SetEntryCustomizations(ctx, hhAda, testWeek, entry.ID, nil)
	if err != nil {
		t.Fatalf("reset error = %v", err)
	}
	if got := p.Entries[0].Customizations; len(got) != 0 {
		t.Errorf("after the reset = %+v", got)
	}
	if resp := NewEntryResponse(p, p.Entries[0]); resp.Customizations != nil {
		t.Errorf("reset entry response = %+v", resp.Customizations)
	}

	if _, err := svc.SetEntryCustomizations(ctx, hhAda, testWeek, "66e5a1f2c3b4a5d6e7f89999", list); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown entry error = %v", err)
	}
	if _, err := svc.SetEntryCustomizations(ctx, hhAda, "nonsense", entry.ID, list); !errors.Is(err, ErrInvalidWeek) {
		t.Errorf("bad week error = %v", err)
	}
	if _, err := svc.SetStatus(ctx, hhAda, testWeek, string(StatusFinalized)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEntryCustomizations(ctx, hhAda, testWeek, entry.ID, list); !errors.Is(err, ErrFinalized) {
		t.Errorf("finalized error = %v", err)
	}
}

func TestFindEntry(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	_, entry := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	week := mustWeek(t, testWeek)

	// The meal was cooked in its own week.
	p, e, found, err := svc.FindEntry(ctx, hhAda, entry.ID, week.Monday().Add(48*time.Hour))
	if err != nil || !found || e.ID != entry.ID || p.Week != week {
		t.Fatalf("FindEntry() = %v, %v, %v, %v", p.Week, e.ID, found, err)
	}
	// Marked cooked weeks later, the entry is still found within the range.
	if _, _, found, err := svc.FindEntry(ctx, hhAda, entry.ID, week.AddWeeks(4).Monday()); err != nil || !found {
		t.Errorf("FindEntry(4 weeks later) = %v, %v", found, err)
	}
	// Outside the range, or in another household, it isn't.
	if _, _, found, err := svc.FindEntry(ctx, hhAda, entry.ID, week.AddWeeks(20).Monday()); err != nil || found {
		t.Errorf("FindEntry(20 weeks later) = %v, %v", found, err)
	}
	if _, _, found, err := svc.FindEntry(ctx, hhBob, entry.ID, week.Monday()); err != nil || found {
		t.Errorf("FindEntry(other household) = %v, %v", found, err)
	}
	if _, _, found, _ := svc.FindEntry(ctx, hhAda, "66e5a1f2c3b4a5d6e7f89999", week.Monday()); found {
		t.Error("FindEntry(unknown) found something")
	}
}

func TestGroceryListAppliesCustomizations(t *testing.T) {
	ctx := context.Background()
	svc, reader := newTestService(t, newMemoryStore())
	fake := &fakeCustomizations{}
	svc.WithCustomizations(fake)
	_, tacos := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	_, salad := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Servings: 2})

	// Without a customized entry the source isn't asked.
	if _, err := svc.GroceryList(ctx, hhAda, testWeek); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 {
		t.Errorf("source called %d times for an uncustomized week", fake.calls)
	}

	// The entries passed line up with the selections, skipped entries aside.
	if _, err := svc.SetEntryCustomizations(ctx, hhAda, testWeek, salad.ID, []Customization{{IngredientKey: ingChicken, ChoiceID: "double", Label: "2x Chicken Breast"}}); err != nil {
		t.Fatal(err)
	}
	reader.remove(recipeTacos)
	if _, err := svc.GroceryList(ctx, hhAda, testWeek); err != nil {
		t.Fatal(err)
	}
	if len(fake.entries) != 1 || fake.entries[0].ID != salad.ID || len(fake.selections) != 1 || fake.selections[0].RecipeID != recipeSalad {
		t.Fatalf("entries = %+v, selections = %+v", fake.entries, fake.selections)
	}
	reader.put(tacosRecipe())
	if _, err := svc.GroceryList(ctx, hhAda, testWeek); err != nil {
		t.Fatal(err)
	}
	if len(fake.entries) != 2 {
		t.Fatalf("entries = %+v", fake.entries)
	}
	for i, e := range fake.entries {
		if e.RecipeID != fake.selections[i].RecipeID {
			t.Errorf("entry %d (%s) doesn't match selection %s", i, e.RecipeID, fake.selections[i].RecipeID)
		}
	}
	_ = tacos

	// A failure fails the list.
	fake.err = errors.New("boom")
	if _, err := svc.GroceryList(ctx, hhAda, testWeek); err == nil || !errors.Is(err, fake.err) {
		t.Errorf("GroceryList() error = %v", err)
	}
}

// TestGroceryCustomizedVia renders the provenance the app shows.
func TestGroceryCustomizedVia(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	fake := &fakeCustomizations{apply: func(sel grocery.RecipeSelection, _ Entry) grocery.RecipeSelection {
		out := sel
		out.Lines = slices.Clone(sel.Lines)
		for i, l := range out.Lines {
			if l.IngredientKey != ingChicken {
				continue
			}
			out.Lines[i].IngredientKey, out.Lines[i].Name = "name:ground beef", "Ground Beef"
			out.Lines[i].Via = &grocery.Via{
				Kind: grocery.ViaCustomized, SpecialtyKey: ingChicken, SpecialtyName: "Chicken Breast",
				OptionID: "swap:ground-beef", OptionName: "Ground Beef",
			}
		}
		return out
	}}
	svc.WithCustomizations(fake)
	_, salad := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Servings: 2})
	if _, err := svc.SetEntryCustomizations(ctx, hhAda, testWeek, salad.ID, []Customization{{IngredientKey: ingChicken, ChoiceID: "swap:ground-beef", Label: "Ground Beef"}}); err != nil {
		t.Fatal(err)
	}
	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	resp := newGroceryListResponse(g)
	var beef *GroceryItemResponse
	for _, c := range resp.Categories {
		for i, it := range c.Items {
			if it.Name == "Ground Beef" {
				beef = &c.Items[i]
			}
			if it.Name == "Chicken Breast" {
				t.Error("the swapped-out chicken is still on the list")
			}
		}
	}
	if beef == nil || len(beef.Via) != 1 {
		t.Fatalf("beef item = %+v", beef)
	}
	if want := "Ground Beef instead of Chicken Breast in Chicken Salad"; beef.Via[0].Text != want {
		t.Errorf("via text = %q, want %q", beef.Via[0].Text, want)
	}
	if beef.Via[0].Kind != grocery.ViaCustomized {
		t.Errorf("via kind = %q", beef.Via[0].Kind)
	}

	// A doubled line keeps its ingredient and reads "2x ...".
	if got := customizedViaText(grocery.ItemVia{
		Via:     grocery.Via{Kind: grocery.ViaCustomized, SpecialtyName: "Ground Pork", OptionID: "double", OptionName: "2x Ground Pork"},
		Recipes: []grocery.Source{{RecipeID: "r1", RecipeName: "One-Pan Pork Tacos"}},
	}); got != "2x Ground Pork in One-Pan Pork Tacos" {
		t.Errorf("doubled via text = %q", got)
	}
}
