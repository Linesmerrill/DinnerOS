package ingredients

import (
	"math/big"
	"strings"
)

// A meal kit sends some ingredients as packets ("1 tomato paste", "1 chicken
// stock concentrate"). A household cooking from its own pantry measures them
// instead, so the cooking screens say what to scoop. Grocery amounts are not
// changed: this is how a packet reads, not what is bought.

// KitchenMeasure is what one packet of an ingredient is in a kitchen measure.
type KitchenMeasure struct {
	// PerPacket is the amount of Unit one packet stands for.
	PerPacket *big.Rat
	Unit      string
	// Or is another way to say it ("or 1 bouillon cube per tsp"), shown
	// after the amount; empty when there is none.
	Or string
}

// KitchenMeasureFor returns the kitchen measure for a packet of the named
// ingredient, and false when it has none or isn't counted in packets.
func KitchenMeasureFor(name, unitCode string) (KitchenMeasure, bool) {
	if unitCode != "count" && unitCode != "package" {
		return KitchenMeasure{}, false
	}
	n := strings.ToLower(name)
	switch {
	case n == "tomato paste":
		return KitchenMeasure{PerPacket: big.NewRat(2, 1), Unit: "tbsp"}, true
	case strings.HasSuffix(n, "stock concentrate"), strings.HasSuffix(n, "broth concentrate"):
		return KitchenMeasure{PerPacket: big.NewRat(1, 1), Unit: "tsp", Or: "bouillon cube"}, true
	}
	return KitchenMeasure{}, false
}
