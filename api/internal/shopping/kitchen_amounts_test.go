package shopping

import "testing"

// Hark's build 121 review, item 14: Shop showed Milk "16 tsp", Flour "8 tsp",
// Milk "4 tsp" and "5 Tbsp + 1 tsp". Spoons read as a kitchen measures them;
// the exact amount stays in Quantity for the app's math.
func TestAmountsReadAsKitchenMeasures(t *testing.T) {
	for _, tc := range []struct {
		in   []Amount
		want string
	}{
		{[]Amount{{Quantity: "16", Unit: "tsp"}}, "⅓ cup"},
		{[]Amount{{Quantity: "8", Unit: "tsp"}}, "2 ½ Tbsp"},
		{[]Amount{{Quantity: "4", Unit: "tsp"}}, "1 ½ Tbsp"},
		{[]Amount{{Quantity: "16/3", Unit: "tbsp"}}, "⅓ cup"},
		{[]Amount{{Quantity: "3", Unit: "tbsp"}}, "3 Tbsp"},
		{[]Amount{{Quantity: "3/2", Unit: "cup"}}, "1 ½ cups"},
		{[]Amount{{Quantity: "10", Unit: "oz"}}, "10 oz"},
		{[]Amount{{Quantity: "2", Unit: "count"}, {Quantity: "8", Unit: "tsp"}}, "2 + 2 ½ Tbsp"},
	} {
		resp, text := amounts(tc.in)
		if text != tc.want {
			t.Errorf("amounts(%v) = %q, want %q", tc.in, text, tc.want)
		}
		if len(resp) != len(tc.in) || resp[0].Quantity != tc.in[0].Quantity || resp[0].Unit != tc.in[0].Unit {
			t.Errorf("amounts(%v) changed the exact amount: %+v", tc.in, resp)
		}
	}
}
