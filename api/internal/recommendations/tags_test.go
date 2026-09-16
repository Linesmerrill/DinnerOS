package recommendations

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Fixtures are made-up recipes. The denylist was chosen against a real
// catalog, but no real recipe data lives in tests.

func tagged(name string, tags ...string) recipes.Recipe {
	return recipes.Recipe{ID: name, Name: name, Tags: tags, Servings: []int{2}}
}

func tagValuesOf(v Vocabulary) []string {
	out := make([]string, 0, len(v.Tags))
	for _, o := range v.Tags {
		out = append(out, o.Value)
	}
	return out
}

// TestHiddenTagsAreNotOffered: source bookkeeping never reaches the menu's
// food types or the Autopilot vocabulary, while real characteristics do.
func TestHiddenTagsAreNotOffered(t *testing.T) {
	catalog := []recipes.Recipe{
		tagged("a", "SEO", "dinners", "One Pot", "Kid Friendly"),
		tagged("b", "seo", "Dinners", "One Pot"),
		tagged("c", "LTO", "static-position", "Spicy"),
		tagged("d", "quick prep internal", "ineligible-reco", "free-addon", "Spicy"),
	}
	got := tagValuesOf(buildVocabulary(catalog))

	for _, hidden := range []string{"seo", "dinners", "lto", "static-position", "quick prep internal", "ineligible-reco", "free-addon"} {
		if slices.Contains(got, hidden) {
			t.Errorf("tag %q is source bookkeeping and should not be offered; got %v", hidden, got)
		}
	}
	for _, kept := range []string{"one pot", "kid friendly", "spicy"} {
		if !slices.Contains(got, kept) {
			t.Errorf("tag %q is a food characteristic and should be offered; got %v", kept, got)
		}
	}
}

// TestNearUniversalTagsAreNotOffered: a tag on nearly every recipe filters
// nothing, so it isn't offered; one on half the catalog is.
func TestNearUniversalTagsAreNotOffered(t *testing.T) {
	var catalog []recipes.Recipe
	for i := range 20 {
		tags := []string{"Half"}
		if i < 10 {
			tags = nil
		}
		// 19 of 20 (95%) carry "Everywhere"; the last one doesn't.
		if i < 19 {
			tags = append(tags, "Everywhere")
		}
		catalog = append(catalog, tagged(fmt.Sprintf("r%d", i), tags...))
	}
	got := tagValuesOf(buildVocabulary(catalog))
	if slices.Contains(got, "everywhere") {
		t.Errorf("a tag on 95%% of the catalog should not be offered; got %v", got)
	}
	if !slices.Contains(got, "half") {
		t.Errorf("a tag on half the catalog should be offered; got %v", got)
	}

	// The threshold is a share, not a count: the same tag on a small catalog
	// is judged the same way.
	if !hiddenTag("x", 10, 10) || hiddenTag("x", 9, 10) {
		t.Errorf("hiddenTag should hide >%.0f%% of the catalog and keep the rest", 100*NearUniversalTagShare)
	}
	if hiddenTag("x", 5, 0) {
		t.Error("an empty catalog hides nothing by share")
	}
}

// TestHiddenTagsStillMatch: hiding a tag changes only what is offered. A
// household that already saved one keeps it, and it still selects recipes.
func TestHiddenTagsStillMatch(t *testing.T) {
	if CanonicalTag("SEO") != "seo" {
		t.Fatalf("CanonicalTag(SEO) = %q", CanonicalTag("SEO"))
	}
	if VisibleTag("seo", 1, 100) {
		t.Error("seo should not be offered as a choice")
	}

	p := DefaultProfile("household-1")
	p.Restrictions.ExcludedTags = []string{"SEO"}
	p.Taste.Likes.Tags = []string{"dinners"}
	p.WeekdayRules = []WeekdayRule{{Day: "mon", Label: "Italian Monday", Tags: []string{"SEO"}, Frequency: "at_most_once"}}

	normalized, err := normalizeProfile(p)
	if err != nil {
		t.Fatalf("normalizeProfile() error = %v; a stored hidden tag must keep working", err)
	}
	if !slices.Contains(normalized.Restrictions.ExcludedTags, "seo") {
		t.Errorf("excludedTags = %v, want the stored tag kept", normalized.Restrictions.ExcludedTags)
	}
	if !slices.Contains(normalized.Taste.Likes.Tags, "dinners") {
		t.Errorf("likes.tags = %v, want the stored tag kept", normalized.Taste.Likes.Tags)
	}

	// And it still reaches the provider, so the exclusion keeps excluding.
	prefs := normalized.preferences(2)
	if !slices.Contains(prefs.Exclusions.Tags, "seo") {
		t.Errorf("provider exclusions = %v, want seo", prefs.Exclusions.Tags)
	}
	if len(prefs.Rules) != 1 || !slices.Contains(prefs.Rules[0].Tags, "seo") {
		t.Errorf("provider rules = %+v, want the stored tag", prefs.Rules)
	}
}
