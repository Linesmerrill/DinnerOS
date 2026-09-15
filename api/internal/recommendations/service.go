package recommendations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// HouseholdReader loads a household. *households.Service implements it.
type HouseholdReader interface {
	GetHousehold(ctx context.Context, id string) (households.Household, error)
}

// RecipeReader reads recipes. *recipes.Service implements it.
type RecipeReader interface {
	Catalog(ctx context.Context, householdID string) ([]recipes.Recipe, error)
	Get(ctx context.Context, householdID, id string) (recipes.Recipe, error)
}

// RatingReader reads ratings. *ratings.Service implements it.
type RatingReader interface {
	HouseholdRatings(ctx context.Context, householdID string) ([]ratings.Rating, error)
}

// EventLog records and reads events. *events.Service implements it.
type EventLog interface {
	events.Recorder
	List(ctx context.Context, q events.Query) ([]events.Event, error)
}

// Planner reads and adds plan entries. *planning.Service implements it.
type Planner interface {
	Get(ctx context.Context, householdID, week string) (planning.Plan, error)
	ListPlans(ctx context.Context, householdID, from, to string) ([]planning.Plan, error)
	AddEntries(ctx context.Context, householdID, userID, week string, in []planning.NewEntry) (planning.Plan, []planning.Entry, error)
}

// PantryReader lists pantry items. *pantry.Service implements it.
type PantryReader interface {
	List(ctx context.Context, householdID string, q pantry.ListQuery) ([]pantry.Item, error)
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Store      Store
	Provider   autopilot.RecommendationProvider
	Households HouseholdReader
	Recipes    RecipeReader
	Ratings    RatingReader
	Events     EventLog
	Plans      Planner
	// Pantry is optional; without it, low pantry items aren't a signal.
	Pantry PantryReader
	Logger *slog.Logger
	// Now is the clock. Default time.Now.
	Now func() time.Time
}

// Service implements Autopilot for DinnerOS. Like the other household
// services it takes IDs that HTTP routes have already authorized.
type Service struct {
	store      Store
	provider   autopilot.RecommendationProvider
	households HouseholdReader
	recipes    RecipeReader
	ratings    RatingReader
	events     EventLog
	plans      Planner
	pantry     PantryReader
	logger     *slog.Logger
	now        func() time.Time
}

// maxAttempts bounds retries of version conflicts on profile and context
// writes.
const maxAttempts = 5

