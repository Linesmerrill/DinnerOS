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
	// StatusFromFreezer is covered by a frozen pantry item: a bulk pack an
	// earlier week portioned and sealed (docs/pantry-usage.md#the-freezer).
	// The item stays on the list, de-emphasized, so the household is told to
	// grab it rather than having it silently disappear — and no export buys
	// it again.
	StatusFromFreezer Status = "fromFreezer"
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
	// SkipRecipe leaves the ingredient out of one recipe, every time that
	// recipe is planned, while other recipes on the list still get it
	// (docs/grocery-engine.md#skipped-ingredients). It applies to lines, not
	// to a whole item: a skipped item with this scope holds only the left-out
	// recipes' share, and the same ingredient can be on the list for the
	// others.
	SkipRecipe SkipScope = "recipe"
)

// Outranks reports whether s wins over other when both skip one ingredient:
// always beats week, and either beats recipe, since an ingredient-wide skip
// already covers every recipe.
func (s SkipScope) Outranks(other SkipScope) bool { return s.rank() > other.rank() }

func (s SkipScope) rank() int {
	switch s {
	case SkipAlways:
		return 3
	case SkipThisWeek:
		return 2
	case SkipRecipe:
		return 1
	}
	return 0
}

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
	// OnHand is set when the pantry has some but not enough: what's at home.
	// The item is then toBuy, and its convertible amount is the rest still
	// to buy (decision 623).
	OnHand *Amount
	// Needed is what the week needs in all, set with OnHand: the list says
	// "20 oz, you have 12 oz", since nobody buys 8 oz of pork.
	Needed *Amount
	// Unsure is set when the pantry has the ingredient but can't tell how
	// much: no amount recorded, a cooked use it couldn't count, or an amount
	// it can't compare with the recipe's. The item is then toBuy: assuming
	// there's enough is the failure that leaves a cook short (decision 629).
	Unsure bool
	// Shares split the item by the recipe each part is for, in the order of
	// Sources: how much of it each meal needs. The list shows an item under
	// every meal that uses it, with that meal's own amount, while it stays one
	// purchase.
	Shares []Share
}

// Share is the part of an item one recipe needs.
type Share struct {
	Source
	// Amounts is this recipe's amount for its planned servings, one entry per
	// group of convertible units, like Item.Amounts. Empty when the recipe
	// gave no amount, or when the share is Combined.
	Amounts      []Amount
	Unquantified bool
	// Combined is true when the line was made for several recipes together
	// (a house-made batch), so no amount can be split out for one of them.
	Combined bool
	// Extra is true when the share is a line added for this meal directly,
	// such as an accepted pairing, rather than one of its ingredients.
	Extra bool
	// Component is set when this share is part of a component of the meal: a
	// specialty ingredient (a sauce, crema, paste, or blend) the household
	// makes from store ingredients. A meal can need the same ingredient on its
	// own and in a component; those are two shares, so leaving the component
	// out removes only its part.
	Component *ShareComponent
}

