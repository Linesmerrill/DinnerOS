package grocery

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// ErrInvalidSelection is returned for selections that cannot be scaled.
var ErrInvalidSelection = errors.New("invalid recipe selection")

// Line is one recipe ingredient line prepared for aggregation. Lines come from
// a recipe's authored amounts for RecipeSelection.RecipeServings.
type Line struct {
	// IngredientKey identifies the canonical ingredient. Unresolved source
	// ingredients use a stable key derived from their normalized name.
	IngredientKey string
	Name          string
	Category      string
	// Quantity is nil when the source gave no amount ("salt to taste").
	Quantity *ingredients.Quantity
	// UnitCode is empty when Quantity is nil.
	UnitCode string
	// PantryStaple is the source's hint that the ingredient is usually at home.
	PantryStaple bool
	// Via is set on a line that stands in for a specialty ingredient
	// (ApplySpecialties).
	Via *Via
	// Specialty is set on a line that is itself a specialty ingredient.
	Specialty *LineSpecialty
	// Sources, when set, are the recipes the line is for, instead of the
	// selection's recipe (a batch made for several recipes).
	Sources []Source
	// Extra is set on a line added to the week directly rather than from a
	// recipe's ingredients (extras.go).
	Extra *Extra
}

// RecipeSelection is a planned recipe with the servings to cook.
type RecipeSelection struct {
	RecipeID       string
	RecipeName     string
	RecipeServings int
	TargetServings int
	Lines          []Line
}

// Status says whether an item needs buying.
type Status string

// Item statuses.
const (
	// StatusToBuy is on the shopping list.
	StatusToBuy Status = "toBuy"
	// StatusInPantry is in the household's pantry.
	StatusInPantry Status = "inPantry"
	// StatusPantryHint is flagged by the recipe source as a staple (salt, oil)
	// but not in the household pantry. Shown as "probably have it".
	StatusPantryHint Status = "pantryHint"
	// StatusSkipped is an ingredient the household chose not to buy. Items with
	// this status are in List.SkippedItems, never in List.Items, so nothing
	// asks anyone to buy them; they are reported rather than dropped so the
	// grocery list and the recipe never disagree about what a recipe needs.
	StatusSkipped Status = "skipped"
)

// SkipScope is how long a skip lasts.
type SkipScope string

// Skip scopes.
const (
	// SkipThisWeek leaves the ingredient off one week's list only.
	SkipThisWeek SkipScope = "week"
	// SkipAlways leaves it off every list until the household resumes it.
	SkipAlways SkipScope = "always"
)

// Amount is a combined quantity in one unit.
type Amount struct {
	Quantity ingredients.Quantity
	Unit     ingredients.Unit
}

// Source is a recipe that contributed to an item.
type Source struct {
	RecipeID   string
	RecipeName string
}

// Item is one aggregated grocery line.
type Item struct {
	IngredientKey string
	Name          string
	Category      string
	// Amounts holds one entry per group of mutually convertible units. "1 onion"
	// and "8 oz onion" stay as two amounts because they cannot be combined.
	Amounts []Amount
	// Unquantified is true when at least one contributing line had no amount.
	Unquantified bool
	Status       Status
	Sources      []Source
	// Via lists the specialty ingredients this item stands in for, with the
	// recipes each is for. Empty for ordinary items.
	Via []ItemVia
	// Specialty is set when the item is a specialty ingredient left on the
	// list or kept as a house-made batch.
	Specialty *LineSpecialty
	// Extras are the week's extra lines this item includes, such as a paired
	// grocery item. Empty for items only recipes ask for.
	Extras []Extra
	// SkipScope is set only on an item the household skipped, and says which
	// lifetime skipped it. Empty on every item in List.Items.
	SkipScope SkipScope
}

// List is the aggregated grocery list.
type List struct {
	Items []Item
	// SkippedItems are the items the household chose not to buy, aggregated
	// exactly like the rest and then held back from Items. They keep their
	// amounts, recipes, and Via, so the app can say what is being skipped and
	// which meals wanted it.
	SkippedItems []Item
}

// Pantry reports whether a household keeps an ingredient at home.
type Pantry interface {
	Has(ingredientKey string) bool
}

// PantrySet is a simple Pantry backed by ingredient keys.
type PantrySet map[string]bool

// Has implements Pantry.
func (p PantrySet) Has(key string) bool { return p[key] }

// OutPantry is a Pantry that also knows which ingredients the household has
// recorded as out. Aggregate lists those as toBuy even when every source flags
// them as staples: the household knows it doesn't have them, so "probably have
// it" would be wrong.
type OutPantry interface {
	Pantry
	Out(ingredientKey string) bool
}

