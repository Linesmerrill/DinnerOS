package menu

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// listCatalog returns 45 made-up recipes: varied cook times (including one
// unknown), proteins, cuisines, tags spelled two ways, order history, and
// five add-ons.
func listCatalog() []recipes.Recipe {
	proteins := []string{"Chicken Breast", "Ground Beef", "Salmon Fillet", "Tofu", "Pork Chops"}
	cuisines := []string{"Italian", "Thai", "Mexican"}
	tags := []string{"Family Friendly", "FamilyFriendly", "One Pot"}
	var out []recipes.Recipe
	for i := range 45 {
		opts := []recipeOpt{
			cookMinutes((i * 7) % 60), withIngredients(proteins[i%len(proteins)]),
			withCuisines(cuisines[i%len(cuisines)]), withTags(tags[i%len(tags)]),
		}
		if n := i % 5; n > 0 {
			opts = append(opts, ordered(n, week("2026-W20").AddWeeks(i%10).String()))
		}
		if i%9 == 8 {
			opts = append(opts, addon())
		}
		out = append(out, newRecipe(fmt.Sprintf("r%02d", i), fmt.Sprintf("Meal %02d", i), opts...))
	}
	return out
}

// listCards pages through the whole list with q's limit.
func listCards(t *testing.T, snap *snapshot, q ListQuery) []Card {
	t.Helper()
	var cards []Card
	for range 100 {
		v, after, err := q.validate()
		if err != nil {
			t.Fatalf("validate(%+v) error = %v", q, err)
		}
		page := snap.list(v, after)
		cards = append(cards, page.Cards...)
		if page.NextCursor == "" {
			return cards
		}
		q.Cursor = page.NextCursor
	}
	t.Fatal("paging did not finish")
	return nil
}

func listIDs(t *testing.T, snap *snapshot, q ListQuery) []string {
	t.Helper()
	out := []string{}
	for _, c := range listCards(t, snap, q) {
		out = append(out, c.Recipe.ID)
	}
	return out
}

func TestListPagingIsStable(t *testing.T) {
	snap := newSnapshot(input(listCatalog()...))
	for _, sort := range []Sort{SortRecommended, SortPopular, SortRecent, SortQuick, SortName} {
		t.Run(string(sort), func(t *testing.T) {
			full := listIDs(t, snap, ListQuery{Sort: sort, Limit: MaxListLimit})
			if len(full) != 40 { // 45 recipes less 5 add-ons
				t.Fatalf("one page returned %d main meals, want 40", len(full))
			}
			if len(slices.Compact(slices.Clone(full))) != len(full) {
				t.Errorf("one page repeats recipes: %v", full)
			}
			paged := listIDs(t, snap, ListQuery{Sort: sort, Limit: 7})
			if !slices.Equal(paged, full) {
				t.Errorf("paging by 7 = %v, want %v", paged, full)
			}
			if again := listIDs(t, snap, ListQuery{Sort: sort, Limit: 7}); !slices.Equal(again, paged) {
				t.Errorf("paging again = %v, want %v", again, paged)
			}
		})
	}
}

// A cursor is a position, not an offset, so a page still follows the one
// before it after the catalog changes.
func TestListCursorSurvivesACatalogChange(t *testing.T) {
	catalog := listCatalog()
	snap := newSnapshot(input(slices.Clone(catalog)...))
	first, _, err := ListQuery{Sort: SortName, Limit: 7}.validate()
	if err != nil {
		t.Fatal(err)
	}
	page1 := snap.list(first, nil)
	second, after, err := ListQuery{Sort: SortName, Limit: 7, Cursor: page1.NextCursor}.validate()
	if err != nil {
		t.Fatal(err)
	}
	want := snap.list(second, after)

	removed := page1.Cards[0].Recipe.ID
	smaller := slices.DeleteFunc(slices.Clone(catalog), func(r recipes.Recipe) bool { return r.ID == removed })
	got := newSnapshot(input(smaller...)).list(second, after)
	if !slices.Equal(cardNames(got.Cards), cardNames(want.Cards)) {
		t.Errorf("page 2 after removing %s = %v, want %v", removed, cardNames(got.Cards), cardNames(want.Cards))
	}
}

