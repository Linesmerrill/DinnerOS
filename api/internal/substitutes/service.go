package substitutes

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Catalog resolves global ingredient catalog entries. *recipes.Service
// implements it.
type Catalog interface {
	IngredientsByID(ctx context.Context, ids []string) ([]recipes.Ingredient, error)
	IngredientsByKey(ctx context.Context, keys []string) ([]recipes.Ingredient, error)
}

// RecipeUsage reports which household recipes use catalog ingredients.
// *recipes.Service implements it.
type RecipeUsage interface {
	FindIngredientUse(ctx context.Context, householdID string, ingredientIDs []string) ([]recipes.IngredientUse, error)
}

// Pantry reads batch stock and records batches. *pantry.Service implements it.
type Pantry interface {
	StockLevels(ctx context.Context, householdID string, keys []string) (map[string]pantry.StockLevel, error)
	RecordHouseMade(ctx context.Context, actor households.Membership, in pantry.HouseMadeInput) (pantry.PurchaseResult, error)
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Store   Store
	Catalog Catalog
	Recipes RecipeUsage
	// Pantry is needed for batch stock and recording batches.
	Pantry Pantry
	Logger *slog.Logger
}

// Service implements specialty ingredients. Reads take a household ID (HTTP
// routes authorize household.view); writes take the actor and check
// pantry.edit, since choices change grocery lists and batches change the
// pantry.
type Service struct {
	store   Store
	catalog Catalog
	recipes RecipeUsage
	pantry  Pantry
	// events is optional; without it a strategy change records nothing.
	events events.Recorder
	logger *slog.Logger
	now    func() time.Time
}

// WithEvents makes SetStrategy record specialty.strategy_updated through
// recorder, and returns s. Recording is best effort (events.RecordOrLog): a
// failure is logged and never fails the change.
func (s *Service) WithEvents(recorder events.Recorder) *Service {
	s.events = recorder
	return s
}

// NewService returns a Service.
func NewService(o ServiceOptions) *Service {
	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: o.Store, catalog: o.Catalog, recipes: o.Recipes, pantry: o.Pantry, logger: logger, now: time.Now}
}

func (s *Service) timestamp() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

func authorizeEdit(actor households.Membership) error {
	if actor.HouseholdID == "" || actor.UserID == "" {
		return errHouseholdRequired
	}
	if !actor.Role.Can(households.PermPantryEdit) {
		return ErrForbidden
	}
	return nil
}

// View is a specialty ingredient as one household sees it.
type View struct {
	Specialty Specialty
	// IngredientIDs are the catalog ingredients that are this specialty.
	IngredientIDs []string
	// RecipeCount is how many of the household's recipes use it.
	RecipeCount int
	// Options are the curated options, then the household's, oldest first.
	Options []Option
	// Choice is the household's explicit choice, or nil.
	Choice *Choice
	// ChoiceOption is the chosen option; nil for as_is or no choice.
	ChoiceOption *Option
	// Resolution is what currently applies: the explicit choice, the
	// household's strategy, or nothing. It is resolved at read time.
	Resolution Resolution
	// Batch is the batch item in the pantry, or nil.
	Batch *pantry.StockLevel
}

// List returns the specialty ingredients the household's recipes use, most
// used first, then by name. With all, it includes every curated specialty
// ingredient.
func (s *Service) List(ctx context.Context, householdID string, all bool) ([]View, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	sps, err := s.store.ListSpecialties(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list specialties: %w", err)
	}
	views, err := s.views(ctx, householdID, sps)
	if err != nil {
		return nil, err
	}
	if !all {
		views = slices.DeleteFunc(views, func(v View) bool { return v.RecipeCount == 0 })
	}
	slices.SortFunc(views, func(a, b View) int {
		if c := cmp.Compare(b.RecipeCount, a.RecipeCount); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(a.Specialty.Name), strings.ToLower(b.Specialty.Name))
	})
	return views, nil
}