// NewService returns a Service.
func NewService(opts ServiceOptions) *Service {
	s := &Service{
		store: opts.Store, provider: opts.Provider, households: opts.Households, recipes: opts.Recipes,
		ratings: opts.Ratings, events: opts.Events, plans: opts.Plans, pantry: opts.Pantry, logger: opts.Logger, now: opts.Now,
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

func (s *Service) record(ctx context.Context, e events.Event) {
	events.RecordOrLog(ctx, s.events, s.logger, e)
}

func required(householdID, userID string) error {
	if householdID == "" || userID == "" {
		return errors.New("recommendations: household and user ids are required")
	}
	return nil
}

// --- profile ------------------------------------------------------------------

// Profile returns the household's profile, or the defaults when it has none.
func (s *Service) Profile(ctx context.Context, householdID string) (Profile, error) {
	p, err := s.store.GetProfile(ctx, householdID)
	if errors.Is(err, ErrNotFound) {
		return DefaultProfile(householdID), nil
	}
	return p, err
}

// DefaultServings resolves the profile's servings: its own value, or the
// household's default when it has none.
func (s *Service) DefaultServings(ctx context.Context, p Profile) (int, error) {
	if p.Schedule.DefaultServings > 0 {
		return p.Schedule.DefaultServings, nil
	}
	h, err := s.households.GetHousehold(ctx, p.HouseholdID)
	if err != nil {
		return 0, fmt.Errorf("load household: %w", err)
	}
	return h.DefaultServings, nil
}

// ProfileUpdate replaces whole sections. Nil sections are unchanged, or reset
// to their defaults when replacing the profile.
type ProfileUpdate struct {
	Taste        *Taste
	Restrictions *Restrictions
	Schedule     *Schedule
	CookTime     *CookTime
	Novelty      *string
	Equipment    *[]string
	WeekdayRules *[]WeekdayRule
}

func (u ProfileUpdate) sections() []Section {
	var out []Section
	for section, set := range map[Section]bool{
		SectionTaste: u.Taste != nil, SectionRestrictions: u.Restrictions != nil, SectionSchedule: u.Schedule != nil,
		SectionCookTime: u.CookTime != nil, SectionNovelty: u.Novelty != nil, SectionEquipment: u.Equipment != nil,
		SectionWeekdayRules: u.WeekdayRules != nil,
	} {
		if set {
			out = append(out, section)
		}
	}
	slices.SortFunc(out, func(a, b Section) int { return slices.Index(Sections, a) - slices.Index(Sections, b) })
	return out
}

// UpdateProfile changes the household's profile. With replace, every section
// is written (onboarding); otherwise only the sections u sets. Sections whose
// values change record who changed them and when, and the change is recorded
// as an autopilot.preferences_updated event with a diff summary. Saving a
// profile for the first time marks every written section as set.
func (s *Service) UpdateProfile(ctx context.Context, householdID, userID string, u ProfileUpdate, replace bool) (Profile, error) {
	if err := required(householdID, userID); err != nil {
		return Profile{}, err
	}
	written := u.sections()
	if replace {
		written = Sections
	} else if len(written) == 0 {
		return Profile{}, invalidf("provide at least one section to change")
	}
	for range maxAttempts {
		current, err := s.store.GetProfile(ctx, householdID)
		exists := err == nil
		if errors.Is(err, ErrNotFound) {
			current = DefaultProfile(householdID)
		} else if err != nil {
			return Profile{}, err
		}
		def := DefaultProfile(householdID)
		next := current
		next.Taste = pick(u.Taste, current.Taste, def.Taste, replace)
		next.Restrictions = pick(u.Restrictions, current.Restrictions, def.Restrictions, replace)
		next.Schedule = pick(u.Schedule, current.Schedule, def.Schedule, replace)
		next.CookTime = pick(u.CookTime, current.CookTime, def.CookTime, replace)
		next.Novelty = pick(u.Novelty, current.Novelty, def.Novelty, replace)
		next.Equipment = pick(u.Equipment, current.Equipment, def.Equipment, replace)
		next.WeekdayRules = pick(u.WeekdayRules, current.WeekdayRules, def.WeekdayRules, replace)
		if next, err = normalizeProfile(next); err != nil {
			return Profile{}, err
		}
		diffs := diffProfile(current, next)
		if exists && len(diffs) == 0 {
			return current, nil
		}
		now := s.now().UTC()
		touched := written
		if exists {
			touched = nil
			for _, section := range Sections {
				if _, ok := diffs[section]; ok {
					touched = append(touched, section)
				}
			}
		}
		next.Sections = maps.Clone(current.Sections)
		if next.Sections == nil {
			next.Sections = map[Section]Change{}
		}
		for _, section := range touched {
			next.Sections[section] = Change{UpdatedBy: userID, UpdatedAt: now}
		}
		if !exists {
			next.CreatedBy, next.CreatedAt = userID, now
		}
		next.UpdatedBy, next.UpdatedAt = userID, now
		saved, err := s.store.SaveProfile(ctx, next)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return Profile{}, err
		}
		payload := events.AutopilotPreferencesUpdated{}
		for _, section := range touched {
			payload.Sections = append(payload.Sections, string(section))
			payload.Changes = append(payload.Changes, diffs[section]...)
		}
		payload.Changes = payload.Changes[:min(len(payload.Changes), events.MaxFieldChanges)]
		s.record(ctx, events.Event{HouseholdID: householdID, UserID: userID, Type: events.TypeAutopilotPreferencesUpdated, OccurredAt: now, Payload: payload})
		return saved, nil
	}
	return Profile{}, ErrConflict
}

func pick[T any](set *T, current, def T, replace bool) T {
	switch {
	case set != nil:
		return *set
	case replace:
		return def
	}
	return current
}

// HistoryEntry is one recorded preference change.
type HistoryEntry struct {
	Type       events.Type
	UserID     string
	OccurredAt time.Time
	Week       string
	RecipeID   string
	Sections   []string
	Changes    []events.FieldChange
	Cleared    bool
	Method     string
	Value      string
	Previous   string
}

// History returns the household's preference changes (profile, week
// contexts, recipe overrides), newest first. limit defaults to
// DefaultHistoryItems and is capped at MaxHistoryItems.
func (s *Service) History(ctx context.Context, householdID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = DefaultHistoryItems
	}
	list, err := s.events.List(ctx, events.Query{
		HouseholdID: householdID, Limit: min(limit, MaxHistoryItems), Newest: true,
		Types: []events.Type{events.TypeAutopilotPreferencesUpdated, events.TypeAutopilotWeekContextUpdated, events.TypeAutopilotRecipeOverrideUpdated},
	})
	if err != nil {
		return nil, err
	}
	out := make([]HistoryEntry, 0, len(list))
	for _, e := range list {
		h := HistoryEntry{Type: e.Type, UserID: e.UserID, OccurredAt: e.OccurredAt, Week: e.Week, RecipeID: e.RecipeID}
		switch payload := e.Payload.(type) {
		case events.AutopilotPreferencesUpdated:
			h.Sections, h.Changes = payload.Sections, payload.Changes
		case events.AutopilotWeekContextUpdated:
			h.Changes, h.Cleared = payload.Changes, payload.Cleared
		case events.AutopilotRecipeOverrideUpdated:
			h.Method, h.Value, h.Previous = payload.Method, payload.Value, payload.Previous
		}
		out = append(out, h)
	}
	return out, nil
}

