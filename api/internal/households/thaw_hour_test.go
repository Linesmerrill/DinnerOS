package households

import (
	"context"
	"errors"
	"testing"
)

func TestUpdateHouseholdThawReminderHour(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	h, admin := newHousehold(t, svc, nil)

	if h.ThawReminderHour != nil {
		t.Errorf("new household ThawReminderHour = %v, want none", h.ThawReminderHour)
	}
	if got := h.ThawHour(); got != DefaultThawReminderHour {
		t.Errorf("ThawHour() = %d, want the default %d", got, DefaultThawReminderHour)
	}

	updated, err := svc.Update(ctx, admin, UpdateInput{SetThawReminderHour: true, ThawReminderHour: ptr(5)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ThawHour() != 5 {
		t.Fatalf("ThawHour() = %d, want 5", updated.ThawHour())
	}
	// Setting the hour alone leaves the rest of the household alone.
	if updated.Name != h.Name || updated.TimeZone != h.TimeZone || updated.OrderDay != h.OrderDay {
		t.Errorf("Update changed more than the thaw hour: %+v", updated)
	}

	// Null returns the household to the default, rather than meaning midnight.
	cleared, err := svc.Update(ctx, admin, UpdateInput{SetThawReminderHour: true})
	if err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if cleared.ThawReminderHour != nil || cleared.ThawHour() != DefaultThawReminderHour {
		t.Errorf("cleared = %v (hour %d), want the default", cleared.ThawReminderHour, cleared.ThawHour())
	}

	// Midnight is a real choice; 24 is not an hour.
	if _, err := svc.Update(ctx, admin, UpdateInput{SetThawReminderHour: true, ThawReminderHour: ptr(0)}); err != nil {
		t.Errorf("hour 0: %v, want it accepted", err)
	}
	var ve *ValidationError
	_, err = svc.Update(ctx, admin, UpdateInput{SetThawReminderHour: true, ThawReminderHour: ptr(24)})
	if !errors.As(err, &ve) {
		t.Errorf("hour 24 = %v, want a ValidationError", err)
	}
}
