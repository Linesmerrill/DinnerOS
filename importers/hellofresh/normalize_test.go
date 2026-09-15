package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Synthetic source recipe shaped like the HelloFresh recipe object.
const syntheticRecipe = `{
  "recipeId": "aaaaaaaaaaaaaaaaaaaaaaaa",
  "id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US",
  "name": " Synthetic Taco Night ",
  "headline": "with Lime Crema",
  "description": "A test recipe.",
  "imagePath": "/image/aaaa-hero.jpg",
  "canonicalLink": "https://www.hellofresh.com/recipes/synthetic-taco-night-aaaaaaaaaaaaaaaaaaaaaaaa",
  "prepTime": "PT10M",
  "totalTime": "PT1H5M",
  "difficulty": 1,
  "isAddon": false,
  "updatedAt": "2026-01-02T00:00:00Z",
  "cuisines": [{"name": "Mexican"}, {"name": "Mexican"}],
  "tags": [{"name": "Quick"}, {"name": " "}],
  "utensils": [{"name": "Large Pan"}],
  "allergens": [{"name": "Milk"}],
  "nutrition": [{"name": "Calories", "amount": 700, "unit": "kcal"}],
  "ingredients": [
    {"id": "ing-lime", "name": "Lime", "slug": "lime", "imagePath": "/ingredient/lime.png", "shipped": true},
    {"id": "ing-cheese", "name": "Parmesan Cheese", "slug": "parmesan", "shipped": true},
    {"id": "ing-salt", "name": "Salt", "slug": "salt", "shipped": false},
    {"id": "ing-weird", "name": "Mystery Paste", "slug": "mystery", "shipped": true},
    {"id": "ing-orphan", "name": "Orphan", "slug": "orphan", "shipped": true}
  ],
  "yields": [
    {"yields": 4, "ingredients": [
      {"id": "ing-lime", "amount": 2, "unit": "unit"},
      {"id": "ing-cheese", "amount": 1, "unit": "ounce"},
      {"id": "ing-salt", "amount": 0, "unit": ""},
      {"id": "ing-weird", "amount": 1, "unit": "dollop"}
    ]},
    {"yields": 2, "ingredients": [
      {"id": "ing-lime", "amount": 1, "unit": "unit"},
      {"id": "ing-cheese", "amount": 0.5, "unit": "ounce"},
      {"id": "ing-salt", "amount": 0, "unit": ""},
      {"id": "ing-weird", "amount": 0.5, "unit": "dollop"}
    ]}
  ],
  "steps": [
    {"index": 2, "instructions": "• Cook the tacos.", "images": []},
    {"index": 1, "instructions": "• Wash and dry produce.\n• Quarter lime. Finely dice\ntomato.", "images": [{"path": "/aaaa/step-1.jpg"}]}
  ]
}`

func rawFrom(t *testing.T, deliveredID, recipe string) RawRecipe {
	t.Helper()
	return RawRecipe{DeliveredID: deliveredID, FinalURL: "https://www.hellofresh.com/recipes/x", Recipe: json.RawMessage(recipe)}
}

