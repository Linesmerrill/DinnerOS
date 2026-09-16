package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Fixtures are synthetic; real imported data never enters the repository.

func TestNormalizeMapsPickToCount(t *testing.T) {
	recipe := strings.ReplaceAll(syntheticRecipe, `"unit": "dollop"`, `"unit": "pick"`)
	file, err := Normalize([]RawRecipe{rawFrom(t, "bbbbbbbbbbbbbbbbbbbbbbbb", recipe)}, History{}, time.Now())
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	found := false
	for _, ing := range file.Recipes[0].Ingredients {
		if ing.Name != "Mystery Paste" {
			continue
		}
		for _, a := range ing.Amounts {
			found = true
			if a.Unit != "count" || a.SourceUnit != "pick" {
				t.Errorf("amount = %+v, want count from pick", a)
			}
		}
	}
	if !found {
		t.Fatal("no Mystery Paste amounts")
	}
	for _, rv := range file.Review {
		if strings.HasSuffix(rv.Field, ".unit") {
			t.Errorf("unexpected unit review item %+v", rv)
		}
	}
}

func TestVariantKeyIgnoresOnlySpelling(t *testing.T) {
	same := [][2]string{
		{"Honey Butter Corn Bread", "honey-butter-cornbread"},
		{"Bánh Mì Burgers", "banh mi burgers"},
		{"Mac 'n' Cheese", "Mac n Cheese"},
		{"Pork & Beans", "PORK AND BEANS"},
		{"Spicy Kung Pao�Style Chicken", "Spicy Kung Pao-Style Chicken"},
		{"Yucatán Citrus Turkey Bowls", "yucatan-citrus-turkey-bowls"},
	}
	for _, p := range same {
		if variantKey(p[0]) != variantKey(p[1]) {
			t.Errorf("%q and %q should be the same variant", p[0], p[1])
		}
	}
	different := [][2]string{
		{"Chocolate Lava Cake", "molten chocolate lava cake"},
		{"One-Pot Beef and Black Bean Chili", "one pot pork and black bean chili"},
		{"Beef Tacos", "Beef Tacos with Chorizo"},
		{"Caramelized Onion Swissburgs", "caramelized onion swissburgers"},
	}
	for _, p := range different {
		if variantKey(p[0]) == variantKey(p[1]) {
			t.Errorf("%q and %q must stay different variants", p[0], p[1])
		}
	}
	if sameVariant("", hfRecipe{}) {
		t.Error("an empty name matched an empty recipe")
	}
}

func cardRaw(t *testing.T, deliveredID string, c Card) RawRecipe {
	t.Helper()
	data, err := json.Marshal(c.hfRecipe(deliveredID))
	if err != nil {
		t.Fatal(err)
	}
	return RawRecipe{DeliveredID: deliveredID, Origin: OriginCard, Recipe: data}
}

func cardLine(name string, two, four float64, unit string) CardIngredient {
	return CardIngredient{Name: name, Amounts: []CardAmount{
		{Servings: 2, Quantity: &two, Unit: unit}, {Servings: 4, Quantity: &four, Unit: unit},
	}}
}