// Get returns one specialty ingredient, including a retired one the
// household may still have chosen.
func (s *Service) Get(ctx context.Context, householdID, specialtyID string) (View, error) {
	if householdID == "" {
		return View{}, errHouseholdRequired
	}
	sp, err := s.store.GetSpecialty(ctx, specialtyID)
	if err != nil {
		return View{}, err
	}
	views, err := s.views(ctx, householdID, []Specialty{sp})
	if err != nil {
		return View{}, err
	}
	return views[0], nil
}

// views builds household views with a fixed number of queries: catalog keys,
// recipe use, choices, household options, and batch stock.
func (s *Service) views(ctx context.Context, householdID string, sps []Specialty) ([]View, error) {
	views := make([]View, len(sps))
	if len(sps) == 0 {
		return views, nil
	}
	var keys []string
	for i, sp := range sps {
		views[i].Specialty = sp
		keys = append(keys, sp.Key)
		keys = append(keys, sp.AliasKeys...)
	}
	found, err := s.catalog.IngredientsByKey(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("look up catalog ingredients: %w", err)
	}
	byKey := map[string][]string{}
	for _, ing := range found {
		byKey[ing.Key] = append(byKey[ing.Key], ing.ID)
	}
	idOwner := map[string]int{}
	var ids []string
	for i, sp := range sps {
		for _, k := range append([]string{sp.Key}, sp.AliasKeys...) {
			for _, id := range byKey[k] {
				views[i].IngredientIDs = append(views[i].IngredientIDs, id)
				idOwner[id] = i
				ids = append(ids, id)
			}
		}
		slices.Sort(views[i].IngredientIDs)
	}
	if s.recipes != nil && len(ids) > 0 {
		uses, err := s.recipes.FindIngredientUse(ctx, householdID, ids)
		if err != nil {
			return nil, err
		}
		for _, use := range uses {
			counted := map[int]bool{}
			for _, id := range use.IngredientIDs {
				if i, ok := idOwner[id]; ok && !counted[i] {
					counted[i] = true
					views[i].RecipeCount++
				}
			}
		}
	}

	choices, err := s.store.ListChoices(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("list choices: %w", err)
	}
	options, err := s.store.ListOptions(ctx, householdID, "")
	if err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}
	set, err := s.Settings(ctx, householdID)
	if err != nil {
		return nil, err
	}
	var levels map[string]pantry.StockLevel
	if s.pantry != nil {
		var batchKeys []string
		for _, sp := range sps {
			batchKeys = append(batchKeys, sp.Key)
		}
		if levels, err = s.pantry.StockLevels(ctx, householdID, batchKeys); err != nil {
			return nil, fmt.Errorf("pantry stock: %w", err)
		}
	}
	for i := range views {
		v := &views[i]
		v.Options = slices.Clone(v.Specialty.Options)
		for _, o := range options {
			if o.SpecialtyID == v.Specialty.ID {
				v.Options = append(v.Options, o)
			}
		}
		for _, c := range choices {
			if c.SpecialtyID != v.Specialty.ID {
				continue
			}
			v.Choice = &c
			if j := slices.IndexFunc(v.Options, func(o Option) bool { return o.ID == c.OptionID }); j >= 0 {
				v.ChoiceOption = &v.Options[j]
			}
		}
		if level, ok := levels[v.Specialty.Key]; ok {
			v.Batch = &level
		}
		v.Resolution = resolve(v.Specialty, v.Choice, options, set.Strategy)
	}
	return views, nil
}

// --- Settings -------------------------------------------------------------------

// Settings returns the household's specialty ingredient settings, or the
// defaults when it never set any.
func (s *Service) Settings(ctx context.Context, householdID string) (Settings, error) {
	if householdID == "" {
		return Settings{}, errHouseholdRequired
	}
	set, err := s.store.GetSettings(ctx, householdID)
	if errors.Is(err, ErrNotFound) {
		return DefaultSettings(householdID), nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("get settings: %w", err)
	}
	if !set.Strategy.Valid() {
		set.Strategy = DefaultStrategy
	}
	return set, nil
}

