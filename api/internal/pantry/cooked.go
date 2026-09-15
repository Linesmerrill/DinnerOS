package pantry

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// CookedMeal is a cooked recipe to deduct from the pantry.
type CookedMeal struct {
	HouseholdID string
	UserID      string
	RecipeID    string
	// EntryID is the plan entry; with it, re-marking the same entry cooked
	// never deducts twice.
	EntryID string
	// ClientEventID identifies a cooked event without an entry.
	ClientEventID string
	Servings      int
	OccurredAt    time.Time
}

// CookSkipReason says why a pantry item wasn't deducted for a cooked recipe.
type CookSkipReason string

// Skip reasons.
const (
	// SkipNotTracked: the item has no recorded amount.
	SkipNotTracked CookSkipReason = "not_tracked"
	// SkipItemOut: the item is marked out.
	SkipItemOut CookSkipReason = "item_out"
	// SkipBeforeCycle: the meal was cooked before the current cycle began
	// (the item was restocked or corrected since).
	SkipBeforeCycle CookSkipReason = "before_cycle"
	// SkipNoAmount: the recipe gives no amount ("to taste").
	SkipNoAmount CookSkipReason = "no_amount"
	// SkipUnitMismatch: the recipe's unit doesn't convert exactly to the
	// item's (tbsp of butter against a count of sticks with no size).
	SkipUnitMismatch CookSkipReason = "unit_mismatch"
)

// CookUsage records one cooked meal's deductions. (HouseholdID, SourceKey) is
// unique, which makes deduction idempotent.
type CookUsage struct {
	ID          string
	HouseholdID string
	// SourceKey is "entry:<entryId>" or "event:<userId>:<clientEventId>".
	SourceKey string
	RecipeID  string
	EntryID   string
	UserID    string
	Servings  int
	// ScaledFrom is the authored serving size amounts were scaled from, or 0
	// when the recipe has the cooked size.
	ScaledFrom int
	OccurredAt time.Time
	CreatedAt  time.Time
	// Lines has one entry per recipe ingredient that matched a pantry item.
	Lines []CookLine
}

// CookLine is one recipe ingredient matched to a pantry item.
type CookLine struct {
	ItemID     string
	Ingredient string
	// Quantity and Unit are the recipe's amount for the cooked servings;
	// empty when it has none.
	Quantity string
	Unit     string
	// Deducted is Quantity in TrackingUnit for the cycle CycleID, when
	// deducted. SkipReason is set otherwise.
	Deducted     string
	TrackingUnit string
	CycleID      string
	SkipReason   CookSkipReason
}

// cookSourceKey returns the idempotency key for a cooked meal, or "" when it
// has neither an entry nor a client event ID.
func cookSourceKey(m CookedMeal) string {
	switch {
	case m.EntryID != "":
		return "entry:" + m.EntryID
	case m.ClientEventID != "" && m.UserID != "":
		return "event:" + m.UserID + ":" + m.ClientEventID
	}
	return ""
}

// recipeNeed is a recipe ingredient's amount for the cooked servings.
type recipeNeed struct {
	ingredientID string
	name         string
	quantity     *big.Rat
	unit         string
}

// recipeNeeds returns each ingredient's amount for servings: the authored
// amount for that size, or else the nearest authored size scaled linearly
// (scaledFrom reports it). A line without a usable amount has a nil quantity.
func recipeNeeds(r recipes.Recipe, servings int) (needs []recipeNeed, scaledFrom int) {
	for _, ing := range r.Ingredients {
		need := recipeNeed{ingredientID: ing.IngredientID, name: ing.Name}
		var chosen *recipes.Amount
		for i := range ing.Amounts {
			a := &ing.Amounts[i]
			if a.Servings <= 0 {
				continue
			}
			if chosen == nil || distance(a.Servings, servings) < distance(chosen.Servings, servings) {
				chosen = a
			}
		}
		if chosen != nil {
			if q, ok := chosen.ExactQuantity(); ok && !q.IsZero() {
				if _, err := ingredients.LookupUnit(chosen.Unit); err == nil {
					need.quantity, need.unit = q.Rat(), chosen.Unit
					if chosen.Servings != servings {
						need.quantity.Mul(need.quantity, big.NewRat(int64(servings), int64(chosen.Servings)))
						scaledFrom = chosen.Servings
					}
				}
			}
		}
		needs = append(needs, need)
	}
	return needs, scaledFrom
}

