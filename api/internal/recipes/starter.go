package recipes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Starter library defaults.
const (
	// StarterBatchSize is how many recipes one copy round trip reads and
	// writes.
	StarterBatchSize = 100
	// DefaultStarterTimeout bounds one background copy.
	DefaultStarterTimeout = 2 * time.Minute
	// DefaultStarterHold is how long household creation waits for the copy
	// before returning and leaving it to finish in the background.
	DefaultStarterHold = 3 * time.Second
)

// StarterResult summarizes copying a starter library into a household.
type StarterResult struct {
	// Source counts the source household's recipes.
	Source int
	// Copied counts recipes written (or, in a dry run, that would be).
	Copied int
	// Existing counts source recipes the household already has, matched by
	// source and source recipe ID or alias.
	Existing int
}

// CopyStarterRecipes copies the recipes of sourceHouseholdID into
// targetHouseholdID. Only recipe content is copied: order history
// (orderWeeks, timesOrdered, lastOrderedWeek) is cleared, and nothing outside
// the recipe (ratings, plans, pantry, review items, events) is read. Catalog
// ingredient IDs are kept as they are, because the catalog is global.
//
// It is idempotent: a source recipe whose sourceRecipeId or any alias matches
// one of the target's recipes from the same source is skipped, the way Import
// matches, so re-running never duplicates. With apply false nothing is
// written. The work is two or three round trips per StarterBatchSize recipes.
func (s *Service) CopyStarterRecipes(ctx context.Context, sourceHouseholdID, targetHouseholdID string, apply bool) (StarterResult, error) {
	if sourceHouseholdID == "" || targetHouseholdID == "" {
		return StarterResult{}, errHouseholdRequired
	}
	if sourceHouseholdID == targetHouseholdID {
		return StarterResult{}, errors.New("recipes: starter source and target are the same household")
	}
	var res StarterResult
	now := s.now().UTC()
	after := ""
	for {
		page, err := s.store.ListRecipesAfter(ctx, sourceHouseholdID, after, StarterBatchSize)
		if err != nil {
			return res, fmt.Errorf("list starter recipes: %w", err)
		}
		if len(page) == 0 {
			return res, nil
		}
		after = page[len(page)-1].ID
		res.Source += len(page)

		idsBySource := map[string][]string{}
		for _, r := range page {
			idsBySource[r.Source] = append(idsBySource[r.Source], sourceIDs(r)...)
		}
		have := map[sourceRecipeKey]bool{}
		for _, source := range sortedKeys(idsBySource) {
			found, err := s.store.FindRecipesBySourceIDs(ctx, targetHouseholdID, source, idsBySource[source])
			if err != nil {
				return res, fmt.Errorf("find existing recipes: %w", err)
			}
			for _, r := range found {
				for _, id := range sourceIDs(r) {
					have[sourceRecipeKey{source: source, id: id}] = true
				}
			}
		}

		var writes []Recipe
		for _, r := range page {
			if slices.ContainsFunc(sourceIDs(r), func(id string) bool { return have[sourceRecipeKey{source: r.Source, id: id}] }) {
				res.Existing++
				continue
			}
			writes = append(writes, starterCopy(r, targetHouseholdID, now))
		}
		res.Copied += len(writes)
		if !apply || len(writes) == 0 {
			continue
		}
		// A concurrent copy into the same household can insert some of these
		// first; the unique (household, source, sourceRecipeId) index rejects
		// those and the rest of the unordered write still lands.
		if err := s.store.SaveRecipes(ctx, targetHouseholdID, writes); err != nil && !errors.Is(err, ErrDuplicate) {
			return res, fmt.Errorf("save starter recipes: %w", err)
		}
	}
}

// sourceIDs is a recipe's source recipe ID followed by its aliases.
func sourceIDs(r Recipe) []string {
	return append([]string{r.SourceRecipeID}, r.SourceAliases...)
}

// starterCopy is r's content as a new recipe of householdID, without the
// source household's order history.
func starterCopy(r Recipe, householdID string, now time.Time) Recipe {
	c := cloneForCopy(r)
	c.ID = ""
	c.HouseholdID = householdID
	c.OrderWeeks, c.TimesOrdered, c.LastOrderedWeek = nil, 0, ""
	c.CreatedAt, c.UpdatedAt = now, now
	return c
}

