package customize

import (
	"math/big"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Target is the ingredient a swap cooks: the catalog ingredient with the
// protein's name when there is one, otherwise the protein's name alone.
type Target struct {
	IngredientID string
	Name         string
	Category     string
	ImageURL     string
}

// Key is the target's line key: its catalog ID, or "name:<normalized name>".
func (t Target) Key() string {
	return LineKey(recipes.RecipeIngredient{IngredientID: t.IngredientID, Name: t.Name})
}

// Pick is one resolved customization of a recipe line.
type Pick struct {
	// IngredientKey is the customized line's key.
	IngredientKey string
	Choice        Choice
	// Target is the swapped-in ingredient; unused for original and double.
	Target Target
}

// ApplyGrocery returns sel with its customized lines changed, for the grocery
// list. It is pure and doesn't change sel.
//
//   - original: the line is unchanged.
//   - double: the line keeps its ingredient with twice the amount.
//   - swap and swap_double: the line becomes the target ingredient, with the
//     amount times the choice's factor, and is no longer a pantry staple.
//
// Changed lines carry a grocery.ViaCustomized provenance naming the original
// line and the choice. A line without an amount stays without one. Lines no
// pick names are unchanged, including specialty ingredients, which
// grocery.ApplySpecialties handles afterwards.
func ApplyGrocery(sel grocery.RecipeSelection, picks []Pick) grocery.RecipeSelection {
	out := sel
	out.Lines = make([]grocery.Line, 0, len(sel.Lines))
	for _, line := range sel.Lines {
		pick, ok := findPick(picks, line.IngredientKey)
		if !ok || pick.Choice.Kind == KindOriginal {
			out.Lines = append(out.Lines, line)
			continue
		}
		changed := line
		if line.Quantity != nil {
			q := line.Quantity.MulRat(pick.Choice.Factor)
			changed.Quantity = &q
		}
		if pick.Choice.IsSwap() {
			changed.IngredientKey, changed.Name, changed.Category, changed.PantryStaple = pick.Target.Key(), pick.Target.Name, pick.Target.Category, false
		}
		changed.Via = &grocery.Via{
			Kind: grocery.ViaCustomized, SpecialtyKey: line.IngredientKey, SpecialtyName: line.Name,
			OptionID: pick.Choice.ID, OptionName: pick.Choice.Label,
		}
		out.Lines = append(out.Lines, changed)
	}
	return out
}

// ApplyRecipe returns r with its customized ingredient lines changed the same
// way, with every authored amount scaled, for pantry cook deductions. It
// doesn't change r.
func ApplyRecipe(r recipes.Recipe, picks []Pick) recipes.Recipe {
	out := r
	out.Ingredients = make([]recipes.RecipeIngredient, 0, len(r.Ingredients))
	for _, ing := range r.Ingredients {
		pick, ok := findPick(picks, LineKey(ing))
		if !ok || pick.Choice.Kind == KindOriginal {
			out.Ingredients = append(out.Ingredients, ing)
			continue
		}
		changed := ing
		changed.Amounts = make([]recipes.Amount, 0, len(ing.Amounts))
		for _, a := range ing.Amounts {
			if q, ok := a.ExactQuantity(); ok {
				a.Quantity = scaled(q, pick.Choice.Factor)
			}
			changed.Amounts = append(changed.Amounts, a)
		}
		if pick.Choice.IsSwap() {
			changed.IngredientID, changed.Name, changed.Category, changed.PantryStaple = pick.Target.IngredientID, pick.Target.Name, pick.Target.Category, false
		}
		out.Ingredients = append(out.Ingredients, changed)
	}
	return out
}

func scaled(q ingredients.Quantity, factor *big.Rat) string { return q.MulRat(factor).String() }

func findPick(picks []Pick, key string) (Pick, bool) {
	for _, p := range picks {
		if p.IngredientKey == key {
			return p, true
		}
	}
	return Pick{}, false
}
