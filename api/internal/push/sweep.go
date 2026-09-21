package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
)

// HouseholdSource is what the sweep reads about households.
// *households.MongoStore implements it.
type HouseholdSource interface {
	ListHouseholdIDs(ctx context.Context, after string, limit int) ([]string, error)
	GetHousehold(ctx context.Context, id string) (households.Household, error)
	ListMembershipsByHousehold(ctx context.Context, householdID string) ([]households.Membership, error)
}

// Refresher brings one household's derived notifications up to date.
// *notifications.Service implements it.
type Refresher interface {
	Refresh(ctx context.Context, householdID string)
}

// Relevance says whether a pending notification is still worth pushing: the
// condition it announced may have ended while it waited (the week was ordered,
// the item restocked). *shopping.Service and *pantry.Service implement it for
// their own types and answer true for others.
type Relevance interface {
	StillRelevant(ctx context.Context, n notifications.Notification) (bool, error)
}

// Sweep defaults.
const (
	// DefaultMaxAge: a pending notification older than this is no longer
	// news and is skipped rather than pushed.
	DefaultMaxAge = 24 * time.Hour
	// DefaultQuietStart and DefaultQuietEnd bound the household's local
	// night, when pushes wait for the morning.
	DefaultQuietStart = 21
	DefaultQuietEnd   = 8

	householdPageSize = 200
	pendingBatchSize  = 1000
	// pushExpiration is how long APNs keeps trying to reach an offline phone.
	pushExpiration = 12 * time.Hour
)

// SweepOptions configures a Sweeper.
type SweepOptions struct {
	Households HouseholdSource
	Refresher  Refresher
	Outbox     notifications.Outbox
	Tokens     Store
	// Relevance checks run after the claim; any false skips the push. An
	// error is logged and the push goes ahead.
	Relevance []Relevance
	// Sender nil refreshes only: notifications stay pending, nothing is
	// sent, and the run logs that push is not configured.
	Sender Sender
	Logger *slog.Logger
	Now    func() time.Time
	MaxAge time.Duration
	// QuietStart and QuietEnd are local hours [QuietStart, QuietEnd) when
	// nothing is pushed. Equal values disable quiet hours.
	QuietStart, QuietEnd int
	// QuietExempt are types quiet hours do not hold back. Nil means
	// DefaultQuietExempt.
	QuietExempt []notifications.Type
}

// DefaultQuietExempt are the notification types quiet hours do not defer:
// reminders the household itself scheduled for an early hour. A thaw
// reminder set for 6am is an errand with a deadline, and holding it until
// the quiet hours end at 8 would be the app overruling the member about
// their own morning.
var DefaultQuietExempt = []notifications.Type{notifications.TypePantryThaw}

// Sweeper is one run of the reminder sweep.
type Sweeper struct {
	opts SweepOptions
}

// NewSweeper returns a Sweeper with defaults filled in.
func NewSweeper(opts SweepOptions) *Sweeper {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MaxAge == 0 {
		opts.MaxAge = DefaultMaxAge
	}
	if opts.QuietStart == 0 && opts.QuietEnd == 0 {
		opts.QuietStart, opts.QuietEnd = DefaultQuietStart, DefaultQuietEnd
	}
	if opts.QuietExempt == nil {
		opts.QuietExempt = DefaultQuietExempt
	}
	return &Sweeper{opts: opts}
}

// SweepReport counts what one run did.
type SweepReport struct {
	Households int
	// Sent, Skipped, and Failed are notifications by final push status.
	Sent, Skipped, Failed int
	// Deferred notifications stayed pending for quiet hours.
	Deferred int
	// Deliveries are devices APNs accepted a push for.
	Deliveries    int
	TokensRemoved int
}

// Run refreshes every household, then pushes pending notifications. A
// failure for one household or notification is logged and the run goes on;
// the error is for failures that stop the run.
func (s *Sweeper) Run(ctx context.Context) (SweepReport, error) {
	var report SweepReport
	after := ""
	for {
		ids, err := s.opts.Households.ListHouseholdIDs(ctx, after, householdPageSize)
		if err != nil {
			return report, fmt.Errorf("list households: %w", err)
		}
		for _, id := range ids {
			s.opts.Refresher.Refresh(ctx, id)
			report.Households++
		}
		if len(ids) < householdPageSize {
			break
		}
		after = ids[len(ids)-1]
	}
	if s.opts.Sender == nil {
		s.opts.Logger.InfoContext(ctx, "push not configured (APNS_* unset); notifications stay in-app only",
			"households", report.Households)
		return report, nil
	}

	pending, err := s.opts.Outbox.PendingPush(ctx, pendingBatchSize)
	if err != nil {
		return report, fmt.Errorf("list pending pushes: %w", err)
	}
	run := sweepRun{Sweeper: s, report: &report,
		households: map[string]*households.Household{}, members: map[string][]string{}}
	for _, n := range pending {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		run.deliver(ctx, n)
	}
	return report, nil
}

// sweepRun caches household lookups for one run.
type sweepRun struct {
	*Sweeper
	report     *SweepReport
	households map[string]*households.Household // nil: household gone
	members    map[string][]string
}