// Vocabulary returns the choices for the onboarding and preference screens.
func (s *Service) Vocabulary(ctx context.Context, householdID string) (Vocabulary, error) {
	catalog, err := s.recipes.Catalog(ctx, householdID)
	if err != nil {
		return Vocabulary{}, fmt.Errorf("load catalog: %w", err)
	}
	return buildVocabulary(catalog), nil
}

// --- week context -----------------------------------------------------------------

// WeekContext returns a week's context, or an empty one.
func (s *Service) WeekContext(ctx context.Context, householdID, week string) (WeekContext, error) {
	w, err := planning.ParseWeek(week)
	if err != nil {
		return WeekContext{}, err
	}
	c, err := s.store.GetWeekContext(ctx, householdID, w.String())
	if errors.Is(err, ErrNotFound) {
		return WeekContext{HouseholdID: householdID, Week: w.String()}, nil
	}
	return c, err
}

// SaveWeekContext replaces a week's context and records what changed.
func (s *Service) SaveWeekContext(ctx context.Context, householdID, userID, week string, in WeekContext) (WeekContext, error) {
	if err := required(householdID, userID); err != nil {
		return WeekContext{}, err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return WeekContext{}, err
	}
	next, err := normalizeWeekContext(in)
	if err != nil {
		return WeekContext{}, err
	}
	for range maxAttempts {
		current, err := s.WeekContext(ctx, householdID, w.String())
		if err != nil {
			return WeekContext{}, err
		}
		next.HouseholdID, next.Week, next.Version = householdID, w.String(), current.Version
		changes := diffWeekContext(current, next)
		if current.Configured() && len(changes) == 0 {
			return current, nil
		}
		now := s.now().UTC()
		next.UpdatedBy, next.UpdatedAt = userID, now
		saved, err := s.store.SaveWeekContext(ctx, next)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return WeekContext{}, err
		}
		s.record(ctx, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypeAutopilotWeekContextUpdated, Week: w.String(), OccurredAt: now,
			Payload: events.AutopilotWeekContextUpdated{Changes: changes[:min(len(changes), events.MaxFieldChanges)]},
		})
		return saved, nil
	}
	return WeekContext{}, ErrConflict
}

