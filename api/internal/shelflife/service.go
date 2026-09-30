package shelflife

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// DateLayout is a calendar date.
const DateLayout = "2006-01-02"

// ErrInvalid is a request the service can't answer.
var ErrInvalid = errors.New("invalid shelf-life request")

// Result is a food's recommended best-by date.
type Result struct {
	// BestBy is the date (DateLayout).
	BestBy string
	// StoredOn is the date it was put away, the start of the count.
	StoredOn string
	Storage  Storage
	// Life is the recommended time for the storage, and Text says it.
	Life Range
	Text string
	// Matched is the library food the time is for ("Carrots, parsnips");
	// empty for an estimate.
	Matched string
	// Estimate is true when the library didn't know the food and the time
	// is its category's typical one.
	Estimate bool
	Source   string
	// Usual is where the food is usually kept: the pantry when the library
	// has a pantry time for it, else the fridge, else the freezer; for an
	// estimate, the fridge for perishable categories.
	Usual Storage
}

// perishable categories are kept in the fridge when nothing else is known.
var perishable = map[string]bool{"produce": true, "meat-seafood": true, "dairy-eggs": true, "deli": true}

func usualStorage(e Entry, matched bool, category string) Storage {
	if matched {
		for _, s := range []Storage{Pantry, Fridge, Freezer} {
			if _, ok := e.In(s); ok {
				return s
			}
		}
	}
	if perishable[category] {
		return Fridge
	}
	return Pantry
}

// Store keeps entries added after the seed, and the names the library
// missed. *MongoStore implements it.
type Store interface {
	Entries(ctx context.Context) ([]Entry, error)
	RecordMiss(ctx context.Context, name string, storage Storage, at time.Time) error
}

// HouseholdSource reads how a household freezes meat.
// *households.Service implements it.
type HouseholdSource interface {
	GetHousehold(ctx context.Context, id string) (households.Household, error)
}

// Options configure a Service.
type Options struct {
	// Store is optional: without it only the seed is used and misses aren't
	// recorded.
	Store      Store
	Households HouseholdSource
	Logger     *slog.Logger
	Now        func() time.Time
}

// Service looks up recommended best-by dates.
type Service struct {
	store      Store
	households HouseholdSource
	logger     *slog.Logger
	now        func() time.Time
}

// NewService returns a Service.
func NewService(o Options) *Service {
	s := &Service{store: o.Store, households: o.Households, logger: o.Logger, now: o.Now}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Lookup is the recommended best-by date for name kept in storage from
// storedOn (DateLayout; today when empty). category, when known, decides
// the estimate for a name the library doesn't have; otherwise it's guessed
// from the name. Frozen food uses the household's wrap.
func (s *Service) Lookup(ctx context.Context, householdID, name, category string, storage Storage, storedOn string) (Result, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Result{}, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if storage == "" {
		storage = Pantry
	}
	if !storage.Valid() {
		return Result{}, fmt.Errorf("%w: storage must be pantry, fridge, or freezer", ErrInvalid)
	}
	start := s.now().UTC().Truncate(24 * time.Hour)
	if storedOn != "" {
		t, err := time.Parse(DateLayout, storedOn)
		if err != nil {
			return Result{}, fmt.Errorf("%w: storedOn must be a date like 2026-09-30", ErrInvalid)
		}
		start = t
	}
	entries := seed
	if s.store != nil {
		custom, err := s.store.Entries(ctx)
		if err != nil {
			s.logger.WarnContext(ctx, "load shelf-life entries", "error", err)
		}
		// Entries added later win: they're checked first and ties go to them.
		if len(custom) > 0 {
			entries = append(append([]Entry(nil), custom...), seed...)
		}
	}
	out := Result{StoredOn: start.Format(DateLayout), Storage: storage}
	e, matched := Match(entries, name, storage)
	if category == "" {
		category, _ = ingredients.Categorize(name)
	}
	// How the household freezes meat decides meat's freezer time; anything
	// else keeps the short end.
	wrap := wrapUnknown
	if storage == Freezer && category == "meat-seafood" {
		wrap = WrapVacuum
		if s.households != nil && householdID != "" {
			if hh, err := s.households.GetHousehold(ctx, householdID); err == nil {
				wrap = hh.Wrap()
			}
		}
	}
	out.Usual = usualStorage(e, matched, category)
	if r, ok := e.In(storage); matched && ok {
		out.Life, out.Matched, out.Source = r, e.Label(), e.Source
	} else {
		r, ok := Estimate(category, storage)
		if !ok {
			r = defaultEstimate[storage]
		}
		out.Life, out.Estimate, out.Source = r, true, "Typical for this kind of food"
		if s.store != nil {
			if err := s.store.RecordMiss(ctx, name, storage, s.now()); err != nil {
				s.logger.WarnContext(ctx, "record shelf-life miss", "name", name, "error", err)
			}
		}
	}
	out.Text = Text(out.Life)
	out.BestBy = BestBy(start, Days(out.Life, storage, wrap)).Format(DateLayout)
	return out, nil
}

// BestBy implements pantry.ShelfLife: the recommended best-by date for a
// pantry item.
func (s *Service) BestBy(ctx context.Context, householdID, name, category, storage, storedOn string) (string, error) {
	res, err := s.Lookup(ctx, householdID, name, category, Storage(storage), storedOn)
	if err != nil {
		return "", err
	}
	return res.BestBy, nil
}
