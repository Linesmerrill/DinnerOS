package shopping

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// beefWeek is two meals that each need 12 oz of ground beef, aggregated the
// way planning does, with the given skips.
func beefWeek(t *testing.T, skips grocery.Skips) planning.GroceryList {
	t.Helper()
	twelve := ingredients.NewQuantity(12, 1)
	beef := func() grocery.Line {
		return grocery.Line{IngredientKey: beefKey, Name: "Ground Beef", Category: "meat-seafood", Quantity: &twelve, UnitCode: "oz"}
	}
	list, err := grocery.AggregateWith([]grocery.RecipeSelection{
		{RecipeID: "tacos", RecipeName: "Beef Tacos", RecipeServings: 2, TargetServings: 2, Lines: []grocery.Line{beef()}},
		{RecipeID: "chili", RecipeName: "Chili", RecipeServings: 2, TargetServings: 2, Lines: []grocery.Line{beef()}},
	}, nil, skips)
	if err != nil {
		t.Fatal(err)
	}
	week, err := planning.ParseWeek("2026-W38")
	if err != nil {
		t.Fatal(err)
	}
	g := planning.GroceryList{Week: week, SkippedItems: list.SkippedItems, Meals: []planning.GroceryMeal{
		{RecipeID: "tacos", RecipeName: "Beef Tacos"}, {RecipeID: "chili", RecipeName: "Chili"},
	}}
	if len(list.Items) > 0 {
		g.Categories = []planning.GroceryCategory{{Category: "meat-seafood", Items: list.Items}}
	}
	return g
}

// A shared ingredient is one purchase: one line, one package count for the
// week's total, with each meal's share beside it. Leaving it out of one meal
// shrinks the line and its package count; it doesn't duplicate or drop it.
func TestBuildProposalBuysASharedIngredientOnceAndRecountsWhenOneMealLeavesItOut(t *testing.T) {
	w := providers.NewWalmart(providers.WalmartOptions{})
	settings := Settings{HouseholdID: "h", Provider: providers.KeyWalmart}

	both, err := buildProposal(w, settings, beefWeek(t, nil), testPrefs(), MatchInput{}, matchNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(both.Lines) != 1 || both.Lines[0].Packages != 2 || len(both.Lines[0].Shares) != 2 {
		t.Fatalf("both meals: lines = %+v, want one beef line of 2 × 16 oz with two shares", both.Lines)
	}
	if len(both.Meals) != 2 || both.Meals[0].RecipeID != "tacos" {
		t.Errorf("meals = %+v, want the plan's", both.Meals)
	}

	rules := grocery.SkipRules{Recipes: map[string]map[string]bool{"chili": {beefKey: true}}}
	one, err := buildProposal(w, settings, beefWeek(t, rules), testPrefs(), MatchInput{}, matchNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Lines) != 1 {
		t.Fatalf("lines = %+v, want the tacos' beef still on the list", one.Lines)
	}
	line := one.Lines[0]
	if line.Packages != 1 || line.Amounts[0] != (Amount{Quantity: "12", Unit: "oz"}) {
		t.Errorf("beef = %d packages of %+v, want 1 for the tacos' 12 oz", line.Packages, line.Amounts)
	}
	if len(line.Shares) != 1 || line.Shares[0].RecipeID != "tacos" {
		t.Errorf("shares = %+v, want only the tacos'", line.Shares)
	}
	var skipped *Excluded
	for i, e := range one.Excluded {
		if e.Reason == ExcludedSkipped {
			skipped = &one.Excluded[i]
		}
	}
	if skipped == nil || skipped.IngredientKey != beefKey || skipped.SkipScope != grocery.SkipRecipe ||
		len(skipped.Recipes) != 1 || skipped.Recipes[0].Name != "Chili" {
		t.Fatalf("excluded = %+v, want the chili's beef, skipped for that recipe", one.Excluded)
	}
	if got := skippedText(*skipped); got != "Left out of Chili" {
		t.Errorf("skipped text = %q", got)
	}
	// Only the line goes to Walmart.
	if len(one.Links) != 1 || one.Links[0].ItemCount != 1 {
		t.Errorf("links = %+v, want one item", one.Links)
	}
}

func TestBuildProposalLeavesAWhollySkippedIngredientOutOfTheCart(t *testing.T) {
	w := providers.NewWalmart(providers.WalmartOptions{})
	rules := grocery.SkipRules{Ingredients: grocery.SkipSet{beefKey: grocery.SkipAlways}}
	p, err := buildProposal(w, Settings{HouseholdID: "h", Provider: providers.KeyWalmart}, beefWeek(t, rules), testPrefs(), MatchInput{}, matchNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Lines) != 0 || len(p.Links) != 0 {
		t.Errorf("lines = %+v, links = %+v; want nothing sent", p.Lines, p.Links)
	}
	if r := reasons(p)[beefKey]; r != ExcludedSkipped {
		t.Errorf("beef reason = %q, want skipped", r)
	}
	if got := skippedText(p.Excluded[0]); got != "Never buying this" {
		t.Errorf("text = %q", got)
	}
}