// ClearWeekContext removes a week's context. Clearing a week without one does
// nothing.
func (s *Service) ClearWeekContext(ctx context.Context, householdID, userID, week string) error {
	if err := required(householdID, userID); err != nil {
		return err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return err
	}
	if _, err := s.store.DeleteWeekContext(ctx, householdID, w.String()); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	s.record(ctx, events.Event{
		HouseholdID: householdID, UserID: userID, Type: events.TypeAutopilotWeekContextUpdated, Week: w.String(),
		OccurredAt: s.now().UTC(), Payload: events.AutopilotWeekContextUpdated{Cleared: true},
	})
	return nil
}

// --- proposals --------------------------------------------------------------------

// GenerateOptions tunes Generate.
type GenerateOptions struct {
	// AvoidPrevious steers away from a pending proposal's meals when
	// generating again. Nil means true.
	AvoidPrevious *bool
}

// loadWeek parses the week and loads its plan, failing for a finalized plan.
func (s *Service) loadWeek(ctx context.Context, householdID, week string) (planning.Week, planning.Plan, error) {
	w, err := planning.ParseWeek(week)
	if err != nil {
		return planning.Week{}, planning.Plan{}, err
	}
	plan, err := s.plans.Get(ctx, householdID, w.String())
	if err != nil {
		return planning.Week{}, planning.Plan{}, fmt.Errorf("load plan: %w", err)
	}
	if plan.Status == planning.StatusFinalized {
		return planning.Week{}, planning.Plan{}, ErrPlanFinalized
	}
	return w, plan, nil
}

// prepareInput loads the profile and week context and builds provider input.
func (s *Service) prepareInput(ctx context.Context, householdID string, w planning.Week, plan planning.Plan) (autopilot.Input, inputData, error) {
	profile, err := s.Profile(ctx, householdID)
	if err != nil {
		return autopilot.Input{}, inputData{}, err
	}
	wc, err := s.WeekContext(ctx, householdID, w.String())
	if err != nil {
		return autopilot.Input{}, inputData{}, err
	}
	return s.buildInput(ctx, householdID, w, profile, wc, plan)
}

// Generate proposes a week and saves it as the week's proposal, replacing any
// earlier one. Meals already in the plan are kept (their days aren't
// planned). A finalized plan fails with ErrPlanFinalized.
func (s *Service) Generate(ctx context.Context, householdID, userID, week string, opts GenerateOptions) (Proposal, error) {
	if err := required(householdID, userID); err != nil {
		return Proposal{}, err
	}
	w, plan, err := s.loadWeek(ctx, householdID, week)
	if err != nil {
		return Proposal{}, err
	}
	previous, err := s.store.GetProposal(ctx, householdID, w.String())
	hasPrevious := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Proposal{}, err
	}
	in, data, err := s.prepareInput(ctx, householdID, w, plan)
	if err != nil {
		return Proposal{}, err
	}
	req := autopilot.WeekRequest{Input: in, Attempt: 1}
	pending := hasPrevious && previous.Status == StatusProposed
	if hasPrevious {
		req.Attempt = previous.Attempt + 1
		if pending && (opts.AvoidPrevious == nil || *opts.AvoidPrevious) {
			for _, sl := range previous.Slots {
				req.Avoid = append(req.Avoid, sl.RecipeID)
				req.Avoid = append(req.Avoid, sl.RejectedRecipeIDs...)
			}
		}
	}
	res, err := s.provider.GenerateWeek(ctx, req)
	if err != nil {
		return Proposal{}, fmt.Errorf("generate week: %w", err)
	}
	now := s.now().UTC()
	p := Proposal{
		ID: newID(), HouseholdID: householdID, Week: w.String(), Status: StatusProposed, Attempt: req.Attempt,
		ModelVersion: res.ModelVersion, InputsHash: inputsHash(req), Requested: res.Requested, Planned: res.Planned,
		Candidates: res.Candidates, ColdStart: res.ColdStart, GeneratedBy: userID, GeneratedAt: now, UpdatedAt: now,
		Objective: Objective{
			Meals: res.Score.Meals, Variety: res.Score.Variety, CookTime: res.Score.CookTime,
			Rules: res.Score.Rules, Novelty: res.Score.Novelty, Total: res.Score.Total,
		},
	}
	if hasPrevious {
		p.Version = previous.Version
	}
	for _, sl := range res.Slots {
		p.Slots = append(p.Slots, data.slot(sl))
	}
	for _, u := range res.Unfilled {
		p.Unfilled = append(p.Unfilled, Unfilled{Day: string(u.Day), Code: u.Code, Text: u.Text})
	}
	for _, m := range res.Messages {
		p.Messages = append(p.Messages, Message{Code: m.Code, Text: m.Text})
	}
	saved, err := s.store.SaveProposal(ctx, p)
	if errors.Is(err, ErrConflict) {
		return Proposal{}, ErrProposalChanged
	}
	if err != nil {
		return Proposal{}, err
	}
	generated := events.WeekGenerated{
		ProposalID: saved.ID, ModelVersion: saved.ModelVersion, Attempt: saved.Attempt, Requested: saved.Requested,
		Planned: saved.Planned, Unfilled: len(saved.Unfilled), Candidates: saved.Candidates, ColdStart: saved.ColdStart,
	}
	if pending {
		generated.ReplacedProposalID = previous.ID
		s.record(ctx, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypeWeekRejected, Week: w.String(), OccurredAt: now,
			Payload: events.WeekRejected{ProposalID: previous.ID, ModelVersion: previous.ModelVersion, Planned: previous.Planned, Swaps: previous.SwapCount, Reason: "regenerated"},
		})
	}
	s.record(ctx, events.Event{HouseholdID: householdID, UserID: userID, Type: events.TypeWeekGenerated, Week: w.String(), OccurredAt: now, Payload: generated})
	return saved, nil
}

