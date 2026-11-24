package ingredients

import "math/big"

// KitchenSpoons writes a US spoon or cup amount (tsp, tbsp, cup) the way a
// kitchen measures it (Hark's build 121 review: "16 tsp" of milk, "8 tsp" of
// flour, "5 Tbsp + 1 tsp" of milk):
//
//   - under 1 Tbsp: teaspoons to the nearest quarter ("½ tsp", "2 tsp"), or
//     "⅛ tsp" for less than a quarter;
//   - under ¼ cup: whole or half tablespoons, else tablespoons plus teaspoons
//     ("2 Tbsp + 2 tsp");
//   - from ¼ cup: cups when it is a quarter or third ("⅓ cup", "1 ¼ cups");
//     under a cup, otherwise whole or half tablespoons ("6 Tbsp"); else cups to
//     the nearest quarter or third, a tie going up.
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
	switch {
	case tsp.Cmp(big.NewRat(1, 4)) < 0:
		return spoonText(big.NewRat(1, 8), "tsp"), true
	case tsp.Cmp(big.NewRat(3, 1)) < 0:
		if r := nearestRat(tsp, 4, 1); r.Cmp(big.NewRat(3, 1)) < 0 {
			return spoonText(r, "tsp"), true
		}
		return "1 Tbsp", true
	case tsp.Cmp(big.NewRat(12, 1)) < 0:
		return tablespoons(tsp), true
	}
	cups := new(big.Rat).Quo(tsp, big.NewRat(48, 1))
	if isMultiple(cups, 4) || isMultiple(cups, 3) {
		return spoonText(cups, "cup"), true
	}
	if tsp.Cmp(big.NewRat(48, 1)) < 0 {
		if tbsp := new(big.Rat).Quo(tsp, big.NewRat(3, 1)); isMultiple(tbsp, 2) {
			return spoonText(tbsp, "tbsp"), true
		}
	}
	quarter, third := nearestRat(cups, 4, 1), nearestRat(cups, 3, 1)
	dq := new(big.Rat).Abs(new(big.Rat).Sub(quarter, cups))
	dt := new(big.Rat).Abs(new(big.Rat).Sub(third, cups))
	switch c := dt.Cmp(dq); {
	case c < 0:
		return spoonText(third, "cup"), true
	case c > 0:
		return spoonText(quarter, "cup"), true
	case third.Cmp(quarter) > 0:
		return spoonText(third, "cup"), true
	default:
		return spoonText(quarter, "cup"), true
	}
}

// tablespoons writes 3 to 12 tsp as whole or half tablespoons, or tablespoons
// plus the teaspoons left to the nearest quarter: 8 tsp is "2 Tbsp + 2 tsp".
func tablespoons(tsp *big.Rat) string {
	tbsp := new(big.Rat).Quo(tsp, big.NewRat(3, 1))
	if isMultiple(tbsp, 2) {
		return spoonText(tbsp, "tbsp")
	}
	whole := new(big.Int).Quo(tbsp.Num(), tbsp.Denom())
	rest := new(big.Rat).Sub(tsp, new(big.Rat).SetInt(new(big.Int).Mul(whole, big.NewInt(3))))
	rest = nearestRat(rest, 4, 0)
	switch {
	case rest.Sign() == 0:
		return spoonText(new(big.Rat).SetInt(whole), "tbsp")
	case rest.Cmp(big.NewRat(3, 1)) >= 0:
		return spoonText(new(big.Rat).SetInt(whole.Add(whole, big.NewInt(1))), "tbsp")
	}
	return spoonText(new(big.Rat).SetInt(whole), "tbsp") + " + " + spoonText(rest, "tsp")
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
