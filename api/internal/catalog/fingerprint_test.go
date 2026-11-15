package catalog

import (
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

const testIngredientID = "66e5a1f2c3b4a5d6e7f8ff01"

// dish builds catalog content. lines are "name" or "name=quantity unit".
func dish(source, sourceID, url, name string, lines ...string) recipes.Recipe {
	r := recipes.Recipe{Source: source, SourceRecipeID: sourceID, SourceURL: url, Name: name, Servings: []int{2}, TotalMinutes: 30}
	for _, l := range lines {
		n, amount, _ := strings.Cut(l, "=")
		q, unit, _ := strings.Cut(amount, " ")
		if q == "" {
			q, unit = "1", "cup"
		}
		r.Ingredients = append(r.Ingredients, recipes.RecipeIngredient{
			IngredientID: testIngredientID, Name: n,
			Amounts: []recipes.Amount{{Servings: 2, Quantity: q, Unit: unit}},
		})
	}
	return r
}

func typed(name string, steps []string, lines ...string) recipes.Recipe {
	r := dish(recipes.SourceManual, "random-id-"+name, "", name, lines...)
	for i, s := range steps {
		r.Steps = append(r.Steps, recipes.Step{Index: i + 1, Text: s})
	}
	return r
}

func TestFingerprintKinds(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   recipes.Recipe
		want string
	}{
		{"importer id and no link", dish(recipes.SourceHelloFresh, "hf-1", "", "Tacos"), FingerprintSource},
		{"importer id and a link", dish(recipes.SourceHelloFresh, "hf-1", "https://hellofresh.com/r/1", "Tacos"), FingerprintURL},
		{"a link", dish(recipes.SourceUser, "random", "https://example.com/tacos", "Tacos"), FingerprintURL},
		{"typed", dish(recipes.SourceManual, "random", "", "Tacos", "beef"), FingerprintTyped},
		{"an import with no id", dish(recipes.SourceImport, "", "", "Tacos"), FingerprintTyped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Fingerprint(tc.in)
			if !strings.HasPrefix(got, tc.want) || len(got) != len(tc.want)+64 {
				t.Errorf("Fingerprint = %q; want %s and a sha256", got, tc.want)
			}
		})
	}
	if got := Fingerprint(recipes.Recipe{Source: recipes.SourceManual}); got != "" {
		t.Errorf("a recipe with nothing to identify it has fingerprint %q", got)
	}
}

func TestFingerprintTypedAndPastedIDsDoNotCount(t *testing.T) {
	// Typed and pasted recipes are given random IDs, so two copies of one
	// typed recipe must match even though their IDs differ.
	a := typed("Brownies", []string{"Mix"}, "cocoa")
	b := a
	b.SourceRecipeID = "another-random-id"
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("two copies of a typed recipe with different random IDs differ")
	}
}

func TestNormalizeSourceURL(t *testing.T) {
	same := []string{
		"https://www.example.com/recipes/brownies/",
		"http://example.com/recipes/brownies",
		"HTTPS://WWW.EXAMPLE.COM/recipes/brownies#comments",
		"example.com/recipes/brownies",
		"https://example.com/recipes/brownies?utm_source=pin&utm_medium=social&fbclid=abc",
		"https://example.com/recipes/brownies?gclid=1&mc_cid=2&mc_eid=3&ref=home&UTM_Campaign=x",
		"https://example.com:443/recipes/brownies",
	}
	want := "example.com/recipes/brownies"
	for _, raw := range same {
		if got := NormalizeSourceURL(raw); got != want {
			t.Errorf("NormalizeSourceURL(%q) = %q; want %q", raw, got, want)
		}
	}
	if got := NormalizeSourceURL("https://example.com/r?b=2&a=1&utm_source=x"); got != "example.com/r?a=1&b=2" {
		t.Errorf("kept params are not sorted: %q", got)
	}
	if NormalizeSourceURL("https://example.com/r?id=1") == NormalizeSourceURL("https://example.com/r?id=2") {
		t.Error("a query parameter that names the recipe was dropped")
	}
	if NormalizeSourceURL("https://example.com/Recipes/A") == NormalizeSourceURL("https://example.com/recipes/a") {
		t.Error("the path was lowercased; only the host is case-insensitive")
	}
	for _, raw := range []string{"", "   ", "not a url at all", "https://"} {
		if got := NormalizeSourceURL(raw); got != "" {
			t.Errorf("NormalizeSourceURL(%q) = %q; want empty", raw, got)
		}
	}
}

