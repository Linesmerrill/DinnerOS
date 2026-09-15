package pantry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

const tacosRecipe = "66e5a1f2c3b4a5d6e7f80e03"

// fakeResolver resolves fixed keys, counting calls.
type fakeResolver struct {
	keys  map[string]ResolvedKey
	err   error
	calls int
}

func (f *fakeResolver) ResolveKeys(_ context.Context, keys []string) (map[string]ResolvedKey, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]ResolvedKey{}
	for _, k := range keys {
		if r, ok := f.keys[k]; ok {
			out[k] = r
		}
	}
	return out, nil
}

func southwestBatch(clientID string) HouseMadeInput {
	return HouseMadeInput{
		Key: "southwest spice blend", DisplayName: "Southwest Spice Blend (house-made)",
		Quantity: "12", Unit: "tbsp", UnitSize: &UnitSize{Unit: "count", Quantity: "1", SizeUnit: "tbsp"},
		ShelfLifeDays: 180, ClientPurchaseID: clientID,
	}
}

func TestRecordHouseMadeAndCookDeduction(t *testing.T) {
	f := newUsageFixture(t)
	f.svc.recipes.(fakeRecipes)[testHousehold+"/"+tacosRecipe] = recipes.Recipe{
		ID: tacosRecipe, Name: "Smoky Pork Tacos", Servings: []int{2},
		Ingredients: []recipes.RecipeIngredient{
			{Name: "Southwest Spice Blend", Amounts: amounts("count", map[int]string{2: "1"})},
			// An alias counted in packets: only the resolver matches and converts it.
			{Name: "Southwestern Spice Blend", Amounts: amounts("package", map[int]string{2: "1"})},
		},
	}

	res, err := f.svc.RecordHouseMade(f.ctx, f.actor, southwestBatch("batch-1"))
	if err != nil || !res.Created {
		t.Fatalf("RecordHouseMade() = %+v, %v", res, err)
	}
	item := res.Item
	if item.Key != "southwest spice blend" || item.DisplayName != "Southwest Spice Blend (house-made)" || item.Category != "spices" ||
		item.Status != StatusInStock || item.ExpiresOn != "2027-03-14" || item.UnitSize == nil || item.UnitSize.Unit != "count" {
		t.Fatalf("batch item = %+v", item)
	}
	if tr := item.Tracking; tr == nil || tr.CycleSource != CycleHouseMade || tr.Reference != "12" || tr.Unit != "tbsp" || tr.CycleID != res.Purchase.ID {
		t.Fatalf("tracking = %+v", item.Tracking)
	}
	if p := res.Purchase; p.Source != PurchaseHouseMade || p.Quantity != "12" || p.Unit != "tbsp" || p.UnitSize != nil || p.Week != "" {
		t.Errorf("purchase = %+v", p)
	}
	again, err := f.svc.RecordHouseMade(f.ctx, f.actor, southwestBatch("batch-1"))
	if err != nil || again.Created || again.Purchase.ID != res.Purchase.ID {
		t.Errorf("retry = %+v, %v", again, err)
	}

	cook := func(entry string) CookUsage {
		t.Helper()
		f.advance(time.Hour)
		u, _, err := f.svc.ApplyCooked(f.ctx, CookedMeal{HouseholdID: testHousehold, UserID: testUser, RecipeID: tacosRecipe, EntryID: entry, Servings: 2, OccurredAt: *f.clock})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}

	// Without a resolver only the exact name matches: 1 count = 1 tbsp.
	if u := cook("e1"); len(u.Lines) != 1 || u.Lines[0].Deducted != "1" {
		t.Fatalf("cook without resolver = %+v", u)
	}
	resolver := &fakeResolver{keys: map[string]ResolvedKey{
		"southwestern spice blend": {Key: "southwest spice blend", UnitSizes: []UnitSize{{Unit: "package", Quantity: "2", SizeUnit: "tbsp"}}},
		"southwest spice blend":    {Key: "southwest spice blend", UnitSizes: []UnitSize{{Unit: "package", Quantity: "2", SizeUnit: "tbsp"}}},
	}}
	if f.svc.SetKeyResolver(resolver) != f.svc {
		t.Fatal("SetKeyResolver must return the service")
	}
	u := cook("e2")
	if len(u.Lines) != 2 || u.Lines[0].Deducted != "1" || u.Lines[1].Deducted != "2" || u.Lines[1].ItemID != item.ID {
		t.Fatalf("cook with resolver = %+v", u)
	}
	if got := f.item(t, item.ID).Tracking; got.RecipeUsed != "4" || got.RecipeUses != 2 {
		t.Errorf("tracking after cooks = %+v", got)
	}

	// A failing resolver doesn't stop direct matches.
	resolver.err = errors.New("catalog unavailable")
	if u := cook("e3"); len(u.Lines) != 1 {
		t.Errorf("cook with failing resolver = %+v", u)
	}

	levels, err := f.svc.StockLevels(f.ctx, testHousehold, []string{"southwest spice blend", "salt", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(t, f.svc, testHousehold, AddInput{Name: "Salt"})
	levels2, _ := f.svc.StockLevels(f.ctx, testHousehold, []string{"salt"})
	if l, ok := levels["southwest spice blend"]; !ok || l.ItemID != item.ID || l.Remaining.RatString() != "7" || l.Unit != "tbsp" ||
		l.PercentRemaining != 58 || l.Status != StatusInStock || l.ExpiresOn != "2027-03-14" {
		t.Errorf("batch level = %+v", l)
	}
	if _, ok := levels["missing"]; ok || len(levels) != 1 {
		t.Errorf("levels = %+v", levels)
	}
	if l := levels2["salt"]; l.Remaining != nil || l.Status != StatusInStock {
		t.Errorf("untracked level = %+v", l)
	}
	if empty, err := f.svc.StockLevels(f.ctx, testHousehold, nil); err != nil || len(empty) != 0 {
		t.Errorf("no keys = %v, %v", empty, err)
	}
}

func TestRecordHouseMadeValidation(t *testing.T) {
	f := newUsageFixture(t)
	if _, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Tex-Mex Paste", Source: PurchaseHouseMade, Quantity: "1"}); !isValidation(err) {
		t.Errorf("house_made through RecordPurchase error = %v", err)
	}
	for name, mutate := range map[string]func(*HouseMadeInput){
		"unnormalized key": func(in *HouseMadeInput) { in.Key = "Southwest Spice Blend" },
		"no quantity":      func(in *HouseMadeInput) { in.Quantity, in.Unit = "", "" },
		"bad unit":         func(in *HouseMadeInput) { in.Unit = "jar" },
		"no name":          func(in *HouseMadeInput) { in.DisplayName = " " },
		"size per volume":  func(in *HouseMadeInput) { in.UnitSize.Unit = "tbsp" },
		"size in counts":   func(in *HouseMadeInput) { in.UnitSize.SizeUnit = "can" },
		"shelf life":       func(in *HouseMadeInput) { in.ShelfLifeDays = MaxShelfLifeDays + 1 },
		"bad category":     func(in *HouseMadeInput) { in.Category = "spice rack" },
		"client id spaces": func(in *HouseMadeInput) { in.ClientPurchaseID = " x" },
	} {
		in := southwestBatch("")
		mutate(&in)
		if _, err := f.svc.RecordHouseMade(f.ctx, f.actor, in); !isValidation(err) {
			t.Errorf("%s: error = %v, want a validation error", name, err)
		}
	}
	noEdit := households.Membership{HouseholdID: testHousehold, UserID: testUser}
	if _, err := f.svc.RecordHouseMade(f.ctx, noEdit, southwestBatch("")); !errors.Is(err, ErrForbidden) {
		t.Errorf("without pantry.edit error = %v", err)
	}
	plain, _, _ := newTestService(t)
	if _, err := plain.RecordHouseMade(f.ctx, f.actor, southwestBatch("")); !errors.Is(err, errUsageNotConfigured) {
		t.Errorf("without usage error = %v", err)
	}
}

func isValidation(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}
