package shopping

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

var checkTime = time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)

// checkedInput validates a match input with product checks, as the service
// does before building a proposal.
func checkedInput(t *testing.T, in MatchInput) MatchInput {
	t.Helper()
	out, err := validateMatchInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.checks, err = validateChecks(in.Checks, checkTime); err != nil {
		t.Fatal(err)
	}
	return out
}

func report(key, product string, status ProductStatus, decision CheckDecision) ProductCheckReport {
	return ProductCheckReport{IngredientKey: key, ProductID: product, Status: status, CheckedAt: checkTime.Add(-time.Minute), Decision: decision}
}

func excludedFor(p Proposal, key string) (Excluded, bool) {
	for _, e := range p.Excluded {
		if e.IngredientKey == key {
			return e, true
		}
	}
	return Excluded{}, false
}

// The rule the whole feature rests on: a product the phone found gone is
// never a cart line, and never in a link, even when the request selects it.
func TestBuildProposalNeverSendsAGoneProduct(t *testing.T) {
	w := providers.NewWalmart(providers.WalmartOptions{})
	for name, in := range map[string]MatchInput{
		"defaults": {Checks: []ProductCheckReport{report(beefKey, "100000001", ProductGone, "")}},
		"selected with a count": {
			Lines:  []LineSelection{{IngredientKey: beefKey, Packages: 3}, {IngredientKey: onionKey}},
			Checks: []ProductCheckReport{report(beefKey, "100000001", ProductGone, "")},
		},
		"selected and left out": {
			Lines:  []LineSelection{{IngredientKey: beefKey, Packages: 3}, {IngredientKey: onionKey}},
			Checks: []ProductCheckReport{report(beefKey, "100000001", ProductGone, DecisionLeaveOut)},
		},
		"unselected": {
			Lines:  []LineSelection{{IngredientKey: onionKey}},
			Checks: []ProductCheckReport{report(beefKey, "100000001", ProductGone, "")},
		},
	} {
		p, err := buildProposal(w, Settings{}, testList(t), testPrefs(), checkedInput(t, in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, l := range p.Lines {
			if l.IngredientKey == beefKey {
				t.Errorf("%s: the gone beef is a line: %+v", name, l)
			}
		}
		if strings.Contains(linkItems(p.Links), "100000001") {
			t.Errorf("%s: links %q include the gone item", name, linkItems(p.Links))
		}
		e, ok := excludedFor(p, beefKey)
		want := ExcludedProductGone
		if name == "selected and left out" {
			want = ExcludedLeftOutGone
		}
		if !ok || e.Reason != want || e.ProductID != "100000001" || e.Check == nil || e.Check.Status != ProductGone {
			t.Errorf("%s: beef excluded = %+v, want %s", name, e, want)
		}
	}
}

func TestBuildProposalAppliesDecisions(t *testing.T) {
	w := providers.NewWalmart(providers.WalmartOptions{})
	p, err := buildProposal(w, Settings{}, testList(t), testPrefs(), checkedInput(t, MatchInput{Checks: []ProductCheckReport{
		report(beefKey, "100000001", ProductUnknown, ""),
		report(onionKey, "100000002", ProductUnavailable, ""),
	}}))
	if err != nil {
		t.Fatal(err)
	}
	// Unknown and undecided: held for a decision. Unavailable: sent.
	if got := reasons(p)[beefKey]; got != ExcludedProductUnverified || len(p.NeedsDecision()) != 1 {
		t.Errorf("undecided unknown = %q, needs decision %d", got, len(p.NeedsDecision()))
	}
	if len(p.Lines) != 1 || p.Lines[0].IngredientKey != onionKey || p.Lines[0].Check.Status != ProductUnavailable {
		t.Errorf("lines = %+v", p.Lines)
	}

	// Send Anyway sends an unknown product, recorded as such; Leave Out
	// excludes it with why.
	p, _ = buildProposal(w, Settings{}, testList(t), testPrefs(), checkedInput(t, MatchInput{Checks: []ProductCheckReport{
		report(beefKey, "100000001", ProductUnknown, DecisionSendAnyway),
		report(onionKey, "100000002", ProductUnknown, DecisionLeaveOut),
	}}))
	if len(p.Lines) != 1 || p.Lines[0].IngredientKey != beefKey || p.Lines[0].Check.Decision != DecisionSendAnyway || len(p.NeedsDecision()) != 0 {
		t.Errorf("send anyway lines = %+v", p.Lines)
	}
	if got := reasons(p)[onionKey]; got != ExcludedLeftOutUnverified || strings.Contains(linkItems(p.Links), "100000002") {
		t.Errorf("left out onion = %q, links %q", got, linkItems(p.Links))
	}

	// Send Anyway on a line the app didn't select still sends it: it was
	// waiting on the decision, not deselected.
	p, _ = buildProposal(w, Settings{}, testList(t), testPrefs(), checkedInput(t, MatchInput{
		Lines:  []LineSelection{{IngredientKey: onionKey}},
		Checks: []ProductCheckReport{report(beefKey, "100000001", ProductUnknown, DecisionSendAnyway)},
	}))
	if len(p.Lines) != 2 || p.Lines[1].IngredientKey != beefKey || p.Lines[1].Packages != 2 {
		t.Errorf("unselected send anyway = %+v", p.Lines)
	}

	// Left out with excludeKeys too: still reported as left out because of
	// its product.
	p, _ = buildProposal(w, Settings{}, testList(t), testPrefs(), checkedInput(t, MatchInput{
		ExcludeKeys: []string{beefKey},
		Checks:      []ProductCheckReport{report(beefKey, "100000001", ProductGone, "")},
	}))
	if got := reasons(p)[beefKey]; got != ExcludedLeftOutGone {
		t.Errorf("excludeKeys on a gone product = %q", got)
	}

	// A report for a product the member has since replaced is unknown and
	// undecided, whatever it says.
	p, _ = buildProposal(w, Settings{}, testList(t), testPrefs(), checkedInput(t, MatchInput{Checks: []ProductCheckReport{
		report(beefKey, "199999999", ProductFound, DecisionSendAnyway),
	}}))
	if got := reasons(p)[beefKey]; got != ExcludedProductUnverified {
		t.Errorf("report for another product = %q", got)
	}

	// A line that isn't being bought (in the pantry) needs no decision.
	p, _ = buildProposal(w, Settings{}, testList(t), testPrefs(), checkedInput(t, MatchInput{Checks: []ProductCheckReport{
		report(pasteKey, "100000003", ProductGone, ""),
	}}))
	if got := reasons(p)[pasteKey]; got != ExcludedInPantry || len(p.NeedsDecision()) != 0 {
		t.Errorf("gone pantry item = %q", got)
	}
}

// Without a report, the saved product's last check still counts when it
// found the product gone, so an app that didn't check can't send it.
func TestBuildProposalUsesTheSavedGoneCheck(t *testing.T) {
	w := providers.NewWalmart(providers.WalmartOptions{})
	prefs := testPrefs()
	prefs[0].Check = &ProductCheckState{ProductID: "100000001", Status: ProductGone, CheckedAt: checkTime}
	prefs[1].Check = &ProductCheckState{ProductID: "100000002", Status: ProductUnknown, CheckedAt: checkTime}
	p, err := buildProposal(w, Settings{}, testList(t), prefs, checkedInput(t, MatchInput{}))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := excludedFor(p, beefKey)
	if e.Reason != ExcludedProductGone || !e.Check.Saved {
		t.Errorf("saved gone = %+v", e)
	}
	if len(p.Lines) != 1 || p.Lines[0].IngredientKey != onionKey || p.Lines[0].Check != nil {
		t.Errorf("a saved unknown doesn't block: %+v", p.Lines)
	}
	// Re-chosen since: the old product's check says nothing.
	prefs[0].ProductID = "100000009"
	p, _ = buildProposal(w, Settings{}, testList(t), prefs, checkedInput(t, MatchInput{}))
	if len(p.Lines) != 2 {
		t.Errorf("re-chosen product = %+v", p.Excluded)
	}
	// A fresh report wins over the saved check.
	prefs[0].ProductID = "100000001"
	p, _ = buildProposal(w, Settings{}, testList(t), prefs, checkedInput(t, MatchInput{Checks: []ProductCheckReport{report(beefKey, "100000001", ProductFound, "")}}))
	if len(p.Lines) != 2 {
		t.Errorf("found again = %+v", p.Excluded)
	}
}

func TestValidateChecks(t *testing.T) {
	ok := report(beefKey, "100000001", ProductFound, "")
	for name, r := range map[string]ProductCheckReport{
		"bad key":           {IngredientKey: "nope", ProductID: "1", Status: ProductFound, CheckedAt: checkTime},
		"no product":        {IngredientKey: beefKey, Status: ProductFound, CheckedAt: checkTime},
		"bad status":        {IngredientKey: beefKey, ProductID: "1", Status: "fine", CheckedAt: checkTime},
		"bad decision":      {IngredientKey: beefKey, ProductID: "1", Status: ProductUnknown, Decision: "maybe", CheckedAt: checkTime},
		"send a gone one":   report(beefKey, "1", ProductGone, DecisionSendAnyway),
		"no time":           {IngredientKey: beefKey, ProductID: "1", Status: ProductFound},
		"negative price":    {IngredientKey: beefKey, ProductID: "1", Status: ProductFound, CheckedAt: checkTime, PriceCents: cents(-1)},
		"implausible price": {IngredientKey: beefKey, ProductID: "1", Status: ProductFound, CheckedAt: checkTime, PriceCents: cents(MaxPriceCents + 1)},
	} {
		var v *ValidationError
		if _, err := validateChecks([]ProductCheckReport{r}, checkTime); !errors.As(err, &v) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
	if _, err := validateChecks([]ProductCheckReport{ok, ok}, checkTime); err == nil {
		t.Error("a key listed twice was accepted")
	}
	future := ok
	future.CheckedAt, future.Name = checkTime.Add(time.Hour), "  Beef   16 oz "
	got, err := validateChecks([]ProductCheckReport{future}, checkTime)
	if err != nil || !got[beefKey].CheckedAt.Equal(checkTime) || got[beefKey].Name != "Beef 16 oz" {
		t.Errorf("normalized = %+v, %v", got, err)
	}
	gone := report(beefKey, "1", ProductGone, "")
	gone.Name, gone.PriceCents = "Old", cents(100)
	if got, _ := validateChecks([]ProductCheckReport{gone}, checkTime); got[beefKey].Name != "" || got[beefKey].PriceCents != nil {
		t.Errorf("a gone report keeps page fields: %+v", got[beefKey])
	}
}

func TestApplyCheckPricePrecedence(t *testing.T) {
	at := checkTime
	found := func(price *int64) ProductCheckReport {
		return ProductCheckReport{IngredientKey: beefKey, ProductID: "1", Status: ProductFound, CheckedAt: at, Name: "Beef", PriceCents: price}
	}
	base := Preference{IngredientKey: beefKey, ProductID: "1"}

	// No saved price: the check fills it.
	p, changed := applyCheck(base, found(cents(499)))
	if !changed || *p.PriceCents != 499 || p.PriceSource != PriceFromProvider || !p.PriceUpdatedAt.Equal(at) || p.Check.PriceCents == nil {
		t.Errorf("empty price = %+v, %v", p, changed)
	}
	// A provider price follows Walmart's.
	p, changed = applyCheck(p, found(cents(529)))
	if !changed || *p.PriceCents != 529 {
		t.Errorf("provider price = %v, %v", *p.PriceCents, changed)
	}
	if _, changed = applyCheck(p, found(cents(529))); changed {
		t.Error("an unchanged price counted as a change")
	}

	// A member's price is never overridden by the first check.
	member := base
	member.PriceCents, member.PriceSource = cents(450), PriceFromMember
	p, changed = applyCheck(member, found(cents(499)))
	if changed || *p.PriceCents != 450 || p.PriceSource != PriceFromMember || *p.Check.PriceCents != 499 {
		t.Errorf("first check over a member price = %+v, %v", p, changed)
	}
	// Nor by a later one at the same listed price.
	p, changed = applyCheck(p, found(cents(499)))
	if changed || *p.PriceCents != 450 {
		t.Errorf("same listed price = %v, %v", *p.PriceCents, changed)
	}
	// But when Walmart's own price moves, the member's number is older than
	// the change.
	p, changed = applyCheck(p, found(cents(549)))
	if !changed || *p.PriceCents != 549 || p.PriceSource != PriceFromProvider {
		t.Errorf("moved listed price = %+v, %v", p, changed)
	}

	// A check without a price keeps the last listed one for the comparison.
	p, _ = applyCheck(member, found(cents(499)))
	p, _ = applyCheck(p, found(nil))
	if p.Check.PriceCents == nil || *p.Check.PriceCents != 499 {
		t.Errorf("listed price dropped: %+v", p.Check)
	}
}

func TestApplyCheckKeepsConclusiveResults(t *testing.T) {
	p := Preference{IngredientKey: beefKey, ProductID: "1"}
	p, _ = applyCheck(p, ProductCheckReport{ProductID: "1", Status: ProductFound, CheckedAt: checkTime, Name: "Beef"})
	p, _ = applyCheck(p, ProductCheckReport{ProductID: "1", Status: ProductGone, CheckedAt: checkTime.Add(time.Hour)})
	if p.Check.Status != ProductGone || p.Check.Name != "Beef" || !p.NeedsRechoosing() {
		t.Errorf("gone = %+v", p.Check)
	}
	// An unknown check says nothing about the item: gone stays gone.
	after, _ := applyCheck(p, ProductCheckReport{ProductID: "1", Status: ProductUnknown, CheckedAt: checkTime.Add(2 * time.Hour)})
	if after.Check != p.Check {
		t.Errorf("unknown replaced gone: %+v", after.Check)
	}
	// With nothing before it, unknown is recorded.
	first, _ := applyCheck(Preference{ProductID: "1"}, ProductCheckReport{ProductID: "1", Status: ProductUnknown, CheckedAt: checkTime})
	if first.Check == nil || first.Check.Status != ProductUnknown || first.NeedsRechoosing() {
		t.Errorf("first unknown = %+v", first.Check)
	}
}
