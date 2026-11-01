package shopping

import (
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
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
// docs/shopping-providers.md. The providers that document assessed (and the
// banners those rows cover) are researched; the rest wait for demand. Nothing
// is available in the catalog itself: that comes from the enabled providers.
func TestCatalogStatusMapping(t *testing.T) {
	for key, want := range map[string]StoreStatus{
		"walmart":      StatusResearched,
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
	for _, e := range CatalogEntries() {
		if e.Status == StatusAvailable {
			t.Errorf("%s is available in the catalog itself; only enabled providers make a store available", e.Key)
		}
	}
}

// TestLiveStoresAreAvailable: a store is available exactly while a provider
// covering it is enabled, with a note saying how it works, and sorts first.
func TestLiveStoresAreAvailable(t *testing.T) {
	status := func(entries []CatalogEntry, key string) (StoreStatus, string) {
		for _, e := range entries {
			if e.Key == key {
				return e.Status, e.Note
			}
		}
		t.Fatalf("%s missing", key)
		return "", ""
	}
	walmart := providers.NewWalmart(providers.WalmartOptions{})

	none := withLiveStores(CatalogEntries(), nil)
	if s, _ := status(none, "walmart"); s != StatusResearched {
		t.Errorf("walmart with nothing enabled = %q, want researched", s)
	}

	live := withLiveStores(CatalogEntries(), []providers.GroceryProvider{walmart, providers.NewKroger("frys")})
	if live[0].Status != StatusAvailable || live[1].Status != StatusAvailable || live[2].Status == StatusAvailable {
		t.Errorf("first three = %+v, want the two live stores first", live[:3])
	}
	if s, _ := status(live, "walmart"); s != StatusAvailable {
		t.Errorf("walmart enabled = %q, want available", s)
	}
	if s, note := status(live, "frys"); s != StatusAvailable || note != "Sign in and DinnerOS adds your list to your Fry's cart" {
		t.Errorf("frys enabled = %q %q", s, note)
	}
	// Another banner of the same API stays researched until it is enabled.
	if s, _ := status(live, "ralphs"); s != StatusResearched {
		t.Errorf("ralphs = %q, want researched while only Fry's is enabled", s)
	}
	// The catalog itself is untouched.
	if e, _ := CatalogEntryByKey("frys"); e.Status != StatusResearched {
		t.Errorf("catalog frys = %q after withLiveStores; it must not be changed", e.Status)
	}
}

// TestEveryKrogerBannerIsInTheCatalog keeps the banner table and the catalog
// in step, so enabling any banner marks its row available.
func TestEveryKrogerBannerIsInTheCatalog(t *testing.T) {
	for _, key := range []string{"frys", "kroger", "ralphs", "kingsoopers", "smiths", "fredmeyer", "qfc", "dillons",
		"citymarket", "marianos", "picknsave", "metromarket", "bakers", "food4less", "foodsco"} {
		k := providers.NewKroger(key)
		if k.Banner().Key != key {
			t.Errorf("banner %q is not in the banner table", key)
			continue
		}
		for _, ck := range k.CatalogKeys() {
			if _, ok := CatalogEntryByKey(ck); !ok {
				t.Errorf("banner %q names catalog key %q, which the catalog doesn't have", key, ck)
			}
		}
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
