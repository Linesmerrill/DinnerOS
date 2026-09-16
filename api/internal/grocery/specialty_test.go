package grocery

import (
	"math/big"
	"reflect"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

func pq(s string) *ingredients.Quantity {
	v, err := ingredients.ParseQuantity(s)
	if err != nil {
		panic(err)
	}
	return &v
}

func TestConvertMeasure(t *testing.T) {
	sizes := []UnitSize{{Unit: "count", Quantity: *pq("2"), SizeUnit: "tsp"}, {Unit: "package", Quantity: *pq("1"), SizeUnit: "oz"}}
	for _, tt := range []struct {
		q, from, to string
		want        string // "" means no conversion
	}{
		{"3", "tbsp", "tbsp", "3"},
		{"3", "tsp", "tbsp", "1"},
		{"1/2", "cup", "tbsp", "8"},
		{"3", "count", "tbsp", "2"},   // 3 × 2 tsp = 6 tsp
		{"1", "tbsp", "count", "3/2"}, // 3 tsp ÷ 2 tsp
		{"1", "count", "count", "1"},
		{"2", "package", "g", "56.69904625"},
		{"1", "package", "count", ""}, // oz can't become tsp
		{"1", "can", "tbsp", ""},      // no size for cans
		{"1", "tbsp", "oz", ""},       // volume never becomes weight
		{"1", "tbsp", "jar", ""},      // unknown unit
		{"1", "dollop", "tbsp", ""},
	} {
		got, ok := ConvertMeasure(pq(tt.q).Rat(), tt.from, tt.to, sizes)
		switch {
		case tt.want == "" && ok:
			t.Errorf("ConvertMeasure(%s %s → %s) = %s, want no conversion", tt.q, tt.from, tt.to, got.RatString())
		case tt.want != "" && (!ok || got.Cmp(pq(tt.want).Rat()) != 0):
			t.Errorf("ConvertMeasure(%s %s → %s) = %v, %v; want %s", tt.q, tt.from, tt.to, got, ok, tt.want)
		}
	}

	// A packet sized in both weight and volume converts from either, but
	// weight still never becomes volume without the packet in between.
	sizes = []UnitSize{{Unit: "count", Quantity: *pq("1"), SizeUnit: "oz"}, {Unit: "count", Quantity: *pq("2"), SizeUnit: "tbsp"}}
	for _, tt := range []struct {
		q, from, to string
		want        string
	}{
		{"1", "oz", "count", "1"},
		{"2", "tbsp", "count", "1"},
		{"56.69904625", "g", "count", "2"},
		{"3", "count", "tbsp", "6"},
		{"3", "count", "oz", "3"},
		{"1", "oz", "tbsp", ""},
		{"1", "oz", "package", ""},
	} {
		got, ok := ConvertMeasure(pq(tt.q).Rat(), tt.from, tt.to, sizes)
		switch {
		case tt.want == "" && ok:
			t.Errorf("ConvertMeasure(%s %s → %s) = %s, want no conversion", tt.q, tt.from, tt.to, got.RatString())
		case tt.want != "" && (!ok || got.Cmp(pq(tt.want).Rat()) != 0):
			t.Errorf("ConvertMeasure(%s %s → %s) = %v, %v; want %s", tt.q, tt.from, tt.to, got, ok, tt.want)
		}
	}
}

// --- Fixtures -------------------------------------------------------------------

const (
	keyTexMex    = "ing-texmex"
	keySouthwest = "ing-southwest"
	keyPaste     = "ing-tomato-paste"
	keyChili     = "ing-chili-powder"
	keyCumin     = "ing-cumin"
)

func sline(key, name, quantity, unit string) Line {
	l := Line{IngredientKey: key, Name: name, Category: "condiments"}
	if quantity != "" {
		l.Quantity, l.UnitCode = pq(quantity), unit
	}
	return l
}

func texMexSpecialty(choice *Choice) *Specialty {
	return &Specialty{
		ID: "tex-mex-paste", Key: "tex mex paste", Name: "Tex-Mex Paste",
		Suggestions: []OptionRef{{ID: "tex-mex-paste.store", Type: ChoiceStoreAlternative, Name: "Tomato paste and chili spices", IsDefault: true}},
		UnitSizes:   []UnitSize{{Unit: "count", Quantity: *pq("2"), SizeUnit: "tbsp"}},
		Choice:      choice,
	}
}

func texMexStore() *Choice {
	return &Choice{
		Type: ChoiceStoreAlternative, OptionID: "tex-mex-paste.store", OptionName: "Tex-Mex paste (store mix)",
		Per: Measure{Quantity: *pq("1"), Unit: "tbsp"},
		Components: []Component{
			{IngredientKey: keyPaste, Name: "Tomato Paste", Category: "pantry", Quantity: pq("2"), Unit: "tsp"},
			{IngredientKey: keyChili, Name: "Chili Powder", Category: "spices", Quantity: pq("1/2"), Unit: "tsp"},
		},
	}
}

func southwestSpecialty(stock *BatchStock) *Specialty {
	return &Specialty{
		ID: "southwest-spice-blend", Key: "southwest spice blend", Name: "Southwest Spice Blend",
		Suggestions: []OptionRef{{ID: "southwest-spice-blend.batch", Type: ChoiceHouseMadeBatch, Name: "Southwest spice blend (house blend)", IsDefault: true}},
		UnitSizes:   []UnitSize{{Unit: "count", Quantity: *pq("1"), SizeUnit: "tbsp"}},
		Choice: &Choice{
			Type: ChoiceHouseMadeBatch, OptionID: "southwest-spice-blend.batch", OptionName: "Southwest spice blend (house blend)",
			Yield:     Measure{Quantity: *pq("12"), Unit: "tbsp"},
			PantryKey: "name:southwest spice blend",
			Stock:     stock,
			Components: []Component{
				{IngredientKey: keyChili, Name: "Chili Powder", Category: "spices", Quantity: pq("3"), Unit: "tbsp"},
				{IngredientKey: keyCumin, Name: "Ground Cumin", Category: "spices", Quantity: pq("2"), Unit: "tbsp"},
				{IngredientKey: "name:salt", Name: "Salt", Category: "spices"},
			},
		},
	}
}

func itemsByName(t *testing.T, selections []RecipeSelection, pantry Pantry) map[string]Item {
	t.Helper()
	list, err := Aggregate(selections, pantry)
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}
	out := map[string]Item{}
	for _, it := range list.Items {
		out[it.Name] = it
	}
	return out
}

