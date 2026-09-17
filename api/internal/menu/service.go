// Package menu composes recipes, ratings, plans, Autopilot preferences, and
// cooked events into the app's Menu screen: the week strip, curated sections,
// and the All Meals list. It stores nothing. Every request reads the catalog
// once with a projection and builds sections in memory (docs/api.md#menu).
package menu

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// ErrInvalid wraps a problem with query parameters. Its message, without the
// "menu: invalid query: " prefix, is safe to show clients.
var ErrInvalid = errors.New("menu: invalid query")

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// RecipeSource reads the catalog. *recipes.Service implements it.
type RecipeSource interface {
	MenuCatalog(ctx context.Context, householdID string) ([]recipes.Recipe, error)
}

// RatingSource reads ratings. *ratings.Service implements it.
type RatingSource interface {
	HouseholdRatings(ctx context.Context, householdID string) ([]ratings.Rating, error)
}

// PlanSource reads week plans. *planning.Service implements it.
type PlanSource interface {
	Get(ctx context.Context, householdID, week string) (planning.Plan, error)
	List(ctx context.Context, householdID, from, to string) ([]planning.Summary, error)
	EarliestPlannedWeek(ctx context.Context, householdID string) (planning.Week, bool, error)
}

// AutopilotSource reads Autopilot preferences and proposals.
// *recommendations.Service implements it.
type AutopilotSource interface {
	Profile(ctx context.Context, householdID string) (recommendations.Profile, error)
	RecipeOverrides(ctx context.Context, householdID string) ([]recommendations.RecipeOverride, error)
	Proposal(ctx context.Context, householdID, week string) (recommendations.Proposal, error)
}

// EventSource reads behavior events. *events.Service implements it.
type EventSource interface {
	List(ctx context.Context, q events.Query) ([]events.Event, error)
}

// HouseholdSource loads a household (for its time zone).
// *households.Service implements it.
type HouseholdSource interface {
	GetHousehold(ctx context.Context, id string) (households.Household, error)
}

// Options configures a Service.
type Options struct {
	Recipes    RecipeSource
	Ratings    RatingSource
	Plans      PlanSource
	Autopilot  AutopilotSource
	Events     EventSource
	Households HouseholdSource
	// Now is the clock. Default time.Now.
	Now func() time.Time
}

// Service builds menus. Like the other household services it takes IDs that
// HTTP routes have already authorized.
type Service struct {
	opts Options
	now  func() time.Time
}

// NewService returns a Service.
func NewService(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{opts: opts, now: now}
}

// Timing places a week relative to the household's current week.
type Timing string

// Timings.
const (
	TimingPast     Timing = "past"
	TimingCurrent  Timing = "current"
	TimingUpcoming Timing = "upcoming"
)

func timingOf(w, current planning.Week) Timing {
	switch d := current.WeeksUntil(w); {
	case d < 0:
		return TimingPast
	case d == 0:
		return TimingCurrent
	}
	return TimingUpcoming
}

// Menu is one week's Menu screen.
type Menu struct {
	Week        planning.Week
	CurrentWeek planning.Week
	// FirstDay is the household's first day of the week, which decides the
	// dates Week covers.
	FirstDay planning.Day
	Timing   Timing
	// Plan is nil when nothing is stored for the week.
	Plan *planning.Plan
	// Proposal is the week's Autopilot proposal, if any.
	Proposal *recommendations.Proposal
	// Sections are non-empty, in display order.
	Sections []Section
	// Bands are the household's cook-time bands, for recipe summaries.
	Bands autopilot.TimeBands
}

// Menu returns the Menu screen for week ("" for the household's current week).
// userID is the caller, whose own ratings appear as myRating.
func (s *Service) Menu(ctx context.Context, householdID, userID, week string) (Menu, error) {
	current, loc, first, err := s.clock(ctx, householdID)
	if err != nil {
		return Menu{}, err
	}
	w, err := parseWeek(week, current)
	if err != nil {
		return Menu{}, err
	}
	in, err := s.load(ctx, householdID, userID, w, current, loc, first)
	if err != nil {
		return Menu{}, err
	}
	snap := newSnapshot(in)
	m := Menu{
		Week: w, CurrentWeek: current, FirstDay: first, Timing: timingOf(w, current), Proposal: in.Proposal,
		Sections: snap.sections(timingOf(w, current)), Bands: snap.bands,
	}
	if !in.Plan.CreatedAt.IsZero() {
		plan := in.Plan
		m.Plan = &plan
	}
	return m, nil
}

// clock returns the household's current week, time zone, and first day of the
// week.
func (s *Service) clock(ctx context.Context, householdID string) (planning.Week, *time.Location, planning.Day, error) {
	h, err := s.opts.Households.GetHousehold(ctx, householdID)
	if err != nil {
		return planning.Week{}, nil, "", fmt.Errorf("load household: %w", err)
	}
	loc, err := time.LoadLocation(h.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	first := planning.Day(h.FirstDay())
	return planning.WeekOfOn(s.now().In(loc), first), loc, first, nil
}

func parseWeek(value string, current planning.Week) (planning.Week, error) {
	if value == "" {
		return current, nil
	}
	w, err := planning.ParseWeek(value)
	if err != nil {
		return planning.Week{}, invalidf("week must be an ISO week that exists, such as 2026-W38")
	}
	return w, nil
}

// load reads everything a snapshot of week needs: the catalog, ratings,
// profile, overrides, cooked events, and the week's plan and proposal.
func (s *Service) load(ctx context.Context, householdID, userID string, w, current planning.Week, loc *time.Location, first planning.Day) (snapshotInput, error) {
	in := snapshotInput{UserID: userID, Week: w, Current: current, Location: loc, FirstDay: first}
	var err error
	if in.Catalog, err = s.opts.Recipes.MenuCatalog(ctx, householdID); err != nil {
		return snapshotInput{}, fmt.Errorf("load catalog: %w", err)
	}
	if in.Ratings, err = s.opts.Ratings.HouseholdRatings(ctx, householdID); err != nil {
		return snapshotInput{}, fmt.Errorf("load ratings: %w", err)
	}
	if in.Profile, err = s.opts.Autopilot.Profile(ctx, householdID); err != nil {
		return snapshotInput{}, fmt.Errorf("load autopilot profile: %w", err)
	}
	if in.Overrides, err = s.opts.Autopilot.RecipeOverrides(ctx, householdID); err != nil {
		return snapshotInput{}, fmt.Errorf("load recipe overrides: %w", err)
	}
	if in.Cooked, err = s.cookedEvents(ctx, householdID, time.Time{}); err != nil {
		return snapshotInput{}, err
	}
	if in.Plan, err = s.opts.Plans.Get(ctx, householdID, w.String()); err != nil {
		return snapshotInput{}, fmt.Errorf("load plan: %w", err)
	}
	proposal, err := s.opts.Autopilot.Proposal(ctx, householdID, w.String())
	switch {
	case errors.Is(err, recommendations.ErrNotFound):
	case err != nil:
		return snapshotInput{}, fmt.Errorf("load proposal: %w", err)
	default:
		in.Proposal = &proposal
	}
	return in, nil
}

func (s *Service) cookedEvents(ctx context.Context, householdID string, since time.Time) ([]events.Event, error) {
	list, err := s.opts.Events.List(ctx, events.Query{
		HouseholdID: householdID, Types: []events.Type{events.TypeRecipeCooked}, Since: since, Limit: events.MaxListLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("load cooked events: %w", err)
	}
	return list, nil
}