// Proposal returns the week's proposal, or ErrNotFound.
func (s *Service) Proposal(ctx context.Context, householdID, week string) (Proposal, error) {
	w, err := planning.ParseWeek(week)
	if err != nil {
		return Proposal{}, err
	}
	return s.store.GetProposal(ctx, householdID, w.String())
}

// pending loads a pending proposal at the client's version.
func (s *Service) pending(ctx context.Context, householdID string, w planning.Week, version int64) (Proposal, error) {
	p, err := s.store.GetProposal(ctx, householdID, w.String())
	switch {
	case err != nil:
		return Proposal{}, err
	case p.Version != version:
		return Proposal{}, ErrProposalChanged
	case p.Status != StatusProposed:
		return Proposal{}, ErrProposalNotPending
	}
	return p, nil
}

// Swap replaces one slot's meal with the next best alternative that honors
// the same constraints, taking the rest of the week (plan entries and the
// other proposed meals) into account. Meals swapped out of a slot are never
// offered for it again.
func (s *Service) Swap(ctx context.Context, householdID, userID, week, slotID string, version int64) (Proposal, error) {
	if err := required(householdID, userID); err != nil {
		return Proposal{}, err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return Proposal{}, err
	}
	if !slices.Contains(optionValues(DayOptions), slotID) {
		return Proposal{}, invalidf("slotId must be a day code: mon, tue, wed, thu, fri, sat, or sun")
	}
	p, err := s.pending(ctx, householdID, w, version)
	if err != nil {
		return Proposal{}, err
	}
	i := p.slot(slotID)
	if i < 0 {
		return Proposal{}, fmt.Errorf("%w: the proposal has no meal on %q", ErrNotFound, slotID)
	}
	_, plan, err := s.loadWeek(ctx, householdID, w.String())
	if err != nil {
		return Proposal{}, err
	}
	in, data, err := s.prepareInput(ctx, householdID, w, plan)
	if err != nil {
		return Proposal{}, err
	}
	current := p.Slots[i]
	for j, other := range p.Slots {
		if j != i {
			in.Fixed = append(in.Fixed, autopilot.Assignment{ItemID: other.RecipeID, Day: autopilot.Day(other.Day)})
		}
	}
	res, err := s.provider.RankMeals(ctx, autopilot.RankRequest{
		Input: in, Day: autopilot.Day(current.Day), Exclude: append([]string{current.RecipeID}, current.RejectedRecipeIDs...), Limit: 1,
	})
	if err != nil {
		return Proposal{}, fmt.Errorf("rank meals: %w", err)
	}
	if len(res.Items) == 0 {
		msg := "no other recipe fits this day"
		if len(res.Messages) > 0 {
			msg = res.Messages[0].Text
		}
		return Proposal{}, fmt.Errorf("%w: %s", ErrNoAlternative, msg)
	}
	next := data.slot(autopilot.Slot{Day: autopilot.Day(current.Day), Recommendation: res.Items[0]})
	next.SwapCount = current.SwapCount + 1
	next.RejectedRecipeIDs = append(slices.Clone(current.RejectedRecipeIDs), current.RecipeID)
	if n := len(next.RejectedRecipeIDs); n > MaxRejectedPerSlot {
		next.RejectedRecipeIDs = next.RejectedRecipeIDs[n-MaxRejectedPerSlot:]
	}
	now := s.now().UTC()
	p.Slots = slices.Clone(p.Slots)
	p.Slots[i] = next
	p.SwapCount++
	p.UpdatedAt = now
	saved, err := s.store.SaveProposal(ctx, p)
	if errors.Is(err, ErrConflict) {
		return Proposal{}, ErrProposalChanged
	}
	if err != nil {
		return Proposal{}, err
	}
	s.record(ctx, events.Event{
		HouseholdID: householdID, UserID: userID, Type: events.TypeMealSwapped, RecipeID: next.RecipeID, Week: w.String(), OccurredAt: now,
		Payload: events.MealSwapped{
			ProposalID: saved.ID, SlotID: next.ID, Day: next.Day, Date: w.Date(planning.Day(next.Day)),
			PreviousRecipeID: current.RecipeID, ModelVersion: res.ModelVersion, SwapNumber: next.SwapCount,
		},
	})
	return saved, nil
}

