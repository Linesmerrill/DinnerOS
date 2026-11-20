package recipes

import (
	"strconv"
	"strings"
)

// NutrientUnitLabel is how the app writes a nutrition unit: "Cal" for
// kilocalories (what a US label calls a Calorie), and metric units as they
// are ("g", "mg", "kJ"). The stored unit is never rewritten.
func NutrientUnitLabel(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "kcal", "cal", "calorie", "calories", "kilocalories":
		return "Cal"
	case "kj":
		return "kJ"
	}
	return strings.TrimSpace(unit)
}

// NutrientText is one nutrition value as the app shows it, number and unit
// with a space between: "640 Cal", "39 g", "1.5 g", "820 mg".
func NutrientText(n Nutrient) string {
	text := strconv.FormatFloat(n.Amount, 'f', -1, 64)
	if label := NutrientUnitLabel(n.Unit); label != "" {
		text += " " + label
	}
	return text
}

// NewNutrientResponse is n as the API sends it, with its display text.
func NewNutrientResponse(n Nutrient) NutrientResponse {
	return NutrientResponse{Name: n.Name, Amount: n.Amount, Unit: n.Unit, Text: NutrientText(n)}
}