func TestNormalizeUsesCardsForDeliveredVariants(t *testing.T) {
	canonical := strings.Replace(syntheticRecipe, `"id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US",`, `"id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US", "slug": "synthetic-taco-night",`, 1)
	const (
		b = "bbbbbbbbbbbbbbbbbbbbbbbb" // the canonical recipe
		c = "cccccccccccccccccccccccc" // pork variant with a card
		d = "dddddddddddddddddddddddd" // respelled name
		e = "eeeeeeeeeeeeeeeeeeeeeeee" // reworded, but its card ships the same ingredients
		f = "ffffffffffffffffffffffff" // beef variant without a card
	)
	history := History{Weeks: []HistoryWeek{
		{Week: "2026-W01", Meals: []HistoryMeal{{ID: b, Name: "synthetic taco night"}}},
		{Week: "2026-W02", Meals: []HistoryMeal{{ID: c, Name: "pork synthetic taco night"}}},
		{Week: "2026-W03", Meals: []HistoryMeal{{ID: d, Name: "SYNTHETIC TACONIGHT!"}}},
		{Week: "2026-W04", Meals: []HistoryMeal{{ID: e, Name: "molten synthetic taco night"}}},
		{Week: "2026-W05", Meals: []HistoryMeal{{ID: f, Name: "beef synthetic taco night"}}},
	}}
	pork := Card{
		Name: "PORK SYNTHETIC TACO NIGHT", Servings: []int{2, 4}, PrepMinutes: 5, CookMinutes: 25,
		Ingredients: []CardIngredient{cardLine("Ground Pork", 10, 20, "oz"), cardLine("Lime", 1, 2, "unit"), {Name: "Kosher salt", Pantry: true}},
		Steps:       []string{"• Brown pork.", "• Serve."},
	}
	molten := Card{
		Name: "MOLTEN SYNTHETIC TACO NIGHT", Servings: []int{2, 4},
		Ingredients: []CardIngredient{cardLine("Lime", 1, 2, "unit"), cardLine("Parmesan Cheese", 1, 2, "oz"), cardLine("Mystery Paste*", 1, 2, "unit"), cardLine("Orphan", 1, 2, "unit")},
		Steps:       []string{"• Cook."},
	}
	raws := []RawRecipe{
		rawFrom(t, b, canonical), rawFrom(t, c, canonical), rawFrom(t, d, canonical), rawFrom(t, e, canonical), rawFrom(t, f, canonical),
		cardRaw(t, c, pork), cardRaw(t, e, molten),
	}
	file, err := Normalize(raws, history, time.Now())
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if len(file.Recipes) != 2 {
		t.Fatalf("recipes = %+v, want the canonical recipe and the pork variant", file.Recipes)
	}
	byName := map[string]ImportRecipe{}
	for _, r := range file.Recipes {
		byName[r.Name] = r
	}
	canon := byName["Synthetic Taco Night"]
	if canon.SourceRecipeID != "aaaaaaaaaaaaaaaaaaaaaaaa" || strings.Join(canon.SourceAliases, ",") != b+","+d+","+e+","+f || strings.Join(canon.OrderWeeks, ",") != "2026-W01,2026-W03,2026-W04,2026-W05" {
		t.Errorf("canonical = id %s aliases %v weeks %v", canon.SourceRecipeID, canon.SourceAliases, canon.OrderWeeks)
	}
	got := byName["Pork Synthetic Taco Night"]
	if got.SourceRecipeID != c || len(got.SourceAliases) != 0 || strings.Join(got.OrderWeeks, ",") != "2026-W02" || got.PrepMinutes != 5 || got.TotalMinutes != 25 {
		t.Fatalf("pork variant = %+v", got)
	}
	if got.ImageURL != "" || strings.Join(got.Cuisines, ",") != "Mexican" || len(got.Servings) != 2 {
		t.Errorf("pork details: image %q cuisines %v servings %v; want the page's cuisines but not its photo", got.ImageURL, got.Cuisines, got.Servings)
	}
	ids := map[string]string{}
	for _, ing := range got.Ingredients {
		ids[ing.Name] = ing.SourceIngredientID
	}
	if ids["Ground Pork"] != "" || ids["Lime"] != "ing-lime" || len(got.Steps) != 2 {
		t.Errorf("pork ingredient IDs %v steps %v: card lines take the page's ID for the same name and no made-up IDs", ids, got.Steps)
	}

	var variants []string
	for _, rv := range file.Review {
		if rv.Field == "variant" {
			variants = append(variants, rv.Value)
		}
	}
	if strings.Join(variants, ",") != "beef synthetic taco night" {
		t.Errorf("variant review items = %v, want only the beef variant without a card", variants)
	}

	pending, err := PendingVariants(raws, history)
	if err != nil || len(pending) != 1 || pending[0].DeliveredID != f {
		t.Errorf("PendingVariants() = %+v, %v; want only the beef variant without a card", pending, err)
	}
}

func TestNormalizeFillsStepsFromACardOfTheSameDish(t *testing.T) {
	noSteps := strings.Replace(syntheticRecipe, `"steps": [`, `"steps": [], "unused": [`, 1)
	other := strings.ReplaceAll(noSteps, "aaaaaaaaaaaaaaaaaaaaaaaa", "ffffffffffffffffffffffff")
	other = strings.Replace(other, " Synthetic Taco Night ", "Other Dinner", 1)
	history := History{Weeks: []HistoryWeek{
		{Week: "2026-W01", Meals: []HistoryMeal{{ID: "bbbbbbbbbbbbbbbbbbbbbbbb", Name: "synthetic taco night"}}},
		{Week: "2026-W02", Meals: []HistoryMeal{{ID: "cccccccccccccccccccccccc", Name: "other dinner"}}},
	}}
	sameDish := Card{Name: "SYNTHETIC TACO NIGHT", Servings: []int{2}, Ingredients: []CardIngredient{{Name: "Lime"}}, Steps: []string{"• Wash and dry\nproduce.", "• Cook."}}
	// A card whose recipe differs from the capture it belongs to is never used.
	otherDish := Card{Name: "PORK DINNER", Servings: []int{2}, Ingredients: []CardIngredient{{Name: "Pork"}}, Steps: []string{"• Cook pork."}}

	file, err := Normalize([]RawRecipe{
		rawFrom(t, "bbbbbbbbbbbbbbbbbbbbbbbb", noSteps),
		cardRaw(t, "bbbbbbbbbbbbbbbbbbbbbbbb", sameDish),
		rawFrom(t, "cccccccccccccccccccccccc", other),
		cardRaw(t, "cccccccccccccccccccccccc", otherDish),
	}, history, time.Now())
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	for _, r := range file.Recipes {
		switch r.Name {
		case "Synthetic Taco Night":
			if len(r.Steps) != 2 || r.Steps[0].Text != "Wash and dry produce." {
				t.Errorf("steps = %+v, want the card's", r.Steps)
			}
		case "Other Dinner":
			if len(r.Steps) != 0 {
				t.Errorf("other dinner steps = %+v, want none from a different dish's card", r.Steps)
			}
		}
	}
	var stepItems []string
	for _, rv := range file.Review {
		if rv.Field == "steps" {
			stepItems = append(stepItems, rv.RecipeName)
		}
	}
	if strings.Join(stepItems, ",") != "Other Dinner" {
		t.Errorf("steps review items = %v, want only Other Dinner", stepItems)
	}
}