// SkippedSlot is a proposed meal that accepting didn't add.
type SkippedSlot struct {
	SlotID string
	Day    string
	// Reason is dayTaken (the plan already has a meal that day) or
	// alreadyPlanned (the recipe is already in the week).
	Reason string
}

// Skip reasons.
const (
	SkipDayTaken       = "dayTaken"
	SkipAlreadyPlanned = "alreadyPlanned"
)

// AcceptResult is the outcome of accepting a proposal.
type AcceptResult struct {
	Proposal Proposal
	Plan     planning.Plan
	Added    []planning.Entry
	Skipped  []SkippedSlot
}

// Accept adds the proposal's meals to the week's draft plan as autopilot
// entries, in one atomic change, except the excluded slots. Meals whose day
// has been planned since, or whose recipe is already in the week, are
// skipped: accepting never replaces or duplicates entries.
func (s *Service) Accept(ctx context.Context, householdID, userID, week string, version int64, excludeSlotIDs []string) (AcceptResult, error) {
	if err := required(householdID, userID); err != nil {
		return AcceptResult{}, err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return AcceptResult{}, err
	}
	p, err := s.pending(ctx, householdID, w, version)
	if err != nil {
		return AcceptResult{}, err
	}
	var excluded []string
	for _, id := range excludeSlotIDs {
		if p.slot(id) < 0 {
			return AcceptResult{}, invalidf("excludeSlotIds: %q is not a slot of this proposal", id)
		}
		if !slices.Contains(excluded, id) {
			excluded = append(excluded, id)
		}
	}
	_, plan, err := s.loadWeek(ctx, householdID, w.String())
	if err != nil {
		return AcceptResult{}, err
	}
	var entries []planning.NewEntry
	var skipped []SkippedSlot
	for _, sl := range p.Slots {
		switch {
		case slices.Contains(excluded, sl.ID):
		case slices.ContainsFunc(plan.Entries, func(e planning.Entry) bool { return string(e.Day) == sl.Day }):
			skipped = append(skipped, SkippedSlot{SlotID: sl.ID, Day: sl.Day, Reason: SkipDayTaken})
		case slices.ContainsFunc(plan.Entries, func(e planning.Entry) bool { return e.RecipeID == sl.RecipeID }):
			skipped = append(skipped, SkippedSlot{SlotID: sl.ID, Day: sl.Day, Reason: SkipAlreadyPlanned})
		default:
			entries = append(entries, planning.NewEntry{
				RecipeID: sl.RecipeID, Day: sl.Day, Servings: sl.Servings, Origin: planning.OriginAutopilot, ProposalID: p.ID,
			})
		}
	}
	if len(entries) == 0 {
		return AcceptResult{}, ErrNothingToAccept
	}

	// Claim the proposal first, so two members accepting at once can't both
	// add its meals; give the claim back if the plan change fails.
	now := s.now().UTC()
	claim := p
	claim.Status, claim.DecidedBy, claim.DecidedAt, claim.UpdatedAt, claim.ExcludedSlots = StatusAccepted, userID, now, now, excluded
	saved, err := s.store.SaveProposal(ctx, claim)
	if errors.Is(err, ErrConflict) {
		return AcceptResult{}, ErrProposalChanged
	}
	if err != nil {
		return AcceptResult{}, err
	}
	updated, added, err := s.plans.AddEntries(ctx, householdID, userID, w.String(), entries)
	if err != nil {
		revert := saved
		revert.Status, revert.DecidedBy, revert.DecidedAt, revert.ExcludedSlots = StatusProposed, "", time.Time{}, nil
		if _, rerr := s.store.SaveProposal(ctx, revert); rerr != nil {
			s.logger.ErrorContext(ctx, "reopen proposal after a failed accept", "householdId", householdID, "week", w.String(), "error", rerr)
		}
		switch {
		case errors.Is(err, planning.ErrFinalized):
			return AcceptResult{}, ErrPlanFinalized
		case errors.Is(err, planning.ErrRecipeNotFound), errors.Is(err, planning.ErrInvalidEntry):
			return AcceptResult{}, fmt.Errorf("%w: %v", ErrProposalStale, err)
		}
		return AcceptResult{}, err
	}
	s.record(ctx, events.Event{
		HouseholdID: householdID, UserID: userID, Type: events.TypeWeekAccepted, Week: w.String(), OccurredAt: now,
		Payload: events.WeekAccepted{
			ProposalID: saved.ID, ModelVersion: saved.ModelVersion, Planned: len(saved.Slots), Added: len(added),
			Excluded: len(excluded), Skipped: len(skipped), Swaps: saved.SwapCount,
		},
	})
	for _, id := range excluded {
		sl := saved.Slots[saved.slot(id)]
		s.record(ctx, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypeMealRejected, RecipeID: sl.RecipeID, Week: w.String(), OccurredAt: now,
			Payload: events.MealRejected{ProposalID: saved.ID, SlotID: sl.ID, Day: sl.Day, Date: w.Date(planning.Day(sl.Day)), ModelVersion: saved.ModelVersion},
		})
	}
	return AcceptResult{Proposal: saved, Plan: updated, Added: added, Skipped: skipped}, nil
}