// PantryStock is a household pantry snapshot. Keys match Line.IngredientKey.
// Ingredients in neither set (unknown to the pantry, or running low) get the
// engine's default status: pantryHint when every source flags them as staples,
// otherwise toBuy.
type PantryStock struct {
	InStock    map[string]bool
	OutOfStock map[string]bool
}

// Has implements Pantry.
func (p PantryStock) Has(key string) bool { return p.InStock[key] }

// Out implements OutPantry.
func (p PantryStock) Out(key string) bool { return p.OutOfStock[key] }

// Skips reports the ingredients a household chose not to buy. It is consulted
// after specialty ingredients are applied, so skipping an ingredient a store
// alternative introduced leaves the rest of that alternative alone.
type Skips interface {
	// Skip returns the scope that skips ingredientKey, and false when the
	// household buys it as usual.
	Skip(ingredientKey string) (SkipScope, bool)
}

// SkipSet is a simple Skips backed by ingredient keys. An ingredient reachable
// under more than one key (a catalog ID and "name:<normalized name>") is
// registered under each.
type SkipSet map[string]SkipScope

// Skip implements Skips.
func (s SkipSet) Skip(key string) (SkipScope, bool) {
	scope, ok := s[key]
	return scope, ok
}

// CategoryOrder is the aisle order used to sort the list.
var CategoryOrder = []string{
	"produce", "meat-seafood", "dairy-eggs", "bakery", "deli",
	"pantry", "spices", "condiments", "frozen", "beverages", "other",
}

type accumulator struct {
	key, name, category string
	unquantified        bool
	allHinted           bool
	groups              map[string]*unitGroup
	sources             map[string]Source
	via                 map[string]*ItemVia
	specialty           *LineSpecialty
	extras              map[string]Extra
}

// unitGroup sums quantities that convert to each other, exactly, in the kind's
// base unit (or the discrete unit itself).
type unitGroup struct {
	kind      ingredients.Kind
	discrete  ingredients.Unit
	baseTotal *big.Rat
	units     map[string]ingredients.Unit
}

// Aggregate builds a grocery list from planned recipes. It is deterministic:
// the output does not depend on the order of selections or lines.
func Aggregate(selections []RecipeSelection, pantry Pantry) (List, error) {
	return AggregateWith(selections, pantry, nil)
}

// AggregateWith is Aggregate with the ingredients the household chose not to
// buy. Those items are aggregated like every other line and then held back in
// List.SkippedItems with StatusSkipped, rather than dropped: the recipe still
// needs the ingredient, and the list says so instead of quietly disagreeing
// with it. A nil skips buys everything.
func AggregateWith(selections []RecipeSelection, pantry Pantry, skips Skips) (List, error) {
	if pantry == nil {
		pantry = PantrySet{}
	}
	outPantry, _ := pantry.(OutPantry)
	isOut := func(key string) bool { return outPantry != nil && outPantry.Out(key) }
	acc := map[string]*accumulator{}

	for _, sel := range selections {
		if sel.RecipeServings <= 0 || sel.TargetServings <= 0 {
			return List{}, fmt.Errorf("%w: recipe %q servings %d → %d", ErrInvalidSelection, sel.RecipeID, sel.RecipeServings, sel.TargetServings)
		}
		factor := ingredients.NewQuantity(int64(sel.TargetServings), int64(sel.RecipeServings))

		for _, line := range sel.Lines {
			key := strings.TrimSpace(line.IngredientKey)
			if key == "" {
				return List{}, fmt.Errorf("%w: recipe %q has a line without an ingredient key", ErrInvalidSelection, sel.RecipeID)
			}
			a, ok := acc[key]
			if !ok {
				a = &accumulator{key: key, name: line.Name, category: line.Category, allHinted: true, groups: map[string]*unitGroup{}, sources: map[string]Source{}, via: map[string]*ItemVia{}}
				acc[key] = a
			}
			// Deterministic name/category choice independent of input order.
			if a.name == "" || (line.Name != "" && line.Name < a.name) {
				a.name = line.Name
			}
			if a.category == "" || (line.Category != "" && line.Category < a.category) {
				a.category = line.Category
			}
			a.allHinted = a.allHinted && line.PantryStaple
			lineSources := line.Sources
			if len(lineSources) == 0 {
				lineSources = []Source{{RecipeID: sel.RecipeID, RecipeName: sel.RecipeName}}
			}
			for _, src := range lineSources {
				a.sources[src.RecipeID] = src
			}
			a.addVia(line.Via, lineSources)
			a.addExtra(line.Extra)
			if s := line.Specialty; s != nil && (a.specialty == nil || s.less(*a.specialty)) {
				c := *s
				a.specialty = &c
			}

			if line.Quantity == nil || line.Quantity.IsZero() || line.UnitCode == "" {
				a.unquantified = true
				continue
			}
			unit, err := ingredients.LookupUnit(line.UnitCode)
			if err != nil {
				return List{}, fmt.Errorf("%w: recipe %q ingredient %q: %w", ErrInvalidSelection, sel.RecipeID, line.Name, err)
			}
			scaled := line.Quantity.Mul(factor)
			a.add(scaled, unit)
		}
	}

	list := List{Items: make([]Item, 0, len(acc))}
	for _, a := range acc {
		item := Item{
			IngredientKey: a.key,
			Name:          a.name,
			Category:      normalizeCategory(a.category),
			Unquantified:  a.unquantified,
			Amounts:       a.amounts(),
			Via:           a.itemVia(),
			Specialty:     a.specialty,
			Extras:        a.itemExtras(),
		}
		var skipScope SkipScope
		isSkipped := false
		if skips != nil {
			skipScope, isSkipped = skips.Skip(a.key)
		}
		switch {
		// A skip outranks the pantry: whatever the household has at home, it
		// asked for this ingredient to stay off the list.
		case isSkipped:
			item.Status, item.SkipScope = StatusSkipped, skipScope
		case pantry.Has(a.key):
			item.Status = StatusInPantry
		case a.allHinted && !isOut(a.key):
			item.Status = StatusPantryHint
		default:
			item.Status = StatusToBuy
		}
		for _, s := range a.sources {
			item.Sources = append(item.Sources, s)
		}
		sort.Slice(item.Sources, func(i, j int) bool {
			if item.Sources[i].RecipeName != item.Sources[j].RecipeName {
				return item.Sources[i].RecipeName < item.Sources[j].RecipeName
			}
			return item.Sources[i].RecipeID < item.Sources[j].RecipeID
		})
		if isSkipped {
			list.SkippedItems = append(list.SkippedItems, item)
			continue
		}
		list.Items = append(list.Items, item)
	}

	sortItems(list.Items)
	sortItems(list.SkippedItems)
	return list, nil
}

