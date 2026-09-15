package customize

import (
	"math/big"
	"slices"
	"strings"
	"testing"
)

func proteinIDs(ps []Protein) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func TestDefaultTableSwapFamilies(t *testing.T) {
	table := DefaultTable()
	for _, tc := range []struct {
		name string
		want []string
	}{
		// Ground meats swap with each other and chopped chicken.
		{"Ground Pork", []string{"ground-beef", "ground-turkey", "ground-chicken", "chopped-chicken-breast"}},
		{"Ground Turkey", []string{"ground-beef", "ground-pork", "ground-chicken", "chopped-chicken-breast"}},
		// Chicken cuts swap with each other, pork chops, and tofu.
		{"Chicken Cutlets", []string{"chopped-chicken-breast", "chicken-breast-strips", "chicken-breasts", "diced-chicken-thighs", "pork-chops", "tofu"}},
		{"Diced Skinless Dark Meat Chicken", []string{"chopped-chicken-breast", "chicken-breast-strips", "chicken-cutlets", "chicken-breasts", "pork-chops", "tofu"}},
		// Seafood swaps with seafood and chicken.
		{"Shrimp", []string{"salmon", "tilapia", "barramundi", "cod", "chopped-chicken-breast"}},
		{"Pork Filet", []string{"pork-chops", "pork-cutlets", "chicken-cutlets", "chicken-breasts"}},
		{"Ranch Steak", []string{"sirloin-steak", "bavette-steak", "beef-tenderloin-steak", "pork-chops", "chicken-breasts"}},
		{"Italian Chicken Sausage Mix", []string{"italian-pork-sausage", "ground-pork", "ground-turkey"}},
		{"Extra Firm Tofu", []string{"chopped-chicken-breast", "shrimp"}},
	} {
		p, ok := table.Match(tc.name)
		if !ok {
			t.Errorf("Match(%q) found nothing", tc.name)
			continue
		}
		if got := proteinIDs(table.Swaps(p)); !slices.Equal(got, tc.want) {
			t.Errorf("Swaps(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTableMatch(t *testing.T) {
	table := DefaultTable()
	for name, want := range map[string]string{
		"Ground Beef":                      "ground-beef",
		"ground  beef":                     "ground-beef",
		"Chopped Chicken Breast":           "chopped-chicken-breast",
		"Diced Chicken Breast":             "chopped-chicken-breast",
		"Bone-In Pork Chops":               "pork-chops",
		"Diced Skinless Dark Meat Chicken": "diced-chicken-thighs",
		"Italian Pork Sausage Mix":         "italian-pork-sausage",
		"Salmon":                           "salmon",
	} {
		if p, ok := table.Match(name); !ok || p.ID != want {
			t.Errorf("Match(%q) = %q, %v; want %q", name, p.ID, ok, want)
		}
	}
	for _, name := range []string{"Chicken Stock Concentrate", "Bacon", "Bold & Savory Steak Spice", "BBQ Pulled Chicken", "Yellow Onion", ""} {
		if p, ok := table.Match(name); ok {
			t.Errorf("Match(%q) = %q, want no protein", name, p.ID)
		}
	}
	if p, ok := table.Protein("tofu"); !ok || p.Name != "Tofu" || len(table.Proteins()) < 20 {
		t.Errorf("Protein(tofu) = %+v, %v", p, ok)
	}
	if _, ok := table.Protein("lobster"); ok {
		t.Error("Protein(lobster) found")
	}
}

func TestDefaultTableRestrictionTags(t *testing.T) {
	for _, p := range DefaultTable().Proteins() {
		if len(p.Proteins) == 0 {
			t.Errorf("%s has no Autopilot protein", p.ID)
		}
		if p.Meat && p.Seafood {
			t.Errorf("%s is both meat and seafood", p.ID)
		}
		if p.Family == FamilySeafood && (!p.Seafood || len(p.Allergens) == 0) {
			t.Errorf("seafood %s isn't tagged as seafood with an allergen", p.ID)
		}
	}
}

func TestNewTableValidation(t *testing.T) {
	a := Protein{ID: "a", Name: "Alpha Meat", Family: "x"}
	b := Protein{ID: "b", Name: "Beta Meat", Family: "y"}
	for _, tc := range []struct {
		name     string
		proteins []Protein
		extras   map[Family][]string
		ratios   []Ratio
		want     string
	}{
		{"missing fields", []Protein{{ID: "a"}}, nil, nil, "needs an id"},
		{"duplicate id", []Protein{a, {ID: "a", Name: "Other", Family: "x"}}, nil, nil, "duplicate protein id"},
		{"duplicate name", []Protein{a, {ID: "c", Name: "alpha meat", Family: "x"}}, nil, nil, "belongs to"},
		{"empty alias", []Protein{{ID: "a", Name: "Alpha", Family: "x", Aliases: []string{"!!"}}}, nil, nil, "empty name"},
		{"unknown extra", []Protein{a, b}, map[Family][]string{"x": {"zzz"}}, nil, "unknown protein"},
		{"bad ratio", []Protein{a, b}, nil, []Ratio{{From: "a", To: "b", Value: big.NewRat(0, 1)}}, "positive value"},
		{"unknown ratio protein", []Protein{a, b}, nil, []Ratio{{From: "a", To: "zzz", Value: big.NewRat(1, 2)}}, "known proteins"},
	} {
		if _, err := NewTable(tc.proteins, tc.extras, tc.ratios); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: NewTable() error = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestTableRatios(t *testing.T) {
	table, err := NewTable([]Protein{
		{ID: "chicken", Name: "Chicken Breasts", Family: "poultry"},
		{ID: "shrimp", Name: "Shrimp", Family: "seafood"},
	}, map[Family][]string{"poultry": {"shrimp"}}, []Ratio{{From: "chicken", To: "shrimp", Value: big.NewRat(3, 4)}})
	if err != nil {
		t.Fatal(err)
	}
	chicken, _ := table.Protein("chicken")
	if r := table.Ratio("chicken", "shrimp"); r.Cmp(big.NewRat(3, 4)) != 0 {
		t.Errorf("Ratio(chicken, shrimp) = %v", r)
	}
	if r := table.Ratio("shrimp", "chicken"); r.Cmp(big.NewRat(1, 1)) != 0 {
		t.Errorf("Ratio(shrimp, chicken) = %v, want same weight", r)
	}
	swap, ok := table.Resolve(chicken, "Chicken Breasts", "swap:shrimp")
	double, _ := table.Resolve(chicken, "Chicken Breasts", "swap:shrimp:double")
	if !ok || swap.Factor.Cmp(big.NewRat(3, 4)) != 0 || double.Factor.Cmp(big.NewRat(3, 2)) != 0 {
		t.Errorf("shrimp factors = %v, %v", swap.Factor, double.Factor)
	}
}
