package recommendations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// newPairingRouter mounts the handlers over an environment with pairings on.
func newPairingRouter(t *testing.T) (http.Handler, *pairingEnv) {
	t.Helper()
	env := newPairingEnv(t)
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: env.svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount)
	return r, env
}

// doPath calls a path under the household, without the autopilot prefix.
func doPath(t *testing.T, router http.Handler, method, path, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/households/"+hhA+path, strings.NewReader(body))
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestPairingHandlerPermissions(t *testing.T) {
	router, _ := newPairingRouter(t)
	for _, tt := range []struct {
		method, path, body, user string
		status                   int
		code                     string
	}{
		{http.MethodGet, "/autopilot/weeks/2026-W38/pairings", "", userView, http.StatusOK, ""},
		{http.MethodGet, "/autopilot/weeks/2026-W38/pairings", "", "", http.StatusUnauthorized, "unauthenticated"},
		{http.MethodGet, "/recipes/" + rRigatoni + "/pairings", "", userView, http.StatusOK, ""},
		{http.MethodPost, "/autopilot/weeks/2026-W38/pairings/accept", `{"entryId":"e","key":"k"}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodPost, "/autopilot/weeks/2026-W38/pairings/dismiss", `{"entryId":"e","key":"k"}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodPost, "/autopilot/weeks/2026-W38/pairings/rules", `{"entryId":"e","key":"k"}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodDelete, "/autopilot/weeks/2026-W38/pairings/grocery-items/x", "", userView, http.StatusForbidden, "forbidden"},
		{http.MethodGet, "/autopilot/weeks/2026-W99/pairings", "", userView, http.StatusBadRequest, "validation_failed"},
		{http.MethodPost, "/autopilot/weeks/2026-W38/pairings/accept", `{"key":"k"}`, userAda, http.StatusBadRequest, "validation_failed"},
		{http.MethodPost, "/autopilot/weeks/2026-W38/pairings/rules", `{"entryId":"e","slotId":"mon","key":"k"}`, userAda, http.StatusBadRequest, "validation_failed"},
	} {
		t.Run(tt.method+" "+tt.path+" as "+tt.user, func(t *testing.T) {
			wantStatus(t, doPath(t, router, tt.method, tt.path, tt.body, tt.user), tt.status, tt.code)
		})
	}
}

func TestPairingHandlerFlow(t *testing.T) {
	router, env := newPairingRouter(t)
	ctx := context.Background()
	env.saveRules(t, pastaBreadRule(), crackersRule())
	pasta := env.planMeal(t, rRigatoni, "mon", 2)
	soup := env.planMeal(t, rSoup, "tue", 2)

	rec := doPath(t, router, http.MethodGet, "/autopilot/weeks/"+testWeek+"/pairings", "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	week := decodeBody[WeekPairingsResponse](t, rec)
	if len(week.Meals) != 2 || week.StartDate == "" {
		t.Fatalf("week = %+v", week)
	}
	meal := week.Meals[0]
	if meal.EntryID != pasta.ID || len(meal.Pairings) != 1 || meal.Pairings[0].Target.Kind != PairingKindRecipe {
		t.Fatalf("meal = %+v", meal)
	}
	target := meal.Pairings[0].Target.Recipe
	if target == nil || target.Name != "Garlic Bread" || !target.IsAddon || target.CookMinutes == nil {
		t.Errorf("target = %+v", target)
	}
	if meal.Pairings[0].RuleID == nil || meal.Pairings[0].Reason == "" || meal.Pairings[0].Servings == nil {
		t.Errorf("pairing = %+v", meal.Pairings[0])
	}
	if g := week.Meals[1].Pairings[0].Target.GroceryItem; g == nil || g.Name != "Club crackers" || g.Quantity == nil || *g.Unit != "package" {
		t.Errorf("grocery target = %+v", g)
	}

	// Accept both kinds.
	rec = doPath(t, router, http.MethodPost, "/autopilot/weeks/"+testWeek+"/pairings/accept",
		`{"entryId":"`+pasta.ID+`","key":"recipe:`+rBread+`"}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	accepted := decodeBody[PairingAcceptResponse](t, rec)
	if accepted.Status != PairingAdded || accepted.Entry == nil || accepted.Entry.Origin != planning.OriginAutopilot || len(accepted.Plan.Entries) != 3 {
		t.Fatalf("accepted = %+v", accepted)
	}
	rec = doPath(t, router, http.MethodPost, "/autopilot/weeks/"+testWeek+"/pairings/accept",
		`{"entryId":"`+soup.ID+`","key":"grocery:club crackers"}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	item := decodeBody[PairingAcceptResponse](t, rec).GroceryItem
	if item == nil || item.Text != "Club crackers for Onion Soup" {
		t.Fatalf("item = %+v", item)
	}

	// The week now lists the grocery item and no longer offers either.
	rec = doPath(t, router, http.MethodGet, "/autopilot/weeks/"+testWeek+"/pairings", "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	week = decodeBody[WeekPairingsResponse](t, rec)
	if len(week.GroceryItems) != 1 || week.GroceryItems[0].ID != item.ID {
		t.Fatalf("grocery items = %+v", week.GroceryItems)
	}
	for _, m := range week.Meals {
		if len(m.Pairings) != 0 {
			t.Errorf("meal still offers %+v", m.Pairings)
		}
	}

	wantStatus(t, doPath(t, router, http.MethodDelete, "/autopilot/weeks/"+testWeek+"/pairings/grocery-items/"+item.ID, "", userAda), http.StatusNoContent, "")
	wantStatus(t, doPath(t, router, http.MethodDelete, "/autopilot/weeks/"+testWeek+"/pairings/grocery-items/"+item.ID, "", userAda), http.StatusNotFound, "not_found")

	// Dismissing a suggestion.
	env.learnRolls(t)
	rec = doPath(t, router, http.MethodPost, "/autopilot/weeks/"+testWeek+"/pairings/dismiss",
		`{"entryId":"`+pasta.ID+`","key":"recipe:`+rRolls+`"}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")

	// Rules from the profile, and a rule made from a learned pairing.
	rec = doPath(t, router, http.MethodGet, "/autopilot/profile", "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	profile := decodeBody[ProfileResponse](t, rec)
	if len(profile.Pairings) != 2 || profile.Pairings[0].Add.Kind != PairingKindRecipe || *profile.Pairings[0].Add.RecipeName != "Garlic Bread" ||
		profile.Sections[string(SectionPairings)] == nil {
		t.Fatalf("profile pairings = %+v", profile.Pairings)
	}
	if g := profile.Pairings[1].Add.GroceryItem; g == nil || g.Name != "Club crackers" {
		t.Errorf("grocery rule = %+v", profile.Pairings[1].Add)
	}

	soupEntry := env.planMeal(t, rTacos, "wed", 2)
	_ = soupEntry
	rec = doPath(t, router, http.MethodPost, "/autopilot/weeks/"+testWeek+"/pairings/rules",
		`{"entryId":"`+pasta.ID+`","key":"recipe:`+rRolls+`","frequency":"always"}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	rule := decodeBody[MakePairingRuleResponse](t, rec)
	if rule.Status != RuleCreated || rule.Rule.Frequency != PairingAlways || len(rule.Profile.Pairings) != 3 {
		t.Fatalf("rule = %+v", rule)
	}

	// Rules are edited through the profile like any other section.
	body := `{"pairings":[{"id":null,"label":"Soup night","when":{"mealCategories":["soup"],"cuisines":[],"tags":[],"proteins":[]},` +
		`"add":{"kind":"grocery_item","recipeId":null,"recipeName":null,"groceryItem":{"name":"Oyster crackers","quantity":2,"unit":"package"}},"frequency":"suggest"}]}`
	rec = doPath(t, router, http.MethodPatch, "/autopilot/profile", body, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	saved := decodeBody[ProfileResponse](t, rec)
	if len(saved.Pairings) != 1 || *saved.Pairings[0].ID == "" || saved.Pairings[0].Label != "Soup night" {
		t.Fatalf("saved rules = %+v", saved.Pairings)
	}
	if q := saved.Pairings[0].Add.GroceryItem.Quantity; q == nil || *q != 2 {
		t.Errorf("quantity = %+v", q)
	}
	// Sending them back unchanged keeps their ids.
	again := doPath(t, router, http.MethodPatch, "/autopilot/profile", `{"pairings":`+mustJSON(t, saved.Pairings)+`}`, userAda)
	wantStatus(t, again, http.StatusOK, "")
	if got := decodeBody[ProfileResponse](t, again); *got.Pairings[0].ID != *saved.Pairings[0].ID {
		t.Errorf("rule id changed: %+v", got.Pairings)
	}
	wantStatus(t, doPath(t, router, http.MethodPatch, "/autopilot/profile",
		`{"pairings":[{"when":{"mealCategories":["pasta"]},"add":{"recipeId":"`+rRigatoni+`"}}]}`, userAda), http.StatusBadRequest, "validation_failed")

	// The recipe carousel.
	rec = doPath(t, router, http.MethodGet, "/recipes/"+rSoup+"/pairings?week="+testWeek, "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	carousel := decodeBody[RecipePairingsResponse](t, rec)
	if carousel.RecipeID != rSoup || carousel.Week == nil || carousel.EntryID == nil || len(carousel.Items) != 1 {
		t.Fatalf("carousel = %+v", carousel)
	}
	if carousel.Items[0].Target.GroceryItem == nil || carousel.Items[0].Target.GroceryItem.Name != "Oyster crackers" {
		t.Errorf("item = %+v", carousel.Items[0].Target)
	}
	_ = ctx
}

func TestProposalPairingsInResponses(t *testing.T) {
	router, env := newPairingRouter(t)
	env.saveRules(t, tacosBreadRule())

	rec := doPath(t, router, http.MethodPost, "/autopilot/weeks/"+testWeek+"/generate", "", userAda)
	wantStatus(t, rec, http.StatusCreated, "")
	proposal := decodeBody[ProposalResponse](t, rec)
	var pairing ProposalPairingJSON
	for _, sl := range proposal.Slots {
		if len(sl.Pairings) > 0 {
			pairing = sl.Pairings[0]
		}
	}
	if pairing.ID == "" || !pairing.Included || pairing.Target.Recipe == nil {
		t.Fatalf("proposal pairing = %+v", pairing)
	}

	rec = doPath(t, router, http.MethodPost, "/autopilot/weeks/"+testWeek+"/proposal/accept",
		`{"version":`+mustJSON(t, proposal.Version)+`,"pairingIds":["`+pairing.ID+`"]}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	accepted := decodeBody[AcceptResponse](t, rec)
	if len(accepted.PairingsAdded) != 1 || accepted.PairingsAdded[0].Entry == nil || len(accepted.PairingsSkipped) != 0 {
		t.Fatalf("accepted = %+v", accepted.PairingsAdded)
	}
}
