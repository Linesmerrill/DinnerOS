package recipes

import (
	"net/http"
	"strings"
	"testing"
)

// An ingredient's note ("minced") is how it's prepared: kept per line, shown
// after the name, and never part of the name (decision 693).

func TestTypedRecipeKeepsIngredientNotes(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Garlic Bread",
		Ingredients: []DraftIngredientWire{
			{Name: "Garlic", Quantity: "2", Unit: "tbsp", Note: "  minced  "},
			{Name: "Butter", Quantity: "4", Unit: "tbsp", Note: "room   temperature"},
			{Name: "Bread", Quantity: "1", Unit: "count"},
		},
		Steps: []string{"Spread and bake."},
	}), userAda))
	got := map[string]string{}
	for _, line := range created.Ingredients {
		got[line.Name] = line.Note
	}
	if got["Garlic"] != "minced" || got["Butter"] != "room temperature" || got["Bread"] != "" {
		t.Errorf("notes = %v", got)
	}
	// Read back, the note is still its own field and the name is untouched.
	again := decodeBody[RecipeResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+created.ID, "", userAda))
	if again.Ingredients[0].Name != "Garlic" || again.Ingredients[0].Note != "minced" {
		t.Errorf("read back = %+v", again.Ingredients[0])
	}
	// A line with no note leaves the field off.
	raw := srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+created.ID, "", userAda).Body.String()
	if strings.Count(raw, `"note"`) != 2 {
		t.Errorf("want two note fields in %s", raw)
	}
}

func TestEditSetsChangesAndClearsANote(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	file := testFile(testRecipe("hf-1", "Garlic Noodles"))
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, file), userAda)
	id := recipeIDFor(t, srv, "Garlic Noodles")
	before := decodeBody[RecipeResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id, "", userAda))
	path := recipesPath(hhAda) + "/" + id + "/edits"

	body := editBody(before.Ingredients[0].IngredientID, "Garlic", "Boil.", "Toss.")
	body.Ingredients[0].Note = " minced "
	got := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPut, path, mustJSON(t, body), userAda))
	if got.Ingredients[0].Note != "minced" || got.Ingredients[0].IngredientID != before.Ingredients[0].IngredientID {
		t.Errorf("after note = %+v", got.Ingredients[0])
	}
	// A re-import keeps the household's edited note.
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, file), userAda)
	after := decodeBody[RecipeResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id, "", userAda))
	if after.Ingredients[0].Note != "minced" {
		t.Errorf("after re-import = %+v", after.Ingredients[0])
	}

	body.Ingredients[0].Note = ""
	got = decodeBody[RecipeResponse](t, srv.do(t, http.MethodPut, path, mustJSON(t, body), userAda))
	if got.Ingredients[0].Note != "" {
		t.Errorf("cleared note = %q", got.Ingredients[0].Note)
	}

	body.Ingredients[1].Note = strings.Repeat("a", MaxIngredientNote+1)
	rec := srv.do(t, http.MethodPut, path, mustJSON(t, body), userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "The note on ingredient 2 can be up to 200 characters.") {
		t.Errorf("long note body = %s", rec.Body.String())
	}
}

func TestImportFileNotesAreTidiedAndCapped(t *testing.T) {
	if got := cleanNote("  finely \n chopped "); got != "finely chopped" {
		t.Errorf("cleanNote = %q", got)
	}
	if got := cleanNote(strings.Repeat("b", 300)); len(got) > MaxIngredientNote {
		t.Errorf("cleanNote kept %d characters", len(got))
	}
}
