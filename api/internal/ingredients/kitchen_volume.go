package ingredients

import "math"

// mlPerTsp is a US teaspoon in milliliters.
const mlPerTsp = 4.92892159375

// KitchenVolume rewrites a metric volume (ml or l) as the spoons and cups a
// US kitchen measures with, rounded to what those hold (build 122 review:
// "18 ml" of ponzu is "1 Tbsp"). Under a tablespoon it is teaspoons to the
// nearest quarter; under a quarter cup, tablespoons to the nearest half; above
// that, cups to the nearest quarter or third, whichever is closer. ok is false
// for any other unit, which stays as it is. It is for what people read, never
// for what a grocery list buys.
func KitchenVolume(q Quantity, unit string) (Quantity, string, bool) {
	var ml float64
	switch unit {
	case "ml":
		ml = q.Float64()
	case "l":
		ml = q.Float64() * 1000
	default:
		return q, unit, false
	}
	if ml <= 0 {
		return q, unit, false
	}
	tsp := ml / mlPerTsp
	if tsp < 2.875 {
		return nearest(tsp, 4, 1), "tsp", true
	}
	tbsp := tsp / 3
	if half := math.Round(tbsp * 2); half < 8 {
		return NewQuantity(int64(max(half, 2)), 2), "tbsp", true
	}
	cups := tbsp / 16
	quarter, third := math.Round(cups*4)/4, math.Round(cups*3)/3
	if math.Abs(third-cups) < math.Abs(quarter-cups) {
		return NewQuantity(int64(math.Round(cups*3)), 3), "cup", true
	}
	return NewQuantity(int64(math.Round(cups*4)), 4), "cup", true
}

// nearest rounds v to the nearest 1/den, and at least least/den.
func nearest(v float64, den, least int64) Quantity {
	return NewQuantity(max(int64(math.Round(v*float64(den))), least), den)
}