// Every chip's count is how many recipes the matching filter returns.
func TestFilterCountsMatchTheList(t *testing.T) {
	catalog := listCatalog()
	f := filtersOf(catalog)
	snap := newSnapshot(input(catalog...))
	for _, group := range []struct {
		kind    string
		options []FilterOption
		query   func(string) ListQuery
	}{
		{"protein", f.Proteins, func(v string) ListQuery { return ListQuery{Protein: v, Limit: MaxListLimit} }},
		{"cuisine", f.Cuisines, func(v string) ListQuery { return ListQuery{Cuisine: v, Limit: MaxListLimit} }},
		{"tag", f.Tags, func(v string) ListQuery { return ListQuery{Tag: v, Limit: MaxListLimit} }},
	} {
		if len(group.options) == 0 {
			t.Errorf("no %s options", group.kind)
		}
		for _, o := range group.options {
			if got := len(listIDs(t, snap, group.query(o.Value))); got != o.Count {
				t.Errorf("%s %q count = %d, but the list returns %d", group.kind, o.Value, o.Count, got)
			}
		}
	}
	if !slices.Equal(f.MaxMinutes, []int{15, 20, 30, 45}) || len(f.Sorts) != 5 {
		t.Errorf("maxMinutes = %v, sorts = %v", f.MaxMinutes, f.Sorts)
	}
}

func TestListFilters(t *testing.T) {
	snap := newSnapshot(input(listCatalog()...))

	// A region matches its cuisines.
	thai := listIDs(t, snap, ListQuery{Cuisine: "Thai", Limit: MaxListLimit})
	if asian := listIDs(t, snap, ListQuery{Cuisine: "asian", Limit: MaxListLimit}); !slices.Equal(asian, thai) || len(thai) == 0 {
		t.Errorf("cuisine=asian = %v, want the Thai recipes %v", asian, thai)
	}
	// Tag spellings that differ by a space are one chip.
	family := listIDs(t, snap, ListQuery{Tag: "family friendly", Limit: MaxListLimit})
	if len(family) != 30 { // both spellings; every add-on carries the third tag
		t.Errorf("tag=family friendly returned %d recipes, want 30", len(family))
	}
	// Add-ons are opt-in.
	if addons := listIDs(t, snap, ListQuery{Addons: true, Limit: MaxListLimit}); len(addons) != 5 {
		t.Errorf("addons=true returned %d recipes, want 5", len(addons))
	}
	// maxMinutes keeps known cook times only.
	for _, c := range listCards(t, snap, ListQuery{MaxMinutes: 20, Limit: MaxListLimit}) {
		if m := c.Recipe.CookMinutes(); m <= 0 || m > 20 {
			t.Errorf("maxMinutes=20 returned %q with %d minutes", c.Recipe.Name, m)
		}
	}
	// Search matches names, case-insensitively.
	for _, c := range listCards(t, snap, ListQuery{Search: "meal 1", Limit: MaxListLimit}) {
		if !strings.Contains(strings.ToLower(c.Recipe.Name), "meal 1") {
			t.Errorf("q=meal 1 returned %q", c.Recipe.Name)
		}
	}
	// A protein filter uses Autopilot's classification.
	for _, c := range listCards(t, snap, ListQuery{Protein: "chicken", Limit: MaxListLimit}) {
		if !strings.Contains(strings.ToLower(c.Recipe.Ingredients[0].Name), "chicken") {
			t.Errorf("protein=chicken returned %q with %v", c.Recipe.Name, c.Recipe.Ingredients)
		}
	}
}

func TestListQueryValidation(t *testing.T) {
	popularCursor := encodeCursor(SortPopular, listKey{Name: "meal 01", ID: "r01"})
	tests := []struct {
		name  string
		query ListQuery
		want  string
	}{
		{"unknown sort", ListQuery{Sort: "best"}, "sort must be recommended, popular, recent, quick, or name"},
		{"unknown protein", ListQuery{Protein: "unicorn"}, "protein must be one of chicken"},
		{"negative limit", ListQuery{Limit: -1}, "limit must be a positive integer"},
		{"negative maxMinutes", ListQuery{MaxMinutes: -5}, "maxMinutes must be a positive integer"},
		{"long search", ListQuery{Search: strings.Repeat("a", 101)}, "at most 100 characters"},
		{"broken cursor", ListQuery{Cursor: "not base64 !!"}, "cursor is invalid"},
		{"cursor from another sort", ListQuery{Sort: SortName, Cursor: popularCursor}, "cursor belongs to a different sort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.query.validate()
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("validate() error = %v, want one containing %q", err, tt.want)
			}
		})
	}

	q, _, err := ListQuery{Limit: 500}.validate()
	if err != nil || q.Limit != MaxListLimit || q.Sort != SortRecommended {
		t.Errorf("validate() = %+v, %v; want the limit capped at %d and the default sort", q, err, MaxListLimit)
	}
}
