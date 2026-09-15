package recipes

import (
	"math"
	"testing"
)

func TestFactsOf(t *testing.T) {
	intPtr := func(n int) *int { return &n }
	tests := []struct {
		name        string
		in          []Nutrient
		wantCal     *int
		wantProtein *int
	}{
		{
			name:        "import names",
			in:          []Nutrient{{Name: "Calories", Amount: 790, Unit: "kcal"}, {Name: "Fat", Amount: 33, Unit: "g"}, {Name: "Protein", Amount: 45, Unit: "g"}},
			wantCal:     intPtr(790),
			wantProtein: intPtr(45),
		},
		{
			name:        "energy kcal, any case, rounded",
			in:          []Nutrient{{Name: "ENERGY (KCAL)", Amount: 689.6, Unit: "KCAL"}, {Name: " protein ", Amount: 35.5, Unit: "G"}},
			wantCal:     intPtr(690),
			wantProtein: intPtr(36),
		},
		{
			name:    "energy in kilojoules only is converted",
			in:      []Nutrient{{Name: "Energy (kJ)", Amount: 2887, Unit: "kJ"}},
			wantCal: intPtr(690),
		},
		{
			name:    "kilocalories win over kilojoules in any order",
			in:      []Nutrient{{Name: "Energy (kJ)", Amount: 1000, Unit: "kJ"}, {Name: "Energy (kcal)", Amount: 600, Unit: "kcal"}},
			wantCal: intPtr(600),
		},
		{
			name:        "protein in milligrams",
			in:          []Nutrient{{Name: "Protein", Amount: 12400, Unit: "mg"}},
			wantProtein: intPtr(12),
		},
		{
			name: "missing, negative, non-finite, or unrelated values are unknown",
			in: []Nutrient{
				{Name: "Calories", Amount: -5, Unit: "kcal"}, {Name: "Protein", Amount: math.NaN(), Unit: "g"},
				{Name: "Protein", Amount: 4, Unit: "oz"}, {Name: "Sodium", Amount: 900, Unit: "mg"},
			},
		},
		{name: "empty", in: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FactsOf(tt.in)
			if !sameInt(got.Calories, tt.wantCal) || !sameInt(got.ProteinGrams, tt.wantProtein) {
				t.Errorf("FactsOf() = {calories %v, protein %v}, want {%v, %v}", show(got.Calories), show(got.ProteinGrams), show(tt.wantCal), show(tt.wantProtein))
			}
		})
	}
}

func sameInt(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func show(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
