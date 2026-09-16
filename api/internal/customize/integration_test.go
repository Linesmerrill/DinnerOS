package customize

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func importAmount(servings int, quantity float64, unit string) recipes.ImportAmount {
	return recipes.ImportAmount{Servings: servings, Quantity: &quantity, Unit: unit, SourceUnit: unit, RawText: "amount"}
}

func importRecipe(sourceID, name string, lines ...recipes.ImportIngredient) recipes.ImportRecipe {
	return recipes.ImportRecipe{
		Source: recipes.SourceManual, SourceRecipeID: sourceID, SourceURL: "https://recipes.example.com/" + sourceID,
		Name: name, Servings: []int{2, 4}, TotalMinutes: 30, Ingredients: lines,
		Steps: []recipes.ImportStep{{Index: 1, Text: "Cook."}},
	}
}

func weightLine(sourceID, name string, small, large float64) recipes.ImportIngredient {
	return recipes.ImportIngredient{SourceIngredientID: sourceID, Name: name,
		Amounts: []recipes.ImportAmount{importAmount(2, small, "oz"), importAmount(4, large, "oz")}}
}

// TestIntegrationCustomizeEndpoints runs both endpoints, the grocery list, the
// recorded event, and the pantry cook deduction against MongoDB with the real
// recipes, planning, pantry, and events modules.
func TestIntegrationCustomizeEndpoints(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, slices.Concat(recipes.Indexes(), planning.Indexes(), pantry.Indexes(), events.Indexes())...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	recipeSvc := recipes.NewService(recipes.NewMongoStore(db))
	if _, err := recipeSvc.Import(ctx, hhAda, recipes.ImportFile{
		Version: recipes.ImportVersion, Source: recipes.SourceManual, GeneratedAt: testNow,
		Recipes: []recipes.ImportRecipe{
			importRecipe("tacos", "One-Pan Pork Tacos",
				weightLine("i-pork", "Ground Pork", 10, 20),
				recipes.ImportIngredient{SourceIngredientID: "i-onion", Name: "Yellow Onion",
					Amounts: []recipes.ImportAmount{importAmount(2, 1, "count"), importAmount(4, 2, "count")}}),
			// A second recipe already uses ground beef, so the swapped line
			// aggregates with it.
			importRecipe("chili", "Beef Chili", weightLine("i-beef", "Ground Beef", 10, 20)),
		},
	}); err != nil {
		t.Fatal(err)
	}
	page, err := recipeSvc.List(ctx, hhAda, recipes.ListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var tacosID, chiliID string
	for _, s := range page.Items {
		switch s.Name {
		case "One-Pan Pork Tacos":
			tacosID = s.ID
		case "Beef Chili":
			chiliID = s.ID
		}
	}
	if tacosID == "" || chiliID == "" {
		t.Fatalf("imported recipes = %+v", page.Items)
	}

	pantryStore := pantry.NewMongoStore(db)
	pantrySvc := pantry.NewService(pantryStore, recipeSvc).WithUsage(pantry.UsageOptions{Store: pantryStore, Recipes: recipeSvc})
	eventSvc := events.NewService(events.ServiceOptions{Store: events.NewMongoStore(db), Recipes: recipeSvc})
	planSvc := planning.NewService(planning.NewMongoStore(db), recipeSvc).WithPantry(pantrySvc)
	customizeSvc := NewService(ServiceOptions{Plans: planSvc, Recipes: recipeSvc, Catalog: recipeSvc, Events: eventSvc})
	planSvc.WithCustomizations(customizeSvc)
	pantrySvc.SetCookAdjuster(customizeSvc)

	// The plan is the current week's, and step 5 cooks the meal after the beef
	// was bought. A meal cooked before a pantry item's cycle began deducts
	// nothing by design (pantry.SkipBeforeCycle), and RecordPurchase stamps the
	// cycle from the wall clock, so a fixed week and a fixed cooked time make
	// this test pass or fail depending on when it runs.
	planWeek := planning.WeekOf(time.Now().UTC()).String()

	_, entry, err := planSvc.AddEntry(ctx, hhAda, userAda, planWeek, planning.NewEntry{RecipeID: tacosID, Servings: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := planSvc.AddEntry(ctx, hhAda, userAda, planWeek, planning.NewEntry{RecipeID: chiliID, Servings: 2}); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	router.Use(httpx.LimitBody(1 << 20))
	router.Route("/api/v1", func(r chi.Router) {
		NewHandler(HandlerOptions{Service: customizeSvc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount(r)
		planning.NewHandler(planning.HandlerOptions{Service: planSvc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount(r)
	})

	// 1. The app lists the meal's options for the entry's servings.
	rec := do(t, router, http.MethodGet,
		"/api/v1/households/"+hhAda+"/recipes/"+tacosID+"/customizations?entryId="+entry.ID+"&week="+planWeek, "", userAda)
	wantStatus(t, rec, http.StatusOK)
	options := decodeBody[CustomizationsResponse](t, rec)
	if len(options.Groups) != 1 || options.Servings != 2 {
		t.Fatalf("options = %+v", options)
	}
	group := options.Groups[0]
	if group.IngredientName != "Ground Pork" || group.AmountText != "10 ounce" || group.SelectedChoiceID == nil || *group.SelectedChoiceID != ChoiceOriginal {
		t.Fatalf("group = %+v", group)
	}
	i := slices.IndexFunc(group.Choices, func(c ChoiceResponse) bool { return c.ID == "swap:ground-beef" })
	if i < 0 || group.Choices[i].AmountText != "10 ounce" || group.Choices[i].IngredientName != "Ground Beef" {
		t.Fatalf("choices = %+v", group.Choices)
	}

	// 2. The member swaps the pork for beef.
	rec = do(t, router, http.MethodPut,
		"/api/v1/households/"+hhAda+"/plans/"+planWeek+"/entries/"+entry.ID+"/customization",
		`{"selections":[{"ingredientKey":"`+group.IngredientKey+`","choiceId":"swap:ground-beef"}]}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	plan := decodeBody[planning.PlanResponse](t, rec)
	saved := plan.Entries[slices.IndexFunc(plan.Entries, func(e planning.EntryResponse) bool { return e.ID == entry.ID })]
	if len(saved.Customizations) != 1 || saved.Customizations[0].ChoiceID != "swap:ground-beef" || saved.Customizations[0].Label != "Ground Beef" {
		t.Fatalf("saved entry = %+v", saved)
	}

	// 3. The week's grocery list buys beef instead of pork, aggregated with
	// the chili's beef and with the provenance the app shows.
	rec = do(t, router, http.MethodGet, "/api/v1/households/"+hhAda+"/plans/"+planWeek+"/grocery", "", userAda)
	wantStatus(t, rec, http.StatusOK)
	grocery := decodeBody[planning.GroceryListResponse](t, rec)
	var beef *planning.GroceryItemResponse
	for _, c := range grocery.Categories {
		for j, item := range c.Items {
			if item.Name == "Ground Beef" {
				beef = &c.Items[j]
			}
			if item.Name == "Ground Pork" {
				t.Error("the swapped-out pork is still on the grocery list")
			}
		}
	}
	if beef == nil || len(beef.Amounts) != 1 || beef.Amounts[0].Quantity != "20" || beef.Amounts[0].Unit != "oz" {
		t.Fatalf("beef item = %+v", beef)
	}
	if len(beef.Recipes) != 2 || len(beef.Via) != 1 {
		t.Fatalf("beef provenance = %+v, via %+v", beef.Recipes, beef.Via)
	}
	if want := "Ground Beef instead of Ground Pork in One-Pan Pork Tacos"; beef.Via[0].Text != want {
		t.Errorf("via text = %q, want %q", beef.Via[0].Text, want)
	}

	// 4. The customization is recorded for Autopilot.
	stored, err := eventSvc.List(ctx, events.Query{HouseholdID: hhAda, Types: []events.Type{events.TypeMealCustomized}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].RecipeID != tacosID || stored[0].Week != planWeek {
		t.Fatalf("events = %+v", stored)
	}
	payload, _ := stored[0].Payload.(events.MealCustomized)
	if payload.EntryID != entry.ID || len(payload.Changes) != 1 || payload.Changes[0].From != "original" || payload.Changes[0].To != "swap:ground-beef" {
		t.Errorf("payload = %+v", payload)
	}

	// 5. Cooking the meal deducts the beef that was cooked, not the pork.
	actor := households.Membership{HouseholdID: hhAda, UserID: userAda, Role: households.RoleMember}
	purchase, err := pantrySvc.RecordPurchase(ctx, actor, pantry.PurchaseInput{
		Name: "Ground Beef", Source: pantry.PurchaseManual, Quantity: "32", Unit: "oz",
	})
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Item.Tracking == nil {
		t.Fatalf("the purchased beef is not tracked: %+v", purchase.Item)
	}
	if _, applied, err := pantrySvc.ApplyCooked(ctx, pantry.CookedMeal{
		HouseholdID: hhAda, UserID: userAda, RecipeID: tacosID, EntryID: entry.ID, Servings: 2,
		OccurredAt: purchase.Item.Tracking.SegmentStartedAt.Add(time.Hour),
	}); err != nil || !applied {
		t.Fatalf("ApplyCooked() = %v, %v", applied, err)
	}
	item, err := pantryStore.GetItem(ctx, hhAda, purchase.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Tracking == nil || item.Tracking.RecipeUsed != "10" {
		t.Errorf("beef deduction = %+v", item.Tracking)
	}

	// 6. A finalized plan can't be customized.
	if _, err := planSvc.SetStatus(ctx, hhAda, planWeek, string(planning.StatusFinalized)); err != nil {
		t.Fatal(err)
	}
	wantError(t, do(t, router, http.MethodPut,
		"/api/v1/households/"+hhAda+"/plans/"+planWeek+"/entries/"+entry.ID+"/customization",
		`{"selections":[]}`, userAda), http.StatusConflict, "plan_finalized")
}
