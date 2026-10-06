package ingredients

import "testing"

// Every packet-counted ingredient a meal kit sends that a store sells by
// size has a size, so the pantry counts it down and the list compares it
// instead of assuming a jar or can is enough (decision 629).
func TestPacketSizes(t *testing.T) {
	cases := []struct {
		name, unit string
		want       string
		wantUnit   string
	}{
		{"Tomato Paste", "count", "2", "tbsp"},
		{"Chicken Stock Concentrate", "count", "1", "tsp"},
		{"Coconut Milk", "count", "27/2", "floz"},
		{"Crushed Tomatoes", "count", "344/25", "oz"},
		{"Black Beans", "count", "67/5", "oz"},
		{"Cannellini Beans", "count", "67/5", "oz"},
		{"Chickpeas", "count", "67/5", "oz"},
		{"Three-Bean Blend", "count", "67/5", "oz"},
		{"Microwavable Rice", "count", "44/5", "oz"},
		{"Apricot Jam", "count", "2", "tbsp"},
		{"Red Pepper Jam", "count", "2", "tbsp"},
		{"Peanut Butter", "count", "2", "tbsp"},
		{"Ketchup", "package", "2", "tbsp"},
	}
	for _, c := range cases {
		q, unit, ok := PacketSizeFor(c.name, c.unit)
		if !ok || q.RatString() != c.want || unit != c.wantUnit {
			t.Errorf("PacketSizeFor(%q) = %v %q %v, want %s %s", c.name, q, unit, ok, c.want, c.wantUnit)
		}
		if _, unit, ok := PacketSizeFor(c.name, "cup"); ok {
			t.Errorf("%s in cups has a packet size (%s)", c.name, unit)
		}
	}
	// Whole things counted as themselves, and packs nobody knows, have none.
	for _, name := range []string{"Lime", "Onion", "Flour Tortillas", "Mystery Sauce", "Jamaican Jerk Seasoning"} {
		if q, unit, ok := PacketSizeFor(name, "count"); ok {
			t.Errorf("%s has a packet size %v %s", name, q, unit)
		}
	}
}
