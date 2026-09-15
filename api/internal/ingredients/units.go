package ingredients

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
)

// Errors returned by unit lookups and conversions.
var (
	ErrUnknownUnit       = errors.New("unknown unit")
	ErrIncompatibleUnits = errors.New("incompatible units")
)

// Kind groups units that measure the same physical dimension.
type Kind string

// Unit kinds. Discrete units (cloves, cans, packages) only combine with the
// same unit; they never convert to each other or to mass/volume.
const (
	KindDiscrete Kind = "discrete"
	KindVolume   Kind = "volume"
	KindMass     Kind = "mass"
)

// Unit is a measurement unit identified by a stable code (see
// docs/grocery-engine.md and docs/import-format.md).
type Unit struct {
	Code     string
	Kind     Kind
	Singular string
	Plural   string
	// toBase is the exact factor to the kind's base unit (ml for volume, g for
	// mass). Nil for discrete units.
	toBase *big.Rat
}

func rat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic("ingredients: bad rational " + s)
	}
	return r
}

// Exact US customary definitions: 1 US fluid ounce = 29.5735295625 ml,
// 1 avoirdupois ounce = 28.349523125 g.
var (
	mlPerFlOz = rat("29.5735295625")
	gPerOz    = rat("28.349523125")
)

var units = func() map[string]Unit {
	mul := func(a, b *big.Rat) *big.Rat { return new(big.Rat).Mul(a, b) }
	list := []Unit{
		{Code: "count", Kind: KindDiscrete, Singular: "", Plural: ""},
		{Code: "clove", Kind: KindDiscrete, Singular: "clove", Plural: "cloves"},
		{Code: "can", Kind: KindDiscrete, Singular: "can", Plural: "cans"},
		{Code: "package", Kind: KindDiscrete, Singular: "package", Plural: "packages"},
		{Code: "slice", Kind: KindDiscrete, Singular: "slice", Plural: "slices"},
		{Code: "bunch", Kind: KindDiscrete, Singular: "bunch", Plural: "bunches"},
		{Code: "pinch", Kind: KindDiscrete, Singular: "pinch", Plural: "pinches"},
		{Code: "thumb", Kind: KindDiscrete, Singular: "thumb", Plural: "thumbs"},

		{Code: "tsp", Kind: KindVolume, Singular: "tsp", Plural: "tsp", toBase: mul(mlPerFlOz, big.NewRat(1, 6))},
		{Code: "tbsp", Kind: KindVolume, Singular: "tbsp", Plural: "tbsp", toBase: mul(mlPerFlOz, big.NewRat(1, 2))},
		{Code: "floz", Kind: KindVolume, Singular: "fl oz", Plural: "fl oz", toBase: mlPerFlOz},
		{Code: "cup", Kind: KindVolume, Singular: "cup", Plural: "cups", toBase: mul(mlPerFlOz, big.NewRat(8, 1))},
		{Code: "ml", Kind: KindVolume, Singular: "ml", Plural: "ml", toBase: big.NewRat(1, 1)},
		{Code: "l", Kind: KindVolume, Singular: "l", Plural: "l", toBase: big.NewRat(1000, 1)},

		{Code: "oz", Kind: KindMass, Singular: "oz", Plural: "oz", toBase: gPerOz},
		{Code: "lb", Kind: KindMass, Singular: "lb", Plural: "lb", toBase: mul(gPerOz, big.NewRat(16, 1))},
		{Code: "g", Kind: KindMass, Singular: "g", Plural: "g", toBase: big.NewRat(1, 1)},
		{Code: "kg", Kind: KindMass, Singular: "kg", Plural: "kg", toBase: big.NewRat(1000, 1)},
	}
	m := make(map[string]Unit, len(list))
	for _, u := range list {
		m[u.Code] = u
	}
	return m
}()

// LookupUnit returns the unit for a code.
func LookupUnit(code string) (Unit, error) {
	u, ok := units[code]
	if !ok {
		return Unit{}, fmt.Errorf("%w: %q", ErrUnknownUnit, code)
	}
	return u, nil
}

// UnitCodes returns every supported unit code, sorted.
func UnitCodes() []string {
	out := make([]string, 0, len(units))
	for c := range units {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Discrete reports whether the unit only combines with itself.
func (u Unit) Discrete() bool { return u.Kind == KindDiscrete }

// BaseFactor returns a copy of the exact factor to the kind's base unit (ml or
// g), or nil for discrete units. Use it to compare unit sizes deterministically.
func (u Unit) BaseFactor() *big.Rat {
	if u.toBase == nil {
		return nil
	}
	return new(big.Rat).Set(u.toBase)
}

// CanConvertTo reports whether a quantity in u converts exactly to v without
// ingredient-specific information.
func (u Unit) CanConvertTo(v Unit) bool {
	if u.Code == v.Code {
		return true
	}
	return !u.Discrete() && !v.Discrete() && u.Kind == v.Kind
}

// Label returns the display label for a quantity of this unit.
func (u Unit) Label(q Quantity) string {
	if q.Cmp(NewQuantity(1, 1)) <= 0 {
		return u.Singular
	}
	return u.Plural
}

// Convert converts q from one unit to another. It fails with
// ErrIncompatibleUnits across kinds (volume↔mass, count↔mass) or between
// different discrete units: "1 onion" is never silently "8 oz onion".
func Convert(q Quantity, from, to Unit) (Quantity, error) {
	if from.Code == to.Code {
		return q, nil
	}
	if !from.CanConvertTo(to) {
		return Quantity{}, fmt.Errorf("%w: %s → %s", ErrIncompatibleUnits, from.Code, to.Code)
	}
	factor := new(big.Rat).Quo(from.toBase, to.toBase)
	return q.MulRat(factor), nil
}

// Amount is a quantity with its unit.
type Amount struct {
	Quantity Quantity
	Unit     Unit
}

// Add returns a + b expressed in a's unit, or ErrIncompatibleUnits.
func (a Amount) Add(b Amount) (Amount, error) {
	converted, err := Convert(b.Quantity, b.Unit, a.Unit)
	if err != nil {
		return Amount{}, err
	}
	return Amount{Quantity: a.Quantity.Add(converted), Unit: a.Unit}, nil
}

// Scale multiplies the amount by a factor (e.g. target servings ÷ recipe servings).
func (a Amount) Scale(factor Quantity) Amount {
	return Amount{Quantity: a.Quantity.Mul(factor), Unit: a.Unit}
}
