package pantry

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

const (
	noodlesRecipe = "66e5a1f2c3b4a5d6e7f80e01"
	otherRecipe   = "66e5a1f2c3b4a5d6e7f80e02"
)

// fakeNotifier records notifications, deduplicated by key like
// notifications.Service.
type fakeNotifier struct {
	mu      sync.Mutex
	created []notifications.New
	err     error
}

func (f *fakeNotifier) Create(_ context.Context, in notifications.New) (notifications.Notification, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return notifications.Notification{}, false, f.err
	}
	for _, n := range f.created {
		if n.HouseholdID == in.HouseholdID && n.DedupeKey == in.DedupeKey {
			return notifications.Notification{DedupeKey: n.DedupeKey}, false, nil
		}
	}
	f.created = append(f.created, in)
	return notifications.Notification{DedupeKey: in.DedupeKey}, true, nil
}

func (f *fakeNotifier) all() []notifications.New {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.created)
}

// fakeRecipes holds recipes by household.
type fakeRecipes map[string]recipes.Recipe

func (f fakeRecipes) Get(_ context.Context, householdID, id string) (recipes.Recipe, error) {
	r, ok := f[householdID+"/"+id]
	if !ok {
		return recipes.Recipe{}, recipes.ErrNotFound
	}
	return r, nil
}

type usageFixture struct {
	svc      *Service
	store    *memoryStore
	usage    *memoryUsageStore
	catalog  *fakeCatalog
	notifier *fakeNotifier
	clock    *time.Time
	ctx      context.Context
	actor    households.Membership
}

func amounts(unit string, bySize map[int]string) []recipes.Amount {
	var out []recipes.Amount
	for _, servings := range []int{2, 4} {
		if q, ok := bySize[servings]; ok {
			out = append(out, recipes.Amount{Servings: servings, Quantity: q, Unit: unit})
		}
	}
	return out
}

func newUsageFixture(t *testing.T) *usageFixture {
	t.Helper()
	svc, store, catalog := newTestService(t, "Butter", "Salt", "Olive Oil")
	usage, notifier := newMemoryUsageStore(), &fakeNotifier{}
	clock := testNow
	svc.now = func() time.Time { return clock }
	ids := 0
	svc.newID = func() string {
		ids++
		return strings.Repeat("0", 22) + string(rune('a'+ids/10)) + string(rune('0'+ids%10))
	}
	noodles := recipes.Recipe{
		ID: noodlesRecipe, Name: "Buttered Noodles", Servings: []int{2, 4},
		Ingredients: []recipes.RecipeIngredient{
			{IngredientID: catalog.id("Butter"), Name: "Butter", Amounts: amounts("tbsp", map[int]string{2: "2", 4: "4"})},
			{IngredientID: catalog.id("Salt"), Name: "Salt", Amounts: []recipes.Amount{{Servings: 2}, {Servings: 4}}},
			{Name: "Egg Noodles", Amounts: amounts("oz", map[int]string{2: "8", 4: "16"})},
			{Name: "Parmesan", Amounts: amounts("cup", map[int]string{2: "1/2", 4: "1"})},
		},
	}
	other := recipes.Recipe{ID: otherRecipe, Name: "Toast", Servings: []int{2},
		Ingredients: []recipes.RecipeIngredient{{Name: "butter", Amounts: amounts("count", map[int]string{2: "1"})}}}
	svc.WithUsage(UsageOptions{
		Store: usage, Notifier: notifier,
		Recipes: fakeRecipes{testHousehold + "/" + noodlesRecipe: noodles, testHousehold + "/" + otherRecipe: other},
	})
	return &usageFixture{svc: svc, store: store, usage: usage, catalog: catalog, notifier: notifier, clock: &clock, ctx: context.Background(), actor: member(testHousehold)}
}

func (f *usageFixture) advance(d time.Duration) { *f.clock = f.clock.Add(d) }

