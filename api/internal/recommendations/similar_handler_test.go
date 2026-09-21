package recommendations

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

func TestAlternativesAndSwapEndpoints(t *testing.T) {
	router, env := newTestRouter(t)
	env.planner.set(planning.Plan{HouseholdID: hhA, Week: mustWeek(t, testWeek), Status: planning.StatusDraft, Entries: []planning.Entry{
		{ID: "e-tacos", RecipeID: rTacos, RecipeName: "Beef Tacos", Day: planning.Tuesday, Servings: 2, Origin: planning.OriginManual},
	}})
	base := "/weeks/" + testWeek + "/entries/e-tacos"

	rec := do(t, router, http.MethodGet, base+"/alternatives?limit=2", "", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET alternatives = %d: %s", rec.Code, rec.Body)
	}
	got := decodeBody[AlternativesResponse](t, rec)
	if got.EntryID != "e-tacos" || got.Day != "tue" || got.Recipe.ID != rTacos || got.Week != testWeek {
		t.Fatalf("response = %+v", got)
	}
	if len(got.Alternatives) == 0 || len(got.Alternatives) > 2 {
		t.Fatalf("alternatives = %+v", got.Alternatives)
	}
	first := got.Alternatives[0]
	if first.Recipe.ID == "" || first.Recipe.Name == "" || first.Servings != 2 || len(first.Reasons) == 0 || first.Similarity <= 0 {
		t.Errorf("alternative = %+v", first)
	}

	// A member who can view but not plan can't see or apply alternatives.
	if rec := do(t, router, http.MethodGet, base+"/alternatives", "", userView); rec.Code != http.StatusForbidden {
		t.Errorf("GET alternatives as a viewer = %d", rec.Code)
	}
	if rec := do(t, router, http.MethodPost, base+"/swap", `{"recipeId":"`+rBurger+`"}`, userView); rec.Code != http.StatusForbidden {
		t.Errorf("POST swap as a viewer = %d", rec.Code)
	}
	if rec := do(t, router, http.MethodGet, base+"/alternatives", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET alternatives without a token = %d", rec.Code)
	}

	rec = do(t, router, http.MethodPost, base+"/swap", `{"recipeId":"`+first.Recipe.ID+`"}`, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST swap = %d: %s", rec.Code, rec.Body)
	}
	swapped := decodeBody[SwapEntryResponse](t, rec)
	if swapped.Entry.ID != "e-tacos" || swapped.Entry.Recipe.ID != first.Recipe.ID || swapped.PreviousRecipe.ID != rTacos {
		t.Fatalf("swap response = %+v", swapped)
	}
	if len(swapped.Plan.Entries) != 1 || swapped.Entry.Day == nil || *swapped.Entry.Day != planning.Tuesday {
		t.Errorf("plan = %+v", swapped.Plan)
	}

	// Bad input and unknown meals are reported, not hidden.
	if rec := do(t, router, http.MethodPost, base+"/swap", `{"recipeId":""}`, userAda); rec.Code != http.StatusBadRequest {
		t.Errorf("POST swap without a recipe = %d", rec.Code)
	}
	if rec := do(t, router, http.MethodGet, "/weeks/"+testWeek+"/entries/e-nope/alternatives", "", userAda); rec.Code != http.StatusNotFound {
		t.Errorf("GET alternatives for an unknown meal = %d", rec.Code)
	}
	if rec := do(t, router, http.MethodGet, base+"/alternatives?limit=99", "", userAda); rec.Code != http.StatusBadRequest {
		t.Errorf("GET alternatives with a huge limit = %d", rec.Code)
	}
}

func TestAlternativesOnAFinalizedWeek(t *testing.T) {
	router, env := newTestRouter(t)
	env.planner.set(planning.Plan{HouseholdID: hhA, Week: mustWeek(t, testWeek), Status: planning.StatusFinalized, Entries: []planning.Entry{
		{ID: "e-tacos", RecipeID: rTacos, Day: planning.Tuesday, Servings: 2},
	}})
	base := "/weeks/" + testWeek + "/entries/e-tacos"
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, base + "/alternatives", ""},
		{http.MethodPost, base + "/swap", `{"recipeId":"` + rBurger + `"}`},
	} {
		rec := do(t, router, tc.method, tc.path, tc.body, userAda)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "plan_finalized") {
			t.Errorf("%s %s on a finalized week = %d: %s", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
}
