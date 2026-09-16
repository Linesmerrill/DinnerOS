package shopping

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

const (
	testHousehold = "66e5a1f2c3b4a5d6e7f80a01"
	testUser      = "66e5a1f2c3b4a5d6e7f80c01"
	otherMember   = "66e5a1f2c3b4a5d6e7f80c02"
	viewerUser    = "66e5a1f2c3b4a5d6e7f80c03"
	outsider      = "66e5a1f2c3b4a5d6e7f80c04"
	testWeek      = "2026-W38"
)

var testNow = time.Date(2026, 9, 15, 18, 30, 0, 0, time.UTC)

func memberOf(userID string) households.Membership {
	return households.Membership{HouseholdID: testHousehold, UserID: userID, Role: households.RoleMember}
}

func viewer() households.Membership {
	return households.Membership{HouseholdID: testHousehold, UserID: viewerUser, Role: households.Role("viewer")}
}

// recorder keeps recorded events.
type recorder struct {
	mu   sync.Mutex
	list []events.Event
}

func (r *recorder) Record(_ context.Context, e events.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.list = append(r.list, e)
	return nil
}

func (r *recorder) types() []events.Type {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []events.Type
	for _, e := range r.list {
		out = append(out, e.Type)
	}
	return out
}

func line(name string, quantity float64, unit string) recipes.ImportIngredient {
	amount := recipes.ImportAmount{Servings: 2, Unit: unit, SourceUnit: unit, RawText: name}
	if unit != "" {
		amount.Quantity = &quantity
	}
	return recipes.ImportIngredient{SourceIngredientID: "i-" + strings.ReplaceAll(strings.ToLower(name), " ", "-"), Name: name, Amounts: []recipes.ImportAmount{amount}}
}

