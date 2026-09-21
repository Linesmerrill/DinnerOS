package pantry

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file owns the freezer: putting a bulk pack's remainder away, and
// estimating how long it takes to thaw in the fridge
// (docs/pantry-usage.md#the-freezer).
//
// The week's list forces package sizes nobody asked for. The smallest pork
// loin at the store is 4 lb and Thursday's meal uses 10 oz, so 54 oz is
// bought that the week has no plan for. Throwing it in the pantry as
// "in stock" would be a lie (nothing keeps raw pork on a shelf), and the
// leftovers rule therefore refuses to track meat at all — which leaves the
// remainder unrecorded, and next month's list buys another 4 lb loin.
//
// The freezer is the third answer. A frozen item is a pantry item like any
// other — one row per ingredient, amounts, usage cycle, low alerts — with
// Storage freezer. Later weeks read it as grocery.StatusFromFreezer: still on
// the list so somebody takes it out in time, never in an export.

// FreezeSource is the handoff line a frozen remainder came from. It makes
// freezing idempotent: sealing the same line twice changes nothing.
type FreezeSource struct {
	// Provider is a shopping provider key, for the record.
	Provider  string
	HandoffID string
	LineID    string
}

func (f FreezeSource) ref() string {
	if f.HandoffID == "" || f.LineID == "" {
		return ""
	}
	return f.HandoffID + ":" + f.LineID
}

// FreezeInput is the input for Service.Freeze: what was sealed and put away.
type FreezeInput struct {
	// IngredientID or Name identifies the ingredient, as for a grocery
	// check-off; a new ingredient is added.
	IngredientID string
	Name         string
	// Quantity and Unit are the amount actually sealed, required. It is the
	// real remainder ("16 oz"), not the package size.
	Quantity string
	Unit     string
	// Portions is how many sealed portions it was split into; 0 means one.
	Portions int
	Note     string
	// Source, when set, is the handoff line the remainder came from.
	Source *FreezeSource
}

// MaxPortions bounds the portions a remainder is split into.
const MaxPortions = 100

// FreezeResult is what Service.Freeze did.
type FreezeResult struct {
	Item Item
	// Created is true when the ingredient wasn't in the pantry yet.
	Created bool
	// AlreadyFrozen is true when this handoff line had already been sealed
	// and nothing changed.
	AlreadyFrozen bool
	// Thaw estimates how long one portion takes to thaw in the fridge.
	Thaw ThawEstimate
}

// Freeze records a bulk pack's remainder in the freezer: the ingredient's
// pantry item (added when missing) is stored with Storage freezer and the
// sealed amount, and a usage cycle starts from it so cooked meals count it
// down like any other pantry amount.
//
// It is idempotent per handoff line. Without a source it is an ordinary
// write, so a member can freeze something the app never bought.
func (s *Service) Freeze(ctx context.Context, actor households.Membership, in FreezeInput) (FreezeResult, error) {
	if err := authorizeEdit(actor); err != nil {
		return FreezeResult{}, err
	}
	quantity, unit, err := normalizeAmount(in.Quantity, in.Unit)
	if err != nil {
		return FreezeResult{}, err
	}
	if quantity == "" {
		return FreezeResult{}, invalid("freezing needs the amount that was sealed")
	}
	if in.Portions < 0 || in.Portions > MaxPortions {
		return FreezeResult{}, invalid("portions must be between 1 and %d", MaxPortions)
	}
	note, err := normalizeNote(in.Note)
	if err != nil {
		return FreezeResult{}, err
	}
	ref := ""
	if in.Source != nil {
		if ref = in.Source.ref(); ref == "" {
			return FreezeResult{}, invalid("a freeze source needs a handoff and a line")
		}
	}
	a, err := s.validateAddition(ctx, AddInput{IngredientID: in.IngredientID, Name: in.Name})
	if err != nil {
		return FreezeResult{}, err
	}

	hh := actor.HouseholdID
	for range maxWriteAttempts {
		existing, err := s.store.FindItemsByKeys(ctx, hh, []string{a.key})
		if err != nil {
			return FreezeResult{}, fmt.Errorf("find pantry item: %w", err)
		}
		now := s.timestamp()
		if len(existing) == 1 && ref != "" && existing[0].FrozenFrom == ref {
			return FreezeResult{Item: existing[0], AlreadyFrozen: true, Thaw: ThawFor(existing[0])}, nil
		}
		var item Item
		created := len(existing) == 0
		if created {
			if err := s.checkCapacity(ctx, hh, 1); err != nil {
				return FreezeResult{}, err
			}
			item = newItem(hh, a, actor.UserID, now)
		} else {
			item = cloneItem(existing[0])
			if item.IngredientID == "" {
				item.IngredientID = a.ingredientID
			}
		}
		freezeItem(&item, quantity, unit, in.Portions, note, ref, s.newID(), now)
		item.UpdatedBy, item.UpdatedAt = actor.UserID, now

		var saved Item
		if created {
			saved, err = s.store.InsertItem(ctx, item)
			if errors.Is(err, ErrDuplicate) {
				continue // someone added it concurrently; merge into theirs
			}
		} else {
			saved, err = s.store.UpdateItem(ctx, item)
			if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
				continue // changed or deleted concurrently; start over
			}
		}
		if err != nil {
			return FreezeResult{}, fmt.Errorf("store frozen pantry item: %w", err)
		}
		s.logger.InfoContext(ctx, "pantry item frozen",
			"householdId", hh, "itemId", saved.ID, "key", saved.Key,
			"quantity", quantity, "unit", unit, "portions", in.Portions)
		return FreezeResult{Item: saved, Created: created, Thaw: ThawFor(saved)}, nil
	}
	return FreezeResult{}, ErrConflict
}

