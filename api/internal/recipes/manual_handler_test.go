package recipes

import (
	"net/http"
	"strings"
	"testing"
)

func TestParseEndpointRequiresRecipesEdit(t *testing.T) {
	s := newRecipeTestServer(t, nil)
	body := mustJSON(t, ParseRecipeRequest{Text: "Chili\nIngredients\n- 1 cup beans\nSteps\n1. Cook"})
	path := recipesPath(hhAda) + "/parse"

	if rec := s.do(t, http.MethodPost, path, body, userViewer); rec.Code != http.StatusForbidden {
		t.Fatalf("a viewer got %d; want 403 (%s)", rec.Code, rec.Body.String())
	}
	rec := s.do(t, http.MethodPost, path, body, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (%s)", rec.Code, rec.Body.String())
	}
	resp := decodeBody[DraftResponse](t, rec)
	if resp.Name != "Chili" || len(resp.Ingredients) != 1 || len(resp.Steps) != 1 {
		t.Fatalf("draft = %+v", resp)
	}
	if resp.FromURL {
		t.Error("a pasted draft is marked as coming from a URL")
	}
	if len(s.store.recipes) != 0 {
		t.Error("parsing stored a recipe")
	}
}

func TestParseEndpointRejectsBadRequests(t *testing.T) {
	s := newRecipeTestServer(t, nil)
	path := recipesPath(hhAda) + "/parse"
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"neither text nor url", mustJSON(t, ParseRecipeRequest{}), http.StatusBadRequest},
		{"both text and url", mustJSON(t, ParseRecipeRequest{Text: "a", URL: "https://example.com"}), http.StatusBadRequest},
		{"a private address", mustJSON(t, ParseRecipeRequest{URL: "http://169.254.169.254/"}), http.StatusUnprocessableEntity},
		{"a non-web scheme", mustJSON(t, ParseRecipeRequest{URL: "file:///etc/passwd"}), http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.do(t, http.MethodPost, path, tc.body, userAda)
			if rec.Code != tc.want {
				t.Fatalf("status = %d; want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestCreateRecipeEndpointStoresAPrivateRecipe(t *testing.T) {
	s := newRecipeTestServer(t, nil)
	draft := DraftResponse{
		Name: "Grandma's Chili", Servings: 4, TotalMinutes: 45,
		Ingredients: []DraftIngredientWire{{Name: "kidney beans", Quantity: "2", Unit: "can"}},
		Steps:       []string{"Simmer everything."},
	}
	rec := s.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, draft), userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201 (%s)", rec.Code, rec.Body.String())
	}
	resp := decodeBody[RecipeResponse](t, rec)
	if resp.Source != SourceManual || resp.Name != "Grandma's Chili" {
		t.Fatalf("stored %+v", resp)
	}
	if len(resp.Ingredients) != 1 || resp.Ingredients[0].Amounts[0].Unit != "can" {
		t.Fatalf("ingredients = %+v", resp.Ingredients)
	}
	if rec := s.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, draft), userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("a viewer created a recipe: %d", rec.Code)
	}
}

func TestCreateRecipeEndpointRejectsAnUnusableDraft(t *testing.T) {
	s := newRecipeTestServer(t, nil)
	rec := s.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{Name: "  "}), userAda)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 (%s)", rec.Code, rec.Body.String())
	}
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestSharingEndpointTogglesTheOptIn(t *testing.T) {
	s := newRecipeTestServer(t, nil)
	created := s.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Grandma's Chili", Ingredients: []DraftIngredientWire{{Name: "beans"}},
	}), userAda)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d (%s)", created.Code, created.Body.String())
	}
	id := decodeBody[RecipeResponse](t, created).ID
	path := recipesPath(hhAda) + "/" + id + "/sharing"

	rec := s.do(t, http.MethodPut, path, mustJSON(t, SharingRequest{SharedToCatalog: true}), userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	resp := decodeBody[SharingResponse](t, rec)
	if !resp.SharedToCatalog || !resp.InCatalog {
		t.Fatalf("response = %+v; want shared and in the catalog", resp)
	}
	off := decodeBody[SharingResponse](t, s.do(t, http.MethodPut, path, mustJSON(t, SharingRequest{}), userAda))
	if off.SharedToCatalog || off.InCatalog {
		t.Fatalf("response = %+v; want the opt-in off", off)
	}
	if rec := s.do(t, http.MethodPut, path, mustJSON(t, SharingRequest{SharedToCatalog: true}), userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("a viewer changed sharing: %d", rec.Code)
	}
	if rec := s.do(t, http.MethodPut, recipesPath(hhAda)+"/66e5a1f2c3b4a5d6e7f8dead/sharing", mustJSON(t, SharingRequest{}), userAda); rec.Code != http.StatusNotFound {
		t.Errorf("sharing an unknown recipe = %d; want 404", rec.Code)
	}
}

func TestRecipeListStillWorksAlongsideTheNewRoutes(t *testing.T) {
	s := newRecipeTestServer(t, nil)
	rec := s.do(t, http.MethodGet, recipesPath(hhAda), "", userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) == "" {
		t.Error("list returned an empty body")
	}
}
