package push

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
)

// Noon in Denver.
var sweepNow = time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC)

const (
	adaPhone = "aaaa000000000000000000000000000000000000000000000000000000000001"
	adaIPad  = "aaaa000000000000000000000000000000000000000000000000000000000002"
	bobPhone = "bbbb000000000000000000000000000000000000000000000000000000000001"
	cyPhone  = "cccc000000000000000000000000000000000000000000000000000000000001"
)

type sweepFixture struct {
	outbox *memoryOutbox
	tokens *memoryTokens
	sender *fakeSender
	now    time.Time
}

func newSweepFixture() *sweepFixture {
	return &sweepFixture{
		outbox: &memoryOutbox{produce: map[string][]notifications.Notification{}},
		tokens: &memoryTokens{tokens: []DeviceToken{
			{Token: adaPhone, UserID: userAda, Environment: EnvironmentProduction},
			{Token: adaIPad, UserID: userAda, Environment: EnvironmentSandbox},
			{Token: bobPhone, UserID: userBob, Environment: EnvironmentProduction},
			{Token: cyPhone, UserID: userCy, Environment: EnvironmentProduction},
		}},
		sender: &fakeSender{errs: map[string]error{}},
		now:    sweepNow,
	}
}

func (f *sweepFixture) sweeper(sender Sender) *Sweeper {
	return NewSweeper(SweepOptions{
		Households: fakeHouseholds{
			households: map[string]households.Household{
				hhA: {ID: hhA, TimeZone: "America/Denver"},
				hhB: {ID: hhB, TimeZone: "Europe/Berlin"},
			},
			members: map[string][]string{hhA: {userAda, userBob}, hhB: {userCy}},
		},
		Refresher: f.outbox,
		Outbox:    f.outbox,
		Tokens:    f.tokens,
		Sender:    sender,
		Now:       func() time.Time { return f.now },
	})
}

func orderDue(id, householdID string, createdAt time.Time, readBy ...string) notifications.Notification {
	return notifications.Notification{
		ID: id, HouseholdID: householdID, Type: notifications.TypeShoppingOrderDue,
		Title: "Time to order this week's groceries", Body: "Thursday is your order day.",
		Subject:   notifications.Subject{Kind: notifications.SubjectShoppingWeek, ID: "2026-W38"},
		DedupeKey: "shopping.order_due:2026-W38", ReadBy: readBy, CreatedAt: createdAt,
	}
}

func TestSweepPushesNewReminderOnceToEveryUnreadMembersDevices(t *testing.T) {
	f := newSweepFixture()
	// The order day arrives while nobody has the app open: only the sweep's
	// refresh creates the reminder.
	f.outbox.produce[hhA] = []notifications.Notification{orderDue("n1", hhA, sweepNow.Add(-time.Minute))}
	ctx := context.Background()

	report, err := f.sweeper(f.sender).Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !slices.Equal(f.outbox.refreshed, []string{hhA, hhB}) || report.Households != 2 {
		t.Errorf("refreshed = %v, report = %+v", f.outbox.refreshed, report)
	}
	if got := f.sender.tokens(); !slices.Equal(got, []string{adaPhone, adaIPad, bobPhone}) {
		t.Errorf("sent to %v; want every device of both hhA members and nobody else", got)
	}
	if report.Sent != 1 || report.Deliveries != 3 || f.outbox.status("n1") != notifications.PushSent {
		t.Errorf("report = %+v, status = %s", report, f.outbox.status("n1"))
	}
	m := f.sender.sent[1]
	if m.Environment != EnvironmentSandbox || m.CollapseID != "n1" || m.ThreadID != "shopping.order_due" ||
		m.Data["type"] != "shopping.order_due" || m.Data["householdId"] != hhA ||
		m.Data["subject"].(map[string]string)["id"] != "2026-W38" || m.Title != "Time to order this week's groceries" {
		t.Errorf("message = %+v", m)
	}

	// Running again, or an hour later, sends nothing more.
	f.now = f.now.Add(time.Hour)
	report, err = f.sweeper(f.sender).Run(ctx)
	if err != nil || report.Sent != 0 || len(f.sender.sent) != 3 {
		t.Errorf("second run: report = %+v, err = %v, sent = %d", report, err, len(f.sender.sent))
	}
}

