package recipes

import (
	"errors"
	"fmt"
	"testing"
)

func TestNutrientText(t *testing.T) {
	for _, tc := range []struct {
		n    Nutrient
		want string
	}{
		{Nutrient{Name: "Calories", Amount: 640, Unit: "kcal"}, "640 Cal"},
		{Nutrient{Name: "Calories", Amount: 640, Unit: "Calories"}, "640 Cal"},
		{Nutrient{Name: "Protein", Amount: 39, Unit: "g"}, "39 g"},
		{Nutrient{Name: "Saturated Fat", Amount: 1.5, Unit: "g"}, "1.5 g"},
		{Nutrient{Name: "Sodium", Amount: 820, Unit: "mg"}, "820 mg"},
		{Nutrient{Name: "Energy (kJ)", Amount: 2678, Unit: "kJ"}, "2678 kJ"},
		{Nutrient{Name: "Fiber", Amount: 4}, "4"},
	} {
		if got := NutrientText(tc.n); got != tc.want {
			t.Errorf("NutrientText(%+v) = %q, want %q", tc.n, got, tc.want)
		}
	}
	if r := NewNutrientResponse(Nutrient{Name: "Calories", Amount: 610, Unit: "kcal"}); r.Text != "610 Cal" || r.Unit != "kcal" {
		t.Errorf("response = %+v; the stored unit stays and the text says Cal", r)
	}
}

func TestPublicMessageReadsLikeASentence(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: Calories can be up to 5000 Cal.", ErrInvalidDraft), "Calories can be up to 5000 Cal."},
		{fmt.Errorf("%w: name is required", ErrInvalidDraft), "Name is required"},
		{fmt.Errorf("%w: line 3 has no name", ErrInvalidImport), "Line 3 has no name"},
		{errors.New("recipes: something else"), "Something else"},
	} {
		if got := publicMessage(tc.err); got != tc.want {
			t.Errorf("publicMessage(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
