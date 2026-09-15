package recommendations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot/baseline"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// pairingImport builds a small catalog: a pasta dish and a soup the household
// orders, plus a garlic bread add-on it has with most pasta weeks.
func pairingImport() recipes.ImportFile {
	qty := func(v float64) *float64 { return &v }
	recipe := func(id, name string, addon bool, orderWeeks []string, ingredients ...string) recipes.ImportRecipe {
		r := recipes.ImportRecipe{
			Source: recipes.SourceManual, SourceRecipeID: id, Name: name, IsAddon: addon, Servings: []int{2, 4},
			PrepMinutes: 20, TotalMinutes: 30, OrderWeeks: orderWeeks, Steps: []recipes.ImportStep{{Index: 1, Text: "Cook it."}},
		}
		for i, n := range ingredients {
			r.Ingredients = append(r.Ingredients, recipes.ImportIngredient{
				SourceIngredientID: "ing-" + strconv.Itoa(i) + "-" + id, Name: n,
				Amounts: []recipes.ImportAmount{
					{Servings: 2, Quantity: qty(1), Unit: "count", SourceUnit: "count", RawText: "1 " + n},
					{Servings: 4, Quantity: qty(2), Unit: "count", SourceUnit: "count", RawText: "2 " + n},
				},
			})
		}
		return r
	}
	var pastaWeeks, breadWeeks, soupWeeks []string
	for i := 1; i <= 6; i++ {
		pastaWeeks = append(pastaWeeks, week(i))
		if i <= 5 {
			breadWeeks = append(breadWeeks, week(i))
		}
	}
	for i := 7; i <= 12; i++ {
		soupWeeks = append(soupWeeks, week(i))
	}
	return recipes.ImportFile{
		Version: recipes.ImportVersion, Source: recipes.SourceManual, GeneratedAt: testNow,
		Recipes: []recipes.ImportRecipe{
			recipe("pasta-1", "Creamy Garlic Spaghetti", false, pastaWeeks, "Spaghetti", "Garlic"),
			recipe("soup-1", "Chicken & Potato Mushroom Soup", false, soupWeeks, "Chicken Breast", "Potato"),
			recipe("bread-1", "Garlic Bread", true, breadWeeks, "Baguette"),
		},
	}
}