func TestSweepSkipsMembersWhoAlreadyRead(t *testing.T) {
	f := newSweepFixture()
	f.outbox.items = []notifications.Notification{orderDue("n1", hhA, sweepNow.Add(-time.Hour), userAda)}
	f.outbox.items[0].Push.Status = notifications.PushPending
	f.outbox.items = append(f.outbox.items, orderDue("n2", hhA, sweepNow.Add(-time.Hour), userAda, userBob))
	f.outbox.items[1].Push.Status = notifications.PushPending

	report, err := f.sweeper(f.sender).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := f.sender.tokens(); !slices.Equal(got, []string{bobPhone}) {
		t.Errorf("sent to %v, want only Bob, who hasn't read n1", got)
	}
	if f.outbox.status("n1") != notifications.PushSent || f.outbox.status("n2") != notifications.PushSkipped || report.Skipped != 1 {
		t.Errorf("statuses = %s, %s; report = %+v", f.outbox.status("n1"), f.outbox.status("n2"), report)
	}
}

func TestSweepWaitsOutQuietHoursAndSkipsStaleNotifications(t *testing.T) {
	f := newSweepFixture()
	f.now = time.Date(2026, 9, 17, 5, 30, 0, 0, time.UTC) // 23:30 in Denver, 07:30 in Berlin
	f.outbox.produce[hhA] = []notifications.Notification{orderDue("night", hhA, f.now)}
	f.outbox.produce[hhB] = []notifications.Notification{
		orderDue("old", hhB, f.now.Add(-25*time.Hour)),
		orderDue("berlin", hhB, f.now),
	}
	ctx := context.Background()

	report, err := f.sweeper(f.sender).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.sender.sent) != 0 || report.Deferred != 2 || report.Skipped != 1 {
		t.Errorf("night run: sent %v, report = %+v", f.sender.tokens(), report)
	}
	if f.outbox.status("night") != notifications.PushPending || f.outbox.status("old") != notifications.PushSkipped {
		t.Errorf("statuses: night = %s, old = %s", f.outbox.status("night"), f.outbox.status("old"))
	}

	f.now = time.Date(2026, 9, 17, 14, 0, 0, 0, time.UTC) // 08:00 in Denver
	if _, err := f.sweeper(f.sender).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.sender.tokens(); !slices.Equal(got, []string{adaPhone, adaIPad, bobPhone, cyPhone}) {
		t.Errorf("morning run sent to %v", got)
	}
}

func TestSweepRemovesTokensAPNsRejects(t *testing.T) {
	f := newSweepFixture()
	f.outbox.produce[hhA] = []notifications.Notification{orderDue("n1", hhA, sweepNow)}
	f.sender.errs[adaPhone] = &APNsError{Status: 410, Reason: "Unregistered"}
	f.sender.errs[adaIPad] = &APNsError{Status: 400, Reason: "BadDeviceToken"}
	f.sender.errs[bobPhone] = &APNsError{Status: 500, Reason: "InternalServerError"}

	report, err := f.sweeper(f.sender).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	left, _ := f.tokens.ListByUsers(context.Background(), []string{userAda, userBob})
	if len(left) != 1 || left[0].Token != bobPhone || report.TokensRemoved != 2 {
		t.Errorf("tokens left = %v, report = %+v; want Ada's two invalid tokens removed, Bob's kept", left, report)
	}
	// A transient failure is final too: sending again could double-alert
	// the devices that did get it.
	if f.outbox.status("n1") != notifications.PushFailed || report.Failed != 1 {
		t.Errorf("status = %s, report = %+v", f.outbox.status("n1"), report)
	}
}

