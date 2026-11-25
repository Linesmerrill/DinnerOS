package pantry

import (
	"encoding/json"
	"strings"
	"testing"
)

// Hark's build 121 review: the pantry's Yellow Onions read "3" with no word.
// A count carries the word it reads with; other units don't.
func TestItemResponseCountNoun(t *testing.T) {
	onions := newItemResponse(Item{DisplayName: "Yellow Onions", Quantity: "3", Unit: "count"})
	if c := onions.CountNoun; c == nil || c.One != "onion" || c.Other != "onions" {
		t.Errorf("Yellow Onions countNoun = %+v, want onion/onions", c)
	}
	lemon := newItemResponse(Item{DisplayName: "Lemon", Quantity: "1", Unit: "count"})
	if c := lemon.CountNoun; c == nil || c.One != "lemon" || c.Other != "lemons" {
		t.Errorf("Lemon countNoun = %+v, want lemon/lemons", c)
	}
	if c := newItemResponse(Item{DisplayName: "Garlic", Quantity: "7", Unit: "clove"}).CountNoun; c != nil {
		t.Errorf("Garlic in cloves countNoun = %+v, want none", c)
	}
	if c := newItemResponse(Item{DisplayName: "Salt"}).CountNoun; c != nil {
		t.Errorf("Salt with no amount countNoun = %+v, want none", c)
	}
	body, err := json.Marshal(onions)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"countNoun":{"one":"onion","other":"onions"}`) {
		t.Errorf("JSON = %s, want countNoun", body)
	}
}
