package shopping

import (
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

func cartLine(id, key, product string, packages int, status LineStatus) HandoffLine {
	return HandoffLine{ID: id, LineSource: LineSource{IngredientKey: key, Name: key}, ProductID: product, Packages: packages, Status: status}
}

func TestApplyCartAndMergeSend(t *testing.T) {
	walmart := providers.NewWalmart(providers.WalmartOptions{})
	sent := Handoff{ID: "h1", Revision: 4, Proposal: Proposal{Lines: []HandoffLine{
		cartLine("l1", "beef", "100000001", 2, LinePending),
		cartLine("l2", "onion", "100000002", 1, LineConfirmed),
		cartLine("l3", "gone", "100000003", 2, LinePending),
		cartLine("l4", "salt", "100000004", 1, LinePending),
		cartLine("l5", "milk", "100000005", 1, LineSkipped),
	}}}
	proposal := Proposal{
		Lines: []HandoffLine{
			cartLine("l1", "beef", "100000001", 3, LinePending),  // up by 1
			cartLine("l2", "onion", "100000002", 2, LinePending), // up by 1, but confirmed
			cartLine("l3", "garlic", "100000006", 1, LinePending),
			cartLine("l4", "milk", "100000005", 1, LinePending), // skipped lines don't count
		},
		Excluded: []Excluded{{LineSource: LineSource{IngredientKey: "salt"}, Reason: ExcludedCheckedOff}},
	}
	if err := applyCart(walmart, &proposal, sent); err != nil {
		t.Fatal(err)
	}
	wantCarts := []LineCart{{SentPackages: 2, AddPackages: 1}, {SentPackages: 1, AddPackages: 1}, {AddPackages: 1}, {AddPackages: 1}}
	for i, want := range wantCarts {
		if got := *proposal.Lines[i].Cart; got != want {
			t.Errorf("line %s cart = %+v, want %+v", proposal.Lines[i].IngredientKey, got, want)
		}
	}
	if got := linkItems(proposal.Links); got != "100000001,100000002,100000006,100000005" {
		t.Errorf("links = %q", got)
	}
	other := proposal.Cart.Other
	if len(other) != 2 || other[0].IngredientKey != "gone" || other[0].Reason != SentNotOnList || other[0].RemovePackages != 2 ||
		other[1].IngredientKey != "salt" || other[1].Reason != SentNotIncluded || other[1].RemovePackages != 0 {
		t.Errorf("other = %+v", other)
	}

	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	next, lineIDs, packages, err := mergeSend(walmart, sent, proposal, now)
	if err != nil {
		t.Fatal(err)
	}
	if packages != 4 || len(lineIDs) != 4 || len(next.Lines) != 8 || !next.UpdatedAt.Equal(now) {
		t.Fatalf("merge = %d packages, lines %v, %+v", packages, lineIDs, next.Lines)
	}
	// Beef grows in place; the confirmed onion gets a new line for the extra.
	if l, _ := next.Line("l1"); l.Packages != 3 || l.Status != LinePending {
		t.Errorf("beef = %+v", l)
	}
	if l, _ := next.Line("l6"); l.IngredientKey != "onion" || l.Packages != 1 {
		t.Errorf("extra onion = %+v", l)
	}
	if l, _ := sent.Line("l1"); l.Packages != 2 {
		t.Errorf("merge changed the handoff it was given: %+v", l)
	}

	// Everything already sent: nothing to merge.
	for i := range proposal.Lines {
		proposal.Lines[i].Cart.AddPackages = 0
	}
	if same, ids, _, err := mergeSend(walmart, sent, proposal, now); err != nil || ids != nil || same.Revision != 4 {
		t.Errorf("nothing new = %v, %v", ids, err)
	}
}