func (r *sweepRun) household(ctx context.Context, id string) (*households.Household, error) {
	if h, ok := r.households[id]; ok {
		return h, nil
	}
	h, err := r.opts.Households.GetHousehold(ctx, id)
	if errors.Is(err, households.ErrNotFound) {
		r.households[id] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.households[id] = &h
	return &h, nil
}

func (r *sweepRun) memberIDs(ctx context.Context, householdID string) ([]string, error) {
	if ids, ok := r.members[householdID]; ok {
		return ids, nil
	}
	ms, err := r.opts.Households.ListMembershipsByHousehold(ctx, householdID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(ms))
	for _, m := range ms {
		ids = append(ids, m.UserID)
	}
	r.members[householdID] = ids
	return ids, nil
}

// quiet reports whether it is night in the household's time zone.
func (r *sweepRun) quiet(h *households.Household) bool {
	start, end := r.opts.QuietStart, r.opts.QuietEnd
	if start == end {
		return false
	}
	loc, err := time.LoadLocation(h.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	hour := r.opts.Now().In(loc).Hour()
	if start < end {
		return hour >= start && hour < end
	}
	return hour >= start || hour < end
}

// deliver pushes one pending notification, claiming it first so no other
// run sends it too.
func (r *sweepRun) deliver(ctx context.Context, n notifications.Notification) {
	logger := r.opts.Logger.With("notificationId", n.ID, "householdId", n.HouseholdID, "type", n.Type)
	now := r.opts.Now()

	h, err := r.household(ctx, n.HouseholdID)
	if err != nil {
		logger.WarnContext(ctx, "push: load household failed", "error", err)
		return
	}
	stale := now.Sub(n.CreatedAt) > r.opts.MaxAge
	if !stale && h != nil && !slices.Contains(r.opts.QuietExempt, n.Type) && r.quiet(h) {
		r.report.Deferred++
		return
	}
	claimed, err := r.opts.Outbox.ClaimPush(ctx, n.ID, now.UTC())
	if err != nil {
		logger.WarnContext(ctx, "push: claim failed", "error", err)
		return
	}
	if !claimed {
		return
	}
	status := r.send(ctx, logger, n, h, stale)
	// The claim already guarantees at most once; recording the outcome
	// uses its own context so a cancelled run still records what it sent.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := r.opts.Outbox.FinishPush(finishCtx, n.ID, status, r.opts.Now().UTC()); err != nil {
		logger.WarnContext(ctx, "push: record outcome failed", "status", status, "error", err)
	}
	switch status {
	case notifications.PushSent:
		r.report.Sent++
	case notifications.PushFailed:
		r.report.Failed++
	default:
		r.report.Skipped++
	}
	logger.InfoContext(ctx, "push finished", "status", status)
}

// send delivers a claimed notification to every device of every member who
// hasn't read it, and returns its final push status.
func (r *sweepRun) send(ctx context.Context, logger *slog.Logger, n notifications.Notification, h *households.Household, stale bool) notifications.PushStatus {
	if stale || h == nil {
		return notifications.PushSkipped
	}
	for _, check := range r.opts.Relevance {
		relevant, err := check.StillRelevant(ctx, n)
		if err != nil {
			logger.WarnContext(ctx, "push: relevance check failed; sending anyway", "error", err)
			continue
		}
		if !relevant {
			return notifications.PushSkipped
		}
	}
	memberIDs, err := r.memberIDs(ctx, n.HouseholdID)
	if err != nil {
		logger.WarnContext(ctx, "push: list members failed", "error", err)
		return notifications.PushFailed
	}
	recipients := slices.DeleteFunc(slices.Clone(memberIDs), n.ReadByUser)
	if len(recipients) == 0 {
		return notifications.PushSkipped
	}
	devices, err := r.opts.Tokens.ListByUsers(ctx, recipients)
	if err != nil {
		logger.WarnContext(ctx, "push: list device tokens failed", "error", err)
		return notifications.PushFailed
	}
	message := Message{
		Title: n.Title, Body: n.Body, ThreadID: string(n.Type), CollapseID: n.ID,
		Expiration: r.opts.Now().Add(pushExpiration),
		Data: map[string]any{
			"notificationId": n.ID, "householdId": n.HouseholdID, "type": string(n.Type),
			"subject": map[string]string{"kind": string(n.Subject.Kind), "id": n.Subject.ID},
		},
	}
	delivered, failed := 0, 0
	seen := map[string]bool{}
	for _, d := range devices {
		if seen[d.Token] {
			continue
		}
		seen[d.Token] = true
		m := message
		m.Token, m.Environment = d.Token, d.Environment
		err := r.opts.Sender.Send(ctx, m)
		switch {
		case err == nil:
			delivered++
		case errors.Is(err, ErrDeviceTokenInvalid):
			if err := r.opts.Tokens.DeleteToken(ctx, d.Token); err != nil {
				logger.WarnContext(ctx, "push: remove invalid device token failed", "error", err)
			} else {
				r.report.TokensRemoved++
			}
		default:
			failed++
			logger.WarnContext(ctx, "push: send failed", "userId", d.UserID, "environment", d.Environment, "error", err)
		}
	}
	r.report.Deliveries += delivered
	switch {
	case delivered > 0:
		return notifications.PushSent
	case failed > 0:
		return notifications.PushFailed
	default:
		return notifications.PushSkipped
	}
}