// ShareComponent names the component a share belongs to.
type ShareComponent struct {
	SpecialtyID   string
	SpecialtyKey  string
	SpecialtyName string
	// LineKey is the recipe line the component replaced, when known: the key
	// to leave out to leave out the whole component.
	LineKey string
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

// FrozenPantry is a Pantry that also knows which ingredients the household
// has in the freezer. Aggregate gives those lines StatusFromFreezer: still on
// the list, but grabbed rather than bought.
type FrozenPantry interface {
	Pantry
	Frozen(ingredientKey string) bool
}

// StockedPantry is a Pantry that also knows how much of an ingredient is at
// home. An in-stock or frozen item whose amount the week needs more than that
// is bought: the rest, not none.
type StockedPantry interface {
	Pantry
	OnHand(ingredientKey string) (Amount, bool)
	// Unmeasured reports an ingredient at home whose amount can't be
	// trusted: none recorded, or a cooked use since it was set that couldn't
	// be counted. Staples are never unmeasured; they keep their own line.
	Unmeasured(ingredientKey string) bool
	// ConvertNeed puts a recipe amount in the unit the pantry tracks the
	// ingredient in when the units alone can't ("1 packet" or "2 tsp" of
	// tomato paste against a can in ounces): the same kitchen measures and
	// densities cooking deducts with.
	ConvertNeed(ingredientKey, name string, need Amount, to ingredients.Unit) (*big.Rat, bool)
}

// PantryStock is a household pantry snapshot. Keys match Line.IngredientKey.
// OutOfStock holds everything the household marked low or out: both mean buy
// it. InFreezer holds what is in stock in the freezer; those lines are neither
// bought nor hidden. Ingredients in no set (unknown to the pantry) get the
// engine's default status: pantryHint when every source flags them as staples,
// otherwise toBuy.
type PantryStock struct {
	InStock    map[string]bool
	OutOfStock map[string]bool
	InFreezer  map[string]bool
	// Amounts are how much of an in-stock or frozen ingredient is at home,
	// when every item for it has a known amount; absent means unknown.
	Amounts map[string]Amount
	// Uncertain holds in-stock keys whose amount can't be trusted
	// (StockedPantry.Unmeasured).
	Uncertain map[string]bool
	// Convert backs ConvertNeed; nil converts nothing beyond the units.
	Convert func(ingredientKey, name string, need Amount, to ingredients.Unit) (*big.Rat, bool)
}

// Unmeasured implements StockedPantry.
func (p PantryStock) Unmeasured(key string) bool { return p.Uncertain[key] }

// ConvertNeed implements StockedPantry.
func (p PantryStock) ConvertNeed(key, name string, need Amount, to ingredients.Unit) (*big.Rat, bool) {
	if p.Convert == nil {
		return nil, false
	}
	return p.Convert(key, name, need, to)
}

// OnHand implements StockedPantry.
func (p PantryStock) OnHand(key string) (Amount, bool) {
	a, ok := p.Amounts[key]
	return a, ok
}

// Has implements Pantry.
func (p PantryStock) Has(key string) bool { return p.InStock[key] }

// Out implements OutPantry.
func (p PantryStock) Out(key string) bool { return p.OutOfStock[key] }

// Frozen implements FrozenPantry.
func (p PantryStock) Frozen(key string) bool { return p.InFreezer[key] }

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

// RecipeSkips is Skips that also knows the ingredients a household leaves out
// of one recipe (SkipRecipe). AggregateWith holds back only that recipe's
// lines for the ingredient; the same ingredient from other recipes stays on
// the list, with its amount reduced to theirs.
type RecipeSkips interface {
	Skips
	// SkipInRecipe reports whether the household leaves ingredientKey out of
	// recipeID.
	SkipInRecipe(recipeID, ingredientKey string) bool
}

// SkipRules is the household's skips in the form AggregateWith takes:
// ingredient-wide skips (week and always) by key, and recipe skips by recipe
// ID, then key. Both are registered under every key the ingredient can carry.
type SkipRules struct {
	Ingredients SkipSet
	Recipes     map[string]map[string]bool
}

// Skip implements Skips.
func (r SkipRules) Skip(key string) (SkipScope, bool) { return r.Ingredients.Skip(key) }

// SkipInRecipe implements RecipeSkips.
func (r SkipRules) SkipInRecipe(recipeID, key string) bool { return r.Recipes[recipeID][key] }

// LeftOut is why one ingredient of a recipe is left out when it is cooked: a
// skip for that recipe, or one for every recipe. A week skip is about buying
// and is never a LeftOut.
type LeftOut struct {
	SkipID string
	Scope  SkipScope
}

// LeftOutSet is what a household leaves out of one recipe, registered under
// every key a line for the ingredient can carry. Cooking instructions read it
// to mark a mention, and cook deductions to skip a line.
type LeftOutSet map[string]LeftOut

// Lookup returns the entry for the first of keys that has one.
func (s LeftOutSet) Lookup(keys ...string) (LeftOut, bool) {
	for _, k := range keys {
		if lo, ok := s[k]; ok && k != "" {
			return lo, true
		}
	}
	return LeftOut{}, false
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
	groups              unitGroups
	sources             map[string]Source
	via                 map[string]*ItemVia
	specialty           *LineSpecialty
	extras              map[string]Extra
	shares              map[shareKey]*shareAccumulator
}

// shareKey identifies one recipe's part of an item. A recipe can need an
// ingredient itself and have it added as a pairing too; those are two shares.
type shareKey struct {
	recipeID  string
	extra     bool
	component string
}

// shareAccumulator sums one recipe's lines for an item.
type shareAccumulator struct {
	source       Source
	groups       unitGroups
	unquantified bool
	combined     bool
	extra        bool
	component    *ShareComponent
}

// unitGroups sums quantities by group of convertible units.
type unitGroups map[string]*unitGroup

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
	frozenPantry, _ := pantry.(FrozenPantry)
	stockedPantry, _ := pantry.(StockedPantry)
	isFrozen := func(key string) bool { return frozenPantry != nil && frozenPantry.Frozen(key) }
	acc := map[string]*accumulator{}
	// held are the parts of items left out line by line — of one recipe, or
	// as part of a left-out component — aggregated the same way and reported
	// in SkippedItems, by key and scope.
	held := map[string]*accumulator{}
	heldScope := map[string]SkipScope{}

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
			lineSources := line.Sources
			if len(lineSources) == 0 {
				lineSources = []Source{{RecipeID: sel.RecipeID, RecipeName: sel.RecipeName}}
			}
			into, accKey := acc, key
			if scope, ok := lineHeld(skips, key, line.Via, lineSources); ok {
				into, accKey = held, key+"\x00"+string(scope)
				heldScope[accKey] = scope
			}
			if err := accumulate(into, accKey, key, line, lineSources, sel, factor); err != nil {
				return List{}, err
			}
		}
	}

	list := List{Items: make([]Item, 0, len(acc))}
	for _, a := range acc {
		item := a.item()
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
		// The freezer beats both the staple hint and buying it again, but
		// not a fresh item already in the pantry: that one is nearer to hand.
		case isFrozen(a.key):
			item.Status = StatusFromFreezer
		case a.allHinted && !isOut(a.key):
			item.Status = StatusPantryHint
		default:
			item.Status = StatusToBuy
		}
		if isSkipped {
			list.SkippedItems = append(list.SkippedItems, item)
			continue
		}
		if item.Status == StatusInPantry || item.Status == StatusFromFreezer {
			shortOf(&item, stockedPantry)
		}
		list.Items = append(list.Items, item)
	}
	// A recipe-skipped part is reported beside the rest of the item, not
	// merged into it: the list line shrinks to the other recipes' amount, and
	// this item says how much was left out, and of which meals.
	for k, a := range held {
		item := a.item()
		item.Status, item.SkipScope = StatusSkipped, heldScope[k]
		list.SkippedItems = append(list.SkippedItems, item)
	}

	sortItems(list.Items)
	sortItems(list.SkippedItems)
	return list, nil
}

