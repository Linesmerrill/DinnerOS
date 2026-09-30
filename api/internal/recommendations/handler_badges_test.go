package recommendations

import "testing"

func TestProposalResponseCarriesBadges(t *testing.T) {
	p := Proposal{Week: "2026-W41", Slots: []Slot{
		{ID: "tue", Day: "tue", Badges: []Badge{{Code: "weather", Label: "Cold and rainy", Symbol: "cloud.rain", Detail: "Picked for the weather."}}},
		{ID: "wed", Day: "wed"},
	}}
	resp := newProposalResponse(p, "sun")
	if got := resp.Slots[0].Badges; len(got) != 1 || got[0] != (BadgeJSON{Code: "weather", Label: "Cold and rainy", Symbol: "cloud.rain", Detail: "Picked for the weather."}) {
		t.Errorf("badges = %+v", got)
	}
	// Always a list, so the app never sees null.
	if got := resp.Slots[1].Badges; got == nil || len(got) != 0 {
		t.Errorf("no badges = %#v, want an empty list", got)
	}
}
