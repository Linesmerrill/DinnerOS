package planning

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// fakeTokens accepts bearer tokens of the form "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer grants permissions per (household, user), mirroring
// households.Service.Authorize: unknown pairs are ErrNotFound, missing
// permissions ErrForbidden.
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

var testAuthorizer = fakeAuthorizer{
	hhAda + "/" + userAda:    {households.PermHouseholdView, households.PermPlanEdit},
	hhAda + "/" + userViewer: {households.PermHouseholdView},
	hhBob + "/" + userBob:    {households.PermHouseholdView, households.PermPlanEdit},
}

func newPlanTestRouter(t *testing.T) *chi.Mux {
	t.Helper()
	svc, _ := newTestService(t, newMemoryStore())
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount)
	return r
}

func do(t *testing.T, router http.Handler, method, path, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	wantStatus(t, rec, status)
	if got := decodeBody[httpx.ErrorResponse](t, rec).Error.Code; got != code {
		t.Fatalf("error code = %q, want %q (body %s)", got, code, rec.Body.String())
	}
}

func plansPath(householdID string) string { return "/api/v1/households/" + householdID + "/plans" }

func TestPlanHandlersFlow(t *testing.T) {
	router := newPlanTestRouter(t)
	plan := plansPath(hhAda) + "/" + testWeek

	// An unplanned week is an empty draft, visible to viewers.
	rec := do(t, router, http.MethodGet, plan, "", userViewer)
	wantStatus(t, rec, http.StatusOK)
	for _, fragment := range []string{`"entries":[]`, `"createdAt":null`, `"updatedAt":null`} {
		if !strings.Contains(rec.Body.String(), fragment) {
			t.Errorf("empty plan %s lacks %s", rec.Body.String(), fragment)
		}
	}
	empty := decodeBody[PlanResponse](t, rec)
	if empty.HouseholdID != hhAda || empty.Week != testWeek || empty.StartDate != testWeekStart || empty.EndDate != "2026-09-20" || empty.Status != StatusDraft {
		t.Errorf("empty plan = %+v", empty)
	}

	rec = do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeTacos+`","day":"tue","servings":2,"note":"extra lime"}`, userAda)
	wantStatus(t, rec, http.StatusCreated)
	added := decodeBody[AddEntryResponse](t, rec)
	tacos := added.Entry
	if tacos.ID == "" || tacos.Day == nil || *tacos.Day != Tuesday || tacos.Date == nil || *tacos.Date != "2026-09-15" ||
		tacos.Recipe != (EntryRecipeResponse{ID: recipeTacos, Name: "Beef Tacos", ImageURL: "https://img.example.com/tacos.jpg"}) ||
		tacos.Servings != 2 || tacos.Note != "extra lime" || tacos.AddedBy != userAda || !tacos.AddedAt.Equal(testNow) {
		t.Errorf("added entry = %+v", tacos)
	}
	if len(added.Plan.Entries) != 1 || added.Plan.CreatedAt == nil || !added.Plan.CreatedAt.Equal(testNow) {
		t.Errorf("plan after add = %+v", added.Plan)
	}

	rec = do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeSoup+`","day":null,"servings":2}`, userAda)
	wantStatus(t, rec, http.StatusCreated)
	if soup := decodeBody[AddEntryResponse](t, rec).Entry; soup.Day != nil || soup.Date != nil || !strings.Contains(rec.Body.String(), `"day":null`) {
		t.Errorf("unscheduled entry = %s", rec.Body.String())
	}
	wantStatus(t, do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeSalad+`","servings":2}`, userAda), http.StatusCreated)

	// PATCH: null unschedules, absent fields stay.
	rec = do(t, router, http.MethodPatch, plan+"/entries/"+tacos.ID, `{"day":null,"servings":4}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	if e := decodeBody[PlanResponse](t, rec).Entries[0]; e.Day != nil || e.Date != nil || e.Servings != 4 || e.Note != "extra lime" {
		t.Errorf("patched entry = %+v", e)
	}
	rec = do(t, router, http.MethodPatch, plan+"/entries/"+tacos.ID, `{"day":"sun","note":""}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	if e := decodeBody[PlanResponse](t, rec).Entries[0]; e.Day == nil || *e.Day != Sunday || *e.Date != "2026-09-20" || e.Servings != 4 || e.Note != "" {
		t.Errorf("patched entry = %+v", e)
	}

	rec = do(t, router, http.MethodGet, plansPath(hhAda)+"?from=2026-W37&to=2026-W39", "", userViewer)
	wantStatus(t, rec, http.StatusOK)
	list := decodeBody[PlanListResponse](t, rec)
	if len(list.Items) != 3 || list.Items[1].Week != testWeek || list.Items[1].EntryCount != 3 || list.Items[1].StartDate != testWeekStart ||
		list.Items[1].UpdatedAt == nil || list.Items[0].EntryCount != 0 || list.Items[0].UpdatedAt != nil {
		t.Errorf("list = %s", rec.Body.String())
	}

	rec = do(t, router, http.MethodGet, plan+"/grocery", "", userViewer)
	wantStatus(t, rec, http.StatusOK)
	g := decodeBody[GroceryListResponse](t, rec)
	if g.Week != testWeek || g.PantryApplied || len(g.Categories) == 0 || g.Categories[0].Category != "produce" || !strings.Contains(rec.Body.String(), `"skipped":[]`) {
		t.Fatalf("grocery = %s", rec.Body.String())
	}
	var onion *GroceryItemResponse
	for i := range g.Categories[0].Items {
		if g.Categories[0].Items[i].Name == "Yellow Onion" {
			onion = &g.Categories[0].Items[i]
		}
	}
	// Tacos for 4 (1 onion) + salad (½ onion) combine; the soup's 8 oz can't.
	wantOnion := GroceryItemResponse{
		IngredientKey: ingOnion, Name: "Yellow Onion", QuantityText: "1 ½ + 8 oz", Status: "toBuy",
		Amounts: []GroceryAmountResponse{
			{Quantity: "3/2", QuantityValue: 1.5, Unit: "count", Text: "1 ½"},
			{Quantity: "8", QuantityValue: 8, Unit: "oz", Text: "8 oz"},
		},
		Recipes: []GroceryRecipeResponse{{recipeTacos, "Beef Tacos"}, {recipeSalad, "Chicken Salad"}, {recipeSoup, "Onion Soup"}},
	}
	if onion == nil || onion.QuantityText != wantOnion.QuantityText || !slices.Equal(onion.Amounts, wantOnion.Amounts) || !slices.Equal(onion.Recipes, wantOnion.Recipes) || onion.IngredientKey != ingOnion {
		t.Errorf("onion = %+v, want %+v", onion, wantOnion)
	}

	rec = do(t, router, http.MethodPut, plan+"/status", `{"status":"finalized"}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	if p := decodeBody[PlanResponse](t, rec); p.Status != StatusFinalized || len(p.Entries) != 3 {
		t.Errorf("finalized plan = %+v", p)
	}
	wantError(t, do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeSoup+`","servings":2}`, userAda), 409, "plan_finalized")
	wantError(t, do(t, router, http.MethodDelete, plan+"/entries/"+tacos.ID, "", userAda), 409, "plan_finalized")

	wantStatus(t, do(t, router, http.MethodPut, plan+"/status", `{"status":"draft"}`, userAda), http.StatusOK)
	rec = do(t, router, http.MethodDelete, plan+"/entries/"+tacos.ID, "", userAda)
	wantStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Errorf("delete body = %q", rec.Body.String())
	}
	if p := decodeBody[PlanResponse](t, do(t, router, http.MethodGet, plan, "", userAda)); len(p.Entries) != 2 {
		t.Errorf("plan after delete = %+v", p)
	}
}

