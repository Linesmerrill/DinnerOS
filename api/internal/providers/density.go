package providers

import (
	"math/big"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file estimates weight from volume, for package counting only.
//
// Recipes measure in spoons and cups and stores sell by the ounce of weight:
// 2 tbsp of jam against an 18 oz jar can't be converted exactly, and flagging
// it every week is noise. Nothing here changes a recipe's or the pantry's
// amounts — ingredients.Convert still refuses volume↔mass — it only decides
// how many packages to put in a cart.
//
// Two numbers are used. A typical density for the ingredient (a small table
// by name, then the grocery category) gives the count. A deliberately extreme
// bound checks it: if even the extreme gives the same count, the count is
// certain; if not, the count stands but is shown as "about".

// Item is what a package count is for: the grocery line's name and category.
type Item struct {
	Name     string
	Category string
}

// Density bounds, in g/ml. No kitchen ingredient is heavier than 1.5 (honey is
// about 1.42) or lighter than 0.1 (loose fresh leaves).
var (
	maxDensity = big.NewRat(3, 2)
	minDensity = big.NewRat(1, 10)
)

type densityRule struct {
	words    []string
	category string // "" for any
	density  *big.Rat
}

func gml(s string) *big.Rat {
	r, _ := new(big.Rat).SetString(s)
	return r
}

// densityRules are matched in order against the line's normalized name; the
// first rule whose phrase appears as whole words wins, so longer phrases come
// before the words inside them ("peanut butter" before "butter").
var densityRules = []densityRule{
	{words: []string{"peanut butter", "almond butter", "nut butter", "tahini"}, density: gml("1.1")},
	{words: []string{"honey", "molasses", "syrup", "jam", "jelly", "preserves", "marmalade", "agave"}, density: gml("1.4")},
	{words: []string{"powdered sugar", "confectioners sugar"}, density: gml("0.56")},
	{words: []string{"brown sugar"}, density: gml("0.9")},
	{words: []string{"sugar"}, density: gml("0.85")},
	{words: []string{"kosher salt", "sea salt flakes"}, density: gml("0.7")},
	{words: []string{"garlic salt", "onion salt", "seasoned salt"}, density: gml("1")},
	{words: []string{"salt"}, density: gml("1.2")},
	{words: []string{"flour", "cornstarch", "corn starch"}, density: gml("0.55")},
	{words: []string{"cocoa"}, density: gml("0.45")},
	{words: []string{"baking powder", "baking soda"}, density: gml("0.9")},
	{words: []string{"oil"}, density: gml("0.92")},
	{words: []string{"butter", "ghee"}, density: gml("0.95")},
	{words: []string{"mayonnaise", "mayo"}, density: gml("0.95")},
	{words: []string{"soy sauce", "fish sauce", "tamari", "worcestershire", "ketchup", "tomato paste", "paste"}, density: gml("1.15")},
	{words: []string{"vinegar", "sauce", "salsa", "mustard", "juice", "broth", "stock", "milk", "cream", "yogurt", "water"}, density: gml("1.05")},
	{words: []string{"wine", "extract", "vanilla"}, density: gml("0.95")},
	{words: []string{"rice", "lentils", "quinoa"}, density: gml("0.85")},
	{words: []string{"oats", "oatmeal", "breadcrumbs", "bread crumbs", "panko"}, density: gml("0.35")},
	{words: []string{"parmesan", "shredded", "grated"}, density: gml("0.4")},
	{words: []string{"chocolate chips", "raisins", "nuts", "almonds", "walnuts", "pecans", "seeds"}, density: gml("0.6")},
	// Fresh leaves are packed loosely into a cup; dried ones are flakes.
	{words: []string{"basil", "cilantro", "parsley", "mint", "dill", "spinach", "arugula", "kale", "lettuce", "greens"}, category: ingredients.CategoryProduce, density: gml("0.15")},
	{words: []string{"oregano", "thyme", "basil", "parsley", "rosemary", "dill", "sage", "italian seasoning", "herbs"}, density: gml("0.3")},
	{words: []string{"powder", "paprika", "cumin", "cinnamon", "turmeric", "nutmeg", "pepper", "cayenne", "seasoning", "spice"}, category: ingredients.CategorySpices, density: gml("0.5")},
}

// categoryDensities are the fallback when no name rule matches.
var categoryDensities = map[string]*big.Rat{
	ingredients.CategorySpices:      gml("0.5"),
	ingredients.CategoryCondiments:  gml("1.1"),
	ingredients.CategoryPantry:      gml("0.8"),
	ingredients.CategoryDairyEggs:   gml("1.03"),
	ingredients.CategoryBeverages:   gml("1"),
	ingredients.CategoryProduce:     gml("0.6"),
	ingredients.CategoryFrozen:      gml("0.6"),
	ingredients.CategoryMeatSeafood: gml("1"),
	ingredients.CategoryBakery:      gml("0.4"),
	ingredients.CategoryDeli:        gml("0.8"),
}

// densityFor is the typical density of item in g/ml, and whether it came from
// a name rule (a specific ingredient) rather than the category fallback.
func densityFor(item Item) (*big.Rat, bool) {
	name := " " + ingredients.NormalizeName(item.Name) + " "
	for _, rule := range densityRules {
		if rule.category != "" && rule.category != item.Category {
			continue
		}
		for _, w := range rule.words {
			if strings.Contains(name, " "+w+" ") || strings.Contains(name, " "+w+"s ") {
				return rule.density, true
			}
		}
	}
	if d, ok := categoryDensities[item.Category]; ok {
		return d, false
	}
	return big.NewRat(1, 1), false
}

// crossKind reports whether from and to are volume and mass, in either order.
func crossKind(from, to ingredients.Unit) bool {
	return (from.Kind == ingredients.KindVolume && to.Kind == ingredients.KindMass) ||
		(from.Kind == ingredients.KindMass && to.Kind == ingredients.KindVolume)
}

// estimate converts q in from to the unit to using density (g/ml). from and
// to must be crossKind.
func estimate(q ingredients.Quantity, from, to ingredients.Unit, density *big.Rat) ingredients.Quantity {
	base := q.MulRat(from.BaseFactor()) // ml or g
	if from.Kind == ingredients.KindVolume {
		base = base.MulRat(density)
	} else {
		base = base.MulRat(new(big.Rat).Inv(density))
	}
	return base.MulRat(new(big.Rat).Inv(to.BaseFactor()))
}

// extremeDensity is the bound that makes an estimate largest in to's kind:
// heaviest when turning volume into weight, lightest the other way.
func extremeDensity(to ingredients.Unit) *big.Rat {
	if to.Kind == ingredients.KindMass {
		return maxDensity
	}
	return minDensity
}

// EstimateAmount converts q between a volume and a weight unit code with
// item's typical density, for estimates that are allowed to be approximate:
// package counting here, and pantry deductions of a recipe's spoons from a
// package bought by weight (docs/pantry-usage.md#unit-conversion). ok is false
// when the units aren't one volume and one weight.
func EstimateAmount(q *big.Rat, from, to string, item Item) (*big.Rat, bool) {
	fu, err1 := ingredients.LookupUnit(from)
	tu, err2 := ingredients.LookupUnit(to)
	if err1 != nil || err2 != nil || !crossKind(fu, tu) {
		return nil, false
	}
	density, _ := densityFor(item)
	return estimate(ingredients.NewQuantity(1, 1).MulRat(q), fu, tu, density).Rat(), true
}