func distance(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// ApplyCooked deducts a cooked recipe's ingredient amounts, for the cooked
// servings, from the matching tracked pantry items, then runs the low-stock
// check on them. applied is false when there was nothing to do: no
// idempotency key, no servings, a recipe outside the household, no matching
// pantry items, or a meal already deducted.
//
// Each ingredient is matched to the item with its catalog ID or normalized
// name. Amounts convert to the item's tracking unit exactly or not at all:
// a line that can't convert is recorded with a skip reason and counted on
// the item, never guessed.
func (s *Service) ApplyCooked(ctx context.Context, m CookedMeal) (usage CookUsage, applied bool, err error) {
	if s.usage == nil || s.recipes == nil {
		return CookUsage{}, false, errUsageNotConfigured
	}
	key := cookSourceKey(m)
	if m.HouseholdID == "" || m.RecipeID == "" || key == "" || m.Servings < 1 {
		return CookUsage{}, false, nil
	}
	recipe, err := s.recipes.Get(ctx, m.HouseholdID, m.RecipeID)
	if errors.Is(err, recipes.ErrNotFound) {
		return CookUsage{}, false, nil
	}
	if err != nil {
		return CookUsage{}, false, fmt.Errorf("load cooked recipe: %w", err)
	}
	needs, scaledFrom := recipeNeeds(recipe, m.Servings)
	lines, err := s.matchNeeds(ctx, m, needs)
	if err != nil || len(lines) == 0 {
		return CookUsage{}, false, err
	}

	usage, err = s.usage.InsertCookUsage(ctx, CookUsage{
		HouseholdID: m.HouseholdID, SourceKey: key, RecipeID: m.RecipeID, EntryID: m.EntryID, UserID: m.UserID,
		Servings: m.Servings, ScaledFrom: scaledFrom, OccurredAt: m.OccurredAt.UTC(), CreatedAt: s.timestamp(), Lines: lines,
	})
	if errors.Is(err, ErrDuplicate) {
		return CookUsage{}, false, nil
	}
	if err != nil {
		return CookUsage{}, false, fmt.Errorf("insert cook usage: %w", err)
	}

	byItem := map[string][]CookLine{}
	var order []string
	for _, line := range lines {
		if _, ok := byItem[line.ItemID]; !ok {
			order = append(order, line.ItemID)
		}
		byItem[line.ItemID] = append(byItem[line.ItemID], line)
	}
	var errs []error
	for _, itemID := range order {
		if err := s.deductItem(ctx, m, itemID, byItem[itemID]); err != nil {
			errs = append(errs, fmt.Errorf("item %s: %w", itemID, err))
		}
	}
	return usage, true, errors.Join(errs...)
}

// matchNeeds matches recipe needs to the household's pantry items and decides
// each line's deduction against the items as they are now.
func (s *Service) matchNeeds(ctx context.Context, m CookedMeal, needs []recipeNeed) ([]CookLine, error) {
	items, err := s.store.ListItems(ctx, m.HouseholdID, ListFilter{})
	if err != nil {
		return nil, fmt.Errorf("list pantry items: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	byIngredientID, byKey := map[string]Item{}, map[string]Item{}
	for _, it := range items {
		if it.IngredientID != "" {
			byIngredientID[it.IngredientID] = it
		}
		byKey[it.Key] = it
	}
	var ids []string
	for _, n := range needs {
		if n.ingredientID != "" {
			ids = append(ids, n.ingredientID)
		}
	}
	catalogKeys := map[string]string{}
	if len(ids) > 0 {
		found, err := s.catalog.IngredientsByID(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("look up catalog ingredients: %w", err)
		}
		for _, ing := range found {
			catalogKeys[ing.ID] = ing.Key
		}
	}

	resolved := s.resolveNeedKeys(ctx, m.HouseholdID, needs, catalogKeys)

	var lines []CookLine
	for _, n := range needs {
		item, ok := byIngredientID[n.ingredientID]
		if !ok || n.ingredientID == "" {
			item, ok = byKey[catalogKeys[n.ingredientID]]
		}
		if !ok {
			item, ok = byKey[ingredients.NormalizeName(n.name)]
		}
		r, hasResolved := resolved[catalogKeys[n.ingredientID]]
		if !hasResolved {
			r, hasResolved = resolved[ingredients.NormalizeName(n.name)]
		}
		if !ok && hasResolved {
			item, ok = byKey[r.Key]
		}
		if !ok {
			continue
		}
		line := CookLine{ItemID: item.ID, Ingredient: n.name}
		if n.quantity != nil {
			line.Quantity, line.Unit = n.quantity.RatString(), n.unit
		}
		t := item.Tracking
		switch {
		case t == nil:
			line.SkipReason = SkipNotTracked
		case item.Status == StatusOut:
			line.SkipReason = SkipItemOut
		case m.OccurredAt.Before(t.SegmentStartedAt):
			line.SkipReason = SkipBeforeCycle
		case n.quantity == nil:
			line.SkipReason = SkipNoAmount
		default:
			converted, ok := convertAmount(n.quantity, n.unit, t.Unit, item.UnitSize)
			for i := 0; !ok && hasResolved && i < len(r.UnitSizes); i++ {
				converted, ok = convertAmount(n.quantity, n.unit, t.Unit, &r.UnitSizes[i])
			}
			if !ok {
				line.SkipReason = SkipUnitMismatch
				break
			}
			line.Deducted, line.TrackingUnit, line.CycleID = converted.RatString(), t.Unit, t.CycleID
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// deductItem applies one item's lines, retrying when the item changes
// concurrently. Lines decided for a cycle that has since ended are dropped.
func (s *Service) deductItem(ctx context.Context, m CookedMeal, itemID string, lines []CookLine) error {
	for range maxWriteAttempts {
		item, err := s.store.GetItem(ctx, m.HouseholdID, itemID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		t := item.Tracking
		if t == nil || item.Status == StatusOut {
			return nil
		}
		next := cloneItem(item)
		deducted, skipped := false, false
		for _, line := range lines {
			switch {
			case line.Deducted != "" && line.CycleID == t.CycleID && !m.OccurredAt.Before(t.SegmentStartedAt):
				amount := ratOf(line.Deducted)
				next.Tracking.SegmentRecipeUsed = new(big.Rat).Add(ratOf(next.Tracking.SegmentRecipeUsed), amount).RatString()
				next.Tracking.RecipeUsed = new(big.Rat).Add(ratOf(next.Tracking.RecipeUsed), amount).RatString()
				deducted = true
			case line.SkipReason == SkipNoAmount || line.SkipReason == SkipUnitMismatch:
				skipped = true
			}
		}
		if deducted {
			next.Tracking.RecipeUses++
		} else if skipped {
			next.Tracking.SkippedUses++
		} else {
			return nil
		}
		saved, err := s.store.UpdateItem(ctx, next)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		s.checkItemAlert(ctx, saved)
		return nil
	}
	return ErrConflict
}

// CookedListener returns an events.Listener that deducts every stored
// recipe.cooked event from the pantry (ApplyCooked). Failures are logged and
// never affect event ingestion.
func (s *Service) CookedListener() events.Listener {
	return cookedListener{s: s}
}

type cookedListener struct{ s *Service }

func (l cookedListener) EventsStored(ctx context.Context, list []events.Event) {
	for _, e := range list {
		if e.Type != events.TypeRecipeCooked {
			continue
		}
		p, _ := e.Payload.(events.RecipeCooked)
		usage, applied, err := l.s.ApplyCooked(ctx, CookedMeal{
			HouseholdID: e.HouseholdID, UserID: e.UserID, RecipeID: e.RecipeID, EntryID: p.EntryID,
			ClientEventID: e.ClientEventID, Servings: p.Servings, OccurredAt: e.OccurredAt,
		})
		attrs := []any{"householdId", e.HouseholdID, "recipeId", e.RecipeID, "entryId", p.EntryID}
		switch {
		case err != nil:
			l.s.logger.WarnContext(ctx, "deduct cooked recipe from pantry failed", append(attrs, "error", err)...)
		case applied:
			l.s.logger.InfoContext(ctx, "cooked recipe deducted from pantry", append(attrs, "lines", len(usage.Lines))...)
		default:
			l.s.logger.DebugContext(ctx, "cooked recipe not deducted from pantry", attrs...)
		}
	}
}
