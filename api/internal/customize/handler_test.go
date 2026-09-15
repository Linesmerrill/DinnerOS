package customize

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

// fakeAuthorizer grants permissions per (household, user).
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
	hhAda + "/" + userAda:  {households.PermHouseholdView, households.PermPlanEdit},
	hhAda + "/" + userView: {households.PermHouseholdView},
}

func newTestRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", h.Mount)
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
	body := decodeBody[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)
	if body.Error.Code != code {
		t.Errorf("error code = %q, want %q (body %s)", body.Error.Code, code, rec.Body.String())
	}
}

func newHandlerFixture(t *testing.T) (*fixture, *chi.Mux) {
	t.Helper()
	f := newFixture(t)
	return f, newTestRouter(NewHandler(HandlerOptions{Service: f.svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}))
}

func TestGetCustomizations(t *testing.T) {
	f, router := newHandlerFixture(t)
	path := "/api/v1/households/" + hhAda + "/recipes/" + householdTacos().ID + "/customizations"

	rec := do(t, router, http.MethodGet, path, "", userAda)
	wantStatus(t, rec, http.StatusOK)
	resp := decodeBody[CustomizationsResponse](t, rec)
	if resp.RecipeID != householdTacos().ID || resp.Servings != 2 || len(resp.Groups) != 2 {
		t.Fatalf("response = %+v", resp)
	}
	g := resp.Groups[0]
	if g.IngredientKey != ingPork || g.IngredientName != "Ground Pork" || g.AmountText != "10 ounce" || g.Quantity != "10" || g.Unit != "oz" {
		t.Errorf("group = %+v", g)
	}
	if g.ImageURL == nil || *g.ImageURL != "https://img.example.com/pork.png" || g.SelectedChoiceID != nil {
		t.Errorf("group image/selection = %+v", g)
	}
	original, double := g.Choices[0], g.Choices[1]
	if original.ID != "original" || original.Label != "Ground Pork" || original.AmountText != "10 ounce" || original.Kind != KindOriginal || original.Badge != nil {
		t.Errorf("original = %+v", original)
	}
	if double.ID != "double" || double.Label != "2x Ground Pork" || double.AmountText != "20 ounce" || double.Kind != KindDouble ||
		double.Badge == nil || *double.Badge != BadgeDouble {
		t.Errorf("double = %+v", double)
	}
	i := slices.IndexFunc(g.Choices, func(c ChoiceResponse) bool { return c.ID == "swap:ground-beef:double" })
	if i < 0 {
		t.Fatalf("no beef swap in %+v", g.Choices)
	}
	if beef := g.Choices[i]; beef.Label != "2x Ground Beef" || beef.IngredientName != "Ground Beef" || beef.AmountText != "20 ounce" || beef.Kind != KindSwapDouble {
		t.Errorf("beef double = %+v", beef)
	}

	// With an entry, the selection is reported.
	if _, err := f.svc.SetCustomizations(context.Background(), hhAda, userAda, testWeek, entryID, []Selection{{IngredientKey: ingPork, ChoiceID: "double"}}); err != nil {
		t.Fatal(err)
	}
	rec = do(t, router, http.MethodGet, path+"?entryId="+entryID+"&week="+testWeek, "", userAda)
	wantStatus(t, rec, http.StatusOK)
	resp = decodeBody[CustomizationsResponse](t, rec)
	if resp.Servings != 4 || resp.Groups[0].SelectedChoiceID == nil || *resp.Groups[0].SelectedChoiceID != "double" {
		t.Errorf("entry response = %+v", resp.Groups[0])
	}
	if resp.Groups[0].AmountText != "20 ounce" {
		t.Errorf("entry amount = %q", resp.Groups[0].AmountText)
	}

	// A recipe without protein lines returns an empty list, not null.
	rec = do(t, router, http.MethodGet, "/api/v1/households/"+hhAda+"/recipes/"+onionRecipe+"/customizations", "", userAda)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"groups":[]`) {
		t.Errorf("onion body = %s", rec.Body.String())
	}
}

func TestGetCustomizationsErrors(t *testing.T) {
	_, router := newHandlerFixture(t)
	base := "/api/v1/households/" + hhAda + "/recipes/"
	path := base + householdTacos().ID + "/customizations"

	wantError(t, do(t, router, http.MethodGet, path, "", ""), http.StatusUnauthorized, "unauthenticated")
	wantError(t, do(t, router, http.MethodGet, base+"66e5a1f2c3b4a5d6e7f89999/customizations", "", userAda), http.StatusNotFound, "not_found")
	wantError(t, do(t, router, http.MethodGet, path+"?servings=0", "", userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodGet, path+"?servings=three", "", userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodGet, path+"?servings=3", "", userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodGet, path+"?entryId="+entryID, "", userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodGet, path+"?entryId=x&week=nonsense", "", userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodGet, path+"?entryId=66e5a1f2c3b4a5d6e7f89999&week="+testWeek, "", userAda), http.StatusNotFound, "not_found")
	// Viewing needs household.view only.
	wantStatus(t, do(t, router, http.MethodGet, path, "", userView), http.StatusOK)
}

func TestPutCustomization(t *testing.T) {
	f, router := newHandlerFixture(t)
	path := "/api/v1/households/" + hhAda + "/plans/" + testWeek + "/entries/" + entryID + "/customization"

	rec := do(t, router, http.MethodPut, path, `{"selections":[{"ingredientKey":"`+ingPork+`","choiceId":"swap:ground-beef"}]}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	plan := decodeBody[planning.PlanResponse](t, rec)
	if len(plan.Entries) != 1 || len(plan.Entries[0].Customizations) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if c := plan.Entries[0].Customizations[0]; c.IngredientKey != ingPork || c.ChoiceID != "swap:ground-beef" || c.Label != "Ground Beef" {
		t.Errorf("customization = %+v", c)
	}

	// An empty list resets, and the field is then omitted.
	rec = do(t, router, http.MethodPut, path, `{"selections":[]}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "customizations") {
		t.Errorf("reset body = %s", rec.Body.String())
	}

	// Validation and permissions.
	wantError(t, do(t, router, http.MethodPut, path, `{}`, userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodPut, path, `{"selections":[{"ingredientKey":"`+ingOnion+`","choiceId":"double"}]}`, userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodPut, path, `{"selections":[{"ingredientKey":"`+ingPork+`","choiceId":"swap:lobster"}]}`, userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodPut, path, `{"selections":[{"ingredientKey":"`+ingPork+`","choiceId":"double","extra":1}]}`, userAda), http.StatusBadRequest, "invalid_request")
	wantError(t, do(t, router, http.MethodPut, path, `{"selections":[]}`, userView), http.StatusForbidden, "forbidden")
	wantError(t, do(t, router, http.MethodPut, path, `{"selections":[]}`, ""), http.StatusUnauthorized, "unauthenticated")
	wantError(t, do(t, router, http.MethodPut,
		"/api/v1/households/"+hhAda+"/plans/"+testWeek+"/entries/66e5a1f2c3b4a5d6e7f89999/customization", `{"selections":[]}`, userAda), http.StatusNotFound, "not_found")
	wantError(t, do(t, router, http.MethodPut,
		"/api/v1/households/"+hhAda+"/plans/nonsense/entries/"+entryID+"/customization", `{"selections":[]}`, userAda), http.StatusBadRequest, "validation_failed")

	// A finalized plan follows the planner's conflict behavior.
	f.plans.mu.Lock()
	p := f.plans.plans[testWeek]
	p.Status = planning.StatusFinalized
	f.plans.plans[testWeek] = p
	f.plans.mu.Unlock()
	wantError(t, do(t, router, http.MethodPut, path, `{"selections":[{"ingredientKey":"`+ingPork+`","choiceId":"double"}]}`, userAda), http.StatusConflict, "plan_finalized")
}
