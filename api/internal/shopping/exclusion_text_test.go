package shopping

import "testing"

// The Not Included text says what the app actually knows: an in-stock pantry
// item is in the pantry, and a staple the pantry doesn't track is only
// usually on hand. (A pantry item marked low or out is never excluded; see
// pantry.GroceryPantry.)
func TestExclusionTextIsHonest(t *testing.T) {
	var h Handler
	for reason, want := range map[ExclusionReason]string{
		ExcludedInPantry:   "In your pantry",
		ExcludedPantryHint: "Usually on hand",
		ExcludedCheckedOff: "Already checked off",
	} {
		if got := h.exclusionText("walmart", reason); got != want {
			t.Errorf("exclusionText(%s) = %q, want %q", reason, got, want)
		}
	}
}
