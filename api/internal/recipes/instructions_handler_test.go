package recipes

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// fakeSpecialties answers with one specialty ingredient per line key, or an
// error when failWith is set.
type fakeSpecialties struct {
	specs    grocery.Specialties
	failWith error
	// lines records what the handler asked about.
	lines []grocery.Line
}

func (f *fakeSpecialties) GrocerySpecialties(_ context.Context, _ string, lines []grocery.Line) (grocery.Specialties, error) {
	f.lines = lines
	if f.failWith != nil {
		return nil, f.failWith
	}
	out := grocery.Specialties{}
	for _, l := range lines {
		if spec, ok := f.specs[l.Name]; ok {
			out[l.IngredientKey] = spec
		}
	}
	return out, nil
}

// instructionsFixture is a recipe whose one step names a specialty
// ingredient and a spicy one.
func instructionsFixture() ImportFile {
	r := testRecipe("r-bowl", "Gochujang Bowl")
	r.Ingredients = append(r.Ingredients,
		ImportIngredient{SourceIngredientID: "ing-gochujang", Name: "Gochujang", Amounts: []ImportAmount{
			{Servings: 2, Quantity: qty(1), Unit: "tbsp", SourceUnit: "tablespoon", RawText: "1 tbsp Gochujang"},
			{Servings: 4, Quantity: qty(2), Unit: "tbsp", SourceUnit: "tablespoon", RawText: "2 tbsp Gochujang"},
		}},
		ImportIngredient{SourceIngredientID: "ing-texmex", Name: "Tex-Mex Paste", Amounts: []ImportAmount{
			{Servings: 2, Quantity: qty(1), Unit: "tbsp", SourceUnit: "tablespoon", RawText: "1 tbsp Tex-Mex Paste"},
			{Servings: 4, Quantity: qty(2), Unit: "tbsp", SourceUnit: "tablespoon", RawText: "2 tbsp Tex-Mex Paste"},
		}})
	r.Steps = []ImportStep{{Index: 1, Text: "Whisk the gochujang with the Tex-Mex Paste."}}
	return testFile(r)
}

func instructionsSpecialty(t *testing.T) *grocery.Specialty {
	t.Helper()
	q := func(s string) ingredients.Quantity {
		parsed, err := ingredients.ParseQuantity(s)
		if err != nil {
			t.Fatalf("ParseQuantity(%q): %v", s, err)
		}
		return parsed
	}
	one := q("1")
	return &grocery.Specialty{
		ID: "tex-mex-paste", Key: "tex mex paste", Name: "Tex-Mex Paste",
		Choice: &grocery.Choice{
			Type: grocery.ChoiceStoreAlternative, OptionID: "opt", OptionName: "Chipotle tomato base",
			Per:        grocery.Measure{Quantity: one, Unit: "tbsp"},
			Components: []grocery.Component{{Name: "Tomato Paste", Quantity: &one, Unit: "tbsp"}},
		},
	}
}

func recipeIDFor(t *testing.T, srv *recipeTestServer, name string) string {
	t.Helper()
	list := decodeBody[RecipeListResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda), "", userAda))
	for _, item := range list.Items {
		if item.Name == name {
			return item.ID
		}
	}
	t.Fatalf("no recipe named %q in %+v", name, list.Items)
	return ""
}

func TestInstructionsAppliesSpecialtyChoicesAndScalesAmounts(t *testing.T) {
	specs := &fakeSpecialties{specs: grocery.Specialties{"Tex-Mex Paste": instructionsSpecialty(t)}}
	srv := newRecipeTestServer(t, func(o *HandlerOptions) { o.Specialties = specs })
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, instructionsFixture()), userAda)
	id := recipeIDFor(t, srv, "Gochujang Bowl")

	rec := srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id+"/instructions?servings=4", "", userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody[InstructionsResponse](t, rec)
	if resp.Servings != 4 || !resp.SpecialtiesApplied || len(resp.Steps) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if want := "Whisk 2 tbsp gochujang with 2 tbsp Tomato Paste."; resp.Steps[0].Text != want {
		t.Errorf("step text = %q, want %q", resp.Steps[0].Text, want)
	}
	if resp.Steps[0].OriginalText == "" {
		t.Error("originalText is empty; a rewritten step keeps the card's wording")
	}
	var spicy, substituted int
	var joinedText string
	for _, seg := range resp.Steps[0].Segments {
		joinedText += seg.Text
		if seg.Spicy {
			spicy++
		}
		if seg.Substituted {
			substituted++
		}
	}
	if joinedText != resp.Steps[0].Text {
		t.Errorf("segments join to %q, want %q", joinedText, resp.Steps[0].Text)
	}
	if spicy != 1 || substituted != 1 {
		t.Errorf("spicy = %d, substituted = %d, want 1 and 1", spicy, substituted)
	}
	if len(resp.Substitutions) != 1 || resp.Substitutions[0].SpecialtyID != "tex-mex-paste" {
		t.Errorf("substitutions = %+v", resp.Substitutions)
	}
	// The resolver is asked about the same lines the grocery list would send.
	if len(specs.lines) != 4 {
		t.Errorf("lines = %+v, want one per ingredient", specs.lines)
	}
}

func TestInstructionsDefaultServingsAndValidation(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, instructionsFixture()), userAda)
	id := recipeIDFor(t, srv, "Gochujang Bowl")

	resp := decodeBody[InstructionsResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id+"/instructions", "", userAda))
	if resp.Servings != 2 || resp.SpecialtiesApplied {
		t.Errorf("response = %+v, want the smallest size and specialties not applied", resp)
	}
	if len(resp.Substitutions) != 0 || len(resp.UnchosenSpecialties) != 0 {
		t.Errorf("arrays = %+v / %+v, want both empty rather than null", resp.Substitutions, resp.UnchosenSpecialties)
	}
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id+"/instructions?servings=3", "", userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/missing/instructions", "", userAda), http.StatusNotFound, "not_found")
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id+"/instructions", "", userBob), http.StatusNotFound, "not_found")
}

func TestInstructionsFailWhenChoicesCannotBeRead(t *testing.T) {
	specs := &fakeSpecialties{failWith: errors.New("mongo is down")}
	srv := newRecipeTestServer(t, func(o *HandlerOptions) { o.Specialties = specs })
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, instructionsFixture()), userAda)
	id := recipeIDFor(t, srv, "Gochujang Bowl")
	// Reading a step that names the ingredient the household replaced is
	// worse than an error.
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id+"/instructions", "", userAda), http.StatusInternalServerError, "internal")
}