// sortItems puts items in aisle order, then by name, then by key, so the
// result never depends on input order.
func sortItems(items []Item) {
	rank := map[string]int{}
	for i, c := range CategoryOrder {
		rank[c] = i
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if rank[a.Category] != rank[b.Category] {
			return rank[a.Category] < rank[b.Category]
		}
		if !strings.EqualFold(a.Name, b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.IngredientKey < b.IngredientKey
	})
}

func (a *accumulator) add(q ingredients.Quantity, unit ingredients.Unit) {
	groupKey := string(unit.Kind)
	if unit.Discrete() {
		groupKey = "discrete:" + unit.Code
	}
	g, ok := a.groups[groupKey]
	if !ok {
		g = &unitGroup{kind: unit.Kind, discrete: unit, baseTotal: new(big.Rat), units: map[string]ingredients.Unit{}}
		a.groups[groupKey] = g
	}
	g.units[unit.Code] = unit
	if unit.Discrete() {
		g.baseTotal.Add(g.baseTotal, q.Rat())
		return
	}
	g.baseTotal.Add(g.baseTotal, new(big.Rat).Mul(q.Rat(), unit.BaseFactor()))
}

// amounts renders each group in a display unit chosen only from the units that
// contributed: the largest unit in which the total is at least 1, otherwise the
// smallest contributing unit. The choice depends on the set of units, never on
// order.
func (a *accumulator) amounts() []Amount {
	keys := make([]string, 0, len(a.groups))
	for k := range a.groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Amount, 0, len(keys))
	for _, k := range keys {
		g := a.groups[k]
		if g.kind == ingredients.KindDiscrete {
			out = append(out, Amount{Quantity: quantityFromRat(g.baseTotal), Unit: g.discrete})
			continue
		}
		units := make([]ingredients.Unit, 0, len(g.units))
		for _, u := range g.units {
			units = append(units, u)
		}
		sort.Slice(units, func(i, j int) bool {
			if c := units[i].BaseFactor().Cmp(units[j].BaseFactor()); c != 0 {
				return c > 0 // largest first
			}
			return units[i].Code < units[j].Code
		})
		chosen := units[len(units)-1]
		for _, u := range units {
			inUnit := new(big.Rat).Quo(g.baseTotal, u.BaseFactor())
			if inUnit.Cmp(big.NewRat(1, 1)) >= 0 {
				chosen = u
				break
			}
		}
		out = append(out, Amount{Quantity: quantityFromRat(new(big.Rat).Quo(g.baseTotal, chosen.BaseFactor())), Unit: chosen})
	}
	return out
}

func quantityFromRat(r *big.Rat) ingredients.Quantity {
	return ingredients.NewQuantity(1, 1).MulRat(r)
}

func normalizeCategory(c string) string {
	c = strings.TrimSpace(strings.ToLower(c))
	for _, known := range CategoryOrder {
		if c == known {
			return c
		}
	}
	return "other"
}