// SetStrategy sets the household's standing answer for specialty ingredients
// nobody has chosen an option for, and records specialty.strategy_updated.
// Nothing is written to the household's choices: the strategy is applied when
// a grocery list is built, so changing it changes future lists.
func (s *Service) SetStrategy(ctx context.Context, actor households.Membership, strategy string) (Settings, error) {
	if err := authorizeEdit(actor); err != nil {
		return Settings{}, err
	}
	next, err := normalizeStrategy(strategy)
	if err != nil {
		return Settings{}, err
	}
	previous, err := s.Settings(ctx, actor.HouseholdID)
	if err != nil {
		return Settings{}, err
	}
	saved, err := s.store.PutSettings(ctx, Settings{
		HouseholdID: actor.HouseholdID, Strategy: next, UpdatedBy: actor.UserID, UpdatedAt: s.timestamp(),
	})
	if err != nil {
		return Settings{}, fmt.Errorf("put settings: %w", err)
	}
	payload := events.SpecialtyStrategyUpdated{Strategy: string(next)}
	if !previous.UpdatedAt.IsZero() {
		payload.Previous = string(previous.Strategy)
	}
	events.RecordOrLog(ctx, s.events, s.logger, events.Event{
		HouseholdID: actor.HouseholdID, UserID: actor.UserID, Type: events.TypeSpecialtyStrategyUpdated,
		OccurredAt: s.timestamp(), Payload: payload,
	})
	s.logger.InfoContext(ctx, "specialty ingredient strategy set", "householdId", actor.HouseholdID, "strategy", next)
	return saved, nil
}

// activeSpecialty returns a specialty that isn't retired.
func (s *Service) activeSpecialty(ctx context.Context, id string) (Specialty, error) {
	sp, err := s.store.GetSpecialty(ctx, id)
	if err != nil {
		return Specialty{}, err
	}
	if sp.Retired {
		return Specialty{}, ErrNotFound
	}
	return sp, nil
}

// findOption returns a curated option of sp or one of the household's options
// for sp.
func (s *Service) findOption(ctx context.Context, householdID string, sp Specialty, id string) (Option, error) {
	if o, ok := sp.option(id); ok {
		return o, nil
	}
	o, err := s.store.GetOption(ctx, householdID, id)
	if errors.Is(err, ErrNotFound) || (err == nil && o.SpecialtyID != sp.ID) {
		return Option{}, ErrNotFound
	}
	return o, err
}

// SetChoice sets the household's option for a specialty ingredient: a curated
// or household option ID, or OptionAsIs.
func (s *Service) SetChoice(ctx context.Context, actor households.Membership, specialtyID, optionID string) (View, error) {
	if err := authorizeEdit(actor); err != nil {
		return View{}, err
	}
	sp, err := s.activeSpecialty(ctx, specialtyID)
	if err != nil {
		return View{}, err
	}
	optionID = strings.TrimSpace(optionID)
	switch {
	case optionID == "":
		return View{}, invalid("optionId is required")
	case optionID != OptionAsIs:
		if _, err := s.findOption(ctx, actor.HouseholdID, sp, optionID); errors.Is(err, ErrNotFound) {
			return View{}, invalid("optionId is not an option for %s", sp.Name)
		} else if err != nil {
			return View{}, err
		}
	}
	if _, err := s.store.PutChoice(ctx, Choice{
		HouseholdID: actor.HouseholdID, SpecialtyID: sp.ID, OptionID: optionID, ChosenBy: actor.UserID, ChosenAt: s.timestamp(),
	}); err != nil {
		return View{}, fmt.Errorf("put choice: %w", err)
	}
	s.logger.InfoContext(ctx, "specialty ingredient choice set", "householdId", actor.HouseholdID, "specialtyId", sp.ID, "optionId", optionID)
	return s.Get(ctx, actor.HouseholdID, sp.ID)
}

