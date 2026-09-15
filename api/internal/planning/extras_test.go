package planning

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// fakeExtras is an ExtrasSource with fixed lines.
type fakeExtras struct {
	lines []grocery.Line
	err   error
	plan  Plan
}

func (f *fakeExtras) GroceryExtras(_ context.Context, plan Plan) ([]grocery.Line, error) {
	f.plan = plan
	return f.lines, f.err
}

// crackerLine is a paired grocery item for the tacos.
func crackerLine() grocery.Line {
	return grocery.Line{
		IngredientKey: "name:club crackers", Name: "Club crackers", Category: "pantry", Quantity: qty("1"), UnitCode: "package",
		Sources: []grocery.Source{{RecipeID: recipeTacos, RecipeName: "Beef Tacos"}},
		Extra:   &grocery.Extra{ID: "e1", Origin: grocery.OriginPairing, Text: "Club crackers for Beef Tacos"},
	}
}

func groceryItem(list GroceryList, key string) grocery.Item {
	for _, c := range list.Categories {
		if i := slices.IndexFunc(c.Items, func(it grocery.Item) bool { return it.IngredientKey == key }); i >= 0 {
			return c.Items[i]
		}
	}
	return grocery.Item{}
}

func TestGroceryListIncludesExtras(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	extras := &fakeExtras{lines: []grocery.Line{crackerLine()}}
	svc.WithExtras(extras)
	ctx := context.Background()
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	list, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	item := groceryItem(list, "name:club crackers")
	if item.Name != "Club crackers" || len(item.Extras) != 1 || item.Extras[0].Text != "Club crackers for Beef Tacos" ||
		item.Extras[0].Origin != grocery.OriginPairing {
		t.Fatalf("item = %+v", item)
	}
	if len(item.Sources) != 1 || item.Sources[0].RecipeName != "Beef Tacos" || len(item.Amounts) != 1 ||
		item.Amounts[0].Quantity.String() != "1" || item.Amounts[0].Unit.Code != "package" {
		t.Errorf("provenance = %+v, amounts %+v", item.Sources, item.Amounts)
	}
	if item.Status != grocery.StatusToBuy {
		t.Errorf("status = %q", item.Status)
	}
	if extras.plan.Week != mustWeek(t, testWeek) || len(extras.plan.Entries) != 1 {
		t.Errorf("the source gets the week's plan: %+v", extras.plan)
	}
}

func TestGroceryListWithoutEntriesSkipsExtras(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	extras := &fakeExtras{lines: []grocery.Line{crackerLine()}}
	svc.WithExtras(extras)
	list, err := svc.GroceryList(context.Background(), hhAda, testWeek)
	if err != nil || len(list.Categories) != 0 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if extras.plan.Entries != nil {
		t.Errorf("an empty week shouldn't ask for extras: %+v", extras.plan)
	}
}

func TestGroceryListFailsWhenExtrasFail(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	svc.WithExtras(&fakeExtras{err: errors.New("boom")})
	ctx := context.Background()
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	if _, err := svc.GroceryList(ctx, hhAda, testWeek); err == nil {
		t.Error("a failing extras source should fail the list")
	}
}

// An extra merges with a recipe line for the same ingredient, keeping both
// recipes as sources and summing the amounts.
func TestGroceryListMergesExtraWithRecipeLine(t *testing.T) {
	svc, reader := newTestService(t, newMemoryStore())
	soup := soupRecipe()
	soup.Ingredients = append(soup.Ingredients, ingredientLine("", "Club Crackers", "pantry", false, amt(2, "1", "package")))
	reader.put(soup)
	svc.WithExtras(&fakeExtras{lines: []grocery.Line{crackerLine()}})
	ctx := context.Background()
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})

	list, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	item := groceryItem(list, "name:club crackers")
	if len(item.Extras) != 1 || len(item.Sources) != 2 || item.Amounts[0].Quantity.String() != "2" {
		t.Errorf("merged item = %+v", item)
	}
}

// Recipes keep working when no extras source is wired.
func TestGroceryListWithoutExtrasSource(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	list, err := svc.GroceryList(context.Background(), hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if got := groceryItem(list, "name:club crackers"); got.Name != "" {
		t.Errorf("item = %+v", got)
	}
}
