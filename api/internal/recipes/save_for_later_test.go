package recipes

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

type fakeSaveForLater struct {
	notes    []SaveForLater
	failWith error
}

func (f *fakeSaveForLater) SaveForLater(context.Context, string, string) ([]SaveForLater, error) {
	return f.notes, f.failWith
}

// The instructions say what to keep for the week's later meals, on the
// recipe ingredient it was bought as (decision 663). A failed lookup leaves
// the notes out and the steps load anyway.
func TestInstructionsSayWhatToSaveForLater(t *testing.T) {
	saves := &fakeSaveForLater{}
	srv := newRecipeTestServer(t, func(o *HandlerOptions) { o.SaveForLater = saves })
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, instructionsFixture()), userAda)
	id := recipeIDFor(t, srv, "Gochujang Bowl")
	path := recipesPath(hhAda) + "/" + id + "/instructions?servings=2"

	first := decodeBody[InstructionsResponse](t, srv.do(t, http.MethodGet, path, "", userViewer))
	if first.SaveForLater == nil || len(first.SaveForLater) != 0 {
		t.Fatalf("saveForLater = %#v, want an empty list", first.SaveForLater)
	}
	var gochujang IngredientStateResponse
	for _, st := range first.Ingredients {
		if st.Name == "Gochujang" {
			gochujang = st
		}
	}
	saves.notes = []SaveForLater{
		{Keys: []string{"other", gochujang.IngredientKey}, Name: "Gochujang Paste", Text: "Use 1 Tbsp. Save 1 Tbsp for Bibimbap on Thursday.", Short: "Save 1 Tbsp for Bibimbap"},
		{Keys: []string{"not-in-recipe"}, Name: "Rice", Text: "Use 1 cup. Save 1 cup for Fried Rice.", Short: "Save 1 cup for Fried Rice"},
	}
	resp := decodeBody[InstructionsResponse](t, srv.do(t, http.MethodGet, path, "", userViewer))
	if len(resp.SaveForLater) != 2 {
		t.Fatalf("saveForLater = %+v", resp.SaveForLater)
	}
	g := resp.SaveForLater[0]
	if g.IngredientIndex == nil || *g.IngredientIndex != gochujang.Index || g.IngredientKey != gochujang.IngredientKey || g.Name != "Gochujang" ||
		g.Short != "Save 1 Tbsp for Bibimbap" || g.Text == "" {
		t.Errorf("gochujang note = %+v, want ingredient %d", g, gochujang.Index)
	}
	if r := resp.SaveForLater[1]; r.IngredientIndex != nil || r.Name != "Rice" {
		t.Errorf("rice note = %+v, want no ingredient", r)
	}

	saves.failWith = errors.New("plan store down")
	rec := srv.do(t, http.MethodGet, path, "", userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := decodeBody[InstructionsResponse](t, rec).SaveForLater; len(got) != 0 {
		t.Errorf("saveForLater after a failure = %+v", got)
	}
}

func TestSameKeyFoldsNamesLikeTheGroceryList(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"66e5", "66e5", true},
		{"name:garlic clove", "name:" + ingredients.SameIngredientKey("garlic clove"), true},
		{"name:garlic", "name:onion", false},
		{"66e5", "name:garlic", false},
	} {
		if got := sameKey(tc.a, tc.b); got != tc.want {
			t.Errorf("sameKey(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// A house-made blend's spices each come back as their own note, and all of
// them land on the blend's line: the cook sees the note once.
func TestSaveForLaterShowsABlendOnce(t *testing.T) {
	text := "Use 1 tsp. Save 1 tsp for Pita Pockets on Monday."
	var notes []SaveForLater
	for _, spice := range []string{"cumin", "oregano", "garlic-powder"} {
		notes = append(notes, SaveForLater{Keys: []string{spice, "cuban-spice-blend"}, PartOf: []string{"cuban-spice-blend"},
			Name: "Cuban Spice Blend", Text: text})
	}
	notes = append(notes, SaveForLater{Keys: []string{"onion"}, Name: "Onion", Text: "Use 1 Onion. Save 1 Onion for Flautas."})
	in := Instructions{Ingredients: []IngredientState{
		{Index: 0, IngredientKey: "onion", Name: "Onion"},
		{Index: 3, IngredientKey: "cuban-spice-blend", Name: "Cuban Spice Blend"},
	}}
	got := saveForLaterResponses(notes, in)
	if len(got) != 2 {
		t.Fatalf("notes = %+v, want the blend once and the onion", got)
	}
	if got[0].Name != "Cuban Spice Blend" || got[0].IngredientIndex == nil || *got[0].IngredientIndex != 3 {
		t.Errorf("blend = %+v", got[0])
	}
	if got[1].Name != "Onion" {
		t.Errorf("onion = %+v", got[1])
	}
	// Different words for the same line both stay.
	notes = append(notes, SaveForLater{Keys: []string{"paprika", "cuban-spice-blend"}, PartOf: []string{"cuban-spice-blend"},
		Name: "Smoked Paprika", Text: "Use 2 tsp."})
	got = saveForLaterResponses(notes, in)
	if len(got) != 3 {
		t.Fatalf("different notes merged: %+v", got)
	}
	// A part of the blend keeps its own name on the blend's line.
	if p := got[2]; p.Name != "Smoked Paprika" || p.IngredientIndex == nil || *p.IngredientIndex != 3 {
		t.Errorf("part = %+v", p)
	}
}
