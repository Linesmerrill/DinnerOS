package pantry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

func newTestService(t *testing.T, catalogNames ...string) (*Service, *memoryStore, *fakeCatalog) {
	t.Helper()
	store, catalog := newMemoryStore(), newFakeCatalog(catalogNames...)
	svc := NewService(store, catalog)
	svc.now = func() time.Time { return testNow }
	return svc, store, catalog
}

func mustAdd(t *testing.T, svc *Service, householdID string, in AddInput) Item {
	t.Helper()
	item, _, err := svc.Add(context.Background(), member(householdID), in)
	if err != nil {
		t.Fatalf("Add(%+v) error = %v", in, err)
	}
	return item
}

func wantValidation(t *testing.T, err error, contains string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v, want a ValidationError containing %q", err, contains)
	}
	if !strings.Contains(ve.Message, contains) {
		t.Errorf("message = %q, want it to contain %q", ve.Message, contains)
	}
}

func displayNames(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.DisplayName)
	}
	return out
}

func TestAddFreeTextLinksToCatalog(t *testing.T) {
	svc, _, catalog := newTestService(t, "Olive Oil")
	ctx := context.Background()

	item, created, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "  olive OIL "})
	want := Item{
		ID: item.ID, HouseholdID: testHousehold, IngredientID: catalog.id("Olive Oil"), Key: "olive oil",
		DisplayName: "olive OIL", Category: "pantry", Status: StatusInStock, Version: 1,
		CreatedAt: testNow, UpdatedBy: testUser, UpdatedAt: testNow,
	}
	if err != nil || !created || !reflect.DeepEqual(item, want) {
		t.Fatalf("Add() = %+v, %v, %v\nwant %+v", item, created, err, want)
	}

	// Not in the catalog: categorized by rule, "2" with no unit is a count.
	zorb := mustAdd(t, svc, testHousehold, AddInput{Name: "Zorblax Root", Quantity: "2"})
	if zorb.IngredientID != "" || zorb.Key != "zorblax root" || zorb.Category != "other" || zorb.Quantity != "2" || zorb.Unit != "count" {
		t.Errorf("zorblax = %+v", zorb)
	}
	// An explicit category wins.
	if topping := mustAdd(t, svc, testHousehold, AddInput{Name: "Crunchy Topping", Category: " Condiments "}); topping.Category != "condiments" {
		t.Errorf("topping category = %q, want condiments", topping.Category)
	}
}

func TestAddByIngredientID(t *testing.T) {
	svc, _, catalog := newTestService(t, "Olive Oil", "Jalapeño")
	oilID := catalog.id("Olive Oil")

	item := mustAdd(t, svc, testHousehold, AddInput{IngredientID: oilID, Quantity: "1 1/2", Unit: "cup", ExpiresOn: "2027-03-01", Note: "  big tin ", IsStaple: ptr(true)})
	if item.IngredientID != oilID || item.DisplayName != "Olive Oil" || item.Key != "olive oil" || item.Category != "pantry" ||
		item.Quantity != "3/2" || item.Unit != "cup" || item.ExpiresOn != "2027-03-01" || item.Note != "big tin" || !item.IsStaple {
		t.Errorf("item = %+v", item)
	}
	// A name overrides the catalog name for display; the key stays the catalog's.
	pepper := mustAdd(t, svc, testHousehold, AddInput{IngredientID: catalog.id("Jalapeño"), Name: "Hot peppers"})
	if pepper.DisplayName != "Hot peppers" || pepper.Key != "jalapeno" || pepper.Category != "produce" {
		t.Errorf("pepper = %+v", pepper)
	}
	_, _, err := svc.Add(context.Background(), member(testHousehold), AddInput{IngredientID: "ffffffffffffffffffffffff", Name: "Ghost"})
	wantValidation(t, err, "ingredientId does not match")
}

