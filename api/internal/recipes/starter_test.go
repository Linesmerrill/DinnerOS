package recipes

import (
	"context"
	"fmt"
	"testing"
	"time"
)

const (
	starterSource = "00000000000000000000aaaa"
	starterTarget = "00000000000000000000bbbb"
)

func TestCopyStarterRecipes(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	svc := NewService(store)
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	var source []Recipe
	for i := range StarterBatchSize + 5 { // more than one page
		source = append(source, Recipe{
			Source: SourceHelloFresh, SourceRecipeID: fmt.Sprintf("hf-%d", i), SourceAliases: []string{fmt.Sprintf("alias-%d", i)},
			Name: fmt.Sprintf("Recipe %d", i), ImageURL: "https://img.example/r.jpg", Tags: []string{"Quick"},
			Ingredients: []RecipeIngredient{{IngredientID: "000000000000000000000001", Name: "Garlic", Amounts: []Amount{{Servings: 2, Quantity: "1"}}}},
			OrderWeeks:  []string{"2026-W30"}, TimesOrdered: 1, LastOrderedWeek: "2026-W30",
		})
	}
	if err := store.SaveRecipes(ctx, starterSource, source); err != nil {
		t.Fatal(err)
	}
	// The target already has one recipe under its alias and one of another
	// source with a colliding ID.
	if err := store.SaveRecipes(ctx, starterTarget, []Recipe{
		{Source: SourceHelloFresh, SourceRecipeID: "alias-3", Name: "Mine"},
		{Source: SourceManual, SourceRecipeID: "hf-4", Name: "Manual"},
	}); err != nil {
		t.Fatal(err)
	}
	total := StarterBatchSize + 5

	dry, err := svc.CopyStarterRecipes(ctx, starterSource, starterTarget, false)
	if err != nil || dry != (StarterResult{Source: total, Copied: total - 1, Existing: 1}) {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if got, _ := store.ListRecipesAfter(ctx, starterTarget, "", 1000); len(got) != 2 {
		t.Fatalf("dry run wrote: target has %d recipes", len(got))
	}

	res, err := svc.CopyStarterRecipes(ctx, starterSource, starterTarget, true)
	if err != nil || res != dry {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	copied, _ := store.ListRecipesAfter(ctx, starterTarget, "", 1000)
	if len(copied) != total+1 {
		t.Fatalf("target has %d recipes, want %d", len(copied), total+1)
	}
	for _, r := range copied {
		if r.Name == "Mine" || r.Name == "Manual" {
			continue
		}
		if r.HouseholdID != starterTarget || r.OrderWeeks != nil || r.TimesOrdered != 0 || r.LastOrderedWeek != "" ||
			!r.CreatedAt.Equal(now) || r.ImageURL == "" || len(r.Tags) != 1 || len(r.Ingredients) != 1 || len(r.SourceAliases) != 1 {
			t.Fatalf("copy = %+v", r)
		}
	}

	again, err := svc.CopyStarterRecipes(ctx, starterSource, starterTarget, true)
	if err != nil || again != (StarterResult{Source: total, Existing: total}) {
		t.Errorf("re-run = %+v, %v", again, err)
	}
	if _, err := svc.CopyStarterRecipes(ctx, starterSource, starterSource, true); err == nil {
		t.Error("copy into the source: error = nil")
	}
	if _, err := svc.CopyStarterRecipes(ctx, "", starterTarget, true); err == nil {
		t.Error("no source: error = nil")
	}
}

func TestStarterHouseholdCreated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := newMemoryStore()
	svc := NewService(store)
	if err := store.SaveRecipes(context.Background(), starterSource, []Recipe{{Source: SourceHelloFresh, SourceRecipeID: "1", Name: "One"}}); err != nil {
		t.Fatal(err)
	}

	st := NewStarter(StarterOptions{Service: svc, SourceHouseholdID: starterSource})
	st.HouseholdCreated(ctx, starterTarget)
	// The hold lets a quick copy finish before creation returns.
	if got, _ := store.ListRecipesAfter(context.Background(), starterTarget, "", 10); len(got) != 1 {
		t.Errorf("after HouseholdCreated the target has %d recipes, want 1", len(got))
	}
	cancel()
	st.HouseholdCreated(context.Background(), starterSource)
	if err := st.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ListRecipesAfter(context.Background(), starterTarget, "", 10); len(got) != 1 {
		t.Errorf("target has %d recipes, want 1", len(got))
	}
	if got, _ := store.ListRecipesAfter(context.Background(), starterSource, "", 10); len(got) != 1 {
		t.Errorf("source has %d recipes, want 1", len(got))
	}

	// Without a hold, and with the request already over, the copy still
	// completes in the background.
	ended, end := context.WithCancel(context.Background())
	end()
	async := NewStarter(StarterOptions{Service: svc, SourceHouseholdID: starterSource, Hold: -1})
	async.HouseholdCreated(ended, "00000000000000000000dddd")
	if err := async.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ListRecipesAfter(context.Background(), "00000000000000000000dddd", "", 10); len(got) != 1 {
		t.Errorf("async target has %d recipes, want 1", len(got))
	}

	off := NewStarter(StarterOptions{Service: svc})
	if off.Enabled() {
		t.Error("Enabled() without a source = true")
	}
	off.HouseholdCreated(context.Background(), "00000000000000000000cccc")
	if got, _ := store.ListRecipesAfter(context.Background(), "00000000000000000000cccc", "", 10); len(got) != 0 {
		t.Errorf("disabled starter copied %d recipes", len(got))
	}
}
