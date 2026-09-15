package events

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// Client ingestion limits.
const (
	// MaxBatchSize is the most events one ingestion request may carry.
	MaxBatchSize = 100
	// MaxPayloadBytes caps one client event's JSON payload.
	MaxPayloadBytes = 1024
	// MaxFutureSkew is how far ahead of the server clock occurredAt may be.
	MaxFutureSkew = 5 * time.Minute
	// MaxEventAge is how old a client event may be. Older events from a long
	// offline queue are rejected rather than rewriting history.
	MaxEventAge = 30 * 24 * time.Hour
)

// Listener reacts to events after they are stored, such as the pantry
// deducting a cooked recipe. It must be idempotent: a client retry passes an
// already stored event again. Listeners run synchronously after the store
// write, with a context detached from the request and bounded by
// ListenerTimeout. They can't fail the write, so they log their own errors.
type Listener interface {
	EventsStored(ctx context.Context, events []Event)
}

// ListenerTimeout bounds how long listeners may delay the request that stored
// the events.
const ListenerTimeout = 10 * time.Second

// RecipeChecker reports which recipe IDs belong to a household. The recipes
// module implements it.
type RecipeChecker interface {
	ExistingRecipeIDs(ctx context.Context, householdID string, ids []string) ([]string, error)
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Store Store
	// Recipes validates the recipe IDs clients send.
	Recipes RecipeChecker
	Logger  *slog.Logger
	// Now is the clock. Default time.Now.
	Now func() time.Time
	// Listeners run after events are stored.
	Listeners []Listener
}

// Service records server events and ingests client events.
type Service struct {
	store     Store
	recipes   RecipeChecker
	logger    *slog.Logger
	now       func() time.Time
	listeners []Listener
}

var _ Recorder = (*Service)(nil)

// NewService returns a Service.
func NewService(opts ServiceOptions) *Service {
	s := &Service{store: opts.Store, recipes: opts.Recipes, logger: opts.Logger, now: opts.Now, listeners: opts.Listeners}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Record implements Recorder.
func (s *Service) Record(ctx context.Context, e Event) error {
	now := s.now().UTC()
	if e.Source == "" {
		e.Source = SourceAPI
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = now
	}
	e.RecordedAt = now
	e, err := normalize(e)
	if err != nil {
		return err
	}
	if _, _, err := s.store.Insert(ctx, []Event{e}); err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	s.notifyListeners(ctx, []Event{e})
	return nil
}

// MaxListLimit caps Query.Limit for List.
const MaxListLimit = 20000

// List returns a household's events matching q. Server modules read history
// through it (the recommender, preference history); clients can't read events.
// q.Limit is capped at MaxListLimit. Callers must already have authorized
// access to the household.
func (s *Service) List(ctx context.Context, q Query) ([]Event, error) {
	if q.HouseholdID == "" {
		return nil, invalid("householdId is required")
	}
	q.Limit = min(q.Limit, MaxListLimit)
	list, err := s.store.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return list, nil
}

// notifyListeners passes stored events to every listener.
func (s *Service) notifyListeners(ctx context.Context, list []Event) {
	if len(s.listeners) == 0 || len(list) == 0 {
		return
	}
	listenCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ListenerTimeout)
	defer cancel()
	for _, l := range s.listeners {
		l.EventsStored(listenCtx, list)
	}
}

// ClientEvent is one event as an app sent it. Fields are untrusted and
// validated by Ingest.
type ClientEvent struct {
	// ClientEventID is an optional idempotency key, unique per user.
	ClientEventID string
	Type          string
	RecipeID      string
	Week          string
	// OccurredAt is an RFC 3339 timestamp.
	OccurredAt string
	// Payload is the JSON payload for Type; empty means no fields.
	Payload []byte
}

// IngestResult reports what happened to each event in a batch. Every event is
// counted exactly once: in Accepted, in Duplicates, or in Rejected.
type IngestResult struct {
	// Accepted counts newly stored events.
	Accepted int
	// Duplicates counts events whose clientEventId was already stored or
	// appeared earlier in the batch. Clients treat them as delivered.
	Duplicates int
	// Rejected lists invalid events by batch index. Retrying them won't help.
	Rejected []Rejection
}

// Rejection explains why one event in a batch was not stored.
type Rejection struct {
	Index   int
	Message string
}

