package mealkit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
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
	// Cipher encrypts stored tokens. Nil disables the feature: every call
	// returns ErrDisabled, which is what an API started without
	// RECIPE_IMPORT_ENCRYPTION_KEY does.
	Cipher *Cipher
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

// Service links meal-kit accounts and enqueues import jobs. It never fetches
// anything itself beyond the one sign-in: the work is the Worker's.
type Service struct {
	store    Store
	cipher   *Cipher
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
		store: opts.Store, cipher: opts.Cipher, sources: opts.Sources,
		notifier: opts.Notifier, logger: logger, attempts: attempts, now: time.Now,
	}
}

// Enabled reports whether the feature is configured. Without an encryption
// key nothing here accepts a credential.
func (s *Service) Enabled() bool { return s != nil && s.cipher != nil }

// Source returns the configured source, or ErrNotFound.
func (s *Service) Source(name string) (Source, error) {
	src, ok := s.sources[name]
	if !ok || src == nil {
		return nil, ErrNotFound
	}
	return src, nil
}

// Status is what the app shows: the household's link, if any, and the newest
// import run, if any.
type Status struct {
	Link *Link
	Job  *Job
}

// Status returns the household's link and newest job for source.
func (s *Service) Status(ctx context.Context, householdID, source string) (Status, error) {
	if householdID == "" {
		return Status{}, errHouseholdRequired
	}
	if !KnownSource(source) {
		return Status{}, invalid("source must be %q", SourceHelloFresh)
	}
	var out Status
	link, err := s.store.GetLink(ctx, householdID, source)
	switch {
	case err == nil:
		out.Link = &link
	case errors.Is(err, ErrNotFound):
	default:
		return Status{}, fmt.Errorf("get meal-kit link: %w", err)
	}
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

// LinkRequest is a member connecting their meal-kit account.
//
// Password is used once, here, to obtain session tokens and is then gone: it
// is not stored, not logged, and not kept in the returned values. That is the
// whole reason this method exists rather than the app handing us a password
// per run.
type LinkRequest struct {
	HouseholdID string
	UserID      string
	Source      string
	Email       string
	Password    string
	// StartImport enqueues a run as soon as the link is stored.
	StartImport bool
}

// Link signs in to the meal-kit account once, stores only the resulting
// session and refresh tokens (encrypted), and optionally enqueues an import.
//
// A link that already existed is replaced, and any job that was paused
// waiting for a new sign-in becomes runnable again.
func (s *Service) Link(ctx context.Context, req LinkRequest) (Status, error) {
	if !s.Enabled() {
		return Status{}, ErrDisabled
	}
	if req.HouseholdID == "" {
		return Status{}, errHouseholdRequired
	}
	if !KnownSource(req.Source) {
		return Status{}, invalid("source must be %q", SourceHelloFresh)
	}
	email := strings.TrimSpace(req.Email)
	if _, err := mail.ParseAddress(email); err != nil {
		return Status{}, invalid("email must be the address you sign in to %s with", req.Source)
	}
	if strings.TrimSpace(req.Password) == "" {
		return Status{}, invalid("password is required")
	}
	src, err := s.Source(req.Source)
	if err != nil {
		return Status{}, invalid("%s imports are not available on this server", req.Source)
	}

	tokens, err := src.SignIn(ctx, email, req.Password)
	if err != nil {
		// The password never reaches a log line, and neither does the error
		// from the source, which could echo the request.
		s.logger.InfoContext(ctx, "meal-kit sign-in failed", "source", req.Source, "householdId", req.HouseholdID, "reason", signInReason(err))
		return Status{}, signInError(err)
	}
	if tokens.Empty() {
		return Status{}, invalid("%s did not return a session; check the email and password", req.Source)
	}

	secret, err := s.cipher.SealTokens(tokens)
	if err != nil {
		return Status{}, err
	}
	now := s.now().UTC().Truncate(time.Millisecond)
	link, err := s.store.UpsertLink(ctx, Link{
		HouseholdID: req.HouseholdID, UserID: req.UserID, Source: req.Source,
		Status: LinkActive, AccountLabel: accountLabel(req.Source),
		Secret: secret, ExpiresAt: tokens.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Status{}, fmt.Errorf("store meal-kit link: %w", err)
	}

	// A run that paused waiting for this sign-in picks up where it stopped.
	resumed, err := s.store.ResumePausedJobs(ctx, req.HouseholdID, req.Source, now)
	if err != nil {
		return Status{}, fmt.Errorf("resume paused import jobs: %w", err)
	}

	out := Status{Link: &link}
	if resumed == 0 && req.StartImport {
		job, err := s.enqueue(ctx, link, req.UserID, now)
		if err != nil {
			return Status{}, err
		}
		out.Job = &job
	} else {
		if job, err := s.store.LatestJob(ctx, req.HouseholdID, req.Source); err == nil {
			out.Job = &job
		}
	}
	return out, nil
}

// StartImport enqueues a run for the household's linked account. An import
// that is already queued, running, or paused is returned as is rather than
// duplicated, so a member tapping twice never doubles the work.
func (s *Service) StartImport(ctx context.Context, householdID, userID, source string) (Job, error) {
	if !s.Enabled() {
		return Job{}, ErrDisabled
	}
	if householdID == "" {
		return Job{}, errHouseholdRequired
	}
	if !KnownSource(source) {
		return Job{}, invalid("source must be %q", SourceHelloFresh)
	}
	link, err := s.store.GetLink(ctx, householdID, source)
	if errors.Is(err, ErrNotFound) {
		return Job{}, ErrNoLink
	}
	if err != nil {
		return Job{}, fmt.Errorf("get meal-kit link: %w", err)
	}
	if existing, err := s.store.ActiveJob(ctx, householdID, source); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Job{}, fmt.Errorf("get active import job: %w", err)
	}
	return s.enqueue(ctx, link, userID, s.now().UTC().Truncate(time.Millisecond))
}

func (s *Service) enqueue(ctx context.Context, link Link, userID string, now time.Time) (Job, error) {
	if strings.TrimSpace(userID) == "" {
		userID = link.UserID
	}
	job, err := s.store.InsertJob(ctx, Job{
		HouseholdID: link.HouseholdID, UserID: userID, LinkID: link.ID, Source: link.Source,
		Status: JobQueued, MaxAttempts: s.attempts, AvailableAt: now,
		Checkpoint: Checkpoint{Phase: PhaseOrders},
		CreatedAt:  now, UpdatedAt: now,
	})
	if err != nil {
		return Job{}, fmt.Errorf("enqueue import job: %w", err)
	}
	s.logger.InfoContext(ctx, "meal-kit import queued",
		"source", link.Source, "householdId", link.HouseholdID, "jobId", job.ID)
	return job, nil
}

// Unlink deletes the household's stored tokens and stops every job for that
// source immediately.
//
// Deletion comes first: after it, a worker that reloads the link finds
// nothing, and its next conditional write fails because the jobs are canceled.
// Unlinking a household that never linked is not an error.
func (s *Service) Unlink(ctx context.Context, householdID, source string) (canceled int, err error) {
	if householdID == "" {
		return 0, errHouseholdRequired
	}
	if !KnownSource(source) {
		return 0, invalid("source must be %q", SourceHelloFresh)
	}
	if err := s.store.DeleteLink(ctx, householdID, source); err != nil {
		return 0, fmt.Errorf("delete meal-kit link: %w", err)
	}
	now := s.now().UTC().Truncate(time.Millisecond)
	canceled, err = s.store.CancelJobs(ctx, householdID, source, "the meal-kit account was unlinked", now)
	if err != nil {
		return 0, fmt.Errorf("cancel import jobs: %w", err)
	}
	s.logger.InfoContext(ctx, "meal-kit account unlinked", "source", source, "householdId", householdID, "jobsCanceled", canceled)
	return canceled, nil
}

// Tokens decrypts a link's stored tokens. Only the worker calls it, and the
// result never leaves the process.
func (s *Service) Tokens(l Link) (Tokens, error) {
	if !s.Enabled() {
		return Tokens{}, ErrDisabled
	}
	return s.cipher.OpenTokens(l.Secret)
}

// accountLabel is what the app shows for a link. It is deliberately not the
// member's email address: the household does not need to see it, and we do
// not want it in a response body or a log.
func accountLabel(source string) string {
	if source == SourceHelloFresh {
		return "HelloFresh account"
	}
	return source + " account"
}

// signInError maps a source failure onto something safe to return.
func signInError(err error) error {
	switch {
	case errors.Is(err, ErrAuthExpired):
		return invalid("that email and password did not sign in to the meal-kit account")
	case errors.Is(err, ErrBlocked):
		return invalid("the meal-kit service refused the sign-in. Try again later")
	}
	var parse *ParseError
	if errors.As(err, &parse) {
		return invalid("%s", parse.Detail)
	}
	return invalid("could not reach the meal-kit service. Try again in a few minutes")
}

// signInReason is the one word that may be logged about a sign-in failure.
// The error itself is never logged: it can quote the request we sent.
func signInReason(err error) string {
	switch {
	case errors.Is(err, ErrAuthExpired):
		return "rejected"
	case errors.Is(err, ErrBlocked):
		return "blocked"
	}
	var parse *ParseError
	if errors.As(err, &parse) {
		return "unreadable_response"
	}
	return "unreachable"
}