// Reject dismisses a pending proposal.
func (s *Service) Reject(ctx context.Context, householdID, userID, week string, version int64) (Proposal, error) {
	if err := required(householdID, userID); err != nil {
		return Proposal{}, err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return Proposal{}, err
	}
	p, err := s.pending(ctx, householdID, w, version)
	if err != nil {
		return Proposal{}, err
	}
	now := s.now().UTC()
	p.Status, p.DecidedBy, p.DecidedAt, p.UpdatedAt = StatusRejected, userID, now, now
	saved, err := s.store.SaveProposal(ctx, p)
	if errors.Is(err, ErrConflict) {
		return Proposal{}, ErrProposalChanged
	}
	if err != nil {
		return Proposal{}, err
	}
	s.record(ctx, events.Event{
		HouseholdID: householdID, UserID: userID, Type: events.TypeWeekRejected, Week: w.String(), OccurredAt: now,
		Payload: events.WeekRejected{ProposalID: saved.ID, ModelVersion: saved.ModelVersion, Planned: saved.Planned, Swaps: saved.SwapCount, Reason: "dismissed"},
	})
	return saved, nil
}

// --- recipe attributes ------------------------------------------------------------

func (s *Service) recipe(ctx context.Context, householdID, recipeID string) (recipes.Recipe, error) {
	r, err := s.recipes.Get(ctx, householdID, recipeID)
	if errors.Is(err, recipes.ErrNotFound) {
		return recipes.Recipe{}, fmt.Errorf("%w: recipe not found", ErrNotFound)
	}
	return r, err
}

