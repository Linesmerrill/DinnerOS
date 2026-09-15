package menu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot/baseline"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer grants permissions per (household, user), mirroring
// households.Service.Authorize.
type fakeAuthorizer map[string][]households.Permission

func (f fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	perms, ok := f[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	m := households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}
	if !slices.Contains(perms, perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

type fakeHouseholds map[string]households.Household

func (f fakeHouseholds) GetHousehold(_ context.Context, id string) (households.Household, error) {
	h, ok := f[id]
	if !ok {
		return households.Household{}, households.ErrNotFound
	}
	return h, nil
}

// integrationCatalog builds an import file with nutrition, order history, and
// add-ons, close to what a real import produces.
func integrationCatalog(n int) recipes.ImportFile {
	cuisines := []string{"Mexican", "Italian", "Thai", "North American"}
	mains := []string{"Chicken Breast", "Ground Beef", "Pork Chops", "Salmon Fillet", "Tofu"}
	qty := func(v float64) *float64 { return &v }
	file := recipes.ImportFile{Version: recipes.ImportVersion, Source: recipes.SourceManual, GeneratedAt: time.Now().UTC()}
	for i := range n {
		main := mains[i%len(mains)]
		r := recipes.ImportRecipe{
			Source: recipes.SourceManual, SourceRecipeID: fmt.Sprintf("menu-%d", i), SourceURL: fmt.Sprintf("https://recipes.example.com/%d", i),
			Name: fmt.Sprintf("Dinner %02d with %s", i, main), Servings: []int{2, 4},
			PrepMinutes: 10 + (i*5)%50, TotalMinutes: 5, IsAddon: i%10 == 9,
			Cuisines: []string{cuisines[i%len(cuisines)]}, Tags: []string{fmt.Sprintf("Tag %d", i%4)},
			Nutrition: []recipes.ImportNutrient{
				{Name: "Calories", Amount: float64(500 + i*7), Unit: "kcal"},
				{Name: "Protein", Amount: float64(20 + i%30), Unit: "g"},
			},
			Steps: []recipes.ImportStep{{Index: 1, Text: "Cook it."}},
		}
		for j, name := range []string{main, "Garlic"} {
			r.Ingredients = append(r.Ingredients, recipes.ImportIngredient{
				SourceIngredientID: fmt.Sprintf("ing-%d", j), Name: name,
				Amounts: []recipes.ImportAmount{
					{Servings: 2, Quantity: qty(1), Unit: "oz", SourceUnit: "ounce", RawText: "1 " + name},
					{Servings: 4, Quantity: qty(2), Unit: "oz", SourceUnit: "ounce", RawText: "2 " + name},
				},
			})
		}
		// Every third recipe was delivered, some of them in the past week the
		// test looks at.
		if i%3 == 0 {
			r.OrderWeeks = []string{"2026-W30"}
			if i%6 == 0 {
				r.OrderWeeks = []string{"2024-W12", "2026-W30", "2026-W36"}
			}
		}
		file.Recipes = append(file.Recipes, r)
	}
	return file
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatalf("decode %T: %v (body %s)", v, err, rec.Body.String())
	}
	return v
}

// TestIntegrationMenuEndpoints runs the menu endpoints against MongoDB with
// the real recipes, ratings, planning, events, and Autopilot modules.
func TestIntegrationMenuEndpoints(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, slices.Concat(
		recipes.Indexes(), ratings.Indexes(), events.Indexes(), planning.Indexes(), recommendations.Indexes(),
	)...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	hh, user, outsider := bson.NewObjectID().Hex(), bson.NewObjectID().Hex(), bson.NewObjectID().Hex()

	recipeService := recipes.NewService(recipes.NewMongoStore(db))
	eventService := events.NewService(events.ServiceOptions{Store: events.NewMongoStore(db), Recipes: recipeService})
	ratingService := ratings.NewService(ratings.ServiceOptions{Store: ratings.NewMongoStore(db), Recipes: recipeService, Events: eventService})
	planService := planning.NewService(planning.NewMongoStore(db), recipeService).WithEvents(eventService, nil)
	householdSource := fakeHouseholds{hh: {ID: hh, DefaultServings: 2, TimeZone: "America/Denver"}}
	autopilotService := recommendations.NewService(recommendations.ServiceOptions{
		Store: recommendations.NewMongoStore(db), Provider: baseline.New(baseline.Options{}), Households: householdSource,
		Recipes: recipeService, Ratings: ratingService, Events: eventService, Plans: planService,
	})

	const total = 40
	if res, err := recipeService.Import(ctx, hh, integrationCatalog(total)); err != nil || res.Created != total {
		t.Fatalf("Import() = %+v, %v", res, err)
	}
	page, err := recipeService.List(ctx, hh, recipes.ListQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	member := households.Membership{HouseholdID: hh, UserID: user, Role: households.RoleMember}
	for i, s := range page.Items[:6] {
		in := ratings.RateInput{Score: 5 - i%2}
		if i%3 == 0 {
			in.Tags = []string{"make-again"}
		}
		if _, err := ratingService.Rate(ctx, member, s.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	// The current week has a plan; a past week has one too, for history.
	if _, _, err := planService.AddEntries(ctx, hh, user, "2026-W38", []planning.NewEntry{
		{RecipeID: page.Items[0].ID, Day: "tue", Servings: 2}, {RecipeID: page.Items[1].ID, Servings: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := planService.AddEntries(ctx, hh, user, "2026-W30", []planning.NewEntry{
		{RecipeID: page.Items[2].ID, Day: "mon", Servings: 2},
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(Options{
		Recipes: recipeService, Ratings: ratingService, Plans: planService, Autopilot: autopilotService,
		Events: eventService, Households: householdSource,
		Now: func() time.Time { return time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC) },
	})
	router := chi.NewRouter()
	authorizer := fakeAuthorizer{hh + "/" + user: {households.PermHouseholdView}, hh + "/" + outsider: {}}
	router.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: authorizer, Tokens: fakeTokens{}}).Mount)
	call := func(userID, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/households/"+hh+path, nil)
		req.Header.Set("Authorization", "Bearer token-"+userID)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	wantOK := func(rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
		}
		return rec
	}

	t.Run("menu", func(t *testing.T) {
		start := time.Now()
		rec := wantOK(call(user, "/menu"))
		t.Logf("GET /menu over %d recipes took %v", total, time.Since(start))
		m := decodeBody[MenuResponse](t, rec)
		if m.Week != "2026-W38" || m.WeekStart != "2026-09-14" || m.WeekEnd != "2026-09-20" || m.Timing != TimingCurrent || m.CurrentWeek != "2026-W38" {
			t.Errorf("menu = %+v", m)
		}
		if m.Plan == nil || len(m.Plan.Entries) != 2 {
			t.Fatalf("plan = %+v", m.Plan)
		}
		if len(m.Sections) == 0 {
			t.Fatal("no sections")
		}
		known := []string{SectionFavorites, SectionQuick, SectionNewToYou, SectionLongCooks, SectionSides}
		inPlan := 0
		for _, sec := range m.Sections {
			if !slices.Contains(known, sec.ID) || sec.Title == "" || len(sec.Items) == 0 || len(sec.Items) > sectionLimit {
				t.Errorf("section %+v", sec)
			}
			for _, card := range sec.Items {
				if len(card.Badges) > maxBadges {
					t.Errorf("card %q has badges %+v", card.Recipe.Name, card.Badges)
				}
				if card.Recipe.Calories == nil || card.Recipe.ProteinGrams == nil || card.Recipe.TimeBand == nil {
					t.Errorf("card %q = calories %v, protein %v, band %v", card.Recipe.Name, card.Recipe.Calories, card.Recipe.ProteinGrams, card.Recipe.TimeBand)
				}
				if card.InPlan {
					inPlan++
				}
			}
		}
		if inPlan == 0 {
			t.Error("no card is marked as in the plan")
		}
	})

	t.Run("past week", func(t *testing.T) {
		m := decodeBody[MenuResponse](t, wantOK(call(user, "/menu?week=2026-W30")))
		if m.Timing != TimingPast {
			t.Errorf("timing = %s", m.Timing)
		}
		ids := []string{}
		for _, sec := range m.Sections {
			ids = append(ids, sec.ID)
		}
		for _, want := range []string{SectionHistoryPlanned, SectionHistoryOrdered} {
			if !slices.Contains(ids, want) {
				t.Errorf("past sections = %v, want %s", ids, want)
			}
		}
		if slices.Contains(ids, SectionQuick) {
			t.Errorf("past sections = %v, want no forward-looking sections", ids)
		}
	})

	t.Run("recipes paging", func(t *testing.T) {
		seen := map[string]bool{}
		cursor := ""
		for page := range 3 {
			path := "/menu/recipes?limit=5"
			if cursor != "" {
				path += "&cursor=" + cursor
			}
			resp := decodeBody[MenuRecipesResponse](t, wantOK(call(user, path)))
			if len(resp.Items) != 5 {
				t.Fatalf("page %d has %d items", page, len(resp.Items))
			}
			for _, item := range resp.Items {
				if seen[item.Recipe.ID] {
					t.Errorf("recipe %q appears on two pages", item.Recipe.Name)
				}
				seen[item.Recipe.ID] = true
				if item.Recipe.IsAddon {
					t.Errorf("add-on %q in the main list", item.Recipe.Name)
				}
			}
			if resp.NextCursor == nil {
				t.Fatalf("page %d has no cursor", page)
			}
			cursor = *resp.NextCursor
		}
		quick := decodeBody[MenuRecipesResponse](t, wantOK(call(user, "/menu/recipes?sort=quick&limit=50")))
		last := 0
		for _, item := range quick.Items {
			if item.Recipe.CookMinutes < last {
				t.Errorf("sort=quick returned %d after %d", item.Recipe.CookMinutes, last)
			}
			last = item.Recipe.CookMinutes
		}
		addons := decodeBody[MenuRecipesResponse](t, wantOK(call(user, "/menu/recipes?addons=true&limit=50")))
		if len(addons.Items) != total/10 {
			t.Errorf("addons=true returned %d items, want %d", len(addons.Items), total/10)
		}
		if rec := call(user, "/menu/recipes?sort=nonsense"); rec.Code != http.StatusBadRequest {
			t.Errorf("an unknown sort = %d, want 400", rec.Code)
		}
	})

	t.Run("filters", func(t *testing.T) {
		f := decodeBody[FiltersResponse](t, wantOK(call(user, "/menu/filters")))
		if len(f.Proteins) == 0 || len(f.Cuisines) == 0 || len(f.Tags) == 0 || len(f.Sorts) != 5 || len(f.MaxMinutes) != 4 {
			t.Fatalf("filters = %+v", f)
		}
		for _, o := range slices.Concat(f.Proteins, f.Cuisines, f.Tags) {
			if o.Value == "" || o.Label == "" || o.Count <= 0 {
				t.Errorf("option = %+v", o)
			}
		}
		// A chip's count is what the list returns for it.
		got := decodeBody[MenuRecipesResponse](t, wantOK(call(user, "/menu/recipes?limit=50&protein="+f.Proteins[0].Value)))
		if len(got.Items) != f.Proteins[0].Count {
			t.Errorf("protein %q count = %d, list returns %d", f.Proteins[0].Value, f.Proteins[0].Count, len(got.Items))
		}
	})

	t.Run("weeks", func(t *testing.T) {
		resp := decodeBody[WeeksResponse](t, wantOK(call(user, "/weeks?around=2026-W38&before=8&after=4")))
		if len(resp.Items) != 13 {
			t.Fatalf("weeks = %d items, want 13", len(resp.Items))
		}
		if first, last := resp.Items[0], resp.Items[12]; first.Week != "2026-W30" || last.Week != "2026-W42" || first.Timing != TimingPast || last.Timing != TimingUpcoming {
			t.Errorf("strip runs %+v .. %+v", first, last)
		}
		byWeek := map[string]WeekSummaryResponse{}
		for _, w := range resp.Items {
			byWeek[w.Week] = w
		}
		if w := byWeek["2026-W30"]; w.PlannedCount != 1 || w.Status != string(planning.StatusDraft) || w.OrderedCount == 0 {
			t.Errorf("2026-W30 = %+v", w)
		}
		if w := byWeek["2026-W38"]; w.PlannedCount != 2 || w.Timing != TimingCurrent {
			t.Errorf("2026-W38 = %+v", w)
		}
		if w := byWeek["2026-W31"]; w.Status != WeekStatusNone || w.PlannedCount != 0 {
			t.Errorf("2026-W31 = %+v", w)
		}
		// Order history reaches further back than any plan.
		if resp.EarliestWeek == nil || *resp.EarliestWeek != "2024-W12" {
			t.Errorf("earliestWeek = %v, want 2024-W12", resp.EarliestWeek)
		}
		if rec := call(user, "/weeks?before=nope"); rec.Code != http.StatusBadRequest {
			t.Errorf("before=nope = %d, want 400", rec.Code)
		}
	})

	t.Run("permissions", func(t *testing.T) {
		if rec := call(outsider, "/menu"); rec.Code != http.StatusForbidden {
			t.Errorf("a member without household.view = %d, want 403", rec.Code)
		}
		if rec := call(bson.NewObjectID().Hex(), "/menu"); rec.Code != http.StatusNotFound {
			t.Errorf("a non-member = %d, want 404", rec.Code)
		}
	})
}
