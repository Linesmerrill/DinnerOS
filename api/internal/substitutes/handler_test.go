package substitutes

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
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

const (
	userViewer = "66e5a1f2c3b4a5d6e7f80c02" // may view but not edit
	userCat    = "66e5a1f2c3b4a5d6e7f80c03" // no household
)

type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer mirrors households.Service.Authorize.
type fakeAuthorizer map[string][]households.Permission

func (f fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	perms, ok := f[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	role := households.RoleMember
	if !slices.Contains(perms, households.PermPantryEdit) {
		role = households.Role("viewer")
	}
	m := households.Membership{HouseholdID: householdID, UserID: userID, Role: role}
	if !slices.Contains(perms, perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

type fakeRenderer struct{}

func (fakeRenderer) ItemResponse(_ context.Context, item pantry.Item) pantry.PantryItemResponse {
	return pantry.PantryItemResponse{ID: item.ID, Key: item.Key, DisplayName: item.DisplayName, Status: item.Status}
}

type testServer struct {
	router *chi.Mux
	f      *fixture
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	f := newFixture(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: fakeRenderer{}, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{
			testHousehold + "/" + testUser:   {households.PermHouseholdView, households.PermPantryEdit},
			testHousehold + "/" + userViewer: {households.PermHouseholdView},
		},
	}).Mount)
	return &testServer{router: r, f: f}
}

func (s *testServer) do(t *testing.T, method, path, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/households/"+testHousehold+"/specialty-ingredients"+path, strings.NewReader(body))
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if got := decode[httpx.ErrorResponse](t, rec, status).Error.Code; got != code {
		t.Fatalf("error code = %q, want %q (body %s)", got, code, rec.Body.String())
	}
}

// TestSettingsEndpoints covers the household strategy: reading the default,
// changing it, and how it shows up on the list as a strategy-sourced choice.
func TestSettingsEndpoints(t *testing.T) {
	s := newTestServer(t)

	set := decode[SpecialtySettingsResponse](t, s.do(t, http.MethodGet, "/settings", "", userViewer), http.StatusOK)
	if set.Strategy != StrategyAsk || len(set.Options) != 3 || set.Options[0].Value != StrategySimilar ||
		set.Options[0].Description == "" || set.Options[1].Value != StrategyClosest || set.Options[2].Value != StrategyAsk {
		t.Fatalf("settings = %+v", set)
	}
	updated := decode[SpecialtySettingsResponse](t, s.do(t, http.MethodPut, "/settings", `{"strategy":"closest"}`, testUser), http.StatusOK)
	if updated.Strategy != StrategyClosest || updated.UpdatedBy == nil || *updated.UpdatedBy != testUser || updated.UpdatedAt == nil {
		t.Errorf("updated settings = %+v", updated)
	}
	// The static route wins over {specialtyId}, so this isn't a 404 lookup.
	if got := decode[SpecialtySettingsResponse](t, s.do(t, http.MethodGet, "/settings", "", testUser), http.StatusOK); got.Strategy != StrategyClosest {
		t.Errorf("settings after the change = %+v", got)
	}
	// With no explicit choice, the list now reports the strategy's pick.
	list := decode[SpecialtyListResponse](t, s.do(t, http.MethodGet, "", "", testUser), http.StatusOK)
	tex := list.Items[0]
	if tex.ID != "tex-mex-paste" || tex.ChoiceSource != ChoiceSourceStrategy || tex.Choice == nil ||
		tex.Choice.OptionID != "tex-mex-paste.batch" || tex.Choice.Strategy == nil || *tex.Choice.Strategy != StrategyClosest ||
		tex.Choice.ChosenBy != nil || tex.Choice.ChosenAt != nil {
		t.Errorf("tex-mex under closest = %+v choice %+v", tex, tex.Choice)
	}
	wantError(t, s.do(t, http.MethodPut, "/settings", `{"strategy":"maybe"}`, testUser), http.StatusBadRequest, "validation_failed")
	wantError(t, s.do(t, http.MethodPut, "/settings", `{"nope":"x"}`, testUser), http.StatusBadRequest, "invalid_request")
	wantError(t, s.do(t, http.MethodPut, "/settings", `{"strategy":"ask"}`, userViewer), http.StatusForbidden, "forbidden")
	wantError(t, s.do(t, http.MethodGet, "/settings", "", userCat), http.StatusNotFound, "not_found")
}

func TestHandlersFlow(t *testing.T) {
	s := newTestServer(t)

	list := decode[SpecialtyListResponse](t, s.do(t, http.MethodGet, "", "", userViewer), http.StatusOK)
	if len(list.Items) != 3 || list.Items[0].ID != "tex-mex-paste" || list.Items[0].RecipeCount != 2 || list.Items[0].Choice != nil ||
		list.Items[0].ChoiceSource != ChoiceSourceNone {
		t.Fatalf("list = %+v", list.Items)
	}
	tex := list.Items[0]
	if tex.DefaultOptionID != "tex-mex-paste.store" || len(tex.Options) != 2 || !tex.Options[0].IsDefault || tex.Options[0].Per == nil ||
		tex.Options[0].Per.Text != "1 tbsp" || tex.Options[0].Ingredients[0].Text != "2 tsp Smoky Chipotle Bouillon Base" ||
		tex.Options[0].Summary != "1 tbsp = 2 tsp Smoky Chipotle Bouillon Base + 1 tsp Tomato Paste" ||
		tex.Options[1].ShelfLifeDays == nil || *tex.Options[1].ShelfLifeDays != 14 || tex.Options[1].Summary != "Makes about 8 tbsp and keeps 14 days." ||
		len(tex.UnitSizes) != 2 || tex.UnitSizes[0].Text != "2 tbsp" || tex.Batch != nil || tex.Aliases == nil {
		t.Errorf("tex-mex = %+v", tex)
	}
	all := decode[SpecialtyListResponse](t, s.do(t, http.MethodGet, "?all=true", "", testUser), http.StatusOK)
	if len(all.Items) <= 20 {
		t.Errorf("all = %d items", len(all.Items))
	}
	wantError(t, s.do(t, http.MethodGet, "?all=maybe", "", testUser), http.StatusBadRequest, "validation_failed")
	wantError(t, s.do(t, http.MethodGet, "", "", userCat), http.StatusNotFound, "not_found")
	wantError(t, s.do(t, http.MethodGet, "", "", ""), http.StatusUnauthorized, "unauthenticated")

	got := decode[SpecialtyResponse](t, s.do(t, http.MethodGet, "/szechuan-paste", "", userViewer), http.StatusOK)
	if got.ID != "szechuan-paste" || !slices.Equal(got.Aliases, []string{"Sichuan Paste"}) || len(got.IngredientIDs) != 1 {
		t.Errorf("get = %+v", got)
	}
	wantError(t, s.do(t, http.MethodGet, "/nope", "", testUser), http.StatusNotFound, "not_found")

	// Choices.
	wantError(t, s.do(t, http.MethodPut, "/tex-mex-paste/choice", `{"optionId":"as_is"}`, userViewer), http.StatusForbidden, "forbidden")
	wantError(t, s.do(t, http.MethodPut, "/tex-mex-paste/choice", `{"optionId":"nope"}`, testUser), http.StatusBadRequest, "validation_failed")
	wantError(t, s.do(t, http.MethodPut, "/tex-mex-paste/choice", `{"option":"x"}`, testUser), http.StatusBadRequest, "invalid_request")
	chosen := decode[SpecialtyResponse](t, s.do(t, http.MethodPut, "/southwest-spice-blend/choice", `{"optionId":"southwest-spice-blend.batch"}`, testUser), http.StatusOK)
	if c := chosen.Choice; c == nil || c.OptionID != "southwest-spice-blend.batch" || c.Type != "house_made_batch" || c.OptionName == nil ||
		c.Source != ChoiceSourceHousehold || c.Strategy != nil || c.ChosenBy == nil || *c.ChosenBy != testUser || c.ChosenAt == nil ||
		chosen.ChoiceSource != ChoiceSourceHousehold {
		t.Errorf("choice = %+v", chosen.Choice)
	}
	asIs := decode[SpecialtyResponse](t, s.do(t, http.MethodPut, "/tex-mex-paste/choice", `{"optionId":"as_is"}`, testUser), http.StatusOK)
	if c := asIs.Choice; c == nil || c.Type != "as_is" || c.OptionName != nil || c.Source != ChoiceSourceHousehold || c.OptionID != "as_is" {
		t.Errorf("as-is choice = %+v", asIs.Choice)
	}
	if rec := s.do(t, http.MethodDelete, "/tex-mex-paste/choice", "", testUser); rec.Code != http.StatusNoContent {
		t.Errorf("clear = %d %s", rec.Code, rec.Body.String())
	}
	defaults := decode[DefaultsResponse](t, s.do(t, http.MethodPost, "/choices/defaults", "", testUser), http.StatusOK)
	if len(defaults.Items) != 2 || defaults.Skipped != 1 {
		t.Errorf("defaults = %+v", defaults)
	}
	wantError(t, s.do(t, http.MethodPost, "/choices/defaults", "", userViewer), http.StatusForbidden, "forbidden")

	// Household options.
	body := `{"type":"house_made_batch","name":"Mild blend","basedOnOptionId":"southwest-spice-blend.batch",
		"ingredients":[{"name":"Chili Powder","quantity":"1 1/2","unit":"tbsp"},{"name":"Salt"}],
		"steps":["Mix."],"yield":{"quantity":"3","unit":"tbsp"},"shelfLifeDays":90}`
	created := decode[OptionResponse](t, s.do(t, http.MethodPost, "/southwest-spice-blend/options", body, testUser), http.StatusCreated)
	if created.Source != SourceHousehold || created.IsDefault || created.Ingredients[0].Quantity == nil || *created.Ingredients[0].Quantity != "3/2" ||
		created.Ingredients[1].Quantity != nil || created.Ingredients[1].Text != "Salt" || created.BasedOnOptionID == nil || created.CreatedAt == nil || created.Per != nil {
		t.Errorf("created = %+v", created)
	}
	wantError(t, s.do(t, http.MethodPost, "/southwest-spice-blend/options", `{"type":"store_alternative","name":"x","ingredients":[{"name":"Salt","quantity":"1"}]}`, testUser),
		http.StatusBadRequest, "validation_failed")
	wantError(t, s.do(t, http.MethodPost, "/southwest-spice-blend/options", body, userViewer), http.StatusForbidden, "forbidden")
	updated := decode[OptionResponse](t, s.do(t, http.MethodPut, "/southwest-spice-blend/options/"+created.ID,
		`{"type":"store_alternative","name":"Rack mix","per":{"quantity":"1","unit":"tbsp"},"ingredients":[{"name":"Chili Powder","quantity":"2","unit":"tsp"}]}`, testUser), http.StatusOK)
	if updated.ID != created.ID || updated.Type != TypeStoreAlternative || updated.Yield != nil || updated.ShelfLifeDays != nil || len(updated.Steps) != 0 {
		t.Errorf("updated = %+v", updated)
	}
	wantError(t, s.do(t, http.MethodPut, "/tex-mex-paste/options/"+created.ID, `{"type":"store_alternative","name":"Rack mix","per":{"quantity":"1","unit":"tbsp"},"ingredients":[{"name":"Chili Powder","quantity":"2","unit":"tsp"}]}`, testUser),
		http.StatusNotFound, "not_found")
	if rec := s.do(t, http.MethodDelete, "/southwest-spice-blend/options/"+created.ID, "", testUser); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d", rec.Code)
	}
	wantError(t, s.do(t, http.MethodDelete, "/southwest-spice-blend/options/"+created.ID, "", testUser), http.StatusNotFound, "not_found")

	// Batches.
	batch := decode[RecordBatchResponse](t, s.do(t, http.MethodPost, "/southwest-spice-blend/batches", `{"clientPurchaseId":"b1"}`, testUser), http.StatusCreated)
	if batch.Purchase.Source != pantry.PurchaseHouseMade || batch.Purchase.Quantity == nil || *batch.Purchase.Quantity != "4" ||
		batch.Item.Key != "southwest spice blend" || batch.Option.ID != "southwest-spice-blend.batch" {
		t.Errorf("batch = %+v", batch)
	}
	if rec := s.do(t, http.MethodPost, "/southwest-spice-blend/batches", `{"clientPurchaseId":"b1"}`, testUser); rec.Code != http.StatusOK {
		t.Errorf("retried batch = %d", rec.Code)
	}
	wantError(t, s.do(t, http.MethodPost, "/southwest-spice-blend/batches", `{"batches":0}`, testUser), http.StatusBadRequest, "validation_failed")
	wantError(t, s.do(t, http.MethodPost, "/southwest-spice-blend/batches", `{"optionId":"southwest-spice-blend.store"}`, testUser), http.StatusBadRequest, "validation_failed")
	wantError(t, s.do(t, http.MethodPost, "/southwest-spice-blend/batches", `{}`, userViewer), http.StatusForbidden, "forbidden")
	s.f.pantry.err = pantry.ErrConflict
	wantError(t, s.do(t, http.MethodPost, "/southwest-spice-blend/batches", `{}`, testUser), http.StatusConflict, "conflict")
	s.f.pantry.err = errors.New("boom")
	wantError(t, s.do(t, http.MethodPost, "/southwest-spice-blend/batches", `{}`, testUser), http.StatusInternalServerError, "internal")
}
