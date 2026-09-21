package mealkit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
)

// Notifier raises the household notifications an import produces. The push
// sweep (cmd/sendreminders) delivers them to members' phones.
type Notifier interface {
	Create(ctx context.Context, in notifications.New) (notifications.Notification, bool, error)
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Store Store
	// Enabled turns the feature on (MEAL_KIT_IMPORT_ENABLED). When it is
	// false every call returns ErrDisabled and the app offers adding recipes
	// by hand instead.
	Enabled bool
	// Sources are the meal-kit services this deployment can import from,
	// keyed by Source.Name().
	Sources map[string]Source
	// Notifier, when set, raises the finished and needs-attention
	// notifications.
	Notifier Notifier
	Logger   *slog.Logger
	// MaxAttempts overrides DefaultMaxAttempts.
	MaxAttempts int
}

// Service queues meal-kit import runs. It fetches nothing and stores no
// credential: the order history arrives already harvested, in the member's own
// browser session, and the work of reading public recipe pages is the
// Worker's.
type Service struct {
	store    Store
	enabled  bool
	sources  map[string]Source
	notifier Notifier
	logger   *slog.Logger
	attempts int
	now      func() time.Time
}

// NewService returns a Service.
func NewService(opts ServiceOptions) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	attempts := opts.MaxAttempts
	if attempts <= 0 {
		attempts = DefaultMaxAttempts
	}
	return &Service{
		store: opts.Store, enabled: opts.Enabled, sources: opts.Sources,
		notifier: opts.Notifier, logger: logger, attempts: attempts, now: time.Now,
	}
}

// Enabled reports whether the feature is configured.
func (s *Service) Enabled() bool { return s != nil && s.enabled && len(s.sources) > 0 }

// Source returns the configured source, or ErrNotFound.
func (s *Service) Source(name string) (Source, error) {
	src, ok := s.sources[name]
	if !ok || src == nil {
		return nil, ErrNotFound
	}
	return src, nil
}

// Status is what the app shows: the household's newest import run, if any,
// and how far back its harvests have read.
type Status struct {
	Job *Job
	// Cursor is where the next harvest should resume from. Its zero value
	// means no harvest has ever run, which the app reads as "start at today".
	Cursor Cursor
}

// Status returns the household's newest job for source and its harvest
// cursor.
func (s *Service) Status(ctx context.Context, householdID, source string) (Status, error) {
	if householdID == "" {
		return Status{}, errHouseholdRequired
	}
	if !KnownSource(source) {
		return Status{}, invalid("source must be %q", SourceHelloFresh)
	}
	var out Status
	job, err := s.store.LatestJob(ctx, householdID, source)
	switch {
	case err == nil:
		out.Job = &job
	case errors.Is(err, ErrNotFound):
	default:
		return Status{}, fmt.Errorf("get meal-kit job: %w", err)
	}
	cursor, err := s.cursor(ctx, householdID, source)
	if err != nil {
		return Status{}, err
	}
	out.Cursor = cursor
	return out, nil
}

// cursor reads the household's harvest cursor, treating "never harvested" as
// the zero cursor rather than an error.
func (s *Service) cursor(ctx context.Context, householdID, source string) (Cursor, error) {
	c, err := s.store.GetCursor(ctx, householdID, source)
	switch {
	case err == nil:
		return c, nil
	case errors.Is(err, ErrNotFound):
		return Cursor{HouseholdID: householdID, Source: source}, nil
	default:
		return Cursor{}, fmt.Errorf("get meal-kit cursor: %w", err)
	}
}

// ListJobs returns the household's import runs, newest first.
func (s *Service) ListJobs(ctx context.Context, householdID string, limit int) ([]Job, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	return s.store.ListJobs(ctx, householdID, limit)
}

// ImportRequest is a member handing us the order history their own browser
// session just read.
//
// Orders is the whole of it. There is no credential, no cookie, and nothing
// about the account: the app is not allowed to send one and this service has
// nowhere to put one.
type ImportRequest struct {
	HouseholdID string
	UserID      string
	Source      string
	Orders      []OrderedRecipe
	// Harvest is where the walk that produced Orders stopped: the oldest week
	// it reached and why it stopped. It is how a history too long for one
	// harvest is finished over several (cursor.go). An absent report is
	// accepted and simply teaches the server nothing.
	Harvest HarvestReport
}