// lineHeld decides whether one line is left out on its own, and by which
// scope, before lines are merged into items.
//
//   - A skip of the line's own ingredient (week or always) is not decided
//     here: it covers the whole item, and the item says so.
//   - A line that is part of a component (a store alternative's or a batch's
//     ingredient) is left out when the component is skipped for the week or
//     always, whichever outranks.
//   - Otherwise it is left out when every recipe it is for leaves out its
//     ingredient, or its component (SkipRecipe). A line for several recipes
//     (a batch) stays while any of them still wants it.
func lineHeld(skips Skips, key string, via *Via, sources []Source) (SkipScope, bool) {
	if skips == nil {
		return "", false
	}
	if _, ok := skips.Skip(key); ok {
		return "", false
	}
	componentKeys := via.componentKeys()
	var best SkipScope
	for _, ck := range componentKeys {
		if scope, ok := skips.Skip(ck); ok && scope.Outranks(best) {
			best = scope
		}
	}
	if best != "" {
		return best, true
	}
	recipeSkips, ok := skips.(RecipeSkips)
	if !ok || len(sources) == 0 {
		return "", false
	}
	for _, src := range sources {
		if !skipsInRecipe(recipeSkips, src.RecipeID, key, componentKeys) {
			return "", false
		}
	}
	return SkipRecipe, true
}

