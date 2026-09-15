package pantry

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestMemoryStoreContract(t *testing.T) {
	runStoreContract(t, newMemoryStore())
}

func itemKeys(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Key)
	}
	slices.Sort(out)
	return out
}

func itemIDs(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	slices.Sort(out)
	return out
}

func sortedStrings(values ...string) []string {
	slices.Sort(values)
	return values
}

// runStoreContract checks the Store behavior the service relies on. Both
// memoryStore and MongoStore must pass it.
func runStoreContract(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	at := testNow

	// Insert and read back every field.
	rice := Item{
		HouseholdID: testHousehold, IngredientID: "cafe00000000000000000001", Key: "rice", DisplayName: "Rice", Category: "pantry",
		Quantity: "3/2", Unit: "cup", Status: StatusInStock, IsStaple: true, ExpiresOn: "2027-01-01", Note: "jasmine",
		CreatedAt: at, UpdatedBy: testUser, UpdatedAt: at,
	}
	saved, err := store.InsertItem(ctx, rice)
	if err != nil || saved.ID == "" {
		t.Fatalf("InsertItem() = %+v, %v", saved, err)
	}
	want := rice
	want.ID, want.Version = saved.ID, 1
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("InsertItem() = %+v\nwant %+v", saved, want)
	}
	if got, err := store.GetItem(ctx, testHousehold, saved.ID); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("GetItem() = %+v, %v\nwant %+v", got, err, want)
	}

	// (householdId, key) is unique per household only.
	dup := rice
	dup.DisplayName = "Rice again"
	if _, err := store.InsertItem(ctx, dup); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate InsertItem() error = %v, want ErrDuplicate", err)
	}
	theirs := rice
	theirs.HouseholdID = otherHousehold
	theirsSaved, err := store.InsertItem(ctx, theirs)
	if err != nil {
		t.Fatalf("InsertItem(other household) error = %v", err)
	}
	milk, err := store.InsertItem(ctx, Item{HouseholdID: testHousehold, Key: "milk", DisplayName: "Milk", Category: "dairy-eggs", Status: StatusLow, CreatedAt: at, UpdatedBy: testUser, UpdatedAt: at})
	if err != nil {
		t.Fatal(err)
	}

	// Lookups are household-scoped.
	for _, id := range []string{saved.ID, "not-an-id", "ffffffffffffffffffffffff"} {
		if _, err := store.GetItem(ctx, otherHousehold, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetItem(other household, %q) error = %v, want ErrNotFound", id, err)
		}
	}
	if got, err := store.GetItems(ctx, testHousehold, []string{saved.ID, "not-an-id", theirsSaved.ID, milk.ID}); err != nil || !slices.Equal(itemIDs(got), sortedStrings(saved.ID, milk.ID)) {
		t.Errorf("GetItems() = %v, %v", itemIDs(got), err)
	}
	if got, err := store.FindItemsByKeys(ctx, testHousehold, []string{"rice", "bread"}); err != nil || len(got) != 1 || got[0].ID != saved.ID {
		t.Errorf("FindItemsByKeys() = %+v, %v", got, err)
	}
	if got, err := store.FindItemsByKeys(ctx, testHousehold, nil); err != nil || len(got) != 0 {
		t.Errorf("FindItemsByKeys(nil) = %+v, %v", got, err)
	}
	if n, err := store.CountItems(ctx, testHousehold); err != nil || n != 2 {
		t.Errorf("CountItems() = %d, %v; want 2", n, err)
	}

	// Updates clear optional fields and keep key and createdAt.
	next := want
	next.DisplayName, next.IngredientID, next.Quantity, next.Unit, next.ExpiresOn, next.Note = "Brown Rice", "", "", "", "", ""
	next.Key, next.CreatedAt, next.UpdatedAt = "changed", at.Add(time.Hour), at.Add(time.Minute)
	updated, err := store.UpdateItem(ctx, next)
	wantUpdated := next
	wantUpdated.Key, wantUpdated.CreatedAt, wantUpdated.Version = "rice", at, 2
	if err != nil || !reflect.DeepEqual(updated, wantUpdated) {
		t.Errorf("UpdateItem() = %+v, %v\nwant %+v", updated, err, wantUpdated)
	}
	if got, err := store.GetItem(ctx, testHousehold, saved.ID); err != nil || !reflect.DeepEqual(got, wantUpdated) {
		t.Errorf("GetItem(updated) = %+v, %v\nwant %+v", got, err, wantUpdated)
	}
	if _, err := store.UpdateItem(ctx, next); !errors.Is(err, ErrConflict) {
		t.Errorf("stale UpdateItem() error = %v, want ErrConflict", err)
	}
	for _, bad := range []Item{
		func() Item { i := updated; i.ID = "ffffffffffffffffffffffff"; return i }(),
		func() Item { i := updated; i.ID = "not-an-id"; return i }(),
		func() Item { i := updated; i.HouseholdID = otherHousehold; return i }(),
	} {
		if _, err := store.UpdateItem(ctx, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("UpdateItem(%s/%s) error = %v, want ErrNotFound", bad.HouseholdID, bad.ID, err)
		}
	}

	// Marking items out clears amounts and bumps versions, in this household only.
	withAmount := updated
	withAmount.Quantity, withAmount.Unit = "2", "lb"
	if withAmount, err = store.UpdateItem(ctx, withAmount); err != nil || withAmount.Version != 3 || withAmount.Quantity != "2" {
		t.Fatalf("UpdateItem(amount) = %+v, %v", withAmount, err)
	}
	later := at.Add(2 * time.Minute)
	if err := store.SetStatus(ctx, testHousehold, []string{saved.ID, milk.ID, theirsSaved.ID, "not-an-id"}, StatusOut, testUser, later); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}
	got, _ := store.GetItem(ctx, testHousehold, saved.ID)
	if got.Status != StatusOut || got.Quantity != "" || got.Unit != "" || got.Version != 4 || !got.UpdatedAt.Equal(later) || got.DisplayName != "Brown Rice" {
		t.Errorf("rice after SetStatus = %+v", got)
	}
	if got, _ := store.GetItem(ctx, testHousehold, milk.ID); got.Status != StatusOut || got.Version != 2 {
		t.Errorf("milk after SetStatus = %+v", got)
	}
	if got, _ := store.GetItem(ctx, otherHousehold, theirsSaved.ID); got.Status != StatusInStock || got.Version != 1 || got.Quantity != "3/2" {
		t.Errorf("other household's item changed: %+v", got)
	}
	if err := store.SetStatus(ctx, testHousehold, []string{milk.ID}, StatusLow, testUser, later); err != nil {
		t.Fatal(err)
	}

	// List filters.
	if _, err := store.InsertItem(ctx, Item{HouseholdID: testHousehold, Key: "salt", DisplayName: "Sea Salt", Category: "spices", Status: StatusInStock, IsStaple: true, CreatedAt: at, UpdatedBy: testUser, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		f    ListFilter
		want []string
	}{
		{"all", ListFilter{}, []string{"milk", "rice", "salt"}},
		{"status", ListFilter{Status: StatusLow}, []string{"milk"}},
		{"category", ListFilter{Category: "spices"}, []string{"salt"}},
		{"staple", ListFilter{Staple: ptr(true)}, []string{"rice", "salt"}},
		{"not staple", ListFilter{Staple: ptr(false)}, []string{"milk"}},
		{"name, case-insensitive", ListFilter{NamePattern: "SEA"}, []string{"salt"}},
		{"key", ListFilter{KeyPattern: "ric"}, []string{"rice"}},
		{"name or key", ListFilter{NamePattern: "brown", KeyPattern: "salt"}, []string{"rice", "salt"}},
		{"key is case-sensitive", ListFilter{KeyPattern: "RICE"}, []string{}},
		{"combined", ListFilter{Status: StatusOut, Staple: ptr(true), NamePattern: "rice"}, []string{"rice"}},
	} {
		items, err := store.ListItems(ctx, testHousehold, tt.f)
		if err != nil || !slices.Equal(itemKeys(items), tt.want) {
			t.Errorf("ListItems(%s) = %v, %v; want %v", tt.name, itemKeys(items), err, tt.want)
		}
	}

	// Delete.
	if err := store.DeleteItem(ctx, testHousehold, saved.ID); err != nil {
		t.Fatalf("DeleteItem() error = %v", err)
	}
	for _, id := range []string{saved.ID, theirsSaved.ID, "not-an-id"} {
		if err := store.DeleteItem(ctx, testHousehold, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteItem(%q) error = %v, want ErrNotFound", id, err)
		}
	}
	if n, _ := store.CountItems(ctx, testHousehold); n != 2 {
		t.Errorf("CountItems() after delete = %d, want 2", n)
	}
}
