package ingredients

import "math/big"

// KitchenSpoons writes a US spoon or cup amount (tsp, tbsp, cup) as one
// everyday kitchen measure, never two added together (Hark's build 121
// reviews: "16 tsp" of milk, "8 tsp" of flour, "2 Tbsp + 2 tsp" of flour):
//
//   - under 1 Tbsp: teaspoons to the nearest quarter ("½ tsp", "2 tsp"), or
//     "⅛ tsp" for less than a quarter;
//   - from 1 Tbsp to a cup: a quarter or third of a cup when it is exactly
//     that, else whole or half tablespoons when it is exactly that ("6 Tbsp"),
//     else the nearest cup fraction (¼ ⅓ ½ ⅔ ¾ 1) within a tenth, else the
//     nearest half tablespoon, a tie going up ("2 ½ Tbsp" for 8 tsp);
//   - from a cup: cups to the nearest quarter or third, a tie going up.
//
// A half-tablespoon result that is a quarter cup exactly reads in cups.
//
// ok is false for any other unit, which the caller writes as it is.
func KitchenSpoons(q Quantity, unit string) (string, bool) {
	var perTsp int64
	switch unit {
	case "tsp":
		perTsp = 1
	case "tbsp":
		perTsp = 3
	case "cup":
		perTsp = 48
	default:
		return "", false
	}
	tsp := new(big.Rat).Mul(q.Rat(), big.NewRat(perTsp, 1))
	if tsp.Sign() <= 0 {
		return "", false
	}
	if tsp.Cmp(big.NewRat(1, 4)) < 0 {
		return spoonText(big.NewRat(1, 8), "tsp"), true
	}
	if tsp.Cmp(big.NewRat(3, 1)) < 0 {
		if r := nearestRat(tsp, 4, 1); r.Cmp(big.NewRat(3, 1)) < 0 {
			return spoonText(r, "tsp"), true
		}
		return "1 Tbsp", true
	}
	cups := new(big.Rat).Quo(tsp, big.NewRat(48, 1))
	tbsp := new(big.Rat).Quo(tsp, big.NewRat(3, 1))
	if cups.Cmp(big.NewRat(1, 1)) >= 0 {
		return spoonText(nearestCups(cups), "cup"), true
	}
	if cups.Cmp(big.NewRat(1, 4)) >= 0 {
		if isMultiple(cups, 4) || isMultiple(cups, 3) {
			return spoonText(cups, "cup"), true
		}
		if isMultiple(tbsp, 2) {
			return spoonText(tbsp, "tbsp"), true
		}
		if c := nearestCups(cups); withinTenth(c, cups) {
			return spoonText(c, "cup"), true
		}
	}
	half := nearestRat(tbsp, 2, 2)
	if c := new(big.Rat).Quo(half, big.NewRat(16, 1)); isMultiple(c, 4) {
		return spoonText(c, "cup"), true
	}
	return spoonText(half, "tbsp"), true
}

// nearestCups rounds cups to the nearest quarter or third, a tie going up.
func nearestCups(cups *big.Rat) *big.Rat {
	quarter, third := nearestRat(cups, 4, 1), nearestRat(cups, 3, 1)
	dq := new(big.Rat).Abs(new(big.Rat).Sub(quarter, cups))
	dt := new(big.Rat).Abs(new(big.Rat).Sub(third, cups))
	switch c := dt.Cmp(dq); {
	case c < 0:
		return third
	case c > 0:
		return quarter
	case third.Cmp(quarter) > 0:
		return third
	default:
		return quarter
	}
}

// withinTenth reports whether near is no more than a tenth away from r.
func withinTenth(near, r *big.Rat) bool {
	diff := new(big.Rat).Abs(new(big.Rat).Sub(near, r))
	return diff.Cmp(new(big.Rat).Mul(r, big.NewRat(1, 10))) <= 0
}

func spoonText(r *big.Rat, unit string) string {
	q := NewQuantity(1, 1).MulRat(r)
	u, err := LookupUnit(unit)
	if err != nil {
		return q.Format()
	}
	return q.Format() + " " + u.Label(q)
}

// isMultiple reports whether r is a whole number of 1/den.
func isMultiple(r *big.Rat, den int64) bool {
	return new(big.Rat).Mul(r, big.NewRat(den, 1)).IsInt()
}

// nearestRat rounds r to the nearest 1/den, half up, and at least least/den.
func nearestRat(r *big.Rat, den, least int64) *big.Rat {
	scaled := new(big.Rat).Mul(r, big.NewRat(den, 1))
	scaled.Add(scaled, big.NewRat(1, 2))
	n := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	if n.Cmp(big.NewInt(least)) < 0 {
		n.SetInt64(least)
	}
	return new(big.Rat).SetFrac(n, big.NewInt(den))
}
