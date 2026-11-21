package cuisine

import (
	"slices"
	"strings"
	"testing"
	"unicode"
)

func TestTable(t *testing.T) {
	seen := map[string]string{}
	for _, e := range Entries() {
		for _, key := range append([]string{e.Value}, e.Aliases...) {
			if Normalize(key) != key {
				t.Errorf("%q isn't normalized", key)
			}
			if other, dup := seen[key]; dup {
				t.Errorf("%q maps to both %q and %q", key, other, e.Value)
			}
			seen[key] = e.Value
			if got := Canonical(key); got != e.Value {
				t.Errorf("Canonical(%q) = %q, want %q", key, got, e.Value)
			}
		}
		if strings.ToLower(e.Label) != e.Value {
			t.Errorf("label %q doesn't spell %q", e.Label, e.Value)
		}
		for _, word := range strings.FieldsFunc(e.Label, func(r rune) bool { return r == ' ' || r == '-' }) {
			if !unicode.IsUpper([]rune(word)[0]) {
				t.Errorf("label %q isn't title case", e.Label)
			}
		}
		if e.Parent != "" && Label(e.Parent) == "" {
			t.Errorf("%q has unknown parent %q", e.Value, e.Parent)
		}
		if n := len(Ancestors(e.Value)); n > 3 {
			t.Errorf("%q has %d ancestors; the hierarchy is cyclic or too deep", e.Value, n)
		}
	}
	for c := range dishCuisines {
		if !Known(c) {
			t.Errorf("dish list for unknown cuisine %q", c)
		}
	}
}

// The standard list is every country and style plus Mediterranean and
// Middle Eastern, and no broad regions (decision 722).
func TestStandard(t *testing.T) {
	std := Standard()
	for _, want := range []string{"american", "mexican", "italian", "indian", "thai", "mediterranean", "middle eastern", "tex-mex", "ethiopian"} {
		if !slices.Contains(std, want) {
			t.Errorf("standard list is missing %q", want)
		}
	}
	for _, not := range []string{"asian", "european", "latin american", "fusion", "north american"} {
		if slices.Contains(std, not) {
			t.Errorf("standard list has region %q", not)
		}
	}
	if len(std) < 35 {
		t.Errorf("standard list has %d cuisines", len(std))
	}
}

