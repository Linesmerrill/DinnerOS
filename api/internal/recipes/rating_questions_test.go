package recipes

import (
	"net/http"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

func TestRatingQuestionsAskAboutTheMealAsCooked(t *testing.T) {
	got := ratingQuestions(Instructions{Ingredients: []IngredientState{
		{IngredientKey: "k1", Name: "Ground Pork", SwapName: "Ground Beef"},
		{IngredientKey: "k2", Name: "Brussels Sprouts"},
		{IngredientKey: "k3", Name: "Kosher Salt"},
		{IngredientKey: "k4", Name: "Olive Oil"},
		{IngredientKey: "k5", Name: "Cilantro", LeftOut: &grocery.LeftOut{}},
		{IngredientKey: "k6", Name: "Tex-Mex Paste", Component: &Component{Parts: []string{"2 tsp Tomato Paste", "1 tsp Chicken Stock Concentrate"}}},
		{IngredientKey: "k7", Name: "brussels sprouts"},
	}})
	names := []string{}
	for _, ing := range got.Ingredients {
		names = append(names, ing.Name)
	}
	want := []string{"Ground Beef", "Brussels Sprouts", "Tex-Mex Paste"}
	if len(names) != len(want) {
		t.Fatalf("ingredients = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("ingredient %d = %q, want %q", i, names[i], want[i])
		}
	}
	if parts := got.Ingredients[2].Parts; len(parts) != 2 || parts[0] != "Tomato Paste" || parts[1] != "Chicken Stock Concentrate" {
		t.Errorf("parts = %v", parts)
	}
	if got.Ingredients[0].IngredientKey != "k1" {
		t.Errorf("swap keeps the line's key: %q", got.Ingredients[0].IngredientKey)
	}
	// Every option is a tag ratings accept, and every reason a miss reason.
	if len(got.Categories) != 5 || len(got.Reasons) != 4 || got.Reasons[3].Reason != "product" {
		t.Errorf("categories %d, reasons %+v", len(got.Categories), got.Reasons)
	}
}

func TestRatingQuestionsEndpoint(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, instructionsFixture()), userAda)
	id := recipeIDFor(t, srv, "Gochujang Bowl")
	rec := srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id+"/rating-questions", "", userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody[RatingQuestionsResponse](t, rec)
	if resp.Title == "" || len(resp.Ingredients) == 0 || len(resp.Categories) == 0 {
		t.Errorf("questions = %+v", resp)
	}
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/missing/rating-questions", "", userAda), http.StatusNotFound, "not_found")
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id+"/rating-questions", "", userBob), http.StatusNotFound, "not_found")
}