// StartImport validates a harvested order history and queues a run for it.
//
// An import that is already queued or running is returned as is rather than
// duplicated, so a member tapping twice never doubles the work — and the newly
// harvested list is simply dropped, because the run already in flight is
// reading the same account. A run in flight on *another* source is refused
// outright: one household is one polite conversation at a time.
//
// It also refuses to queue anything for BlockedCooldown after the source
// turned us away. Being refused is the one failure that must not be argued
// with, so it is surfaced to the member instead of retried.
//
// Running it again later is safe and is how a re-sync works: the recipes
// pipeline matches on source and source recipe id, so recipes already in the
// library come back as unchanged or updated, never as duplicates. What the
// second run does *not* do is walk the whole history again: the harvest
// cursor (cursor.go) tells the app where to resume, and this is where the
// report of that walk is folded back in.
func (s *Service) StartImport(ctx context.Context, req ImportRequest) (Job, error) {
	if !s.Enabled() {
		return Job{}, ErrDisabled
	}
	if req.HouseholdID == "" {
		return Job{}, errHouseholdRequired
	}
	if !KnownSource(req.Source) {
		return Job{}, invalid("source must be %q", SourceHelloFresh)
	}
	src, err := s.Source(req.Source)
	if err != nil {
		return Job{}, invalid("%s imports are not available on this server", req.Source)
	}
	orders, err := normalizeOrders(src, req.Orders)
	if err != nil {
		return Job{}, err
	}
	harvest, err := req.Harvest.Validate()
	if err != nil {
		return Job{}, err
	}

	if existing, err := s.store.ActiveJob(ctx, req.HouseholdID, req.Source); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Job{}, fmt.Errorf("get active import job: %w", err)
	}
	if other, err := s.store.AnyActiveJob(ctx, req.HouseholdID); err == nil {
		return Job{}, invalid("an import from %s is already running for this household; it has to finish first", other.Source)
	} else if !errors.Is(err, ErrNotFound) {
		return Job{}, fmt.Errorf("get active import job: %w", err)
	}

	cursor, err := s.cursor(ctx, req.HouseholdID, req.Source)
	if err != nil {
		return Job{}, err
	}
	now := s.now().UTC().Truncate(time.Millisecond)
	if cursor.Blocked(now) {
		return Job{}, invalid(
			"%s asked us to stop, so we are leaving them alone for a few hours. Try again later.",
			displayName(req.Source))
	}
	job, err := s.enqueue(ctx, req, orders, harvest, now)
	if err != nil {
		return Job{}, err
	}
	// The cursor moves only once the run is really queued, so a harvest that
	// was refused never advances the floor and is not silently lost.
	if err := s.store.SaveCursor(ctx, cursor.Merge(harvest, now)); err != nil {
		// The recipes are queued; losing the cursor costs a re-walk next
		// time, not a recipe. It is logged, not returned.
		s.logger.ErrorContext(ctx, "saving the meal-kit harvest cursor failed",
			"source", req.Source, "householdId", req.HouseholdID, "error", err)
	}
	return job, nil
}

// displayName is the service's name as a member would write it.
func displayName(source string) string {
	if source == SourceHelloFresh {
		return "HelloFresh"
	}
	return "the meal-kit service"
}

// normalizeOrders puts every submitted entry through the source's own door and
// keeps the ones that survive. Entries this build will not fetch are dropped
// silently rather than failing the whole import: one odd row in a harvested
// history should not cost a member their other two hundred recipes.
func normalizeOrders(src Source, in []OrderedRecipe) ([]OrderedRecipe, error) {
	if len(in) == 0 {
		return nil, invalid("there were no recipes to import")
	}
	if len(in) > MaxOrderedRecipes {
		return nil, invalid("that is more than %d recipes; something is wrong with the order history", MaxOrderedRecipes)
	}
	out := make([]OrderedRecipe, 0, len(in))
	seen := make(map[string]int, len(in))
	for _, raw := range in {
		o, ok := src.NormalizeOrder(raw)
		if !ok {
			continue
		}
		if at, dup := seen[o.SourceRecipeID]; dup {
			out[at].Weeks = mergeWeeks(out[at].Weeks, o.Weeks)
			continue
		}
		seen[o.SourceRecipeID] = len(out)
		out = append(out, o)
	}
	if len(out) == 0 {
		return nil, invalid("none of those looked like %s recipes", SourceHelloFresh)
	}
	return out, nil
}

func mergeWeeks(into, extra []string) []string {
	have := make(map[string]bool, len(into))
	for _, w := range into {
		have[w] = true
	}
	for _, w := range extra {
		if !have[w] && len(into) < MaxOrderWeeks {
			have[w] = true
			into = append(into, w)
		}
	}
	return into
}

func (s *Service) enqueue(
	ctx context.Context, req ImportRequest, orders []OrderedRecipe, harvest HarvestReport, now time.Time,
) (Job, error) {
	job, err := s.store.InsertJob(ctx, Job{
		HouseholdID: req.HouseholdID, UserID: strings.TrimSpace(req.UserID), Source: req.Source,
		Status: JobQueued, MaxAttempts: s.attempts, AvailableAt: now,
		Checkpoint: Checkpoint{Phase: PhaseRecipes, Orders: orders},
		Harvest:    harvest,
		CreatedAt:  now, UpdatedAt: now,
	})
	if err != nil {
		return Job{}, fmt.Errorf("enqueue import job: %w", err)
	}
	// A partial harvest is queued exactly like a whole one: the recipes it
	// did reach belong in the library now, not after however many more
	// sign-ins the rest of the history takes.
	s.logger.InfoContext(ctx, "meal-kit import queued",
		"source", req.Source, "householdId", req.HouseholdID, "jobId", job.ID, "recipes", len(orders),
		"earliestWeek", harvest.EarliestWeek, "stopped", string(harvest.Stopped))
	return job, nil
}

// StopImports cancels every run in flight for the household's source.
//
// There is nothing to unlink any more — no stored credential, nothing about
// the account — so this is all "disconnect" can honestly mean. Recipes already
// imported stay in the library, which is the point of having imported them.
func (s *Service) StopImports(ctx context.Context, householdID, source string) (canceled int, err error) {
	if householdID == "" {
		return 0, errHouseholdRequired
	}
	if !KnownSource(source) {
		return 0, invalid("source must be %q", SourceHelloFresh)
	}
	now := s.now().UTC().Truncate(time.Millisecond)
	canceled, err = s.store.CancelJobs(ctx, householdID, source, "the import was stopped", now)
	if err != nil {
		return 0, fmt.Errorf("cancel import jobs: %w", err)
	}
	s.logger.InfoContext(ctx, "meal-kit imports stopped", "source", source, "householdId", householdID, "jobsCanceled", canceled)
	return canceled, nil
}