// What a person sees is always a standard cuisine: variants of one map onto
// it, and regions, styles that aren't on the list, and unknown words are
// left out rather than guessed (decision 732).
func TestShown(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want []string
	}{
		{[]string{"North America"}, []string{"american"}},
		{[]string{"North American"}, []string{"american"}},
		{[]string{"american"}, []string{"american"}},
		{[]string{" USA "}, []string{"american"}},
		{[]string{"United States"}, []string{"american"}},
		{[]string{"North America", "North American", "American"}, []string{"american"}},
		{[]string{"Southern Europe"}, nil},
		{[]string{"Southern European", "Italian"}, []string{"italian"}},
		{[]string{"East Asia", "Japanese"}, []string{"japanese"}},
		{[]string{"Asian"}, nil},
		{[]string{"Latin American", "Fusion", "Caribbean"}, nil},
		{[]string{"Middle East"}, []string{"middle eastern"}},
		{[]string{"Tex Mex", "tex-mex"}, []string{"tex-mex"}},
		{[]string{"Southern"}, []string{"southern"}},
		{[]string{"Klingon", "Comfort Food", ""}, nil},
		{[]string{"thai", "korean", "greek", "french", "german", "irish"}, []string{"thai", "korean", "greek", "french", "german"}},
		{nil, nil},
	} {
		if got := Shown(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("Shown(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, v := range Shown([]string{"North America", "Italian", "Middle East"}) {
		if !IsStandard(v) || DisplayLabel(v) == "" {
			t.Errorf("%q isn't a standard cuisine with a label", v)
		}
	}
	if DisplayLabel("american") != "American" {
		t.Errorf("american's label = %q", DisplayLabel("american"))
	}
	if IsStandard("north american") || IsStandard("southern european") || IsStandard("klingon") || !IsStandard("american") {
		t.Error("IsStandard is wrong for a region, an unknown word, or American")
	}
	// American still covers its styles for matching.
	if got := Ancestors("cajun"); !slices.Equal(got, []string{"american"}) {
		t.Errorf("Ancestors(cajun) = %v", got)
	}
}

func TestClean(t *testing.T) {
	got := Clean([]string{" Mexican", "mexican", "", "Middle East", "Tex Mex", strings.Repeat("x", 61), "Klingon", "Thai", "Greek", "Korean"})
	want := []string{"mexican", "middle eastern", "tex-mex", "klingon", "thai"}
	if !slices.Equal(got, want) {
		t.Errorf("Clean = %v, want %v", got, want)
	}
	if DisplayLabel("tex-mex") != "Tex-Mex" || DisplayLabel("klingon fusion") != "Klingon Fusion" {
		t.Errorf("DisplayLabel = %q, %q", DisplayLabel("tex-mex"), DisplayLabel("klingon fusion"))
	}
}

// Titles here are made up; none come from a real recipe.
func TestGuess(t *testing.T) {
	for _, tc := range []struct {
		name string
		tags []string
		want string
	}{
		// A dish that belongs to one cuisine.
		{"Chicken Tikka Masala", nil, "indian"},
		{"Carnitas Tacos", nil, "mexican"},
		{"QA Carnitas Tacos 4821", nil, "mexican"},
		{"Pesto Penne", nil, "italian"},
		{"Sheet Pan Chicken Fajitas", nil, "mexican"},
		{"Beef Bulgogi Bowls", nil, "korean"},
		{"Weeknight Pad See Ew", nil, "thai"},
		{"Shepherd's Pie", nil, "british"},
		{"Shrimp Étouffée", nil, "cajun"},
		{"Pork Bánh Mì", nil, "vietnamese"},
		{"Lamb Kofta with Hummus", nil, "middle eastern"},
		{"Spaghetti Bolognese", nil, "italian"},
		{"Teriyaki Salmon", nil, "japanese"},
		{"Chicken Shawarma Wraps", nil, "middle eastern"},
		{"Beef Stroganoff", nil, "russian"},
		// A cuisine named in the title wins over a dish word.
		{"Korean Tacos", nil, "korean"},
		{"Thai Basil Chicken", nil, "thai"},
		{"Greek Chicken Pizza", nil, "greek"},
		{"Mediterranean Quinoa Bowl", nil, "mediterranean"},
		{"Tex-Mex Rice Skillet", nil, "tex-mex"},
		{"Cajun Shrimp Pasta", nil, "cajun"},
		{"All-American Cheeseburgers", nil, "american"},
		// A region says too little: the dish word decides, or nothing does.
		{"Asian Chicken Lettuce Wraps", nil, ""},
		{"Caribbean Jerk Chicken", nil, "jamaican"},
		{"Weeknight Noodles", []string{"East Asian"}, ""},
		{"Penne Bake", []string{"Southern Europe"}, "italian"},
		{"Weeknight Burgers", []string{"North America"}, "american"},
		// A tag that names a cuisine wins over the title.
		{"Weeknight Noodles", []string{"Quick", "Japanese"}, "japanese"},
		{"Chicken Bowls", []string{"Family Friendly", "Tacos"}, "mexican"},
		// Nothing says: no guess.
		{"Chicken Curry", nil, ""},
		{"Grilled Chicken with Rice", nil, ""},
		{"Beef Stew", nil, ""},
		{"Steak and French Fries", nil, ""},
		{"Salmon with Greek Yogurt Sauce", nil, ""},
		{"Pacific Cod with Lemon", nil, ""},
		{"Turkey Burgers", nil, ""},
		{"Fried Rice", nil, ""},
		{"Smoked Brisket", []string{"Quick", "High Protein"}, ""},
		{"Tacoma Meatloaf", nil, ""},
		{"Phoenix Chicken Salad", nil, ""},
		{"", nil, ""},
	} {
		got := Guess(tc.name, tc.tags)
		var want []string
		if tc.want != "" {
			want = []string{tc.want}
		}
		if !slices.Equal(got, want) {
			t.Errorf("Guess(%q, %v) = %v, want %v", tc.name, tc.tags, got, want)
		}
	}
}

// A recipe's own cuisines win; a household's "none" stays none; otherwise
// the guess fills in.
func TestFill(t *testing.T) {
	if got := Fill([]string{"Italian"}, false, "Carnitas Tacos", nil); !slices.Equal(got, []string{"italian"}) {
		t.Errorf("own = %v", got)
	}
	if got := Fill(nil, true, "Carnitas Tacos", nil); got != nil {
		t.Errorf("chosen none = %v", got)
	}
	if got := Fill(nil, false, "Carnitas Tacos", nil); !slices.Equal(got, []string{"mexican"}) {
		t.Errorf("guess = %v", got)
	}
	// Stored variants read as the standard cuisine; a pure region gives way
	// to the title's guess, or to nothing.
	for _, tc := range []struct {
		own    []string
		chosen bool
		name   string
		want   []string
	}{
		{[]string{"North America"}, false, "Burgers", []string{"american"}},
		{[]string{"Southern Europe"}, false, "Pesto Penne", []string{"italian"}},
		{[]string{"Southern Europe"}, false, "Roast Chicken", nil},
		{[]string{"Southern Europe"}, true, "Pesto Penne", nil},
		{[]string{"East Asian", "Korean"}, false, "Pesto Penne", []string{"korean"}},
	} {
		if got := Fill(tc.own, tc.chosen, tc.name, nil); !slices.Equal(got, tc.want) {
			t.Errorf("Fill(%q, %v, %q) = %q, want %q", tc.own, tc.chosen, tc.name, got, tc.want)
		}
	}
}

// HelloFresh files enchiladas verdes under "North American"; the title says
// Mexican, so that's what shows. A plain American dish, an American style
// ("Cajun"), or a household's own choice stays as it is.
func TestFillPrefersTheTitleOverPlainAmerican(t *testing.T) {
	for _, tc := range []struct {
		own    []string
		chosen bool
		name   string
		want   []string
	}{
		{[]string{"North American"}, false, "One-Pan Chicken Enchiladas Verdes", []string{"mexican"}},
		{[]string{"North American"}, false, "Cheeseburger Sliders", []string{"american"}},
		{[]string{"North American"}, false, "Cajun Shrimp Tacos", []string{"american"}},
		{[]string{"North American"}, true, "Chicken Enchiladas", []string{"american"}},
		{[]string{"North American", "Italian"}, false, "Chicken Enchiladas", []string{"american", "italian"}},
	} {
		if got := Fill(tc.own, tc.chosen, tc.name, nil); !slices.Equal(got, tc.want) {
			t.Errorf("Fill(%v, %v, %q) = %v, want %v", tc.own, tc.chosen, tc.name, got, tc.want)
		}
	}
}
