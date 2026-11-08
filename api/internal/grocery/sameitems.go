package grocery

import (
	"maps"
	"math/big"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// SameItem is one name a household said is the same thing to buy as another
// ("Chipotle Powder" is "Chipotle Chili Powder", decision 661). Lines under
// any of VariantKeys are bought as the kept item: one line, one product.
type SameItem struct {
	// LinkID is the household's link, so the app can undo it.
	LinkID string
	// VariantKeys are every grocery line key the variant's lines can carry:
	// the key it was linked from and its "name:" spelling.
	VariantKeys []string
	VariantName string
	KeptKey     string
	KeptName    string
}

// AlsoCalled is a name a merged item absorbed, with the link that merged it.
type AlsoCalled struct {
	LinkID string
	// Key is the variant's own line key, so a product saved for it can be
	// used for the merged item.
	Key  string
	Name string
}

// SameItems are a household's links, applied to a week's lines before they
// are aggregated.
type SameItems []SameItem

// byKey maps every variant key to its link.
func (s SameItems) byKey() map[string]SameItem {
	out := make(map[string]SameItem, len(s)*2)
	for _, link := range s {
		for _, k := range link.VariantKeys {
			if k != "" && k != link.KeptKey {
				out[k] = link
			}
		}
	}
	return out
}

// Apply renames every variant line to its kept item, so the two aggregate
// into one. It returns the names each kept key absorbed this week, in link
// order, only for variants that actually had a line. A line that is itself a
// specialty ingredient keeps its key: it stands for a recipe, not a product.
func (s SameItems) Apply(selections []RecipeSelection) ([]RecipeSelection, map[string][]AlsoCalled) {
	if len(s) == 0 {
		return selections, nil
	}
	links := s.byKey()
	used := map[string]bool{}
	out := make([]RecipeSelection, len(selections))
	for i, sel := range selections {
		lines := make([]Line, len(sel.Lines))
		for j, line := range sel.Lines {
			if link, ok := links[line.IngredientKey]; ok && line.Specialty == nil {
				line.IngredientKey, line.Name = link.KeptKey, link.KeptName
				used[link.LinkID] = true
			}
			lines[j] = line
		}
		sel.Lines = lines
		out[i] = sel
	}
	also := map[string][]AlsoCalled{}
	for _, link := range s {
		if used[link.LinkID] {
			also[link.KeptKey] = append(also[link.KeptKey], AlsoCalled{
				LinkID: link.LinkID, Key: link.VariantKeys[0], Name: link.VariantName,
			})
		}
	}
	return out, also
}

// Pantry returns stock that answers for a kept key with what's at home under
// a variant's key, when the pantry knows nothing of the kept key itself: a
// jar the household keeps as "Chipotle Powder" covers "Chipotle Chili
// Powder". A pantry entry for the kept key always wins.
func (s SameItems) Pantry(stock PantryStock) PantryStock {
	if len(s) == 0 {
		return stock
	}
	out := PantryStock{
		InStock: maps.Clone(stock.InStock), OutOfStock: maps.Clone(stock.OutOfStock),
		InFreezer: maps.Clone(stock.InFreezer), Amounts: maps.Clone(stock.Amounts),
		Uncertain: maps.Clone(stock.Uncertain),
	}
	known := func(k string) bool { return stock.InStock[k] || stock.OutOfStock[k] || stock.InFreezer[k] }
	// The pantry key each kept key reads from, when it isn't its own.
	from := map[string]string{}
	for _, link := range s {
		if known(link.KeptKey) || from[link.KeptKey] != "" {
			continue
		}
		for _, k := range link.VariantKeys {
			if known(k) {
				from[link.KeptKey] = k
				break
			}
		}
	}
	for kept, variant := range from {
		set := func(m map[string]bool) map[string]bool {
			if m[variant] {
				if m == nil {
					m = map[string]bool{}
				}
				m[kept] = true
			}
			return m
		}
		out.InStock, out.OutOfStock, out.InFreezer, out.Uncertain =
			set(out.InStock), set(out.OutOfStock), set(out.InFreezer), set(out.Uncertain)
		if a, ok := stock.Amounts[variant]; ok {
			if out.Amounts == nil {
				out.Amounts = map[string]Amount{}
			}
			out.Amounts[kept] = a
		}
	}
	if convert := stock.Convert; convert != nil {
		out.Convert = func(key, name string, need Amount, to ingredients.Unit) (*big.Rat, bool) {
			if variant, ok := from[key]; ok {
				return convert(variant, name, need, to)
			}
			return convert(key, name, need, to)
		}
	}
	return out
}

// Skips returns rules where a skip on a variant also covers its kept item: a
// recipe skip always (the recipe's line is now the kept one), and an
// ingredient-wide skip unless the kept item's own skip outranks it.
func (s SameItems) Skips(rules SkipRules) SkipRules {
	if len(s) == 0 {
		return rules
	}
	out := SkipRules{Ingredients: maps.Clone(rules.Ingredients), Recipes: map[string]map[string]bool{}}
	if out.Ingredients == nil {
		out.Ingredients = SkipSet{}
	}
	for id, keys := range rules.Recipes {
		out.Recipes[id] = maps.Clone(keys)
	}
	for _, link := range s {
		for _, k := range link.VariantKeys {
			if scope, ok := rules.Ingredients[k]; ok {
				if current, has := out.Ingredients[link.KeptKey]; !has || scope.Outranks(current) {
					out.Ingredients[link.KeptKey] = scope
				}
			}
			for _, keys := range out.Recipes {
				if keys[k] {
					keys[link.KeptKey] = true
				}
			}
		}
	}
	return out
}

// LeftOut returns set with each link's names answering for one another: a
// "Just for this dish" skip made on the merged Shop line is saved under the
// kept name, but the recipe's own line may carry the other name, and its
// cooking steps should still cross it out. The stronger scope wins where both
// names have an entry.
func (s SameItems) LeftOut(set LeftOutSet) LeftOutSet {
	if len(s) == 0 || len(set) == 0 {
		return set
	}
	out := maps.Clone(set)
	put := func(key string, lo LeftOut) {
		if current, ok := out[key]; !ok || lo.Scope.Outranks(current.Scope) {
			out[key] = lo
		}
	}
	for _, link := range s {
		names := append([]string{link.KeptKey}, link.VariantKeys...)
		for _, from := range names {
			lo, ok := set[from]
			if !ok || from == "" {
				continue
			}
			for _, to := range names {
				if to != "" && to != from {
					put(to, lo)
				}
			}
		}
	}
	return out
}
