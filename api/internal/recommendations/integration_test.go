package recommendations

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot/baseline"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// syntheticCatalog builds n made-up recipes with varied cuisines, proteins,
// cook times (including inconsistent prep and total shapes), and order
// history.
func syntheticCatalog(n int) recipes.ImportFile {
	cuisines := []string{"Mexican", "Italian", "Thai", "Indian", "American", "Greek", "Japanese", "Korean"}
	mains := []string{"Pork Tenderloin", "Bone-In Chicken Thighs", "Ground Beef", "Chickpeas", "Salmon Fillet", "Tofu", "Chicken Breast", "Pork Chops", "Shrimp", "Eggs"}
	qty := func(v float64) *float64 { return &v }
	file := recipes.ImportFile{Version: recipes.ImportVersion, Source: recipes.SourceManual, GeneratedAt: testNow}
	for i := range n {
		prep, total := 10+i%15, 15+(i*7)%70
		if i%9 == 0 {
			prep, total = 25, 5 // total below prep
		}
		main := mains[i%len(mains)]
		r := recipes.ImportRecipe{
			Source: recipes.SourceManual, SourceRecipeID: fmt.Sprintf("synthetic-%d", i), SourceURL: fmt.Sprintf("https://recipes.example.com/synthetic-%d", i),
			Name: fmt.Sprintf("Dinner %d with %s", i, main), Servings: []int{2, 4}, PrepMinutes: prep, TotalMinutes: total,
			Cuisines: []string{cuisines[i%len(cuisines)]}, Tags: []string{fmt.Sprintf("Tag %d", i%12)},
			Steps: []recipes.ImportStep{{Index: 1, Text: "Cook it."}},
		}
		for j, name := range []string{main, "Garlic", "Onion"} {
			r.Ingredients = append(r.Ingredients, recipes.ImportIngredient{
				SourceIngredientID: fmt.Sprintf("ing-%s-%d", strings.ReplaceAll(strings.ToLower(name), " ", "-"), j), Name: name,
				Amounts: []recipes.ImportAmount{
					{Servings: 2, Quantity: qty(1), Unit: "clove", SourceUnit: "clove", RawText: "1 " + name},
					{Servings: 4, Quantity: qty(2), Unit: "clove", SourceUnit: "clove", RawText: "2 " + name},
				},
			})
		}
		for k := range i % 4 {
			r.OrderWeeks = append(r.OrderWeeks, fmt.Sprintf("2026-W%02d", 10+(i+k*5)%25))
		}
		file.Recipes = append(file.Recipes, r)
	}
	return file
}

