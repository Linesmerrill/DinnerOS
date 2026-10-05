package recipes

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

func TestSafeTempForMeatAndSeafood(t *testing.T) {
	cases := []struct {
		name       string
		f, rest    int
		isMeatLike bool
	}{
		{"Chicken Breasts", 165, 0, true},
		{"Boneless Skinless Chicken Thighs", 165, 0, true},
		{"Ground Turkey", 165, 0, true},
		{"Chicken Sausage", 165, 0, true},
		{"Ground Beef", 160, 0, true},
		{"Ground Pork", 160, 0, true},
		{"Italian Pork Sausage", 160, 0, true},
		{"Chorizo", 160, 0, true},
		{"Pork Tenderloin", 145, 3, true},
		{"Pork Chops", 145, 3, true},
		{"Sirloin Steak", 145, 3, true},
		{"Salmon Fillets", 145, 0, true},
		{"Shrimp", 145, 0, true},
		{"Tilapia", 145, 0, true},
		// Not raw meat, whatever the name says.
		{"Chicken Stock Concentrate", 0, 0, false},
		{"Beef Stock Concentrate", 0, 0, false},
		{"Ground Cumin", 0, 0, false},
		{"Ground Black Pepper", 0, 0, false},
		{"Fish Sauce", 0, 0, false},
		{"Bacon", 0, 0, false},
		{"Fully Cooked Chicken Sausage", 0, 0, false},
		{"Tortilla Strips", 0, 0, false},
		{"Burger Buns", 0, 0, false},
		{"Roasted Red Peppers", 0, 0, false},
		{"Chopped Cilantro", 0, 0, false},
		{"Asian Chop Salad Kit", 0, 0, false},
		{"Pulled Pork", 0, 0, false},
		{"BBQ Pulled Chicken", 0, 0, false},
		{"Sesame Ginger Chicken Gyoza", 0, 0, false},
		{"Pork Cutlets", 145, 3, true},
		{"Bavette Steak", 145, 3, true},
		{"Diced Skinless Dark Meat Chicken", 165, 0, true},
		{"Lobster Tails", 145, 0, true},
	}
	for _, c := range cases {
		f, rest, ok := safeTempFor(c.name)
		if ok != c.isMeatLike || f != c.f || rest != c.rest {
			t.Errorf("%s = %d°F rest %d (%v), want %d rest %d (%v)", c.name, f, rest, ok, c.f, c.rest, c.isMeatLike)
		}
	}
}

func TestSafeTempsFollowWhatTheHouseholdCooks(t *testing.T) {
	got := safeTemps([]IngredientState{
		{Name: "Ground Pork", SwapName: "Ground Beef"},
		{Name: "Chicken Breasts"},
		{Name: "chicken breasts"},
		{Name: "Shrimp", LeftOut: &grocery.LeftOut{}},
		{Name: "Pork Chops"},
		{Name: "Chicken Stock Concentrate"},
	})
	want := []string{"Ground Beef: 160°F", "Chicken Breasts: 165°F", "Pork Chops: 145°F, then rest 3 minutes"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		if got[i].Text != w {
			t.Errorf("%d = %q, want %q", i, got[i].Text, w)
		}
	}
}