// ClearChoice removes the household's choice, so grocery lists keep the
// specialty ingredient and suggest options again.
func (s *Service) ClearChoice(ctx context.Context, actor households.Membership, specialtyID string) error {
	if err := authorizeEdit(actor); err != nil {
		return err
	}
	if _, err := s.store.GetSpecialty(ctx, specialtyID); err != nil {
		return err
	}
	return s.store.DeleteChoice(ctx, actor.HouseholdID, specialtyID)
}

// DefaultsResult is returned by ApplyDefaults.
type DefaultsResult struct {
	// Chosen are the specialty ingredients that got their default option.
	Chosen []View
	// Skipped counts those that already had a choice.
	Skipped int
}

// ApplyDefaults chooses the curated default option for every specialty
// ingredient the household's recipes use and that has no choice yet. Existing
// choices are never changed, so calling it again chooses nothing.
func (s *Service) ApplyDefaults(ctx context.Context, actor households.Membership) (DefaultsResult, error) {
	if err := authorizeEdit(actor); err != nil {
		return DefaultsResult{}, err
	}
	views, err := s.List(ctx, actor.HouseholdID, false)
	if err != nil {
		return DefaultsResult{}, err
	}
	var res DefaultsResult
	chosen := map[string]bool{}
	for _, v := range views {
		if v.Choice != nil || v.Specialty.DefaultOptionID == "" {
			res.Skipped++
			continue
		}
		inserted, err := s.store.InsertChoice(ctx, Choice{
			HouseholdID: actor.HouseholdID, SpecialtyID: v.Specialty.ID, OptionID: v.Specialty.DefaultOptionID,
			ChosenBy: actor.UserID, ChosenAt: s.timestamp(),
		})
		if err != nil {
			return DefaultsResult{}, fmt.Errorf("insert choice: %w", err)
		}
		if !inserted {
			res.Skipped++
			continue
		}
		chosen[v.Specialty.ID] = true
	}
	if len(chosen) > 0 {
		if views, err = s.List(ctx, actor.HouseholdID, false); err != nil {
			return DefaultsResult{}, err
		}
		for _, v := range views {
			if chosen[v.Specialty.ID] {
				res.Chosen = append(res.Chosen, v)
			}
		}
	}
	return res, nil
}

// OptionInput is a household option's content.
type OptionInput struct {
	Type            OptionType
	Name            string
	Notes           string
	Per             *Measure
	Ingredients     []Component
	Steps           []string
	Yield           *Measure
	ShelfLifeDays   int
	BasedOnOptionID string
}

func (s *Service) validOptionInput(ctx context.Context, householdID string, sp Specialty, selfID string, in OptionInput) (Option, error) {
	o, err := normalizeOption(Option{
		Type: in.Type, Name: in.Name, Notes: in.Notes, Per: in.Per, Ingredients: in.Ingredients,
		Steps: in.Steps, Yield: in.Yield, ShelfLifeDays: in.ShelfLifeDays,
	})
	if err != nil {
		return Option{}, err
	}
	if based := strings.TrimSpace(in.BasedOnOptionID); based != "" {
		if based == selfID {
			return Option{}, invalid("basedOnOptionId can't be the option itself")
		}
		if _, err := s.findOption(ctx, householdID, sp, based); errors.Is(err, ErrNotFound) {
			return Option{}, invalid("basedOnOptionId is not an option for %s", sp.Name)
		} else if err != nil {
			return Option{}, err
		}
		o.BasedOnOptionID = based
	}
	return o, nil
}