// RecipeAttributes returns what Autopilot derives from a recipe, with the
// household's overrides applied.
func (s *Service) RecipeAttributes(ctx context.Context, householdID, recipeID string) (RecipeAttributes, error) {
	r, err := s.recipe(ctx, householdID, recipeID)
	if err != nil {
		return RecipeAttributes{}, err
	}
	profile, err := s.Profile(ctx, householdID)
	if err != nil {
		return RecipeAttributes{}, err
	}
	var override *RecipeOverride
	if o, err := s.store.GetOverride(ctx, householdID, recipeID); err == nil {
		override = &o
	} else if !errors.Is(err, ErrNotFound) {
		return RecipeAttributes{}, err
	}
	return attributes(r, override, profile.bands()), nil
}

func overrideValue(methods map[string]bool, method string) string {
	v, ok := methods[method]
	switch {
	case !ok:
		return "auto"
	case v:
		return "yes"
	}
	return "no"
}

// SetRecipeOverride says whether a recipe suits cooking methods. A nil value
// removes the method's override, returning it to the heuristic. Each changed
// method records an autopilot.recipe_override_updated event.
func (s *Service) SetRecipeOverride(ctx context.Context, householdID, userID, recipeID string, methods map[string]*bool) (RecipeAttributes, error) {
	if err := required(householdID, userID); err != nil {
		return RecipeAttributes{}, err
	}
	if len(methods) == 0 {
		return RecipeAttributes{}, invalidf("methods must name at least one method")
	}
	allowed := optionValues(EquipmentOptions)
	names := slices.Sorted(maps.Keys(methods))
	for _, m := range names {
		if !slices.Contains(allowed, m) {
			return RecipeAttributes{}, invalidf("methods: %q must be one of %v", m, allowed)
		}
	}
	if _, err := s.recipe(ctx, householdID, recipeID); err != nil {
		return RecipeAttributes{}, err
	}
	current, err := s.store.GetOverride(ctx, householdID, recipeID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return RecipeAttributes{}, err
	}
	next := maps.Clone(current.Methods)
	if next == nil {
		next = map[string]bool{}
	}
	type change struct{ method, value, previous string }
	var changes []change
	for _, m := range names {
		previous := overrideValue(current.Methods, m)
		if v := methods[m]; v == nil {
			delete(next, m)
		} else {
			next[m] = *v
		}
		if value := overrideValue(next, m); value != previous {
			changes = append(changes, change{m, value, previous})
		}
	}
	if len(changes) > 0 {
		now := s.now().UTC()
		o := RecipeOverride{HouseholdID: householdID, RecipeID: recipeID, Methods: next, UpdatedBy: userID, UpdatedAt: now}
		if err := s.store.SaveOverride(ctx, o); err != nil {
			return RecipeAttributes{}, err
		}
		for _, c := range changes {
			s.record(ctx, events.Event{
				HouseholdID: householdID, UserID: userID, Type: events.TypeAutopilotRecipeOverrideUpdated, RecipeID: recipeID, OccurredAt: now,
				Payload: events.AutopilotRecipeOverrideUpdated{Method: c.method, Value: c.value, Previous: c.previous},
			})
		}
	}
	return s.RecipeAttributes(ctx, householdID, recipeID)
}

// RecipeOverrides lists the household's recipe overrides.
func (s *Service) RecipeOverrides(ctx context.Context, householdID string) ([]RecipeOverride, error) {
	return s.store.ListOverrides(ctx, householdID)
}
