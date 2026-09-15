package planning

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// itemView is a grocery item reduced to what the tests compare.
type itemView struct {
	Category     string
	Name         string
	Amounts      []string // exact quantity and unit code: "3/2 count"
	Unquantified bool
	Status       grocery.Status
	Recipes      []string
}

func viewGroceryList(g GroceryList) (categories []string, items []itemView) {
	for _, c := range g.Categories {
		categories = append(categories, c.Category)
		for _, it := range c.Items {
			v := itemView{Category: it.Category, Name: it.Name, Unquantified: it.Unquantified, Status: it.Status}
			for _, a := range it.Amounts {
				v.Amounts = append(v.Amounts, a.Quantity.String()+" "+a.Unit.Code)
			}
			for _, s := range it.Sources {
				v.Recipes = append(v.Recipes, s.RecipeName)
			}
			items = append(items, v)
		}
	}
	return categories, items
}

func TestGroceryListAggregatesOverlappingRecipes(t *testing.T) {
	svc, reader := newTestService(t, newMemoryStore())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Day: "mon", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Day: "wed", Servings: 2})

	g, err := svc.GroceryList(context.Background(), hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	categories, items := viewGroceryList(g)
	if want := []string{"produce", "meat-seafood", "dairy-eggs", "pantry", "spices"}; !slices.Equal(categories, want) {
		t.Errorf("categories = %v, want %v (grocery.CategoryOrder, each once)", categories, want)
	}
	want := []itemView{
		{"produce", "Garlic", []string{"5 clove"}, false, grocery.StatusToBuy, []string{"Beef Tacos", "Onion Soup"}},
		// A count and a weight of onion can't combine, so they stay separate.
		{"produce", "Yellow Onion", []string{"1 count", "8 oz"}, false, grocery.StatusToBuy, []string{"Beef Tacos", "Chicken Salad", "Onion Soup"}},
		{"meat-seafood", "Chicken Breast", []string{"10 oz"}, false, grocery.StatusToBuy, []string{"Chicken Salad"}},
		// 2 tbsp + ¼ cup = 6 tbsp, less than a cup, so it shows in tbsp.
		{"dairy-eggs", "Sour Cream", []string{"6 tbsp"}, false, grocery.StatusToBuy, []string{"Beef Tacos", "Onion Soup"}},
		// Every source marks olive oil as a staple, and there is no pantry yet.
		{"pantry", "Olive Oil", []string{"1 tbsp"}, false, grocery.StatusPantryHint, []string{"Onion Soup"}},
		// The salad doesn't mark salt as a staple, and two recipes give no amount.
		{"spices", "Salt", []string{"1/4 tsp"}, true, grocery.StatusToBuy, []string{"Beef Tacos", "Chicken Salad", "Onion Soup"}},
	}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("items =\n%+v\nwant\n%+v", items, want)
	}
	if len(g.Skipped) != 0 || g.Week.String() != testWeek || g.Status != StatusDraft {
		t.Errorf("list = %+v", g)
	}
	if len(reader.getManyCalls) != 1 || len(reader.getManyCalls[0]) != 3 {
		t.Errorf("recipe loads = %v, want one batch of 3 IDs", reader.getManyCalls)
	}
}

func TestGroceryListUsesAuthoredAmountsPerEntry(t *testing.T) {
	svc, reader := newTestService(t, newMemoryStore())
	// The same recipe twice, at different sizes: amounts come from each size's
	// authored values and add up; the recipe is listed once.
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 4})

	g, err := svc.GroceryList(context.Background(), hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	_, items := viewGroceryList(g)
	byName := map[string]itemView{}
	for _, it := range items {
		byName[it.Name] = it
	}
	if garlic := byName["Garlic"]; !slices.Equal(garlic.Amounts, []string{"6 clove"}) || !slices.Equal(garlic.Recipes, []string{"Beef Tacos"}) {
		t.Errorf("garlic = %+v", garlic)
	}
	if onion := byName["Yellow Onion"]; !slices.Equal(onion.Amounts, []string{"3/2 count"}) {
		t.Errorf("onion = %+v", onion)
	}
	if len(reader.getManyCalls) != 1 || len(reader.getManyCalls[0]) != 1 {
		t.Errorf("recipe loads = %v, want one ID loaded once", reader.getManyCalls)
	}
}