// freezeItem applies a sealed remainder to item. The amount restocks it, so
// the usage machinery treats the freezer exactly like a shelf: a cycle starts
// at the sealed amount and cooked meals count it down.
func freezeItem(item *Item, quantity, unit string, portions int, note, ref, cycleID string, now time.Time) {
	restock(item, cycleID, CycleFrozen, quantity, unit, now)
	item.Storage = StorageFreezer
	item.FrozenOn = now.Format(DateLayout)
	item.Portions = portions
	item.FrozenFrom = ref
	if note != "" {
		item.Note = note
	}
	// Frozen stock has no shelf-life date of its own; a stale fridge date
	// from before it was sealed would only mislead.
	item.ExpiresOn = ""
}

// --- The thaw model -----------------------------------------------------------
//
// A frozen item has to leave the freezer before it is cooked, and the answer
// to "when?" is a weight. The USDA's fridge-thawing guidance is about a day
// per four to five pounds of meat, which is where FridgeHoursPerPound comes
// from: 5 hours per pound, on the safe side of that range.
//
// Two adjustments make it useful rather than merely correct:
//
//   - Portions. A 16 oz bag split into two sealed portions thaws a portion at
//     a time, so the estimate weighs one portion, not the bag. That is the
//     difference between "start tomorrow morning" and "after lunch".
//   - Category. Only meat and seafood are dense frozen blocks. Bread, deli
//     slices and dairy thaw far faster, so they use LightHoursPerPound.
//
// Amounts that aren't a weight are converted when they can be (a volume
// through the item's typical density), and otherwise fall back to
// DefaultThawHours — long enough to be safe, short enough to be actionable.
// Every estimate is clamped to [MinThawHours, MaxThawHours] and rounded to
// whole hours, because nobody plans a thaw to the minute.

// Thaw model constants.
const (
	// FridgeHoursPerPound is fridge thawing for dense frozen protein.
	FridgeHoursPerPound = 5
	// LightHoursPerPound is fridge thawing for bread, deli and dairy.
	LightHoursPerPound = 2
	// MinThawHours is the floor: even a single small portion needs a couple
	// of hours out of the freezer.
	MinThawHours = 2
	// MaxThawHours is the ceiling. Beyond two days the advice is the same:
	// start now.
	MaxThawHours = 48
	// DefaultThawHours is used when the amount is not a weight and cannot be
	// converted to one.
	DefaultThawHours = 8
)

// ThawEstimate is how long one portion of a frozen item takes to thaw in the
// fridge.
type ThawEstimate struct {
	// Hours is the estimate, whole hours in [MinThawHours, MaxThawHours].
	Hours int
	// Measured is false when the item's amount isn't a weight and the hours
	// are DefaultThawHours rather than a calculation.
	Measured bool
	// PortionOunces is the weight the estimate used, or nil when unmeasured.
	PortionOunces *big.Rat
	// Portions is how many portions the amount was split into (at least 1).
	Portions int
	// Summary is ready to show: "about 4 hours".
	Summary string
}