func TestNormalizeRecipe(t *testing.T) {
	history := History{Weeks: []HistoryWeek{
		{Week: "2026-W02", Meals: []HistoryMeal{{ID: "bbbbbbbbbbbbbbbbbbbbbbbb"}}},
		{Week: "2026-W10", Meals: []HistoryMeal{{ID: "cccccccccccccccccccccccc"}}},
	}}
	older := strings.Replace(syntheticRecipe, `"updatedAt": "2026-01-02T00:00:00Z"`, `"updatedAt": "2025-01-01T00:00:00Z"`, 1)
	older = strings.Replace(older, "Synthetic Taco Night", "Old Name", 1)

	file, err := Normalize([]RawRecipe{
		rawFrom(t, "bbbbbbbbbbbbbbbbbbbbbbbb", older),
		rawFrom(t, "cccccccccccccccccccccccc", syntheticRecipe),
	}, history, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if file.Version != ImportVersion || file.Source != "hellofresh" {
		t.Errorf("file header = %d %q", file.Version, file.Source)
	}
	if len(file.Recipes) != 1 {
		t.Fatalf("recipes = %d, want 1 (clones merged)", len(file.Recipes))
	}
	r := file.Recipes[0]

	checks := map[string][2]any{
		"name":         {r.Name, "Synthetic Taco Night"},
		"sourceId":     {r.SourceRecipeID, "aaaaaaaaaaaaaaaaaaaaaaaa"},
		"aliases":      {strings.Join(r.SourceAliases, ","), "bbbbbbbbbbbbbbbbbbbbbbbb,cccccccccccccccccccccccc"},
		"weeks":        {strings.Join(r.OrderWeeks, ","), "2026-W02,2026-W10"},
		"servings":     {len(r.Servings) == 2 && r.Servings[0] == 2 && r.Servings[1] == 4, true},
		"prep":         {r.PrepMinutes, 10},
		"total":        {r.TotalMinutes, 65},
		"cuisines":     {strings.Join(r.Cuisines, ","), "Mexican"},
		"tags":         {strings.Join(r.Tags, ","), "Quick"},
		"image":        {r.ImageURL, imageBaseURL + "/image/aaaa-hero.jpg"},
		"sourceUrl":    {r.SourceURL, "https://www.hellofresh.com/recipes/synthetic-taco-night-aaaaaaaaaaaaaaaaaaaaaaaa"},
		"steps":        {len(r.Steps), 2},
		"step1":        {r.Steps[0].Text, "Wash and dry produce.\nQuarter lime. Finely dice tomato."},
		"step1image":   {r.Steps[0].ImageURL, imageBaseURL + "/aaaa/step-1.jpg"},
		"step2":        {r.Steps[1].Text, "Cook the tacos."},
		"ingredients":  {len(r.Ingredients), 5},
		"nutrition":    {len(r.Nutrition), 1},
		"generatedAt":  {file.GeneratedAt.Format(time.RFC3339), "2026-09-15T00:00:00Z"},
		"allergens":    {strings.Join(r.Allergens, ","), "Milk"},
		"utensils":     {strings.Join(r.Utensils, ","), "Large Pan"},
		"headline":     {r.Headline, "with Lime Crema"},
		"isAddon":      {r.IsAddon, false},
		"difficulty":   {r.Difficulty, 1},
		"description":  {r.Description, "A test recipe."},
		"sourceIsHF":   {r.Source, "hellofresh"},
		"reviewCounts": {len(file.Review), 3},
	}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %v, want %v", name, c[0], c[1])
		}
	}

	byName := map[string]ImportIngredient{}
	for _, ing := range r.Ingredients {
		byName[ing.Name] = ing
	}

	cheese := byName["Parmesan Cheese"]
	if len(cheese.Amounts) != 2 || cheese.Amounts[0].Servings != 2 {
		t.Fatalf("cheese amounts = %+v", cheese.Amounts)
	}
	if a := cheese.Amounts[0]; a.Quantity == nil || *a.Quantity != 0.5 || a.Unit != "oz" || a.RawText != "½ ounce Parmesan Cheese" {
		t.Errorf("cheese 2-serving amount = %+v", a)
	}

	salt := byName["Salt"]
	if !salt.PantryStaple {
		t.Error("salt should be a pantry staple (not shipped)")
	}
	if a := salt.Amounts[0]; a.Quantity != nil || a.Unit != "" || a.RawText != "Salt" {
		t.Errorf("salt amount = %+v, want no invented quantity", a)
	}

	if weird := byName["Mystery Paste"]; weird.Amounts[0].Unit != "" || weird.Amounts[0].SourceUnit != "dollop" {
		t.Errorf("unknown unit handling = %+v", weird.Amounts[0])
	}
	if byName["Lime"].PantryStaple {
		t.Error("lime is shipped, not a pantry staple")
	}

	reasons := []string{}
	for _, rv := range file.Review {
		reasons = append(reasons, rv.Reason)
	}
	joined := strings.Join(reasons, "|")
	if strings.Count(joined, "unknown unit") != 2 || !strings.Contains(joined, "no amounts") {
		t.Errorf("review reasons = %v", reasons)
	}
}

func TestNormalizeFlagsDeliveredVariants(t *testing.T) {
	recipe := strings.Replace(syntheticRecipe, `"id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US",`, `"id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US", "slug": "synthetic-taco-night",`, 1)
	history := History{Weeks: []HistoryWeek{
		{Week: "2026-W01", Meals: []HistoryMeal{{ID: "bbbbbbbbbbbbbbbbbbbbbbbb", Name: "synthetic taco night"}}},
		{Week: "2026-W02", Meals: []HistoryMeal{{ID: "cccccccccccccccccccccccc", Name: "Pork & Synthetic Taco Night"}}},
		{Week: "2026-W03", Meals: []HistoryMeal{{ID: "dddddddddddddddddddddddd", Name: "pork and synthetic taco night"}}},
	}}
	file, err := Normalize([]RawRecipe{
		rawFrom(t, "bbbbbbbbbbbbbbbbbbbbbbbb", recipe),
		rawFrom(t, "cccccccccccccccccccccccc", recipe),
		rawFrom(t, "dddddddddddddddddddddddd", recipe),
	}, history, time.Now())
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	var variants []ReviewItem
	for _, rv := range file.Review {
		if rv.Field == "variant" {
			variants = append(variants, rv)
		}
	}
	if len(variants) != 1 || variants[0].Value != "Pork & Synthetic Taco Night" {
		t.Errorf("variant review items = %+v, want exactly one for the pork variant", variants)
	}
}

