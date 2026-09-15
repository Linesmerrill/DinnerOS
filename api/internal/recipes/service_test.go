package recipes

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// --- Synthetic fixtures (never real order history) ----------------------------

func qty(f float64) *float64 { return &f }

func testRecipe(id, name string, weeks ...string) ImportRecipe {
	return ImportRecipe{
		Source:         SourceHelloFresh,
		SourceRecipeID: id,
		SourceURL:      "https://recipes.example.com/" + id,
		Name:           name,
		Headline:       "with test sauce",
		Servings:       []int{2, 4},
		TotalMinutes:   30,
		Tags:           []string{"Quick"},
		Cuisines:       []string{"Mexican"},
		Nutrition:      []ImportNutrient{{Name: "Calories", Amount: 640, Unit: "kcal"}},
		Ingredients: []ImportIngredient{
			{SourceIngredientID: "ing-garlic", Name: "Garlic", Amounts: []ImportAmount{
				{Servings: 2, Quantity: qty(2), Unit: "clove", SourceUnit: "clove", RawText: "2 clove Garlic"},
				{Servings: 4, Quantity: qty(4), Unit: "clove", SourceUnit: "clove", RawText: "4 clove Garlic"},
			}},
			{SourceIngredientID: "ing-salt", Name: "Salt", PantryStaple: true, Amounts: []ImportAmount{
				{Servings: 2, RawText: "Salt"},
			}},
		},
		Steps:      []ImportStep{{Index: 1, Text: "Cook everything."}},
		OrderWeeks: weeks,
	}
}

func testFile(recipes ...ImportRecipe) ImportFile {
	return ImportFile{Version: ImportVersion, Source: SourceHelloFresh, GeneratedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Recipes: recipes}
}

func mustImport(t *testing.T, svc *Service, householdID string, file ImportFile) ImportResult {
	t.Helper()
	res, err := svc.Import(context.Background(), householdID, file)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	return res
}