func TestFingerprintTypedRules(t *testing.T) {
	base := typed("Fudgy Brownies", []string{"Melt the butter.", "Fold in the cocoa."}, "butter=1/2 cup", "cocoa powder=0.5 cup", "sugar")
	for _, tc := range []struct {
		name string
		edit func(*recipes.Recipe)
		same bool
	}{
		{"case and whitespace", func(r *recipes.Recipe) {
			r.Name = "  fudgy   BROWNIES "
			r.Ingredients[1].Name = "Cocoa  Powder"
			r.Steps[0].Text = "melt the   butter."
		}, true},
		{"ingredient order", func(r *recipes.Recipe) {
			r.Ingredients[0], r.Ingredients[2] = r.Ingredients[2], r.Ingredients[0]
		}, true},
		{"the same amount written another way", func(r *recipes.Recipe) {
			r.Ingredients[0].Amounts[0].Quantity = "2/4"
		}, true},
		{"a different ingredient", func(r *recipes.Recipe) { r.Ingredients[0].Name = "coconut oil" }, false},
		{"a different amount", func(r *recipes.Recipe) { r.Ingredients[2].Amounts[0].Quantity = "2" }, false},
		{"a different unit", func(r *recipes.Recipe) { r.Ingredients[2].Amounts[0].Unit = "tbsp" }, false},
		{"a different step", func(r *recipes.Recipe) { r.Steps[1].Text = "Fold in the chocolate chips." }, false},
		{"steps in another order", func(r *recipes.Recipe) {
			r.Steps[0].Text, r.Steps[1].Text = r.Steps[1].Text, r.Steps[0].Text
		}, false},
		{"a different title", func(r *recipes.Recipe) { r.Name = "Cakey Brownies" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := base
			other.Ingredients = append([]recipes.RecipeIngredient(nil), base.Ingredients...)
			for i := range other.Ingredients {
				other.Ingredients[i].Amounts = append([]recipes.Amount(nil), base.Ingredients[i].Amounts...)
			}
			other.Steps = append([]recipes.Step(nil), base.Steps...)
			tc.edit(&other)
			if got := Fingerprint(other) == Fingerprint(base); got != tc.same {
				t.Errorf("same fingerprint = %v; want %v", got, tc.same)
			}
		})
	}
}

func TestDedupePicksTheMostCompleteThenTheOldestCopy(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	url := "https://example.com/pie"
	entry := func(id string, at time.Time, edit func(*recipes.Recipe)) Recipe {
		c := dish(recipes.SourceUser, id, url, "Pie", "apples")
		if edit != nil {
			edit(&c)
		}
		return Recipe{ID: id, CatalogKey: "fp:" + id, Content: c, Fingerprint: Fingerprint(c), FirstPublishedAt: at}
	}
	photo := func(r *recipes.Recipe) { r.ImageURL = "https://example.com/pie.jpg" }
	steps := func(r *recipes.Recipe) { r.Steps = []recipes.Step{{Index: 1, Text: "Bake"}} }
	nutrition := func(r *recipes.Recipe) { r.Nutrition = []recipes.Nutrient{{Name: "Calories", Amount: 400}} }
	both := func(r *recipes.Recipe) { photo(r); steps(r) }

	got := Dedupe([]Recipe{
		entry("a", t0, func(r *recipes.Recipe) { steps(r); nutrition(r) }), // 3
		entry("b", t0, photo),                    // 4
		entry("c", t0.Add(2*time.Hour), both),    // 6, newer
		entry("d", t0.Add(time.Hour), both),      // 6, older: the one shown
		entry("e", t0.Add(time.Hour), nutrition), // 1
		entry("f", t0.Add(-time.Hour), nil),      // 0, oldest
	})
	if len(got) != 1 {
		t.Fatalf("Dedupe = %d groups; want 1", len(got))
	}
	if got[0].ID != "d" {
		t.Errorf("representative = %s; want d, the older of the two with a photo and steps", got[0].ID)
	}
	if len(got[0].GroupKeys) != 6 || got[0].GroupKeys[0] != "fp:d" {
		t.Errorf("group keys = %v; want all six, the representative's first", got[0].GroupKeys)
	}

	// Same completeness and the same time: the lower ID wins.
	tie := Dedupe([]Recipe{entry("y", t0, photo), entry("x", t0, photo)})
	if tie[0].ID != "x" {
		t.Errorf("tie representative = %s; want x", tie[0].ID)
	}
}