// porkVariant is an account capture: the exact delivered variant, which has no
// canonical recipeId.
func porkVariant(deliveredID string) string {
	r := strings.Replace(syntheticRecipe, `"recipeId": "aaaaaaaaaaaaaaaaaaaaaaaa",`, ``, 1)
	r = strings.Replace(r, `"id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US",`, `"id": "`+deliveredID+`", "slug": "pork-synthetic-taco-night",`, 1)
	return strings.Replace(r, " Synthetic Taco Night ", "Pork & Synthetic Taco Night", 1)
}

func TestNormalizePrefersAccountCaptures(t *testing.T) {
	canonical := strings.Replace(syntheticRecipe, `"id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US",`, `"id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US", "slug": "synthetic-taco-night",`, 1)
	history := History{Weeks: []HistoryWeek{
		{Week: "2026-W01", Meals: []HistoryMeal{{ID: "bbbbbbbbbbbbbbbbbbbbbbbb", Name: "synthetic-taco-night"}}},
		{Week: "2026-W02", Meals: []HistoryMeal{{ID: "cccccccccccccccccccccccc", Name: "pork-and-synthetic-taco-night"}}},
		{Week: "2026-W03", Meals: []HistoryMeal{{ID: "dddddddddddddddddddddddd", Name: "pork-synthetic-taco-night"}}},
	}}
	account := func(id string) RawRecipe {
		r := rawFrom(t, id, porkVariant(id))
		r.Origin = OriginAccount
		return r
	}
	file, err := Normalize([]RawRecipe{
		rawFrom(t, "bbbbbbbbbbbbbbbbbbbbbbbb", canonical),
		rawFrom(t, "cccccccccccccccccccccccc", canonical), // redirected to the wrong variant
		account("cccccccccccccccccccccccc"),
		account("dddddddddddddddddddddddd"),
	}, history, time.Now())
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if len(file.Recipes) != 2 {
		t.Fatalf("recipes = %d, want canonical + pork variant", len(file.Recipes))
	}
	byName := map[string]ImportRecipe{}
	for _, r := range file.Recipes {
		byName[r.Name] = r
	}
	if r := byName["Synthetic Taco Night"]; r.SourceRecipeID != "aaaaaaaaaaaaaaaaaaaaaaaa" || strings.Join(r.OrderWeeks, ",") != "2026-W01" {
		t.Errorf("canonical = %s weeks %v, want only the matching delivery", r.SourceRecipeID, r.OrderWeeks)
	}
	pork := byName["Pork & Synthetic Taco Night"]
	if pork.SourceRecipeID != "dddddddddddddddddddddddd" || strings.Join(pork.SourceAliases, ",") != "cccccccccccccccccccccccc" || strings.Join(pork.OrderWeeks, ",") != "2026-W02,2026-W03" {
		t.Errorf("pork variant = id %s aliases %v weeks %v", pork.SourceRecipeID, pork.SourceAliases, pork.OrderWeeks)
	}
	for _, rv := range file.Review {
		if rv.Field == "variant" {
			t.Errorf("unexpected variant review item %+v: captures are exact", rv)
		}
	}
}

func TestNormalizeMergesRepublishedRecipesByName(t *testing.T) {
	older := strings.Replace(syntheticRecipe, `"updatedAt": "2026-01-02T00:00:00Z"`, `"updatedAt": "2023-01-01T00:00:00Z"`, 1)
	republished := strings.ReplaceAll(syntheticRecipe, "aaaaaaaaaaaaaaaaaaaaaaaa", "ffffffffffffffffffffffff")
	history := History{Weeks: []HistoryWeek{
		{Week: "2023-W05", Meals: []HistoryMeal{{ID: "bbbbbbbbbbbbbbbbbbbbbbbb"}}},
		{Week: "2026-W05", Meals: []HistoryMeal{{ID: "cccccccccccccccccccccccc"}}},
	}}
	file, err := Normalize([]RawRecipe{
		rawFrom(t, "bbbbbbbbbbbbbbbbbbbbbbbb", older),
		rawFrom(t, "cccccccccccccccccccccccc", republished),
	}, history, time.Now())
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if len(file.Recipes) != 1 {
		t.Fatalf("recipes = %d, want one merged recipe", len(file.Recipes))
	}
	r := file.Recipes[0]
	if r.SourceRecipeID != "ffffffffffffffffffffffff" ||
		strings.Join(r.SourceAliases, ",") != "aaaaaaaaaaaaaaaaaaaaaaaa,bbbbbbbbbbbbbbbbbbbbbbbb,cccccccccccccccccccccccc" ||
		strings.Join(r.OrderWeeks, ",") != "2023-W05,2026-W05" {
		t.Errorf("merged = id %s aliases %v weeks %v", r.SourceRecipeID, r.SourceAliases, r.OrderWeeks)
	}
}