func TestPlanHandlerErrors(t *testing.T) {
	router := newPlanTestRouter(t)
	plan := plansPath(hhAda) + "/" + testWeek
	entries := plan + "/entries"
	rec := do(t, router, http.MethodPost, entries, `{"recipeId":"`+recipeTacos+`","servings":2}`, userAda)
	wantStatus(t, rec, http.StatusCreated)
	entry := entries + "/" + decodeBody[AddEntryResponse](t, rec).Entry.ID
	status, groceryPath, list := plan+"/status", plan+"/grocery", plansPath(hhAda)+"?from=2026-W38&to=2026-W40"
	valid := `{"recipeId":"` + recipeTacos + `","servings":2}`

	tests := []struct {
		name         string
		method, path string
		body         string
		userID       string
		status       int
		code         string
	}{
		{"get unauthenticated", "GET", plan, "", "", 401, "unauthenticated"},
		{"list unauthenticated", "GET", list, "", "", 401, "unauthenticated"},
		{"grocery unauthenticated", "GET", groceryPath, "", "", 401, "unauthenticated"},
		{"add unauthenticated", "POST", entries, valid, "", 401, "unauthenticated"},
		{"update unauthenticated", "PATCH", entry, `{"note":"x"}`, "", 401, "unauthenticated"},
		{"delete unauthenticated", "DELETE", entry, "", "", 401, "unauthenticated"},
		{"status unauthenticated", "PUT", status, `{"status":"draft"}`, "", 401, "unauthenticated"},

		{"get by non-member", "GET", plan, "", userCat, 404, "not_found"},
		{"list by non-member", "GET", list, "", userBob, 404, "not_found"},
		{"grocery by non-member", "GET", groceryPath, "", userBob, 404, "not_found"},
		{"add by non-member", "POST", entries, valid, userBob, 404, "not_found"},
		{"update by non-member", "PATCH", entry, `{"note":"x"}`, userCat, 404, "not_found"},
		{"delete by non-member", "DELETE", entry, "", userBob, 404, "not_found"},
		{"status by non-member", "PUT", status, `{"status":"draft"}`, userBob, 404, "not_found"},
		{"malformed household", "GET", plansPath("not-a-household") + "/" + testWeek, "", userAda, 404, "not_found"},

		{"add without plan.edit", "POST", entries, valid, userViewer, 403, "forbidden"},
		{"update without plan.edit", "PATCH", entry, `{"note":"x"}`, userViewer, 403, "forbidden"},
		{"delete without plan.edit", "DELETE", entry, "", userViewer, 403, "forbidden"},
		{"status without plan.edit", "PUT", status, `{"status":"finalized"}`, userViewer, 403, "forbidden"},

		{"get bad week", "GET", plansPath(hhAda) + "/2026-W60", "", userAda, 400, "validation_failed"},
		{"get week without W", "GET", plansPath(hhAda) + "/2026-38", "", userAda, 400, "validation_failed"},
		{"grocery bad week", "GET", plansPath(hhAda) + "/2025-W53/grocery", "", userAda, 400, "validation_failed"},
		{"add bad week", "POST", plansPath(hhAda) + "/next/entries", valid, userAda, 400, "validation_failed"},
		{"list without range", "GET", plansPath(hhAda), "", userAda, 400, "validation_failed"},
		{"list reversed range", "GET", plansPath(hhAda) + "?from=2026-W40&to=2026-W38", "", userAda, 400, "validation_failed"},
		{"list range too long", "GET", plansPath(hhAda) + "?from=2026-W01&to=2026-W27", "", userAda, 400, "validation_failed"},
		{"list bad from", "GET", plansPath(hhAda) + "?from=soon&to=2026-W38", "", userAda, 400, "validation_failed"},

		{"add empty body", "POST", entries, "", userAda, 400, "invalid_request"},
		{"add unknown field", "POST", entries, `{"recipeId":"` + recipeTacos + `","servings":2,"addedBy":"someone"}`, userAda, 400, "invalid_request"},
		{"add wrong type", "POST", entries, `{"recipeId":"` + recipeTacos + `","servings":"two"}`, userAda, 400, "invalid_request"},
		{"add missing recipe", "POST", entries, `{"servings":2}`, userAda, 400, "validation_failed"},
		{"add another household's recipe", "POST", entries, `{"recipeId":"` + recipeBobs + `","servings":2}`, userAda, 400, "validation_failed"},
		{"add own recipe to another household", "POST", plansPath(hhBob) + "/" + testWeek + "/entries", valid, userBob, 400, "validation_failed"},
		{"add servings not offered", "POST", entries, `{"recipeId":"` + recipeTacos + `","servings":3}`, userAda, 400, "validation_failed"},
		{"add missing servings", "POST", entries, `{"recipeId":"` + recipeTacos + `"}`, userAda, 400, "validation_failed"},
		{"add unknown day", "POST", entries, `{"recipeId":"` + recipeTacos + `","servings":2,"day":"someday"}`, userAda, 400, "validation_failed"},
		{"add empty day", "POST", entries, `{"recipeId":"` + recipeTacos + `","servings":2,"day":""}`, userAda, 400, "validation_failed"},
		{"add long note", "POST", entries, `{"recipeId":"` + recipeTacos + `","servings":2,"note":"` + strings.Repeat("x", MaxNoteLength+1) + `"}`, userAda, 400, "validation_failed"},

		{"update no fields", "PATCH", entry, `{}`, userAda, 400, "validation_failed"},
		{"update day wrong type", "PATCH", entry, `{"day":5}`, userAda, 400, "invalid_request"},
		{"update empty day", "PATCH", entry, `{"day":""}`, userAda, 400, "validation_failed"},
		{"update servings not offered", "PATCH", entry, `{"servings":3}`, userAda, 400, "validation_failed"},
		{"update unknown field", "PATCH", entry, `{"recipeId":"` + recipeSoup + `"}`, userAda, 400, "invalid_request"},
		{"update unknown entry", "PATCH", entries + "/ffffffffffffffffffffffff", `{"note":"x"}`, userAda, 404, "not_found"},
		{"update entry in another week", "PATCH", plansPath(hhAda) + "/2026-W39/entries/" + entry[strings.LastIndex(entry, "/")+1:], `{"note":"x"}`, userAda, 404, "not_found"},
		{"delete unknown entry", "DELETE", entries + "/nope", "", userAda, 404, "not_found"},
		{"delete entry through another household", "DELETE", plansPath(hhBob) + "/" + testWeek + "/entries/" + entry[strings.LastIndex(entry, "/")+1:], "", userBob, 404, "not_found"},

		{"status unknown", "PUT", status, `{"status":"cooked"}`, userAda, 400, "validation_failed"},
		{"status empty body", "PUT", status, "", userAda, 400, "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, do(t, router, tt.method, tt.path, tt.body, tt.userID), tt.status, tt.code)
		})
	}
}