func TestAddMergesIntoExistingItem(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	actor := member(testHousehold)

	butter := mustAdd(t, svc, testHousehold, AddInput{Name: "Butter", Quantity: "2", Unit: "tbsp", IsStaple: ptr(true), Note: "salted"})

	merged, created, err := svc.Add(ctx, actor, AddInput{Name: "BUTTER", Status: StatusLow})
	if err != nil || created || merged.ID != butter.ID || merged.Version != 2 {
		t.Fatalf("merge = %+v, created %v, %v", merged, created, err)
	}
	if merged.Status != StatusLow || merged.Quantity != "2" || merged.Unit != "tbsp" || !merged.IsStaple || merged.Note != "salted" || merged.DisplayName != "Butter" {
		t.Errorf("omitted fields must keep their values: %+v", merged)
	}

	if out, _, _ := svc.Add(ctx, actor, AddInput{Name: "butter", Status: StatusOut}); out.Status != StatusOut || out.Quantity != "" || out.Unit != "" {
		t.Errorf("merge as out = %+v, want the amount cleared", out)
	}
	restocked, _, err := svc.Add(ctx, actor, AddInput{Name: "butter", Quantity: "1", Unit: "lb", IsStaple: ptr(false), Category: "dairy-eggs"})
	if err != nil || restocked.Status != StatusInStock || restocked.Quantity != "1" || restocked.Unit != "lb" || restocked.IsStaple {
		t.Errorf("restock = %+v, %v; status defaults to in_stock", restocked, err)
	}
	if n, _ := store.CountItems(ctx, testHousehold); n != 1 {
		t.Errorf("items = %d, want 1", n)
	}
	// The same name in another household is a separate item.
	if other := mustAdd(t, svc, otherHousehold, AddInput{Name: "Butter"}); other.ID == butter.ID || other.Version != 1 {
		t.Errorf("other household's butter = %+v", other)
	}
}

func TestAddRetriesWhenAConcurrentAddWins(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	// Another member's add lands between our lookup and insert: simulate by
	// inserting directly, then adding through a store that reports the key as
	// free on the first lookup.
	racing := &racingStore{memoryStore: store}
	svc.store = racing
	racing.onFirstFind = func() {
		_, _ = store.InsertItem(ctx, Item{HouseholdID: testHousehold, Key: "rice", DisplayName: "Rice", Category: "pantry", Status: StatusLow, CreatedAt: testNow, UpdatedBy: testUser, UpdatedAt: testNow})
	}
	item, created, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "rice", Note: "basmati"})
	if err != nil || created || item.Status != StatusInStock || item.Note != "basmati" || item.DisplayName != "Rice" {
		t.Errorf("Add() = %+v, created %v, %v; want a merge into the concurrent insert", item, created, err)
	}
}

// racingStore hides existing items from the first FindItemsByKeys call, after
// running onFirstFind.
type racingStore struct {
	*memoryStore
	onFirstFind func()
}

func (r *racingStore) FindItemsByKeys(ctx context.Context, householdID string, keys []string) ([]Item, error) {
	if f := r.onFirstFind; f != nil {
		r.onFirstFind = nil
		f()
		return nil, nil
	}
	return r.memoryStore.FindItemsByKeys(ctx, householdID, keys)
}