func TestPendingVariants(t *testing.T) {
	canonical := strings.Replace(syntheticRecipe, `"id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US",`, `"id": "aaaaaaaaaaaaaaaaaaaaaaaa-en-US", "slug": "synthetic-taco-night",`, 1)
	history := History{Weeks: []HistoryWeek{
		{Week: "2026-W01", Meals: []HistoryMeal{{ID: "bbbbbbbbbbbbbbbbbbbbbbbb", Name: "synthetic-and-taco-night"}}},
		{Week: "2026-W02", Meals: []HistoryMeal{{ID: "cccccccccccccccccccccccc", Name: "pork-synthetic-taco-night"}}},
		{Week: "2026-W05", Meals: []HistoryMeal{{ID: "dddddddddddddddddddddddd", Name: "beef-synthetic-taco-night"}}},
		{Week: "2026-W06", Meals: []HistoryMeal{{ID: "eeeeeeeeeeeeeeeeeeeeeeee", Name: "turkey-synthetic-taco-night"}}},
	}}
	captured := rawFrom(t, "eeeeeeeeeeeeeeeeeeeeeeee", porkVariant("eeeeeeeeeeeeeeeeeeeeeeee"))
	captured.Origin = OriginAccount
	pending, err := PendingVariants([]RawRecipe{
		rawFrom(t, "bbbbbbbbbbbbbbbbbbbbbbbb", canonical),
		rawFrom(t, "cccccccccccccccccccccccc", canonical),
		rawFrom(t, "dddddddddddddddddddddddd", canonical),
		rawFrom(t, "eeeeeeeeeeeeeeeeeeeeeeee", canonical),
		captured,
	}, history)
	if err != nil {
		t.Fatalf("PendingVariants() error = %v", err)
	}
	var got []string
	for _, p := range pending {
		got = append(got, p.DeliveredID[:1]+"@"+p.LastWeek)
	}
	if strings.Join(got, ",") != "d@2026-W05,c@2026-W02" {
		t.Errorf("pending = %v, want beef then pork (newest first; stopword and captured ones skipped)", got)
	}
}

func TestVariantKey(t *testing.T) {
	want := variantKey("Rigatoni with Beef & Zucchini Ragu")
	for _, s := range []string{"rigatoni-with-beef-and-zucchini-ragu", "rigatoni-beef-zucchini-ragu"} {
		if got := variantKey(s); got != want {
			t.Errorf("variantKey(%q) = %q, want %q", s, got, want)
		}
	}
	if variantKey("pork-schnitzel") == variantKey("chicken-schnitzel") {
		t.Error("different proteins must be different variants")
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"One-Pan Pork & Green Pepper Tacos":   "one-pan-pork-and-green-pepper-tacos",
		"one pan pork and green pepper tacos": "one-pan-pork-and-green-pepper-tacos",
		"  Che Buono!  Chicken ":              "che-buono-chicken",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseISODurationMinutes(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"PT30M", 30, true},
		{"PT1H5M", 65, true},
		{"PT2H", 120, true},
		{"P1D", 1440, true},
		{"PT90S", 2, true},
		{"", 0, false},
		{"PT", 0, false},
		{"30 minutes", 0, false},
	}
	for _, tt := range tests {
		got, ok := ParseISODurationMinutes(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseISODurationMinutes(%q) = %d, %v; want %d, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestFormatQuantity(t *testing.T) {
	tests := map[float64]string{
		1:       "1",
		0.5:     "½",
		0.25:    "¼",
		1.5:     "1 ½",
		2.75:    "2 ¾",
		1.0 / 3: "⅓",
		0.6:     "0.6",
		10:      "10",
	}
	for in, want := range tests {
		if got := FormatQuantity(in); got != want {
			t.Errorf("FormatQuantity(%v) = %q, want %q", in, got, want)
		}
	}
}