func skipsInRecipe(skips RecipeSkips, recipeID, key string, componentKeys []string) bool {
	if skips.SkipInRecipe(recipeID, key) {
		return true
	}
	for _, ck := range componentKeys {
		if skips.SkipInRecipe(recipeID, ck) {
			return true
		}
	}
	return false
}

// accumulate adds one line, for the ingredient key, to the item at accKey in
// acc.
func accumulate(acc map[string]*accumulator, accKey, key string, line Line, lineSources []Source, sel RecipeSelection, factor ingredients.Quantity) error {
	a, ok := acc[accKey]
	if !ok {
		a = &accumulator{
			key: key, name: line.Name, category: line.Category, allHinted: true, groups: unitGroups{},
			sources: map[string]Source{}, via: map[string]*ItemVia{}, shares: map[shareKey]*shareAccumulator{},
		}
		acc[accKey] = a
	}
	// Deterministic name/category choice independent of input order.
	if a.name == "" || (line.Name != "" && line.Name < a.name) {
		a.name = line.Name
	}
	if a.category == "" || (line.Category != "" && line.Category < a.category) {
		a.category = line.Category
	}
	a.allHinted = a.allHinted && line.PantryStaple
	for _, src := range lineSources {
		a.sources[src.RecipeID] = src
	}
	a.addVia(line.Via, lineSources)
	a.addExtra(line.Extra)
	if s := line.Specialty; s != nil && (a.specialty == nil || s.less(*a.specialty)) {
		c := *s
		a.specialty = &c
	}
	var component *ShareComponent
	if v := line.Via; v.IsComponent() {
		component = &ShareComponent{SpecialtyID: v.SpecialtyID, SpecialtyKey: v.SpecialtyKey, SpecialtyName: v.SpecialtyName, LineKey: v.LineKey}
	}
	shares := make([]*shareAccumulator, 0, len(lineSources))
	for _, src := range lineSources {
		k := shareKey{recipeID: src.RecipeID, extra: line.Extra != nil}
		if component != nil {
			k.component = component.SpecialtyKey
		}
		sh := a.shares[k]
		if sh == nil {
			sh = &shareAccumulator{source: src, groups: unitGroups{}, extra: k.extra, component: component}
			a.shares[k] = sh
		}
		if len(lineSources) > 1 {
			sh.combined = true
		}
		shares = append(shares, sh)
	}

	if line.Quantity == nil || line.Quantity.IsZero() || line.UnitCode == "" {
		a.unquantified = true
		for _, sh := range shares {
			sh.unquantified = true
		}
		return nil
	}
	unit, err := ingredients.LookupUnit(line.UnitCode)
	if err != nil {
		return fmt.Errorf("%w: recipe %q ingredient %q: %w", ErrInvalidSelection, sel.RecipeID, line.Name, err)
	}
	scaled := line.Quantity.Mul(factor)
	a.groups.add(scaled, unit)
	// A line for several recipes at once can't be split between them.
	if len(shares) == 1 {
		shares[0].groups.add(scaled, unit)
	}
	return nil
}

// item renders the accumulated item, without a status.
func (a *accumulator) item() Item {
	item := Item{
		IngredientKey: a.key,
		Name:          a.name,
		Category:      normalizeCategory(a.category),
		Unquantified:  a.unquantified,
		Amounts:       a.groups.amounts(),
		Via:           a.itemVia(),
		Specialty:     a.specialty,
		Extras:        a.itemExtras(),
	}
	for _, s := range a.sources {
		item.Sources = append(item.Sources, s)
	}
	sort.Slice(item.Sources, func(i, j int) bool { return sourceLess(item.Sources[i], item.Sources[j]) })
	for _, sh := range a.shares {
		share := Share{Source: sh.source, Unquantified: sh.unquantified, Combined: sh.combined, Extra: sh.extra, Component: sh.component}
		if !sh.combined {
			share.Amounts = sh.groups.amounts()
		}
		item.Shares = append(item.Shares, share)
	}
	sort.Slice(item.Shares, func(i, j int) bool {
		a, b := item.Shares[i], item.Shares[j]
		if a.Source != b.Source {
			return sourceLess(a.Source, b.Source)
		}
		if a.Extra != b.Extra {
			return !a.Extra
		}
		return componentName(a.Component) < componentName(b.Component)
	})
	return item
}

