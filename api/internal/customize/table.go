package customize

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Family groups proteins that swap with each other.
type Family string

// Families.
const (
	FamilyGround  Family = "ground"
	FamilyChicken Family = "chicken"
	FamilyPork    Family = "pork"
	FamilySteak   Family = "steak"
	FamilySausage Family = "sausage"
	FamilySeafood Family = "seafood"
	FamilyTofu    Family = "tofu"
)

// Protein is one curated protein.
type Protein struct {
	// ID is stable: it appears in stored choice IDs ("swap:ground-beef").
	ID string
	// Name is the display name. A swap uses the catalog ingredient with this
	// name (its ID, category, and image) when there is one.
	Name   string
	Family Family
	// Aliases are other ingredient names that are this protein. Names match
	// after ingredients.NormalizeName.
	Aliases []string
	// Proteins and Allergens use Autopilot's values
	// (recommendations.ProteinOptions and AllergenOptions).
	Proteins  []string
	Allergens []string
	// Meat is land meat, which vegetarian and pescatarian diets exclude.
	// Seafood is fish or shellfish, which vegetarian and vegan diets exclude.
	Meat    bool
	Seafood bool
}

// Ratio sets the amount of To used per amount of From. Pairs without a ratio
// swap the same weight.
type Ratio struct {
	From, To string
	Value    *big.Rat
}

// Table is a validated protein table. It is read-only after NewTable.
type Table struct {
	proteins []Protein
	byID     map[string]int
	byName   map[string]int
	extras   map[Family][]string
	ratios   map[[2]string]*big.Rat
}

// NewTable validates and indexes proteins. extras lists, per family, the
// proteins of other families it also swaps with; ratios override same-weight
// swaps for single pairs.
func NewTable(proteins []Protein, extras map[Family][]string, ratios []Ratio) (*Table, error) {
	t := &Table{
		proteins: proteins, byID: map[string]int{}, byName: map[string]int{},
		extras: map[Family][]string{}, ratios: map[[2]string]*big.Rat{},
	}
	for i, p := range proteins {
		if p.ID == "" || p.Name == "" || p.Family == "" {
			return nil, fmt.Errorf("customize: protein %d needs an id, name, and family", i)
		}
		if _, dup := t.byID[p.ID]; dup {
			return nil, fmt.Errorf("customize: duplicate protein id %q", p.ID)
		}
		t.byID[p.ID] = i
		for _, name := range append([]string{p.Name}, p.Aliases...) {
			key := ingredients.NormalizeName(name)
			if key == "" {
				return nil, fmt.Errorf("customize: protein %q has an empty name", p.ID)
			}
			if other, dup := t.byName[key]; dup {
				return nil, fmt.Errorf("customize: name %q belongs to %q and %q", key, proteins[other].ID, p.ID)
			}
			t.byName[key] = i
		}
	}
	for family, ids := range extras {
		for _, id := range ids {
			if _, ok := t.byID[id]; !ok {
				return nil, fmt.Errorf("customize: family %q swaps with unknown protein %q", family, id)
			}
		}
		t.extras[family] = append([]string(nil), ids...)
	}
	for _, r := range ratios {
		_, fromOK := t.byID[r.From]
		_, toOK := t.byID[r.To]
		if !fromOK || !toOK || r.Value == nil || r.Value.Sign() <= 0 {
			return nil, errors.New("customize: a ratio needs known proteins and a positive value")
		}
		t.ratios[[2]string{r.From, r.To}] = new(big.Rat).Set(r.Value)
	}
	return t, nil
}

// Proteins returns the table's proteins in order.
func (t *Table) Proteins() []Protein { return append([]Protein(nil), t.proteins...) }

// Protein returns the protein with id.
func (t *Table) Protein(id string) (Protein, bool) {
	i, ok := t.byID[id]
	if !ok {
		return Protein{}, false
	}
	return t.proteins[i], true
}

// Match returns the protein an ingredient name is, by exact normalized name
// or alias. "Chicken Stock Concentrate" is no protein.
func (t *Table) Match(name string) (Protein, bool) {
	i, ok := t.byName[ingredients.NormalizeName(name)]
	if !ok {
		return Protein{}, false
	}
	return t.proteins[i], true
}

// Swaps returns what p swaps with, in table order: the other members of its
// family, then its family's extras.
func (t *Table) Swaps(p Protein) []Protein {
	var out []Protein
	seen := map[string]bool{p.ID: true}
	for _, q := range t.proteins {
		if q.Family == p.Family && !seen[q.ID] {
			seen[q.ID] = true
			out = append(out, q)
		}
	}
	for _, id := range t.extras[p.Family] {
		if !seen[id] {
			seen[id] = true
			out = append(out, t.proteins[t.byID[id]])
		}
	}
	return out
}

// Ratio is the amount of to used per amount of from: 1 unless the table sets
// one for the pair.
func (t *Table) Ratio(from, to string) *big.Rat {
	if r, ok := t.ratios[[2]string{from, to}]; ok {
		return new(big.Rat).Set(r)
	}
	return big.NewRat(1, 1)
}

// DefaultTable returns the curated table (defaultProteins).
func DefaultTable() *Table { return defaultTable }

var defaultTable = func() *Table {
	t, err := NewTable(defaultProteins, defaultExtras, nil)
	if err != nil {
		panic(err)
	}
	return t
}()

func meat(protein string) []string { return []string{protein} }

