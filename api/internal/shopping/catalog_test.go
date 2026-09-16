package shopping

import (
	"slices"
	"testing"
)

func TestCatalogIntegrityAndOrder(t *testing.T) {
	list := CatalogEntries()
	if len(list) < 80 {
		t.Fatalf("the catalog has %d entries, want the curated list", len(list))
	}
	seen := map[string]bool{}
	lastRank, lastName := -1, ""
	for _, e := range list {
		switch {
		case seen[e.Key]:
			t.Errorf("duplicate key %q", e.Key)
		case e.Key == "" || e.Name == "":
			t.Errorf("%+v has an empty key or name", e)
		}
		seen[e.Key] = true
		switch e.Kind {
		case KindGrocer, KindDelivery, KindWarehouse, KindOther:
		default:
			t.Errorf("%s has kind %q", e.Key, e.Kind)
		}
		rank := statusRank(e.Status)
		if rank > 2 {
			t.Errorf("%s has status %q", e.Key, e.Status)
		}
		// Ordered by status, then name.
		if rank < lastRank || (rank == lastRank && e.Name < lastName) {
			t.Errorf("%s (%s) sorts after %s", e.Name, e.Status, lastName)
		}
		// An unsupported store has nothing researched to say, and a store we
		// did assess has to say what the research found.
		if e.Status == StatusUnsupported && e.Note != "" {
			t.Errorf("%s is unsupported but carries a note", e.Key)
		}
		if e.Status != StatusUnsupported && e.Note == "" {
			t.Errorf("%s is %q but carries no note", e.Key, e.Status)
		}
		lastRank, lastName = rank, e.Name
	}
}

// TestCatalogStatusMapping pins the status each entry takes from
// docs/shopping-providers.md. Walmart ships; the providers that document
// assessed (and the banners those rows cover) are researched; the rest wait
// for demand.
func TestCatalogStatusMapping(t *testing.T) {
	for key, want := range map[string]StoreStatus{
		"walmart":      StatusAvailable,
		"kroger":       StatusResearched,
		"instacart":    StatusResearched,
		"amazon-fresh": StatusResearched,
		"target":       StatusResearched,
		"albertsons":   StatusResearched,
		"frys":         StatusResearched, // a Kroger banner
		"safeway":      StatusResearched, // an Albertsons banner
		"publix":       StatusUnsupported,
		"costco":       StatusUnsupported,
		"trader-joes":  StatusUnsupported,
		"doordash":     StatusUnsupported,
	} {
		e, ok := CatalogEntryByKey(key)
		if !ok {
			t.Errorf("%s is not in the catalog", key)
			continue
		}
		if e.Status != want {
			t.Errorf("%s status = %q, want %q", key, e.Status, want)
		}
	}
	if _, ok := CatalogEntryByKey("some-local-market"); ok {
		t.Error("a free-text store must not be in the catalog")
	}
	// Only Walmart is available, so it sorts first.
	if first := CatalogEntries()[0]; first.Key != "walmart" || first.Status != StatusAvailable {
		t.Errorf("first entry = %+v, want walmart available", first)
	}
}

func TestSearchCatalog(t *testing.T) {
	keys := func(q string) []string {
		out := []string{}
		for _, e := range SearchCatalog(q) {
			out = append(out, e.Key)
		}
		return out
	}
	if got, want := len(keys("")), len(CatalogEntries()); got != want {
		t.Errorf("empty q returned %d entries, want %d", got, want)
	}
	for _, tc := range []struct {
		name, q string
		want    []string
	}{
		{"name prefix, case insensitive", "KROG", []string{"kroger"}},
		{"partial name", "sam", []string{"sams-club"}},
		{"alias", "quality food", []string{"qfc"}},
		{"apostrophes fold away", "fry's", []string{"frys"}},
		{"ampersand spelled out", "stop and", []string{"stop-and-shop"}},
		{"key prefix", "misfits", []string{"misfits-market"}},
		{"no match", "zzzz", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := keys(tc.q); !slices.Equal(got, tc.want) {
				t.Errorf("SearchCatalog(%q) = %v, want %v", tc.q, got, tc.want)
			}
		})
	}
	// Results keep the catalog's order: available, then researched, then
	// unsupported.
	if got := keys("s"); len(got) < 2 {
		t.Fatalf("SearchCatalog(\"s\") = %v", got)
	} else {
		last := -1
		for _, k := range got {
			e, _ := CatalogEntryByKey(k)
			if statusRank(e.Status) < last {
				t.Errorf("search results are out of order: %v", got)
				break
			}
			last = statusRank(e.Status)
		}
	}
}

// TestCatalogAliasesAreUnambiguous guards the index built in init(): two
// entries claiming the same normalized text would silently give one of them
// the other's requests, since catalogByAlias keeps whichever was indexed last.
func TestCatalogAliasesAreUnambiguous(t *testing.T) {
	owner := map[string]string{}
	for _, e := range CatalogEntries() {
		for _, text := range append([]string{e.Name, e.Key}, e.Aliases...) {
			n := normalizeStoreText(text)
			if n == "" {
				t.Errorf("%s has the unmatchable text %q", e.Key, text)
				continue
			}
			if prev, ok := owner[n]; ok && prev != e.Key {
				t.Errorf("%q matches both %s and %s", n, prev, e.Key)
			}
			owner[n] = e.Key
		}
	}
}

func TestMatchCatalogName(t *testing.T) {
	for in, want := range map[string]string{
		"frys":                 "frys",
		"Fry's":                "frys",
		"  FRYS  ":             "frys",
		"quality food centers": "qfc",
		"trader joes":          "trader-joes",
		"Trader Joe's":         "trader-joes",
		"Stop & Shop":          "stop-and-shop",
		"stop and shop":        "stop-and-shop",
		"heb":                  "heb",
		"H-E-B":                "heb",
	} {
		e, ok := matchCatalogName(in)
		if !ok || e.Key != want {
			t.Errorf("matchCatalogName(%q) = %+v, %v; want %s", in, e, ok, want)
		}
	}
	for _, in := range []string{"some local market", "fry", "", "!!!"} {
		if e, ok := matchCatalogName(in); ok {
			t.Errorf("matchCatalogName(%q) matched %s; free text and prefixes must not", in, e.Key)
		}
	}
}

func TestStoreNameNormalization(t *testing.T) {
	for _, tc := range []struct{ in, key, display string }{
		{"  some   local market ", "some-local-market", "Some Local Market"},
		{"Joe's Corner Store", "joes-corner-store", "Joe's Corner Store"},
		{"HEB", "heb", "HEB"},
		{"the 99 cent market", "the-99-cent-market", "The 99 Cent Market"},
		{"!!!", "", ""},
	} {
		if got := storeKeyOf(tc.in); got != tc.key {
			t.Errorf("storeKeyOf(%q) = %q, want %q", tc.in, got, tc.key)
		}
		if got := displayStoreName(tc.in); got != tc.display && tc.key != "" {
			t.Errorf("displayStoreName(%q) = %q, want %q", tc.in, got, tc.display)
		}
	}
}