func amountStrings(it Item) []string {
	var out []string
	for _, a := range it.Amounts {
		out = append(out, a.Quantity.String()+" "+a.Unit.Code)
	}
	return out
}

func recipeNames(src []Source) []string {
	var out []string
	for _, s := range src {
		out = append(out, s.RecipeName)
	}
	return out
}

// --- Store alternatives ------------------------------------------------------------

func TestApplySpecialtiesStoreAlternative(t *testing.T) {
	selections := []RecipeSelection{
		// Scaled 2 → 4 servings: 2 tbsp becomes 4 tbsp of paste, so 8 tsp tomato paste.
		{RecipeID: "a", RecipeName: "Smoky Pork Tacos", RecipeServings: 2, TargetServings: 4, Lines: []Line{
			sline(keyTexMex, "Tex-Mex Paste", "2", "tbsp"),
			sline("ing-onion", "Onion", "1", "count"),
		}},
		// One packet is 2 tbsp: 4 tsp tomato paste.
		{RecipeID: "b", RecipeName: "Chili Bowls", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			sline(keyTexMex, "Tex-Mex Paste", "1", "count"),
		}},
		// An ordinary line for the same component: 3 tsp.
		{RecipeID: "c", RecipeName: "Pasta", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			sline(keyPaste, "Tomato Paste", "1", "tbsp"),
		}},
	}
	original := slices.Clone(selections[0].Lines)
	specs := Specialties{keyTexMex: texMexSpecialty(texMexStore())}

	out, plans, err := ApplySpecialties(selections, specs)
	if err != nil || len(plans) != 0 {
		t.Fatalf("ApplySpecialties() = %d plans, %v", len(plans), err)
	}
	if !reflect.DeepEqual(selections[0].Lines, original) {
		t.Error("ApplySpecialties changed its input")
	}
	items := itemsByName(t, out, nil)
	if _, ok := items["Tex-Mex Paste"]; ok {
		t.Error("the specialty line is still on the list")
	}
	paste := items["Tomato Paste"]
	// 8 + 4 + 3 = 15 tsp = 5 tbsp.
	if got := amountStrings(paste); !slices.Equal(got, []string{"5 tbsp"}) {
		t.Errorf("tomato paste = %v, want 5 tbsp", got)
	}
	if got := recipeNames(paste.Sources); !slices.Equal(got, []string{"Chili Bowls", "Pasta", "Smoky Pork Tacos"}) {
		t.Errorf("tomato paste recipes = %v", got)
	}
	if len(paste.Via) != 1 || paste.Via[0].Kind != ViaStoreAlternative || paste.Via[0].SpecialtyName != "Tex-Mex Paste" ||
		paste.Via[0].OptionID != "tex-mex-paste.store" || !slices.Equal(recipeNames(paste.Via[0].Recipes), []string{"Chili Bowls", "Smoky Pork Tacos"}) {
		t.Errorf("tomato paste via = %+v", paste.Via)
	}
	// 4 tbsp and 2 tbsp of paste at 1/2 tsp per tbsp: 3 tsp of chili powder.
	chili := items["Chili Powder"]
	if got := amountStrings(chili); !slices.Equal(got, []string{"3 tsp"}) || chili.Specialty != nil || chili.Category != "spices" {
		t.Errorf("chili powder = %v %+v", got, chili)
	}
	if onion := items["Onion"]; onion.Via != nil || onion.Specialty != nil {
		t.Errorf("an ordinary item has provenance: %+v", onion)
	}
}