func (f *usageFixture) cook(t *testing.T, entryID string, servings int) (CookUsage, bool) {
	t.Helper()
	u, applied, err := f.svc.ApplyCooked(f.ctx, CookedMeal{
		HouseholdID: testHousehold, UserID: testUser, RecipeID: noodlesRecipe, EntryID: entryID, Servings: servings, OccurredAt: *f.clock,
	})
	if err != nil {
		t.Fatalf("ApplyCooked(%s) error = %v", entryID, err)
	}
	return u, applied
}

func (f *usageFixture) item(t *testing.T, id string) Item {
	t.Helper()
	item, err := f.store.GetItem(f.ctx, testHousehold, id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestPurchaseCookAndAlertFlow(t *testing.T) {
	f := newUsageFixture(t)

	// Checked off the grocery list and confirmed: 1 cup (16 tbsp) of butter.
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{
		Name: "Butter", Source: PurchaseGroceryList, Quantity: "1", Unit: "cup", Week: "2026-W38", ClientPurchaseID: "p1",
	})
	if err != nil || !res.Created {
		t.Fatalf("RecordPurchase() = %+v, %v", res, err)
	}
	butter := res.Item
	if butter.Status != StatusInStock || butter.Quantity != "1" || butter.IngredientID != f.catalog.id("Butter") ||
		butter.Tracking == nil || butter.Tracking.CycleID != res.Purchase.ID || butter.Tracking.Reference != "1" || butter.Tracking.CycleSource != CycleGroceryList {
		t.Fatalf("purchased item = %+v tracking %+v", butter, butter.Tracking)
	}
	if p := res.Purchase; p.ItemID != butter.ID || p.Source != PurchaseGroceryList || p.Week != "2026-W38" || p.RecordedBy != testUser || !p.PurchasedAt.Equal(testNow) {
		t.Errorf("purchase = %+v", p)
	}
	again, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Butter", Source: PurchaseGroceryList, Quantity: "2", Unit: "cup", ClientPurchaseID: "p1"})
	if err != nil || again.Created || again.Purchase.ID != res.Purchase.ID || again.Item.Quantity != "1" {
		t.Errorf("retried purchase = %+v, %v", again, err)
	}

	salt := mustAdd(t, f.svc, testHousehold, AddInput{Name: "Salt"}) // have some: not tracked

	// Cooked for 4: 4 tbsp of butter.
	f.advance(24 * time.Hour)
	u, applied := f.cook(t, "entry1", 4)
	if !applied || u.SourceKey != "entry:entry1" || u.ScaledFrom != 0 || len(u.Lines) != 2 {
		t.Fatalf("cook usage = %+v, applied %v", u, applied)
	}
	byItem := map[string]CookLine{}
	for _, line := range u.Lines {
		byItem[line.ItemID] = line
	}
	if l := byItem[butter.ID]; l.Deducted != "1/4" || l.TrackingUnit != "cup" || l.Quantity != "4" || l.Unit != "tbsp" || l.SkipReason != "" {
		t.Errorf("butter line = %+v", l)
	}
	if l := byItem[salt.ID]; l.SkipReason != SkipNotTracked {
		t.Errorf("salt line = %+v", l)
	}

	// Marking the same entry cooked again doesn't deduct twice.
	if _, applied := f.cook(t, "entry1", 4); applied {
		t.Error("second cook of entry1 applied")
	}
	if got := f.item(t, butter.ID).Tracking; got.RecipeUsed != "1/4" || got.RecipeUses != 1 {
		t.Errorf("after repeat = %+v", got)
	}

	// 4 more tbsp, then 3 servings (scaled from the 2-serving 2 tbsp): 11/16.
	f.cook(t, "entry2", 4)
	u, _ = f.cook(t, "entry3", 3)
	if u.ScaledFrom != 2 {
		t.Errorf("scaledFrom = %d, want 2", u.ScaledFrom)
	}
	got := f.item(t, butter.ID)
	if got.Tracking.RecipeUsed != "11/16" || got.Status != StatusInStock || len(f.notifier.all()) != 0 {
		t.Fatalf("at 69%% used = %+v, notifications %v", got.Tracking, f.notifier.all())
	}

	// 2 more tbsp crosses 80%: marked low by the estimate, with one alert.
	f.cook(t, "entry4", 2)
	got = f.item(t, butter.ID)
	if got.Status != StatusLow || got.StatusSource != StatusSourceEstimate || got.LowAlertCycleID != res.Purchase.ID {
		t.Fatalf("after crossing = %+v", got)
	}
	alerts := f.notifier.all()
	if len(alerts) != 1 || alerts[0].Type != notifications.TypePantryLow || alerts[0].Title != "Butter is running low" ||
		alerts[0].Subject.ID != butter.ID || alerts[0].DedupeKey != LowAlertDedupeKey(butter.ID, res.Purchase.ID) ||
		alerts[0].Body != "About 19% left: 4 recipes used 0.81 cup." {
		t.Fatalf("alerts = %+v", alerts)
	}

	// A person says it's fine: their status wins, and there's no second alert.
	fine := StatusInStock
	updated, err := f.svc.Update(f.ctx, f.actor, butter.ID, UpdateInput{Status: &fine})
	if err != nil || updated.Status != StatusInStock || updated.StatusSource != StatusSourcePerson {
		t.Fatalf("person update = %+v, %v", updated, err)
	}
	f.advance(24 * time.Hour)
	f.cook(t, "entry5", 2)
	if err := f.svc.Refresh(f.ctx, testHousehold); err != nil {
		t.Fatal(err)
	}
	if got := f.item(t, butter.ID); got.Status != StatusInStock || len(f.notifier.all()) != 1 {
		t.Errorf("after person override = %+v, %d alerts", got, len(f.notifier.all()))
	}

	// Restocking by hand starts a new cycle that can alert again.
	f.advance(24 * time.Hour)
	res2, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{ItemID: butter.ID, Source: PurchaseManual, Quantity: "1", Unit: "cup"})
	if err != nil || !res2.Created || res2.Item.Tracking.CycleID != res2.Purchase.ID || len(res2.Item.History) != 1 {
		t.Fatalf("restock = %+v, %v", res2, err)
	}
	if h := res2.Item.History[0]; h.RecipeUsed != "15/16" || h.Remaining != "0" || h.Observed {
		t.Errorf("closed segment = %+v", h)
	}
	// A cook that happened before the restock arrives late: skipped.
	late, applied, err := f.svc.ApplyCooked(f.ctx, CookedMeal{HouseholdID: testHousehold, UserID: testUser, RecipeID: noodlesRecipe, EntryID: "late", Servings: 4, OccurredAt: testNow})
	if err != nil || !applied || late.Lines[0].SkipReason != SkipBeforeCycle {
		t.Errorf("late cook = %+v, %v, %v", late, applied, err)
	}
	list, err := f.svc.ListPurchases(f.ctx, testHousehold, butter.ID)
	if err != nil || len(list) != 2 || list[0].ID != res2.Purchase.ID {
		t.Errorf("ListPurchases() = %+v, %v", list, err)
	}
}

