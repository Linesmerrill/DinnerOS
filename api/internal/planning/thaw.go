package planning

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
)

// This file holds the thaw reminder: on the day a frozen pantry item is
// needed, a nudge to move it to the fridge
// (docs/pantry-usage.md#thaw-reminders).
//
// The freezer only works if something takes the food out of it in time. A
// four-pound pork loin sealed in March is not dinner on a Thursday unless
// somebody remembers on Thursday morning, and "I'll remember" is exactly the
// thing that fails.
//
// Like the grocery order reminder, nothing is stored. "Is a frozen item
// needed today, and has it long enough to thaw?" is a question about today,
// answered from facts that are stored: the week's plan, its recipes'
// ingredients, and what is in the freezer. The reminder is derived on read
// and, for a phone that is closed, by the hourly sweep (cmd/sendreminders).
//
// The household chooses the hour (households.Household.ThawReminderHour,
// default 6am), because a reminder that arrives after the errand is useless
// and one that arrives at 4am is worse than useless.

// DinnerHour is the local hour a planned meal is assumed to be eaten. It is
// the only guess in the model, and it is the one the advice is anchored to:
// "take it out by 2pm" means "about four hours before dinner".
const DinnerHour = 18

// HouseholdSource reads a household's time zone, week start and thaw
// reminder hour. *households.Service implements it.
type HouseholdSource interface {
	GetHousehold(ctx context.Context, id string) (households.Household, error)
}

// FrozenSource lists a household's in-stock freezer items with their thaw
// estimates. *pantry.Service implements it.
type FrozenSource interface {
	FrozenStock(ctx context.Context, householdID string) ([]pantry.FrozenItem, error)
}

// ThawNotifier creates household notifications. *notifications.Service
// implements it.
type ThawNotifier interface {
	Create(ctx context.Context, in notifications.New) (notifications.Notification, bool, error)
}

// WithFreezer makes the planner answer thaw reminders from source, for the
// household read through hh, and returns s. Without all three the reminder
// is simply off, which keeps the rest of planning working.
func (s *Service) WithFreezer(source FrozenSource, hh HouseholdSource, notifier ThawNotifier) *Service {
	s.frozen, s.households, s.thawNotifier = source, hh, notifier
	return s
}

// ThawItem is one frozen item a meal planned for Date needs.
type ThawItem struct {
	ItemID string
	Name   string
	// Date is the calendar date of the meal, in the household's time zone.
	Date string
	// Recipes are the planned meals that need it, by name.
	Recipes []string
	// Hours is the thaw estimate for one portion; Measured is false when it
	// is the category default rather than a weight calculation.
	Hours    int
	Measured bool
	// MoveBy is the local time to put it in the fridge ("14:00"), DinnerHour
	// less Hours. Overnight is true when that time has already passed today,
	// which is the usual case for anything that takes longer than a workday:
	// the honest advice is then "this morning".
	MoveBy    string
	Overnight bool
	// Summary is ready to show.
	Summary string
}

// ThawDue is what a household should move to the fridge today.
type ThawDue struct {
	HouseholdID string
	Date        string
	// Hour is the household's thaw reminder hour.
	Hour int
	// Items are today's frozen items, by name. Never nil.
	Items []ThawItem
}

// ThawDue returns the frozen pantry items today's planned meals need. It is
// empty, without error, when the household has no freezer stock, no meals
// today, or nothing in common between them.
func (s *Service) ThawDue(ctx context.Context, householdID string) (ThawDue, error) {
	if householdID == "" {
		return ThawDue{}, errHouseholdRequired
	}
	out := ThawDue{HouseholdID: householdID, Items: []ThawItem{}}
	if s.frozen == nil || s.households == nil {
		return out, nil
	}
	hh, err := s.households.GetHousehold(ctx, householdID)
	if err != nil {
		return ThawDue{}, fmt.Errorf("get household: %w", err)
	}
	now := s.now().In(thawLocation(hh.TimeZone))
	out.Date, out.Hour = now.Format(time.DateOnly), hh.ThawHour()

	stock, err := s.frozen.FrozenStock(ctx, householdID)
	if err != nil {
		return ThawDue{}, fmt.Errorf("list frozen pantry items: %w", err)
	}
	if len(stock) == 0 {
		return out, nil
	}
	first := Day(hh.FirstDay())
	plan, err := s.Get(ctx, householdID, WeekOfOn(now, first).String())
	if err != nil {
		return ThawDue{}, fmt.Errorf("load plan: %w", err)
	}
	var todayIDs []string
	for _, e := range plan.Entries {
		if plan.DateOf(e.Day) == out.Date {
			todayIDs = append(todayIDs, e.RecipeID)
		}
	}
	if len(todayIDs) == 0 {
		return out, nil
	}
	byID, err := s.recipesByID(ctx, householdID, todayIDs)
	if err != nil {
		return ThawDue{}, err
	}
	byKey := map[string]pantry.FrozenItem{}
	for _, f := range stock {
		for _, key := range f.Keys {
			byKey[key] = f
		}
	}

	needed := map[string]*ThawItem{}
	for _, id := range todayIDs {
		r, ok := byID[id]
		if !ok {
			continue
		}
		for _, ing := range r.Ingredients {
			f, ok := byKey[thawKey(ing.IngredientID, ing.Name)]
			if !ok {
				continue
			}
			item := needed[f.Item.ID]
			if item == nil {
				item = newThawItem(f, out.Date, now)
				needed[f.Item.ID] = item
			}
			if !slices.Contains(item.Recipes, r.Name) {
				item.Recipes = append(item.Recipes, r.Name)
			}
		}
	}
	for _, item := range needed {
		sort.Strings(item.Recipes)
		item.Summary = thawSummary(*item)
		out.Items = append(out.Items, *item)
	}
	sort.Slice(out.Items, func(i, j int) bool {
		if !strings.EqualFold(out.Items[i].Name, out.Items[j].Name) {
			return strings.ToLower(out.Items[i].Name) < strings.ToLower(out.Items[j].Name)
		}
		return out.Items[i].ItemID < out.Items[j].ItemID
	})
	return out, nil
}