// cloneForCopy lists every copied field explicitly, so a field added to
// Recipe later is not copied until someone decides it is recipe content.
func cloneForCopy(r Recipe) Recipe {
	c := Recipe{
		Source: r.Source, SourceRecipeID: r.SourceRecipeID, SourceAliases: slices.Clone(r.SourceAliases), SourceURL: r.SourceURL,
		Name: r.Name, Headline: r.Headline, Description: r.Description, ImageURL: r.ImageURL, IsAddon: r.IsAddon,
		Servings: slices.Clone(r.Servings), PrepMinutes: r.PrepMinutes, TotalMinutes: r.TotalMinutes, Difficulty: r.Difficulty,
		Cuisines: slices.Clone(r.Cuisines), Tags: slices.Clone(r.Tags), Utensils: slices.Clone(r.Utensils), Allergens: slices.Clone(r.Allergens),
		Nutrition: slices.Clone(r.Nutrition), Steps: slices.Clone(r.Steps),
	}
	for _, line := range r.Ingredients {
		c.Ingredients = append(c.Ingredients, RecipeIngredient{
			IngredientID: line.IngredientID, Name: line.Name, PantryStaple: line.PantryStaple, Amounts: slices.Clone(line.Amounts),
		})
	}
	return c
}

// Starter gives new households a starter recipe library by copying a
// configured source household's recipes in the background. The zero source
// disables it. It implements households.CreatedListener.
type Starter struct {
	service *Service
	source  string
	timeout time.Duration
	hold    time.Duration
	logger  *slog.Logger
	wg      sync.WaitGroup
}

// StarterOptions configures a Starter.
type StarterOptions struct {
	Service *Service
	// SourceHouseholdID is the household whose recipes are copied. Empty
	// disables the starter library.
	SourceHouseholdID string
	// Timeout bounds one copy. Default DefaultStarterTimeout.
	Timeout time.Duration
	// Hold is how long HouseholdCreated waits for the copy to finish, so the
	// app's first Menu load usually finds the recipes. Zero means
	// DefaultStarterHold; negative returns at once.
	Hold   time.Duration
	Logger *slog.Logger
}

// NewStarter returns a Starter.
func NewStarter(opts StarterOptions) *Starter {
	st := &Starter{service: opts.Service, source: opts.SourceHouseholdID, timeout: opts.Timeout, hold: opts.Hold, logger: opts.Logger}
	if st.timeout <= 0 {
		st.timeout = DefaultStarterTimeout
	}
	if st.hold == 0 {
		st.hold = DefaultStarterHold
	}
	if st.logger == nil {
		st.logger = slog.New(slog.DiscardHandler)
	}
	return st
}

// Enabled reports whether a source household is configured.
func (st *Starter) Enabled() bool { return st != nil && st.source != "" }

// HouseholdCreated copies the starter library into a new household in the
// background, waiting at most the hold for it to finish; a slower copy keeps
// running after it returns. A failure is logged, never returned: the
// household exists either way, and cmd/seedstarter can re-run the copy.
func (st *Starter) HouseholdCreated(ctx context.Context, householdID string) {
	if !st.Enabled() || householdID == st.source {
		return
	}
	done := make(chan struct{})
	st.wg.Add(1)
	go func() {
		defer st.wg.Done()
		defer close(done)
		// The request that created the household ends before the copy does.
		copyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), st.timeout)
		defer cancel()
		started := time.Now()
		res, err := st.service.CopyStarterRecipes(copyCtx, st.source, householdID, true)
		if err != nil {
			st.logger.ErrorContext(copyCtx, "copy starter recipes failed; re-run cmd/seedstarter for this household",
				"householdId", householdID, "copied", res.Copied, "error", err)
			return
		}
		st.logger.InfoContext(copyCtx, "starter recipes copied",
			"householdId", householdID, "copied", res.Copied, "existing", res.Existing, "durationMs", time.Since(started).Milliseconds())
	}()
	if st.hold < 0 {
		return
	}
	hold := time.NewTimer(st.hold)
	defer hold.Stop()
	select {
	case <-done:
	case <-hold.C:
	case <-ctx.Done():
	}
}

// Wait blocks until running copies finish or ctx ends, for graceful shutdown.
func (st *Starter) Wait(ctx context.Context) error {
	if st == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		st.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