// TestIntegrationAutopilotEndpoints runs the Autopilot endpoints against
// MongoDB with the real recipes, ratings, events, and planning modules.
func TestIntegrationAutopilotEndpoints(t *testing.T) {
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

	imported, err := recipeService.Import(ctx, hh, syntheticCatalog(500))
	if err != nil || imported.Created != 500 {
		t.Fatalf("Import() = %+v, %v", imported, err)
	}
	page, err := recipeService.List(ctx, hh, recipes.ListQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	member := households.Membership{HouseholdID: hh, UserID: userAda, Role: households.RoleMember}
	for i, s := range page.Items {
		in := ratings.RateInput{Score: 1 + i%5}
		if i%5 == 4 {
			in.Tags = []string{"make-again"}
		}
		if _, err := ratingService.Rate(ctx, member, s.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	// Last week's plan gives weekday affinity and recency.
	if _, _, err := planService.AddEntries(ctx, hh, userAda, "2026-W37", []planning.NewEntry{
		{RecipeID: page.Items[0].ID, Day: "tue", Servings: 2}, {RecipeID: page.Items[1].ID, Day: "wed", Servings: 2},
	}); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	router.Use(httpx.LimitBody(1 << 20))
	authorizer := fakeAuthorizer{hh + "/" + userAda: {households.PermHouseholdView, households.PermPlanEdit}}
	router.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: authorizer, Tokens: fakeTokens{}}).Mount)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/households/"+hh+"/autopilot"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userAda)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	wantStatus(t, call(http.MethodPut, "/profile", onboardingJSON), http.StatusOK, "")
	wantStatus(t, call(http.MethodPut, "/weeks/2026-W38/context", `{"busy": false, "maxMinutes": 60, "days": [{"day": "sat", "skip": true}], "note": "away Saturday"}`), http.StatusOK, "")

	start := time.Now()
	rec := call(http.MethodPost, "/weeks/2026-W38/generate", "")
	elapsed := time.Since(start)
	wantStatus(t, rec, http.StatusCreated, "")
	t.Logf("generated a week from 500 recipes in %v", elapsed)
	if elapsed > 3*time.Second {
		t.Errorf("generation took %v", elapsed)
	}
	proposal := decodeBody[ProposalResponse](t, rec)
	if proposal.PlannedMeals != 5 || proposal.CandidateCount == 0 || proposal.ColdStart {
		t.Fatalf("proposal = %+v", proposal)
	}
	for _, s := range proposal.Slots {
		if s.Day == "sat" || s.CookMinutes == nil || *s.CookMinutes > 60 {
			t.Errorf("slot ignores the week context: %+v", s)
		}
	}
	if slices.IndexFunc(proposal.Slots, func(s SlotJSON) bool { return s.Day == "sun" }) < 0 {
		t.Errorf("the Sunday rule day should be planned: %+v", proposal.Slots)
	}

	rec = call(http.MethodPost, "/weeks/2026-W38/proposal/slots/"+proposal.Slots[0].Day+"/swap", `{"version": `+strconv.FormatInt(proposal.Version, 10)+`}`)
	wantStatus(t, rec, http.StatusOK, "")
	swapped := decodeBody[ProposalResponse](t, rec)
	rec = call(http.MethodPost, "/weeks/2026-W38/proposal/accept", `{"version": `+strconv.FormatInt(swapped.Version, 10)+`, "excludeSlotIds": ["`+swapped.Slots[1].Day+`"]}`)
	wantStatus(t, rec, http.StatusOK, "")
	accepted := decodeBody[AcceptResponse](t, rec)
	if len(accepted.Added) != 4 {
		t.Fatalf("accepted = %+v", accepted)
	}

	plan, err := planService.Get(ctx, hh, "2026-W38")
	if err != nil || len(plan.Entries) != 4 {
		t.Fatalf("stored plan = %+v, %v", plan, err)
	}
	for _, e := range plan.Entries {
		if e.Origin != planning.OriginAutopilot || e.ProposalID != proposal.ID {
			t.Errorf("stored entry = %+v", e)
		}
	}
	stored, err := eventService.List(ctx, events.Query{HouseholdID: hh, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[events.Type]int{}
	for _, e := range stored {
		counts[e.Type]++
		if p, ok := e.Payload.(events.RecipePlanned); ok && e.Week == "2026-W38" && (p.Origin != "autopilot" || p.ProposalID != proposal.ID) {
			t.Errorf("recipe.planned for an accepted meal = %+v", p)
		}
	}
	for typ, want := range map[events.Type]int{
		events.TypeAutopilotPreferencesUpdated: 1, events.TypeAutopilotWeekContextUpdated: 1, events.TypeWeekGenerated: 1,
		events.TypeMealSwapped: 1, events.TypeWeekAccepted: 1, events.TypeMealRejected: 1, events.TypeRecipePlanned: 6,
	} {
		if counts[typ] != want {
			t.Errorf("%s events = %d, want %d (all: %v)", typ, counts[typ], want, counts)
		}
	}
	rec = call(http.MethodGet, "/profile/history", "")
	wantStatus(t, rec, http.StatusOK, "")
	if h := decodeBody[HistoryResponse](t, rec); len(h.Items) != 2 || h.Items[0].Type != string(events.TypeAutopilotWeekContextUpdated) {
		t.Errorf("history = %+v", h)
	}
}
