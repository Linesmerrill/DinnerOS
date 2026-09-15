package grocery

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

func q(num, den int64) *ingredients.Quantity {
	v := ingredients.NewQuantity(num, den)
	return &v
}

func line(key, name, category string, qty *ingredients.Quantity, unit string) Line {
	return Line{IngredientKey: key, Name: name, Category: category, Quantity: qty, UnitCode: unit}
}

// render turns a list into a stable string for comparisons and readable failures.
func render(l List) string {
	var b strings.Builder
	for _, it := range l.Items {
		var parts []string
		for _, a := range it.Amounts {
			parts = append(parts, a.Quantity.String()+" "+a.Unit.Code)
		}
		if it.Unquantified {
			parts = append(parts, "to taste")
		}
		var srcs []string
		for _, s := range it.Sources {
			srcs = append(srcs, s.RecipeID)
		}
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s\n", it.Category, it.Name, it.IngredientKey, strings.Join(parts, " + "), it.Status, strings.Join(srcs, ","))
	}
	return b.String()
}

func mustAggregate(t *testing.T, sels []RecipeSelection, pantry Pantry) List {
	t.Helper()
	l, err := Aggregate(sels, pantry)
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}
	return l
}

func find(t *testing.T, l List, key string) Item {
	t.Helper()
	for _, it := range l.Items {
		if it.IngredientKey == key {
			return it
		}
	}
	t.Fatalf("item %q not found in:\n%s", key, render(l))
	return Item{}
}

func amountString(it Item) string {
	var parts []string
	for _, a := range it.Amounts {
		parts = append(parts, a.Quantity.String()+" "+a.Unit.Code)
	}
	if it.Unquantified {
		parts = append(parts, "to taste")
	}
	return strings.Join(parts, " + ")
}

func TestHalfOnionPlusHalfOnionIsOneOnion(t *testing.T) {
	l := mustAggregate(t, []RecipeSelection{
		{RecipeID: "a", RecipeName: "Recipe A", RecipeServings: 2, TargetServings: 2, Lines: []Line{line("onion", "Yellow Onion", "produce", q(1, 2), "count")}},
		{RecipeID: "b", RecipeName: "Recipe B", RecipeServings: 2, TargetServings: 2, Lines: []Line{line("onion", "Yellow Onion", "produce", q(1, 2), "count")}},
	}, nil)
	onion := find(t, l, "onion")
	if got := amountString(onion); got != "1 count" {
		t.Errorf("onion = %s, want 1 count", got)
	}
	if len(onion.Sources) != 2 || onion.Status != StatusToBuy {
		t.Errorf("onion = %+v", onion)
	}
}

func TestScalingIsExact(t *testing.T) {
	tests := []struct {
		name            string
		recipe, target  int
		qty             *ingredients.Quantity
		unit, wantTotal string
	}{
		{"double", 2, 4, q(3, 4), "cup", "3/2 cup"},
		{"halve", 4, 2, q(1, 1), "count", "1/2 count"},
		{"thirds", 3, 2, q(1, 1), "count", "2/3 count"},
		{"same", 2, 2, q(10, 1), "oz", "10 oz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := mustAggregate(t, []RecipeSelection{{RecipeID: "r", RecipeServings: tt.recipe, TargetServings: tt.target,
				Lines: []Line{line("x", "X", "pantry", tt.qty, tt.unit)}}}, nil)
			if got := amountString(find(t, l, "x")); got != tt.wantTotal {
				t.Errorf("total = %s, want %s", got, tt.wantTotal)
			}
		})
	}
}

func TestCompatibleUnitsCombineIntoLargestSensibleUnit(t *testing.T) {
	l := mustAggregate(t, []RecipeSelection{
		{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			line("oil", "Olive Oil", "pantry", q(3, 1), "tsp"),
			line("beef", "Ground Beef", "meat-seafood", q(12, 1), "oz"),
		}},
		{RecipeID: "b", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			line("oil", "Olive Oil", "pantry", q(1, 1), "tbsp"),
			line("beef", "Ground Beef", "meat-seafood", q(1, 2), "lb"),
		}},
	}, nil)
	if got := amountString(find(t, l, "oil")); got != "2 tbsp" {
		t.Errorf("oil = %s, want 2 tbsp (3 tsp + 1 tbsp)", got)
	}
	if got := amountString(find(t, l, "beef")); got != "5/4 lb" {
		t.Errorf("beef = %s, want 5/4 lb (12 oz + ½ lb)", got)
	}
}