func TestSweepWithoutSenderOnlyRefreshes(t *testing.T) {
	f := newSweepFixture()
	f.outbox.produce[hhA] = []notifications.Notification{orderDue("n1", hhA, sweepNow)}
	report, err := f.sweeper(nil).Run(context.Background())
	if err != nil || report.Households != 2 {
		t.Fatalf("Run() = %+v, %v", report, err)
	}
	if f.outbox.status("n1") != notifications.PushPending {
		t.Errorf("status = %s; with push unconfigured nothing is claimed", f.outbox.status("n1"))
	}
}

func TestSweepSkipsNotificationsOfDeletedHouseholds(t *testing.T) {
	f := newSweepFixture()
	gone := orderDue("n1", "66e5a1f2c3b4a5d6e7f80c01", sweepNow)
	gone.Push.Status = notifications.PushPending
	f.outbox.items = []notifications.Notification{gone}
	if _, err := f.sweeper(f.sender).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.outbox.status("n1") != notifications.PushSkipped || len(f.sender.sent) != 0 {
		t.Errorf("status = %s, sent = %v", f.outbox.status("n1"), f.sender.tokens())
	}
}

type relevanceFunc func(n notifications.Notification) (bool, error)

func (f relevanceFunc) StillRelevant(_ context.Context, n notifications.Notification) (bool, error) {
	return f(n)
}

func TestSweepSkipsWhatStoppedBeingTrueWhileItWaited(t *testing.T) {
	f := newSweepFixture()
	f.outbox.produce[hhA] = []notifications.Notification{orderDue("ordered", hhA, sweepNow), orderDue("open", hhA, sweepNow)}
	s := f.sweeper(f.sender)
	var checked []string
	s.opts.Relevance = []Relevance{relevanceFunc(func(n notifications.Notification) (bool, error) {
		checked = append(checked, n.ID)
		// The week was marked ordered at breakfast, before this run.
		return n.ID != "ordered", nil
	})}
	report, err := s.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Skipped != 1 || report.Sent != 1 || f.outbox.status("ordered") != notifications.PushSkipped {
		t.Errorf("report = %+v, ordered = %s", report, f.outbox.status("ordered"))
	}
	if len(f.sender.sent) != 3 || f.sender.sent[0].CollapseID != "open" || len(checked) != 2 {
		t.Errorf("sent %d (%v), checked %v", len(f.sender.sent), f.sender.tokens(), checked)
	}

	// A failing check alone doesn't cost the household its reminder.
	g := newSweepFixture()
	g.outbox.produce[hhA] = []notifications.Notification{orderDue("n1", hhA, sweepNow)}
	s = g.sweeper(g.sender)
	s.opts.Relevance = []Relevance{relevanceFunc(func(notifications.Notification) (bool, error) {
		return false, errors.New("database unavailable")
	})}
	if report, err := s.Run(context.Background()); err != nil || report.Sent != 1 {
		t.Errorf("Run() with a failing check = %+v, %v", report, err)
	}
}

// claimLosingOutbox simulates another sweep claiming every notification first.
type claimLosingOutbox struct{ *memoryOutbox }

func (claimLosingOutbox) ClaimPush(context.Context, string, time.Time) (bool, error) {
	return false, nil
}

func TestSweepNeverSendsWhatAnotherRunClaimed(t *testing.T) {
	f := newSweepFixture()
	f.outbox.produce[hhA] = []notifications.Notification{orderDue("n1", hhA, sweepNow)}
	s := f.sweeper(f.sender)
	s.opts.Outbox = claimLosingOutbox{f.outbox}
	if _, err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.sender.sent) != 0 {
		t.Errorf("sent %v after losing the claim", f.sender.tokens())
	}
}

func TestAPNsErrorIsInvalidOnlyForDeadTokens(t *testing.T) {
	if !errors.Is(error(&APNsError{Status: 410}), ErrDeviceTokenInvalid) {
		t.Error("410 should be invalid")
	}
	if errors.Is(error(&APNsError{Status: 403, Reason: "InvalidProviderToken"}), ErrDeviceTokenInvalid) {
		t.Error("a provider token problem must not delete device tokens")
	}
}