func TestApplySpecialtiesStoreAlternativeWithoutAmounts(t *testing.T) {
	selections := []RecipeSelection{{RecipeID: "a", RecipeName: "Tacos", RecipeServings: 2, TargetServings: 2, Lines: []Line{
		sline(keyTexMex, "Tex-Mex Paste", "", ""),
	}}, {RecipeID: "b", RecipeName: "Bowls", RecipeServings: 2, TargetServings: 2, Lines: []Line{
		sline(keyTexMex, "Tex-Mex Paste", "1", "oz"), // weight doesn't convert to tbsp
	}}}
	out, _, err := ApplySpecialties(selections, Specialties{keyTexMex: texMexSpecialty(texMexStore())})
	if err != nil {
		t.Fatal(err)
	}
	items := itemsByName(t, out, nil)
	for _, name := range []string{"Tomato Paste", "Chili Powder"} {
		if it := items[name]; !it.Unquantified || len(it.Amounts) != 0 || len(it.Via) != 1 || len(it.Via[0].Recipes) != 2 {
			t.Errorf("%s = %+v, want listed without an amount for both recipes", name, it)
		}
	}
}

func TestApplySpecialtiesWithoutChoice(t *testing.T) {
	sel := []RecipeSelection{{RecipeID: "a", RecipeName: "Tacos", RecipeServings: 2, TargetServings: 2, Lines: []Line{
		sline(keyTexMex, "Tex-Mex Paste", "1", "count"),
		sline(keySouthwest, "Southwest Spice Blend", "1", "tbsp"),
	}}}
	asIs := southwestSpecialty(nil)
	asIs.Choice = &Choice{Type: ChoiceAsIs, OptionID: "as_is"}
	out, plans, err := ApplySpecialties(sel, Specialties{keyTexMex: texMexSpecialty(nil), keySouthwest: asIs})
	if err != nil || len(plans) != 0 {
		t.Fatal(err)
	}
	items := itemsByName(t, out, nil)
	if s := items["Tex-Mex Paste"].Specialty; s == nil || s.ID != "tex-mex-paste" || s.Key != "tex mex paste" || s.ChoiceType != "" || s.HouseMade ||
		len(s.Suggestions) != 1 || !s.Suggestions[0].IsDefault {
		t.Errorf("unchosen specialty = %+v", s)
	}
	if it := items["Tex-Mex Paste"]; !slices.Equal(amountStrings(it), []string{"1 count"}) || it.Status != StatusToBuy {
		t.Errorf("unchosen item = %+v", it)
	}
	if s := items["Southwest Spice Blend"].Specialty; s == nil || s.ChoiceType != ChoiceAsIs || s.OptionID != "as_is" || s.Suggestions != nil {
		t.Errorf("as-is specialty = %+v", s)
	}
}

// --- House-made batches --------------------------------------------------------------

