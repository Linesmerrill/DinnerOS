package shopping

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// The reminder touches three ordered-week methods and nothing else, so these
// tests use fakes instead of MongoDB. The embedded nil Store makes any other
// call panic loudly rather than pass quietly.

type orderStore struct {
	Store
	weeks map[string]OrderedWeek
}

func newOrderStore() *orderStore { return &orderStore{weeks: map[string]OrderedWeek{}} }

func (s *orderStore) GetOrderedWeek(_ context.Context, householdID, week string) (OrderedWeek, error) {
	w, ok := s.weeks[householdID+"/"+week]
	if !ok {
		return OrderedWeek{}, ErrNotFound
	}
	return w, nil
}

// MarkWeekOrdered keeps the first marker, like the collection's upsert.
func (s *orderStore) MarkWeekOrdered(_ context.Context, w OrderedWeek) (OrderedWeek, error) {
	key := w.HouseholdID + "/" + w.Week
	if existing, ok := s.weeks[key]; ok {
		return existing, nil
	}
	w.ID = "ordered-" + w.Week
	s.weeks[key] = w
	return w, nil
}

func (s *orderStore) UnmarkWeekOrdered(_ context.Context, householdID, week string) error {
	key := householdID + "/" + week
	if _, ok := s.weeks[key]; !ok {
		return ErrNotFound
	}
	delete(s.weeks, key)
	return nil
}

type orderHouseholds struct{ household households.Household }

func (h orderHouseholds) GetHousehold(context.Context, string) (households.Household, error) {
	return h.household, nil
}

// orderNotifier records what the reminder created and marked read, and
// deduplicates like the real notification service.
type orderNotifier struct {
	created  []notifications.New
	readKeys []string
	seen     map[string]bool
}

func (n *orderNotifier) Create(_ context.Context, in notifications.New) (notifications.Notification, bool, error) {
	if n.seen == nil {
		n.seen = map[string]bool{}
	}
	if n.seen[in.DedupeKey] {
		return notifications.Notification{DedupeKey: in.DedupeKey}, false, nil
	}
	n.seen[in.DedupeKey] = true
	n.created = append(n.created, in)
	return notifications.Notification{DedupeKey: in.DedupeKey}, true, nil
}

func (n *orderNotifier) MarkReadByDedupeKey(_ context.Context, _ households.Membership, key string) error {
	n.readKeys = append(n.readKeys, key)
	return nil
}

// newOrderService builds a Service with only what the reminder needs.
func newOrderService(t *testing.T, orderDay string, now time.Time) (*Service, *orderStore, *orderNotifier) {
	t.Helper()
	store := newOrderStore()
	notifier := &orderNotifier{}
	svc := NewService(ServiceOptions{
		Store: store,
		Households: orderHouseholds{household: households.Household{
			ID: testHousehold, TimeZone: "America/Denver", OrderDay: orderDay,
		}},
		Notifier: notifier,
	})
	svc.now = func() time.Time { return now }
	return svc, store, notifier
}

// denverNoon is midday on September `day`, 2026 in the household's zone.
// 2026-W38 runs Monday the 14th through Sunday the 20th.
func denverNoon(t *testing.T, day int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return time.Date(2026, 9, day, 12, 0, 0, 0, loc)
}

const orderDueKey = "shopping.order_due:" + testWeek

func TestOrderReminderFollowsTheOrderDay(t *testing.T) {
	ctx := context.Background()
	// Wednesday of 2026-W38 is September 16.
	cases := []struct {
		name               string
		day                int
		wantDue, wantRemid bool
	}{
		{"the day before", 15, false, false},
		{"the order day itself", 16, true, true},
		{"later the same week", 19, true, true},
		{"after the week ended", 22, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newOrderService(t, "wed", denverNoon(t, tc.day))
			r, err := svc.OrderReminder(ctx, testHousehold, testWeek)
			if err != nil {
				t.Fatalf("OrderReminder() error = %v", err)
			}
			if r.Due != tc.wantDue || r.Remind != tc.wantRemid {
				t.Errorf("Due = %v, Remind = %v; want %v, %v", r.Due, r.Remind, tc.wantDue, tc.wantRemid)
			}
			if r.DueOn != "2026-09-16" || r.OrderDay != "wed" {
				t.Errorf("DueOn = %q, OrderDay = %q; want 2026-09-16, wed", r.DueOn, r.OrderDay)
			}
		})
	}
}

func TestOrderReminderOffWithoutAnOrderDay(t *testing.T) {
	svc, _, _ := newOrderService(t, "", denverNoon(t, 19))
	r, err := svc.OrderReminder(context.Background(), testHousehold, testWeek)
	if err != nil {
		t.Fatalf("OrderReminder() error = %v", err)
	}
	if r.Due || r.Remind || r.DueOn != "" || r.OrderDay != "" {
		t.Errorf("reminder = %+v; want it off", r)
	}
}

