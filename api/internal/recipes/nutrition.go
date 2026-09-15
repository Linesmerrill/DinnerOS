package recipes

import (
	"math"
	"strings"
)

// NutritionFacts are the headline per-serving values shown on recipe cards.
// A nil field means the source didn't report it.
type NutritionFacts struct {
	// Calories is kilocalories per serving.
	Calories *int
	// ProteinGrams is grams of protein per serving.
	ProteinGrams *int
}

// kilojoulesPerKilocalorie converts energy reported only in kJ.
const kilojoulesPerKilocalorie = 4.184

// FactsOf reads calories and protein from a nutrition list. Names and units
// match case-insensitively: "Calories", "Energy (kcal)", or "Energy" in kcal
// give calories, and energy reported only in kJ ("Energy (kJ)") is converted.
// "Protein" in g (or mg) gives protein. Values are rounded to whole numbers;
// negative amounts are ignored.
func FactsOf(nutrients []Nutrient) NutritionFacts {
	var facts NutritionFacts
	var kilojoules *float64
	for _, n := range nutrients {
		if n.Amount < 0 || math.IsNaN(n.Amount) || math.IsInf(n.Amount, 0) {
			continue
		}
		name, unit := normalizeNutrient(n.Name), normalizeNutrient(n.Unit)
		switch {
		case isKilojoules(name, unit):
			if kilojoules == nil {
				amount := n.Amount
				kilojoules = &amount
			}
		case isCalories(name, unit):
			if facts.Calories == nil {
				facts.Calories = roundedInt(n.Amount)
			}
		case name == "protein" || strings.HasPrefix(name, "protein ("):
			if facts.ProteinGrams != nil {
				continue
			}
			switch unit {
			case "", "g", "gram", "grams":
				facts.ProteinGrams = roundedInt(n.Amount)
			case "mg":
				facts.ProteinGrams = roundedInt(n.Amount / 1000)
			}
		}
	}
	if facts.Calories == nil && kilojoules != nil {
		facts.Calories = roundedInt(*kilojoules / kilojoulesPerKilocalorie)
	}
	return facts
}

// Facts returns the recipe's headline nutrition values.
func (r Recipe) Facts() NutritionFacts { return FactsOf(r.Nutrition) }

// Facts returns the summary's headline nutrition values.
func (s RecipeSummary) Facts() NutritionFacts { return FactsOf(s.Nutrition) }

func normalizeNutrient(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func isKilojoules(name, unit string) bool {
	if unit == "kj" {
		return true
	}
	return unit == "" && (name == "energy (kj)" || name == "kilojoules" || name == "kj")
}

func isCalories(name, unit string) bool {
	switch name {
	case "calories", "calorie", "energy (kcal)", "energy", "kcal", "kilocalories":
		return unit == "" || unit == "kcal" || unit == "cal" || unit == "calories"
	}
	return false
}

func roundedInt(v float64) *int {
	n := int(math.Round(v))
	return &n
}