func TestSmallTotalsStayInSmallestContributingUnit(t *testing.T) {
	l := mustAggregate(t, []RecipeSelection{
		{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{line("spice", "Cumin", "spices", q(1, 2), "tsp")}},
		{RecipeID: "b", RecipeServings: 2, TargetServings: 2, Lines: []Line{line("spice", "Cumin", "spices", q(1, 4), "tbsp")}},
	}, nil)
	// ½ tsp + ¼ tbsp = 1¼ tsp < 1 tbsp → shown in tsp.
	if got := amountString(find(t, l, "spice")); got != "5/4 tsp" {
		t.Errorf("cumin = %s, want 5/4 tsp", got)
	}
}

func TestIncompatibleUnitsStaySeparate(t *testing.T) {
	l := mustAggregate(t, []RecipeSelection{
		{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			line("onion", "Onion", "produce", q(1, 1), "count"),
			line("garlic", "Garlic", "produce", q(2, 1), "clove"),
		}},
		{RecipeID: "b", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			line("onion", "Onion", "produce", q(8, 1), "oz"),
			line("garlic", "Garlic", "produce", q(1, 1), "count"),
		}},
	}, nil)
	if got := amountString(find(t, l, "onion")); got != "1 count + 8 oz" {
		t.Errorf("onion = %s, want 1 count + 8 oz (never silently converted)", got)
	}
	if got := amountString(find(t, l, "garlic")); got != "2 clove + 1 count" {
		t.Errorf("garlic = %s, want separate clove and count", got)
	}
}

func TestUnquantifiedLinesAreKeptWithoutInventingAmounts(t *testing.T) {
	l := mustAggregate(t, []RecipeSelection{
		{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{line("salt", "Salt", "spices", nil, "")}},
		{RecipeID: "b", RecipeServings: 2, TargetServings: 4, Lines: []Line{line("salt", "Salt", "spices", q(1, 2), "tsp")}},
	}, nil)
	if got := amountString(find(t, l, "salt")); got != "1 tsp + to taste" {
		t.Errorf("salt = %s, want 1 tsp + to taste", got)
	}
}

func TestPantryStatus(t *testing.T) {
	hinted := line("salt", "Salt", "spices", nil, "")
	hinted.PantryStaple = true
	oil := line("oil", "Oil", "pantry", q(1, 1), "tbsp")
	oil.PantryStaple = true
	oilUnhinted := line("oil", "Oil", "pantry", q(1, 1), "tbsp")

	l := mustAggregate(t, []RecipeSelection{
		{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{hinted, oil, line("butter", "Butter", "dairy-eggs", q(2, 1), "tbsp")}},
		{RecipeID: "b", RecipeServings: 2, TargetServings: 2, Lines: []Line{oilUnhinted}},
	}, PantrySet{"butter": true})

	if s := find(t, l, "butter").Status; s != StatusInPantry {
		t.Errorf("butter status = %s, want inPantry (household pantry wins)", s)
	}
	if s := find(t, l, "salt").Status; s != StatusPantryHint {
		t.Errorf("salt status = %s, want pantryHint", s)
	}
	if s := find(t, l, "oil").Status; s != StatusToBuy {
		t.Errorf("oil status = %s, want toBuy (not every source hinted it)", s)
	}
}

func TestPantryStockOutOverridesStapleHint(t *testing.T) {
	salt := line("salt", "Salt", "spices", nil, "")
	salt.PantryStaple = true
	pepper := line("pepper", "Pepper", "spices", nil, "")
	pepper.PantryStaple = true
	oil := line("oil", "Oil", "pantry", q(2, 1), "tbsp")
	oil.PantryStaple = true
	butter := line("butter", "Butter", "dairy-eggs", q(1, 1), "tbsp")

	stock := PantryStock{
		InStock:    map[string]bool{"oil": true},
		OutOfStock: map[string]bool{"salt": true, "butter": true},
	}
	l := mustAggregate(t, []RecipeSelection{
		{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{salt, pepper, oil, butter}},
	}, stock)

	want := map[string]Status{
		"oil":    StatusInPantry,   // in stock
		"salt":   StatusToBuy,      // hinted, but the household recorded it as out
		"pepper": StatusPantryHint, // hinted and unknown to the pantry
		"butter": StatusToBuy,      // out, not hinted
	}
	for key, status := range want {
		if got := find(t, l, key).Status; got != status {
			t.Errorf("%s status = %s, want %s", key, got, status)
		}
	}
	// A plain PantrySet has no out-of-stock knowledge, so hints still apply.
	if got := find(t, mustAggregate(t, []RecipeSelection{{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{salt}}}, PantrySet{}), "salt").Status; got != StatusPantryHint {
		t.Errorf("salt with PantrySet = %s, want pantryHint", got)
	}
}