func TestTimeDecayAlertsOnRead(t *testing.T) {
	f := newUsageFixture(t)
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Olive Oil", Source: PurchaseManual, Quantity: "16", Unit: "tbsp"})
	if err != nil {
		t.Fatal(err)
	}
	oil := f.item(t, res.Item.ID)
	next := cloneItem(oil)
	next.Rate = &Rate{PerDay: "1", Unit: "tbsp", Segments: 2}
	if _, err := f.store.UpdateItem(f.ctx, next); err != nil {
		t.Fatal(err)
	}

	f.advance(12 * 24 * time.Hour) // 12 of 16 tbsp: 75%
	items, err := f.svc.List(f.ctx, testHousehold, ListQuery{})
	if err != nil || items[0].Status != StatusInStock {
		t.Fatalf("List() at 75%% = %+v, %v", items, err)
	}
	// The household lowers its threshold: the next read alerts.
	if _, err := f.svc.UpdateSettings(f.ctx, f.actor, 70); err != nil {
		t.Fatal(err)
	}
	items, _ = f.svc.List(f.ctx, testHousehold, ListQuery{})
	if items[0].Status != StatusLow || items[0].StatusSource != StatusSourceEstimate || len(f.notifier.all()) != 1 {
		t.Fatalf("List() after threshold change = %+v, alerts %d", items[0], len(f.notifier.all()))
	}
	e := f.svc.Estimate(items[0], Settings{LowThresholdPercent: 70})
	if e.PercentRemaining != 25 || e.OtherUsed.Cmp(ratOf("12")) != 0 {
		t.Errorf("estimate = %+v", e)
	}
}

