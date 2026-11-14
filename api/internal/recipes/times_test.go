package recipes

import (
	"net/http"
	"strings"
	"testing"
)

// Prep and cook times on a recipe the household typed (decision 690): the
// app sends prep and total, and an edit can change or clear them.

func TestDraftTimesUpToADay(t *testing.T) {
	srv, _ := newPhotoTestServer(t)
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Smoked Brisket", PrepMinutes: 30, TotalMinutes: 30 + 14*60,
		Ingredients: []DraftIngredientWire{{Name: "brisket", Quantity: "1", Unit: "count"}}, Steps: []string{"Smoke."},
	}), userAda))
	if created.PrepMinutes != 30 || created.TotalMinutes != 870 {
		t.Errorf("times = %d prep, %d total; want 30, 870", created.PrepMinutes, created.TotalMinutes)
	}
	for _, tc := range []struct {
		prep, total int
		want        string
	}{
		{-5, 10, "Times can't be less than zero."},
		{25 * 60, 25 * 60, "Prep time can be up to 24 hours."},
		{60, 49 * 60, "Cook time can be up to 24 hours."},
	} {
		rec := srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
			Name: "Too Long", PrepMinutes: tc.prep, TotalMinutes: tc.total,
			Ingredients: []DraftIngredientWire{{Name: "salt"}}, Steps: []string{"Wait."},
		}), userAda)
		wantError(t, rec, http.StatusBadRequest, "validation_failed")
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%d/%d body = %s; want %q", tc.prep, tc.total, rec.Body.String(), tc.want)
		}
	}
}

func TestEditChangesAndClearsTimes(t *testing.T) {
	srv, _ := newPhotoTestServer(t)
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Chili", TotalMinutes: 40,
		Ingredients: []DraftIngredientWire{{Name: "beans", Quantity: "2", Unit: "can"}}, Steps: []string{"Simmer."},
	}), userAda))
	path := recipesPath(hhAda) + "/" + created.ID + "/edits"

	// Only the times: ingredients and steps stay.
	rec := srv.do(t, http.MethodPut, path, `{"prepMinutes":15,"totalMinutes":255}`, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("times only = %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[RecipeResponse](t, rec)
	if got.PrepMinutes != 15 || got.TotalMinutes != 255 || len(got.Ingredients) != 1 || len(got.Steps) != 1 {
		t.Errorf("after times = %+v", got)
	}
	// With the rest of the edit; absent times stay, 0 clears one.
	zero := 0
	body := EditRecipeRequest{
		Ingredients: []EditIngredientRequest{{Name: "Beans", Amounts: []EditAmountRequest{{Servings: created.Servings[0], Quantity: "2", Unit: "can"}}}},
		Steps:       []string{"Simmer."},
	}
	got = decodeBody[RecipeResponse](t, srv.do(t, http.MethodPut, path, mustJSON(t, body), userAda))
	if got.PrepMinutes != 15 || got.TotalMinutes != 255 {
		t.Errorf("absent times = %d/%d; want 15/255", got.PrepMinutes, got.TotalMinutes)
	}
	body.PrepMinutes = &zero
	got = decodeBody[RecipeResponse](t, srv.do(t, http.MethodPut, path, mustJSON(t, body), userAda))
	if got.PrepMinutes != 0 || got.TotalMinutes != 255 {
		t.Errorf("cleared prep = %d/%d; want 0/255", got.PrepMinutes, got.TotalMinutes)
	}

	rec = srv.do(t, http.MethodPut, path, `{"prepMinutes":1500}`, userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "Prep time can be up to 24 hours.") {
		t.Errorf("too long body = %s", rec.Body.String())
	}
}

func TestEditTimesOnlyForRecipesTheHouseholdAdded(t *testing.T) {
	srv, _ := newPhotoTestServer(t)
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, testFile(testRecipe("hf-1", "Garlic Noodles"))), userAda)
	id := recipeIDFor(t, srv, "Garlic Noodles")
	rec := srv.do(t, http.MethodPut, recipesPath(hhAda)+"/"+id+"/edits", `{"totalMinutes":30}`, userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "Only recipes you added can get new times.") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestEditRenamesARecipeTheHouseholdAdded(t *testing.T) {
	srv, _ := newPhotoTestServer(t)
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "no bake cookies", Ingredients: []DraftIngredientWire{{Name: "oats"}}, Steps: []string{"Stir."},
	}), userAda))
	path := recipesPath(hhAda) + "/" + created.ID + "/edits"
	rec := srv.do(t, http.MethodPut, path, `{"name":"  No Bake   Cookies "}`, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename = %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[RecipeResponse](t, rec)
	if got.Name != "No Bake Cookies" || len(got.Ingredients) != 1 {
		t.Errorf("after rename = %q with %d ingredients", got.Name, len(got.Ingredients))
	}
	rec = srv.do(t, http.MethodPut, path, `{"name":"   "}`, userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "A recipe needs a name.") {
		t.Errorf("blank name body = %s", rec.Body.String())
	}

	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, testFile(testRecipe("hf-1", "Garlic Noodles"))), userAda)
	id := recipeIDFor(t, srv, "Garlic Noodles")
	rec = srv.do(t, http.MethodPut, recipesPath(hhAda)+"/"+id+"/edits", `{"name":"Mine"}`, userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "Only recipes you added can be renamed.") {
		t.Errorf("meal kit rename body = %s", rec.Body.String())
	}
}
