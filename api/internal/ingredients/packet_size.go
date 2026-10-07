package ingredients

import (
	"math/big"
	"strings"
)

// A meal kit counts some ingredients in packets, cans and pouches ("1 coconut
// milk", "1 black beans") while a household buys them by the ounce. Counting
// those down, and comparing them with what's at home, needs what one holds.
// This is for counting only: the cooking steps still say "1 can"
// (KitchenMeasureFor is what they show). Sizes are the meal kit's usual
// packs; an ingredient missing here is never guessed — the grocery list buys
// it rather than assume there's enough (decision 629).

// packetSizes are matched in order against the normalized name: an exact
// name, or a phrase the name ends with ("apricot jam" ends with "jam").
var packetSizes = []struct {
	names  []string
	suffix bool
	amount *big.Rat
	unit   string
}{
	// Cans and cartons.
	{names: []string{"coconut milk"}, amount: big.NewRat(27, 2), unit: "floz"},
	{names: []string{"crushed tomatoes", "diced tomatoes"}, amount: big.NewRat(344, 25), unit: "oz"},
	{names: []string{"black beans", "cannellini beans", "kidney beans", "pinto beans", "chickpeas", "three bean blend", "three-bean blend"}, amount: big.NewRat(67, 5), unit: "oz"},
	// Pouches.
	{names: []string{"microwavable rice", "microwaveable rice", "microwavable jasmine rice", "microwaveable jasmine rice"}, amount: big.NewRat(44, 5), unit: "oz"},
	// Spoon-sized packets.
	{names: []string{"jam", "preserves", "marmalade"}, suffix: true, amount: big.NewRat(2, 1), unit: "tbsp"},
	{names: []string{"peanut butter", "ketchup"}, amount: big.NewRat(2, 1), unit: "tbsp"},
}

// PacketSizeFor returns what one packet, can, or pouch of the named
// ingredient holds, for counting it against a package bought by size. It
// includes every kitchen measure ("1 tomato paste" is 2 Tbsp). false when
// the unit isn't a count or the ingredient's pack isn't known.
func PacketSizeFor(name, unitCode string) (*big.Rat, string, bool) {
	if km, ok := KitchenMeasureFor(name, unitCode); ok {
		return km.PerPacket, km.Unit, true
	}
	if unitCode != "count" && unitCode != "package" {
		return nil, "", false
	}
	n := NormalizeName(name)
	for _, p := range packetSizes {
		for _, w := range p.names {
			if n == w || (p.suffix && strings.HasSuffix(n, " "+w)) {
				return new(big.Rat).Set(p.amount), p.unit, true
			}
		}
	}
	return nil, "", false
}

// SpoonMeasured reports a thick ingredient a cook spoons out rather than
// weighs: "1.5 oz tomato paste" can't be measured in a kitchen, "2½ Tbsp"
// can. A recipe's weight of one reads in spoons (decision 631).
func SpoonMeasured(name string) bool {
	n := NormalizeName(name)
	switch {
	case strings.HasSuffix(n, " paste") || n == "paste":
		// Tomato, curry, miso, chipotle, garlic, ginger, and the like.
		return true
	case n == "pesto" || strings.HasSuffix(n, " pesto"), n == "harissa", n == "gochujang", n == "tahini",
		n == "miso", n == "white miso", n == "red miso", n == "peanut butter", n == "mayonnaise", n == "mayo",
		n == "sour cream", n == "dijon mustard", n == "whole grain mustard", n == "honey":
		return true
	}
	return false
}