func TestAddValidation(t *testing.T) {
	svc, store, _ := newTestService(t)
	tests := []struct {
		name string
		in   AddInput
		want string
	}{
		{"no name or id", AddInput{}, "name or ingredientId is required"},
		{"blank name", AddInput{Name: "   "}, "name or ingredientId is required"},
		{"punctuation only", AddInput{Name: "!!!"}, "letters or digits"},
		{"long name", AddInput{Name: strings.Repeat("a", MaxNameLength+1)}, "at most 100"},
		{"unknown unit", AddInput{Name: "Rice", Quantity: "1", Unit: "dollop"}, `unit "dollop" is not a DinnerOS unit code`},
		{"negative quantity", AddInput{Name: "Rice", Quantity: "-1", Unit: "cup"}, "positive amount"},
		{"zero quantity", AddInput{Name: "Rice", Quantity: "0", Unit: "cup"}, "positive amount"},
		{"text quantity", AddInput{Name: "Rice", Quantity: "some", Unit: "cup"}, "positive amount"},
		{"zero denominator", AddInput{Name: "Rice", Quantity: "1/0", Unit: "cup"}, "positive amount"},
		{"long quantity", AddInput{Name: "Rice", Quantity: strings.Repeat("1", 40)}, "positive amount"},
		{"unit without quantity", AddInput{Name: "Rice", Unit: "cup"}, "unit requires a quantity"},
		{"bad status", AddInput{Name: "Rice", Status: "plenty"}, "status must be"},
		{"bad category", AddInput{Name: "Rice", Category: "snacks"}, "category must be one of"},
		{"bad date", AddInput{Name: "Rice", ExpiresOn: "2026-13-01"}, "YYYY-MM-DD"},
		{"US date", AddInput{Name: "Rice", ExpiresOn: "10/01/2026"}, "YYYY-MM-DD"},
		{"out with quantity", AddInput{Name: "Rice", Status: StatusOut, Quantity: "1"}, "out can't have a quantity"},
		{"long note", AddInput{Name: "Rice", Note: strings.Repeat("n", MaxNoteLength+1)}, "note must be at most"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.Add(context.Background(), member(testHousehold), tt.in)
			wantValidation(t, err, tt.want)
		})
	}
	if n, _ := store.CountItems(context.Background(), testHousehold); n != 0 {
		t.Errorf("invalid adds wrote %d items", n)
	}
}

func TestWritesRequirePantryEdit(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	item := mustAdd(t, svc, testHousehold, AddInput{Name: "Rice"})
	noEdit := households.Membership{HouseholdID: testHousehold, UserID: testUser, Role: "viewer"} // unknown roles grant nothing

	checks := map[string]error{}
	_, _, checks["Add"] = svc.Add(ctx, noEdit, AddInput{Name: "Beans"})
	_, checks["Update"] = svc.Update(ctx, noEdit, item.ID, UpdateInput{Note: ptr("x")})
	checks["Delete"] = svc.Delete(ctx, noEdit, item.ID)
	_, checks["SetStatuses"] = svc.SetStatuses(ctx, noEdit, []StatusUpdate{{ItemID: item.ID, Status: StatusOut}})
	_, checks["AddDefaultStaples"] = svc.AddDefaultStaples(ctx, noEdit)
	for name, err := range checks {
		if !errors.Is(err, ErrForbidden) {
			t.Errorf("%s error = %v, want ErrForbidden", name, err)
		}
	}
	if got, _ := store.GetItem(ctx, testHousehold, item.ID); !reflect.DeepEqual(got, item) {
		t.Errorf("item changed without permission: %+v", got)
	}
	if _, _, err := svc.Add(ctx, households.Membership{Role: households.RoleAdmin}, AddInput{Name: "Beans"}); err == nil {
		t.Error("Add without a household succeeded")
	}
	if _, err := svc.List(ctx, "", ListQuery{}); err == nil {
		t.Error("List without a household succeeded")
	}
}

