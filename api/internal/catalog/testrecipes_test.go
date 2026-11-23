package catalog

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// QA runs' recipes ("QA Hide Pilaf 12341") never fill Shared Recipes' rows,
// Discover or browsing (build 122 review), but a search that names one finds
// it, and it still opens, as the report and hide tests need.
func TestTestRecipesStayOutOfSharedRecipesLists(t *testing.T) {
	h := newHarness(t, nil)
	seedHome(t, h)
	qa := publish(t, h, "qa-1", "QA Hide Pilaf 12341")
	publish(t, h, "qa-2", "QA Report Pilaf 12341")
	// Build 123's runs name them like people do, with the run's number.
	publish(t, h, "qa-3", "Lemon Herb Pilaf 24421")
	publish(t, h, "qa-4", "Saffron Pilaf 90901")
	// Only the "QA " prefix marks one: a real "Quail" dinner stays.
	publish(t, h, "hf-quail", "Quail with Grapes")

	check := func(where string, names []string) {
		t.Helper()
		for _, n := range names {
			if strings.HasPrefix(n, "QA ") || strings.HasSuffix(n, "24421") || strings.HasSuffix(n, "90901") {
				t.Errorf("%s shows a test recipe: %v", where, names)
			}
		}
	}
	home, err := h.service.Home(t.Context(), hhAda)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range home.Sections {
		check("row "+s.ID, itemNames(s.Items))
		_, page, err := h.service.Section(t.Context(), hhAda, s.ID, Query{})
		if err != nil {
			t.Fatal(err)
		}
		check("see all "+s.ID, itemNames(page.Items))
	}
	for _, q := range []Query{{}, {Text: "pilaf"}, {Text: "hide"}, {Text: "lemon herb pilaf"}} {
		d, err := h.service.Discover(t.Context(), hhAda, q)
		if err != nil {
			t.Fatal(err)
		}
		check("discover", resultNames(d))
		s, err := h.service.Search(t.Context(), hhAda, q)
		if err != nil {
			t.Fatal(err)
		}
		check("search "+q.Text, resultNames(s))
	}
	browse, err := h.service.Search(t.Context(), hhAda, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(resultNames(browse), "Quail with Grapes") {
		t.Errorf("browse = %v; want Quail with Grapes", resultNames(browse))
	}

	for _, text := range []string{"QA Hide Pilaf 12341", "qa hide pilaf"} {
		named, err := h.service.Search(t.Context(), hhAda, Query{Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if got := resultNames(named); !slices.Equal(got, []string{"QA Hide Pilaf 12341"}) {
			t.Errorf("search %q = %v; want the test recipe", text, got)
		}
	}
	named, err := h.service.Search(t.Context(), hhAda, Query{Text: "Lemon Herb Pilaf 24421"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultNames(named); !slices.Equal(got, []string{"Lemon Herb Pilaf 24421"}) {
		t.Errorf("search by its full name = %v; want the test recipe", got)
	}
	if _, err := h.service.Get(t.Context(), hhAda, qa); err != nil {
		t.Errorf("Get(test recipe) = %v; want it to open", err)
	}
}

func TestIsTestRecipeName(t *testing.T) {
	for name, want := range map[string]bool{
		"QA Hide Pilaf 12341": true, " QA Fudge 229": true, "Quail with Grapes": false,
		"QAnon Stew": false, "Aqua Fresca": false, "qa lowercase": false,
		"Lemon Herb Pilaf 24421": true, "Saffron Pilaf 90901 ": true, "Mushroom Pilaf 1234": false,
		"Chili 2": false, "Soup for 12": false, "24421 Pilaf": false, "Pilaf24421": false,
	} {
		if got := IsTestRecipeName(name); got != want {
			t.Errorf("IsTestRecipeName(%q) = %v; want %v", name, got, want)
		}
	}
}

func TestIntegrationSearchLeavesTestRecipesOutUnlessNamed(t *testing.T) {
	store := newTestMongoStore(t)
	entries := []Recipe{
		{CatalogKey: "hellofresh:a", Content: mongoContent("Chicken Pilaf", "rice", "chicken")},
		{CatalogKey: "household:b", Content: mongoContent("QA Hide Pilaf 12341", "rice", "onion")},
		{CatalogKey: "household:c", Content: mongoContent("Saffron Pilaf 90901", "rice", "saffron")},
	}
	if _, err := store.Upsert(t.Context(), entries, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	names := func(f Filter) []string {
		t.Helper()
		found, total, err := store.Search(t.Context(), f)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, e := range found {
			out = append(out, e.Content.Name)
		}
		if total != len(out) {
			t.Errorf("total = %d; want %d", total, len(out))
		}
		return out
	}
	if got := names(Filter{Limit: 10, HideTestRecipes: true}); !slices.Equal(got, []string{"Chicken Pilaf"}) {
		t.Errorf("browse = %v; want only Chicken Pilaf", got)
	}
	if got := names(Filter{Text: "pilaf", Limit: 10, HideTestRecipes: true}); !slices.Equal(got, []string{"Chicken Pilaf"}) {
		t.Errorf("search pilaf = %v; want only Chicken Pilaf", got)
	}
	if got := names(Filter{Text: "QA Hide Pilaf 12341", Limit: 10}); !slices.Contains(got, "QA Hide Pilaf 12341") {
		t.Errorf("named search = %v; want the test recipe", got)
	}
	if got := names(Filter{Text: "Saffron Pilaf 90901", Limit: 10}); !slices.Contains(got, "Saffron Pilaf 90901") {
		t.Errorf("named search = %v; want the run-number recipe", got)
	}
}
