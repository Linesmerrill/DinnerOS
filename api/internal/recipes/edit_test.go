package recipes

import (
	"net/http"
	"strings"
	"testing"
)

func editBody(garlicID, garlicName string, steps ...string) EditRecipeRequest {
	return EditRecipeRequest{
		Ingredients: []EditIngredientRequest{
			{IngredientID: garlicID, Name: garlicName, Amounts: []EditAmountRequest{
				{Servings: 2, Quantity: "2", Unit: "clove"}, {Servings: 4, Quantity: "4", Unit: "clove"},
			}},
			{Name: "Lo Mein Noodles", Amounts: []EditAmountRequest{
				{Servings: 4, Quantity: "12", Unit: "oz"}, {Servings: 2, Quantity: "6", Unit: "oz"},
			}},
		},
		Steps: steps,
	}
}

// A member swaps in lo mein and rewrites a step: the recipe reads that way
// from then on, says it was edited, and a re-import of the meal kit recipe
// keeps the household's version (decision 667).
func TestEditRecipeSavesAndOutlastsReimport(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	file := testFile(testRecipe("hf-1", "Garlic Noodles"))
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, file), userAda)
	id := recipeIDFor(t, srv, "Garlic Noodles")
	before := decodeBody[RecipeResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id, "", userAda))
	if before.EditedAt != nil {
		t.Fatalf("editedAt before an edit = %v", before.EditedAt)
	}
	garlicID := before.Ingredients[0].IngredientID

	body := mustJSON(t, editBody(garlicID, "Garlic", "Boil lo mein noodles, 6 minutes.", "  ", "Toss with garlic."))
	rec := srv.do(t, http.MethodPut, recipesPath(hhAda)+"/"+id+"/edits", body, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[RecipeResponse](t, rec)
	if got.EditedAt == nil || len(got.Ingredients) != 2 || len(got.Steps) != 2 {
		t.Fatalf("edited = %+v", got)
	}
	if got.Ingredients[0].IngredientID != garlicID {
		t.Errorf("garlic kept? %s vs %s", got.Ingredients[0].IngredientID, garlicID)
	}
	noodles := got.Ingredients[1]
	if noodles.Name != "Lo Mein Noodles" || noodles.IngredientID == "" || len(noodles.Amounts) != 2 ||
		noodles.Amounts[0].Servings != 2 || noodles.Amounts[0].Quantity == nil || *noodles.Amounts[0].Quantity != "6" {
		t.Errorf("noodles = %+v", noodles)
	}
	if got.Steps[0].Text != "Boil lo mein noodles, 6 minutes." || got.Steps[1].Index != 2 {
		t.Errorf("steps = %+v", got.Steps)
	}

	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, file), userAda)
	after := decodeBody[RecipeResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id, "", userAda))
	if len(after.Ingredients) != 2 || after.Ingredients[1].Name != "Lo Mein Noodles" || after.Steps[0].Text != got.Steps[0].Text ||
		after.EditedAt == nil {
		t.Errorf("after re-import = %+v", after)
	}
}

func TestEditRecipeRejectsWhatItCantSave(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, testFile(testRecipe("hf-1", "Garlic Noodles"))), userAda)
	id := recipeIDFor(t, srv, "Garlic Noodles")
	path := recipesPath(hhAda) + "/" + id + "/edits"

	if rec := srv.do(t, http.MethodPut, path, mustJSON(t, editBody("", "Garlic", "Cook.")), userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer status = %d", rec.Code)
	}
	if rec := srv.do(t, http.MethodPut, recipesPath(hhAda)+"/66e5a1f2c3b4a5d6e7f8ffff/edits", mustJSON(t, editBody("", "Garlic", "Cook.")), userAda); rec.Code != http.StatusNotFound {
		t.Errorf("missing recipe status = %d", rec.Code)
	}
	threeServings := editBody("", "Garlic", "Cook.")
	threeServings.Ingredients[0].Amounts[0].Servings = 3
	badUnit := editBody("", "Garlic", "Cook.")
	badUnit.Ingredients[1].Amounts[0].Unit = "handfuls"
	for name, req := range map[string]EditRecipeRequest{
		"no steps":       editBody("", "Garlic"),
		"no name":        editBody("", "  ", "Cook."),
		"three servings": threeServings,
		"bad unit":       badUnit,
		"no ingredients": {Steps: []string{"Cook."}},
	} {
		rec := srv.do(t, http.MethodPut, path, mustJSON(t, req), userAda)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", name, rec.Code)
			continue
		}
		if msg := rec.Body.String(); strings.Contains(msg, "recipes:") || strings.Contains(msg, "invalid edit") || !strings.Contains(msg, ".") {
			t.Errorf("%s: message = %s", name, msg)
		}
	}
}

func TestSwapPreviewRoute(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	body := mustJSON(t, SwapPreviewRequest{
		OldName: "Ramen Noodles", NewName: "Lo Mein Noodles",
		Steps: []string{"Boil ramen noodles until tender, 3 minutes."},
	})
	rec := srv.do(t, http.MethodPost, recipesPath(hhAda)+"/swap-preview", body, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[SwapPreviewResponse](t, rec)
	if len(got.Steps) != 1 || got.Steps[0].Text != "Boil lo mein noodles until tender, 3 minutes." || !got.Steps[0].Changed {
		t.Errorf("steps = %+v", got.Steps)
	}
	if got.CookTime == nil || got.CookTime.SuggestedMinutes != 6 || got.CookTime.Text != "Lo mein noodles usually cook 4 to 6 minutes." {
		t.Errorf("cook time = %+v", got.CookTime)
	}
	if len(got.Times) != 1 || got.Times[0].Minutes != 3 {
		t.Errorf("times = %+v", got.Times)
	}
	if rec := srv.do(t, http.MethodPost, recipesPath(hhAda)+"/swap-preview", body, userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer status = %d", rec.Code)
	}
}