func TestSetWeekOrderedSilencesAndUndoes(t *testing.T) {
	ctx := context.Background()
	svc, store, notifier := newOrderService(t, "wed", denverNoon(t, 16))
	actor := memberOf(testUser)

	r, err := svc.SetWeekOrdered(ctx, actor, testWeek, true)
	if err != nil {
		t.Fatalf("SetWeekOrdered(true) error = %v", err)
	}
	if !r.Ordered || r.Remind || r.OrderedBy != testUser {
		t.Errorf("marked reminder = %+v; want ordered and quiet", r)
	}
	// Marking also clears the bell's copy for the member who acted.
	if len(notifier.readKeys) != 1 || notifier.readKeys[0] != orderDueKey {
		t.Errorf("read keys = %v; want [%s]", notifier.readKeys, orderDueKey)
	}

	// Marking twice keeps the first marker rather than rewriting it.
	if _, err := svc.SetWeekOrdered(ctx, actor, testWeek, true); err != nil {
		t.Fatalf("second SetWeekOrdered(true) error = %v", err)
	}
	if len(store.weeks) != 1 {
		t.Errorf("stored %d markers, want 1", len(store.weeks))
	}

	// A mis-tap is undoable, and the reminder comes straight back.
	back, err := svc.SetWeekOrdered(ctx, actor, testWeek, false)
	if err != nil {
		t.Fatalf("SetWeekOrdered(false) error = %v", err)
	}
	if back.Ordered || !back.Remind {
		t.Errorf("undone reminder = %+v; want unordered and reminding", back)
	}
	// Undoing a week that was never marked is not an error.
	if _, err := svc.SetWeekOrdered(ctx, actor, testWeek, false); err != nil {
		t.Errorf("redundant SetWeekOrdered(false) error = %v", err)
	}
}

func TestSetWeekOrderedChecksPermissionAndWeek(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newOrderService(t, "wed", denverNoon(t, 16))

	if _, err := svc.SetWeekOrdered(ctx, viewer(), testWeek, true); !errors.Is(err, ErrForbidden) {
		t.Errorf("SetWeekOrdered() by a viewer = %v, want ErrForbidden", err)
	}
	// The handler maps ErrInvalidWeek to 400.
	if _, err := svc.OrderReminder(ctx, testHousehold, "week-38"); !errors.Is(err, planning.ErrInvalidWeek) {
		t.Errorf("OrderReminder() with a malformed week = %v, want ErrInvalidWeek", err)
	}
	if _, err := svc.SetWeekOrdered(ctx, memberOf(testUser), "week-38", true); !errors.Is(err, planning.ErrInvalidWeek) {
		t.Errorf("SetWeekOrdered() with a malformed week = %v, want ErrInvalidWeek", err)
	}
}

func TestRefreshCreatesOneReminderPerWeek(t *testing.T) {
	ctx := context.Background()
	svc, _, notifier := newOrderService(t, "wed", denverNoon(t, 16))

	// Reading notifications all day must not nag.
	for range 3 {
		if err := svc.Refresh(ctx, testHousehold); err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
	}
	if len(notifier.created) != 1 {
		t.Fatalf("created %d notifications, want 1", len(notifier.created))
	}
	n := notifier.created[0]
	if n.Type != notifications.TypeShoppingOrderDue || n.DedupeKey != orderDueKey ||
		n.Subject.Kind != notifications.SubjectShoppingWeek || n.Subject.ID != testWeek {
		t.Errorf("notification = %+v", n)
	}
	if n.Title == "" || n.Body == "" {
		t.Errorf("notification needs display text: %+v", n)
	}
}

func TestRefreshStaysQuiet(t *testing.T) {
	ctx := context.Background()
	t.Run("before the order day", func(t *testing.T) {
		svc, _, notifier := newOrderService(t, "wed", denverNoon(t, 15))
		if err := svc.Refresh(ctx, testHousehold); err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if len(notifier.created) != 0 {
			t.Errorf("created %+v before the order day", notifier.created)
		}
	})
	t.Run("no order day", func(t *testing.T) {
		svc, _, notifier := newOrderService(t, "", denverNoon(t, 19))
		if err := svc.Refresh(ctx, testHousehold); err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if len(notifier.created) != 0 {
			t.Errorf("created %+v without an order day", notifier.created)
		}
	})
	t.Run("already marked ordered", func(t *testing.T) {
		svc, _, notifier := newOrderService(t, "wed", denverNoon(t, 16))
		if _, err := svc.SetWeekOrdered(ctx, memberOf(testUser), testWeek, true); err != nil {
			t.Fatalf("SetWeekOrdered() error = %v", err)
		}
		if err := svc.Refresh(ctx, testHousehold); err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if len(notifier.created) != 0 {
			t.Errorf("created %+v for a week already ordered", notifier.created)
		}
	})
}