// CreateOption adds a household option for a specialty ingredient, for
// example the household's own version of a curated blend (BasedOnOptionID).
func (s *Service) CreateOption(ctx context.Context, actor households.Membership, specialtyID string, in OptionInput) (Option, error) {
	if err := authorizeEdit(actor); err != nil {
		return Option{}, err
	}
	sp, err := s.activeSpecialty(ctx, specialtyID)
	if err != nil {
		return Option{}, err
	}
	o, err := s.validOptionInput(ctx, actor.HouseholdID, sp, "", in)
	if err != nil {
		return Option{}, err
	}
	n, err := s.store.CountOptions(ctx, actor.HouseholdID, sp.ID)
	if err != nil {
		return Option{}, fmt.Errorf("count options: %w", err)
	}
	if n >= MaxOptionsPerSpecialty {
		return Option{}, invalid("a specialty ingredient holds at most %d household options", MaxOptionsPerSpecialty)
	}
	now := s.timestamp()
	o.HouseholdID, o.SpecialtyID, o.Source = actor.HouseholdID, sp.ID, SourceHousehold
	o.CreatedBy, o.UpdatedBy, o.CreatedAt, o.UpdatedAt = actor.UserID, actor.UserID, now, now
	saved, err := s.store.InsertOption(ctx, o)
	if err != nil {
		return Option{}, fmt.Errorf("insert option: %w", err)
	}
	return saved, nil
}

// UpdateOption replaces a household option's content.
func (s *Service) UpdateOption(ctx context.Context, actor households.Membership, specialtyID, optionID string, in OptionInput) (Option, error) {
	if err := authorizeEdit(actor); err != nil {
		return Option{}, err
	}
	sp, existing, err := s.householdOption(ctx, actor.HouseholdID, specialtyID, optionID)
	if err != nil {
		return Option{}, err
	}
	o, err := s.validOptionInput(ctx, actor.HouseholdID, sp, existing.ID, in)
	if err != nil {
		return Option{}, err
	}
	o.ID, o.HouseholdID, o.SpecialtyID, o.Source = existing.ID, actor.HouseholdID, sp.ID, SourceHousehold
	o.UpdatedBy, o.UpdatedAt = actor.UserID, s.timestamp()
	saved, err := s.store.ReplaceOption(ctx, o)
	if err != nil {
		return Option{}, err
	}
	return saved, nil
}

// DeleteOption deletes a household option and any choice of it.
func (s *Service) DeleteOption(ctx context.Context, actor households.Membership, specialtyID, optionID string) error {
	if err := authorizeEdit(actor); err != nil {
		return err
	}
	if _, _, err := s.householdOption(ctx, actor.HouseholdID, specialtyID, optionID); err != nil {
		return err
	}
	if err := s.store.DeleteOption(ctx, actor.HouseholdID, optionID); err != nil {
		return err
	}
	return s.store.DeleteChoicesForOption(ctx, actor.HouseholdID, optionID)
}

func (s *Service) householdOption(ctx context.Context, householdID, specialtyID, optionID string) (Specialty, Option, error) {
	sp, err := s.store.GetSpecialty(ctx, specialtyID)
	if err != nil {
		return Specialty{}, Option{}, err
	}
	o, err := s.store.GetOption(ctx, householdID, optionID)
	if err != nil {
		return Specialty{}, Option{}, err
	}
	if o.SpecialtyID != sp.ID {
		return Specialty{}, Option{}, ErrNotFound
	}
	return sp, o, nil
}

// BatchInput is the input for RecordBatch.
type BatchInput struct {
	// OptionID is a house-made batch option; empty uses the household's
	// choice.
	OptionID string
	// Batches is how many batches were made; 0 means 1.
	Batches          int
	ClientPurchaseID string
}

// BatchResult is returned by RecordBatch.
type BatchResult struct {
	pantry.PurchaseResult
	Option Option
}