// ThawFor estimates the fridge thaw time for one portion of item. It is
// meaningful only for frozen items; others get the default.
func ThawFor(item Item) ThawEstimate {
	portions := item.Portions
	if portions < 1 {
		portions = 1
	}
	e := ThawEstimate{Hours: DefaultThawHours, Portions: portions}
	if oz := portionOunces(item, portions); oz != nil {
		e.Measured, e.PortionOunces = true, oz
		e.Hours = thawHours(oz, item.Category)
	}
	e.Summary = thawSummary(e.Hours)
	return e
}

// portionOunces is the weight of one portion of item, or nil when its amount
// isn't a weight and doesn't convert to one.
func portionOunces(item Item, portions int) *big.Rat {
	quantity, unit := item.Quantity, item.Unit
	if t := item.Tracking; t != nil && t.Reference != "" {
		// The tracking amount is already in a measurable unit when a
		// discrete purchase had a size ("1 package = 16 oz").
		quantity, unit = t.Reference, t.Unit
	}
	if quantity == "" || unit == "" {
		return nil
	}
	q := ratOf(quantity)
	if q.Sign() <= 0 {
		return nil
	}
	oz, ok := convertAmount(q, unit, "oz", item.UnitSize)
	if !ok {
		// A volume becomes a weight only through the item's typical density,
		// which is an estimate; a thaw time is an estimate anyway.
		if oz, ok = estimateAmount(q, unit, "oz", item); !ok {
			return nil
		}
	}
	if oz.Sign() <= 0 {
		return nil
	}
	return oz.Quo(oz, big.NewRat(int64(portions), 1))
}

// lightThawCategories thaw far faster than a frozen block of protein.
var lightThawCategories = map[string]bool{
	ingredients.CategoryBakery:    true,
	ingredients.CategoryDeli:      true,
	ingredients.CategoryDairyEggs: true,
}

// thawHours is the model: hours per pound by category, clamped and rounded.
func thawHours(ounces *big.Rat, category string) int {
	perPound := int64(FridgeHoursPerPound)
	if lightThawCategories[category] {
		perPound = LightHoursPerPound
	}
	// hours = ounces ÷ 16 × perPound
	hours := new(big.Rat).Mul(ounces, big.NewRat(perPound, 16))
	whole := roundRat(hours, 1).Num().Int64()
	switch {
	case whole < MinThawHours:
		return MinThawHours
	case whole > MaxThawHours:
		return MaxThawHours
	}
	return int(whole)
}

func thawSummary(hours int) string {
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

// --- Frozen stock -------------------------------------------------------------

// FrozenItem is one in-stock freezer item with its thaw estimate, and the
// ingredient keys a grocery line for it can carry.
type FrozenItem struct {
	Item Item
	Keys []string
	Thaw ThawEstimate
}

// FrozenStock returns the household's in-stock freezer items. The thaw
// reminder (internal/planning) joins it with the week's planned meals.
func (s *Service) FrozenStock(ctx context.Context, householdID string) ([]FrozenItem, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	items, err := s.store.ListItems(ctx, householdID, ListFilter{Status: StatusInStock, Storage: StorageFreezer})
	if err != nil {
		return nil, fmt.Errorf("list frozen pantry items: %w", err)
	}
	sortItems(items)
	out := make([]FrozenItem, 0, len(items))
	for _, item := range items {
		out = append(out, FrozenItem{Item: item, Keys: groceryKeys(item), Thaw: ThawFor(item)})
	}
	return out, nil
}

// groceryKeys are the grocery-line keys an item answers for: its catalog
// ingredient ID when it has one, and "name:" + each normalized name.
func groceryKeys(item Item) []string {
	var keys []string
	if item.IngredientID != "" {
		keys = append(keys, item.IngredientID)
	}
	for _, k := range pantryKeys(item) {
		keys = append(keys, UnresolvedKeyPrefix+k)
	}
	if name := ingredients.NormalizeName(item.DisplayName); name != "" &&
		!strings.EqualFold(name, item.Key) {
		keys = append(keys, UnresolvedKeyPrefix+name)
	}
	return keys
}
