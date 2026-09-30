package shelflife

import (
	"context"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// What the library picks for names households type, and where they keep them.
func TestMatchCommonGroceries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		storage Storage
		want    string
		text    string
	}{
		{"Carrots", Fridge, "Carrots, parsnips", "2–3 weeks"},
		{"Ground Pork", Freezer, "Pork, ground", "3–4 months"},
		{"Ground Pork", Fridge, "Pork, ground", "1–2 days"},
		{"Poblano Pepper", Fridge, "Peppers", "4–14 days"},
		{"Long Green Pepper", Fridge, "Peppers", "4–14 days"},
		{"Chicken Breasts", Fridge, "Chicken parts, breast halves, bone-in", "1–2 days"},
		{"Baby Carrots", Fridge, "Baby carrots", "4 weeks"},
		{"Sour Cream", Fridge, "Sour cream", "1–3 weeks"},
		{"Lime", Fridge, "Limes", "2–3 weeks"},
		{"Scallions", Fridge, "Green onions", "1–2 weeks"},
		{"Jalapeño", Fridge, "Hot peppers", "1 week"},
		{"Eggs", Fridge, "Eggs, in shell", "3–5 weeks"},
		{"Bacon", Fridge, "Bacon", "1 week"},
		{"Tomatoes", Pantry, "Tomatoes", "3–7 days"},
		{"Salmon Fillets", Fridge, "Salmon", "1–2 days"},
		{"Flour Tortillas", Pantry, "Tortillas, flour", "3 months"},
	} {
		e, ok := Match(Seed(), tc.name, tc.storage)
		r, has := e.In(tc.storage)
		if !ok || !has || e.Label() != tc.want || Text(r) != tc.text {
			t.Errorf("%s in the %s = %q %s, want %q %s", tc.name, tc.storage, e.Label(), Text(r), tc.want, tc.text)
		}
	}
	// The food doesn't change with the storage asked about: carrots in the
	// pantry are still carrots, not baby carrots (which have a pantry time).
	for _, s := range []Storage{Pantry, Fridge, Freezer} {
		if e, _ := Match(Seed(), "Carrots", s); e.Label() != "Carrots, parsnips" {
			t.Errorf("Carrots in the %s = %q", s, e.Label())
		}
	}
}

func TestText(t *testing.T) {
	for r, want := range map[Range]string{{14, 21}: "2–3 weeks", {180, 365}: "6–12 months", {90, 120}: "3–4 months", {365, 365}: "1 year", {1, 2}: "1–2 days", {7, 7}: "1 week"} {
		if got := Text(r); got != want {
			t.Errorf("Text(%v) = %q, want %q", r, got, want)
		}
	}
}

func TestBestByCountsMonthsAsMonths(t *testing.T) {
	sep29 := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		days int
		want string
	}{{120, "2027-01-29"}, {90, "2026-12-29"}, {14, "2026-10-13"}, {365, "2027-09-29"}} {
		if got := BestBy(sep29, tc.days).Format(DateLayout); got != tc.want {
			t.Errorf("%d days from Sep 29 = %s, want %s", tc.days, got, tc.want)
		}
	}
}

type fakeHouseholds struct{ wrap string }

func (f fakeHouseholds) GetHousehold(context.Context, string) (households.Household, error) {
	return households.Household{FreezerWrap: f.wrap}, nil
}

type fakeStore struct {
	entries []Entry
	misses  []string
}

func (f *fakeStore) Entries(context.Context) ([]Entry, error) { return f.entries, nil }
func (f *fakeStore) RecordMiss(_ context.Context, name string, _ Storage, _ time.Time) error {
	f.misses = append(f.misses, name)
	return nil
}

func TestLookup(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{}
	now := func() time.Time { return time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC) }
	s := NewService(Options{Store: store, Households: fakeHouseholds{}, Now: now})

	// Frozen ground pork, vacuum sealed: the long end, from the day it went in.
	res, err := s.Lookup(ctx, "h1", "Ground Pork", "", Freezer, "2026-09-29")
	if err != nil || res.BestBy != "2027-01-29" || res.Matched != "Pork, ground" || res.Estimate {
		t.Fatalf("frozen pork = %+v, %v", res, err)
	}
	// In a freezer bag: the short end.
	bag := NewService(Options{Households: fakeHouseholds{wrap: households.FreezerWrapBag}, Now: now})
	if res, _ := bag.Lookup(ctx, "h1", "Ground Pork", "", Freezer, "2026-09-29"); res.BestBy != "2026-12-29" {
		t.Errorf("bagged pork = %s, want 2026-12-29", res.BestBy)
	}
	// Carrots in the fridge today: the short end, 2 weeks.
	if res, _ := s.Lookup(ctx, "h1", "Carrots", "", Fridge, ""); res.BestBy != "2026-10-14" || res.Text != "2–3 weeks" {
		t.Errorf("carrots = %+v", res)
	}
	// The household's wrap is for meat: frozen carrots keep the short end.
	if res, _ := s.Lookup(ctx, "h1", "Carrots", "", Freezer, "2026-09-30"); res.BestBy != "2027-07-30" {
		t.Errorf("frozen carrots = %+v, want 2027-07-30", res)
	}
	// Unknown: the category's typical time, marked, and the miss recorded.
	res, _ = s.Lookup(ctx, "h1", "Zorbleberry Compote", "condiments", Fridge, "2026-09-30")
	if !res.Estimate || res.Text != "1–2 months" || len(store.misses) != 1 {
		t.Errorf("unknown = %+v, misses %v", res, store.misses)
	}
	// An entry added later wins over the seed.
	store.entries = []Entry{{ID: 200001, Name: "Carrots", Fridge: &Range{21, 28}, Source: "Test"}}
	if res, _ := s.Lookup(ctx, "h1", "Carrots", "", Fridge, "2026-09-30"); res.Text != "3–4 weeks" || res.Source != "Test" {
		t.Errorf("added entry = %+v", res)
	}
	// Where each is usually kept, for the form's default.
	for name, want := range map[string]Storage{"Carrots": Fridge, "Red Onion": Pantry, "Ground Pork": Fridge, "Rice": Pantry} {
		if res, _ := s.Lookup(ctx, "h1", name, "", Pantry, ""); res.Usual != want {
			t.Errorf("%s usually = %s, want %s", name, res.Usual, want)
		}
	}
	if _, err := s.Lookup(ctx, "h1", "Carrots", "", "shelf", ""); err == nil {
		t.Error("unknown storage accepted")
	}
}