func TestUpdate(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	item := mustAdd(t, svc, testHousehold, AddInput{Name: "Rice", Quantity: "2", Unit: "cup", Note: "jasmine", ExpiresOn: "2027-01-01"})
	update := func(in UpdateInput) (Item, error) {
		return svc.Update(ctx, member(testHousehold), item.ID, in)
	}

	got, err := update(UpdateInput{DisplayName: ptr(" Jasmine Rice "), Category: ptr("Pantry"), Quantity: ptr("1/2"), IsStaple: ptr(true)})
	if err != nil || got.DisplayName != "Jasmine Rice" || got.Category != "pantry" || got.Quantity != "1/2" || got.Unit != "cup" || !got.IsStaple || got.Key != "rice" || got.Version != 2 {
		t.Fatalf("update = %+v, %v", got, err)
	}
	steps := []struct {
		name  string
		in    UpdateInput
		check func(Item) bool
	}{
		{"unit only", UpdateInput{Unit: ptr("lb")}, func(i Item) bool { return i.Quantity == "1/2" && i.Unit == "lb" }},
		{"clear quantity", UpdateInput{Quantity: ptr("")}, func(i Item) bool { return i.Quantity == "" && i.Unit == "" }},
		{"quantity without unit is a count", UpdateInput{Quantity: ptr("3")}, func(i Item) bool { return i.Quantity == "3" && i.Unit == "count" }},
		{"out clears the amount", UpdateInput{Status: ptr(StatusOut)}, func(i Item) bool { return i.Status == StatusOut && i.Quantity == "" && i.Unit == "" }},
		{"restock with an amount; clear date and note", UpdateInput{Status: ptr(StatusInStock), Quantity: ptr("1"), Unit: ptr("kg"), ExpiresOn: ptr(""), Note: ptr("")},
			func(i Item) bool {
				return i.Status == StatusInStock && i.Quantity == "1" && i.Unit == "kg" && i.ExpiresOn == "" && i.Note == ""
			}},
	}
	for _, step := range steps {
		got, err := update(step.in)
		if err != nil || !step.check(got) {
			t.Fatalf("%s: %+v, %v", step.name, got, err)
		}
	}
	before, _ := store.GetItem(ctx, testHousehold, item.ID)

	for _, tt := range []struct {
		name string
		in   UpdateInput
		want string
	}{
		{"unit without quantity", UpdateInput{Quantity: ptr(""), Unit: ptr("cup")}, "unit requires a quantity"},
		{"unknown unit", UpdateInput{Unit: ptr("dollop")}, "not a DinnerOS unit code"},
		{"negative quantity", UpdateInput{Quantity: ptr("-1")}, "positive amount"},
		{"bad status", UpdateInput{Status: ptr(Status("gone"))}, "status must be"},
		{"out with quantity", UpdateInput{Status: ptr(StatusOut), Quantity: ptr("2")}, "out can't have a quantity"},
		{"blank name", UpdateInput{DisplayName: ptr(" ")}, "name is required"},
		{"bad category", UpdateInput{Category: ptr("snacks")}, "category must be one of"},
		{"bad date", UpdateInput{ExpiresOn: ptr("tomorrow")}, "YYYY-MM-DD"},
	} {
		_, err := update(tt.in)
		wantValidation(t, err, tt.want)
	}
	if after, _ := store.GetItem(ctx, testHousehold, item.ID); !reflect.DeepEqual(after, before) {
		t.Errorf("invalid updates changed the item: %+v", after)
	}

	if _, err := svc.Update(ctx, member(otherHousehold), item.ID, UpdateInput{Note: ptr("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update(other household) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Update(ctx, member(testHousehold), "not-an-id", UpdateInput{Note: ptr("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update(malformed) error = %v, want ErrNotFound", err)
	}
}

func TestUpdateRetriesAfterConcurrentChange(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	item := mustAdd(t, svc, testHousehold, AddInput{Name: "Milk"})

	calls := 0
	store.beforeUpdate = func() {
		calls++
		if calls == 1 { // another member marks it low between our read and write
			_ = store.SetStatus(ctx, testHousehold, []string{item.ID}, StatusLow, testUser, testNow)
		}
	}
	got, err := svc.Update(ctx, member(testHousehold), item.ID, UpdateInput{Note: ptr("oat")})
	if err != nil || got.Note != "oat" || got.Status != StatusLow || got.Version != 3 || calls != 2 {
		t.Errorf("Update() = %+v, %v after %d attempts; want the concurrent status kept", got, err, calls)
	}

	store.beforeUpdate = func() { _ = store.SetStatus(ctx, testHousehold, []string{item.ID}, StatusOut, testUser, testNow) }
	if _, err := svc.Update(ctx, member(testHousehold), item.ID, UpdateInput{Note: ptr("again")}); !errors.Is(err, ErrConflict) {
		t.Errorf("Update() under constant contention error = %v, want ErrConflict", err)
	}
}

func TestSetStatuses(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	eggs := mustAdd(t, svc, testHousehold, AddInput{Name: "Eggs", Quantity: "12"})
	milk := mustAdd(t, svc, testHousehold, AddInput{Name: "Milk"})
	bread := mustAdd(t, svc, testHousehold, AddInput{Name: "Bread", Status: StatusLow})
	theirs := mustAdd(t, svc, otherHousehold, AddInput{Name: "Cheese"})
	const ghost = "ffffffffffffffffffffffff"

	res, err := svc.SetStatuses(ctx, member(testHousehold), []StatusUpdate{
		{ItemID: eggs.ID, Status: StatusOut},
		{ItemID: ghost, Status: StatusInStock},
		{ItemID: milk.ID, Status: StatusLow},
		{ItemID: theirs.ID, Status: StatusOut},
		{ItemID: bread.ID, Status: StatusInStock},
	})
	if err != nil {
		t.Fatalf("SetStatuses() error = %v", err)
	}
	var got []string
	for _, it := range res.Items {
		got = append(got, fmt.Sprintf("%s=%s", it.DisplayName, it.Status))
	}
	if want := []string{"Eggs=out", "Milk=low", "Bread=in_stock"}; !slices.Equal(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
	if !slices.Equal(res.Missing, []string{ghost, theirs.ID}) {
		t.Errorf("missing = %v", res.Missing)
	}
	if e := res.Items[0]; e.Quantity != "" || e.Unit != "" || e.Version != 2 || e.UpdatedBy != testUser {
		t.Errorf("eggs = %+v, want amount cleared and version bumped", e)
	}
	if c, _ := store.GetItem(ctx, otherHousehold, theirs.ID); c.Status != StatusInStock {
		t.Errorf("other household's item = %+v", c)
	}

	many := make([]StatusUpdate, MaxBulkUpdates+1)
	for i := range many {
		many[i] = StatusUpdate{ItemID: fmt.Sprintf("%024x", i), Status: StatusOut}
	}
	for _, tt := range []struct {
		name    string
		updates []StatusUpdate
		want    string
	}{
		{"empty", nil, "must not be empty"},
		{"too many", many, "at most 200"},
		{"blank id", []StatusUpdate{{ItemID: " ", Status: StatusOut}}, "items[0].id is required"},
		{"repeated id", []StatusUpdate{{ItemID: eggs.ID, Status: StatusOut}, {ItemID: eggs.ID, Status: StatusLow}}, "items[1].id repeats"},
		{"bad status", []StatusUpdate{{ItemID: eggs.ID, Status: "gone"}}, "items[0].status"},
	} {
		_, err := svc.SetStatuses(ctx, member(testHousehold), tt.updates)
		wantValidation(t, err, tt.want)
	}
}

func TestAddDefaultStaples(t *testing.T) {
	svc, store, catalog := newTestService(t, "Pepper", "Olive Oil", "Salt")
	ctx := context.Background()
	butter := mustAdd(t, svc, testHousehold, AddInput{Name: "Butter", Status: StatusLow})

	res, err := svc.AddDefaultStaples(ctx, member(testHousehold))
	if err != nil {
		t.Fatalf("AddDefaultStaples() error = %v", err)
	}
	wantNames := slices.DeleteFunc(DefaultStapleNames(), func(n string) bool { return n == "Butter" })
	if !slices.Equal(displayNames(res.Added), wantNames) || res.Skipped != 1 {
		t.Fatalf("added %v, skipped %d; want %v, 1", displayNames(res.Added), res.Skipped, wantNames)
	}
	byName := map[string]Item{}
	for _, it := range res.Added {
		byName[it.DisplayName] = it
		if !it.IsStaple || it.Status != StatusInStock || it.UpdatedBy != testUser || it.Quantity != "" {
			t.Errorf("staple %s = %+v", it.DisplayName, it)
		}
	}
	// "Black Pepper" links to the catalog's "Pepper" through its alias.
	if p := byName["Black Pepper"]; p.Key != "pepper" || p.IngredientID != catalog.id("Pepper") || p.Category != "spices" {
		t.Errorf("black pepper = %+v", p)
	}
	if o := byName["Olive Oil"]; o.IngredientID != catalog.id("Olive Oil") || o.Key != "olive oil" || o.Category != "pantry" {
		t.Errorf("olive oil = %+v", o)
	}
	if c := byName["Cooking Oil"]; c.IngredientID != "" || c.Key != "cooking oil" || c.Category != "pantry" {
		t.Errorf("cooking oil = %+v", c)
	}
	if b, _ := store.GetItem(ctx, testHousehold, butter.ID); !reflect.DeepEqual(b, butter) {
		t.Errorf("existing butter was changed: %+v", b)
	}

	again, err := svc.AddDefaultStaples(ctx, member(testHousehold))
	if err != nil || len(again.Added) != 0 || again.Skipped != len(defaultStaples) {
		t.Errorf("second call = %+v, %v; want nothing added", again, err)
	}
	if n, _ := store.CountItems(ctx, testHousehold); n != len(defaultStaples) {
		t.Errorf("items = %d, want %d", n, len(defaultStaples))
	}

	// An alias already in the pantry counts as having the staple.
	mustAdd(t, svc, otherHousehold, AddInput{Name: "Vegetable Oil"})
	other, err := svc.AddDefaultStaples(ctx, member(otherHousehold))
	if err != nil || other.Skipped != 1 || slices.Contains(displayNames(other.Added), "Cooking Oil") {
		t.Errorf("other household = added %v, skipped %d, %v", displayNames(other.Added), other.Skipped, err)
	}
}

func TestListFiltersAndOrder(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	mustAdd(t, svc, testHousehold, AddInput{Name: "Salt", IsStaple: ptr(true)})
	mustAdd(t, svc, testHousehold, AddInput{Name: "Jalapeño", Status: StatusLow})
	mustAdd(t, svc, testHousehold, AddInput{Name: "olive oil", IsStaple: ptr(true)})
	mustAdd(t, svc, testHousehold, AddInput{Name: "Butter", Status: StatusOut})
	mustAdd(t, svc, testHousehold, AddInput{Name: "Apple"})
	mustAdd(t, svc, otherHousehold, AddInput{Name: "Apricot"})

	for _, tt := range []struct {
		name string
		q    ListQuery
		want []string
	}{
		{"aisle order, then name", ListQuery{}, []string{"Apple", "Jalapeño", "Butter", "olive oil", "Salt"}},
		{"status", ListQuery{Status: "low"}, []string{"Jalapeño"}},
		{"category", ListQuery{Category: " Produce"}, []string{"Apple", "Jalapeño"}},
		{"staples", ListQuery{Staple: ptr(true)}, []string{"olive oil", "Salt"}},
		{"search name", ListQuery{Search: "OIL"}, []string{"olive oil"}},
		{"search ignores accents through the key", ListQuery{Search: "jalapeno"}, []string{"Jalapeño"}},
		{"search is literal", ListQuery{Search: "a.p"}, []string{}},
		{"search stays in the household", ListQuery{Search: "ap"}, []string{"Apple", "Jalapeño"}},
	} {
		items, err := svc.List(ctx, testHousehold, tt.q)
		if err != nil || !slices.Equal(displayNames(items), tt.want) {
			t.Errorf("List(%s) = %v, %v; want %v", tt.name, displayNames(items), err, tt.want)
		}
	}
	for _, q := range []ListQuery{{Status: "gone"}, {Category: "snacks"}, {Search: strings.Repeat("a", 101)}} {
		_, err := svc.List(ctx, testHousehold, q)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("List(%+v) error = %v, want ValidationError", q, err)
		}
	}
}

func TestDelete(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	item := mustAdd(t, svc, testHousehold, AddInput{Name: "Rice"})
	if err := svc.Delete(ctx, member(otherHousehold), item.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(other household) error = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, member(testHousehold), item.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := svc.Delete(ctx, member(testHousehold), item.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete() error = %v, want ErrNotFound", err)
	}
}

func TestPantryCapacity(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	for i := range MaxItems {
		if _, err := store.InsertItem(ctx, Item{HouseholdID: testHousehold, Key: fmt.Sprintf("item %d", i), DisplayName: "Item", Category: "other", Status: StatusInStock}); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "One Too Many"})
	wantValidation(t, err, "at most 1000 items")
	_, err = svc.AddDefaultStaples(ctx, member(testHousehold))
	wantValidation(t, err, "at most 1000 items")
	// Merging into an existing item still works when full.
	if _, created, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "Item 7", Status: StatusLow}); err != nil || created {
		t.Errorf("merge when full = created %v, %v", created, err)
	}
}
