package shopping

import (
	"context"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
)

func TestStillRelevantStopsAPushOnceTheWeekIsOrdered(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newOrderService(t, "wed", denverNoon(t, 16))
	due := notifications.Notification{
		HouseholdID: testHousehold, Type: notifications.TypeShoppingOrderDue,
		Subject: notifications.Subject{Kind: notifications.SubjectShoppingWeek, ID: testWeek},
	}
	if ok, err := svc.StillRelevant(ctx, due); err != nil || !ok {
		t.Fatalf("StillRelevant(unordered) = %v, %v; want true", ok, err)
	}
	if _, err := svc.SetWeekOrdered(ctx, memberOf(testUser), testWeek, true); err != nil {
		t.Fatal(err)
	}
	if ok, err := svc.StillRelevant(ctx, due); err != nil || ok {
		t.Errorf("StillRelevant(ordered) = %v, %v; want false", ok, err)
	}
	// Other types aren't shopping's to judge.
	if ok, _ := svc.StillRelevant(ctx, notifications.Notification{Type: notifications.TypePantryLow}); !ok {
		t.Error("StillRelevant(pantry.low) = false")
	}
}
