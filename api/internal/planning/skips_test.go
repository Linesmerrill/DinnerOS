package planning

import (
	"context"
	"errors"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// fakeSkips is a SkipSource with fixed ingredient-wide skips per week, and
// fixed recipe skips for every week.
type fakeSkips struct {
	byWeek  map[string]grocery.SkipSet
	recipes map[string]map[string]bool
	err     error
	calls   []string
}

func (f *fakeSkips) GrocerySkips(_ context.Context, householdID, week string) (grocery.SkipRules, error) {
	f.calls = append(f.calls, householdID+"/"+week)
	if f.err != nil {
		return grocery.SkipRules{}, f.err
	}
	return grocery.SkipRules{Ingredients: f.byWeek[week], Recipes: f.recipes}, nil
}

func skippedNames(g GroceryList) []string {
	var out []string
	for _, it := range g.SkippedItems {
		out = append(out, it.Name)
	}
	return out
}

func listedNames(g GroceryList) []string {
	_, items := viewGroceryList(g)
	var out []string
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func TestGroceryListHoldsBackSkippedIngredients(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	source := &fakeSkips{byWeek: map[string]grocery.SkipSet{
		testWeek: {ingGarlic: grocery.SkipAlways},
	}}
	if got := svc.WithSkips(source); got != svc {
		t.Fatal("WithSkips must return the service it configures")
	}
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	for _, name := range listedNames(g) {
		if name == "Garlic" {
			t.Error("a skipped ingredient must not be on the list")
		}
	}
	if got := skippedNames(g); len(got) != 1 || got[0] != "Garlic" {
		t.Fatalf("SkippedItems = %v, want [Garlic]", got)
	}
	if got := g.SkippedItems[0]; got.Status != grocery.StatusSkipped || got.SkipScope != grocery.SkipAlways {
		t.Errorf("skipped item = status %q scope %q, want skipped/always", got.Status, got.SkipScope)
	}
	// The recipes that wanted it are still named, so nothing is silent.
	if len(g.SkippedItems[0].Sources) == 0 {
		t.Error("a skipped item must still say which recipes needed it")
	}
	if len(source.calls) != 1 || source.calls[0] != hhAda+"/"+testWeek {
		t.Errorf("skips were loaded as %v, want one call for this household and week", source.calls)
	}
}

// Garlic is in the tacos and the soup; the household leaves it out of the
// tacos only. The list keeps the soup's garlic, and the response says which
// meal each part is for.
func TestGroceryListRecipeSkipLeavesOutOneMealsShare(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	svc.WithSkips(&fakeSkips{recipes: map[string]map[string]bool{recipeTacos: {ingGarlic: true}}})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2, Day: "thu"})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2, Day: "mon"})

	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	resp := newGroceryListResponse(g)
	var garlic *GroceryItemResponse
	for _, c := range resp.Categories {
		for i := range c.Items {
			if c.Items[i].IngredientKey == ingGarlic {
				garlic = &c.Items[i]
			}
		}
	}
	if garlic == nil || garlic.QuantityText != "3 cloves" {
		t.Fatalf("garlic on the list = %+v, want the soup's 3 cloves", garlic)
	}
	if len(garlic.Shares) != 1 || garlic.Shares[0].RecipeID != recipeSoup || garlic.Shares[0].QuantityText != "3 cloves" {
		t.Errorf("garlic shares = %+v, want only the soup's", garlic.Shares)
	}
	if len(resp.SkippedItems) != 1 {
		t.Fatalf("SkippedItems = %+v, want the tacos' garlic", resp.SkippedItems)
	}
	left := resp.SkippedItems[0]
	if left.SkipScope != grocery.SkipRecipe || left.QuantityText != "2 cloves" || left.SkipText != "Left out of Beef Tacos" {
		t.Errorf("left-out garlic = %q %q %q, want recipe, 2 cloves, \"Left out of Beef Tacos\"", left.SkipScope, left.QuantityText, left.SkipText)
	}
	// Sour cream is shared too and nobody left it out: one line, two shares.
	for _, c := range resp.Categories {
		for _, it := range c.Items {
			if it.IngredientKey == ingSourCream && len(it.Shares) != 2 {
				t.Errorf("sour cream shares = %+v, want one per meal", it.Shares)
			}
		}
	}
	// Meals come in plan order, by day, whatever order they were added in.
	if len(resp.Meals) != 2 || resp.Meals[0].RecipeID != recipeTacos || resp.Meals[1].RecipeID != recipeSoup {
		t.Fatalf("meals = %+v, want tacos (Monday) then soup (Thursday)", resp.Meals)
	}
	if resp.Meals[0].ImageURL == nil || *resp.Meals[0].ImageURL != "https://img.example.com/tacos.jpg" || resp.Meals[0].Day == nil {
		t.Errorf("tacos meal = %+v, want its image and day", resp.Meals[0])
	}
}