// TestIntegrationPairings runs pairings against MongoDB with the real
// recipes, events, planning, and grocery modules: rules and the week's state
// round-trip, accepting adds a plan entry and a grocery line, and a proposal
// keeps its pairings.
func TestIntegrationPairings(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, slices.Concat(recipes.Indexes(), ratings.Indexes(), events.Indexes(), planning.Indexes(), Indexes())...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	hh := bson.NewObjectID().Hex()

	recipeService := recipes.NewService(recipes.NewMongoStore(db))
	eventService := events.NewService(events.ServiceOptions{Store: events.NewMongoStore(db), Recipes: recipeService})
	ratingService := ratings.NewService(ratings.ServiceOptions{Store: ratings.NewMongoStore(db), Recipes: recipeService, Events: eventService})
	planService := planning.NewService(planning.NewMongoStore(db), recipeService).WithEvents(eventService, nil)
	svc := NewService(ServiceOptions{
		Store: NewMongoStore(db), Provider: baseline.New(baseline.Options{}),
		Households: fakeHouseholds{hh: {ID: hh, DefaultServings: 2, TimeZone: "America/Denver"}},
		Recipes:    recipeService, Ratings: ratingService, Events: eventService, Plans: planService,
	})
	// The grocery list picks up accepted grocery pairings, as cmd/server wires it.
	planService.WithExtras(svc)

	if res, err := recipeService.Import(ctx, hh, pairingImport()); err != nil || res.Created != 3 {
		t.Fatalf("Import() = %+v, %v", res, err)
	}
	page, err := recipeService.List(ctx, hh, recipes.ListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var pastaID, soupID, breadID string
	for _, r := range page.Items {
		switch r.Name {
		case "Creamy Garlic Spaghetti":
			pastaID = r.ID
		case "Chicken & Potato Mushroom Soup":
			soupID = r.ID
		case "Garlic Bread":
			breadID = r.ID
		}
	}
	if pastaID == "" || soupID == "" || breadID == "" {
		t.Fatalf("imported recipes = %+v", page.Items)
	}

	// A grocery rule and a meal-category override round-trip through Mongo.
	rules := []PairingRule{{
		Label: "Soup night", When: PairingWhen{MealCategories: []string{"soup"}},
		Add: PairingTarget{GroceryItem: &GroceryItem{Name: "Club crackers", Quantity: "1", Unit: "package"}},
	}}
	profile, err := svc.UpdateProfile(ctx, hh, userAda, ProfileUpdate{Pairings: &rules}, false)
	if err != nil {
		t.Fatal(err)
	}
	ruleID := profile.Pairings[0].ID
	reread, err := svc.Profile(ctx, hh)
	if err != nil {
		t.Fatal(err)
	}
	if len(reread.Pairings) != 1 || reread.Pairings[0].ID != ruleID || reread.Pairings[0].Add.GroceryItem.Unit != "package" ||
		reread.Pairings[0].Label != "Soup night" {
		t.Fatalf("stored rules = %+v", reread.Pairings)
	}

	// The week's plan gets both meals; pairings are offered for each.
	_, added, err := planService.AddEntries(ctx, hh, userAda, testWeek, []planning.NewEntry{
		{RecipeID: pastaID, Day: "mon", Servings: 2}, {RecipeID: soupID, Day: "tue", Servings: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	pastaEntry, soupEntry := added[0], added[1]
	view, err := svc.WeekPairings(ctx, hh, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Meals) != 2 {
		t.Fatalf("meals = %+v", view.Meals)
	}
	pastaPairings := mealFor(t, view, pastaEntry.ID).Pairings
	if len(pastaPairings) != 1 || pastaPairings[0].Recipe == nil || pastaPairings[0].Recipe.ID != breadID ||
		pastaPairings[0].Source != PairingSourceLearned {
		t.Fatalf("learned pasta pairings = %+v", pastaPairings)
	}

	// Accepting the add-on plans it on the meal's day.
	accepted, err := svc.AcceptPairing(ctx, hh, userAda, testWeek, pastaEntry.ID, "recipe:"+breadID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != PairingAdded || accepted.Entry == nil || string(accepted.Entry.Day) != "mon" ||
		accepted.Entry.Origin != planning.OriginAutopilot {
		t.Fatalf("accepted = %+v", accepted)
	}

	// Accepting the grocery item puts it on the week's grocery list.
	item, err := svc.AcceptPairing(ctx, hh, userAda, testWeek, soupEntry.ID, "grocery:club crackers")
	if err != nil || item.GroceryItem == nil {
		t.Fatalf("accepted grocery pairing = %+v, %v", item, err)
	}
	list, err := planService.GroceryList(ctx, hh, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	var crackers grocery.Item
	for _, c := range list.Categories {
		for _, it := range c.Items {
			if it.IngredientKey == "name:club crackers" {
				crackers = it
			}
		}
	}
	if crackers.Name != "Club crackers" || len(crackers.Extras) != 1 ||
		crackers.Extras[0].Text != "Club crackers for Chicken & Potato Mushroom Soup" {
		t.Fatalf("grocery list item = %+v", crackers)
	}

	// The stored week state survives a reload, and the item leaves the list
	// with its meal.
	stored, err := NewMongoStore(db).GetWeekPairings(ctx, hh, testWeek)
	if err != nil || len(stored.GroceryItems) != 1 || len(stored.Decisions) != 2 {
		t.Fatalf("stored week pairings = %+v, %v", stored, err)
	}
	if _, err := planService.DeleteEntry(ctx, hh, userAda, testWeek, soupEntry.ID); err != nil {
		t.Fatal(err)
	}
	if list, err = planService.GroceryList(ctx, hh, testWeek); err != nil {
		t.Fatal(err)
	}
	for _, c := range list.Categories {
		for _, it := range c.Items {
			if it.IngredientKey == "name:club crackers" {
				t.Errorf("the item should leave with its meal: %+v", it)
			}
		}
	}

	// A proposal keeps its pairings through Mongo, and the HTTP layer shows
	// them.
	router := chi.NewRouter()
	router.Use(httpx.LimitBody(1 << 20))
	authorizer := fakeAuthorizer{hh + "/" + userAda: {households.PermHouseholdView, households.PermPlanEdit}}
	router.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: authorizer, Tokens: fakeTokens{}}).Mount)
	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/households/"+hh+path, strings.NewReader(""))
		req.Header.Set("Authorization", "Bearer token-"+userAda)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	rec := call(http.MethodGet, "/recipes/"+pastaID+"/pairings?week="+testWeek)
	wantStatus(t, rec, http.StatusOK, "")
	carousel := decodeBody[RecipePairingsResponse](t, rec)
	if len(carousel.Items) != 1 || carousel.Items[0].Target.Recipe == nil || !carousel.Items[0].InPlan {
		t.Fatalf("carousel = %+v", carousel)
	}

	if _, err := svc.Generate(ctx, hh, userAda, "2026-W39", GenerateOptions{}); err != nil {
		t.Fatal(err)
	}
	proposal, err := svc.Proposal(ctx, hh, "2026-W39")
	if err != nil {
		t.Fatal(err)
	}
	var learnedPairing, rulePairing Pairing
	for _, sl := range proposal.Slots {
		for _, p := range sl.Pairings {
			switch p.Key {
			case "recipe:" + breadID:
				learnedPairing = p
			case "grocery:club crackers":
				rulePairing = p
			}
		}
	}
	if learnedPairing.Recipe == nil || learnedPairing.Recipe.Name != "Garlic Bread" || learnedPairing.Learned == nil ||
		learnedPairing.Source != PairingSourceLearned {
		t.Errorf("stored learned pairing = %+v", learnedPairing)
	}
	if rulePairing.GroceryItem == nil || rulePairing.GroceryItem.Unit != "package" || rulePairing.RuleID != ruleID {
		t.Errorf("stored rule pairing = %+v", rulePairing)
	}
}