// RecordBatch records that the household made a batch of a specialty
// ingredient: a pantry purchase with source house_made for the option's yield
// (times Batches), which starts a usage cycle on the batch item. Retrying
// with the same ClientPurchaseID changes nothing.
func (s *Service) RecordBatch(ctx context.Context, actor households.Membership, specialtyID string, in BatchInput) (BatchResult, error) {
	if err := authorizeEdit(actor); err != nil {
		return BatchResult{}, err
	}
	if s.pantry == nil {
		return BatchResult{}, errors.New("substitutes: pantry is not configured")
	}
	sp, err := s.activeSpecialty(ctx, specialtyID)
	if err != nil {
		return BatchResult{}, err
	}
	batches := in.Batches
	if batches == 0 {
		batches = 1
	}
	if batches < 1 || batches > MaxBatches {
		return BatchResult{}, invalid("batches must be between 1 and %d", MaxBatches)
	}
	optionID := strings.TrimSpace(in.OptionID)
	if optionID == "" {
		choices, err := s.store.ListChoices(ctx, actor.HouseholdID)
		if err != nil {
			return BatchResult{}, fmt.Errorf("list choices: %w", err)
		}
		for _, c := range choices {
			if c.SpecialtyID == sp.ID {
				optionID = c.OptionID
			}
		}
	}
	notBatch := invalid("choose a house-made batch option for %s, or send its optionId", sp.Name)
	if optionID == "" || optionID == OptionAsIs {
		return BatchResult{}, notBatch
	}
	option, err := s.findOption(ctx, actor.HouseholdID, sp, optionID)
	if errors.Is(err, ErrNotFound) {
		return BatchResult{}, invalid("optionId is not an option for %s", sp.Name)
	}
	if err != nil {
		return BatchResult{}, err
	}
	if option.Type != TypeHouseMadeBatch || option.Yield == nil {
		return BatchResult{}, notBatch
	}
	yield, err := ingredients.ParseQuantity(option.Yield.Quantity)
	if err != nil {
		return BatchResult{}, fmt.Errorf("substitutes: option %s yield: %w", option.ID, err)
	}
	in2 := pantry.HouseMadeInput{
		Key: sp.Key, DisplayName: sp.Name + " (house-made)", Category: sp.Category,
		Quantity: yield.Mul(ingredients.NewQuantity(int64(batches), 1)).String(), Unit: option.Yield.Unit,
		ShelfLifeDays: option.ShelfLifeDays, ClientPurchaseID: in.ClientPurchaseID,
	}
	if len(sp.UnitSizes) > 0 {
		u := sp.UnitSizes[0]
		in2.UnitSize = &pantry.UnitSize{Unit: u.Per, Quantity: u.Quantity, SizeUnit: u.Unit}
	}
	res, err := s.pantry.RecordHouseMade(ctx, actor, in2)
	if err != nil {
		return BatchResult{}, err
	}
	if res.Created {
		s.logger.InfoContext(ctx, "specialty ingredient batch recorded", "householdId", actor.HouseholdID, "specialtyId", sp.ID,
			"optionId", option.ID, "batches", batches, "itemId", res.Item.ID)
	}
	return BatchResult{PurchaseResult: res, Option: option}, nil
}

// --- Grocery lists and cook deductions --------------------------------------------

// index maps normalized names and aliases to specialties.
func index(sps []Specialty) map[string]*Specialty {
	out := map[string]*Specialty{}
	for i := range sps {
		out[sps[i].Key] = &sps[i]
		for _, k := range sps[i].AliasKeys {
			out[k] = &sps[i]
		}
	}
	return out
}

