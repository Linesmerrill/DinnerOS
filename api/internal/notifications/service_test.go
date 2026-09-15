package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

type refresherFunc func(ctx context.Context, householdID string) error

func (f refresherFunc) Refresh(ctx context.Context, householdID string) error {
	return f(ctx, householdID)
}

func newTestService() (*Service, *memoryStore) {
	store := &memoryStore{}
	return NewService(ServiceOptions{Store: store, Now: func() time.Time { return testNow }}), store
}

func memberOf(householdID, userID string) households.Membership {
	return households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}
}

func lowNotification(key string) New {
	return New{
		HouseholdID: hhA, Type: TypePantryLow, Title: "Butter is running low", Body: "About 20% left.",
		Subject: Subject{Kind: SubjectPantryItem, ID: "item1"}, DedupeKey: key,
	}
}

func TestCreateDeduplicates(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	n, created, err := svc.Create(ctx, lowNotification("pantry.low:item1:cycle1"))
	if err != nil || !created || n.Push.Status != PushPending || !n.CreatedAt.Equal(testNow) {
		t.Fatalf("Create() = %+v, %v, %v", n, created, err)
	}
	again, created, err := svc.Create(ctx, lowNotification("pantry.low:item1:cycle1"))
	if err != nil || created || again.ID != n.ID {
		t.Errorf("repeated Create() = %+v, %v, %v; want the existing notification", again, created, err)
	}
	if len(store.items) != 1 {
		t.Errorf("stored %d notifications, want 1", len(store.items))
	}

	for _, bad := range []New{
		{Type: TypePantryLow, Title: "x", DedupeKey: "k"},
		{HouseholdID: hhA, Title: "x", DedupeKey: "k"},
		{HouseholdID: hhA, Type: TypePantryLow, DedupeKey: "k"},
		{HouseholdID: hhA, Type: TypePantryLow, Title: "x"},
	} {
		if _, _, err := svc.Create(ctx, bad); err == nil {
			t.Errorf("Create(%+v) succeeded", bad)
		}
	}
	store.err = errors.New("boom")
	if _, _, err := svc.Create(ctx, lowNotification("k2")); err == nil {
		t.Error("Create() with a failing store succeeded")
	}
}

func TestListPagesAndRefreshes(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	refreshed := 0
	svc.SetRefresher(refresherFunc(func(_ context.Context, householdID string) error {
		if householdID != hhA {
			t.Errorf("refreshed %s", householdID)
		}
		refreshed++
		return errors.New("refresh failures are logged, not returned")
	}))
	for _, key := range []string{"a", "b", "c"} {
		if _, _, err := svc.Create(ctx, lowNotification(key)); err != nil {
			t.Fatal(err)
		}
	}

	page, err := svc.List(ctx, memberOf(hhA, userA), ListQuery{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.Items[0].DedupeKey != "c" || page.NextCursor != page.Items[1].ID {
		t.Fatalf("List() = %+v, %v", page, err)
	}
	next, err := svc.List(ctx, memberOf(hhA, userA), ListQuery{Limit: 2, Before: page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].DedupeKey != "a" || next.NextCursor != "" {
		t.Fatalf("List(page 2) = %+v, %v", next, err)
	}
	if refreshed != 2 {
		t.Errorf("refreshed %d times, want 2", refreshed)
	}

	for _, q := range []ListQuery{{Limit: -1}, {Limit: MaxListLimit + 1}, {Before: "nope"}} {
		var ve *ValidationError
		if _, err := svc.List(ctx, memberOf(hhA, userA), q); !errors.As(err, &ve) {
			t.Errorf("List(%+v) error = %v, want validation", q, err)
		}
	}
	if _, err := svc.List(ctx, households.Membership{HouseholdID: hhA, UserID: userA, Role: "nobody"}, ListQuery{}); !errors.Is(err, households.ErrForbidden) {
		t.Errorf("List(no permission) error = %v", err)
	}
}

func TestMarkReadAndUnreadCount(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	a, _, _ := svc.Create(ctx, lowNotification("a"))
	if _, _, err := svc.Create(ctx, lowNotification("b")); err != nil {
		t.Fatal(err)
	}
	ada, bob := memberOf(hhA, userA), memberOf(hhA, userB)

	if n, err := svc.UnreadCount(ctx, ada); err != nil || n != 2 {
		t.Fatalf("UnreadCount() = %d, %v", n, err)
	}
	if n, err := svc.MarkRead(ctx, ada, []string{a.ID}, false); err != nil || n != 1 {
		t.Fatalf("MarkRead(one) = %d, %v", n, err)
	}
	page, _ := svc.List(ctx, ada, ListQuery{UnreadOnly: true})
	if len(page.Items) != 1 || page.Items[0].DedupeKey != "b" {
		t.Errorf("unread list = %+v", page.Items)
	}
	if n, _ := svc.UnreadCount(ctx, bob); n != 2 {
		t.Errorf("another member's unread count = %d, want 2", n)
	}
	if n, err := svc.MarkRead(ctx, bob, nil, true); err != nil || n != 0 {
		t.Errorf("MarkRead(all) = %d, %v", n, err)
	}

	for name, call := range map[string]func() error{
		"neither": func() error { _, err := svc.MarkRead(ctx, ada, nil, false); return err },
		"both":    func() error { _, err := svc.MarkRead(ctx, ada, []string{a.ID}, true); return err },
		"blank":   func() error { _, err := svc.MarkRead(ctx, ada, []string{" "}, false); return err },
		"too many": func() error {
			_, err := svc.MarkRead(ctx, ada, make([]string, MaxMarkRead+1), false)
			return err
		},
	} {
		var ve *ValidationError
		if err := call(); !errors.As(err, &ve) {
			t.Errorf("%s: error = %v, want validation", name, err)
		}
	}
}
