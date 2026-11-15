package recipes

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Nutrition a member types for a recipe the household added (decision 694):
// per serving, in the same {name, amount, unit} list imports carry, so the
// recipe page, the cards' calories and protein, and the app's label order all
// read it the same way.

// typedNutrient is one value a member can enter: its name as stored, its
// unit, and the most it can be per serving.
type typedNutrient struct {
	name string
	unit string
	max  float64
}

// typedNutrients are in label order, with the names meal kit imports use.
var typedNutrients = []typedNutrient{
	{"Calories", "kcal", 5000},
	{"Fat", "g", 1000},
	{"Saturated Fat", "g", 1000},
	{"Carbohydrate", "g", 1000},
	{"Sugar", "g", 1000},
	{"Dietary Fiber", "g", 1000},
	{"Protein", "g", 1000},
	{"Cholesterol", "mg", 20000},
	{"Sodium", "mg", 20000},
}

// nutrientAliases maps other common spellings to a typed nutrient's name.
var nutrientAliases = map[string]string{
	"calories": "Calories", "calorie": "Calories", "energy": "Calories", "energy (kcal)": "Calories", "kcal": "Calories",
	"fat": "Fat", "total fat": "Fat",
	"saturated fat": "Saturated Fat", "saturatedfat": "Saturated Fat", "saturated": "Saturated Fat",
	"carbohydrate": "Carbohydrate", "carbohydrates": "Carbohydrate", "carbs": "Carbohydrate", "total carbohydrate": "Carbohydrate",
	"sugar": "Sugar", "sugars": "Sugar", "total sugar": "Sugar",
	"dietary fiber": "Dietary Fiber", "fiber": "Dietary Fiber", "fibre": "Dietary Fiber", "dietaryfiber": "Dietary Fiber",
	"protein":     "Protein",
	"cholesterol": "Cholesterol",
	"sodium":      "Sodium", "salt": "Sodium",
}

// maxTypedNutrients allows each typed nutrient once.
var maxTypedNutrients = len(typedNutrients)

func typedNutrientNamed(name string) (typedNutrient, bool) {
	canonical, ok := nutrientAliases[normalizeNutrient(name)]
	if !ok {
		return typedNutrient{}, false
	}
	for _, t := range typedNutrients {
		if t.name == canonical {
			return t, true
		}
	}
	return typedNutrient{}, false
}

// cleanTypedNutrition checks typed nutrition and returns it in label order
// with each value's own name and unit, or a sentence saying what's wrong.
// An empty list is no nutrition.
func cleanTypedNutrition(in []Nutrient) ([]Nutrient, string) {
	if len(in) == 0 {
		return nil, ""
	}
	if len(in) > maxTypedNutrients {
		return nil, fmt.Sprintf("A recipe can list at most %d nutrition values.", maxTypedNutrients)
	}
	byName := map[string]Nutrient{}
	for _, n := range in {
		t, ok := typedNutrientNamed(n.Name)
		if !ok {
			return nil, fmt.Sprintf("%q isn't a nutrition value DinnerOS knows. Use calories, fat, saturated fat, carbohydrate, sugar, dietary fiber, protein, cholesterol, or sodium.", strings.TrimSpace(n.Name))
		}
		if _, dup := byName[t.name]; dup {
			return nil, "You listed " + strings.ToLower(t.name) + " twice."
		}
		if u := normalizeNutrient(n.Unit); u != "" && u != t.unit {
			return nil, fmt.Sprintf("Enter %s in %s.", strings.ToLower(t.name), t.unit)
		}
		switch {
		case math.IsNaN(n.Amount) || math.IsInf(n.Amount, 0) || n.Amount < 0:
			return nil, t.name + " can't be less than zero."
		case n.Amount > t.max:
			return nil, fmt.Sprintf("%s can be up to %s %s.", t.name, strconv.FormatFloat(t.max, 'f', -1, 64), t.unit)
		}
		// One decimal place is as precise as a label gets.
		byName[t.name] = Nutrient{Name: t.name, Amount: math.Round(n.Amount*10) / 10, Unit: t.unit}
	}
	out := make([]Nutrient, 0, len(byName))
	for _, t := range typedNutrients {
		if n, ok := byName[t.name]; ok {
			out = append(out, n)
		}
	}
	return out, ""
}

func importNutrients(in []Nutrient) []ImportNutrient {
	out := make([]ImportNutrient, 0, len(in))
	for _, n := range in {
		out = append(out, ImportNutrient(n))
	}
	return out
}

func nutrientResponses(in []Nutrient) []NutrientResponse {
	out := make([]NutrientResponse, 0, len(in))
	for _, n := range in {
		out = append(out, NutrientResponse(n))
	}
	return out
}

func nutrientsFromWire(in []NutrientResponse) []Nutrient {
	out := make([]Nutrient, 0, len(in))
	for _, n := range in {
		out = append(out, Nutrient(n))
	}
	return out
}
