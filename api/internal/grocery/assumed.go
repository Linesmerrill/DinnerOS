package grocery

import (
	"maps"
	"math/big"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Without returns the pantry as it will be once the items' meals are made:
// each amount at home less what those meals use (decision 662). An
// ingredient they use up is no longer in stock, so the list buys it in full.
// One whose use can't be put in the pantry's unit is marked uncertain rather
// than guessed. Ingredients the pantry has no amount for are left as they
// are, as is everything with no items to take away.
func (p PantryStock) Without(items []Item) PantryStock {
	if len(items) == 0 {
		return p
	}
	out := p
	out.InStock, out.InFreezer = maps.Clone(p.InStock), maps.Clone(p.InFreezer)
	out.Amounts, out.Uncertain = maps.Clone(p.Amounts), maps.Clone(p.Uncertain)
	if out.Uncertain == nil {
		out.Uncertain = map[string]bool{}
	}
	for _, item := range items {
		key := item.IngredientKey
		have, ok := out.Amounts[key]
		if !ok || len(item.Amounts) == 0 {
			continue
		}
		used := new(big.Rat)
		convertible := true
		for _, need := range item.Amounts {
			var in *big.Rat
			if need.Unit.CanConvertTo(have.Unit) {
				if q, err := ingredients.Convert(need.Quantity, need.Unit, have.Unit); err == nil {
					in = q.Rat()
				}
			}
			if in == nil {
				if in, ok = p.ConvertNeed(key, item.Name, need, have.Unit); !ok {
					convertible = false
					break
				}
			}
			used.Add(used, in)
		}
		if !convertible {
			out.Uncertain[key] = true
			continue
		}
		left := new(big.Rat).Sub(have.Quantity.Rat(), used)
		if left.Sign() <= 0 {
			delete(out.Amounts, key)
			delete(out.InStock, key)
			delete(out.InFreezer, key)
			continue
		}
		out.Amounts[key] = Amount{Quantity: quantityFromRat(left), Unit: have.Unit}
	}
	return out
}
