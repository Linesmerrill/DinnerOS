package shopping

import (
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

func added(week, key, name string, count int, day int) AddedItem {
	return AddedItem{Week: week, IngredientKey: key, Name: name, Count: count, AddedAt: time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC)}
}

func usualKeys(v UsualsView) []string {
	var out []string
	for _, u := range v.Items {
		out = append(out, u.IngredientKey)
	}
	return out
}

func TestThreeOfTheLastSixShoppingWeeksMakesAUsual(t *testing.T) {
	history := []AddedItem{
		added("2026-W40", "milk", "Milk", 1, 1),
		added("2026-W42", "milk", "Milk", 2, 2),
		added("2026-W43", "milk", "Milk", 2, 3),
		added("2026-W42", "eggs", "Eggs", 1, 2),
		added("2026-W43", "eggs", "Eggs", 1, 3),
	}
	v := findUsuals("2026-W44", history, []string{"2026-W41"}, nil, nil)
	if got := usualKeys(v); len(got) != 1 || got[0] != "milk" {
		t.Fatalf("usuals = %v, want only milk (eggs is 2 of 4)", got)
	}
	milk := v.Items[0]
	if milk.Weeks != 3 || milk.Count != 2 || milk.Name != "Milk" || milk.Pinned || milk.OnList {
		t.Errorf("milk = %+v, want 3 weeks and the usual count 2", milk)
	}
	if v.ShoppingWeeks != 4 {
		t.Errorf("shopping weeks = %d, want W40-W43", v.ShoppingWeeks)
	}
}

func TestOnlyTheLastSixShoppingWeeksCount(t *testing.T) {
	// Milk was a usual long ago; six newer shopping weeks push it out.
	history := []AddedItem{
		added("2026-W30", "milk", "Milk", 1, 1),
		added("2026-W31", "milk", "Milk", 1, 1),
		added("2026-W32", "milk", "Milk", 1, 1),
	}
	sent := []string{"2026-W38", "2026-W39", "2026-W40", "2026-W41", "2026-W42", "2026-W43"}
	if v := findUsuals("2026-W44", history, sent, nil, nil); len(v.Items) != 0 || v.ShoppingWeeks != UsualWindow {
		t.Fatalf("view = %+v", v)
	}
	// Weeks with nothing sent or added aren't shopping weeks, so a gap
	// doesn't age an item out.
	if v := findUsuals("2026-W44", history, nil, nil, nil); len(v.Items) != 1 {
		t.Fatalf("usuals = %v", usualKeys(v))
	}
}

func TestTheWeekBeingPlannedDoesNotCount(t *testing.T) {
	history := []AddedItem{
		added("2026-W42", "milk", "Milk", 1, 1),
		added("2026-W43", "milk", "Milk", 1, 2),
		added("2026-W44", "milk", "Milk", 1, 3),
	}
	v := findUsuals("2026-W44", history, []string{"2026-W44", "2026-W45"}, nil, map[string]bool{"milk": true})
	if len(v.Items) != 0 || v.ShoppingWeeks != 2 {
		t.Fatalf("view = %+v", v)
	}
}

func TestPinsAndNotAUsual(t *testing.T) {
	history := []AddedItem{
		added("2026-W41", "milk", "Milk", 1, 1), added("2026-W42", "milk", "Milk", 1, 2), added("2026-W43", "milk", "Milk", 1, 3),
		added("2026-W41", "bread", "Bread", 1, 1), added("2026-W42", "bread", "Bread", 1, 2), added("2026-W43", "bread", "Bread", 1, 3),
		added("2026-W43", "coffee", "Coffee", 3, 3),
	}
	prefs := []UsualPref{
		{IngredientKey: "bread", Name: "Bread", NotUsual: true},
		{IngredientKey: "coffee", Name: "Coffee", Pinned: true},
		{IngredientKey: "name:bananas", Name: "Bananas", Pinned: true, Count: 6},
	}
	v := findUsuals("2026-W44", history, nil, prefs, map[string]bool{"milk": true})
	got := usualKeys(v)
	want := []string{"name:bananas", "coffee", "milk"}
	if len(got) != len(want) {
		t.Fatalf("usuals = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("usuals = %v, want %v (pinned first, then by name)", got, want)
		}
	}
	if v.Items[0].Count != 6 || !v.Items[0].Pinned || v.Items[0].Weeks != 0 {
		t.Errorf("bananas = %+v: a pin keeps its count", v.Items[0])
	}
	if v.Items[1].Count != 3 || v.Items[1].Weeks != 1 {
		t.Errorf("coffee = %+v: a pin without a count uses the usual one", v.Items[1])
	}
	if !v.Items[2].OnList {
		t.Errorf("milk = %+v, want on the list", v.Items[2])
	}
	if offered := v.Offered(); len(offered) != 2 {
		t.Errorf("offered = %+v, want everything but milk", offered)
	}
	if len(v.NotUsual) != 1 || v.NotUsual[0].Name != "Bread" {
		t.Errorf("not usual = %+v", v.NotUsual)
	}
}

func TestTheUsualCountTieGoesToTheLatest(t *testing.T) {
	history := []AddedItem{
		added("2026-W40", "milk", "Milk", 1, 1), added("2026-W41", "milk", "Milk", 2, 2),
		added("2026-W42", "milk", "Milk", 1, 3), added("2026-W43", "milk", "Whole Milk", 2, 4),
	}
	v := findUsuals("2026-W44", history, nil, nil, nil)
	if len(v.Items) != 1 || v.Items[0].Count != 2 || v.Items[0].Name != "Whole Milk" {
		t.Fatalf("usuals = %+v", v.Items)
	}
}

func TestAnAddedLineCountsItsPackages(t *testing.T) {
	l := HandoffLine{LineSource: LineSource{Added: true, Amounts: []Amount{{Quantity: "3", Unit: "count"}}}}
	if got := l.PackageCount().Packages; got != 3 {
		t.Errorf("packages = %d, want 3", got)
	}
	l.Amounts = nil
	if got := l.PackageCount().Packages; got != 1 {
		t.Errorf("packages = %d, want at least 1", got)
	}
	l.Amounts = []Amount{{Quantity: "500", Unit: "count"}}
	if got := l.PackageCount().Packages; got != providers.MaxPackages {
		t.Errorf("packages = %d, want the cap", got)
	}
}

func TestATypedNameIsCapitalizedOnlyWhenAllLowerCase(t *testing.T) {
	for in, want := range map[string]string{
		"paper towels":  "Paper Towels",
		"iPhone case":   "iPhone case",
		"HEB tortillas": "HEB tortillas",
		"Oat milk":      "Oat milk",
	} {
		if got := displayItemName(in); got != want {
			t.Errorf("displayItemName(%q) = %q, want %q", in, got, want)
		}
	}
}
