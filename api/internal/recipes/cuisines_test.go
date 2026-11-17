package recipes

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// Recipe names here are made up; none come from a real recipe.

// A household can set the cuisine on any recipe, a meal kit one included.
// It's stored canonical, shown with its label, and kept through a re-import
// that says otherwise; a cuisine alone doesn't mark the recipe edited, so
// the kit's fixes to ingredients and steps still come through (decision 720).
func TestEditRecipeCuisines(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	file := testFile(testRecipe("hf-1", "Garlic Noodles"))
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, file), userAda)
	id := recipeIDFor(t, srv, "Garlic Noodles")
	path := recipesPath(hhAda) + "/" + id + "/edits"

	before := decodeBody[RecipeResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id, "", userAda))
	if !slices.Equal(before.CuisineValues, []string{"mexican"}) || !slices.Equal(before.CuisineLabels, []string{"Mexican"}) {
		t.Fatalf("before = %v %v", before.CuisineValues, before.CuisineLabels)
	}

	picked := []string{" Middle East", "italian", "Italian"}
	rec := srv.do(t, http.MethodPut, path, mustJSON(t, EditRecipeRequest{Cuisines: &picked}), userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[RecipeResponse](t, rec)
	if !slices.Equal(got.Cuisines, []string{"middle eastern", "italian"}) ||
		!slices.Equal(got.CuisineLabels, []string{"Middle Eastern", "Italian"}) {
		t.Errorf("edited = %v %v", got.Cuisines, got.CuisineLabels)
	}
	if got.EditedAt != nil {
		t.Errorf("a cuisine alone marked the recipe edited")
	}

	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, file), userAda)
	after := decodeBody[RecipeResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id, "", userAda))
	if !slices.Equal(after.CuisineValues, []string{"middle eastern", "italian"}) {
		t.Errorf("after re-import = %v", after.CuisineValues)
	}

	// None is a choice too: no guess comes back.
	none := []string{}
	got = decodeBody[RecipeResponse](t, srv.do(t, http.MethodPut, path, mustJSON(t, EditRecipeRequest{Cuisines: &none}), userAda))
	if len(got.CuisineValues) != 0 || got.CuisineLabels == nil {
		t.Errorf("none = %v %v", got.CuisineValues, got.CuisineLabels)
	}

	for name, values := range map[string][]string{
		"too many": {"thai", "greek", "indian", "korean", "french", "german"},
		"too long": {strings.Repeat("x", 61)},
	} {
		rec := srv.do(t, http.MethodPut, path, mustJSON(t, EditRecipeRequest{Cuisines: &values}), userAda)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d", name, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, "canonical") || strings.Contains(body, "—") {
			t.Errorf("%s: message reads like code: %s", name, body)
		}
	}
	if rec := srv.do(t, http.MethodPut, path, mustJSON(t, EditRecipeRequest{Cuisines: &picked}), userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer status = %d", rec.Code)
	}
}

