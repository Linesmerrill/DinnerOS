package grocery

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file replaces specialty ingredient lines (meal-kit blends, sauces, and
// concentrates) with what the household chose before aggregation. It is pure:
// the household's choices and batch stock come in as Specialties.
// docs/specialty-ingredients.md explains the model.

// ChoiceType is how the household handles a specialty ingredient.
type ChoiceType string

// Choice types.
const (
	// ChoiceAsIs keeps the specialty ingredient on the list by its own name.
	ChoiceAsIs ChoiceType = "as_is"
	// ChoiceStoreAlternative replaces it with regular ingredients.
	ChoiceStoreAlternative ChoiceType = "store_alternative"
	// ChoiceHouseMadeBatch uses a batch kept in the pantry.
	ChoiceHouseMadeBatch ChoiceType = "house_made_batch"
)

// ViaKind says how a line stands in for a specialty ingredient.
type ViaKind string

// Via kinds.
const (
	ViaStoreAlternative ViaKind = "store_alternative"
	ViaHouseMadeBatch   ViaKind = "house_made_batch"
	// ViaCustomized marks a line changed by a meal customization (package
	// customize): SpecialtyKey and SpecialtyName are the recipe's original
	// line, OptionID the choice ("double", "swap:ground-beef"), and
	// OptionName its label ("2x Ground Beef").
	ViaCustomized ViaKind = "customized"
)

// Via is a line's provenance when it stands in for a specialty ingredient:
// "for Tex-Mex Paste" (a store alternative) or "to make Southwest spice blend"
// (a batch).
type Via struct {
	Kind          ViaKind
	SpecialtyID   string
	SpecialtyKey  string
	SpecialtyName string
	OptionID      string
	OptionName    string
	// Strategy is the household strategy that picked the option, empty when a
	// member chose it explicitly. It lets the list say "(your default)".
	Strategy string
	// Yield is what one batch makes, and Batches how many the list asks for.
	// Both are zero for a store alternative.
	Yield   *Measure
	Batches int
}

// ItemVia is a Via with the recipes it's for.
type ItemVia struct {
	Via
	Recipes []Source
}

// LineSpecialty marks a line (and its item) that is itself a specialty
// ingredient.
type LineSpecialty struct {
	ID   string
	Key  string
	Name string
	// ChoiceType is empty when the household hasn't chosen, ChoiceAsIs, or
	// ChoiceHouseMadeBatch for a batch that's in the pantry.
	ChoiceType ChoiceType
	OptionID   string
	// HouseMade is true when the line is covered by a batch in the pantry.
	HouseMade bool
	// Suggestions are the options to offer when the household hasn't chosen.
	Suggestions []OptionRef
}

// OptionRef names an option a household may choose for a specialty
// ingredient.
type OptionRef struct {
	ID        string
	Type      ChoiceType
	Name      string
	IsDefault bool
}

func (s LineSpecialty) less(o LineSpecialty) bool {
	if s.Key != o.Key {
		return s.Key < o.Key
	}
	return s.OptionID < o.OptionID
}

// Measure is an exact amount in a unit code.
type Measure struct {
	Quantity ingredients.Quantity
	Unit     string
}

// UnitSize says one Unit (a discrete unit such as count) holds Quantity
// SizeUnit.
type UnitSize struct {
	Unit     string
	Quantity ingredients.Quantity
	SizeUnit string
}

// Component is a regular ingredient in a store alternative (per Choice.Per)
// or a batch (for one batch).
type Component struct {
	IngredientKey string
	Name          string
	Category      string
	// Quantity is nil for "to taste".
	Quantity *ingredients.Quantity
	Unit     string
}

// BatchStock is the pantry's batch item.
type BatchStock struct {
	ItemID string
	// Status is in_stock, low, or out (pantry statuses).
	Status string
	// Remaining is the estimated amount in RemainingUnit, or nil when the
	// item has no recorded amount.
	Remaining     *ingredients.Quantity
	RemainingUnit string
	UnitSize      *UnitSize
}

// Choice is the household's chosen option for a specialty ingredient.
type Choice struct {
	Type       ChoiceType
	OptionID   string
	OptionName string
	// Per and Components describe a store alternative: Per of the specialty
	// is Components.
	Per Measure
	// Components are a store alternative's per Per, or one batch's.
	Components []Component
	// Yield is what one batch makes.
	Yield Measure
	// Stock is the batch in the pantry, or nil when there is none.
	Stock *BatchStock
	// PantryKey is the line key the batch item is registered under in the
	// PantryStock ("name:<key>").
	PantryKey string
	// Strategy is the household strategy that picked this option, empty when a
	// member chose it explicitly.
	Strategy string
}

// Specialty is one specialty ingredient and the household's choice.
type Specialty struct {
	// ID is the specialty's stable identifier ("tex-mex-paste").
	ID   string
	Key  string
	Name string
	// Aliases are the other names the same specialty ingredient goes by
	// ("Sichuan Paste" for "Szechuan Paste"). Cooking instructions look for
	// them in step text; the grocery list doesn't use them.
	Aliases []string
	// UnitSizes convert its discrete units ("1 count = 2 tsp").
	UnitSizes []UnitSize
	// Choice is nil when the household hasn't chosen.
	Choice *Choice
	// Suggestions are the options offered when Choice is nil.
	Suggestions []OptionRef
}

// Specialties maps Line.IngredientKey to the specialty ingredient it is.
type Specialties map[string]*Specialty

// BatchStatus says whether the week needs a batch made.
type BatchStatus string

// Batch statuses.
const (
	BatchInPantry BatchStatus = "inPantry"
	BatchMake     BatchStatus = "make"
)

// BatchReason explains a BatchStatus.
type BatchReason string

// Batch reasons.
const (
	// BatchEnough: in stock, and the estimate covers the week.
	BatchEnough BatchReason = "enough"
	// BatchInStock: in stock, and the amounts can't be compared (no recorded
	// amount, a recipe without an amount, or units that don't convert).
	BatchInStock BatchReason = "inStock"
	// BatchNotEnough: in stock, but the estimate doesn't cover the week.
	BatchNotEnough BatchReason = "notEnough"
	BatchLow       BatchReason = "low"
	BatchOut       BatchReason = "out"
	BatchMissing   BatchReason = "missing"
)

// BatchPlan is a house-made specialty ingredient the week uses.
type BatchPlan struct {
	SpecialtyID   string
	SpecialtyKey  string
	SpecialtyName string
	OptionID      string
	OptionName    string
	Yield         Measure
	Status        BatchStatus
	Reason        BatchReason
	// Batches is how many batches to make; 0 when in the pantry.
	Batches int
	// ItemID is the batch's pantry item, or "" when there is none.
	ItemID string
	// Remaining is the pantry estimate, or nil. Needed is the week's total in
	// Yield's unit, or nil when it can't be known.
	Remaining *Measure
	Needed    *Measure
	Recipes   []Source
}

// ApplySpecialties returns selections with specialty ingredient lines handled
// as the household chose, and the house-made batches the week uses. It
// doesn't change its arguments.
//
//   - No choice, or ChoiceAsIs: the line stays, marked with Specialty.
//   - A store alternative: the line becomes its components, scaled by the
//     line's amount (converted to Per's unit, through the specialty's unit
//     sizes when counted in packets), each with a Via. When the amount is
//     missing or doesn't convert, the components are listed without amounts.
//   - A house-made batch: the week's lines are totaled. If the batch is in
//     stock and its estimate covers the total (or can't be compared), the
//     lines stay under the batch's PantryKey, marked HouseMade. Otherwise they
//     are replaced by the components of enough batches to cover the shortfall
//     (at least one), for the recipes that need it.
//
// The result doesn't depend on input order.
func ApplySpecialties(selections []RecipeSelection, specs Specialties) ([]RecipeSelection, []BatchPlan, error) {
	out := make([]RecipeSelection, len(selections))
	batches := map[string]*batchAcc{}
	for i, sel := range selections {
		out[i] = sel
		out[i].Lines = make([]Line, 0, len(sel.Lines))
		if sel.RecipeServings <= 0 || sel.TargetServings <= 0 {
			return nil, nil, fmt.Errorf("%w: recipe %q servings %d → %d", ErrInvalidSelection, sel.RecipeID, sel.RecipeServings, sel.TargetServings)
		}
		factor := big.NewRat(int64(sel.TargetServings), int64(sel.RecipeServings))
		for _, line := range sel.Lines {
			spec := specs[strings.TrimSpace(line.IngredientKey)]
			if spec == nil {
				out[i].Lines = append(out[i].Lines, line)
				continue
			}
			choice := spec.Choice
			switch {
			case choice == nil || choice.Type == ChoiceAsIs:
				marked := line
				marked.Specialty = &LineSpecialty{ID: spec.ID, Key: spec.Key, Name: spec.Name, Suggestions: spec.Suggestions}
				if choice != nil {
					marked.Specialty.ChoiceType, marked.Specialty.OptionID, marked.Specialty.Suggestions = ChoiceAsIs, choice.OptionID, nil
				}
				out[i].Lines = append(out[i].Lines, marked)
			case choice.Type == ChoiceStoreAlternative:
				out[i].Lines = append(out[i].Lines, storeLines(line, spec, choice)...)
			case choice.Type == ChoiceHouseMadeBatch:
				acc, ok := batches[spec.Key]
				if !ok {
					acc = &batchAcc{spec: spec, need: new(big.Rat), needKnown: true, sources: map[string]Source{}}
					batches[spec.Key] = acc
				}
				acc.add(i, line, factor, sel)
			default:
				return nil, nil, fmt.Errorf("%w: specialty %q has unknown choice type %q", ErrInvalidSelection, spec.Key, choice.Type)
			}
		}
	}

	keys := make([]string, 0, len(batches))
	for k := range batches {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	plans := make([]BatchPlan, 0, len(keys))
	for _, k := range keys {
		acc := batches[k]
		plan := acc.plan()
		plans = append(plans, plan)
		spec, choice := acc.spec, acc.spec.Choice
		if plan.Status == BatchInPantry {
			for _, ref := range acc.lines {
				kept := ref.line
				kept.IngredientKey = choice.PantryKey
				kept.Specialty = &LineSpecialty{ID: spec.ID, Key: spec.Key, Name: spec.Name, ChoiceType: ChoiceHouseMadeBatch, OptionID: choice.OptionID, HouseMade: true}
				out[ref.selection].Lines = append(out[ref.selection].Lines, kept)
			}
			continue
		}
		yield := choice.Yield
		via := &Via{
			Kind: ViaHouseMadeBatch, SpecialtyID: spec.ID, SpecialtyKey: spec.Key, SpecialtyName: spec.Name,
			OptionID: choice.OptionID, OptionName: choice.OptionName, Strategy: choice.Strategy,
			Yield: &yield, Batches: plan.Batches,
		}
		made := RecipeSelection{RecipeID: "batch:" + spec.Key, RecipeName: choice.OptionName, RecipeServings: 1, TargetServings: 1}
		for _, c := range choice.Components {
			l := componentLine(c, big.NewRat(int64(plan.Batches), 1), true, via)
			l.Sources = plan.Recipes
			made.Lines = append(made.Lines, l)
		}
		out = append(out, made)
	}
	return out, plans, nil
}

// storeLines replaces a line with a store alternative's components.
func storeLines(line Line, spec *Specialty, choice *Choice) []Line {
	var ratio *big.Rat
	if line.Quantity != nil && !line.Quantity.IsZero() && !choice.Per.Quantity.IsZero() {
		if inPer, ok := ConvertMeasure(line.Quantity.Rat(), line.UnitCode, choice.Per.Unit, spec.UnitSizes); ok {
			ratio = inPer.Quo(inPer, choice.Per.Quantity.Rat())
		}
	}
	via := &Via{
		Kind: ViaStoreAlternative, SpecialtyID: spec.ID, SpecialtyKey: spec.Key, SpecialtyName: spec.Name,
		OptionID: choice.OptionID, OptionName: choice.OptionName, Strategy: choice.Strategy,
	}
	lines := make([]Line, 0, len(choice.Components))
	for _, c := range choice.Components {
		lines = append(lines, componentLine(c, ratio, ratio != nil, via))
	}
	return lines
}

// componentLine is a component scaled by factor, or without an amount when
// known is false.
func componentLine(c Component, factor *big.Rat, known bool, via *Via) Line {
	l := Line{IngredientKey: c.IngredientKey, Name: c.Name, Category: c.Category, Via: via}
	if known && c.Quantity != nil && !c.Quantity.IsZero() && c.Unit != "" {
		q := c.Quantity.MulRat(factor)
		l.Quantity, l.UnitCode = &q, c.Unit
	}
	return l
}

type lineRef struct {
	selection int
	line      Line
}

type batchAcc struct {
	spec *Specialty
	// need is the week's total in the yield's unit; needKnown is false when a
	// line has no amount or doesn't convert.
	need      *big.Rat
	needKnown bool
	sources   map[string]Source
	lines     []lineRef
}

func (b *batchAcc) sizes() []UnitSize {
	sizes := b.spec.UnitSizes
	if st := b.spec.Choice.Stock; st != nil && st.UnitSize != nil {
		sizes = append([]UnitSize{*st.UnitSize}, sizes...)
	}
	return sizes
}

func (b *batchAcc) add(selection int, line Line, factor *big.Rat, sel RecipeSelection) {
	b.lines = append(b.lines, lineRef{selection: selection, line: line})
	srcs := line.Sources
	if len(srcs) == 0 {
		srcs = []Source{{RecipeID: sel.RecipeID, RecipeName: sel.RecipeName}}
	}
	for _, s := range srcs {
		b.sources[s.RecipeID] = s
	}
	if line.Quantity == nil || line.Quantity.IsZero() {
		b.needKnown = false
		return
	}
	amount := new(big.Rat).Mul(line.Quantity.Rat(), factor)
	inYield, ok := ConvertMeasure(amount, line.UnitCode, b.spec.Choice.Yield.Unit, b.sizes())
	if !ok {
		b.needKnown = false
		return
	}
	b.need.Add(b.need, inYield)
}

func (b *batchAcc) plan() BatchPlan {
	spec, choice := b.spec, b.spec.Choice
	p := BatchPlan{
		SpecialtyID: spec.ID, SpecialtyKey: spec.Key, SpecialtyName: spec.Name, OptionID: choice.OptionID, OptionName: choice.OptionName,
		Yield: choice.Yield,
	}
	for _, s := range b.sources {
		p.Recipes = append(p.Recipes, s)
	}
	sort.Slice(p.Recipes, func(i, j int) bool {
		if p.Recipes[i].RecipeName != p.Recipes[j].RecipeName {
			return p.Recipes[i].RecipeName < p.Recipes[j].RecipeName
		}
		return p.Recipes[i].RecipeID < p.Recipes[j].RecipeID
	})
	if b.needKnown {
		p.Needed = &Measure{Quantity: quantityFromRat(b.need), Unit: choice.Yield.Unit}
	}

	// remaining is the stock in the yield's unit, when known and not out.
	var remaining *big.Rat
	st := choice.Stock
	if st != nil {
		p.ItemID = st.ItemID
		if st.Remaining != nil {
			p.Remaining = &Measure{Quantity: *st.Remaining, Unit: st.RemainingUnit}
			if st.Status != "out" {
				remaining, _ = ConvertMeasure(st.Remaining.Rat(), st.RemainingUnit, choice.Yield.Unit, b.sizes())
			}
		}
	}
	switch {
	case st == nil:
		p.Status, p.Reason = BatchMake, BatchMissing
	case st.Status == "out":
		p.Status, p.Reason = BatchMake, BatchOut
	case st.Status == "low":
		p.Status, p.Reason = BatchMake, BatchLow
	case st.Remaining == nil || !b.needKnown || remaining == nil:
		p.Status, p.Reason = BatchInPantry, BatchInStock
	case remaining.Cmp(b.need) >= 0:
		p.Status, p.Reason = BatchInPantry, BatchEnough
	default:
		p.Status, p.Reason = BatchMake, BatchNotEnough
	}
	if p.Status == BatchMake {
		p.Batches = 1
		yield := choice.Yield.Quantity.Rat()
		if b.needKnown && yield.Sign() > 0 {
			short := new(big.Rat).Set(b.need)
			if remaining != nil {
				short.Sub(short, remaining)
			}
			if short.Sign() > 0 {
				n := new(big.Rat).Quo(short, yield)
				count := new(big.Int).Quo(n.Num(), n.Denom())
				if !n.IsInt() {
					count.Add(count, big.NewInt(1))
				}
				p.Batches = max(1, int(count.Int64()))
			}
		}
	}
	return p
}

// ConvertMeasure converts q from one unit code to another exactly, through
// sizes for discrete units: with "1 count = 2 tsp", 3 count is 2 tbsp and
// 1 tbsp is 3/2 count. A discrete unit may carry one size per kind ("1 count
// = 1 oz" and "1 count = 2 tbsp"), so a packet a recipe measures in either
// weight or volume converts; weight and volume never convert to each other
// directly. ok is false when no exact conversion exists.
func ConvertMeasure(q *big.Rat, from, to string, sizes []UnitSize) (*big.Rat, bool) {
	if from == to {
		return new(big.Rat).Set(q), true
	}
	fu, err := ingredients.LookupUnit(from)
	if err != nil {
		return nil, false
	}
	tu, err := ingredients.LookupUnit(to)
	if err != nil {
		return nil, false
	}
	type measured struct {
		amount *big.Rat
		unit   ingredients.Unit
	}
	var sources []measured
	if !fu.Discrete() {
		sources = append(sources, measured{q, fu})
	}
	for _, size := range sizes {
		if su, err := ingredients.LookupUnit(size.SizeUnit); fu.Discrete() && size.Unit == from && err == nil && !su.Discrete() {
			sources = append(sources, measured{new(big.Rat).Mul(q, size.Quantity.Rat()), su})
		}
	}
	for _, src := range sources {
		if !tu.Discrete() {
			if src.unit.CanConvertTo(tu) {
				return scaleUnit(src.amount, src.unit, tu), true
			}
			continue
		}
		for _, size := range sizes {
			su, err := ingredients.LookupUnit(size.SizeUnit)
			if size.Unit != to || err != nil || size.Quantity.IsZero() || su.Discrete() || !src.unit.CanConvertTo(su) {
				continue
			}
			inSize := scaleUnit(src.amount, src.unit, su)
			return inSize.Quo(inSize, size.Quantity.Rat()), true
		}
	}
	return nil, false
}

// scaleUnit converts q between two convertible units of the same kind.
func scaleUnit(q *big.Rat, from, to ingredients.Unit) *big.Rat {
	r := new(big.Rat).Mul(q, from.BaseFactor())
	return r.Quo(r, to.BaseFactor())
}

func viaKey(v Via) string {
	return string(v.Kind) + "\x00" + v.SpecialtyKey + "\x00" + v.OptionID
}

func (a *accumulator) addVia(v *Via, sources []Source) {
	if v == nil {
		return
	}
	k := viaKey(*v)
	iv, ok := a.via[k]
	if !ok {
		iv = &ItemVia{Via: *v}
		a.via[k] = iv
	}
	for _, s := range sources {
		if !containsSource(iv.Recipes, s) {
			iv.Recipes = append(iv.Recipes, s)
		}
	}
}

func containsSource(list []Source, s Source) bool {
	for _, x := range list {
		if x.RecipeID == s.RecipeID {
			return true
		}
	}
	return false
}

// itemVia returns the item's provenance sorted by kind, specialty, and option.
func (a *accumulator) itemVia() []ItemVia {
	if len(a.via) == 0 {
		return nil
	}
	keys := make([]string, 0, len(a.via))
	for k := range a.via {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ItemVia, 0, len(keys))
	for _, k := range keys {
		iv := *a.via[k]
		iv.Recipes = append([]Source(nil), iv.Recipes...)
		sort.Slice(iv.Recipes, func(i, j int) bool {
			if iv.Recipes[i].RecipeName != iv.Recipes[j].RecipeName {
				return iv.Recipes[i].RecipeName < iv.Recipes[j].RecipeName
			}
			return iv.Recipes[i].RecipeID < iv.Recipes[j].RecipeID
		})
		out = append(out, iv)
	}
	return out
}
