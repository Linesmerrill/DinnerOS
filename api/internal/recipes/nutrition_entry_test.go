package recipes

import (
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Typed nutrition on a recipe the household added (decision 694).

func TestCleanTypedNutritionUsesTheImportNamesAndUnits(t *testing.T) {
	got, problem := cleanTypedNutrition([]Nutrient{
		{Name: " sodium ", Amount: 820, Unit: "MG"},
		{Name: "carbs", Amount: 41.26},
		{Name: "Calories", Amount: 540, Unit: "kcal"},
		{Name: "fiber", Amount: 6},
		{Name: "Saturated Fat", Amount: 4},
		{Name: "protein", Amount: 32, Unit: "g"},
	})
	want := []Nutrient{
		{Name: "Calories", Amount: 540, Unit: "kcal"},
		{Name: "Saturated Fat", Amount: 4, Unit: "g"},
		{Name: "Carbohydrate", Amount: 41.3, Unit: "g"},
		{Name: "Dietary Fiber", Amount: 6, Unit: "g"},
		{Name: "Protein", Amount: 32, Unit: "g"},
		{Name: "Sodium", Amount: 820, Unit: "mg"},
	}
	if problem != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %q; want %+v", got, problem, want)
	}
	if facts := FactsOf(got); *facts.Calories != 540 || *facts.ProteinGrams != 32 {
		t.Errorf("facts = %d kcal, %d g", *facts.Calories, *facts.ProteinGrams)
	}
	if got, problem := cleanTypedNutrition(nil); got != nil || problem != "" {
		t.Errorf("none = %+v, %q", got, problem)
	}
}

func TestCleanTypedNutritionSaysWhatsWrong(t *testing.T) {
	for _, tc := range []struct {
		in   []Nutrient
		want string
	}{
		{[]Nutrient{{Name: "Calories", Amount: 5001}}, "Calories can be up to 5000 kcal."},
		{[]Nutrient{{Name: "Protein", Amount: 1001}}, "Protein can be up to 1000 g."},
		{[]Nutrient{{Name: "Sodium", Amount: 20001}}, "Sodium can be up to 20000 mg."},
		{[]Nutrient{{Name: "Fat", Amount: -1}}, "Fat can't be less than zero."},
		{[]Nutrient{{Name: "Fat", Amount: math.NaN()}}, "Fat can't be less than zero."},
		{[]Nutrient{{Name: "Sodium", Amount: 1, Unit: "g"}}, "Enter sodium in mg."},
		{[]Nutrient{{Name: "Protein", Amount: 1}, {Name: "protein", Amount: 2}}, "You listed protein twice."},
		{[]Nutrient{{Name: "Vitamin C", Amount: 1}}, `"Vitamin C" isn't a nutrition value DinnerOS knows.`},
		{make([]Nutrient, 10), "A recipe can list at most 9 nutrition values."},
	} {
		if _, problem := cleanTypedNutrition(tc.in); !strings.HasPrefix(problem, tc.want) {
			t.Errorf("%+v: %q; want %q", tc.in, problem, tc.want)
		}
	}
}