// thawKey is the grocery key a recipe ingredient answers to, matching
// pantry.FrozenItem.Keys.
func thawKey(ingredientID, name string) string {
	if ingredientID != "" {
		return ingredientID
	}
	return unnamedKeyPrefix + ingredients.NormalizeName(name)
}

func newThawItem(f pantry.FrozenItem, date string, now time.Time) *ThawItem {
	item := &ThawItem{
		ItemID: f.Item.ID, Name: f.Item.DisplayName, Date: date,
		Hours: f.Thaw.Hours, Measured: f.Thaw.Measured,
	}
	moveBy := time.Date(now.Year(), now.Month(), now.Day(), DinnerHour, 0, 0, 0, now.Location()).
		Add(-time.Duration(f.Thaw.Hours) * time.Hour)
	item.MoveBy = moveBy.Format("15:04")
	item.Overnight = !moveBy.After(now)
	return item
}

// thawSummary is the sentence a member reads. It says how long it takes, when
// to start, and — when the clock has already passed that point — that doing
// it now is the answer, rather than pretending the deadline is still ahead.
func thawSummary(item ThawItem) string {
	var b strings.Builder
	if len(item.Recipes) > 0 {
		fmt.Fprintf(&b, "%s is for %s tonight. ", item.Name, strings.Join(item.Recipes, " and "))
	}
	fmt.Fprintf(&b, "This usually takes %s in the fridge", hoursText(item.Hours))
	if item.Overnight {
		b.WriteString(" — move it over this morning.")
		return b.String()
	}
	fmt.Fprintf(&b, " — put it in the fridge by %s, or this morning if that's easier.", clockText(item.MoveBy))
	return b.String()
}

func hoursText(hours int) string {
	switch {
	case hours == 1:
		return "about an hour"
	case hours < 24:
		return fmt.Sprintf("about %d hours", hours)
	case hours < 36:
		return "about a day"
	}
	return "a day or two"
}

// clockText turns "14:00" into "2 PM", and "14:30" into "2:30 PM".
func clockText(hhmm string) string {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return hhmm
	}
	if t.Minute() == 0 {
		return t.Format("3 PM")
	}
	return t.Format("3:04 PM")
}

func thawLocation(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

// thawDedupeKey is unique per household, item, and day, so however often
// notifications are read, the bell holds one reminder per item per day.
func thawDedupeKey(date, itemID string) string { return "pantry.thaw:" + date + ":" + itemID }

// Refresh creates today's thaw reminders once the household's reminder hour
// has arrived. It implements notifications.Refresher, so reading the bell
// brings them up to date, and the hourly sweep does the same for a phone
// nobody opened.
//
// It is idempotent: the dedupe key is the day and the item.
func (s *Service) Refresh(ctx context.Context, householdID string) error {
	if s.thawNotifier == nil || s.frozen == nil || s.households == nil {
		return nil
	}
	due, err := s.ThawDue(ctx, householdID)
	if err != nil {
		return err
	}
	if len(due.Items) == 0 {
		return nil
	}
	hh, err := s.households.GetHousehold(ctx, householdID)
	if err != nil {
		return fmt.Errorf("get household: %w", err)
	}
	// Before the household's hour there is nothing to say yet; the errand is
	// theirs to do when they are up.
	if s.now().In(thawLocation(hh.TimeZone)).Hour() < due.Hour {
		return nil
	}
	for _, item := range due.Items {
		if _, _, err := s.thawNotifier.Create(ctx, notifications.New{
			HouseholdID: householdID,
			Type:        notifications.TypePantryThaw,
			Title:       "Take " + item.Name + " out to thaw",
			Body:        item.Summary,
			Subject:     notifications.Subject{Kind: notifications.SubjectPantryItem, ID: item.ItemID},
			DedupeKey:   thawDedupeKey(item.Date, item.ItemID),
		}); err != nil {
			return fmt.Errorf("create thaw reminder notification: %w", err)
		}
	}
	return nil
}

// StillRelevant reports whether a thaw reminder is still worth pushing: the
// item is still frozen and still needed today. A reminder that waited out the
// night isn't pushed after the meal was moved or the item taken out. Other
// notification types are not the planner's to judge. It implements
// push.Relevance.
func (s *Service) StillRelevant(ctx context.Context, n notifications.Notification) (bool, error) {
	if n.Type != notifications.TypePantryThaw {
		return true, nil
	}
	due, err := s.ThawDue(ctx, n.HouseholdID)
	if err != nil {
		return true, err
	}
	for _, item := range due.Items {
		if item.ItemID == n.Subject.ID {
			return true, nil
		}
	}
	return false, nil
}