func TestGroceryListSkipsUnavailableEntries(t *testing.T) {
	svc, reader := newTestService(t, newMemoryStore())
	_, tacos := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 4})
	_, salad := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})

	// A re-import dropped the 4-serving size, and the salad left the household.
	twoOnly := tacosRecipe()
	twoOnly.Servings = []int{2}
	reader.put(twoOnly)
	reader.remove(recipeSalad)

	g, err := svc.GroceryList(context.Background(), hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	wantSkipped := []SkippedEntry{
		{EntryID: tacos.ID, RecipeID: recipeTacos, RecipeName: "Beef Tacos", Reason: SkipServingsUnavailable},
		{EntryID: salad.ID, RecipeID: recipeSalad, RecipeName: "Chicken Salad", Reason: SkipRecipeUnavailable},
	}
	if !reflect.DeepEqual(g.Skipped, wantSkipped) {
		t.Errorf("skipped = %+v, want %+v", g.Skipped, wantSkipped)
	}
	_, items := viewGroceryList(g)
	for _, it := range items {
		if !slices.Equal(it.Recipes, []string{"Onion Soup"}) {
			t.Errorf("item %s has sources %v, want only the soup", it.Name, it.Recipes)
		}
	}
}

func TestGroceryListEmptyWeek(t *testing.T) {
	svc, reader := newTestService(t, newMemoryStore())
	g, err := svc.GroceryList(context.Background(), hhAda, testWeek)
	if err != nil || len(g.Categories) != 0 || len(g.Skipped) != 0 || g.Status != StatusDraft {
		t.Errorf("GroceryList(empty) = %+v, %v", g, err)
	}
	if len(reader.getManyCalls) != 0 {
		t.Errorf("an empty week loaded recipes: %v", reader.getManyCalls)
	}
}

func TestGroceryLinesWithoutUsableAmounts(t *testing.T) {
	r := recipes.Recipe{
		ID: recipeTacos, Name: "Odd Recipe", Servings: []int{2, 4},
		Ingredients: []recipes.RecipeIngredient{
			ingredientLine(ingGarlic, "Garlic", "produce", false, amt(4, "4", "clove")), // no amount for 2
			ingredientLine(ingOnion, "Onion", "produce", false, amt(2, "1", "dollop")),  // unknown unit
			ingredientLine(ingSalt, "Salt", "spices", true, amt(2, "a pinch", "pinch")), // unparsable quantity
			ingredientLine(ingChicken, "Chicken", "meat-seafood", false, amt(2, "0", "oz")),
			ingredientLine("", "  Mystery   Paste ", "", false, amt(2, "1", "tbsp")),
			ingredientLine("", "   ", "", false, amt(2, "1", "tbsp")), // no identity at all
		},
	}
	lines := groceryLines(r, 2)
	if len(lines) != 5 {
		t.Fatalf("lines = %+v, want 5 (the nameless line is dropped)", lines)
	}
	for _, l := range lines[:4] {
		if l.Quantity != nil || l.UnitCode != "" {
			t.Errorf("%s: quantity %v %q, want none", l.Name, l.Quantity, l.UnitCode)
		}
	}
	if l := lines[4]; l.IngredientKey != "name:mystery paste" || l.Quantity == nil || l.Quantity.String() != "1" || l.UnitCode != "tbsp" {
		t.Errorf("uncatalogued line = %+v", l)
	}
	if _, err := grocery.Aggregate([]grocery.RecipeSelection{{RecipeID: r.ID, RecipeName: r.Name, RecipeServings: 2, TargetServings: 2, Lines: lines}}, nil); err != nil {
		t.Errorf("Aggregate() error = %v; unusable amounts must not fail the list", err)
	}
}