func TestDraftSavesNutrition(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	rec := srv.do(t, http.MethodPost, recipesPath(hhAda), `{"name":"Chili","servings":4,
		"nutritionPerServing":[{"name":"Protein","amount":30,"unit":"g"},{"name":"Calories","amount":610,"unit":"kcal"}],
		"ingredients":[{"name":"beans","quantity":"2","unit":"can"}],"steps":["Simmer."]}`, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[RecipeResponse](t, rec)
	want := []NutrientResponse{{Name: "Calories", Amount: 610, Unit: "kcal"}, {Name: "Protein", Amount: 30, Unit: "g"}}
	if !reflect.DeepEqual(got.Nutrition, want) || got.Calories == nil || *got.Calories != 610 || *got.ProteinGrams != 30 {
		t.Errorf("nutrition = %+v, calories %v, protein %v", got.Nutrition, got.Calories, got.ProteinGrams)
	}
	list := decodeBody[RecipeListResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda), "", userAda))
	if len(list.Items) != 1 || list.Items[0].Calories == nil || *list.Items[0].Calories != 610 {
		t.Errorf("list = %+v", list.Items)
	}

	rec = srv.do(t, http.MethodPost, recipesPath(hhAda), `{"name":"Too Much","nutritionPerServing":[{"name":"Calories","amount":9000}],
		"ingredients":[{"name":"salt"}],"steps":["Wait."]}`, userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "Calories can be up to 5000 kcal.") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestEditReplacesAndClearsNutrition(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Chili", Nutrition: []NutrientResponse{{Name: "Calories", Amount: 610, Unit: "kcal"}},
		Ingredients: []DraftIngredientWire{{Name: "beans", Quantity: "2", Unit: "can"}}, Steps: []string{"Simmer."},
	}), userAda))
	path := recipesPath(hhAda) + "/" + created.ID + "/edits"

	// Only nutrition: ingredients and steps stay.
	rec := srv.do(t, http.MethodPut, path, `{"nutritionPerServing":[{"name":"calories","amount":480},{"name":"protein","amount":22}]}`, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("nutrition only = %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[RecipeResponse](t, rec)
	if len(got.Nutrition) != 2 || *got.Calories != 480 || *got.ProteinGrams != 22 || len(got.Ingredients) != 1 || len(got.Steps) != 1 {
		t.Errorf("after nutrition = %+v", got)
	}
	list := decodeBody[RecipeListResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda), "", userAda))
	if len(list.Items) != 1 || *list.Items[0].Calories != 480 || *list.Items[0].ProteinGrams != 22 {
		t.Errorf("list after edit = %+v", list.Items)
	}
	// Absent keeps it.
	body := EditRecipeRequest{
		Ingredients: []EditIngredientRequest{{Name: "Beans", Amounts: []EditAmountRequest{{Servings: created.Servings[0], Quantity: "2", Unit: "can"}}}},
		Steps:       []string{"Simmer."},
	}
	if got = decodeBody[RecipeResponse](t, srv.do(t, http.MethodPut, path, mustJSON(t, body), userAda)); len(got.Nutrition) != 2 {
		t.Errorf("absent nutrition = %+v", got.Nutrition)
	}
	// [] clears it.
	rec = srv.do(t, http.MethodPut, path, `{"nutritionPerServing":[]}`, userAda)
	if got = decodeBody[RecipeResponse](t, rec); rec.Code != http.StatusOK || len(got.Nutrition) != 0 || got.Calories != nil || got.ProteinGrams != nil {
		t.Errorf("cleared = %d %+v", rec.Code, got)
	}

	rec = srv.do(t, http.MethodPut, path, `{"nutritionPerServing":[{"name":"Sodium","amount":2,"unit":"g"}]}`, userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "Enter sodium in mg.") {
		t.Errorf("wrong unit body = %s", rec.Body.String())
	}
	if rec := srv.do(t, http.MethodPut, path, `{"nutritionPerServing":[]}`, userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer = %d", rec.Code)
	}
}

func TestEditNutritionOnlyForRecipesTheHouseholdAdded(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, testFile(testRecipe("hf-1", "Garlic Noodles"))), userAda)
	id := recipeIDFor(t, srv, "Garlic Noodles")
	rec := srv.do(t, http.MethodPut, recipesPath(hhAda)+"/"+id+"/edits", `{"nutritionPerServing":[]}`, userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "Only recipes you added can get new nutrition.") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestIntegrationMongoTypedNutrition(t *testing.T) {
	svc, _, _ := newTestMongoService(t)
	stored, err := svc.CreateFromDraft(t.Context(), hhAda, Draft{
		Name: "Chili", Nutrition: []Nutrient{{Name: "Calories", Amount: 610}},
		Ingredients: []DraftIngredient{{Name: "beans", Quantity: "2", Unit: "can"}}, Steps: []string{"Simmer."},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	nutrition := []Nutrient{{Name: "Calories", Amount: 480}, {Name: "Protein", Amount: 22}}
	if _, err := svc.Edit(t.Context(), hhAda, stored.ID, EditInput{Nutrition: &nutrition}); err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(t.Context(), hhAda, ListQuery{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list = %+v, %v", page, err)
	}
	if f := page.Items[0].Facts(); f.Calories == nil || *f.Calories != 480 || f.ProteinGrams == nil || *f.ProteinGrams != 22 {
		t.Errorf("summary facts = %+v", f)
	}
	empty := []Nutrient{}
	if _, err := svc.Edit(t.Context(), hhAda, stored.ID, EditInput{Nutrition: &empty}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(t.Context(), hhAda, stored.ID)
	if err != nil || len(got.Nutrition) != 0 {
		t.Errorf("cleared = %+v, %v", got.Nutrition, err)
	}
}
