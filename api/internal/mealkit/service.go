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

// Status is what the app shows: the household's newest import run, if any.
type Status struct {
	Job *Job
}

// Status returns the household's newest job for source.
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
	return out, nil
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
}

// StartImport validates a harvested order history and queues a run for it.
//
// An import that is already queued or running is returned as is rather than
// duplicated, so a member tapping twice never doubles the work — and the newly
// harvested list is simply dropped, because the run already in flight is
// reading the same account.
//
// Running it again later is safe and is how a re-sync works: the recipes
// pipeline matches on source and source recipe id, so recipes already in the
// library come back as unchanged or updated, never as duplicates.
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

	if existing, err := s.store.ActiveJob(ctx, req.HouseholdID, req.Source); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Job{}, fmt.Errorf("get active import job: %w", err)
	}
	return s.enqueue(ctx, req, orders)
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

func (s *Service) enqueue(ctx context.Context, req ImportRequest, orders []OrderedRecipe) (Job, error) {
	now := s.now().UTC().Truncate(time.Millisecond)
	job, err := s.store.InsertJob(ctx, Job{
		HouseholdID: req.HouseholdID, UserID: strings.TrimSpace(req.UserID), Source: req.Source,
		Status: JobQueued, MaxAttempts: s.attempts, AvailableAt: now,
		Checkpoint: Checkpoint{Phase: PhaseRecipes, Orders: orders},
		CreatedAt:  now, UpdatedAt: now,
	})
	if err != nil {
		return Job{}, fmt.Errorf("enqueue import job: %w", err)
	}
	s.logger.InfoContext(ctx, "meal-kit import queued",
		"source", req.Source, "householdId", req.HouseholdID, "jobId", job.ID, "recipes", len(orders))
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