// defaultProteins are the proteins meal kits ship, named as they appear on
// recipes. Names and aliases were checked against an imported catalog of
// about 400 meal-kit recipes, where every protein line is a weight.
var defaultProteins = []Protein{
	{ID: "ground-beef", Name: "Ground Beef", Family: FamilyGround, Proteins: meat("beef"), Meat: true},
	{ID: "ground-pork", Name: "Ground Pork", Family: FamilyGround, Proteins: meat("pork"), Meat: true},
	{ID: "ground-turkey", Name: "Ground Turkey", Family: FamilyGround, Proteins: meat("turkey"), Meat: true},
	{ID: "ground-chicken", Name: "Ground Chicken", Family: FamilyGround, Proteins: meat("chicken"), Meat: true},

	{ID: "chopped-chicken-breast", Name: "Chopped Chicken Breast", Family: FamilyChicken, Aliases: []string{"Diced Chicken Breast"}, Proteins: meat("chicken"), Meat: true},
	{ID: "chicken-breast-strips", Name: "Chicken Breast Strips", Family: FamilyChicken, Aliases: []string{"Chicken Strips"}, Proteins: meat("chicken"), Meat: true},
	{ID: "chicken-cutlets", Name: "Chicken Cutlets", Family: FamilyChicken, Aliases: []string{"Organic Chicken Cutlets"}, Proteins: meat("chicken"), Meat: true},
	{ID: "chicken-breasts", Name: "Chicken Breasts", Family: FamilyChicken, Aliases: []string{"Chicken Breast", "Boneless Skinless Chicken Breasts"}, Proteins: meat("chicken"), Meat: true},
	{ID: "diced-chicken-thighs", Name: "Diced Chicken Thighs", Family: FamilyChicken, Aliases: []string{"Chicken Thighs", "Boneless Chicken Thighs", "Diced Skinless Dark Meat Chicken"}, Proteins: meat("chicken"), Meat: true},

	{ID: "pork-chops", Name: "Pork Chops", Family: FamilyPork, Aliases: []string{"Boneless Pork Chops", "Bone-In Pork Chops"}, Proteins: meat("pork"), Meat: true},
	{ID: "pork-tenderloin", Name: "Pork Tenderloin", Family: FamilyPork, Aliases: []string{"Pork Filet"}, Proteins: meat("pork"), Meat: true},
	{ID: "pork-cutlets", Name: "Pork Cutlets", Family: FamilyPork, Proteins: meat("pork"), Meat: true},

	{ID: "sirloin-steak", Name: "Sirloin Steak", Family: FamilySteak, Aliases: []string{"Steak", "Sirloin", "Sirloin Steaks"}, Proteins: meat("beef"), Meat: true},
	{ID: "ranch-steak", Name: "Ranch Steak", Family: FamilySteak, Proteins: meat("beef"), Meat: true},
	{ID: "bavette-steak", Name: "Bavette Steak", Family: FamilySteak, Proteins: meat("beef"), Meat: true},
	{ID: "beef-tenderloin-steak", Name: "Beef Tenderloin Steak", Family: FamilySteak, Proteins: meat("beef"), Meat: true},

	{ID: "italian-pork-sausage", Name: "Italian Pork Sausage", Family: FamilySausage, Aliases: []string{"Italian Pork Sausage Mix", "Italian Sausage", "Pork Sausage"}, Proteins: meat("pork"), Meat: true},
	{ID: "italian-chicken-sausage", Name: "Italian Chicken Sausage", Family: FamilySausage, Aliases: []string{"Italian Chicken Sausage Mix", "Chicken Sausage"}, Proteins: meat("chicken"), Meat: true},

	{ID: "shrimp", Name: "Shrimp", Family: FamilySeafood, Proteins: []string{"shellfish"}, Allergens: []string{"shellfish"}, Seafood: true},
	{ID: "salmon", Name: "Salmon Fillets", Family: FamilySeafood, Aliases: []string{"Salmon", "Skin-On Salmon Fillets"}, Proteins: []string{"fish"}, Allergens: []string{"fish"}, Seafood: true},
	{ID: "tilapia", Name: "Tilapia", Family: FamilySeafood, Aliases: []string{"Tilapia Fillets"}, Proteins: []string{"fish"}, Allergens: []string{"fish"}, Seafood: true},
	{ID: "barramundi", Name: "Barramundi", Family: FamilySeafood, Aliases: []string{"Barramundi Fillets"}, Proteins: []string{"fish"}, Allergens: []string{"fish"}, Seafood: true},
	{ID: "cod", Name: "Cod", Family: FamilySeafood, Aliases: []string{"Cod Fillets"}, Proteins: []string{"fish"}, Allergens: []string{"fish"}, Seafood: true},

	{ID: "tofu", Name: "Tofu", Family: FamilyTofu, Aliases: []string{"Firm Tofu", "Extra Firm Tofu"}, Proteins: []string{"tofu"}, Allergens: []string{"soy"}},
}

// defaultExtras are the swaps across families:
//
//   - ground meats also swap with chopped chicken breast;
//   - chicken cuts with pork chops and tofu;
//   - pork cuts with chicken cutlets and chicken breasts;
//   - steaks with pork chops and chicken breasts;
//   - sausage with ground pork and ground turkey;
//   - seafood with chopped chicken breast;
//   - tofu with chopped chicken breast and shrimp.
var defaultExtras = map[Family][]string{
	FamilyGround:  {"chopped-chicken-breast"},
	FamilyChicken: {"pork-chops", "tofu"},
	FamilyPork:    {"chicken-cutlets", "chicken-breasts"},
	FamilySteak:   {"pork-chops", "chicken-breasts"},
	FamilySausage: {"ground-pork", "ground-turkey"},
	FamilySeafood: {"chopped-chicken-breast"},
	FamilyTofu:    {"chopped-chicken-breast", "shrimp"},
}