// A typed recipe with no cuisine shows the one its title names, read fresh
// rather than stored over; one the member picked, even none, is kept, and a
// recipe from a page keeps its cuisine too (decisions 720 and 721).
func TestDraftCuisines(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	line := []DraftIngredientWire{{Name: "Pork Shoulder", Quantity: "2", Unit: "lb", RawText: "2 lb pork shoulder"}}

	// An older app sends no choice: the title's cuisine fills in.
	rec := srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{Name: "Carnitas Tacos", Servings: 4, Ingredients: line, Steps: []string{"Cook."}}), userAda)
	got := decodeBody[RecipeResponse](t, rec)
	if !slices.Equal(got.CuisineValues, []string{"mexican"}) || !slices.Equal(got.CuisineLabels, []string{"Mexican"}) {
		t.Errorf("guessed = %v %v (status %d)", got.CuisineValues, got.CuisineLabels, rec.Code)
	}

	// A pick of none stays none.
	rec = srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{Name: "Carnitas Bowls", Servings: 4, Ingredients: line, Steps: []string{"Cook."}, Cuisines: []string{}, CuisinesChosen: true}), userAda)
	if got := decodeBody[RecipeResponse](t, rec); len(got.CuisineValues) != 0 {
		t.Errorf("picked none = %v", got.CuisineValues)
	}

	// A page's recipe keeps the cuisine the member saw.
	page := DraftResponse{
		Name: "Weeknight Chili", Servings: 4, SourceURL: "https://recipes.example.com/chili", FromURL: true,
		Ingredients: line, Cuisines: []string{"Tex Mex"}, CuisinesChosen: true,
	}
	rec = srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, page), userAda)
	if got := decodeBody[RecipeResponse](t, rec); !slices.Equal(got.Cuisines, []string{"tex-mex"}) {
		t.Errorf("page recipe cuisines = %v (status %d, %s)", got.Cuisines, rec.Code, rec.Body.String())
	}

	// A parsed draft offers a guess to start from.
	text := "Pesto Penne\n\nIngredients\n8 oz penne\n1/2 cup pesto\n\nInstructions\nBoil the penne.\nToss with pesto."
	rec = srv.do(t, http.MethodPost, recipesPath(hhAda)+"/parse", mustJSON(t, ParseRecipeRequest{Text: text}), userAda)
	if d := decodeBody[DraftResponse](t, rec); !slices.Equal(d.Cuisines, []string{"italian"}) {
		t.Errorf("parsed draft cuisines = %v (name %q)", d.Cuisines, d.Name)
	}
}

// Add Recipe asks for a guess while the member types a title; a viewer may
// ask, since nothing is stored.
func TestCuisineGuessRoute(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	path := recipesPath(hhAda) + "/cuisine-guess"
	rec := srv.do(t, http.MethodPost, path, mustJSON(t, CuisineGuessRequest{Name: "QA Carnitas Tacos 1234"}), userViewer)
	got := decodeBody[CuisineGuessResponse](t, rec)
	if rec.Code != http.StatusOK || !slices.Equal(got.Cuisines, []string{"mexican"}) || !slices.Equal(got.Labels, []string{"Mexican"}) {
		t.Errorf("guess = %d %+v", rec.Code, got)
	}
	rec = srv.do(t, http.MethodPost, path, mustJSON(t, CuisineGuessRequest{Name: "Beef Stew"}), userAda)
	if got := decodeBody[CuisineGuessResponse](t, rec); got.Cuisines == nil || len(got.Cuisines) != 0 || got.Labels == nil {
		t.Errorf("no guess = %+v", got)
	}
}

// A stored recipe with no cuisine gets the guess only when read; one the
// household cleared stays clear.
func TestShownCuisines(t *testing.T) {
	r := Recipe{Name: "Chicken Tikka Masala"}
	if got := r.ShownCuisines(); !slices.Equal(got, []string{"indian"}) {
		t.Errorf("guess = %v", got)
	}
	r.CuisinesChosen = true
	if got := r.ShownCuisines(); got != nil {
		t.Errorf("chosen none = %v", got)
	}
	r = Recipe{Name: "Chicken Tikka Masala", Cuisines: []string{"South Asia"}}
	if got := r.ShownCuisines(); !slices.Equal(got, []string{"south asian"}) {
		t.Errorf("own = %v", got)
	}
}

// mergeStored keeps a household's cuisine pick over the file's.
func TestMergeStoredKeepsChosenCuisines(t *testing.T) {
	stored := Recipe{ID: "r1", Cuisines: []string{"italian"}, CuisinesChosen: true, CreatedAt: time.Unix(1, 0)}
	got := mergeStored(Recipe{Cuisines: []string{"Mexican"}}, stored, nil)
	if !got.CuisinesChosen || !slices.Equal(got.Cuisines, []string{"italian"}) {
		t.Errorf("merged = %v %v", got.Cuisines, got.CuisinesChosen)
	}
	got = mergeStored(Recipe{Cuisines: []string{"Mexican"}}, Recipe{ID: "r1", Cuisines: []string{"thai"}}, nil)
	if got.CuisinesChosen || !slices.Equal(got.Cuisines, []string{"Mexican"}) {
		t.Errorf("unchosen merged = %v %v", got.Cuisines, got.CuisinesChosen)
	}
}
