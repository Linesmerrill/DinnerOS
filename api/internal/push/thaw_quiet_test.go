package push

import (
	"context"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
)

func thawDue(id, householdID string, createdAt time.Time) notifications.Notification {
	return notifications.Notification{
		ID: id, HouseholdID: householdID, Type: notifications.TypePantryThaw,
		Title:     "Take Chicken Thighs out to thaw",
		Body:      "This usually takes about 5 hours in the fridge — put it in the fridge by 1 PM.",
		Subject:   notifications.Subject{Kind: notifications.SubjectPantryItem, ID: "item1"},
		DedupeKey: "pantry.thaw:2026-09-17:item1", CreatedAt: createdAt,
	}
}

// A household that asked for its thaw reminder at 6am gets it at 6am. Quiet
// hours protect people from pushes they did not ask for; this is one they
// scheduled themselves, and holding it until 8 would make the setting a lie.
func TestSweepDoesNotHoldThawRemindersForQuietHours(t *testing.T) {
	f := newSweepFixture()
	f.now = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC) // 06:00 in Denver
	f.outbox.produce[hhA] = []notifications.Notification{
		thawDue("thaw", hhA, f.now),
		orderDue("order", hhA, f.now),
	}
	report, err := f.sweeper(f.sender).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Deferred != 1 {
		t.Errorf("Deferred = %d, want 1: the order reminder still waits for 8am", report.Deferred)
	}
	if report.Sent != 1 {
		t.Errorf("Sent = %d, want 1: the thaw reminder goes out now", report.Sent)
	}
	if f.outbox.status("thaw") != notifications.PushSent {
		t.Errorf("thaw status = %s, want %s", f.outbox.status("thaw"), notifications.PushSent)
	}
	if f.outbox.status("order") != notifications.PushPending {
		t.Errorf("order status = %s, want %s", f.outbox.status("order"), notifications.PushPending)
	}
}

// Exemption is not immunity: a thaw reminder nobody saw for a day is no
// longer news, and is skipped like any other stale notification.
func TestSweepStillSkipsStaleThawReminders(t *testing.T) {
	f := newSweepFixture()
	f.now = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	f.outbox.produce[hhA] = []notifications.Notification{thawDue("old", hhA, f.now.Add(-25*time.Hour))}
	if _, err := f.sweeper(f.sender).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.outbox.status("old") != notifications.PushSkipped {
		t.Errorf("status = %s, want %s", f.outbox.status("old"), notifications.PushSkipped)
	}
}
