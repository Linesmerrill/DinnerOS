package shopping

import (
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

func TestSearchTermsForBiasesByCategory(t *testing.T) {
	tests := []struct {
		name       string
		ingredient string
		category   string
		wantQuery  string
	}{
		{"produce goes fresh and whole", "Garlic", ingredients.CategoryProduce, "fresh whole Garlic"},
		{"produce multiword", "Green Beans", ingredients.CategoryProduce, "fresh whole Green Beans"},
		{"meat goes fresh", "Ground Beef", ingredients.CategoryMeatSeafood, "fresh Ground Beef"},
		{"frozen goes frozen", "Peas", ingredients.CategoryFrozen, "frozen Peas"},
		{"dairy has no qualifier", "Sour Cream", ingredients.CategoryDairyEggs, "Sour Cream"},
		{"spices have no qualifier", "Cumin", ingredients.CategorySpices, "Cumin"},
		{"pantry is left alone", "Jasmine Rice", ingredients.CategoryPantry, "Jasmine Rice"},
		{"condiments are left alone", "Sriracha", ingredients.CategoryCondiments, "Sriracha"},
		{"unknown category is left alone", "Skewers", ingredients.CategoryOther, "Skewers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SearchTermsFor(tt.ingredient, tt.category)
			if got.Query != tt.wantQuery {
				t.Errorf("SearchTermsFor(%q, %q).Query = %q, want %q", tt.ingredient, tt.category, got.Query, tt.wantQuery)
			}
		})
	}
}

// The owner's case: searching "garlic" returns powder and snacks, and the
// product he wanted is titled "Garlic Bulb Fresh Whole, Each".
func TestSearchTermsForGarlicMatchesTheWantedTitle(t *testing.T) {
	got := SearchTermsFor("Garlic", ingredients.CategoryProduce)
	if got.Query != "fresh whole Garlic" {
		t.Fatalf("Query = %q, want %q", got.Query, "fresh whole Garlic")
	}
	title := strings.ToLower("Garlic Bulb Fresh Whole, Each")
	for _, word := range strings.Fields(strings.ToLower(got.Query)) {
		if !strings.Contains(title, word) {
			t.Errorf("query word %q is not in the wanted product title %q", word, title)
		}
	}
	for _, avoid := range []string{"powder", "minced", "dried", "snack"} {
		if !slices.Contains(got.Avoid, avoid) {
			t.Errorf("Avoid = %v, want it to include %q", got.Avoid, avoid)
		}
	}
	if got.Why == "" {
		t.Error("Why is empty; the bias should be explainable to the member")
	}
}

// A name that already names a form is what the recipe asked for, so no
// qualifier is added. This is the rule that keeps the table free of
// per-ingredient exceptions.
func TestSearchTermsForKeepsNamedForms(t *testing.T) {
	tests := []struct {
		name       string
		ingredient string
		category   string
	}{
		{"powder stays powder", "Garlic Powder", ingredients.CategorySpices},
		{"minced stays minced", "Minced Garlic", ingredients.CategoryProduce},
		{"frozen produce stays frozen", "Frozen Corn", ingredients.CategoryProduce},
		{"dried stays dried", "Dried Apricots", ingredients.CategoryProduce},
		{"canned meat stays canned", "Canned Tuna", ingredients.CategoryMeatSeafood},
		{"a fresh herb in spices stays fresh", "Fresh Thyme", ingredients.CategorySpices},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SearchTermsFor(tt.ingredient, tt.category)
			if got.Query != tt.ingredient {
				t.Errorf("SearchTermsFor(%q, %q).Query = %q, want the name unchanged", tt.ingredient, tt.category, got.Query)
			}
			if len(got.Qualifiers) != 0 {
				t.Errorf("Qualifiers = %v, want none for a name that already names a form", got.Qualifiers)
			}
		})
	}
}

// Whole-word matching: "chipotle" must not read as "chips", and
// "freshwater" must not read as "fresh".
func TestSearchTermsForMatchesWholeWords(t *testing.T) {
	if got := SearchTermsFor("Chipotle Pepper", ingredients.CategoryProduce); got.Query != "fresh whole Chipotle Pepper" {
		t.Errorf("Query = %q, want the qualifier applied: \"chips\" must not match \"chipotle\"", got.Query)
	}
	if got := SearchTermsFor("Freshwater Bass", ingredients.CategoryMeatSeafood); got.Query != "fresh Freshwater Bass" {
		t.Errorf("Query = %q, want the qualifier applied: \"fresh\" must not match \"freshwater\"", got.Query)
	}
}

func TestSearchTermsForEmptyName(t *testing.T) {
	if got := SearchTermsFor("   ", ingredients.CategoryProduce); got.Query != "" || got.Why != "" {
		t.Errorf("SearchTermsFor(blank) = %+v, want the zero value", got)
	}
}

// The table is data, so callers must not be able to write into it.
func TestSearchTermsForDoesNotAliasTheTable(t *testing.T) {
	first := SearchTermsFor("Garlic", ingredients.CategoryProduce)
	first.Avoid[0] = "mutated"
	first.Qualifiers[0] = "mutated"
	second := SearchTermsFor("Garlic", ingredients.CategoryProduce)
	if second.Avoid[0] == "mutated" || second.Qualifiers[0] == "mutated" {
		t.Error("SearchTermsFor returns slices aliasing the profile table")
	}
}
