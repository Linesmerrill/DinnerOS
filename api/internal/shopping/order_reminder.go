package shopping

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// This file holds the weekly order reminder: the weekday a household means to
// place its grocery order, and whether a member has marked a week ordered.
//
// The reminder itself is never stored. "Has the order day arrived, and is the
// week still unmarked?" is a question about today, so it is answered on read
// from two facts that are stored: the household's order day, and the one
// thing no amount of data can derive — whether someone actually ordered.
// Nothing infers that: opening a Walmart cart link is not placing an order,
// and marking the week from a handoff would silence the reminder for a
// household that never checked out.

// OrderedWeek records that a household marked one week's groceries ordered.
// It is unique per household and week.
type OrderedWeek struct {
	ID          string
	HouseholdID string
	// Week is an ISO week ("2026-W38").
	Week      string
	OrderedBy string
	OrderedAt time.Time
}

// OrderReminder is a week's order state. Every field is derived on read.
type OrderReminder struct {
	Week string
	// OrderDay is the household's weekday code, or "" when they set none,
	// which turns the reminder off.
	OrderDay string
	// DueOn is the calendar date of OrderDay in Week ("YYYY-MM-DD"), or ""
	// without an order day.
	DueOn string
	// Due reports that the order day has arrived and the week hasn't ended,
	// both read in the household's time zone.
	Due bool
	// Remind is Due and not Ordered: the only state that shows a reminder.
	// It goes quiet the moment a member marks the week, and comes back if
	// they take that back.
	Remind    bool
	Ordered   bool
	OrderedBy string
	OrderedAt time.Time
}

// HouseholdSource reads a household's order day and time zone.
// *households.Service implements it.
type HouseholdSource interface {
	GetHousehold(ctx context.Context, id string) (households.Household, error)
}

// Notifier creates household notifications and marks one read.
// *notifications.Service implements it.
type Notifier interface {
	Create(ctx context.Context, in notifications.New) (notifications.Notification, bool, error)
	MarkReadByDedupeKey(ctx context.Context, actor households.Membership, dedupeKey string) error
}

// weekdayNames turns an order day code into English for notification text.
var weekdayNames = map[string]string{
	"mon": "Monday", "tue": "Tuesday", "wed": "Wednesday", "thu": "Thursday",
	"fri": "Friday", "sat": "Saturday", "sun": "Sunday",
}

// orderDueDedupeKey is unique per household and week, so however often
// notifications are read, the bell holds one reminder per week and never
// nags twice.
func orderDueDedupeKey(week string) string { return "shopping.order_due:" + week }

