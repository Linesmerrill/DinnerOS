package substitutes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// TestIntegrationSpecialtyStrategy runs the household strategy on MongoDB: the
// settings endpoints, and a week's grocery list built under each strategy with
// no per-ingredient choice at all.
func TestIntegrationSpecialtyStrategy(t *testing.T) {
	ctx := context.Background()
	client := newTestMongoClient(t)
	if err := client.EnsureIndexes(ctx, append(append(recipes.Indexes(), pantry.Indexes()...), planning.Indexes()...)...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	recipeSvc := recipes.NewService(recipes.NewMongoStore(db))
	if _, err := recipeSvc.Import(ctx, testHousehold, recipes.ImportFile{
		Version: recipes.ImportVersion, Source: recipes.SourceHelloFresh, GeneratedAt: testNow,
		Recipes: []recipes.ImportRecipe{
			importRecipe("r-tacos", "Smoky Pork Tacos",
				importLine("i-texmex", "Tex-Mex Paste", 2, "tbsp"),
				importLine("i-southwest", "Southwest Spice Blend", 1, "tbsp")),
		},
	}); err != nil {
		t.Fatal(err)
	}
	pantryStore := pantry.NewMongoStore(db)
	pantrySvc := pantry.NewService(pantryStore, recipeSvc).WithUsage(pantry.UsageOptions{Store: pantryStore, Recipes: recipeSvc})
	store := NewMongoStore(db)
	if err := EnsureSeed(ctx, store, nil); err != nil {
		t.Fatal(err)
	}
	rec := &fakeRecorder{}
	svc := NewService(ServiceOptions{Store: store, Catalog: recipeSvc, Recipes: recipeSvc, Pantry: pantrySvc}).WithEvents(rec)
	pantrySvc.SetKeyResolver(svc)
	plans := planning.NewService(planning.NewMongoStore(db), recipeSvc).WithPantry(pantrySvc).WithSpecialties(svc)
	actor := member(testHousehold)

	page, err := recipeSvc.List(ctx, testHousehold, recipes.ListQuery{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("recipes = %+v, %v", page.Items, err)
	}
	if _, _, err := plans.AddEntry(ctx, testHousehold, testUser, "2026-W38", planning.NewEntry{RecipeID: page.Items[0].ID, Servings: 2}); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	router.Route("/api/v1", NewHandler(HandlerOptions{
		Service: svc, Pantry: pantrySvc, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{
			testHousehold + "/" + testUser:   {households.PermHouseholdView, households.PermPantryEdit},
			testHousehold + "/" + userViewer: {households.PermHouseholdView},
		},
	}).Mount)
	do := func(method, path, body, userID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/households/"+testHousehold+"/specialty-ingredients"+path, strings.NewReader(body))
		if userID != "" {
			req.Header.Set("Authorization", "Bearer token-"+userID)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}

	// A household that never set one defaults to similar, with no author.
	if set := decode[SpecialtySettingsResponse](t, do(http.MethodGet, "/settings", "", userViewer), http.StatusOK); set.Strategy != StrategySimilar ||
		len(set.Options) != 3 || set.UpdatedBy != nil || set.UpdatedAt != nil {
		t.Fatalf("default settings = %+v", set)
	}
	wantError(t, do(http.MethodPut, "/settings", `{"strategy":"closest"}`, userViewer), http.StatusForbidden, "forbidden")

	// The same week, resolved differently under each strategy, storing nothing.
	for _, tc := range []struct {
		strategy   Strategy
		wantItems  []string
		wantAbsent []string
		batches    int
	}{
		{strategy: StrategySimilar, wantItems: []string{"Tomato Paste", "Smoky Chipotle Bouillon Base", "Chili Powder"},
			wantAbsent: []string{"Tex-Mex Paste", "Southwest Spice Blend"}},
		{strategy: StrategyClosest, wantItems: []string{"Tomato Paste", "Smoked Paprika", "Garlic Powder"},
			wantAbsent: []string{"Tex-Mex Paste", "Southwest Spice Blend"}, batches: 2},
		{strategy: StrategyAsk, wantItems: []string{"Tex-Mex Paste", "Southwest Spice Blend"}},
	} {
		t.Run(string(tc.strategy), func(t *testing.T) {
			set := decode[SpecialtySettingsResponse](t, do(http.MethodPut, "/settings", `{"strategy":"`+string(tc.strategy)+`"}`, testUser), http.StatusOK)
			if set.Strategy != tc.strategy || set.UpdatedBy == nil || *set.UpdatedBy != testUser || set.UpdatedAt == nil {
				t.Fatalf("settings = %+v", set)
			}
			g, err := plans.GroceryList(ctx, testHousehold, "2026-W38")
			if err != nil {
				t.Fatal(err)
			}
			items := groceryItems(g)
			for _, name := range tc.wantItems {
				if _, ok := items[name]; !ok {
					t.Errorf("%q is missing from the list", name)
				}
			}
			for _, name := range tc.wantAbsent {
				if _, ok := items[name]; ok {
					t.Errorf("%q is still on the list", name)
				}
			}
			if len(g.Batches) != tc.batches {
				t.Errorf("batches = %d, want %d", len(g.Batches), tc.batches)
			}
			// Resolved at read time: building a list stores no choice.
			if choices, err := store.ListChoices(ctx, testHousehold); err != nil || len(choices) != 0 {
				t.Errorf("stored choices = %+v, %v", choices, err)
			}
		})
	}

	// An explicit choice wins; the strategy still covers everything else.
	if _, err := svc.SetChoice(ctx, actor, "tex-mex-paste", OptionAsIs); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetStrategy(ctx, actor, string(StrategySimilar)); err != nil {
		t.Fatal(err)
	}
	g, err := plans.GroceryList(ctx, testHousehold, "2026-W38")
	if err != nil {
		t.Fatal(err)
	}
	items := groceryItems(g)
	if _, ok := items["Tex-Mex Paste"]; !ok {
		t.Error("an explicit as_is must keep Tex-Mex Paste on the list")
	}
	if _, ok := items["Southwest Spice Blend"]; ok {
		t.Error("the strategy must still replace Southwest Spice Blend")
	}
	// Provenance says the strategy produced the line.
	blend := items["Chili Powder"]
	if len(blend.Via) == 0 || blend.Via[0].Strategy != string(StrategySimilar) {
		t.Errorf("chili powder via = %+v", blend.Via)
	}

	list := decode[SpecialtyListResponse](t, do(http.MethodGet, "", "", testUser), http.StatusOK)
	var tex, sw SpecialtyResponse
	for _, it := range list.Items {
		switch it.ID {
		case "tex-mex-paste":
			tex = it
		case "southwest-spice-blend":
			sw = it
		}
	}
	if tex.ChoiceSource != ChoiceSourceHousehold || tex.Choice == nil || tex.Choice.Type != OptionAsIs || tex.Choice.ChosenBy == nil {
		t.Errorf("tex-mex = %s choice %+v", tex.ChoiceSource, tex.Choice)
	}
	if sw.ChoiceSource != ChoiceSourceStrategy || sw.Choice == nil || sw.Choice.Strategy == nil ||
		*sw.Choice.Strategy != StrategySimilar || sw.Choice.ChosenBy != nil {
		t.Errorf("southwest = %s choice %+v", sw.ChoiceSource, sw.Choice)
	}
	if types := rec.types(); len(types) != 4 {
		t.Errorf("recorded events = %v, want 4 strategy updates", types)
	}
}

func importLine(id, name string, quantity float64, unit string) recipes.ImportIngredient {
	return recipes.ImportIngredient{SourceIngredientID: id, Name: name, Amounts: []recipes.ImportAmount{
		{Servings: 2, Quantity: &quantity, Unit: unit, SourceUnit: unit, RawText: name},
	}}
}

func importRecipe(id, name string, lines ...recipes.ImportIngredient) recipes.ImportRecipe {
	return recipes.ImportRecipe{
		Source: recipes.SourceHelloFresh, SourceRecipeID: id, SourceURL: "https://recipes.example.com/" + id, Name: name,
		Servings: []int{2}, TotalMinutes: 30, Ingredients: lines, Steps: []recipes.ImportStep{{Index: 1, Text: "Cook."}},
	}
}

func groceryItems(g planning.GroceryList) map[string]grocery.Item {
	out := map[string]grocery.Item{}
	for _, c := range g.Categories {
		for _, it := range c.Items {
			out[it.Name] = it
		}
	}
	return out
}

func amountsOf(it grocery.Item) []string {
	var out []string
	for _, a := range it.Amounts {
		out = append(out, a.Quantity.String()+" "+a.Unit.Code)
	}
	return out
}

// TestIntegrationGroceryAndBatchDeduction runs the whole flow on MongoDB:
// choices change the week's grocery list, a batch made starts a pantry cycle,
// cooking deducts from it (including an alias counted in packets), and a low
// batch goes back on the list.
func TestIntegrationGroceryAndBatchDeduction(t *testing.T) {
	ctx := context.Background()
	client := newTestMongoClient(t)
	if err := client.EnsureIndexes(ctx, append(append(recipes.Indexes(), pantry.Indexes()...), planning.Indexes()...)...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	recipeSvc := recipes.NewService(recipes.NewMongoStore(db))
	if _, err := recipeSvc.Import(ctx, testHousehold, recipes.ImportFile{
		Version: recipes.ImportVersion, Source: recipes.SourceHelloFresh, GeneratedAt: testNow,
		Recipes: []recipes.ImportRecipe{
			importRecipe("r-tacos", "Smoky Pork Tacos",
				importLine("i-texmex", "Tex-Mex Paste", 2, "tbsp"),
				importLine("i-southwest", "Southwest Spice Blend", 1, "count"),
				importLine("i-onion", "Yellow Onion", 1, "count")),
			importRecipe("r-bowls", "Chili Bowls",
				importLine("i-southwestern", "Southwestern Spice Blend", 1, "tbsp"),
				importLine("i-paste", "Tomato Paste", 1, "tbsp")),
		},
	}); err != nil {
		t.Fatal(err)
	}
	pantryStore := pantry.NewMongoStore(db)
	pantrySvc := pantry.NewService(pantryStore, recipeSvc).WithUsage(pantry.UsageOptions{Store: pantryStore, Recipes: recipeSvc})
	store := NewMongoStore(db)
	if err := EnsureSeed(ctx, store, nil); err != nil {
		t.Fatal(err)
	}
	svc := NewService(ServiceOptions{Store: store, Catalog: recipeSvc, Recipes: recipeSvc, Pantry: pantrySvc})
	pantrySvc.SetKeyResolver(svc)
	plans := planning.NewService(planning.NewMongoStore(db), recipeSvc).WithPantry(pantrySvc).WithSpecialties(svc)
	actor := member(testHousehold)

	views, err := svc.List(ctx, testHousehold, false)
	if err != nil || len(views) != 2 || views[0].Specialty.ID != "southwest-spice-blend" || views[0].RecipeCount != 2 || len(views[0].IngredientIDs) != 2 ||
		views[1].Specialty.ID != "tex-mex-paste" || views[1].RecipeCount != 1 {
		t.Fatalf("List() = %+v, %v", views, err)
	}
	if _, err := svc.SetChoice(ctx, actor, "tex-mex-paste", "tex-mex-paste.store"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetChoice(ctx, actor, "southwest-spice-blend", "southwest-spice-blend.batch"); err != nil {
		t.Fatal(err)
	}

	page, err := recipeSvc.List(ctx, testHousehold, recipes.ListQuery{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("recipes = %+v, %v", page.Items, err)
	}
	entries := map[string]string{}
	for _, r := range page.Items {
		_, e, err := plans.AddEntry(ctx, testHousehold, testUser, "2026-W38", planning.NewEntry{RecipeID: r.ID, Servings: 2})
		if err != nil {
			t.Fatal(err)
		}
		entries[r.Name] = e.ID
	}
	recipeID := map[string]string{page.Items[0].Name: page.Items[0].ID, page.Items[1].Name: page.Items[1].ID}

	// No batch yet: its ingredients are on the list; the paste is replaced.
	g, err := plans.GroceryList(ctx, testHousehold, "2026-W38")
	if err != nil || !g.SpecialtiesApplied || len(g.Batches) != 1 {
		t.Fatalf("GroceryList() = %+v, %v", g, err)
	}
	items := groceryItems(g)
	if _, ok := items["Tex-Mex Paste"]; ok {
		t.Error("Tex-Mex Paste is still listed")
	}
	// 2 tbsp of paste is 2 tsp of tomato paste, plus the bowls' own 1 tbsp.
	if paste := items["Tomato Paste"]; len(paste.Amounts) != 1 || amountsOf(paste)[0] != "5/3 tbsp" || len(paste.Via) != 1 || len(paste.Sources) != 2 {
		t.Errorf("tomato paste = %+v", paste)
	}
	// The store route for the paste is a bouillon base now, so cumin comes only from the batch.
	if cumin := items["Ground Cumin"]; len(cumin.Via) != 1 {
		t.Errorf("cumin = %+v, want the batch provenance", cumin)
	}
	if b := g.Batches[0]; b.Status != grocery.BatchMake || b.Reason != grocery.BatchMissing || b.Batches != 1 || b.Needed == nil ||
		b.Needed.Quantity.String() != "2" || len(b.Recipes) != 2 {
		t.Errorf("batch plan = %+v", b)
	}

	// Make a batch: a house_made purchase starts a 4 tbsp cycle.
	res, err := svc.RecordBatch(ctx, actor, "southwest-spice-blend", BatchInput{ClientPurchaseID: "b1"})
	if err != nil || !res.Created {
		t.Fatalf("RecordBatch() = %+v, %v", res, err)
	}
	item := res.Item
	if tr := item.Tracking; tr == nil || tr.CycleSource != pantry.CycleHouseMade || tr.Reference != "4" || tr.Unit != "tbsp" ||
		item.Key != "southwest spice blend" || item.ExpiresOn == "" || item.IngredientID == "" {
		t.Fatalf("batch item = %+v tracking %+v", item, item.Tracking)
	}
	if again, err := svc.RecordBatch(ctx, actor, "southwest-spice-blend", BatchInput{ClientPurchaseID: "b1"}); err != nil || again.Created {
		t.Errorf("retried batch = %+v, %v", again, err)
	}

	g, err = plans.GroceryList(ctx, testHousehold, "2026-W38")
	if err != nil {
		t.Fatal(err)
	}
	items = groceryItems(g)
	blend := items["Southwest Spice Blend"]
	if b := g.Batches[0]; b.Status != grocery.BatchInPantry || b.Reason != grocery.BatchEnough || b.ItemID != item.ID {
		t.Errorf("in-pantry plan = %+v", b)
	}
	if blend.Status != grocery.StatusInPantry || blend.Specialty == nil || !blend.Specialty.HouseMade || len(blend.Sources) != 2 {
		t.Errorf("batch line = %+v", blend)
	}
	// The batch covers the blend and the paste's store route is a bouillon base, so nothing on
	// the list calls for cumin any more.
	if cumin, ok := items["Ground Cumin"]; ok {
		t.Errorf("cumin with a batch in the pantry = %+v, want it off the list", cumin)
	}

	// Cooking deducts: 1 packet (1 tbsp) for the tacos, and 1 tbsp named by the
	// alias for the bowls.
	for name, entry := range entries {
		u, applied, err := pantrySvc.ApplyCooked(ctx, pantry.CookedMeal{
			HouseholdID: testHousehold, UserID: testUser, RecipeID: recipeID[name], EntryID: entry, Servings: 2, OccurredAt: time.Now().Add(time.Second),
		})
		if err != nil || !applied {
			t.Fatalf("cook %s = %+v, %v, %v", name, u, applied, err)
		}
	}
	levels, err := pantrySvc.StockLevels(ctx, testHousehold, []string{"southwest spice blend"})
	if l := levels["southwest spice blend"]; err != nil || l.Remaining == nil || l.Remaining.RatString() != "2" || l.PercentRemaining != 50 {
		t.Errorf("after cooking = %+v, %v", l, err)
	}

	// Marked low, the week asks for a batch again.
	low := pantry.StatusLow
	if _, err := pantrySvc.Update(ctx, actor, item.ID, pantry.UpdateInput{Status: &low}); err != nil {
		t.Fatal(err)
	}
	g, err = plans.GroceryList(ctx, testHousehold, "2026-W38")
	if err != nil {
		t.Fatal(err)
	}
	if b := g.Batches[0]; b.Status != grocery.BatchMake || b.Reason != grocery.BatchLow || b.Batches != 1 || b.Remaining == nil {
		t.Errorf("low plan = %+v", b)
	}
	if _, ok := groceryItems(g)["Southwest Spice Blend"]; ok {
		t.Error("a low batch is still listed as in the pantry")
	}
}