func TestAlertRetriesWhenNotificationFails(t *testing.T) {
	f := newUsageFixture(t)
	res, _ := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "2", Unit: "tbsp"})
	f.notifier.err = errors.New("down")
	f.advance(time.Hour)
	f.cook(t, "e1", 2) // uses all of it
	if got := f.item(t, res.Item.ID); got.Status != StatusInStock {
		t.Fatalf("status with failing notifier = %s; the alert must be retried, so the item stays in stock", got.Status)
	}
	f.notifier.err = nil
	if err := f.svc.Refresh(f.ctx, testHousehold); err != nil {
		t.Fatal(err)
	}
	if got := f.item(t, res.Item.ID); got.Status != StatusLow || len(f.notifier.all()) != 1 {
		t.Errorf("after refresh = %s, %d alerts", got.Status, len(f.notifier.all()))
	}
}

func TestCookSkipsWhatItCannotConvert(t *testing.T) {
	f := newUsageFixture(t)
	// Butter bought as 4 sticks of 1/2 cup: tracked in cups.
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{
		Name: "Butter", Source: PurchaseGroceryList, Quantity: "4", Unit: "count", UnitSizeQuantity: "1/2", UnitSizeUnit: "cup",
	})
	if err != nil || res.Item.Tracking.Unit != "cup" || res.Item.Tracking.Reference != "2" || res.Item.UnitSize.Unit != "count" {
		t.Fatalf("sized purchase = %+v, %v", res.Item, err)
	}
	// Parmesan bought by weight; the recipe measures it in cups.
	parm, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Parmesan", Source: PurchaseManual, Quantity: "8", Unit: "oz"})
	if err != nil {
		t.Fatal(err)
	}
	f.advance(time.Hour)
	u, _ := f.cook(t, "e1", 2)
	lines := map[string]CookLine{}
	for _, l := range u.Lines {
		lines[l.ItemID] = l
	}
	if l := lines[res.Item.ID]; l.Deducted != "1/8" {
		t.Errorf("butter line = %+v", l)
	}
	if l := lines[parm.Item.ID]; l.SkipReason != SkipUnitMismatch || l.Deducted != "" {
		t.Errorf("parmesan line = %+v", l)
	}
	got := f.item(t, parm.Item.ID)
	if got.Tracking.RecipeUsed != "0" || got.Tracking.SkippedUses != 1 {
		t.Errorf("parmesan tracking = %+v", got.Tracking)
	}
	// "1 butter" by count uses the stick size.
	if _, applied, err := f.svc.ApplyCooked(f.ctx, CookedMeal{HouseholdID: testHousehold, RecipeID: otherRecipe, EntryID: "toast", Servings: 2, OccurredAt: *f.clock}); err != nil || !applied {
		t.Fatalf("toast = %v, %v", applied, err)
	}
	if got := f.item(t, res.Item.ID); got.Tracking.RecipeUsed != "5/8" {
		t.Errorf("butter used = %s, want 1/8 + 1/2", got.Tracking.RecipeUsed)
	}

	// Nothing to do without an idempotency key, servings, or a known recipe.
	for _, m := range []CookedMeal{
		{HouseholdID: testHousehold, RecipeID: noodlesRecipe, Servings: 2},
		{HouseholdID: testHousehold, RecipeID: noodlesRecipe, EntryID: "x"},
		{HouseholdID: otherHousehold, RecipeID: noodlesRecipe, EntryID: "y", Servings: 2},
	} {
		if _, applied, err := f.svc.ApplyCooked(f.ctx, m); applied || err != nil {
			t.Errorf("ApplyCooked(%+v) = %v, %v", m, applied, err)
		}
	}
	// A client event ID stands in for a missing entry.
	m := CookedMeal{HouseholdID: testHousehold, UserID: testUser, ClientEventID: "ev1", RecipeID: noodlesRecipe, Servings: 2, OccurredAt: *f.clock}
	if u, applied, _ := f.svc.ApplyCooked(f.ctx, m); !applied || u.SourceKey != "event:"+testUser+":ev1" {
		t.Errorf("event-keyed cook = %+v, %v", u, applied)
	}
	if _, applied, _ := f.svc.ApplyCooked(f.ctx, m); applied {
		t.Error("event-keyed cook applied twice")
	}
}

