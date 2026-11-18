package main

import (
	"slices"
	"testing"
)

func TestOrderPutsTheRecipesOwnPhotoFirst(t *testing.T) {
	list := []listed{{Recipe: "Beef and Broccoli"}, {Recipe: "Salmon with Asparagus and Rice"}, {Recipe: "Chicken Skewers with Garlic Sauce"}}
	if got := order(list, "Salmon with Asparagus and Rice", 3); !slices.Equal(got, []int{1, 0, 2}) {
		t.Errorf("order = %v; want the salmon first and the rest as listed (with is ignored)", got)
	}
	if got := order(nil, "anything", 4); !slices.Equal(got, []int{0, 1, 2, 3}) {
		t.Errorf("without a list: %v", got)
	}
}
