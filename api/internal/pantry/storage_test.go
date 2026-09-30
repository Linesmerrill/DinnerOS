package pantry

import (
	"context"
	"testing"
)

// fakeShelfLife dates everything by storage, so tests can see which was used.
type fakeShelfLife struct{ calls []string }

func (f *fakeShelfLife) BestBy(_ context.Context, _, name, _, storage, storedOn string) (string, error) {
	f.calls = append(f.calls, name+"@"+storage+"@"+storedOn)
	switch storage {
	case "freezer":
		return "2027-01-29", nil
	case "fridge":
		return "2026-10-14", nil
	}
	return "2027-03-30", nil
}

func TestAddToTheFridgeOrFreezerGetsABestByDate(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	shelf := &fakeShelfLife{}
	svc.SetShelfLife(shelf)

	carrots, _, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "Carrots", Storage: StorageFridge, StoredOn: "2026-09-30"})
	if err != nil {
		t.Fatal(err)
	}
	if carrots.Storage != StorageFridge || carrots.StoredOn != "2026-09-30" || carrots.ExpiresOn != "2026-10-14" {
		t.Errorf("carrots = storage %q stored %q expires %q", carrots.Storage, carrots.StoredOn, carrots.ExpiresOn)
	}

	pork, _, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "Ground Pork", Quantity: "10", Unit: "oz", Storage: StorageFreezer, StoredOn: "2026-09-29"})
	if err != nil {
		t.Fatal(err)
	}
	if pork.Storage != StorageFreezer || pork.FrozenOn != "2026-09-29" || pork.ExpiresOn != "2027-01-29" {
		t.Errorf("pork = storage %q frozen %q expires %q", pork.Storage, pork.FrozenOn, pork.ExpiresOn)
	}

	// A date the member typed wins.
	milk, _, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "Milk", Storage: StorageFridge, ExpiresOn: "2026-10-05"})
	if err != nil || milk.ExpiresOn != "2026-10-05" {
		t.Errorf("milk = %q, %v; want the typed date", milk.ExpiresOn, err)
	}
	// No storage said: nothing is guessed.
	salt, _, _ := svc.Add(ctx, member(testHousehold), AddInput{Name: "Salt"})
	if salt.ExpiresOn != "" {
		t.Errorf("salt got a date %q with no storage given", salt.ExpiresOn)
	}
}

func TestMovingAnItemIntoTheFreezerRedatesIt(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	svc.SetShelfLife(&fakeShelfLife{})
	item, _, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "Chicken Breasts", Storage: StorageFridge, StoredOn: "2026-09-28"})
	if err != nil {
		t.Fatal(err)
	}
	freezer, on := StorageFreezer, "2026-09-29"
	moved, err := svc.Update(ctx, member(testHousehold), item.ID, UpdateInput{Storage: &freezer, StoredOn: &on})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Storage != StorageFreezer || moved.FrozenOn != "2026-09-29" || moved.ExpiresOn != "2027-01-29" {
		t.Errorf("moved = storage %q frozen %q expires %q", moved.Storage, moved.FrozenOn, moved.ExpiresOn)
	}
	bad := Storage("shelf")
	if _, err := svc.Update(ctx, member(testHousehold), item.ID, UpdateInput{Storage: &bad}); err == nil {
		t.Error("an unknown storage was accepted")
	}
}

func TestClearingAnOwnDateGetsTheRecommendedOne(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	svc.SetShelfLife(&fakeShelfLife{})
	item, _, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "Yogurt", Storage: StorageFridge, ExpiresOn: "2026-10-20"})
	if err != nil {
		t.Fatal(err)
	}
	fridge, on, clear := StorageFridge, "2026-09-30", ""
	back, err := svc.Update(ctx, member(testHousehold), item.ID, UpdateInput{ExpiresOn: &clear, Storage: &fridge, StoredOn: &on})
	if err != nil || back.ExpiresOn != "2026-10-14" {
		t.Errorf("cleared date = %q, %v; want the recommended 2026-10-14", back.ExpiresOn, err)
	}
}