// Ingest validates and stores a batch of client-observed events for the
// actor's household. Invalid events are rejected individually and the rest are
// stored, so one bad event can't block an app's offline queue. Only the types
// ClientTypes lists are accepted, recipe IDs must belong to the household, and
// the user and household always come from the actor, never the client.
//
// It returns ErrInvalidBatch for an empty or oversized batch and
// households.ErrForbidden when the actor may not view the household.
func (s *Service) Ingest(ctx context.Context, actor households.Membership, batch []ClientEvent) (IngestResult, error) {
	if !actor.Role.Can(households.PermHouseholdView) {
		return IngestResult{}, households.ErrForbidden
	}
	switch {
	case len(batch) == 0:
		return IngestResult{}, fmt.Errorf("%w: events must contain at least one event", ErrInvalidBatch)
	case len(batch) > MaxBatchSize:
		return IngestResult{}, fmt.Errorf("%w: events must contain at most %d events", ErrInvalidBatch, MaxBatchSize)
	}

	now := s.now().UTC()
	var res IngestResult
	type candidate struct {
		index int
		event Event
	}
	var candidates []candidate
	seenKeys := map[string]bool{}
	for i, in := range batch {
		e, err := clientEvent(actor, in, now)
		if err != nil {
			res.Rejected = append(res.Rejected, Rejection{Index: i, Message: publicMessage(err)})
			continue
		}
		if e.ClientEventID != "" {
			if seenKeys[e.ClientEventID] {
				res.Duplicates++
				continue
			}
			seenKeys[e.ClientEventID] = true
		}
		candidates = append(candidates, candidate{index: i, event: e})
	}

	var recipeIDs []string
	for _, c := range candidates {
		if id := c.event.RecipeID; id != "" && !slices.Contains(recipeIDs, id) {
			recipeIDs = append(recipeIDs, id)
		}
	}
	if len(recipeIDs) > 0 {
		existing, err := s.recipes.ExistingRecipeIDs(ctx, actor.HouseholdID, recipeIDs)
		if err != nil {
			return IngestResult{}, fmt.Errorf("check recipes: %w", err)
		}
		kept := candidates[:0]
		for _, c := range candidates {
			if c.event.RecipeID != "" && !slices.Contains(existing, c.event.RecipeID) {
				res.Rejected = append(res.Rejected, Rejection{Index: c.index, Message: "recipeId does not match a recipe in this household"})
				continue
			}
			kept = append(kept, c)
		}
		candidates = kept
	}

	if len(candidates) > 0 {
		list := make([]Event, 0, len(candidates))
		for _, c := range candidates {
			list = append(list, c.event)
		}
		inserted, duplicates, err := s.store.Insert(ctx, list)
		if err != nil {
			return IngestResult{}, fmt.Errorf("insert events: %w", err)
		}
		res.Accepted = inserted
		res.Duplicates += duplicates
		// Stored duplicates are passed too: the earlier request's listeners
		// may not have run, and listeners are idempotent.
		s.notifyListeners(ctx, list)
	}
	slices.SortFunc(res.Rejected, func(a, b Rejection) int { return cmp.Compare(a.Index, b.Index) })
	s.logger.InfoContext(ctx, "client events ingested",
		"householdId", actor.HouseholdID, "userId", actor.UserID,
		"accepted", res.Accepted, "duplicates", res.Duplicates, "rejected", len(res.Rejected))
	return res, nil
}

// clientEvent turns one client event into a validated Event for the actor.
func clientEvent(actor households.Membership, in ClientEvent, now time.Time) (Event, error) {
	t := Type(in.Type)
	spec, ok := typeSpecs[t]
	if !ok || !spec.client {
		return Event{}, invalid(fmt.Sprintf("type must be one of %s", joinTypes(ClientTypes())))
	}
	if strings.TrimSpace(in.OccurredAt) == "" {
		return Event{}, invalid("occurredAt is required")
	}
	occurredAt, err := time.Parse(time.RFC3339, in.OccurredAt)
	switch {
	case err != nil:
		return Event{}, invalid("occurredAt must be an RFC 3339 timestamp")
	case occurredAt.After(now.Add(MaxFutureSkew)):
		return Event{}, invalid("occurredAt must not be in the future")
	case occurredAt.Before(now.Add(-MaxEventAge)):
		return Event{}, invalid("occurredAt must be within the last 30 days")
	case len(in.Payload) > MaxPayloadBytes:
		return Event{}, invalid(fmt.Sprintf("payload must be at most %d bytes", MaxPayloadBytes))
	case in.ClientEventID != "" && strings.TrimSpace(in.ClientEventID) != in.ClientEventID:
		return Event{}, invalid("clientEventId must not have surrounding whitespace")
	}
	payload, err := spec.decode(in.Payload, unmarshalJSONStrict)
	if err != nil {
		return Event{}, invalid(fmt.Sprintf("payload is not valid for %s: %s", t, strings.TrimPrefix(err.Error(), "json: ")))
	}
	return normalize(Event{
		HouseholdID:   actor.HouseholdID,
		UserID:        actor.UserID,
		Type:          t,
		RecipeID:      in.RecipeID,
		Week:          in.Week,
		Payload:       payload,
		ClientEventID: in.ClientEventID,
		OccurredAt:    occurredAt,
		RecordedAt:    now,
		Source:        SourceClient,
	})
}

func unmarshalJSONStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after the payload")
	}
	return nil
}

func joinTypes(types []Type) string {
	s := make([]string, 0, len(types))
	for _, t := range types {
		s = append(s, string(t))
	}
	return strings.Join(s, ", ")
}

// publicMessage strips the sentinel prefix from a validation error.
func publicMessage(err error) string {
	msg := strings.TrimPrefix(err.Error(), ErrInvalidEvent.Error()+": ")
	return strings.TrimPrefix(msg, ErrInvalidBatch.Error()+": ")
}
