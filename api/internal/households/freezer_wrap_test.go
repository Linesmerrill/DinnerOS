package households

import (
	"context"
	"errors"
	"testing"
)

func TestUpdateHouseholdFreezerWrap(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	h, admin := newHousehold(t, svc, nil)
	if h.Wrap() != FreezerWrapVacuum {
		t.Errorf("new household Wrap() = %q, want vacuum sealed", h.Wrap())
	}
	bag := FreezerWrapBag
	updated, err := svc.Update(ctx, admin, UpdateInput{FreezerWrap: &bag})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Wrap() != FreezerWrapBag || updated.Name != h.Name {
		t.Errorf("updated = %+v, want only the wrap changed", updated)
	}
	bad := "foil"
	var ve *ValidationError
	if _, err := svc.Update(ctx, admin, UpdateInput{FreezerWrap: &bad}); !errors.As(err, &ve) {
		t.Errorf("foil: err = %v, want invalid input", err)
	}
}
