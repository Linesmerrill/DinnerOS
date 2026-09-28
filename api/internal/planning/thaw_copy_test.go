package planning

import "testing"

// TestThawSummary is the thaw reminder's body: short sentences, one idea
// each.
func TestThawSummary(t *testing.T) {
	tests := []struct {
		name string
		item ThawItem
		want string
	}{
		{"deadline ahead",
			ThawItem{Name: "Chicken Thighs", Recipes: []string{"Tuscan Chicken"}, Hours: 5, MoveBy: "13:00"},
			"Chicken Thighs is for Tuscan Chicken tonight. It takes about 5 hours to thaw in the fridge. Move it to the fridge by 1 PM."},
		{"deadline passed",
			ThawItem{Name: "Pork Loin", Recipes: []string{"Sheet-Pan Pork", "Pork Tacos"}, Hours: 24, MoveBy: "18:00", Overnight: true},
			"Pork Loin is for Sheet-Pan Pork and Pork Tacos tonight. It takes about a day to thaw in the fridge. Move it to the fridge this morning."},
		{"labeled bag",
			ThawItem{Name: "Ground Pork", FrozenOn: "2026-09-27", Recipes: []string{"Pork Tacos"}, Hours: 6, MoveBy: "12:00"},
			"Grab the Ground Pork bag dated Sep 27. It's for Pork Tacos tonight. It takes about 6 hours to thaw in the fridge. Move it to the fridge by 12 PM."},
		{"no recipe names", ThawItem{Name: "Peas", Hours: 1, MoveBy: "17:30"},
			"It takes about an hour to thaw in the fridge. Move it to the fridge by 5:30 PM."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := thawSummary(tt.item); got != tt.want {
				t.Errorf("thawSummary = %q, want %q", got, tt.want)
			}
		})
	}
}
