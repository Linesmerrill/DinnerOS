package shopping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// This file holds "request a store": the households asking DinnerOS to
// support a grocer or delivery service it doesn't hand lists to yet. It is
// demand data. Nothing here touches internal/providers, and asking for a
// store never makes it usable.

// ErrTooManyRequests means the household is already at
// MaxStoreRequestsPerHousehold.
var ErrTooManyRequests = errors.New("shopping: too many store requests")

// Limits on store requests.
const (
	// MaxStoreRequestsPerHousehold caps how many stores one household can ask
	// for, so the demand signal can't be flooded from a single account.
	MaxStoreRequestsPerHousehold = 25
	// MaxStoreRequestNote bounds a member's note.
	MaxStoreRequestNote = 280
	// MaxStoreNameLength bounds a typed store name and the catalog's ?q=.
	MaxStoreNameLength = 100
)

// StoreRequest is one household asking for one store. It is unique per
// household and store key.
type StoreRequest struct {
	ID          string
	HouseholdID string
	// Key is a catalog key, or the key normalized from a typed name.
	Key string
	// Name is the catalog's display name, or the member's name title cased.
	Name string
	// Note is why they want it, or "".
	Note string
	// RequestedBy and RequestedAt are the first request; a later request from
	// the same household updates the note and leaves them alone.
	RequestedBy string
	RequestedAt time.Time
	UpdatedAt   time.Time
}

// Status returns what docs/shopping-providers.md concluded about the store,
// or StatusUnsupported for one the catalog doesn't list.
func (r StoreRequest) Status() StoreStatus {
	if e, ok := CatalogEntryByKey(r.Key); ok {
		return e.Status
	}
	return StatusUnsupported
}

// StoreRequestInput asks for a store. Exactly one of Key (a catalog entry)
// and Name (a store the catalog doesn't list) is required.
type StoreRequestInput struct {
	Key  string
	Name string
	Note string
}

// CatalogItem is a catalog entry with the demand behind it.
type CatalogItem struct {
	CatalogEntry
	// Requests counts the households that asked for the store, across all of
	// DinnerOS. It is our own demand signal, not a public number.
	Requests int
	// RequestedByHousehold reports whether the caller's household is one of
	// them.
	RequestedByHousehold bool
}

// StoreCatalog returns the curated catalog filtered by q, each entry carrying
// its demand. An empty q returns the whole list, ordered by status then name.
// householdID may be empty (a user in no household), which leaves every
// RequestedByHousehold false.
func (s *Service) StoreCatalog(ctx context.Context, householdID, q string) ([]CatalogItem, error) {
	if utf8.RuneCountInString(q) > MaxStoreNameLength {
		return nil, invalid("q must be at most %d characters", MaxStoreNameLength)
	}
	entries := SearchCatalog(q)
	counts, err := s.store.CountStoreRequestsByKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("count store requests: %w", err)
	}
	mine := map[string]bool{}
	if householdID != "" {
		list, err := s.store.ListStoreRequests(ctx, householdID)
		if err != nil {
			return nil, fmt.Errorf("list store requests: %w", err)
		}
		for _, r := range list {
			mine[r.Key] = true
		}
	}
	out := make([]CatalogItem, 0, len(entries))
	for _, e := range entries {
		out = append(out, CatalogItem{CatalogEntry: e, Requests: counts[e.Key], RequestedByHousehold: mine[e.Key]})
	}
	return out, nil
}

// RequestStore records that the household wants a store, and reports whether
// this created the request. It is idempotent per household and store: asking
// again updates the note instead of adding a duplicate, and keeps the first
// requester and time.
func (s *Service) RequestStore(ctx context.Context, actor households.Membership, in StoreRequestInput) (StoreRequest, bool, error) {
	if err := authorize(actor, households.PermHouseholdView); err != nil {
		return StoreRequest{}, false, err
	}
	req, err := newStoreRequest(actor, in)
	if err != nil {
		return StoreRequest{}, false, err
	}
	req.RequestedAt = s.timestamp()
	req.UpdatedAt = req.RequestedAt

	// The cap counts stores, not requests, so updating a note never trips it.
	if _, err := s.store.GetStoreRequestByKey(ctx, actor.HouseholdID, req.Key); errors.Is(err, ErrNotFound) {
		n, err := s.store.CountStoreRequests(ctx, actor.HouseholdID)
		if err != nil {
			return StoreRequest{}, false, fmt.Errorf("count store requests: %w", err)
		}
		if n >= MaxStoreRequestsPerHousehold {
			return StoreRequest{}, false, ErrTooManyRequests
		}
	} else if err != nil {
		return StoreRequest{}, false, fmt.Errorf("get store request: %w", err)
	}

	saved, created, err := s.store.UpsertStoreRequest(ctx, req)
	if err != nil {
		return StoreRequest{}, false, fmt.Errorf("upsert store request: %w", err)
	}
	if created {
		_, inCatalog := CatalogEntryByKey(saved.Key)
		events.RecordOrLog(ctx, s.events, s.logger, events.Event{
			HouseholdID: saved.HouseholdID, UserID: actor.UserID, Type: events.TypeShoppingStoreRequested,
			OccurredAt: saved.RequestedAt, Payload: events.ShoppingStoreRequested{Key: saved.Key, Catalog: inCatalog},
		})
		s.logger.InfoContext(ctx, "shopping store requested", "householdId", saved.HouseholdID, "key", saved.Key, "catalog", inCatalog)
	}
	return saved, created, nil
}

// ListStoreRequests returns the household's store requests, newest first.
func (s *Service) ListStoreRequests(ctx context.Context, householdID string) ([]StoreRequest, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	return s.store.ListStoreRequests(ctx, householdID)
}

// DeleteStoreRequest withdraws one of the household's store requests.
func (s *Service) DeleteStoreRequest(ctx context.Context, actor households.Membership, id string) error {
	if err := authorize(actor, households.PermHouseholdView); err != nil {
		return err
	}
	return s.store.DeleteStoreRequest(ctx, actor.HouseholdID, id)
}

// newStoreRequest validates and normalizes a request. A typed name is matched
// against catalog names, keys, and aliases first, so "frys" records as Fry's
// rather than a second entry for the same store.
func newStoreRequest(actor households.Membership, in StoreRequestInput) (StoreRequest, error) {
	key, name := strings.TrimSpace(in.Key), strings.TrimSpace(in.Name)
	if (key == "") == (name == "") {
		return StoreRequest{}, invalid("send key for a store in the catalog, or name for one that isn't, not both")
	}
	req := StoreRequest{HouseholdID: actor.HouseholdID, RequestedBy: actor.UserID}
	switch {
	case key != "":
		e, ok := CatalogEntryByKey(key)
		if !ok {
			return StoreRequest{}, invalid("key isn't a store in the catalog; send the name instead")
		}
		req.Key, req.Name = e.Key, e.Name
	case utf8.RuneCountInString(name) > MaxStoreNameLength:
		return StoreRequest{}, invalid("name must be at most %d characters", MaxStoreNameLength)
	default:
		if e, ok := matchCatalogName(name); ok {
			req.Key, req.Name = e.Key, e.Name
			break
		}
		if req.Key = storeKeyOf(name); req.Key == "" {
			return StoreRequest{}, invalid("name must have at least one letter or digit")
		}
		req.Name = displayStoreName(name)
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > MaxStoreRequestNote {
		return StoreRequest{}, invalid("note must be at most %d characters", MaxStoreRequestNote)
	}
	req.Note = note
	return req, nil
}
