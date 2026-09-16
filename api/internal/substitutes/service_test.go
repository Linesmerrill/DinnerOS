package substitutes

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// --- Fakes ------------------------------------------------------------------------

type fakeCatalog struct{ items []recipes.Ingredient }

func newFakeCatalog(names ...string) *fakeCatalog {
	c := &fakeCatalog{}
	for i, name := range names {
		category, confident := ingredients.Categorize(name)
		c.items = append(c.items, recipes.Ingredient{
			ID: fmt.Sprintf("cafe%020x", i+1), Key: ingredients.NormalizeName(name), Name: name, Category: category, CategoryConfident: confident,
		})
	}
	return c
}

func (c *fakeCatalog) id(name string) string {
	for _, ing := range c.items {
		if ing.Key == ingredients.NormalizeName(name) {
			return ing.ID
		}
	}
	return ""
}

func (c *fakeCatalog) IngredientsByID(_ context.Context, ids []string) ([]recipes.Ingredient, error) {
	var out []recipes.Ingredient
	for _, ing := range c.items {
		if slices.Contains(ids, ing.ID) {
			out = append(out, ing)
		}
	}
	return out, nil
}

func (c *fakeCatalog) IngredientsByKey(_ context.Context, keys []string) ([]recipes.Ingredient, error) {
	var out []recipes.Ingredient
	for _, ing := range c.items {
		if slices.Contains(keys, ing.Key) {
			out = append(out, ing)
		}
	}
	return out, nil
}

// fakeRecipeUsage holds each household's recipes as ingredient ID lists.
type fakeRecipeUsage map[string]map[string][]string // household → recipe → ingredient IDs

func (f fakeRecipeUsage) FindIngredientUse(_ context.Context, householdID string, ids []string) ([]recipes.IngredientUse, error) {
	var out []recipes.IngredientUse
	for recipeID, used := range f[householdID] {
		use := recipes.IngredientUse{RecipeID: recipeID}
		for _, id := range used {
			if slices.Contains(ids, id) {
				use.IngredientIDs = append(use.IngredientIDs, id)
			}
		}
		if len(use.IngredientIDs) > 0 {
			out = append(out, use)
		}
	}
	return out, nil
}

type fakePantry struct {
	mu       sync.Mutex
	levels   map[string]pantry.StockLevel
	recorded []pantry.HouseMadeInput
	err      error
}

func (f *fakePantry) StockLevels(_ context.Context, _ string, keys []string) (map[string]pantry.StockLevel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]pantry.StockLevel{}
	for _, k := range keys {
		if l, ok := f.levels[k]; ok {
			out[k] = l
		}
	}
	return out, f.err
}

func (f *fakePantry) RecordHouseMade(_ context.Context, actor households.Membership, in pantry.HouseMadeInput) (pantry.PurchaseResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return pantry.PurchaseResult{}, f.err
	}
	created := true
	for _, r := range f.recorded {
		if in.ClientPurchaseID != "" && r.ClientPurchaseID == in.ClientPurchaseID {
			created = false
		}
	}
	if created {
		f.recorded = append(f.recorded, in)
	}
	item := pantry.Item{ID: "66e5a1f2c3b4a5d6e7f80d01", HouseholdID: actor.HouseholdID, Key: in.Key, DisplayName: in.DisplayName, Status: pantry.StatusInStock}
	purchase := pantry.Purchase{ID: "66e5a1f2c3b4a5d6e7f80f01", HouseholdID: actor.HouseholdID, ItemID: item.ID, Source: pantry.PurchaseHouseMade,
		Quantity: in.Quantity, Unit: in.Unit, RecordedBy: actor.UserID, PurchasedAt: testNow}
	return pantry.PurchaseResult{Purchase: purchase, Item: item, Created: created}, nil
}

type fixture struct {
	svc     *Service
	store   *memoryStore
	catalog *fakeCatalog
	pantry  *fakePantry
	ctx     context.Context
	actor   households.Membership
}