type fixture struct {
	ctx     context.Context
	svc     *Service
	store   *MongoStore
	pantry  *pantry.Service
	events  *recorder
	clock   *time.Time
	keys    map[string]string // grocery line name → ingredient key
	actor   households.Membership
	recipes *recipes.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(ctx, slices.Concat(recipes.Indexes(), pantry.Indexes(), planning.Indexes(), Indexes())...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	recipeSvc := recipes.NewService(recipes.NewMongoStore(db))
	recipe := func(id, name string, lines ...recipes.ImportIngredient) recipes.ImportRecipe {
		return recipes.ImportRecipe{
			Source: recipes.SourceHelloFresh, SourceRecipeID: id, SourceURL: "https://recipes.example.com/" + id, Name: name,
			Servings: []int{2}, TotalMinutes: 30, Ingredients: lines, Steps: []recipes.ImportStep{{Index: 1, Text: "Cook."}},
		}
	}
	if _, err := recipeSvc.Import(ctx, testHousehold, recipes.ImportFile{
		Version: recipes.ImportVersion, Source: recipes.SourceHelloFresh, GeneratedAt: testNow,
		Recipes: []recipes.ImportRecipe{
			recipe("r-tacos", "Beef Tacos",
				line("Ground Beef", 20, "oz"), line("Yellow Onion", 2, "count"), line("Garlic", 4, "clove"),
				line("Milk", 1, "cup"), line("Flour Tortillas", 6, "count"), line("Tomato Paste", 1, "tbsp")),
			recipe("r-chili", "Beef Chili", line("Ground Beef", 1, "lb"), line("Kidney Beans", 2, "can")),
		},
	}); err != nil {
		t.Fatal(err)
	}
	pantryStore := pantry.NewMongoStore(db)
	pantrySvc := pantry.NewService(pantryStore, recipeSvc).WithUsage(pantry.UsageOptions{Store: pantryStore, Recipes: recipeSvc})
	actor := memberOf(testUser)
	if _, _, err := pantrySvc.Add(ctx, actor, pantry.AddInput{Name: "Tomato Paste"}); err != nil {
		t.Fatal(err)
	}
	plans := planning.NewService(planning.NewMongoStore(db), recipeSvc).WithPantry(pantrySvc)
	page, err := recipeSvc.List(ctx, testHousehold, recipes.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range page.Items {
		if _, _, err := plans.AddEntry(ctx, testHousehold, testUser, testWeek, planning.NewEntry{RecipeID: r.ID, Servings: 2}); err != nil {
			t.Fatal(err)
		}
	}
	g, err := plans.GroceryList(ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, c := range g.Categories {
		for _, it := range c.Items {
			keys[it.Name] = it.IngredientKey
		}
	}
	store := NewMongoStore(db)
	rec := &recorder{}
	clock := testNow
	svc := NewService(ServiceOptions{
		Store: store, Providers: providers.NewRegistry(providers.NewWalmart(providers.WalmartOptions{})),
		Grocery: plans, Catalog: recipeSvc, Pantry: pantrySvc, Events: rec,
	})
	svc.now = func() time.Time { return clock }
	return &fixture{ctx: ctx, svc: svc, store: store, pantry: pantrySvc, events: rec, clock: &clock, keys: keys, actor: actor, recipes: recipeSvc}
}

func (f *fixture) save(t *testing.T, name, product string, size *PackageSize) Preference {
	t.Helper()
	p, _, err := f.svc.PutPreference(f.ctx, f.actor, "walmart", f.keys[name], PreferenceInput{ProductURL: product, DisplayName: name + " (store)", PackageSize: size})
	if err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
	return p
}

func lineFor(t *testing.T, h Proposal, name string) HandoffLine {
	t.Helper()
	for _, l := range h.Lines {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("no line for %s in %+v", name, h.Lines)
	return HandoffLine{}
}

func wantValidation(t *testing.T, err error, contains string) {
	t.Helper()
	var v *ValidationError
	var pv *providers.ValidationError
	switch {
	case errors.As(err, &v) && strings.Contains(v.Message, contains):
	case errors.As(err, &pv) && strings.Contains(pv.Message, contains):
	default:
		t.Errorf("error = %v, want a validation error containing %q", err, contains)
	}
}

func TestIntegrationSettingsAndPreferences(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	s, err := f.svc.Settings(ctx, testHousehold)
	if err != nil || s.Provider != "" || s.StoreID != "" {
		t.Fatalf("default settings = %+v, %v", s, err)
	}
	if s, err = f.svc.UpdateSettings(ctx, f.actor, "walmart", " 05435 "); err != nil || s.Provider != providers.KeyWalmart || s.StoreID != "5435" ||
		s.UpdatedBy != testUser || !s.UpdatedAt.Equal(testNow) {
		t.Fatalf("UpdateSettings() = %+v, %v", s, err)
	}
	if s, err = f.svc.UpdateSettings(ctx, f.actor, "walmart", ""); err != nil || s.StoreID != "" {
		t.Errorf("clearing the store = %+v, %v", s, err)
	}
	if _, err := f.svc.UpdateSettings(ctx, viewer(), "walmart", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer error = %v", err)
	}
	if _, err := f.svc.UpdateSettings(ctx, f.actor, "instacart", ""); !errors.Is(err, ErrProviderUnavailable) {
		t.Errorf("instacart error = %v", err)
	}
	if _, err := f.svc.UpdateSettings(ctx, f.actor, "amazon", ""); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("amazon error = %v", err)
	}
	_, err = f.svc.UpdateSettings(ctx, f.actor, "walmart", "store 12")
	wantValidation(t, err, "storeId")

	beef := f.keys["Ground Beef"]
	p, created, err := f.svc.PutPreference(ctx, f.actor, "walmart", beef, PreferenceInput{
		ProductURL: "https://www.walmart.com/ip/Test-Ground-Beef-16-oz/100000001?classType=REGULAR", DisplayName: " Beef, 16 oz ",
		PackageSize: &PackageSize{Quantity: "16.0", Unit: "oz"},
	})
	if err != nil || !created || p.ProductID != "100000001" || p.DisplayName != "Beef, 16 oz" || p.IngredientName != "Ground Beef" ||
		p.PackageSize == nil || *p.PackageSize != (PackageSize{Quantity: "16", Unit: "oz"}) || p.CreatedBy != testUser {
		t.Fatalf("PutPreference() = %+v, %v, %v", p, created, err)
	}
	f.advance(time.Minute)
	updated, created, err := f.svc.PutPreference(ctx, memberOf(otherMember), "walmart", beef, PreferenceInput{ProductID: "100000011", DisplayName: "Beef 1 lb"})
	if err != nil || created || updated.ID != p.ID || updated.ProductID != "100000011" || updated.PackageSize != nil ||
		updated.CreatedBy != testUser || updated.UpdatedBy != otherMember || !updated.CreatedAt.Equal(testNow) {
		t.Errorf("update = %+v, %v, %v", updated, created, err)
	}
	named, _, err := f.svc.PutPreference(ctx, f.actor, "walmart", "name:store brand salsa", PreferenceInput{ProductID: "100000099", DisplayName: "Salsa"})
	if err != nil || named.IngredientName != "store brand salsa" {
		t.Errorf("name key = %+v, %v", named, err)
	}
	list, err := f.svc.ListPreferences(ctx, testHousehold, "walmart")
	if err != nil || len(list) != 2 || list[0].IngredientKey != beef || list[1].IngredientKey != "name:store brand salsa" {
		t.Errorf("ListPreferences() = %+v, %v", list, err)
	}
	if got, err := f.svc.GetPreference(ctx, testHousehold, "walmart", beef); err != nil || got.ProductID != "100000011" {
		t.Errorf("GetPreference() = %+v, %v", got, err)
	}
	if err := f.svc.DeletePreference(ctx, f.actor, "walmart", "name:store brand salsa"); err != nil {
		t.Error(err)
	}
	if err := f.svc.DeletePreference(ctx, f.actor, "walmart", "name:store brand salsa"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete error = %v", err)
	}
	if _, err := f.svc.GetPreference(ctx, "66e5a1f2c3b4a5d6e7f80a99", "walmart", beef); !errors.Is(err, ErrNotFound) {
		t.Errorf("other household error = %v", err)
	}

	for _, tc := range []struct {
		key  string
		in   PreferenceInput
		want string
	}{
		{beef, PreferenceInput{DisplayName: "x"}, "productUrl or productId"},
		{beef, PreferenceInput{ProductURL: "https://www.walmart.com/ip/100000001", ProductID: "100000001", DisplayName: "x"}, "not both"},
		{beef, PreferenceInput{ProductURL: "https://www.target.com/p/beef/-/A-100000001", DisplayName: "x"}, "walmart.com"},
		{beef, PreferenceInput{ProductID: "https://www.walmart.com/ip/100000001", DisplayName: "x"}, "numeric ID"},
		{beef, PreferenceInput{ProductID: "100000001"}, "displayName is required"},
		{beef, PreferenceInput{ProductID: "100000001", DisplayName: "x", PackageSize: &PackageSize{Quantity: "0", Unit: "oz"}}, "packageSize"},
		{beef, PreferenceInput{ProductID: "100000001", DisplayName: "x", PackageSize: &PackageSize{Quantity: "16", Unit: "ounces"}}, "packageSize"},
		{"ground beef", PreferenceInput{ProductID: "100000001", DisplayName: "x"}, "ingredientKey"},
		{"66e5a1f2c3b4a5d6e7f80999", PreferenceInput{ProductID: "100000001", DisplayName: "x"}, "catalog ingredient"},
	} {
		_, _, err := f.svc.PutPreference(ctx, f.actor, "walmart", tc.key, tc.in)
		wantValidation(t, err, tc.want)
	}
	if _, _, err := f.svc.PutPreference(ctx, viewer(), "walmart", beef, PreferenceInput{ProductID: "100000001", DisplayName: "x"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer error = %v", err)
	}
	if err := f.svc.DeletePreference(ctx, viewer(), "walmart", beef); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer delete error = %v", err)
	}
}

func (f *fixture) advance(d time.Duration) { *f.clock = f.clock.Add(d) }

func TestIntegrationHandoffAndConfirm(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	if _, err := f.svc.UpdateSettings(ctx, f.actor, "walmart", "5435"); err != nil {
		t.Fatal(err)
	}
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	f.save(t, "Yellow Onion", "https://www.walmart.com/ip/100000002", &PackageSize{Quantity: "3", Unit: "count"})
	f.save(t, "Garlic", "100000003", &PackageSize{Quantity: "3", Unit: "count"})
	f.save(t, "Milk", "https://walmart.com/ip/Test-Milk/100000004", nil)
	f.save(t, "Kidney Beans", "https://www.walmart.com/ip/Test-Beans/100000005", &PackageSize{Quantity: "1", Unit: "can"})
	f.save(t, "Tomato Paste", "https://www.walmart.com/ip/Test-Paste/100000006", &PackageSize{Quantity: "6", Unit: "oz"})

	// Match: nothing is stored.
	m, err := f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	beef := lineFor(t, m, "Ground Beef")
	if beef.Packages != 3 || beef.Reason != "" || beef.PackageCount().Needed == nil || providers.CoverageText(beef.PackageCount(), amountFromSize(beef.PackageSize)) != "3 × 16 oz covers 36 oz" {
		t.Errorf("beef = %+v", beef)
	}
	if l := lineFor(t, m, "Yellow Onion"); l.Packages != 1 || l.Reason != "" {
		t.Errorf("onion = %+v", l)
	}
	// 4 cloves against a bulb can't be measured, so the weekly rule buys one
	// bulb for the week instead of flagging the line (docs/api.md#coverage).
	if l := lineFor(t, m, "Garlic"); l.Packages != 1 || l.Reason != "" || !l.CoversWeek || l.Coverage != providers.CoveragePerWeek {
		t.Errorf("garlic = %+v", l)
	}
	if l := lineFor(t, m, "Milk"); l.Packages != 1 || l.Reason != providers.ReasonNoPackageSize {
		t.Errorf("milk = %+v", l)
	}
	if l := lineFor(t, m, "Kidney Beans"); l.Packages != 2 {
		t.Errorf("beans = %+v", l)
	}
	got := reasons(m)
	if got[f.keys["Tomato Paste"]] != ExcludedInPantry || got[f.keys["Flour Tortillas"]] != ExcludedNoProduct || len(m.Lines) != 5 {
		t.Errorf("excluded = %v, lines %d", got, len(m.Lines))
	}
	if len(m.Links) != 1 || !strings.HasSuffix(m.Links[0].URL, "&storeId=5435") || !strings.Contains(m.Links[0].URL, "100000001_3") || len(m.Links[0].LineIDs) != 5 {
		t.Errorf("links = %+v", m.Links)
	}
	if list, _ := f.svc.ListHandoffs(ctx, testHousehold, HandoffFilter{}); len(list) != 0 {
		t.Errorf("match stored a handoff: %+v", list)
	}
	if _, err := f.svc.Match(ctx, testHousehold, "2026-38", "walmart", MatchInput{}); !errors.Is(err, planning.ErrInvalidWeek) {
		t.Errorf("bad week error = %v", err)
	}

	// Create.
	if _, _, err := f.svc.CreateHandoff(ctx, viewer(), testWeek, "walmart", MatchInput{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer error = %v", err)
	}
	_, _, err = f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{Lines: []LineSelection{}})
	wantValidation(t, err, "no grocery line")
	h, isNew, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{CheckedOffKeys: []string{f.keys["Milk"]}})
	if err != nil || !isNew || !h.Active || h.Revision != 1 {
		t.Fatalf("CreateHandoff() = %+v, %v, %v", h, isNew, err)
	}
	if h.ID == "" || len(h.Lines) != 4 || h.Status() != HandoffOpen || h.CreatedBy != testUser || h.StoreID != "5435" || reasons(h.Proposal)[f.keys["Milk"]] != ExcludedCheckedOff {
		t.Fatalf("handoff = %+v", h)
	}
	stored, err := f.svc.GetHandoff(ctx, testHousehold, h.ID)
	storedGarlic := lineFor(t, stored.Proposal, "Garlic")
	if err != nil || len(stored.Lines) != 4 || stored.Links[0].URL != h.Links[0].URL ||
		storedGarlic.Coverage != providers.CoveragePerWeek || !storedGarlic.CoversWeek || storedGarlic.Reason != "" ||
		stored.Lines[0].Amounts[0].Quantity == "" || !stored.CreatedAt.Equal(testNow) {
		t.Errorf("GetHandoff() = %+v, %v", stored, err)
	}
	if open, _ := f.svc.ListHandoffs(ctx, testHousehold, HandoffFilter{Week: testWeek, Status: HandoffOpen}); len(open) != 1 {
		t.Errorf("open handoffs = %d", len(open))
	}
	if _, err := f.svc.GetHandoff(ctx, "66e5a1f2c3b4a5d6e7f80a99", h.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("other household error = %v", err)
	}

	// Confirm some lines, with an adjusted count.
	beefLine, beansLine := lineFor(t, h.Proposal, "Ground Beef"), lineFor(t, h.Proposal, "Kidney Beans")
	if _, err := f.svc.Confirm(ctx, viewer(), h.ID, ConfirmInput{All: true}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer confirm error = %v", err)
	}
	in := ConfirmInput{Lines: []ConfirmLine{{LineID: beefLine.ID, Packages: 2}, {LineID: beansLine.ID}}}
	res, err := f.svc.Confirm(ctx, f.actor, h.ID, in)
	if err != nil || len(res.Purchases) != 2 || !res.Purchases[0].Created || res.Handoff.Status() != HandoffOpen {
		t.Fatalf("Confirm() = %+v, %v", res, err)
	}
	beefBuy := res.Purchases[0]
	if p := beefBuy.Purchase; p.Source != pantry.PurchaseProvider || p.Quantity != "2" || p.Unit != "package" || p.UnitSize == nil || p.UnitSize.Quantity != "16" ||
		p.Provider == nil || p.Provider.HandoffID != h.ID || p.Provider.LineID != beefLine.ID || p.Provider.ProductID != "100000001" || p.Week != testWeek {
		t.Errorf("beef purchase = %+v", p)
	}
	if it := beefBuy.Item; it.Status != pantry.StatusInStock || it.Tracking == nil || it.Tracking.Reference != "32" || it.Tracking.Unit != "oz" || it.IngredientID != f.keys["Ground Beef"] {
		t.Errorf("beef item = %+v", it)
	}
	if p := res.Purchases[1].Purchase; p.Quantity != "2" || p.Unit != "can" || p.UnitSize != nil {
		t.Errorf("beans purchase = %+v", p)
	}
	if l, _ := res.Handoff.Line(beefLine.ID); l.Status != LineConfirmed || l.ConfirmedPackages != 2 || l.PurchaseID != beefBuy.Purchase.ID || l.ConfirmedBy != testUser {
		t.Errorf("confirmed line = %+v", l)
	}

	// Retrying records nothing new.
	f.advance(time.Hour)
	again, err := f.svc.Confirm(ctx, memberOf(otherMember), h.ID, in)
	if err != nil || len(again.Purchases) != 2 || again.Purchases[0].Created || again.Purchases[0].Purchase.ID != beefBuy.Purchase.ID {
		t.Errorf("retry = %+v, %v", again, err)
	}
	history, err := f.pantry.ListPurchases(ctx, testHousehold, beefBuy.Item.ID)
	if err != nil || len(history) != 1 {
		t.Errorf("beef purchases = %+v, %v", history, err)
	}

	// A line another request is confirming conflicts until its claim is stale.
	onion := lineFor(t, h.Proposal, "Yellow Onion")
	if ok, err := f.store.ClaimLine(ctx, testHousehold, h.ID, onion.ID, *f.clock, f.clock.Add(-claimTimeout)); err != nil || !ok {
		t.Fatalf("ClaimLine() = %v, %v", ok, err)
	}
	if ok, _ := f.store.ClaimLine(ctx, testHousehold, h.ID, onion.ID, *f.clock, f.clock.Add(-claimTimeout)); ok {
		t.Error("a held claim was taken")
	}
	if _, err := f.svc.Confirm(ctx, f.actor, h.ID, ConfirmInput{Lines: []ConfirmLine{{LineID: onion.ID}}}); !errors.Is(err, ErrConflict) {
		t.Errorf("claimed line error = %v", err)
	}
	f.advance(2 * claimTimeout)

	// Everything else, as another member.
	all, err := f.svc.Confirm(ctx, memberOf(otherMember), h.ID, ConfirmInput{All: true})
	if err != nil || len(all.Purchases) != 4 || all.Handoff.Status() != HandoffDone {
		t.Fatalf("Confirm(all) = %+v, %v", all, err)
	}
	created := 0
	for _, p := range all.Purchases {
		if p.Created {
			created++
		}
		if p.LineID == onion.ID && (p.Purchase.Quantity != "3" || p.Purchase.Unit != "count" || p.Purchase.RecordedBy != otherMember) {
			t.Errorf("onion purchase = %+v", p.Purchase)
		}
	}
	if created != 2 {
		t.Errorf("created %d purchases, want 2", created)
	}
	if done, _ := f.svc.ListHandoffs(ctx, testHousehold, HandoffFilter{Status: HandoffDone}); len(done) != 1 {
		t.Errorf("done handoffs = %d", len(done))
	}
	if types := f.events.types(); !slices.Equal(types, []events.Type{events.TypeShoppingHandoffCreated, events.TypeShoppingOrderConfirmed, events.TypeShoppingOrderConfirmed}) {
		t.Errorf("events = %v", types)
	}
	if p, ok := f.events.list[1].Payload.(events.ShoppingOrderConfirmed); !ok || p.Confirmed != 2 || p.Packages != 4 || p.HandoffID != h.ID || f.events.list[1].Week != testWeek {
		t.Errorf("confirm event = %+v", f.events.list[1])
	}

	// Confirmed purchases put those items in the pantry, so the default list
	// now leaves them out.
	next, err := f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil || len(next.Lines) != 1 || next.Lines[0].Name != "Milk" || reasons(next)[f.keys["Ground Beef"]] != ExcludedInPantry {
		t.Errorf("match after confirming = %+v, %v", next, err)
	}

	// Skip the rest, then confirm a skipped line later.
	// Answering every line closed the first handoff, so this send starts a
	// new one rather than adding to it.
	if closed, _ := f.svc.GetHandoff(ctx, testHousehold, h.ID); closed.Active || closed.ClosedReason != ClosedConfirmed {
		t.Errorf("answered handoff = active %v, reason %q", closed.Active, closed.ClosedReason)
	}
	h2, isNew, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{Lines: []LineSelection{
		{IngredientKey: f.keys["Milk"]}, {IngredientKey: f.keys["Ground Beef"]}, {IngredientKey: f.keys["Kidney Beans"]},
	}})
	if err != nil || !isNew || len(h2.Lines) != 3 || h2.ID == h.ID {
		t.Fatalf("second handoff = %+v, %v", h2, err)
	}
	res, err = f.svc.Confirm(ctx, f.actor, h2.ID, ConfirmInput{Lines: []ConfirmLine{{LineID: "l1"}}, SkipRest: true})
	if err != nil || res.Handoff.Status() != HandoffDone || len(res.Purchases) != 1 {
		t.Fatalf("skip rest = %+v, %v", res, err)
	}
	skipped, _ := res.Handoff.Line("l2")
	if skipped.Status != LineSkipped || skipped.SkippedBy != testUser {
		t.Errorf("skipped line = %+v", skipped)
	}
	if all, _ := f.svc.Confirm(ctx, f.actor, h2.ID, ConfirmInput{All: true}); len(all.Purchases) != 1 {
		t.Errorf("all after skipping = %+v", all.Purchases)
	}
	res, err = f.svc.Confirm(ctx, f.actor, h2.ID, ConfirmInput{Lines: []ConfirmLine{{LineID: "l2", Packages: 1}}})
	if l, _ := res.Handoff.Line("l2"); err != nil || l.Status != LineConfirmed || !res.Purchases[0].Created {
		t.Errorf("confirming a skipped line = %+v, %v", l, err)
	}
	if _, err := f.svc.Confirm(ctx, f.actor, "66e5a1f2c3b4a5d6e7f80b99", ConfirmInput{All: true}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown handoff error = %v", err)
	}
}

// --- HTTP ---------------------------------------------------------------------

type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer mirrors households.Service.Authorize.
type fakeAuthorizer map[string]households.Role

func (f fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	role, ok := f[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	m := households.Membership{HouseholdID: householdID, UserID: userID, Role: role}
	if perm != households.PermHouseholdView && !role.Can(perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

func TestIntegrationShoppingHTTP(t *testing.T) {
	f := newFixture(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember, testHousehold + "/" + viewerUser: "viewer"},
	}).Mount)
	do := func(method, path, body, userID string) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}
	hh := "/households/" + testHousehold

	rec, body := do(http.MethodGet, "/shopping/providers", "", outsider)
	items, _ := body["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["key"] != "walmart" ||
		items[0].(map[string]any)["capabilities"].(map[string]any)["handoff"] != "cart_link" {
		t.Fatalf("providers = %d %v", rec.Code, body)
	}
	if rec, body = do(http.MethodGet, hh+"/shopping/settings", "", testUser); rec.Code != 200 || body["provider"] != nil {
		t.Errorf("settings = %d %v", rec.Code, body)
	}
	if rec, body = do(http.MethodPut, hh+"/shopping/settings", `{"provider":"walmart","storeId":"5435"}`, testUser); rec.Code != 200 || body["storeId"] != "5435" {
		t.Errorf("put settings = %d %v", rec.Code, body)
	}
	for _, tc := range []struct {
		method, path, body, user string
		status                   int
		code                     string
	}{
		{http.MethodPut, "/shopping/settings", `{"provider":"walmart"}`, viewerUser, 403, "forbidden"},
		{http.MethodGet, "/shopping/settings", "", outsider, 404, "not_found"},
		{http.MethodPut, "/shopping/settings", `{"provider":"walmart","storeId":""}`, testUser, 400, "validation_failed"},
		{http.MethodGet, "/shopping/instacart/preferences", "", testUser, 503, "provider_unavailable"},
		{http.MethodGet, "/shopping/amazon/preferences", "", testUser, 404, "not_found"},
		{http.MethodPut, "/shopping/walmart/preferences/name:red%20onion", `{"productUrl":"https://walmrt.us/abc","displayName":"x"}`, testUser, 400, "validation_failed"},
		{http.MethodPut, "/shopping/walmart/preferences/name:red%20onion", `{"productId":"100000001","displayName":"x"}`, viewerUser, 403, "forbidden"},
		{http.MethodPost, "/plans/2026-W99/shopping/walmart/match", `{}`, testUser, 400, "validation_failed"},
		{http.MethodPost, "/plans/2026-W38/shopping/walmart/handoffs", `{}`, viewerUser, 403, "forbidden"},
		{http.MethodPost, "/plans/2026-W38/shopping/walmart/handoffs", `{}`, testUser, 400, "validation_failed"},
		{http.MethodGet, "/shopping/handoffs/66e5a1f2c3b4a5d6e7f80b99", "", testUser, 404, "not_found"},
		{http.MethodGet, "/shopping/handoffs?status=closed", "", testUser, 400, "validation_failed"},
	} {
		rec, body := do(tc.method, hh+tc.path, tc.body, tc.user)
		errBody, _ := body["error"].(map[string]any)
		if rec.Code != tc.status || errBody["code"] != tc.code {
			t.Errorf("%s %s as %s = %d %v, want %d %s", tc.method, tc.path, tc.user, rec.Code, body, tc.status, tc.code)
		}
	}

	rec, body = do(http.MethodPut, hh+"/shopping/walmart/preferences/name:red%20onion",
		`{"productUrl":"https://www.walmart.com/ip/Test-Red-Onion/100000021","displayName":"Red onions","packageSize":{"quantity":"2","unit":"lb"}}`, testUser)
	if rec.Code != http.StatusCreated || body["ingredientKey"] != "name:red onion" || body["productUrl"] != "https://www.walmart.com/ip/100000021" ||
		body["ingredientId"] != nil || body["packageSize"].(map[string]any)["text"] != "2 lb" {
		t.Errorf("put preference = %d %v", rec.Code, body)
	}
	beefPath := hh + "/shopping/walmart/preferences/" + f.keys["Ground Beef"]
	if rec, body = do(http.MethodPut, beefPath, `{"productId":"100000001","displayName":"Beef","packageSize":{"quantity":"16","unit":"oz"}}`, testUser); rec.Code != 201 {
		t.Fatalf("put beef = %d %v", rec.Code, body)
	}
	if rec, _ = do(http.MethodPut, beefPath, `{"productId":"100000001","displayName":"Beef 16 oz","packageSize":{"quantity":"16","unit":"oz"}}`, testUser); rec.Code != 200 {
		t.Errorf("update beef = %d", rec.Code)
	}
	if rec, body = do(http.MethodGet, hh+"/shopping/walmart/preferences", "", viewerUser); rec.Code != 200 || len(body["items"].([]any)) != 2 {
		t.Errorf("list preferences = %d %v", rec.Code, body)
	}
	if rec, _ = do(http.MethodDelete, hh+"/shopping/walmart/preferences/name:red%20onion", "", testUser); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d", rec.Code)
	}

	rec, body = do(http.MethodPost, hh+"/plans/2026-W38/shopping/walmart/match", `{}`, viewerUser)
	lines, _ := body["lines"].([]any)
	if rec.Code != 200 || len(lines) != 1 || body["storeId"] != "5435" || len(body["cartLinks"].([]any)) != 1 {
		t.Fatalf("match = %d %v", rec.Code, body)
	}
	beefLine := lines[0].(map[string]any)
	if beefLine["packages"] != 3.0 || beefLine["coverageText"] != "3 × 16 oz covers 36 oz" || beefLine["checkAmount"] != false || beefLine["confirmation"] != nil {
		t.Errorf("match line = %v", beefLine)
	}

	rec, body = do(http.MethodPost, hh+"/plans/2026-W38/shopping/walmart/handoffs", `{"lines":[{"ingredientKey":"`+f.keys["Ground Beef"]+`","packages":2}]}`, testUser)
	if rec.Code != http.StatusCreated || body["status"] != "open" || body["id"] == "" {
		t.Fatalf("create handoff = %d %v", rec.Code, body)
	}
	id := body["id"].(string)
	line := body["lines"].([]any)[0].(map[string]any)
	if line["packagesOverridden"] != true || line["confirmation"].(map[string]any)["status"] != "pending" ||
		!strings.HasSuffix(body["cartLinks"].([]any)[0].(map[string]any)["url"].(string), "items=100000001_2&storeId=5435") {
		t.Errorf("handoff = %v", body)
	}
	if rec, _ = do(http.MethodPost, hh+"/shopping/handoffs/"+id+"/confirm", `{"all":true}`, viewerUser); rec.Code != http.StatusForbidden {
		t.Errorf("viewer confirm = %d", rec.Code)
	}
	for range 2 {
		rec, body = do(http.MethodPost, hh+"/shopping/handoffs/"+id+"/confirm", `{"all":true}`, testUser)
		purchases, _ := body["purchases"].([]any)
		if rec.Code != 200 || len(purchases) != 1 || body["handoff"].(map[string]any)["status"] != "done" {
			t.Fatalf("confirm = %d %v", rec.Code, body)
		}
		p := purchases[0].(map[string]any)
		purchase := p["purchase"].(map[string]any)
		if p["ingredientKey"] != f.keys["Ground Beef"] || purchase["source"] != "provider" || purchase["provider"].(map[string]any)["lineId"] != "l1" ||
			p["item"].(map[string]any)["status"] != "in_stock" {
			t.Errorf("confirmed purchase = %v", p)
		}
	}
	if rec, body = do(http.MethodGet, hh+"/shopping/handoffs?week=2026-W38&status=done&limit=5", "", viewerUser); rec.Code != 200 || len(body["items"].([]any)) != 1 {
		t.Errorf("list handoffs = %d %v", rec.Code, body)
	}
	if rec, _ = do(http.MethodPost, hh+"/shopping/handoffs/"+id+"/confirm", `{}`, testUser); rec.Code != 400 {
		t.Errorf("empty confirm = %d", rec.Code)
	}
}