func TestCookedListener(t *testing.T) {
	f := newUsageFixture(t)
	res, _ := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "1", Unit: "cup"})
	f.advance(time.Hour)
	cooked := events.Event{
		HouseholdID: testHousehold, UserID: testUser, Type: events.TypeRecipeCooked, RecipeID: noodlesRecipe,
		Payload: events.RecipeCooked{EntryID: "entry1", Servings: 2}, OccurredAt: *f.clock,
	}
	viewed := events.Event{HouseholdID: testHousehold, Type: events.TypeRecipeViewed, RecipeID: noodlesRecipe, Payload: events.RecipeViewed{}}
	listener := f.svc.CookedListener()
	listener.EventsStored(f.ctx, []events.Event{viewed, cooked, cooked})
	if got := f.item(t, res.Item.ID).Tracking; got.RecipeUsed != "1/8" || got.RecipeUses != 1 {
		t.Errorf("after listener = %+v", got)
	}
	if n := len(f.usage.cookUsages()); n != 1 {
		t.Errorf("cook usage records = %d", n)
	}
}

func TestRecordPurchaseValidation(t *testing.T) {
	f := newUsageFixture(t)
	for _, tc := range []struct {
		in   PurchaseInput
		want string
	}{
		{PurchaseInput{Name: "Butter"}, "source must be"},
		{PurchaseInput{Name: "Butter", Source: PurchaseProvider}, "shopping providers"},
		{PurchaseInput{Source: PurchaseManual}, "itemId, ingredientId, or name is required"},
		{PurchaseInput{ItemID: "x", Name: "Butter", Source: PurchaseManual}, "not both"},
		{PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "-1"}, "quantity must be"},
		{PurchaseInput{Name: "Butter", Source: PurchaseManual, Week: "2026-W38"}, "week applies only"},
		{PurchaseInput{Name: "Butter", Source: PurchaseGroceryList, Week: "38"}, "ISO week"},
		{PurchaseInput{Name: "Butter", Source: PurchaseManual, UnitSizeQuantity: "8", UnitSizeUnit: "oz"}, "requires a quantity"},
		{PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "1", Unit: "cup", UnitSizeQuantity: "8", UnitSizeUnit: "oz"}, "applies only to a unit such as"},
		{PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "1", Unit: "package", UnitSizeQuantity: "8", UnitSizeUnit: "can"}, "volume or weight"},
		{PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "1", Unit: "package", UnitSizeQuantity: "8"}, "positive quantity and a unit"},
		{PurchaseInput{Name: "Butter", Source: PurchaseManual, ClientPurchaseID: " x"}, "whitespace"},
		{PurchaseInput{Name: "Butter", Source: PurchaseManual, ClientPurchaseID: strings.Repeat("x", 65)}, "at most 64"},
	} {
		_, err := f.svc.RecordPurchase(f.ctx, f.actor, tc.in)
		wantValidation(t, err, tc.want)
	}
	if _, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{ItemID: "66e5a1f2c3b4a5d6e7f80d99", Source: PurchaseManual}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown item error = %v", err)
	}
	viewer := households.Membership{HouseholdID: testHousehold, UserID: testUser, Role: "viewer"}
	if _, err := f.svc.RecordPurchase(f.ctx, viewer, PurchaseInput{Name: "Butter", Source: PurchaseManual}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer error = %v", err)
	}
	if _, err := f.svc.UpdateSettings(f.ctx, f.actor, 0); err == nil {
		t.Error("UpdateSettings(0) succeeded")
	}
	// Without an amount the item is in stock but not tracked.
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Salt", Source: PurchaseManual})
	if err != nil || res.Item.Tracking != nil || res.Item.Status != StatusInStock {
		t.Errorf("unquantified purchase = %+v, %v", res.Item, err)
	}

	unconfigured, _, _ := newTestService(t)
	if _, err := unconfigured.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Salt", Source: PurchaseManual}); !errors.Is(err, errUsageNotConfigured) {
		t.Errorf("unconfigured error = %v", err)
	}
	if s, err := unconfigured.Settings(f.ctx, testHousehold); err != nil || s.LowThresholdPercent != DefaultLowThresholdPercent {
		t.Errorf("unconfigured settings = %+v, %v", s, err)
	}
}