func TestGroceryMealsListARecipePlannedTwiceOnce(t *testing.T) {
	p := Plan{Week: mustWeek(t, testWeek), Entries: []Entry{
		{RecipeID: recipeSoup, RecipeName: "Onion Soup"},
		{RecipeID: recipeTacos, RecipeName: "Beef Tacos", Day: Friday},
		{RecipeID: recipeTacos, RecipeName: "Beef Tacos", Day: Tuesday},
	}}
	meals := groceryMeals(p)
	if len(meals) != 2 || meals[0].RecipeID != recipeTacos || meals[0].Day != Tuesday || meals[1].RecipeID != recipeSoup {
		t.Errorf("meals = %+v, want tacos once on its first day (Tuesday), then the unscheduled soup", meals)
	}
}

func TestGroceryListSkipOnceAppliesToItsWeekOnly(t *testing.T) {
	ctx := context.Background()
	const nextWeek = "2026-W39"
	svc, _ := newTestService(t, newMemoryStore())
	svc.WithSkips(&fakeSkips{byWeek: map[string]grocery.SkipSet{
		testWeek: {ingGarlic: grocery.SkipThisWeek},
	}})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, nextWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	this, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	if got := skippedNames(this); len(got) != 1 || got[0] != "Garlic" {
		t.Fatalf("this week's SkippedItems = %v, want [Garlic]", got)
	}

	// Next week it is back on the list with no action from anyone.
	next, err := svc.GroceryList(ctx, hhAda, nextWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	if len(next.SkippedItems) != 0 {
		t.Errorf("next week's SkippedItems = %v, want none", skippedNames(next))
	}
	var found bool
	for _, name := range listedNames(next) {
		found = found || name == "Garlic"
	}
	if !found {
		t.Error("a skip-once must put the ingredient back on next week's list")
	}
}

func TestGroceryListWithoutSkipsBuysEverything(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	if len(g.SkippedItems) != 0 {
		t.Errorf("SkippedItems = %v on a server without skips, want none", skippedNames(g))
	}
}

func TestGroceryListFailsWhenSkipsCannotBeLoaded(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	sentinel := errors.New("boom")
	svc.WithSkips(&fakeSkips{err: sentinel})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	if _, err := svc.GroceryList(ctx, hhAda, testWeek); !errors.Is(err, sentinel) {
		t.Fatalf("GroceryList() error = %v, want the skip failure", err)
	}
}

func TestGroceryListResponseRendersSkippedItems(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	svc.WithSkips(&fakeSkips{byWeek: map[string]grocery.SkipSet{
		testWeek: {ingGarlic: grocery.SkipAlways},
	}})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}

	resp := newGroceryListResponse(g)
	if len(resp.SkippedItems) != 1 {
		t.Fatalf("response SkippedItems = %d, want 1", len(resp.SkippedItems))
	}
	item := resp.SkippedItems[0]
	if item.Name != "Garlic" || item.Status != grocery.StatusSkipped {
		t.Errorf("skipped item = %q/%q, want Garlic/skipped", item.Name, item.Status)
	}
	if item.SkipScope != grocery.SkipAlways || item.SkipText != "Never buying this" {
		t.Errorf("skip wording = %q/%q, want always/\"Never buying this\"", item.SkipScope, item.SkipText)
	}
	// Ordinary items carry no skip fields at all.
	for _, c := range resp.Categories {
		for _, it := range c.Items {
			if it.SkipScope != "" || it.SkipText != "" {
				t.Errorf("item %q carries skip fields it should not: %+v", it.Name, it)
			}
		}
	}
}