func batchWeek() []RecipeSelection {
	return []RecipeSelection{
		{RecipeID: "a", RecipeName: "Smoky Pork Tacos", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			sline(keySouthwest, "Southwest Spice Blend", "2", "count"), // 2 tbsp
		}},
		{RecipeID: "b", RecipeName: "Chili Bowls", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			sline(keySouthwest, "Southwest Spice Blend", "1", "tbsp"),
			sline(keyChili, "Chili Powder", "1", "tsp"),
		}},
	}
}

func stock(status, remaining, unit string) *BatchStock {
	s := &BatchStock{ItemID: "item-1", Status: status, RemainingUnit: unit}
	if remaining != "" {
		s.Remaining = pq(remaining)
	}
	return s
}

func TestApplySpecialtiesBatchInPantry(t *testing.T) {
	for name, tt := range map[string]struct {
		stock  *BatchStock
		reason BatchReason
	}{
		"enough":         {stock("in_stock", "9", "tbsp"), BatchEnough},
		"exactly":        {stock("in_stock", "1/5", "cup"), BatchEnough}, // 3.2 tbsp ≥ 3
		"untracked":      {stock("in_stock", "", ""), BatchInStock},
		"not comparable": {stock("in_stock", "2", "oz"), BatchInStock},
	} {
		t.Run(name, func(t *testing.T) {
			out, plans, err := ApplySpecialties(batchWeek(), Specialties{keySouthwest: southwestSpecialty(tt.stock)})
			if err != nil || len(plans) != 1 {
				t.Fatalf("ApplySpecialties() = %+v, %v", plans, err)
			}
			p := plans[0]
			if p.Status != BatchInPantry || p.Reason != tt.reason || p.Batches != 0 || p.ItemID != "item-1" ||
				p.Needed == nil || p.Needed.Quantity.String() != "3" || p.Needed.Unit != "tbsp" ||
				!slices.Equal(recipeNames(p.Recipes), []string{"Chili Bowls", "Smoky Pork Tacos"}) {
				t.Fatalf("plan = %+v", p)
			}
			pantry := PantryStock{InStock: map[string]bool{"name:southwest spice blend": true}}
			items := itemsByName(t, out, pantry)
			blend := items["Southwest Spice Blend"]
			if blend.IngredientKey != "name:southwest spice blend" || blend.Status != StatusInPantry || blend.Specialty == nil || !blend.Specialty.HouseMade ||
				blend.Specialty.OptionID != "southwest-spice-blend.batch" || len(blend.Via) != 0 {
				t.Errorf("batch item = %+v", blend)
			}
			if c := items["Chili Powder"]; !slices.Equal(amountStrings(c), []string{"1 tsp"}) || len(c.Via) != 0 {
				t.Errorf("chili powder = %+v, want only the recipe's own line", c)
			}
		})
	}
}