func componentName(c *ShareComponent) string {
	if c == nil {
		return ""
	}
	return c.SpecialtyName + "\x00" + c.SpecialtyKey
}

func sourceLess(a, b Source) bool {
	if a.RecipeName != b.RecipeName {
		return a.RecipeName < b.RecipeName
	}
	return a.RecipeID < b.RecipeID
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

func (groups unitGroups) add(q ingredients.Quantity, unit ingredients.Unit) {
	groupKey := string(unit.Kind)
	if unit.Discrete() {
		groupKey = "discrete:" + unit.Code
	}
	g, ok := groups[groupKey]
	if !ok {
		g = &unitGroup{kind: unit.Kind, discrete: unit, baseTotal: new(big.Rat), units: map[string]ingredients.Unit{}}
		groups[groupKey] = g
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
func (groups unitGroups) amounts() []Amount {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Amount, 0, len(keys))
	for _, k := range keys {
		g := groups[k]
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

// shortOf turns a covered item into one to buy when the week needs more than
// is at home: "20 oz ground pork" with 12 oz in the freezer buys 8 oz. Every
// amount is put in the pantry's unit, by units or else by the pantry's
// kitchen measures and densities. When that can't be done, or the pantry
// can't say how much it has, the item is bought and marked Unsure: never
// assumed to be enough (decision 629). A pantry that knows no amounts at all
// (not a StockedPantry) leaves the item covered.
func shortOf(item *Item, pantry StockedPantry) {
	if pantry == nil {
		return
	}
	if pantry.Unmeasured(item.IngredientKey) {
		unsure(item)
		return
	}
	have, ok := pantry.OnHand(item.IngredientKey)
	if !ok {
		// A staple, or an ingredient the pantry has no item for by this key.
		return
	}
	if len(item.Amounts) == 0 {
		if item.Unquantified {
			return // "salt to taste": having some is having enough.
		}
		unsure(item)
		return
	}
	total := new(big.Rat)
	for _, need := range item.Amounts {
		var inHave *big.Rat
		if need.Unit.CanConvertTo(have.Unit) {
			if q, err := ingredients.Convert(need.Quantity, need.Unit, have.Unit); err == nil {
				inHave = q.Rat()
			}
		}
		if inHave == nil {
			inHave, ok = pantry.ConvertNeed(item.IngredientKey, item.Name, need, have.Unit)
			if !ok {
				unsure(item)
				return
			}
		}
		total.Add(total, inHave)
	}
	if total.Cmp(have.Quantity.Rat()) <= 0 {
		return
	}
	// The rest, as a share of each amount in its own unit.
	share := new(big.Rat).Quo(new(big.Rat).Sub(total, have.Quantity.Rat()), total)
	if len(item.Amounts) == 1 {
		needed := item.Amounts[0]
		item.Needed = &needed
		if item.Amounts[0].Unit.CanConvertTo(have.Unit) {
			// In the need's unit, so the line reads "2 lb of the 2 ¼ lb", not "32 oz".
			if q, err := ingredients.Convert(have.Quantity, have.Unit, needed.Unit); err == nil {
				have = Amount{Quantity: q, Unit: needed.Unit}
			}
		}
	}
	for i, need := range item.Amounts {
		item.Amounts[i] = Amount{Quantity: need.Quantity.MulRat(share), Unit: need.Unit}
	}
	item.Status = StatusToBuy
	item.OnHand = &have
}

// unsure buys an item the pantry has but can't measure.
func unsure(item *Item) {
	item.Status = StatusToBuy
	item.Unsure = true
}