// An add-on is a plan entry like any other, so the plan itself has to say which
// entries are add-ons: without it a client can only tell them apart once the
// menu's cards are loaded, and a week of seven meals and one add-on read as
// eight meals in the app's week strip.
func TestPlanEntryRecipeMarksAddOns(t *testing.T) {
	router := newPlanTestRouter(t)
	plan := plansPath(hhAda) + "/" + testWeek

	rec := do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeTacos+`","day":"tue","servings":2}`, userAda)
	wantStatus(t, rec, http.StatusCreated)
	if meal := decodeBody[AddEntryResponse](t, rec).Entry; meal.Recipe.IsAddon {
		t.Errorf("meal recipe = %+v, want isAddon false", meal.Recipe)
	}
	// The field is always sent, so nothing has to work it out from the catalog.
	if !strings.Contains(rec.Body.String(), `"isAddon":false`) {
		t.Errorf("meal entry %s lacks an isAddon of false", rec.Body.String())
	}

	rec = do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeBread+`","servings":2}`, userAda)
	wantStatus(t, rec, http.StatusCreated)
	if addOn := decodeBody[AddEntryResponse](t, rec).Entry; !addOn.Recipe.IsAddon {
		t.Errorf("add-on recipe = %+v, want isAddon true", addOn.Recipe)
	}

	// And the whole plan says so, which is what a week's counts are read from.
	rec = do(t, router, http.MethodGet, plan, "", userViewer)
	wantStatus(t, rec, http.StatusOK)
	addOns := map[string]bool{}
	for _, e := range decodeBody[PlanResponse](t, rec).Entries {
		addOns[e.Recipe.Name] = e.Recipe.IsAddon
	}
	if len(addOns) != 2 || addOns["Beef Tacos"] || !addOns["Garlic Bread"] {
		t.Errorf("plan add-on flags = %+v", addOns)
	}
}