func TestApplySpecialtiesBatchToMake(t *testing.T) {
	for name, tt := range map[string]struct {
		stock   *BatchStock
		week    []RecipeSelection
		reason  BatchReason
		batches int
	}{
		"missing":    {nil, batchWeek(), BatchMissing, 1},
		"out":        {stock("out", "", ""), batchWeek(), BatchOut, 1},
		"low":        {stock("low", "8", "tbsp"), batchWeek(), BatchLow, 1},
		"not enough": {stock("in_stock", "2", "tbsp"), batchWeek(), BatchNotEnough, 1},
		// 30 tbsp needed with 5 left: 25 more is 3 batches of 12.
		"several batches": {stock("in_stock", "5", "tbsp"), []RecipeSelection{
			{RecipeID: "a", RecipeName: "Tacos", RecipeServings: 2, TargetServings: 4, Lines: []Line{sline(keySouthwest, "Southwest Spice Blend", "15", "tbsp")}},
		}, BatchNotEnough, 3},
		// A recipe without an amount: at least one batch.
		"unknown need": {nil, []RecipeSelection{
			{RecipeID: "a", RecipeName: "Tacos", RecipeServings: 2, TargetServings: 2, Lines: []Line{sline(keySouthwest, "Southwest Spice Blend", "", "")}},
		}, BatchMissing, 1},
	} {
		t.Run(name, func(t *testing.T) {
			out, plans, err := ApplySpecialties(tt.week, Specialties{keySouthwest: southwestSpecialty(tt.stock)})
			if err != nil || len(plans) != 1 {
				t.Fatalf("ApplySpecialties() = %+v, %v", plans, err)
			}
			p := plans[0]
			if p.Status != BatchMake || p.Reason != tt.reason || p.Batches != tt.batches || p.Yield.Quantity.String() != "12" {
				t.Fatalf("plan = %+v", p)
			}
			items := itemsByName(t, out, PantryStock{InStock: map[string]bool{"name:southwest spice blend": true}})
			if _, ok := items["Southwest Spice Blend"]; ok {
				t.Error("the specialty line is still on the list")
			}
			cumin := items["Ground Cumin"]
			wantCumin := new(big.Rat).Mul(big.NewRat(2, 1), big.NewRat(int64(tt.batches), 1)).RatString() + " tbsp"
			if got := amountStrings(cumin); !slices.Equal(got, []string{wantCumin}) {
				t.Errorf("cumin = %v, want %s", got, wantCumin)
			}
			if len(cumin.Via) != 1 || cumin.Via[0].Kind != ViaHouseMadeBatch || cumin.Via[0].SpecialtyID != "southwest-spice-blend" || p.SpecialtyID != "southwest-spice-blend" || cumin.Via[0].Batches != tt.batches ||
				cumin.Via[0].Yield == nil || cumin.Via[0].Yield.Unit != "tbsp" || !slices.Equal(recipeNames(cumin.Sources), recipeNames(p.Recipes)) {
				t.Errorf("cumin = %+v", cumin)
			}
			if salt := items["Salt"]; !salt.Unquantified {
				t.Errorf("salt to taste = %+v", salt)
			}
		})
	}
}

func TestApplySpecialtiesIsOrderIndependent(t *testing.T) {
	specs := Specialties{keySouthwest: southwestSpecialty(stock("in_stock", "2", "tbsp")), keyTexMex: texMexSpecialty(texMexStore())}
	week := append(batchWeek(), RecipeSelection{RecipeID: "c", RecipeName: "Nachos", RecipeServings: 2, TargetServings: 2, Lines: []Line{
		sline(keyTexMex, "Tex-Mex Paste", "1", "count"), sline(keySouthwest, "Southwest Spice Blend", "1", "tsp"),
	}})
	aggregate := func(sel []RecipeSelection) (List, []BatchPlan) {
		out, plans, err := ApplySpecialties(sel, specs)
		if err != nil {
			t.Fatal(err)
		}
		list, err := Aggregate(out, nil)
		if err != nil {
			t.Fatal(err)
		}
		return list, plans
	}
	wantList, wantPlans := aggregate(week)
	reversed := slices.Clone(week)
	slices.Reverse(reversed)
	for i := range reversed {
		reversed[i].Lines = slices.Clone(reversed[i].Lines)
		slices.Reverse(reversed[i].Lines)
	}
	gotList, gotPlans := aggregate(reversed)
	if !reflect.DeepEqual(gotList, wantList) || !reflect.DeepEqual(gotPlans, wantPlans) {
		t.Errorf("reversed input changed the result:\n%+v\n%+v\nwant\n%+v\n%+v", gotList, gotPlans, wantList, wantPlans)
	}
}

func TestApplySpecialtiesRejectsBadInput(t *testing.T) {
	if _, _, err := ApplySpecialties([]RecipeSelection{{RecipeID: "a", RecipeServings: 0, TargetServings: 2}}, nil); err == nil {
		t.Error("zero servings: error = nil")
	}
	bad := texMexSpecialty(&Choice{Type: "magic"})
	sel := []RecipeSelection{{RecipeID: "a", RecipeServings: 2, TargetServings: 2, Lines: []Line{sline(keyTexMex, "Tex-Mex Paste", "1", "tbsp")}}}
	if _, _, err := ApplySpecialties(sel, Specialties{keyTexMex: bad}); err == nil {
		t.Error("unknown choice type: error = nil")
	}
	// Without specialties the selections pass through unchanged.
	out, plans, err := ApplySpecialties(sel, nil)
	if err != nil || len(plans) != 0 || !reflect.DeepEqual(out, sel) {
		t.Errorf("no specialties = %+v, %+v, %v", out, plans, err)
	}
}