func member(householdID string) households.Membership {
	return households.Membership{HouseholdID: householdID, UserID: testUser, Role: households.RoleMember}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	store := newMemoryStore()
	if err := EnsureSeed(ctx, store, nil); err != nil {
		t.Fatal(err)
	}
	catalog := newFakeCatalog("Tex-Mex Paste", "Southwest Spice Blend", "Sichuan Paste", "Chili Powder", "Tomato Paste", "Onion")
	usage := fakeRecipeUsage{
		testHousehold: {
			"r1": {catalog.id("Tex-Mex Paste"), catalog.id("Southwest Spice Blend"), catalog.id("Onion")},
			"r2": {catalog.id("Sichuan Paste")},
			"r3": {catalog.id("Tex-Mex Paste")},
			"r4": {catalog.id("Onion")},
		},
		otherHousehold: {"r5": {catalog.id("Southwest Spice Blend")}},
	}
	p := &fakePantry{levels: map[string]pantry.StockLevel{}}
	svc := NewService(ServiceOptions{Store: store, Catalog: catalog, Recipes: usage, Pantry: p})
	svc.now = func() time.Time { return testNow }
	// These tests cover explicit choices, so the household asks rather than
	// applying a default; the strategy tests set their own (settings_test.go).
	for _, hh := range []string{testHousehold, otherHousehold} {
		if _, err := store.PutSettings(ctx, Settings{HouseholdID: hh, Strategy: StrategyAsk, UpdatedBy: testUser, UpdatedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	return &fixture{svc: svc, store: store, catalog: catalog, pantry: p, ctx: ctx, actor: member(testHousehold)}
}

func isValidation(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}

func viewIDs(views []View) []string {
	var out []string
	for _, v := range views {
		out = append(out, v.Specialty.ID)
	}
	return out
}

// --- Tests --------------------------------------------------------------------------

func TestListAndGet(t *testing.T) {
	f := newFixture(t)
	views, err := f.svc.List(f.ctx, testHousehold, false)
	if err != nil {
		t.Fatal(err)
	}
	// Most used first; ties by name. "Sichuan Paste" is an alias of Szechuan Paste.
	if got := viewIDs(views); !slices.Equal(got, []string{"tex-mex-paste", "southwest-spice-blend", "szechuan-paste"}) {
		t.Fatalf("List() = %v", got)
	}
	tex := views[0]
	if tex.RecipeCount != 2 || !slices.Equal(tex.IngredientIDs, []string{f.catalog.id("Tex-Mex Paste")}) || tex.Choice != nil || tex.Batch != nil ||
		len(tex.Options) != 2 || tex.Options[0].Source != SourceCurated {
		t.Errorf("tex-mex view = %+v", tex)
	}
	if v := views[2]; v.RecipeCount != 1 || !slices.Equal(v.IngredientIDs, []string{f.catalog.id("Sichuan Paste")}) {
		t.Errorf("szechuan view = %+v", v)
	}
	seed, _ := LoadSeed()
	if all, err := f.svc.List(f.ctx, testHousehold, true); err != nil || len(all) != len(seed.Specialties) {
		t.Errorf("List(all) = %d, %v", len(all), err)
	}
	if other, _ := f.svc.List(f.ctx, otherHousehold, false); !slices.Equal(viewIDs(other), []string{"southwest-spice-blend"}) {
		t.Errorf("other household = %v", viewIDs(other))
	}

	f.pantry.levels["southwest spice blend"] = pantry.StockLevel{ItemID: "item", Status: pantry.StatusLow}
	v, err := f.svc.Get(f.ctx, testHousehold, "southwest-spice-blend")
	if err != nil || v.RecipeCount != 1 || v.Batch == nil || v.Batch.Status != pantry.StatusLow {
		t.Errorf("Get() = %+v, %v", v, err)
	}
	if _, err := f.svc.Get(f.ctx, testHousehold, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) error = %v", err)
	}
	if _, err := f.svc.List(f.ctx, "", false); err == nil {
		t.Error("List() without household: error = nil")
	}
}

func TestChoicesAndDefaults(t *testing.T) {
	f := newFixture(t)
	v, err := f.svc.SetChoice(f.ctx, f.actor, "tex-mex-paste", "tex-mex-paste.batch")
	if err != nil || v.Choice == nil || v.Choice.OptionID != "tex-mex-paste.batch" || v.ChoiceOption == nil || v.ChoiceOption.Type != TypeHouseMadeBatch ||
		v.Choice.ChosenBy != testUser || !v.Choice.ChosenAt.Equal(testNow) {
		t.Fatalf("SetChoice() = %+v, %v", v, err)
	}
	if v, err := f.svc.SetChoice(f.ctx, f.actor, "tex-mex-paste", " as_is "); err != nil || v.Choice.OptionID != OptionAsIs || v.ChoiceOption != nil {
		t.Errorf("SetChoice(as_is) = %+v, %v", v, err)
	}
	for _, bad := range []string{"", "southwest-spice-blend.batch", "66e5a1f2c3b4a5d6e7f8ffff"} {
		if _, err := f.svc.SetChoice(f.ctx, f.actor, "tex-mex-paste", bad); !isValidation(err) {
			t.Errorf("SetChoice(%q) error = %v", bad, err)
		}
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "missing", OptionAsIs); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetChoice(missing) error = %v", err)
	}
	noEdit := households.Membership{HouseholdID: testHousehold, UserID: testUser}
	if _, err := f.svc.SetChoice(f.ctx, noEdit, "tex-mex-paste", OptionAsIs); !errors.Is(err, ErrForbidden) {
		t.Errorf("SetChoice() without pantry.edit error = %v", err)
	}
	// A household option of another household isn't an option here.
	otherOption, err := f.svc.CreateOption(f.ctx, member(otherHousehold), "tex-mex-paste", OptionInput{
		Type: TypeStoreAlternative, Name: "Theirs", Per: &Measure{Quantity: "1", Unit: "tbsp"},
		Ingredients: []Component{{Name: "Tomato Paste", Quantity: "1", Unit: "tbsp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "tex-mex-paste", otherOption.ID); !isValidation(err) {
		t.Errorf("SetChoice(other household's option) error = %v", err)
	}

	// Defaults: tex-mex already has a choice; southwest and szechuan get theirs.
	res, err := f.svc.ApplyDefaults(f.ctx, f.actor)
	if err != nil || res.Skipped != 1 || !slices.Equal(viewIDs(res.Chosen), []string{"southwest-spice-blend", "szechuan-paste"}) {
		t.Fatalf("ApplyDefaults() = %+v, %v", res, err)
	}
	if c := res.Chosen[0].Choice; c == nil || c.OptionID != "southwest-spice-blend.batch" {
		t.Errorf("southwest default = %+v", c)
	}
	if again, err := f.svc.ApplyDefaults(f.ctx, f.actor); err != nil || len(again.Chosen) != 0 || again.Skipped != 3 {
		t.Errorf("second ApplyDefaults() = %+v, %v", again, err)
	}

	for range 2 {
		if err := f.svc.ClearChoice(f.ctx, f.actor, "tex-mex-paste"); err != nil {
			t.Errorf("ClearChoice() error = %v", err)
		}
	}
	if v, _ := f.svc.Get(f.ctx, testHousehold, "tex-mex-paste"); v.Choice != nil {
		t.Errorf("after clearing = %+v", v.Choice)
	}
	if err := f.svc.ClearChoice(f.ctx, noEdit, "tex-mex-paste"); !errors.Is(err, ErrForbidden) {
		t.Errorf("ClearChoice() without pantry.edit error = %v", err)
	}

	// A retired specialty can't be chosen, but still reads.
	if err := f.store.RetireSpecialties(f.ctx, []string{"cuban-spice-blend"}, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "cuban-spice-blend", OptionAsIs); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetChoice(retired) error = %v", err)
	}
	if v, err := f.svc.Get(f.ctx, testHousehold, "cuban-spice-blend"); err != nil || !v.Specialty.Retired {
		t.Errorf("Get(retired) = %+v, %v", v.Specialty, err)
	}
}

func mildBlend() OptionInput {
	return OptionInput{
		Type: TypeHouseMadeBatch, Name: "  Mild southwest blend ", Notes: "No cayenne.",
		Ingredients: []Component{
			{Name: "Chili Powder", Quantity: "1 1/2", Unit: "tbsp"},
			{Name: "Ground Cumin", Quantity: "1", Unit: "tbsp", Category: "Spices"},
			{Name: "Salt"},
		},
		Steps: []string{" Mix. "}, Yield: &Measure{Quantity: "3", Unit: "tbsp"}, ShelfLifeDays: 90,
		BasedOnOptionID: "southwest-spice-blend.batch",
	}
}

func TestHouseholdOptions(t *testing.T) {
	f := newFixture(t)
	o, err := f.svc.CreateOption(f.ctx, f.actor, "southwest-spice-blend", mildBlend())
	if err != nil {
		t.Fatalf("CreateOption() error = %v", err)
	}
	if o.ID == "" || o.Source != SourceHousehold || o.HouseholdID != testHousehold || o.SpecialtyID != "southwest-spice-blend" ||
		o.Name != "Mild southwest blend" || o.Ingredients[0].Quantity != "3/2" || o.Ingredients[1].Category != "spices" ||
		o.Ingredients[2].Quantity != "" || o.Steps[0] != "Mix." || o.CreatedBy != testUser || !o.CreatedAt.Equal(testNow) {
		t.Fatalf("created = %+v", o)
	}
	if v, _ := f.svc.Get(f.ctx, testHousehold, "southwest-spice-blend"); len(v.Options) != 3 || v.Options[2].ID != o.ID {
		t.Errorf("options after create = %+v", v.Options)
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "southwest-spice-blend", o.ID); err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*OptionInput){
		"no type":        func(in *OptionInput) { in.Type = "" },
		"no name":        func(in *OptionInput) { in.Name = " " },
		"no ingredients": func(in *OptionInput) { in.Ingredients = nil },
		"bad unit":       func(in *OptionInput) { in.Ingredients[0].Unit = "dollop" },
		"zero amount":    func(in *OptionInput) { in.Ingredients[0].Quantity = "0" },
		"unit alone":     func(in *OptionInput) { in.Ingredients[2].Unit = "tsp" },
		"bad category":   func(in *OptionInput) { in.Ingredients[1].Category = "rack" },
		"no yield":       func(in *OptionInput) { in.Yield = nil },
		"no shelf life":  func(in *OptionInput) { in.ShelfLifeDays = 0 },
		"per on a batch": func(in *OptionInput) { in.Per = &Measure{Quantity: "1", Unit: "tbsp"} },
		"store without per": func(in *OptionInput) {
			in.Type, in.Yield, in.ShelfLifeDays, in.Steps = TypeStoreAlternative, nil, 0, nil
		},
		"store to taste": func(in *OptionInput) {
			in.Type, in.Yield, in.ShelfLifeDays, in.Steps, in.Per = TypeStoreAlternative, nil, 0, nil, &Measure{Quantity: "1", Unit: "tbsp"}
		},
		"unknown base":   func(in *OptionInput) { in.BasedOnOptionID = "tex-mex-paste.batch" },
		"blank step":     func(in *OptionInput) { in.Steps = []string{"Mix.", " "} },
		"long name":      func(in *OptionInput) { in.Name = strings.Repeat("a", MaxNameLength+1) },
		"too many steps": func(in *OptionInput) { in.Steps = slices.Repeat([]string{"Stir."}, MaxSteps+1) },
	} {
		in := mildBlend()
		mutate(&in)
		if _, err := f.svc.CreateOption(f.ctx, f.actor, "southwest-spice-blend", in); !isValidation(err) {
			t.Errorf("%s: CreateOption() error = %v", name, err)
		}
	}

	// Update: switch to a store alternative.
	store := OptionInput{
		Type: TypeStoreAlternative, Name: "Rack mix", Per: &Measure{Quantity: "1", Unit: "tbsp"},
		Ingredients: []Component{{Name: "Chili Powder", Quantity: "2", Unit: "tsp"}},
	}
	updated, err := f.svc.UpdateOption(f.ctx, f.actor, "southwest-spice-blend", o.ID, store)
	if err != nil || updated.Type != TypeStoreAlternative || updated.Yield != nil || updated.Steps != nil || updated.BasedOnOptionID != "" ||
		!updated.CreatedAt.Equal(testNow) || updated.Notes != "" {
		t.Fatalf("UpdateOption() = %+v, %v", updated, err)
	}
	store.BasedOnOptionID = o.ID
	if _, err := f.svc.UpdateOption(f.ctx, f.actor, "southwest-spice-blend", o.ID, store); !isValidation(err) {
		t.Errorf("based on itself: error = %v", err)
	}
	store.BasedOnOptionID = ""
	if _, err := f.svc.UpdateOption(f.ctx, member(otherHousehold), "southwest-spice-blend", o.ID, store); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateOption(other household) error = %v", err)
	}
	if _, err := f.svc.UpdateOption(f.ctx, f.actor, "tex-mex-paste", o.ID, store); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateOption(wrong specialty) error = %v", err)
	}

	// The limit per specialty.
	for i := 1; i < MaxOptionsPerSpecialty; i++ {
		if _, err := f.svc.CreateOption(f.ctx, f.actor, "southwest-spice-blend", mildBlend()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.CreateOption(f.ctx, f.actor, "southwest-spice-blend", mildBlend()); !isValidation(err) {
		t.Errorf("CreateOption() over the limit error = %v", err)
	}

	// Deleting removes the choice of it.
	if err := f.svc.DeleteOption(f.ctx, f.actor, "southwest-spice-blend", o.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.svc.Get(f.ctx, testHousehold, "southwest-spice-blend"); v.Choice != nil {
		t.Errorf("choice after deleting its option = %+v", v.Choice)
	}
	if err := f.svc.DeleteOption(f.ctx, f.actor, "southwest-spice-blend", o.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteOption() error = %v", err)
	}
	if _, err := f.svc.CreateOption(f.ctx, households.Membership{HouseholdID: testHousehold, UserID: testUser}, "southwest-spice-blend", mildBlend()); !errors.Is(err, ErrForbidden) {
		t.Errorf("CreateOption() without pantry.edit error = %v", err)
	}
}

func TestRecordBatch(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.RecordBatch(f.ctx, f.actor, "southwest-spice-blend", BatchInput{}); !isValidation(err) {
		t.Errorf("no choice: error = %v", err)
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "southwest-spice-blend", "southwest-spice-blend.batch"); err != nil {
		t.Fatal(err)
	}
	res, err := f.svc.RecordBatch(f.ctx, f.actor, "southwest-spice-blend", BatchInput{Batches: 2, ClientPurchaseID: "b1"})
	if err != nil || !res.Created || res.Option.ID != "southwest-spice-blend.batch" {
		t.Fatalf("RecordBatch() = %+v, %v", res, err)
	}
	in := f.pantry.recorded[0]
	if in.Key != "southwest spice blend" || in.DisplayName != "Southwest Spice Blend (house-made)" || in.Category != "spices" ||
		in.Quantity != "8" || in.Unit != "tbsp" || in.ShelfLifeDays != 180 || in.ClientPurchaseID != "b1" ||
		in.UnitSize == nil || *in.UnitSize != (pantry.UnitSize{Unit: "count", Quantity: "1", SizeUnit: "tbsp"}) {
		t.Errorf("pantry input = %+v", in)
	}
	if again, err := f.svc.RecordBatch(f.ctx, f.actor, "southwest-spice-blend", BatchInput{Batches: 2, ClientPurchaseID: "b1"}); err != nil || again.Created {
		t.Errorf("retry = %+v, %v", again, err)
	}

	custom, err := f.svc.CreateOption(f.ctx, f.actor, "southwest-spice-blend", mildBlend())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RecordBatch(f.ctx, f.actor, "southwest-spice-blend", BatchInput{OptionID: custom.ID}); err != nil || f.pantry.recorded[1].Quantity != "3" {
		t.Errorf("custom batch = %+v, %v", f.pantry.recorded, err)
	}
	for name, in := range map[string]BatchInput{
		"store option": {OptionID: "southwest-spice-blend.store"},
		"other option": {OptionID: "tex-mex-paste.batch"},
		"as is":        {OptionID: OptionAsIs},
		"too many":     {Batches: MaxBatches + 1},
		"negative":     {Batches: -1},
		"unknown":      {OptionID: "66e5a1f2c3b4a5d6e7f8ffff"},
	} {
		if _, err := f.svc.RecordBatch(f.ctx, f.actor, "southwest-spice-blend", in); !isValidation(err) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if _, err := f.svc.RecordBatch(f.ctx, households.Membership{HouseholdID: testHousehold, UserID: testUser}, "southwest-spice-blend", BatchInput{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("without pantry.edit error = %v", err)
	}
	f.pantry.err = &pantry.ValidationError{Message: "bad"}
	if _, err := f.svc.RecordBatch(f.ctx, f.actor, "southwest-spice-blend", BatchInput{}); err == nil {
		t.Error("a pantry failure must fail the batch")
	}
}

func quantityOf(q *ingredients.Quantity) string {
	if q == nil {
		return ""
	}
	return q.String()
}

func TestGrocerySpecialties(t *testing.T) {
	f := newFixture(t)
	texID, southwestID, onionID := f.catalog.id("Tex-Mex Paste"), f.catalog.id("Southwest Spice Blend"), f.catalog.id("Onion")
	lines := []grocery.Line{
		{IngredientKey: texID, Name: "Tex-Mex Paste"},
		{IngredientKey: "name:sichuan paste", Name: "Sichuan Paste"},
		{IngredientKey: southwestID, Name: "Southwest Spice Blend"},
		{IngredientKey: onionID, Name: "Onion"},
		{IngredientKey: "name:fajita seasoning", Name: "Fajita Seasoning"},
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "tex-mex-paste", "tex-mex-paste.store"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "southwest-spice-blend", "southwest-spice-blend.batch"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "fajita-spice-blend", OptionAsIs); err != nil {
		t.Fatal(err)
	}
	nine := ratOf(t, "9")
	f.pantry.levels["southwest spice blend"] = pantry.StockLevel{
		ItemID: "batch-item", Status: pantry.StatusInStock, Remaining: nine, Unit: "tbsp",
		UnitSize: &pantry.UnitSize{Unit: "count", Quantity: "1", SizeUnit: "tbsp"},
	}

	specs, err := f.svc.GrocerySpecialties(f.ctx, testHousehold, lines)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 4 || specs[onionID] != nil {
		t.Fatalf("specialties = %v", specs)
	}
	tex := specs[texID]
	if tex.ID != "tex-mex-paste" || tex.Choice == nil || tex.Choice.Type != grocery.ChoiceStoreAlternative || tex.Choice.Per.Unit != "tbsp" ||
		len(tex.UnitSizes) != 2 || len(tex.Choice.Components) != 2 {
		t.Fatalf("tex-mex = %+v choice %+v", tex, tex.Choice)
	}
	base, paste := tex.Choice.Components[0], tex.Choice.Components[1]
	if base.Name != "Smoky Chipotle Bouillon Base" || base.Category != "condiments" || quantityOf(base.Quantity) != "2" || base.Unit != "tsp" {
		t.Errorf("chipotle base = %+v", base)
	}
	if paste.IngredientKey != f.catalog.id("Tomato Paste") || paste.Name != "Tomato Paste" || quantityOf(paste.Quantity) != "1" || paste.Unit != "tsp" {
		t.Errorf("tomato paste = %+v", paste)
	}
	sw := specs[southwestID]
	if c := sw.Choice; c == nil || c.Type != grocery.ChoiceHouseMadeBatch || c.PantryKey != "name:southwest spice blend" || c.Yield.Quantity.String() != "4" ||
		c.Stock == nil || c.Stock.ItemID != "batch-item" || c.Stock.Status != "in_stock" || quantityOf(c.Stock.Remaining) != "9" || c.Stock.UnitSize == nil {
		t.Errorf("southwest choice = %+v", sw.Choice)
	}
	sz := specs["name:sichuan paste"]
	if sz.ID != "szechuan-paste" || sz.Choice != nil || len(sz.Suggestions) != 2 || !sz.Suggestions[0].IsDefault || sz.Suggestions[0].ID != "szechuan-paste.store" {
		t.Errorf("szechuan = %+v", sz)
	}
	if fj := specs["name:fajita seasoning"]; fj.ID != "fajita-spice-blend" || fj.Choice == nil || fj.Choice.Type != grocery.ChoiceAsIs {
		t.Errorf("fajita = %+v", fj)
	}

	// A choice of a deleted household option is no choice.
	custom, _ := f.svc.CreateOption(f.ctx, f.actor, "tex-mex-paste", mildTexMex())
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "tex-mex-paste", custom.ID); err != nil {
		t.Fatal(err)
	}
	if specs, _ := f.svc.GrocerySpecialties(f.ctx, testHousehold, lines[:1]); specs[texID].Choice == nil || specs[texID].Choice.OptionID != custom.ID {
		t.Errorf("custom choice = %+v", specs[texID])
	}
	if err := f.store.DeleteOption(f.ctx, testHousehold, custom.ID); err != nil {
		t.Fatal(err)
	}
	if specs, _ := f.svc.GrocerySpecialties(f.ctx, testHousehold, lines[:1]); specs[texID].Choice != nil {
		t.Errorf("deleted option choice = %+v", specs[texID].Choice)
	}

	if none, err := f.svc.GrocerySpecialties(f.ctx, testHousehold, lines[3:4]); err != nil || none != nil {
		t.Errorf("no specialty lines = %v, %v", none, err)
	}
	if none, err := f.svc.GrocerySpecialties(f.ctx, testHousehold, nil); err != nil || none != nil {
		t.Errorf("no lines = %v, %v", none, err)
	}
	f.pantry.err = errors.New("pantry down")
	if _, err := f.svc.GrocerySpecialties(f.ctx, testHousehold, lines); err == nil {
		t.Error("a pantry failure must fail")
	}
}

func ratOf(t *testing.T, s string) *big.Rat {
	t.Helper()
	q, err := ingredients.ParseQuantity(s)
	if err != nil {
		t.Fatal(err)
	}
	return q.Rat()
}

func mildTexMex() OptionInput {
	return OptionInput{
		Type: TypeStoreAlternative, Name: "Just tomato paste", Per: &Measure{Quantity: "1", Unit: "tbsp"},
		Ingredients: []Component{{Name: "Tomato Paste", Quantity: "1", Unit: "tbsp"}},
	}
}

func TestResolveKeys(t *testing.T) {
	f := newFixture(t)
	got, err := f.svc.ResolveKeys(f.ctx, []string{"sichuan paste", "szechuan paste", "onion"})
	if err != nil || len(got) != 2 {
		t.Fatalf("ResolveKeys() = %+v, %v", got, err)
	}
	r := got["sichuan paste"]
	if r.Key != "szechuan paste" || len(r.UnitSizes) != 2 || r.UnitSizes[0] != (pantry.UnitSize{Unit: "count", Quantity: "1", SizeUnit: "tbsp"}) {
		t.Errorf("resolved = %+v", r)
	}
	if none, err := f.svc.ResolveKeys(f.ctx, nil); err != nil || none != nil {
		t.Errorf("no keys = %v, %v", none, err)
	}
}