func onlyRecipe(t *testing.T, svc *Service, householdID string) Recipe {
	t.Helper()
	page, err := svc.List(context.Background(), householdID, ListQuery{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("List() = %+v, %v; want exactly one recipe", page.Items, err)
	}
	r, err := svc.Get(context.Background(), householdID, page.Items[0].ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	return r
}

// --- Import -------------------------------------------------------------------

func TestImportRejectsUnusableFiles(t *testing.T) {
	svc := NewService(newMemoryStore())
	ctx := context.Background()

	wrongVersion := testFile(testRecipe("r1", "Tacos"))
	wrongVersion.Version = 2
	if _, err := svc.Import(ctx, "hh", wrongVersion); !errors.Is(err, ErrInvalidImport) {
		t.Errorf("version 2 error = %v, want ErrInvalidImport", err)
	}
	wrongSource := testFile()
	wrongSource.Source = "grocer"
	if _, err := svc.Import(ctx, "hh", wrongSource); !errors.Is(err, ErrInvalidImport) {
		t.Errorf("unknown source error = %v, want ErrInvalidImport", err)
	}
	if _, err := svc.Import(ctx, "", testFile()); !errors.Is(err, errHouseholdRequired) {
		t.Errorf("no household error = %v, want errHouseholdRequired", err)
	}
}

func TestImportReportsInvalidRecipesAndImportsTheRest(t *testing.T) {
	bad := func(id string, mutate func(*ImportRecipe)) ImportRecipe {
		r := testRecipe(id, "Recipe "+id)
		mutate(&r)
		return r
	}
	file := testFile(
		testRecipe("ok-1", "Good Tacos"),
		bad("no-name", func(r *ImportRecipe) { r.Name = "  " }),
		bad("bad-source", func(r *ImportRecipe) { r.Source = "grocer" }),
		bad("bad-unit", func(r *ImportRecipe) { r.Ingredients[0].Amounts[0].Unit = "dollop" }),
		bad("bad-servings", func(r *ImportRecipe) { r.Servings = []int{0, 2} }),
		bad("bad-amount-servings", func(r *ImportRecipe) { r.Ingredients[1].Amounts[0].Servings = -2 }),
		bad("bad-quantity", func(r *ImportRecipe) { r.Ingredients[0].Amounts[0].Quantity = qty(-1) }),
		bad("bad-week", func(r *ImportRecipe) { r.OrderWeeks = []string{"2026-30"} }),
		bad("", func(*ImportRecipe) {}),
		bad("no-ingredient-name", func(r *ImportRecipe) { r.Ingredients[0].Name = "!!" }),
		testRecipe("ok-1", "Duplicate Tacos"),
		bad("ok-2", func(r *ImportRecipe) { r.SourceAliases = []string{"ok-1"} }),
		testRecipe("ok-3", "Other Good Tacos"),
	)
	wantProblems := map[int]string{
		1:  "name is required",
		2:  `source "grocer" is not supported`,
		3:  `unit "dollop" is not a DinnerOS unit code`,
		4:  "servings must be positive",
		5:  "amount servings must be positive",
		6:  "quantity -1 is invalid",
		7:  `"2026-30" is not an ISO week`,
		8:  "sourceRecipeId is required",
		9:  "ingredients[0]: name is required",
		10: "shares a source ID with recipes[0]",
		11: "shares a source ID with recipes[0]",
	}

	store := newMemoryStore()
	res := mustImport(t, NewService(store), "hh", file)

	if res.Created != 2 || res.Updated != 0 || res.Unchanged != 0 || len(store.recipes) != 2 {
		t.Errorf("result = %+v, stored %d; want 2 created", res, len(store.recipes))
	}
	if len(res.Errors) != len(wantProblems) {
		t.Fatalf("errors = %+v, want %d", res.Errors, len(wantProblems))
	}
	for i, e := range res.Errors {
		if i > 0 && res.Errors[i-1].Index >= e.Index {
			t.Errorf("errors not in file order: %+v", res.Errors)
		}
		want, ok := wantProblems[e.Index]
		if !ok || !strings.Contains(strings.Join(e.Problems, "; "), want) {
			t.Errorf("errors[%d] = %+v, want problem containing %q", e.Index, e, want)
		}
	}
	if e := res.Errors[0]; e.SourceRecipeID != "no-name" {
		t.Errorf("error identifies recipe as %q, want no-name", e.SourceRecipeID)
	}
}

func TestImportResolvesAndCategorizesCatalogIngredients(t *testing.T) {
	store := newMemoryStore()
	svc := NewService(store)

	r2 := testRecipe("r2", "Bowls")
	r2.Ingredients = append(r2.Ingredients,
		// Same name as "Garlic" under a different source ID: one catalog entry.
		ImportIngredient{SourceIngredientID: "ing-garlic-2", Name: "garlic"},
		ImportIngredient{SourceIngredientID: "ing-zorb", Name: "Zorblax Root", ImageURL: "https://img.example.com/zorb.png"},
		ImportIngredient{Name: "Sour Cream"},
	)
	res := mustImport(t, svc, "hh", testFile(testRecipe("r1", "Tacos"), r2))
	if res.IngredientsCreated != 4 || len(store.ingredients) != 4 {
		t.Fatalf("IngredientsCreated = %d, catalog = %+v; want 4", res.IngredientsCreated, store.ingredients)
	}

	byKey := map[string]Ingredient{}
	for _, ing := range store.ingredients {
		byKey[ing.Key] = ing
	}
	checks := []struct {
		key, category string
		confident     bool
		refs          []string
	}{
		{"garlic", ingredients.CategoryProduce, true, []string{"ing-garlic", "ing-garlic-2"}},
		{"salt", ingredients.CategorySpices, true, []string{"ing-salt"}},
		{"zorblax root", ingredients.CategoryOther, false, []string{"ing-zorb"}},
		{"sour cream", ingredients.CategoryDairyEggs, true, nil},
	}
	for _, c := range checks {
		ing, ok := byKey[c.key]
		var refs []string
		for _, ref := range ing.SourceRefs {
			refs = append(refs, ref.SourceIngredientID)
		}
		if !ok || ing.Category != c.category || ing.CategoryConfident != c.confident || !slices.Equal(refs, c.refs) {
			t.Errorf("catalog[%q] = %+v, want category %s confident=%v refs %v", c.key, ing, c.category, c.confident, c.refs)
		}
	}
	if byKey["zorblax root"].ImageURL == "" || byKey["zorblax root"].Name != "Zorblax Root" {
		t.Errorf("zorblax = %+v, want name and image kept", byKey["zorblax root"])
	}

	// A renamed ingredient with a known source ID resolves by ID, and a new
	// source ID for a known name is attached to the existing entry.
	r3 := testRecipe("r3", "Stew")
	r3.Ingredients = []ImportIngredient{
		{SourceIngredientID: "ing-zorb", Name: "Zorblax Tuber"},
		{SourceIngredientID: "ing-garlic-3", Name: "Garlic"},
	}
	res = mustImport(t, svc, "hh", testFile(r3))
	if res.IngredientsCreated != 0 || len(store.ingredients) != 4 {
		t.Fatalf("second import created %d ingredients (catalog %d), want 0", res.IngredientsCreated, len(store.ingredients))
	}
	var stew Recipe
	for _, r := range store.recipes {
		if r.SourceRecipeID == "r3" {
			stew = r
		}
	}
	if stew.Ingredients[0].IngredientID != byKey["zorblax root"].ID || stew.Ingredients[1].IngredientID != byKey["garlic"].ID {
		t.Errorf("stew ingredients = %+v, want zorblax and garlic catalog IDs", stew.Ingredients)
	}
	garlic, _ := store.FindIngredients(context.Background(), nil, []string{"garlic"})
	if len(garlic) != 1 || len(garlic[0].SourceRefs) != 3 {
		t.Errorf("garlic = %+v, want 3 source refs", garlic)
	}

	// Get joins categories from the catalog.
	got, err := svc.Get(context.Background(), "hh", stew.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ingredients[0].Category != ingredients.CategoryOther || got.Ingredients[1].Category != ingredients.CategoryProduce {
		t.Errorf("Get() ingredient categories = %+v", got.Ingredients)
	}
}

func TestImportStoresExactQuantities(t *testing.T) {
	r := testRecipe("r1", "Fractions")
	r.Ingredients = []ImportIngredient{{SourceIngredientID: "ing-flour", Name: "Flour", Amounts: []ImportAmount{
		{Servings: 1, Quantity: qty(0.333), Unit: "cup"},
		{Servings: 2, Quantity: qty(0.25), Unit: "cup"},
		{Servings: 3, Quantity: qty(1.5), Unit: "cup"},
		{Servings: 4, Quantity: qty(2.7), Unit: "cup"},
		{Servings: 5, Quantity: nil, Unit: ""},
	}}}
	svc := NewService(newMemoryStore())
	mustImport(t, svc, "hh", testFile(r))

	want := []struct {
		quantity string
		value    float64
		ok       bool
	}{{"1/3", 1.0 / 3, true}, {"1/4", 0.25, true}, {"3/2", 1.5, true}, {"27/10", 2.7, true}, {"", 0, false}}
	amounts := onlyRecipe(t, svc, "hh").Ingredients[0].Amounts
	for i, w := range want {
		q, ok := amounts[i].ExactQuantity()
		if amounts[i].Quantity != w.quantity || ok != w.ok || (ok && q.Float64() != w.value) {
			t.Errorf("amounts[%d] = %+v (ok=%v float=%v), want %q", i, amounts[i], ok, q.Float64(), w.quantity)
		}
	}
}

func TestImportIsIdempotent(t *testing.T) {
	store := newMemoryStore()
	svc := NewService(store)
	clock := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return clock }

	file := testFile(testRecipe("r1", "Tacos", "2026-W10"), testRecipe("r2", "Bowls"))
	review := ImportReviewItem{SourceRecipeID: "r1", RecipeName: "Tacos", Field: "ingredients.Mystery.unit", Value: "dollop", Reason: "unknown unit"}
	file.Review = []ImportReviewItem{review, review, {SourceRecipeID: "r2", RecipeName: "Bowls", Field: "steps", Reason: "recipe has no steps"}}

	first := mustImport(t, svc, "hh", file)
	if first.Created != 2 || first.IngredientsCreated != 2 || first.ReviewItems != 2 {
		t.Fatalf("first import = %+v", first)
	}

	clock = clock.Add(time.Hour)
	second := mustImport(t, svc, "hh", file)
	want := ImportResult{Unchanged: 2, ReviewItems: 2}
	if second.Created != want.Created || second.Updated != 0 || second.Unchanged != 2 || second.IngredientsCreated != 0 || second.ReviewItems != 2 || len(second.Errors) != 0 {
		t.Errorf("second import = %+v, want %+v", second, want)
	}
	if store.saveRecipesCalls != 1 || store.upsertCalls != 1 || len(store.reviews) != 2 || len(store.recipes) != 2 {
		t.Errorf("second import wrote: saves=%d upserts=%d reviews=%d recipes=%d", store.saveRecipesCalls, store.upsertCalls, len(store.reviews), len(store.recipes))
	}

	// A real change updates the recipe and keeps its identity and creation time.
	changed := testFile(testRecipe("r1", "Tacos Deluxe", "2026-W10"))
	third := mustImport(t, svc, "hh", changed)
	if third.Updated != 1 || third.Created != 0 {
		t.Fatalf("changed import = %+v, want 1 updated", third)
	}
	for _, r := range store.recipes {
		if r.SourceRecipeID == "r1" && (r.Name != "Tacos Deluxe" || !r.UpdatedAt.Equal(clock) || r.CreatedAt.Equal(clock)) {
			t.Errorf("updated recipe = %+v", r)
		}
	}
}

func TestImportMatchesAliases(t *testing.T) {
	store := newMemoryStore()
	svc := NewService(store)

	// First seen as a weekly menu clone with no canonical ID yet.
	mustImport(t, svc, "hh", testFile(testRecipe("menu-1", "Tacos", "2026-W01")))
	created := store.recipes[0]

	canonical := testRecipe("canon-1", "Tacos", "2026-W02")
	canonical.SourceAliases = []string{"menu-1", "menu-2"}
	res := mustImport(t, svc, "hh", testFile(canonical))
	if res.Updated != 1 || res.Created != 0 || len(store.recipes) != 1 {
		t.Fatalf("result = %+v with %d stored recipes, want the clone updated in place", res, len(store.recipes))
	}
	got := store.recipes[0]
	if got.ID != created.ID || got.SourceRecipeID != "canon-1" || !slices.Equal(got.SourceAliases, []string{"menu-1", "menu-2"}) {
		t.Errorf("recipe identity = id %s source %q aliases %v", got.ID, got.SourceRecipeID, got.SourceAliases)
	}
	if !slices.Equal(got.OrderWeeks, []string{"2026-W01", "2026-W02"}) || got.TimesOrdered != 2 || got.LastOrderedWeek != "2026-W02" {
		t.Errorf("order history = %v times=%d last=%q", got.OrderWeeks, got.TimesOrdered, got.LastOrderedWeek)
	}

	// Two recipes in one file that resolve to the same stored recipe: the
	// second is rejected rather than silently merged.
	res = mustImport(t, svc, "hh", testFile(testRecipe("menu-1", "Tacos", "2026-W03"), testRecipe("menu-2", "Tacos", "2026-W04")))
	if res.Updated != 1 || len(res.Errors) != 1 || res.Errors[0].Index != 1 || !strings.Contains(res.Errors[0].Problems[0], "same stored recipe as recipes[0]") {
		t.Errorf("result = %+v, want second recipe rejected", res)
	}
	if len(store.recipes) != 1 {
		t.Errorf("stored %d recipes, want 1", len(store.recipes))
	}
}

func TestImportUnionsOrderWeeks(t *testing.T) {
	store := newMemoryStore()
	svc := NewService(store)

	mustImport(t, svc, "hh", testFile(testRecipe("r1", "Tacos", "2026-W30", "2026-W10", "2026-W30")))
	if got := store.recipes[0].OrderWeeks; !slices.Equal(got, []string{"2026-W10", "2026-W30"}) {
		t.Fatalf("weeks = %v, want sorted unique", got)
	}
	if res := mustImport(t, svc, "hh", testFile(testRecipe("r1", "Tacos", "2026-W20", "2026-W10"))); res.Updated != 1 {
		t.Fatalf("result = %+v, want updated", res)
	}
	got := store.recipes[0]
	if !slices.Equal(got.OrderWeeks, []string{"2026-W10", "2026-W20", "2026-W30"}) || got.TimesOrdered != 3 || got.LastOrderedWeek != "2026-W30" {
		t.Errorf("recipe weeks = %v times=%d last=%q", got.OrderWeeks, got.TimesOrdered, got.LastOrderedWeek)
	}
	// A file with only some of the weeks never removes history.
	if res := mustImport(t, svc, "hh", testFile(testRecipe("r1", "Tacos", "2026-W10"))); res.Unchanged != 1 {
		t.Errorf("subset import = %+v, want unchanged", res)
	}
}

func TestRecipesAreScopedByHousehold(t *testing.T) {
	store := newMemoryStore()
	svc := NewService(store)
	ctx := context.Background()
	file := testFile(testRecipe("r1", "Tacos"))

	if res := mustImport(t, svc, "hh-a", file); res.Created != 1 {
		t.Fatalf("hh-a import = %+v", res)
	}
	if res := mustImport(t, svc, "hh-b", file); res.Created != 1 {
		t.Fatalf("hh-b import = %+v, want its own copy", res)
	}
	a := onlyRecipe(t, svc, "hh-a")
	if _, err := svc.Get(ctx, "hh-b", a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(other household) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(ctx, "", a.ID); !errors.Is(err, errHouseholdRequired) {
		t.Errorf("Get(no household) error = %v, want errHouseholdRequired", err)
	}
	// The catalog is global: the second household reused the ingredients.
	if len(store.ingredients) != 2 {
		t.Errorf("catalog has %d ingredients, want 2", len(store.ingredients))
	}
}

// --- List ---------------------------------------------------------------------

// listFixture is shared with the MongoDB integration test so both stores are
// held to the same ordering contract.
func listFixture() ImportFile {
	recipe := func(id, name string, addon bool, tags, cuisines []string, weeks ...string) ImportRecipe {
		r := testRecipe(id, name, weeks...)
		r.IsAddon, r.Tags, r.Cuisines = addon, tags, cuisines
		return r
	}
	return testFile(
		recipe("r-apple", "apple Crumble", true, []string{"Dessert"}, nil, "2026-W01"),
		recipe("r-beef", "Beef Tacos", false, []string{"Quick"}, []string{"Mexican"}, "2026-W05", "2026-W10", "2026-W12"),
		recipe("r-chicken", "Chicken Bowl", false, []string{"quick"}, []string{"Asian"}, "2026-W12"),
		recipe("r-duck", "Duck Salad", false, nil, nil),
		recipe("r-egg", "egg Fried Rice", false, nil, []string{"Asian"}, "2026-W03", "2026-W04"),
		recipe("r-spicy-paren", "Tofu (Spicy)", false, nil, nil),
		recipe("r-spicy", "Tofu Spicy", false, nil, nil),
		recipe("r-dot", "Pasta a.b", false, nil, nil),
		recipe("r-x", "Pasta axb", false, nil, nil),
		recipe("r-bracket", "Rice [special]", false, nil, nil),
	)
}

func runListContract(t *testing.T, svc *Service, householdID string) {
	t.Helper()
	ctx := context.Background()
	names := func(items []RecipeSummary) []string {
		out := []string{}
		for _, it := range items {
			out = append(out, it.Name)
		}
		return out
	}
	allPages := func(t *testing.T, q ListQuery) []string {
		t.Helper()
		var out []string
		for range 20 {
			page, err := svc.List(ctx, householdID, q)
			if err != nil {
				t.Fatalf("List(%+v) error = %v", q, err)
			}
			if len(page.Items) > q.Limit {
				t.Fatalf("page has %d items, limit %d", len(page.Items), q.Limit)
			}
			out = append(out, names(page.Items)...)
			if page.NextCursor == "" {
				return out
			}
			q.Cursor = page.NextCursor
		}
		t.Fatal("pagination did not terminate")
		return nil
	}
	yes, no := true, false

	orders := []struct {
		sort Sort
		want []string
	}{
		{SortName, []string{"apple Crumble", "Beef Tacos", "Chicken Bowl", "Duck Salad", "egg Fried Rice", "Pasta a.b", "Pasta axb", "Rice [special]", "Tofu (Spicy)", "Tofu Spicy"}},
		{SortRecent, []string{"Beef Tacos", "Chicken Bowl", "egg Fried Rice", "apple Crumble", "Duck Salad", "Pasta a.b", "Pasta axb", "Rice [special]", "Tofu (Spicy)", "Tofu Spicy"}},
		{SortPopular, []string{"Beef Tacos", "egg Fried Rice", "apple Crumble", "Chicken Bowl", "Duck Salad", "Pasta a.b", "Pasta axb", "Rice [special]", "Tofu (Spicy)", "Tofu Spicy"}},
	}
	for _, o := range orders {
		t.Run("sort "+string(o.sort), func(t *testing.T) {
			for _, limit := range []int{1, 3, 50} {
				if got := allPages(t, ListQuery{Sort: o.sort, Limit: limit}); !slices.Equal(got, o.want) {
					t.Errorf("limit %d pages = %v, want %v", limit, got, o.want)
				}
			}
		})
	}

	filters := []struct {
		name string
		q    ListQuery
		want []string
	}{
		{"add-ons only", ListQuery{Addons: &yes}, []string{"apple Crumble"}},
		{"main meals only", ListQuery{Addons: &no, Search: "o"}, []string{"Beef Tacos", "Chicken Bowl", "Tofu (Spicy)", "Tofu Spicy"}},
		{"add-on filter combines with search", ListQuery{Addons: &yes, Search: "o"}, []string{}},
		{"tag is case-insensitive", ListQuery{Tag: "QUICK"}, []string{"Beef Tacos", "Chicken Bowl"}},
		{"cuisine", ListQuery{Cuisine: "asian", Sort: SortPopular}, []string{"egg Fried Rice", "Chicken Bowl"}},
		{"search is case-insensitive", ListQuery{Search: " TOFU "}, []string{"Tofu (Spicy)", "Tofu Spicy"}},
		{"search escapes parentheses", ListQuery{Search: "(spicy)"}, []string{"Tofu (Spicy)"}},
		{"search escapes dot", ListQuery{Search: "a.b"}, []string{"Pasta a.b"}},
		{"search escapes brackets", ListQuery{Search: "[special"}, []string{"Rice [special]"}},
		{"search .* is literal", ListQuery{Search: ".*"}, []string{}},
	}
	for _, f := range filters {
		t.Run(f.name, func(t *testing.T) {
			page, err := svc.List(ctx, householdID, f.q)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if got := names(page.Items); !slices.Equal(got, f.want) || page.NextCursor != "" {
				t.Errorf("names = %v next=%q, want %v", got, page.NextCursor, f.want)
			}
		})
	}

	t.Run("invalid queries", func(t *testing.T) {
		first, err := svc.List(ctx, householdID, ListQuery{Limit: 1})
		if err != nil || first.NextCursor == "" {
			t.Fatalf("List() = %+v, %v", first, err)
		}
		for _, q := range []ListQuery{
			{Sort: "spicy"},
			{Limit: -1},
			{Cursor: "not a cursor!"},
			{Cursor: first.NextCursor, Sort: SortRecent},
			{Search: strings.Repeat("a", maxFilterLength+1)},
		} {
			if _, err := svc.List(ctx, householdID, q); !errors.Is(err, ErrInvalidQuery) {
				t.Errorf("List(%+v) error = %v, want ErrInvalidQuery", q, err)
			}
		}
	})
}

func TestListContract(t *testing.T) {
	svc := NewService(newMemoryStore())
	mustImport(t, svc, "hh", listFixture())
	mustImport(t, svc, "other-hh", testFile(testRecipe("r-other", "Another Household Recipe")))
	runListContract(t, svc, "hh")
}

func TestListQueryLimits(t *testing.T) {
	for _, tt := range []struct{ in, want int }{{0, DefaultListLimit}, {7, 7}, {MaxListLimit + 1, MaxListLimit}} {
		f, err := ListQuery{Limit: tt.in}.filter()
		if err != nil || f.Limit != tt.want || f.Sort != SortName {
			t.Errorf("filter(limit %d) = %+v, %v; want limit %d sorted by name", tt.in, f, err, tt.want)
		}
	}
}
