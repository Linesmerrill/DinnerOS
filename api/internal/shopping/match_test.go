package shopping

import (
	"errors"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// Item IDs are made up.
const (
	beefKey  = "66e5a1f2c3b4a5d6e7f80101"
	onionKey = "66e5a1f2c3b4a5d6e7f80102"
	pasteKey = "66e5a1f2c3b4a5d6e7f80103"
	hintKey  = "66e5a1f2c3b4a5d6e7f80104"
	blendKey = "66e5a1f2c3b4a5d6e7f80105"
)

func groceryAmount(t *testing.T, quantity, unit string) grocery.Amount {
	t.Helper()
	q, err := ingredients.ParseQuantity(quantity)
	if err != nil {
		t.Fatal(err)
	}
	u, err := ingredients.LookupUnit(unit)
	if err != nil {
		t.Fatal(err)
	}
	return grocery.Amount{Quantity: q, Unit: u}
}

func testList(t *testing.T) planning.GroceryList {
	t.Helper()
	week, err := planning.ParseWeek("2026-W38")
	if err != nil {
		t.Fatal(err)
	}
	return planning.GroceryList{Week: week, Categories: []planning.GroceryCategory{
		{Category: "produce", Items: []grocery.Item{
			{IngredientKey: onionKey, Name: "Yellow Onion", Amounts: []grocery.Amount{groceryAmount(t, "2", "count")}, Status: grocery.StatusToBuy},
			{IngredientKey: "name:shallot", Name: "Shallot", Unquantified: true, Status: grocery.StatusToBuy},
		}},
		{Category: "meat-seafood", Items: []grocery.Item{
			{IngredientKey: beefKey, Name: "Ground Beef", Amounts: []grocery.Amount{groceryAmount(t, "20", "oz")}, Status: grocery.StatusToBuy},
		}},
		{Category: "pantry", Items: []grocery.Item{
			{IngredientKey: pasteKey, Name: "Tomato Paste", Amounts: []grocery.Amount{groceryAmount(t, "2", "tbsp")}, Status: grocery.StatusInPantry},
			{IngredientKey: hintKey, Name: "Olive Oil", Status: grocery.StatusPantryHint, Unquantified: true},
			{IngredientKey: blendKey, Name: "Southwest Spice Blend", Status: grocery.StatusInPantry, Specialty: &grocery.LineSpecialty{HouseMade: true}},
		}},
	}}
}

func testPrefs() []Preference {
	return []Preference{
		{IngredientKey: beefKey, ProductID: "100000001", DisplayName: "Beef 16 oz", PackageSize: &PackageSize{Quantity: "16", Unit: "oz"}},
		{IngredientKey: onionKey, ProductID: "100000002", DisplayName: "Onions 3 ct", PackageSize: &PackageSize{Quantity: "3", Unit: "count"}},
		{IngredientKey: pasteKey, ProductID: "100000003", DisplayName: "Paste"},
		{IngredientKey: hintKey, ProductID: "100000004", DisplayName: "Oil"},
		{IngredientKey: blendKey, ProductID: "100000005", DisplayName: "Blend"},
	}
}

func reasons(p Proposal) map[string]ExclusionReason {
	out := map[string]ExclusionReason{}
	for _, e := range p.Excluded {
		out[e.IngredientKey] = e.Reason
	}
	return out
}

func TestBuildProposalDefaults(t *testing.T) {
	w := providers.NewWalmart(providers.WalmartOptions{})
	settings := Settings{HouseholdID: "h", Provider: providers.KeyWalmart, StoreID: "5435"}
	p, err := buildProposal(w, settings, testList(t), testPrefs(), MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Lines) != 2 || p.Lines[0].ID != "l1" || p.Lines[0].IngredientKey != onionKey || p.Lines[1].IngredientKey != beefKey {
		t.Fatalf("lines = %+v", p.Lines)
	}
	if onion := p.Lines[0]; onion.Packages != 1 || onion.Reason != "" || onion.Category != "produce" || onion.Status != LinePending {
		t.Errorf("onion = %+v", onion)
	}
	if beef := p.Lines[1]; beef.Packages != 2 || beef.ComputedPackages != 2 || beef.Amounts[0] != (Amount{Quantity: "20", Unit: "oz"}) {
		t.Errorf("beef = %+v", beef)
	}
	want := map[string]ExclusionReason{"name:shallot": ExcludedNoProduct, pasteKey: ExcludedInPantry, hintKey: ExcludedPantryHint, blendKey: ExcludedHouseMade}
	if got := reasons(p); len(got) != len(want) {
		t.Errorf("excluded = %v, want %v", got, want)
	} else {
		for k, r := range want {
			if got[k] != r {
				t.Errorf("excluded[%s] = %q, want %q", k, got[k], r)
			}
		}
	}
	if len(p.Links) != 1 || p.Links[0].URL != "https://www.walmart.com/sc/cart/addToCart?items=100000002,100000001_2&storeId=5435" ||
		strings.Join(p.Links[0].LineIDs, ",") != "l1,l2" || p.Links[0].ItemCount != 2 {
		t.Errorf("links = %+v", p.Links)
	}

	// A store saved for another provider isn't used.
	p, _ = buildProposal(w, Settings{Provider: "kroger", StoreID: "5435"}, testList(t), testPrefs(), MatchInput{})
	if p.StoreID != "" || strings.Contains(p.Links[0].URL, "storeId") {
		t.Errorf("store from another provider: %+v", p.Links)
	}
}

func TestBuildProposalSelection(t *testing.T) {
	w := providers.NewWalmart(providers.WalmartOptions{})
	in, err := validateMatchInput(MatchInput{
		Lines: []LineSelection{
			{IngredientKey: beefKey, Packages: 4},
			{IngredientKey: pasteKey},
			{IngredientKey: blendKey},
			{IngredientKey: onionKey},
			{IngredientKey: "66e5a1f2c3b4a5d6e7f801ff"},
		},
		CheckedOffKeys: []string{onionKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := buildProposal(w, Settings{}, testList(t), testPrefs(), in)
	if err != nil {
		t.Fatal(err)
	}
	// Selected lines are included whatever their status; checked-off and
	// house-made lines never are.
	if len(p.Lines) != 2 || p.Lines[0].IngredientKey != beefKey || p.Lines[0].Packages != 4 || p.Lines[0].ComputedPackages != 2 ||
		p.Lines[1].IngredientKey != pasteKey || p.Lines[1].Reason != providers.ReasonNoPackageSize {
		t.Fatalf("lines = %+v", p.Lines)
	}
	want := map[string]ExclusionReason{
		onionKey: ExcludedCheckedOff, "name:shallot": ExcludedNotSelected, hintKey: ExcludedNotSelected,
		blendKey: ExcludedHouseMade, "66e5a1f2c3b4a5d6e7f801ff": ExcludedNotOnList,
	}
	got := reasons(p)
	for k, r := range want {
		if got[k] != r {
			t.Errorf("excluded[%s] = %q, want %q", k, got[k], r)
		}
	}
	if p.Links[0].URL != "https://www.walmart.com/sc/cart/addToCart?items=100000001_4,100000003" {
		t.Errorf("link = %s", p.Links[0].URL)
	}

	// An empty selection selects nothing.
	p, _ = buildProposal(w, Settings{}, testList(t), testPrefs(), MatchInput{Lines: []LineSelection{}})
	if len(p.Lines) != 0 || len(p.Links) != 0 {
		t.Errorf("empty selection = %+v", p)
	}
}

func TestValidateMatchInput(t *testing.T) {
	for name, in := range map[string]MatchInput{
		"bad key":        {ExcludeKeys: []string{"onion"}},
		"unnormalized":   {CheckedOffKeys: []string{"name:Red Onion"}},
		"empty name":     {CheckedOffKeys: []string{"name:"}},
		"duplicate":      {Lines: []LineSelection{{IngredientKey: beefKey}, {IngredientKey: beefKey}}},
		"negative":       {Lines: []LineSelection{{IngredientKey: beefKey, Packages: -1}}},
		"too many packs": {Lines: []LineSelection{{IngredientKey: beefKey, Packages: 100}}},
		"too many keys":  {ExcludeKeys: make([]string, MaxSelectionKeys+1)},
	} {
		var v *ValidationError
		if _, err := validateMatchInput(in); !errors.As(err, &v) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	in, err := validateMatchInput(MatchInput{Lines: []LineSelection{{IngredientKey: " 66E5A1F2C3B4A5D6E7F80101 "}}, ExcludeKeys: []string{"name:red onion"}})
	if err != nil || in.Lines[0].IngredientKey != beefKey || in.ExcludeKeys[0] != "name:red onion" {
		t.Errorf("normalized = %+v, %v", in, err)
	}
}

func TestProviderPurchase(t *testing.T) {
	h := Handoff{ID: "66e5a1f2c3b4a5d6e7f80b01", Proposal: Proposal{Week: "2026-W38", Provider: providers.KeyWalmart}}
	measured := providerPurchase(h, HandoffLine{ID: "l1", LineSource: LineSource{IngredientKey: beefKey, Name: "Ground Beef"},
		ProductID: "100000001", PackageSize: &PackageSize{Quantity: "16", Unit: "oz"}}, 3)
	if measured.IngredientID != beefKey || measured.Name != "" || measured.Quantity != "3" || measured.Unit != "package" ||
		measured.UnitSize == nil || *measured.UnitSize != (pantry.UnitSize{Unit: "package", Quantity: "16", SizeUnit: "oz"}) ||
		measured.Week != "2026-W38" || measured.Provider.LineID != "l1" || measured.Provider.ProductID != "100000001" || measured.Provider.Key != "walmart" {
		t.Errorf("measured = %+v", measured)
	}
	counted := providerPurchase(h, HandoffLine{ID: "l2", LineSource: LineSource{IngredientKey: "name:large eggs", Name: "Large Eggs"},
		PackageSize: &PackageSize{Quantity: "12", Unit: "count"}}, 2)
	if counted.IngredientID != "" || counted.Name != "Large Eggs" || counted.Quantity != "24" || counted.Unit != "count" || counted.UnitSize != nil {
		t.Errorf("counted = %+v", counted)
	}
	bare := providerPurchase(h, HandoffLine{ID: "l3", LineSource: LineSource{IngredientKey: "name:tortillas", Name: "Flour Tortillas (8 inch)"}}, 1)
	if bare.Name != "tortillas" || bare.Quantity != "1" || bare.Unit != "package" || bare.UnitSize != nil {
		t.Errorf("bare = %+v", bare)
	}
}

func TestConfirmTargets(t *testing.T) {
	h := Handoff{Proposal: Proposal{Lines: []HandoffLine{
		{ID: "l1", Packages: 2, Status: LinePending},
		{ID: "l2", Packages: 1, Status: LineSkipped},
		{ID: "l3", Packages: 3, Status: LineConfirmed},
	}}}
	all, err := confirmTargets(h, ConfirmInput{All: true})
	if err != nil || len(all) != 2 || all[0] != (ConfirmLine{LineID: "l1", Packages: 2}) || all[1].LineID != "l3" {
		t.Errorf("all = %+v, %v", all, err)
	}
	some, err := confirmTargets(h, ConfirmInput{Lines: []ConfirmLine{{LineID: "l2", Packages: 5}, {LineID: "l1"}}})
	if err != nil || len(some) != 2 || some[0] != (ConfirmLine{LineID: "l1", Packages: 2}) || some[1] != (ConfirmLine{LineID: "l2", Packages: 5}) {
		t.Errorf("some = %+v, %v", some, err)
	}
	if none, err := confirmTargets(h, ConfirmInput{SkipRest: true}); err != nil || len(none) != 0 {
		t.Errorf("skip only = %+v, %v", none, err)
	}
	for name, in := range map[string]ConfirmInput{
		"nothing":      {},
		"all and some": {All: true, Lines: []ConfirmLine{{LineID: "l1"}}},
		"unknown":      {Lines: []ConfirmLine{{LineID: "l9"}}},
		"duplicate":    {Lines: []ConfirmLine{{LineID: "l1"}, {LineID: "l1"}}},
		"negative":     {Lines: []ConfirmLine{{LineID: "l1", Packages: -2}}},
		"too many":     {Lines: []ConfirmLine{{LineID: "l1", Packages: 100}}},
	} {
		var v *ValidationError
		if _, err := confirmTargets(h, in); !errors.As(err, &v) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}
