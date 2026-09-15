package recommendations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
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

// fakeAuthorizer grants permissions per household and user, like
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

var testAuthorizer = fakeAuthorizer{
	hhA + "/" + userAda:  {households.PermHouseholdView, households.PermPlanEdit},
	hhA + "/" + userView: {households.PermHouseholdView},
}

func newTestRouter(t *testing.T) (http.Handler, *testEnv) {
	t.Helper()
	env := newTestEnv(t)
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: env.svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount)
	return r, env
}

func do(t *testing.T, router http.Handler, method, path, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/households/"+hhA+"/autopilot"+path, strings.NewReader(body))
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

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	if code != "" {
		var body struct {
			Error struct{ Code string } `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Error.Code != code {
			t.Fatalf("error code = %q, want %q: %s", body.Error.Code, code, rec.Body.String())
		}
	}
}

func TestHandlerPermissions(t *testing.T) {
	router, _ := newTestRouter(t)
	for _, tt := range []struct {
		method, path, body, user string
		status                   int
		code                     string
	}{
		{http.MethodGet, "/profile", "", userView, http.StatusOK, ""},
		{http.MethodGet, "/profile", "", "", http.StatusUnauthorized, "unauthenticated"},
		{http.MethodGet, "/profile", "", userAlan, http.StatusNotFound, "not_found"},
		{http.MethodPut, "/profile", `{}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodPatch, "/profile", `{"novelty":"favorites"}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodPut, "/weeks/2026-W38/context", `{}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodDelete, "/weeks/2026-W38/context", "", userView, http.StatusForbidden, "forbidden"},
		{http.MethodPost, "/weeks/2026-W38/generate", "", userView, http.StatusForbidden, "forbidden"},
		{http.MethodPost, "/weeks/2026-W38/proposal/slots/mon/swap", `{"version":1}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodPost, "/weeks/2026-W38/proposal/accept", `{"version":1}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodPost, "/weeks/2026-W38/proposal/reject", `{"version":1}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodPut, "/recipes/" + rTenderloin + "/override", `{"methods":{"grill":true}}`, userView, http.StatusForbidden, "forbidden"},
		{http.MethodGet, "/vocabulary", "", userView, http.StatusOK, ""},
		{http.MethodGet, "/weeks/2026-W38/context", "", userView, http.StatusOK, ""},
		{http.MethodGet, "/recipe-overrides", "", userView, http.StatusOK, ""},
		{http.MethodGet, "/profile/history", "", userView, http.StatusOK, ""},
	} {
		t.Run(tt.method+" "+tt.path+" as "+tt.user, func(t *testing.T) {
			wantStatus(t, do(t, router, tt.method, tt.path, tt.body, tt.user), tt.status, tt.code)
		})
	}
}

const onboardingJSON = `{
  "taste": {"likes": {"cuisines": ["Mexican"], "tags": [], "proteins": ["pork"]}, "dislikes": {"cuisines": [], "tags": [], "proteins": []}},
  "restrictions": {"diets": [], "allergens": ["peanuts"], "excludedIngredients": ["cilantro"], "excludedCuisines": [], "excludedProteins": [], "excludedTags": [], "noSpicy": true},
  "schedule": {"planDays": ["mon", "tue", "wed", "thu", "fri", "sat", "sun"], "weeknights": ["mon", "tue", "wed", "thu"], "mealsPerWeek": 5, "defaultServings": null, "weeknightMaxMinutes": 40},
  "cookTime": {"quickMaxMinutes": 20, "mediumMaxMinutes": 35, "maxLongPerWeek": 1, "minQuickPerWeek": 1, "avoidConsecutiveLong": true},
  "novelty": "balanced",
  "equipment": ["smoker"],
  "weekdayRules": [{"day": "sun", "label": "Sunday smoker night", "cuisines": [], "tags": [], "proteins": ["chicken", "pork"], "methods": ["smoker"], "timeBand": "long", "frequency": "at_most_once"}]
}`

func TestHandlerFlow(t *testing.T) {
	router, env := newTestRouter(t)

	// Profile.
	rec := do(t, router, http.MethodGet, "/profile", "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if raw["configured"] != false || raw["sections"].(map[string]any)["taste"] != nil || raw["createdAt"] != nil ||
		raw["effective"].(map[string]any)["defaultServings"] != float64(2) || raw["schedule"].(map[string]any)["defaultServings"] != nil {
		t.Errorf("default profile = %s", rec.Body.String())
	}
	rec = do(t, router, http.MethodPut, "/profile", onboardingJSON, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	profile := decodeBody[ProfileResponse](t, rec)
	if !profile.Configured || profile.Sections["taste"] == nil || profile.Sections["taste"].UpdatedBy != userAda ||
		*profile.WeekdayRules[0].TimeBand != "long" || *profile.Schedule.WeeknightMaxMinutes != 40 || profile.Taste.Likes.Cuisines[0] != "mexican" {
		t.Errorf("profile = %+v", profile)
	}
	rec = do(t, router, http.MethodPatch, "/profile", `{"cookTime": {"quickMaxMinutes": 20, "mediumMaxMinutes": 35, "maxLongPerWeek": 2, "minQuickPerWeek": 0, "avoidConsecutiveLong": false}}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	if p := decodeBody[ProfileResponse](t, rec); p.CookTime.MaxLongPerWeek != 2 || p.Equipment[0] != "smoker" {
		t.Errorf("patched = %+v", p)
	}
	wantStatus(t, do(t, router, http.MethodPut, "/profile", `{"tast": {}}`, userAda), http.StatusBadRequest, "invalid_request")
	wantStatus(t, do(t, router, http.MethodPatch, "/profile", `{}`, userAda), http.StatusBadRequest, "validation_failed")
	wantStatus(t, do(t, router, http.MethodPatch, "/profile", `{"schedule": {"planDays": ["mon"], "weeknights": [], "mealsPerWeek": 1, "defaultServings": 0}}`, userAda),
		http.StatusBadRequest, "validation_failed")
	wantStatus(t, do(t, router, http.MethodPatch, "/profile", `{"equipment": []}`, userAda), http.StatusBadRequest, "validation_failed")

	// Vocabulary.
	rec = do(t, router, http.MethodGet, "/vocabulary", "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	vocab := decodeBody[VocabularyResponse](t, rec)
	if len(vocab.Cuisines) == 0 || vocab.Cuisines[0].RecipeCount == nil || vocab.Diets[0].RecipeCount != nil || vocab.Limits.MaxNoteLength != MaxNoteLength || vocab.CatalogRecipeCount != 10 {
		t.Errorf("vocabulary = %+v", vocab)
	}

	// Week context.
	rec = do(t, router, http.MethodPut, "/weeks/2026-W38/context", `{"busy": false, "maxMinutes": null, "days": [{"day": "fri", "skip": false, "maxMinutes": null, "servings": 4}], "note": "guests Friday"}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	if c := decodeBody[WeekContextResponse](t, rec); !c.Configured || *c.Days[0].Servings != 4 || c.StartDate != "2026-09-14" || c.MaxMinutes != nil || *c.UpdatedBy != userAda {
		t.Errorf("context = %+v", c)
	}
	wantStatus(t, do(t, router, http.MethodPut, "/weeks/2026-W38/context", `{"maxMinutes": 0}`, userAda), http.StatusBadRequest, "validation_failed")
	wantStatus(t, do(t, router, http.MethodGet, "/weeks/2026-W99/context", "", userView), http.StatusBadRequest, "validation_failed")

	// Generate, swap, accept.
	wantStatus(t, do(t, router, http.MethodGet, "/weeks/2026-W38/proposal", "", userView), http.StatusNotFound, "not_found")
	rec = do(t, router, http.MethodPost, "/weeks/2026-W38/generate", "", userAda)
	wantStatus(t, rec, http.StatusCreated, "")
	proposal := decodeBody[ProposalResponse](t, rec)
	if proposal.Status != StatusProposed || proposal.Version != 1 || len(proposal.Slots) != 5 || proposal.ModelVersion == "" || proposal.InputsHash == "" {
		t.Fatalf("proposal = %+v", proposal)
	}
	for _, s := range proposal.Slots {
		if s.Date == "" || s.Recipe.Name == "" || len(s.Reasons) == 0 || s.Signals == nil || s.TimeBand == "" {
			t.Errorf("slot = %+v", s)
		}
		if s.Day == "fri" && s.Servings != 4 {
			t.Errorf("friday servings = %d, want the context's 4", s.Servings)
		}
	}
	wantStatus(t, do(t, router, http.MethodGet, "/weeks/2026-W38/proposal", "", userView), http.StatusOK, "")
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/generate", `{"avoidPrevious": "yes"}`, userAda), http.StatusBadRequest, "invalid_request")

	day := proposal.Slots[0].Day
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/proposal/slots/"+day+"/swap", `{}`, userAda), http.StatusBadRequest, "validation_failed")
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/proposal/slots/"+day+"/swap", `{"version": 99}`, userAda), http.StatusConflict, "proposal_changed")
	rec = do(t, router, http.MethodPost, "/weeks/2026-W38/proposal/slots/"+day+"/swap", `{"version": 1}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	swapped := decodeBody[ProposalResponse](t, rec)
	if swapped.SwapCount != 1 || swapped.Slots[0].SwapCount != 1 || swapped.Slots[0].Recipe.ID == proposal.Slots[0].Recipe.ID {
		t.Errorf("swapped = %+v", swapped.Slots[0])
	}

	rec = do(t, router, http.MethodPost, "/weeks/2026-W38/proposal/accept", `{"version": `+strconv.FormatInt(swapped.Version, 10)+`, "excludeSlotIds": []}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	accepted := decodeBody[AcceptResponse](t, rec)
	if accepted.Proposal.Status != StatusAccepted || len(accepted.Added) != 5 || len(accepted.Plan.Entries) != 5 || len(accepted.Skipped) != 0 || *accepted.Proposal.DecidedBy != userAda {
		t.Fatalf("accepted = %+v", accepted)
	}
	for _, e := range accepted.Plan.Entries {
		if e.Origin != planning.OriginAutopilot {
			t.Errorf("plan entry origin = %q", e.Origin)
		}
	}
	version := strconv.FormatInt(accepted.Proposal.Version, 10)
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/proposal/accept", `{"version": `+version+`}`, userAda), http.StatusConflict, "proposal_not_pending")
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/proposal/reject", `{"version": `+version+`}`, userAda), http.StatusConflict, "proposal_not_pending")

	// A finalized week.
	env.planner.set(planning.Plan{HouseholdID: hhA, Week: mustWeek(t, "2026-W40"), Status: planning.StatusFinalized})
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W40/generate", `{}`, userAda), http.StatusConflict, "plan_finalized")

	// Recipe attributes and overrides.
	rec = do(t, router, http.MethodPut, "/recipes/"+rTenderloin+"/override", `{"methods": {"smoker": false}}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	attrs := decodeBody[AttributesResponse](t, rec)
	if attrs.Override == nil || attrs.Override.Methods["smoker"] || *attrs.CookMinutes != 90 || attrs.Methods[0].Source != SourceOverride || attrs.Methods[0].Label != "Smoker" {
		t.Errorf("attributes = %+v", attrs)
	}
	wantStatus(t, do(t, router, http.MethodPut, "/recipes/"+rTenderloin+"/override", `{"methods": {"oven": true}}`, userAda), http.StatusBadRequest, "validation_failed")
	wantStatus(t, do(t, router, http.MethodGet, "/recipes/ffffffffffffffffffffffff/attributes", "", userView), http.StatusNotFound, "not_found")
	rec = do(t, router, http.MethodGet, "/recipe-overrides", "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	if list := decodeBody[OverrideListResponse](t, rec); len(list.Items) != 1 || list.Items[0].RecipeID != rTenderloin {
		t.Errorf("overrides = %+v", list)
	}

	// History.
	rec = do(t, router, http.MethodGet, "/profile/history?limit=2", "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	if h := decodeBody[HistoryResponse](t, rec); len(h.Items) != 2 || h.Items[0].Type != "autopilot.recipe_override_updated" || h.Items[1].Week != testWeek {
		t.Errorf("history = %+v", h)
	}
	wantStatus(t, do(t, router, http.MethodGet, "/profile/history?limit=0", "", userView), http.StatusBadRequest, "validation_failed")
	wantStatus(t, do(t, router, http.MethodDelete, "/weeks/2026-W38/context", "", userAda), http.StatusNoContent, "")
}