func TestOutputIsIndependentOfInputOrder(t *testing.T) {
	sels := []RecipeSelection{
		{RecipeID: "a", RecipeName: "Tacos", RecipeServings: 2, TargetServings: 4, Lines: []Line{
			line("onion", "Onion", "produce", q(1, 2), "count"),
			line("oil", "Oil", "pantry", q(1, 1), "tsp"),
			line("beef", "Beef", "meat-seafood", q(10, 1), "oz"),
		}},
		{RecipeID: "b", RecipeName: "Pasta", RecipeServings: 4, TargetServings: 2, Lines: []Line{
			line("onion", "Onion", "produce", q(1, 1), "count"),
			line("oil", "Oil", "pantry", q(2, 1), "tbsp"),
			line("parm", "Parmesan", "dairy-eggs", q(1, 2), "oz"),
		}},
		{RecipeID: "c", RecipeName: "Soup", RecipeServings: 3, TargetServings: 2, Lines: []Line{
			line("onion", "Onion", "produce", q(8, 1), "oz"),
			line("beef", "Beef", "meat-seafood", q(1, 2), "lb"),
			line("salt", "Salt", "spices", nil, ""),
		}},
	}
	want := render(mustAggregate(t, sels, nil))

	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		shuffled := make([]RecipeSelection, len(sels))
		for j, idx := range rng.Perm(len(sels)) {
			s := sels[idx]
			lines := append([]Line(nil), s.Lines...)
			rng.Shuffle(len(lines), func(a, b int) { lines[a], lines[b] = lines[b], lines[a] })
			s.Lines = lines
			shuffled[j] = s
		}
		if got := render(mustAggregate(t, shuffled, nil)); got != want {
			t.Fatalf("permutation %d changed output:\n got:\n%s\nwant:\n%s", i, got, want)
		}
	}
}

func TestScalingThenAggregatingIsLinear(t *testing.T) {
	base := []RecipeSelection{
		{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{line("rice", "Rice", "pantry", q(3, 4), "cup"), line("lime", "Lime", "produce", q(1, 1), "count")}},
		{RecipeID: "b", RecipeServings: 2, TargetServings: 2, Lines: []Line{line("rice", "Rice", "pantry", q(1, 4), "cup"), line("lime", "Lime", "produce", q(1, 2), "count")}},
	}
	doubled := make([]RecipeSelection, len(base))
	for i, s := range base {
		s.TargetServings *= 2
		doubled[i] = s
	}
	one := mustAggregate(t, base, nil)
	two := mustAggregate(t, doubled, nil)
	for _, key := range []string{"rice", "lime"} {
		a := find(t, one, key).Amounts[0]
		b := find(t, two, key).Amounts[0]
		if !a.Quantity.Mul(ingredients.NewQuantity(2, 1)).Equal(b.Quantity) || a.Unit.Code != b.Unit.Code {
			t.Errorf("%s: 2×(%s %s) != %s %s", key, a.Quantity, a.Unit.Code, b.Quantity, b.Unit.Code)
		}
	}
}

func TestSortingByCategoryThenName(t *testing.T) {
	l := mustAggregate(t, []RecipeSelection{{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{
		line("z", "zucchini", "produce", q(1, 1), "count"),
		line("c", "Cheddar", "dairy-eggs", q(4, 1), "oz"),
		line("a", "apple", "produce", q(1, 1), "count"),
		line("m", "Mystery", "unknown-aisle", q(1, 1), "count"),
		line("s", "Salmon", "meat-seafood", q(1, 1), "lb"),
	}}}, nil)
	var order []string
	for _, it := range l.Items {
		order = append(order, it.Name+"@"+it.Category)
	}
	want := "apple@produce,zucchini@produce,Salmon@meat-seafood,Cheddar@dairy-eggs,Mystery@other"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
}

func TestInvalidSelections(t *testing.T) {
	tests := map[string]RecipeSelection{
		"zero recipe servings": {RecipeID: "a", RecipeServings: 0, TargetServings: 2},
		"zero target":          {RecipeID: "a", RecipeServings: 2, TargetServings: 0},
		"missing key":          {RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{line("", "X", "", q(1, 1), "count")}},
		"unknown unit":         {RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{line("x", "X", "", q(1, 1), "dollop")}},
	}
	for name, sel := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Aggregate([]RecipeSelection{sel}, nil); !errors.Is(err, ErrInvalidSelection) {
				t.Errorf("error = %v, want ErrInvalidSelection", err)
			}
		})
	}
}

func TestEmptySelectionsYieldEmptyList(t *testing.T) {
	l := mustAggregate(t, nil, nil)
	if len(l.Items) != 0 {
		t.Errorf("items = %d, want 0", len(l.Items))
	}
}
