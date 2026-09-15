package ingredients

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// ErrInvalidQuantity is returned for negative, NaN, or infinite amounts.
var ErrInvalidQuantity = errors.New("invalid quantity")

// Quantity is an exact, non-negative rational amount. Recipe math must be exact:
// 1/3 + 1/3 + 1/3 is 1, not 0.9999999. The zero value is 0. Quantities are
// immutable; every operation returns a new value.
type Quantity struct {
	r *big.Rat
}

// NewQuantity returns num/den. It panics if den is zero or the result is
// negative; use it for literals, and ParseQuantity/QuantityFromFloat for input.
func NewQuantity(num, den int64) Quantity {
	if den == 0 {
		panic("ingredients: zero denominator")
	}
	r := big.NewRat(num, den)
	if r.Sign() < 0 {
		panic("ingredients: negative quantity")
	}
	return Quantity{r: r}
}

// snapDenominators are the fractions recipe sources use. Floats from sources
// (0.25, 0.333, 1.5) are snapped to these before exact math begins.
var snapDenominators = []int64{1, 2, 3, 4, 8, 16}

const snapTolerance = 0.002

// QuantityFromFloat converts a source float into an exact quantity, snapping to
// common kitchen fractions (halves, thirds, quarters, eighths, sixteenths) when
// the value is within a small tolerance, and otherwise keeping the decimal.
func QuantityFromFloat(f float64) (Quantity, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return Quantity{}, fmt.Errorf("%w: %v", ErrInvalidQuantity, f)
	}
	for _, d := range snapDenominators {
		n := math.Round(f * float64(d))
		if math.Abs(f-n/float64(d)) <= snapTolerance {
			return NewQuantity(int64(n), d), nil
		}
	}
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(f, 'f', -1, 64))
	if !ok {
		return Quantity{}, fmt.Errorf("%w: %v", ErrInvalidQuantity, f)
	}
	return Quantity{r: r}, nil
}

// ParseQuantity parses "2", "0.5", "1/3", "1 1/2", "½", or "1½".
func ParseQuantity(s string) (Quantity, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Quantity{}, fmt.Errorf("%w: empty", ErrInvalidQuantity)
	}
	for glyph, frac := range unicodeFractions {
		s = strings.Replace(s, glyph, " "+frac, 1)
	}
	total := new(big.Rat)
	for _, part := range strings.Fields(s) {
		r, ok := new(big.Rat).SetString(part)
		if !ok || r.Sign() < 0 {
			return Quantity{}, fmt.Errorf("%w: %q", ErrInvalidQuantity, s)
		}
		total.Add(total, r)
	}
	return Quantity{r: total}, nil
}

var unicodeFractions = map[string]string{
	"½": "1/2", "⅓": "1/3", "⅔": "2/3", "¼": "1/4", "¾": "3/4",
	"⅛": "1/8", "⅜": "3/8", "⅝": "5/8", "⅞": "7/8",
}

func (q Quantity) rat() *big.Rat {
	if q.r == nil {
		return new(big.Rat)
	}
	return q.r
}

// Add returns q + o.
func (q Quantity) Add(o Quantity) Quantity {
	return Quantity{r: new(big.Rat).Add(q.rat(), o.rat())}
}

// Mul returns q × o.
func (q Quantity) Mul(o Quantity) Quantity {
	return Quantity{r: new(big.Rat).Mul(q.rat(), o.rat())}
}

// MulRat returns q × r (r must be non-negative).
func (q Quantity) MulRat(r *big.Rat) Quantity {
	return Quantity{r: new(big.Rat).Mul(q.rat(), r)}
}

// Div returns q ÷ o, or an error when o is zero.
func (q Quantity) Div(o Quantity) (Quantity, error) {
	if o.IsZero() {
		return Quantity{}, fmt.Errorf("%w: division by zero", ErrInvalidQuantity)
	}
	return Quantity{r: new(big.Rat).Quo(q.rat(), o.rat())}, nil
}

// Cmp compares q and o (-1, 0, +1).
func (q Quantity) Cmp(o Quantity) int { return q.rat().Cmp(o.rat()) }

// Equal reports exact equality.
func (q Quantity) Equal(o Quantity) bool { return q.Cmp(o) == 0 }

// IsZero reports whether q is 0.
func (q Quantity) IsZero() bool { return q.rat().Sign() == 0 }

// Float64 returns the nearest float, for display and APIs only — never for math.
func (q Quantity) Float64() float64 {
	f, _ := q.rat().Float64()
	return f
}

// Rat returns a copy of the exact value.
func (q Quantity) Rat() *big.Rat { return new(big.Rat).Set(q.rat()) }

// Fraction returns the reduced numerator and denominator.
func (q Quantity) Fraction() (num, den int64) {
	r := q.rat()
	return r.Num().Int64(), r.Denom().Int64()
}

// String returns the exact value as "n" or "n/d".
func (q Quantity) String() string { return q.rat().RatString() }

var displayFractions = []struct {
	num, den int64
	glyph    string
}{
	{1, 8, "⅛"}, {1, 4, "¼"}, {1, 3, "⅓"}, {3, 8, "⅜"}, {1, 2, "½"},
	{5, 8, "⅝"}, {2, 3, "⅔"}, {3, 4, "¾"}, {7, 8, "⅞"},
}

// Format renders q for people: whole numbers, kitchen fractions ("1 ½"), or a
// short decimal when the value is not a common fraction.
func (q Quantity) Format() string {
	r := q.rat()
	whole := new(big.Int).Quo(r.Num(), r.Denom())
	rem := new(big.Rat).Sub(r, new(big.Rat).SetInt(whole))
	if rem.Sign() == 0 {
		return whole.String()
	}
	for _, f := range displayFractions {
		if rem.Cmp(big.NewRat(f.num, f.den)) == 0 {
			if whole.Sign() == 0 {
				return f.glyph
			}
			return whole.String() + " " + f.glyph
		}
	}
	return strconv.FormatFloat(q.Float64(), 'f', -1, 64)
}