// orderLocation resolves a household's time zone, falling back to UTC for a
// name this build's zoneinfo doesn't know.
func orderLocation(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

// OrderReminder returns the week's order state for the household.
func (s *Service) OrderReminder(ctx context.Context, householdID, week string) (OrderReminder, error) {
	if householdID == "" {
		return OrderReminder{}, errHouseholdRequired
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return OrderReminder{}, err
	}
	hh, err := s.orderHousehold(ctx, householdID)
	if err != nil {
		return OrderReminder{}, err
	}
	ordered, err := s.orderedWeek(ctx, householdID, w.String())
	if err != nil {
		return OrderReminder{}, err
	}
	return s.buildReminder(hh, w, ordered), nil
}

// SetWeekOrdered marks the week's groceries ordered, or takes that back, and
// returns the week's new state. Taking it back is why this is a value rather
// than a one-way "done": a mis-tap must not leave a household un-remindable
// for the rest of the week.
func (s *Service) SetWeekOrdered(
	ctx context.Context, actor households.Membership, week string, ordered bool,
) (OrderReminder, error) {
	if err := authorize(actor, households.PermShoppingEdit); err != nil {
		return OrderReminder{}, err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return OrderReminder{}, err
	}
	if ordered {
		if _, err := s.store.MarkWeekOrdered(ctx, OrderedWeek{
			HouseholdID: actor.HouseholdID, Week: w.String(), OrderedBy: actor.UserID, OrderedAt: s.timestamp(),
		}); err != nil {
			return OrderReminder{}, fmt.Errorf("mark week ordered: %w", err)
		}
		// The bell's copy is marked read for the member who acted. Taking the
		// mark back deliberately leaves it read: they did see it, and an
		// unread badge reappearing would be its own kind of nagging.
		s.markReminderRead(ctx, actor, w.String())
	} else if err := s.store.UnmarkWeekOrdered(ctx, actor.HouseholdID, w.String()); err != nil &&
		!errors.Is(err, ErrNotFound) {
		return OrderReminder{}, fmt.Errorf("unmark week ordered: %w", err)
	}
	s.logger.InfoContext(ctx, "shopping week ordered set",
		"householdId", actor.HouseholdID, "week", w.String(), "ordered", ordered)
	return s.OrderReminder(ctx, actor.HouseholdID, w.String())
}

// Refresh creates the current week's reminder notification when its order day
// has arrived and nobody has marked the week ordered. It implements
// notifications.Refresher, so reading the bell brings it up to date: no
// scheduler, no background worker, and nothing to drift out of sync.
//
// It is idempotent. The dedupe key is the week, so a household reading
// notifications all day ends with one reminder for that week.
func (s *Service) Refresh(ctx context.Context, householdID string) error {
	if householdID == "" {
		return errHouseholdRequired
	}
	if s.notifier == nil || s.households == nil {
		return nil
	}
	hh, err := s.households.GetHousehold(ctx, householdID)
	if err != nil {
		return fmt.Errorf("get household: %w", err)
	}
	if hh.OrderDay == "" {
		return nil
	}
	week := planning.WeekOf(s.now().In(orderLocation(hh.TimeZone)))
	stored, err := s.orderedWeek(ctx, householdID, week.String())
	if err != nil {
		return err
	}
	reminder := s.buildReminder(hh, week, stored)
	if !reminder.Remind {
		return nil
	}
	if _, _, err := s.notifier.Create(ctx, notifications.New{
		HouseholdID: householdID,
		Type:        notifications.TypeShoppingOrderDue,
		Title:       "Time to order this week's groceries",
		Body:        orderReminderBody(reminder),
		Subject:     notifications.Subject{Kind: notifications.SubjectShoppingWeek, ID: week.String()},
		DedupeKey:   orderDueDedupeKey(week.String()),
	}); err != nil {
		return fmt.Errorf("create order reminder notification: %w", err)
	}
	return nil
}

// StillRelevant reports whether an order reminder should still be pushed:
// the week isn't marked ordered and its reminder is still due. A reminder that
// waited out the night isn't pushed after someone ordered at breakfast. Other
// notification types are not shopping's to judge. It implements
// push.Relevance.
func (s *Service) StillRelevant(ctx context.Context, n notifications.Notification) (bool, error) {
	if n.Type != notifications.TypeShoppingOrderDue {
		return true, nil
	}
	r, err := s.OrderReminder(ctx, n.HouseholdID, n.Subject.ID)
	if err != nil {
		return true, err
	}
	return r.Remind, nil
}

// buildReminder answers the reminder question for one week.
func (s *Service) buildReminder(hh households.Household, w planning.Week, ordered *OrderedWeek) OrderReminder {
	r := OrderReminder{Week: w.String(), OrderDay: hh.OrderDay}
	if ordered != nil {
		r.Ordered, r.OrderedBy, r.OrderedAt = true, ordered.OrderedBy, ordered.OrderedAt
	}
	if hh.OrderDay == "" {
		return r
	}
	r.DueOn = w.Date(planning.Day(hh.OrderDay))
	if r.DueOn == "" {
		return r
	}
	// Dates are YYYY-MM-DD, so string order is calendar order.
	today := s.now().In(orderLocation(hh.TimeZone)).Format(time.DateOnly)
	// A reminder belongs to its own week: it starts on the order day and
	// stops when the week ends. That is what makes next week start fresh,
	// and what stops a run of unmarked past weeks all asking at once.
	r.Due = today >= r.DueOn && today <= w.Date(planning.Sunday)
	r.Remind = r.Due && !r.Ordered
	return r
}

// orderHousehold reads the household. Without a configured source the
// reminder is simply off, which keeps the rest of shopping working.
func (s *Service) orderHousehold(ctx context.Context, householdID string) (households.Household, error) {
	if s.households == nil {
		return households.Household{}, nil
	}
	hh, err := s.households.GetHousehold(ctx, householdID)
	if err != nil {
		return households.Household{}, fmt.Errorf("get household: %w", err)
	}
	return hh, nil
}

// orderedWeek returns the week's marker, or nil when it isn't marked.
func (s *Service) orderedWeek(ctx context.Context, householdID, week string) (*OrderedWeek, error) {
	ow, err := s.store.GetOrderedWeek(ctx, householdID, week)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ordered week: %w", err)
	}
	return &ow, nil
}

// markReminderRead is best effort: the mark itself is what silences the
// reminder, so a failed read-marking must not fail the request.
func (s *Service) markReminderRead(ctx context.Context, actor households.Membership, week string) {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.MarkReadByDedupeKey(ctx, actor, orderDueDedupeKey(week)); err != nil {
		s.logger.WarnContext(ctx, "mark order reminder read failed",
			"householdId", actor.HouseholdID, "week", week, "error", err)
	}
}

func orderReminderBody(r OrderReminder) string {
	day, ok := weekdayNames[r.OrderDay]
	if !ok {
		day = r.OrderDay
	}
	return fmt.Sprintf(
		"%s is your order day. Open Shop to send your list, then mark the week ordered so this stops reminding you.",
		day)
}