func TestSettingsAndItemThreshold(t *testing.T) {
	f := newUsageFixture(t)
	if s, err := f.svc.Settings(f.ctx, testHousehold); err != nil || s.LowThresholdPercent != 80 || !s.UpdatedAt.IsZero() {
		t.Fatalf("default settings = %+v, %v", s, err)
	}
	s, err := f.svc.UpdateSettings(f.ctx, f.actor, 60)
	if err != nil || s.LowThresholdPercent != 60 || s.UpdatedBy != testUser {
		t.Fatalf("UpdateSettings() = %+v, %v", s, err)
	}
	res, _ := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "1", Unit: "cup"})
	item, err := f.svc.Update(f.ctx, f.actor, res.Item.ID, UpdateInput{LowThresholdPercent: ptr(90)})
	if err != nil || item.LowThresholdPercent != 90 {
		t.Fatalf("set item threshold = %+v, %v", item, err)
	}
	if e := f.svc.Estimate(item, s); e.ThresholdPct != 90 || !e.ThresholdItem {
		t.Errorf("estimate threshold = %+v", e)
	}
	item, _ = f.svc.Update(f.ctx, f.actor, res.Item.ID, UpdateInput{LowThresholdPercent: ptr(0)})
	if e := f.svc.Estimate(item, s); item.LowThresholdPercent != 0 || e.ThresholdPct != 60 || e.ThresholdItem {
		t.Errorf("cleared threshold = %d, %+v", item.LowThresholdPercent, e)
	}
	_, err = f.svc.Update(f.ctx, f.actor, res.Item.ID, UpdateInput{LowThresholdPercent: ptr(101)})
	wantValidation(t, err, "lowThresholdPercent")

	// A correction by a person that lands below the threshold alerts once.
	item, err = f.svc.Update(f.ctx, f.actor, res.Item.ID, UpdateInput{Quantity: ptr("2"), Unit: ptr("tbsp")})
	if err != nil || item.Status != StatusLow || item.StatusSource != StatusSourceEstimate || len(f.notifier.all()) != 1 {
		t.Errorf("low correction = %+v, %v, alerts %d", item, err, len(f.notifier.all()))
	}
}
