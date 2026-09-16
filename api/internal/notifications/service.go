package notifications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// Refresher brings a household's notifications up to date before they are
// read, for producers whose conditions change with time alone (a pantry item
// running low as days pass). *pantry.Service implements it.
type Refresher interface {
	Refresh(ctx context.Context, householdID string) error
}

// RefreshTimeout bounds how long a refresh may delay a read.
const RefreshTimeout = 3 * time.Second

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Store Store
	// Refresher is optional. It runs before List and UnreadCount; a failure
	// is logged and the read continues.
	Refresher Refresher
	Logger    *slog.Logger
	// Now is the clock. Default time.Now.
	Now func() time.Time
}

// Service creates and reads household notifications.
type Service struct {
	store Store
	// refreshers all run before a read, in the order they were added.
	refreshers []Refresher
	logger     *slog.Logger
	now        func() time.Time
}

// NewService returns a Service.
func NewService(opts ServiceOptions) *Service {
	s := &Service{store: opts.Store, logger: opts.Logger, now: opts.Now}
	if opts.Refresher != nil {
		s.refreshers = append(s.refreshers, opts.Refresher)
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// SetRefresher replaces the refreshers with r. Call it while wiring, before
// serving.
func (s *Service) SetRefresher(r Refresher) { s.refreshers = []Refresher{r} }

// AddRefresher adds another refresher, so several producers can bring their
// own time-based conditions up to date before a read. Call it while wiring,
// before serving.
func (s *Service) AddRefresher(r Refresher) {
	if r != nil {
		s.refreshers = append(s.refreshers, r)
	}
}

// Create stores a household notification with a pending push, unless one with
// the same DedupeKey exists; then it returns that one and created is false.
func (s *Service) Create(ctx context.Context, in New) (n Notification, created bool, err error) {
	switch {
	case in.HouseholdID == "":
		return Notification{}, false, errHouseholdRequired
	case in.Type == "":
		return Notification{}, false, errors.New("notifications: type is required")
	case strings.TrimSpace(in.Title) == "" || utf8.RuneCountInString(in.Title) > maxTitleLength:
		return Notification{}, false, fmt.Errorf("notifications: title must be 1-%d characters", maxTitleLength)
	case utf8.RuneCountInString(in.Body) > maxBodyLength:
		return Notification{}, false, fmt.Errorf("notifications: body must be at most %d characters", maxBodyLength)
	case in.DedupeKey == "" || len(in.DedupeKey) > maxDedupeKey:
		return Notification{}, false, fmt.Errorf("notifications: dedupe key must be 1-%d bytes", maxDedupeKey)
	}
	saved, err := s.store.Insert(ctx, Notification{
		HouseholdID: in.HouseholdID, Type: in.Type, Title: in.Title, Body: in.Body, Subject: in.Subject,
		DedupeKey: in.DedupeKey, Push: Push{Status: PushPending},
		CreatedAt: s.now().UTC().Truncate(time.Millisecond),
	})
	if errors.Is(err, ErrDuplicate) {
		existing, findErr := s.store.FindByDedupeKey(ctx, in.HouseholdID, in.DedupeKey)
		if findErr != nil {
			return Notification{}, false, fmt.Errorf("find duplicate notification: %w", findErr)
		}
		return existing, false, nil
	}
	if err != nil {
		return Notification{}, false, fmt.Errorf("insert notification: %w", err)
	}
	s.logger.InfoContext(ctx, "notification created",
		"householdId", saved.HouseholdID, "notificationId", saved.ID, "type", saved.Type)
	return saved, true, nil
}

// ListQuery selects a page of notifications for the actor.
type ListQuery struct {
	// UnreadOnly keeps notifications the actor hasn't read.
	UnreadOnly bool
	// Before is the previous page's NextCursor.
	Before string
	// Limit defaults to DefaultListLimit and is at most MaxListLimit.
	Limit int
}

// Page is one page of notifications, newest first. NextCursor is empty on the
// last page.
type Page struct {
	Items      []Notification
	NextCursor string
}

var objectIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

func authorizeView(actor households.Membership) error {
	if actor.HouseholdID == "" {
		return errHouseholdRequired
	}
	if !actor.Role.Can(households.PermHouseholdView) {
		return households.ErrForbidden
	}
	return nil
}

// List returns a page of the household's notifications for the actor.
func (s *Service) List(ctx context.Context, actor households.Membership, q ListQuery) (Page, error) {
	if err := authorizeView(actor); err != nil {
		return Page{}, err
	}
	limit := q.Limit
	switch {
	case limit == 0:
		limit = DefaultListLimit
	case limit < 1 || limit > MaxListLimit:
		return Page{}, invalid("limit must be between 1 and %d", MaxListLimit)
	}
	if q.Before != "" && !objectIDPattern.MatchString(q.Before) {
		return Page{}, invalid("before must be a cursor from a previous page")
	}
	s.refresh(ctx, actor.HouseholdID)
	f := ListFilter{Before: q.Before, Limit: limit + 1}
	if q.UnreadOnly {
		f.UnreadBy = actor.UserID
	}
	items, err := s.store.List(ctx, actor.HouseholdID, f)
	if err != nil {
		return Page{}, fmt.Errorf("list notifications: %w", err)
	}
	page := Page{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = items[limit-1].ID
	}
	return page, nil
}

// UnreadCount counts the notifications the actor hasn't read.
func (s *Service) UnreadCount(ctx context.Context, actor households.Membership) (int, error) {
	if err := authorizeView(actor); err != nil {
		return 0, err
	}
	s.refresh(ctx, actor.HouseholdID)
	n, err := s.store.CountUnread(ctx, actor.HouseholdID, actor.UserID)
	if err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return n, nil
}

// MarkRead marks the given notifications, or all of them when all is true,
// read by the actor, and returns the actor's new unread count. Exactly one of
// ids and all must be given. Unknown IDs are ignored.
func (s *Service) MarkRead(ctx context.Context, actor households.Membership, ids []string, all bool) (int, error) {
	if err := authorizeView(actor); err != nil {
		return 0, err
	}
	switch {
	case all && len(ids) > 0:
		return 0, invalid("send either ids or all, not both")
	case !all && len(ids) == 0:
		return 0, invalid("ids must not be empty, or send all: true")
	case len(ids) > MaxMarkRead:
		return 0, invalid("ids must have at most %d entries", MaxMarkRead)
	}
	for i, id := range ids {
		if strings.TrimSpace(id) == "" {
			return 0, invalid("ids[%d] is empty", i)
		}
	}
	var scope []string
	if !all {
		scope = ids
	}
	if err := s.store.MarkRead(ctx, actor.HouseholdID, actor.UserID, scope); err != nil {
		return 0, fmt.Errorf("mark notifications read: %w", err)
	}
	n, err := s.store.CountUnread(ctx, actor.HouseholdID, actor.UserID)
	if err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return n, nil
}

// Refresh brings the household's time-based notifications up to date without
// reading them, for the push sweep: a reminder derived on read would
// otherwise never exist while nobody opens the app. Failures are logged.
func (s *Service) Refresh(ctx context.Context, householdID string) {
	s.refresh(ctx, householdID)
}

// MarkReadByDedupeKey marks the household's notification with dedupeKey read
// by the actor. A key with no notification is not an error: the producer may
// never have created one, which is the same outcome the caller wanted.
func (s *Service) MarkReadByDedupeKey(ctx context.Context, actor households.Membership, dedupeKey string) error {
	if err := authorizeView(actor); err != nil {
		return err
	}
	n, err := s.store.FindByDedupeKey(ctx, actor.HouseholdID, dedupeKey)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find notification by dedupe key: %w", err)
	}
	if n.ReadByUser(actor.UserID) {
		return nil
	}
	if err := s.store.MarkRead(ctx, actor.HouseholdID, actor.UserID, []string{n.ID}); err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	return nil
}

// refresh runs every refresher, each bounded by RefreshTimeout. One failing
// is logged and never stops the others or the read.
func (s *Service) refresh(ctx context.Context, householdID string) {
	for _, r := range s.refreshers {
		func() {
			refreshCtx, cancel := context.WithTimeout(ctx, RefreshTimeout)
			defer cancel()
			if err := r.Refresh(refreshCtx, householdID); err != nil {
				s.logger.WarnContext(ctx, "refresh notifications failed", "householdId", householdID, "error", err)
			}
		}()
	}
}