// GrocerySpecialties returns the specialty ingredients among a grocery list's
// lines, keyed by line key, with the household's choices resolved: store
// alternatives' ingredients linked to the catalog, and batches with their
// pantry stock. A line is a specialty ingredient when its catalog key, its
// name: key, or its normalized name is a curated name or alias. It
// implements planning.SpecialtySource.
func (s *Service) GrocerySpecialties(ctx context.Context, householdID string, lines []grocery.Line) (grocery.Specialties, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if len(lines) == 0 {
		return nil, nil
	}
	sps, err := s.store.ListSpecialties(ctx, false)
	if err != nil || len(sps) == 0 {
		return nil, err
	}
	byKey := index(sps)
	var ids []string
	for _, l := range lines {
		if !strings.HasPrefix(l.IngredientKey, pantry.UnresolvedKeyPrefix) && !slices.Contains(ids, l.IngredientKey) {
			ids = append(ids, l.IngredientKey)
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
	matched := map[string]*Specialty{}
	for _, l := range lines {
		for _, k := range []string{catalogKeys[l.IngredientKey], strings.TrimPrefix(l.IngredientKey, pantry.UnresolvedKeyPrefix), ingredients.NormalizeName(l.Name)} {
			if sp := byKey[k]; k != "" && sp != nil {
				matched[l.IngredientKey] = sp
				break
			}
		}
	}
	if len(matched) == 0 {
		return nil, nil
	}

	choices, err := s.store.ListChoices(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("list choices: %w", err)
	}
	householdOptions, err := s.store.ListOptions(ctx, householdID, "")
	if err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}
	set, err := s.Settings(ctx, householdID)
	if err != nil {
		return nil, err
	}
	// The household's explicit choice wins; where there is none, its strategy
	// picks an option. Nothing is stored: a later list reflects a later
	// setting.
	resolutions := map[string]Resolution{} // specialty ID → what applies
	var componentKeys, batchKeys []string
	for _, sp := range matched {
		if _, done := resolutions[sp.ID]; done {
			continue
		}
		var choice *Choice
		for i := range choices {
			if choices[i].SpecialtyID == sp.ID {
				choice = &choices[i]
			}
		}
		r := resolve(*sp, choice, householdOptions, set.Strategy)
		resolutions[sp.ID] = r
		if r.Option == nil {
			continue
		}
		for _, comp := range r.Option.Ingredients {
			componentKeys = append(componentKeys, ingredients.NormalizeName(comp.Name))
		}
		if r.Option.Type == TypeHouseMadeBatch {
			batchKeys = append(batchKeys, sp.Key)
		}
	}
	componentCatalog := map[string]recipes.Ingredient{}
	if len(componentKeys) > 0 {
		found, err := s.catalog.IngredientsByKey(ctx, componentKeys)
		if err != nil {
			return nil, fmt.Errorf("look up option ingredients: %w", err)
		}
		for _, ing := range found {
			componentCatalog[ing.Key] = ing
		}
	}
	levels := map[string]pantry.StockLevel{}
	if len(batchKeys) > 0 && s.pantry != nil {
		if levels, err = s.pantry.StockLevels(ctx, householdID, batchKeys); err != nil {
			return nil, fmt.Errorf("pantry stock: %w", err)
		}
	}

	built := map[string]*grocery.Specialty{}
	out := grocery.Specialties{}
	for lineKey, sp := range matched {
		gs, ok := built[sp.ID]
		if !ok {
			gs = s.grocerySpecialty(*sp, householdOptions, resolutions, componentCatalog, levels)
			built[sp.ID] = gs
		}
		out[lineKey] = gs
	}
	return out, nil
}

func (s *Service) grocerySpecialty(sp Specialty, householdOptions []Option, resolutions map[string]Resolution, catalog map[string]recipes.Ingredient, levels map[string]pantry.StockLevel) *grocery.Specialty {
	gs := &grocery.Specialty{ID: sp.ID, Key: sp.Key, Name: sp.Name, Aliases: slices.Clone(sp.Aliases), UnitSizes: groceryUnitSizes(sp.UnitSizes)}
	for _, o := range append(slices.Clone(sp.Options), householdOptions...) {
		if o.SpecialtyID == sp.ID {
			gs.Suggestions = append(gs.Suggestions, grocery.OptionRef{ID: o.ID, Type: grocery.ChoiceType(o.Type), Name: o.Name, IsDefault: o.ID == sp.DefaultOptionID})
		}
	}
	slices.SortStableFunc(gs.Suggestions, func(a, b grocery.OptionRef) int {
		switch {
		case a.IsDefault == b.IsDefault:
			return 0
		case a.IsDefault:
			return -1
		}
		return 1
	})
	r := resolutions[sp.ID]
	switch {
	case r.AsIs:
		gs.Choice = &grocery.Choice{Type: grocery.ChoiceAsIs, OptionID: OptionAsIs}
		return gs
	case r.Option == nil:
		return gs
	}
	o := *r.Option
	choice := &grocery.Choice{
		Type: grocery.ChoiceType(o.Type), OptionID: o.ID, OptionName: o.Name, Strategy: string(r.Strategy),
	}
	for _, c := range o.Ingredients {
		key := ingredients.NormalizeName(c.Name)
		comp := grocery.Component{IngredientKey: pantry.UnresolvedKeyPrefix + key, Name: c.Name, Category: c.Category, Unit: c.Unit}
		if ing, ok := catalog[key]; ok {
			comp.IngredientKey = ing.ID
			if ing.CategoryConfident || comp.Category == "" {
				comp.Category = ing.Category
			}
		}
		if comp.Category == "" {
			comp.Category, _ = ingredients.Categorize(c.Name)
		}
		if q, err := ingredients.ParseQuantity(c.Quantity); c.Quantity != "" && err == nil {
			comp.Quantity = &q
		}
		choice.Components = append(choice.Components, comp)
	}
	switch o.Type {
	case TypeStoreAlternative:
		choice.Per = groceryMeasure(o.Per)
	case TypeHouseMadeBatch:
		choice.Yield = groceryMeasure(o.Yield)
		choice.PantryKey = pantry.UnresolvedKeyPrefix + sp.Key
		if level, ok := levels[sp.Key]; ok {
			stock := &grocery.BatchStock{ItemID: level.ItemID, Status: string(level.Status), RemainingUnit: level.Unit}
			if level.Remaining != nil {
				q := ingredients.NewQuantity(1, 1).MulRat(level.Remaining)
				stock.Remaining = &q
			}
			if u := level.UnitSize; u != nil {
				if q, err := ingredients.ParseQuantity(u.Quantity); err == nil {
					stock.UnitSize = &grocery.UnitSize{Unit: u.Unit, Quantity: q, SizeUnit: u.SizeUnit}
				}
			}
			choice.Stock = stock
		}
	}
	gs.Choice = choice
	return gs
}

func groceryMeasure(m *Measure) grocery.Measure {
	if m == nil {
		return grocery.Measure{}
	}
	q, _ := ingredients.ParseQuantity(m.Quantity)
	return grocery.Measure{Quantity: q, Unit: m.Unit}
}

func groceryUnitSizes(sizes []UnitSize) []grocery.UnitSize {
	var out []grocery.UnitSize
	for _, u := range sizes {
		if q, err := ingredients.ParseQuantity(u.Quantity); err == nil {
			out = append(out, grocery.UnitSize{Unit: u.Per, Quantity: q, SizeUnit: u.Unit})
		}
	}
	return out
}

// ResolveKeys maps catalog keys and normalized names to the specialty
// ingredient's key and unit sizes, so a cooked recipe that names an alias or
// counts packets deducts from the batch. It implements pantry.KeyResolver.
func (s *Service) ResolveKeys(ctx context.Context, keys []string) (map[string]pantry.ResolvedKey, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	sps, err := s.store.ListSpecialties(ctx, false)
	if err != nil {
		return nil, err
	}
	byKey := index(sps)
	out := map[string]pantry.ResolvedKey{}
	for _, k := range keys {
		sp := byKey[k]
		if sp == nil {
			continue
		}
		r := pantry.ResolvedKey{Key: sp.Key}
		for _, u := range sp.UnitSizes {
			r.UnitSizes = append(r.UnitSizes, pantry.UnitSize{Unit: u.Per, Quantity: u.Quantity, SizeUnit: u.Unit})
		}
		out[k] = r
	}
	return out, nil
}
