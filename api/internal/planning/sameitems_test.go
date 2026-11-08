package planning

import (
	"context"
	"errors"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

type fakeSameItems struct {
	links grocery.SameItems
	err   error
}

func (f fakeSameItems) GrocerySameItems(context.Context, string) (grocery.SameItems, error) {
	return f.links, f.err
}

// The household said its recipes' "Olive Oil" is the "Extra Virgin Olive Oil"
// it buys: the list has one line under the kept name, and says what else it's
// called so the link can be undone.
func TestGroceryListBuysLinkedNamesAsOneItem(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	if got := svc.WithSameItems(fakeSameItems{links: grocery.SameItems{{
		LinkID: "link-1", VariantKeys: []string{ingOliveOil, "name:olive oil"}, VariantName: "Olive Oil",
		KeptKey: "name:extra virgin olive oil", KeptName: "Extra Virgin Olive Oil",
	}}}); got != svc {
		t.Fatal("WithSameItems must return the service it configures")
	}
	svc.WithSkips(&fakeSkips{byWeek: map[string]grocery.SkipSet{testWeek: {ingOliveOil: grocery.SkipThisWeek}}})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})

	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	// The skip made under the old name still holds the merged line back.
	resp := newGroceryListResponse(g)
	if len(resp.SkippedItems) != 1 {
		t.Fatalf("SkippedItems = %+v; want the merged olive oil", resp.SkippedItems)
	}
	oil := resp.SkippedItems[0]
	if oil.Name != "Extra Virgin Olive Oil" || oil.IngredientKey != "name:extra virgin olive oil" {
		t.Errorf("merged item = %q %q; want the kept name and key", oil.Name, oil.IngredientKey)
	}
	if len(oil.AlsoCalled) != 1 || oil.AlsoCalled[0].Name != "Olive Oil" || oil.AlsoCalled[0].LinkID != "link-1" {
		t.Errorf("alsoCalled = %+v; want Olive Oil with its link", oil.AlsoCalled)
	}
	for _, c := range resp.Categories {
		for _, it := range c.Items {
			if it.AlsoCalled == nil {
				t.Errorf("%s alsoCalled is null; want an empty list", it.Name)
			}
		}
	}
}

func TestGroceryListFailsWhenLinksDontLoad(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	sentinel := errors.New("links down")
	svc.WithSameItems(fakeSameItems{err: sentinel})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})
	if _, err := svc.GroceryList(context.Background(), hhAda, testWeek); !errors.Is(err, sentinel) {
		t.Errorf("GroceryList() error = %v; want the links error", err)
	}
}
