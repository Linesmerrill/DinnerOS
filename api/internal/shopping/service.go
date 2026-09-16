package shopping

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// GrocerySource builds a week's grocery list with the pantry and specialty
// choices applied. *planning.Service implements it.
type GrocerySource interface {
	GroceryList(ctx context.Context, householdID, week string) (planning.GroceryList, error)
}

// Catalog resolves catalog ingredients. *recipes.Service implements it.
type Catalog interface {
	IngredientsByID(ctx context.Context, ids []string) ([]recipes.Ingredient, error)
}

// Pantry records confirmed orders. *pantry.Service implements it.
type Pantry interface {
	RecordProviderPurchase(ctx context.Context, actor households.Membership, in pantry.ProviderPurchaseInput) (pantry.PurchaseResult, error)
	FindProviderPurchase(ctx context.Context, householdID, handoffID, lineID string) (pantry.PurchaseResult, bool, error)
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Store     Store
	Providers *providers.Registry
	Grocery   GrocerySource
	// Catalog, when set, checks catalog IDs and names saved products.
	Catalog Catalog
	Pantry  Pantry
	// Households, when set, reads the household's order day and time zone for
	// the weekly order reminder. Without it the reminder is simply off.
	Households HouseholdSource
	// Notifier, when set, receives the weekly order reminder and is told when
	// a week is marked ordered.
	Notifier Notifier
	// Events, when set, records shopping.handoff_created and
	// shopping.order_confirmed (best effort).
	Events events.Recorder
	Logger *slog.Logger
}

// Service implements shopping handoffs. Reads take a household ID (routes
// authorize household.view); writes take the actor: settings, saved
// products, and handoffs check shopping.edit, and confirming an order checks
// pantry.edit, because it writes pantry purchases.
type Service struct {
	store      Store
	providers  *providers.Registry
	grocery    GrocerySource
	catalog    Catalog
	pantry     Pantry
	households HouseholdSource
	notifier   Notifier
	events     events.Recorder
	logger     *slog.Logger
	now        func() time.Time
}

// NewService returns a Service.
func NewService(o ServiceOptions) *Service {
	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{
		store: o.Store, providers: o.Providers, grocery: o.Grocery, catalog: o.Catalog, pantry: o.Pantry,
		households: o.Households, notifier: o.Notifier, events: o.Events, logger: logger, now: time.Now,
	}
}

func (s *Service) timestamp() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

func authorize(actor households.Membership, perm households.Permission) error {
	if actor.HouseholdID == "" || actor.UserID == "" {
		return errHouseholdRequired
	}
	if !actor.Role.Can(perm) {
		return ErrForbidden
	}
	return nil
}

// Providers returns the enabled providers.
func (s *Service) Providers() []providers.GroceryProvider { return s.providers.Enabled() }

// Provider returns the enabled provider named key: ErrUnknownProvider for a
// key DinnerOS doesn't plan to support, ErrProviderUnavailable for one that
// isn't enabled.
func (s *Service) Provider(key string) (providers.GroceryProvider, error) {
	k := providers.Key(key)
	if p, ok := s.providers.Get(k); ok {
		return p, nil
	}
	if k.Known() {
		return nil, ErrProviderUnavailable
	}
	return nil, ErrUnknownProvider
}

// --- Settings -----------------------------------------------------------------

// Settings returns the household's settings; unset settings have no
// provider.
func (s *Service) Settings(ctx context.Context, householdID string) (Settings, error) {
	if householdID == "" {
		return Settings{}, errHouseholdRequired
	}
	settings, err := s.store.GetSettings(ctx, householdID)
	if errors.Is(err, ErrNotFound) {
		return Settings{HouseholdID: householdID}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("get shopping settings: %w", err)
	}
	return settings, nil
}

// UpdateSettings sets the household's provider and optional store.
func (s *Service) UpdateSettings(ctx context.Context, actor households.Membership, provider, storeID string) (Settings, error) {
	if err := authorize(actor, households.PermShoppingEdit); err != nil {
		return Settings{}, err
	}
	if strings.TrimSpace(provider) == "" {
		return Settings{}, invalid("provider is required")
	}
	p, err := s.Provider(provider)
	if err != nil {
		return Settings{}, err
	}
	next := Settings{HouseholdID: actor.HouseholdID, Provider: p.Key(), UpdatedBy: actor.UserID, UpdatedAt: s.timestamp()}
	if strings.TrimSpace(storeID) != "" {
		if next.StoreID, err = p.NormalizeStoreID(storeID); err != nil {
			return Settings{}, err
		}
	}
	saved, err := s.store.PutSettings(ctx, next)
	if err != nil {
		return Settings{}, fmt.Errorf("put shopping settings: %w", err)
	}
	return saved, nil
}

// --- Saved products -----------------------------------------------------------

// ListPreferences returns the household's saved products for a provider.
func (s *Service) ListPreferences(ctx context.Context, householdID, provider string) ([]Preference, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	p, err := s.Provider(provider)
	if err != nil {
		return nil, err
	}
	return s.store.ListPreferences(ctx, householdID, p.Key())
}

// GetPreference returns the product saved for one ingredient.
func (s *Service) GetPreference(ctx context.Context, householdID, provider, ingredientKey string) (Preference, error) {
	if householdID == "" {
		return Preference{}, errHouseholdRequired
	}
	p, err := s.Provider(provider)
	if err != nil {
		return Preference{}, err
	}
	key, err := normalizeIngredientKey(ingredientKey)
	if err != nil {
		return Preference{}, err
	}
	return s.store.GetPreference(ctx, householdID, p.Key(), key)
}

// PutPreference saves the product a household buys for an ingredient from a
// pasted product link or product ID, read without fetching anything. It
// reports whether the preference was created.
func (s *Service) PutPreference(ctx context.Context, actor households.Membership, provider, ingredientKey string, in PreferenceInput) (Preference, bool, error) {
	if err := authorize(actor, households.PermShoppingEdit); err != nil {
		return Preference{}, false, err
	}
	p, err := s.Provider(provider)
	if err != nil {
		return Preference{}, false, err
	}
	key, err := normalizeIngredientKey(ingredientKey)
	if err != nil {
		return Preference{}, false, err
	}
	pref := Preference{HouseholdID: actor.HouseholdID, Provider: p.Key(), IngredientKey: key, UpdatedBy: actor.UserID, UpdatedAt: s.timestamp()}

	productURL, productID := strings.TrimSpace(in.ProductURL), strings.TrimSpace(in.ProductID)
	switch {
	case (productURL == "") == (productID == ""):
		return Preference{}, false, invalid("send productUrl or productId, not both")
	case productID != "" && strings.Trim(productID, "0123456789") != "":
		return Preference{}, false, invalid("productId must be the product's numeric ID; send a link as productUrl")
	}
	ref, err := p.ParseProduct(productURL + productID)
	if err != nil {
		return Preference{}, false, err
	}
	pref.ProductID = ref.ProductID

	if pref.DisplayName, err = cleanName(in.DisplayName, "displayName", true); err != nil {
		return Preference{}, false, err
	}
	if pref.PackageSize, err = normalizePackageSize(in.PackageSize); err != nil {
		return Preference{}, false, err
	}
	if pref.IngredientName, err = cleanName(in.IngredientName, "ingredientName", false); err != nil {
		return Preference{}, false, err
	}
	if id := catalogID(key); id != "" && s.catalog != nil {
		found, err := s.catalog.IngredientsByID(ctx, []string{id})
		if err != nil {
			return Preference{}, false, fmt.Errorf("look up catalog ingredient: %w", err)
		}
		if len(found) == 0 {
			return Preference{}, false, invalid("ingredientKey doesn't match a catalog ingredient")
		}
		if pref.IngredientName == "" {
			pref.IngredientName = found[0].Name
		}
	}
	if pref.IngredientName == "" {
		pref.IngredientName = strings.TrimPrefix(key, unnamedKeyPrefix)
	}

	if _, err := s.store.GetPreference(ctx, actor.HouseholdID, p.Key(), key); errors.Is(err, ErrNotFound) {
		n, err := s.store.CountPreferences(ctx, actor.HouseholdID, p.Key())
		if err != nil {
			return Preference{}, false, fmt.Errorf("count saved products: %w", err)
		}
		if n >= MaxPreferencesPerProvider {
			return Preference{}, false, invalid("a household saves at most %d products per store", MaxPreferencesPerProvider)
		}
	} else if err != nil {
		return Preference{}, false, fmt.Errorf("get saved product: %w", err)
	}
	return s.store.UpsertPreference(ctx, pref)
}

// DeletePreference removes the product saved for one ingredient.
func (s *Service) DeletePreference(ctx context.Context, actor households.Membership, provider, ingredientKey string) error {
	if err := authorize(actor, households.PermShoppingEdit); err != nil {
		return err
	}
	p, err := s.Provider(provider)
	if err != nil {
		return err
	}
	key, err := normalizeIngredientKey(ingredientKey)
	if err != nil {
		return err
	}
	return s.store.DeletePreference(ctx, actor.HouseholdID, p.Key(), key)
}

func cleanName(name, field string, required bool) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "" && required:
		return "", invalid("%s is required", field)
	case utf8.RuneCountInString(name) > MaxDisplayNameLength:
		return "", invalid("%s must be at most %d characters", field, MaxDisplayNameLength)
	}
	return name, nil
}

func normalizePackageSize(size *PackageSize) (*PackageSize, error) {
	if size == nil {
		return nil, nil
	}
	const msg = "packageSize needs a positive quantity (such as 16, 1.5, or 1/2) and a DinnerOS unit code (such as oz, floz, lb, g, count, or can)"
	quantity := strings.TrimSpace(size.Quantity)
	q, err := ingredients.ParseQuantity(quantity)
	if err != nil || q.IsZero() || len(quantity) > maxQuantityLength {
		return nil, invalid(msg)
	}
	if _, err := ingredients.LookupUnit(strings.TrimSpace(size.Unit)); err != nil {
		return nil, invalid(msg)
	}
	return &PackageSize{Quantity: q.String(), Unit: strings.TrimSpace(size.Unit)}, nil
}

// --- Match and handoff --------------------------------------------------------

// Match matches the week's grocery list to the household's saved products
// for a provider, without storing anything.
func (s *Service) Match(ctx context.Context, householdID, week, provider string, in MatchInput) (Proposal, error) {
	if householdID == "" {
		return Proposal{}, errHouseholdRequired
	}
	p, err := s.Provider(provider)
	if err != nil {
		return Proposal{}, err
	}
	if in, err = validateMatchInput(in); err != nil {
		return Proposal{}, err
	}
	settings, err := s.Settings(ctx, householdID)
	if err != nil {
		return Proposal{}, err
	}
	g, err := s.grocery.GroceryList(ctx, householdID, week)
	if err != nil {
		return Proposal{}, err
	}
	prefs, err := s.store.ListPreferences(ctx, householdID, p.Key())
	if err != nil {
		return Proposal{}, fmt.Errorf("list saved products: %w", err)
	}
	return buildProposal(p, settings, g, prefs, in)
}

// CreateHandoff matches the week's list like Match and stores the result,
// so what members confirm can be recorded. It needs at least one line with a
// saved product.
func (s *Service) CreateHandoff(ctx context.Context, actor households.Membership, week, provider string, in MatchInput) (Handoff, error) {
	if err := authorize(actor, households.PermShoppingEdit); err != nil {
		return Handoff{}, err
	}
	proposal, err := s.Match(ctx, actor.HouseholdID, week, provider, in)
	if err != nil {
		return Handoff{}, err
	}
	if len(proposal.Lines) == 0 {
		return Handoff{}, invalid("no grocery line has a saved product to add to the cart")
	}
	now := s.timestamp()
	h, err := s.store.InsertHandoff(ctx, Handoff{Proposal: proposal, CreatedBy: actor.UserID, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return Handoff{}, fmt.Errorf("insert handoff: %w", err)
	}
	payload := events.ShoppingHandoffCreated{HandoffID: h.ID, Provider: string(h.Provider), Lines: len(h.Lines), Excluded: len(h.Excluded), Links: len(h.Links)}
	for _, l := range h.Lines {
		payload.Packages += l.Packages
		if l.Reason != "" {
			payload.CheckAmount++
		}
	}
	events.RecordOrLog(ctx, s.events, s.logger, events.Event{
		HouseholdID: h.HouseholdID, UserID: actor.UserID, Type: events.TypeShoppingHandoffCreated, Week: h.Week, OccurredAt: now, Payload: payload,
	})
	s.logger.InfoContext(ctx, "shopping handoff created", "householdId", h.HouseholdID, "handoffId", h.ID, "provider", h.Provider,
		"lines", len(h.Lines), "links", len(h.Links))
	return h, nil
}

var isoWeek = regexp.MustCompile(`^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$`)

// ListHandoffs returns the household's handoffs, newest first.
func (s *Service) ListHandoffs(ctx context.Context, householdID string, f HandoffFilter) ([]Handoff, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	switch {
	case f.Week != "" && !isoWeek.MatchString(f.Week):
		return nil, invalid("week must be an ISO week such as 2026-W38")
	case f.Status != "" && f.Status != HandoffOpen && f.Status != HandoffDone:
		return nil, invalid("status must be open or done")
	case f.Limit < 0 || f.Limit > MaxHandoffList:
		return nil, invalid("limit must be between 1 and %d", MaxHandoffList)
	}
	return s.store.ListHandoffs(ctx, householdID, f)
}

// GetHandoff returns one of the household's handoffs.
func (s *Service) GetHandoff(ctx context.Context, householdID, id string) (Handoff, error) {
	if householdID == "" {
		return Handoff{}, errHouseholdRequired
	}
	return s.store.GetHandoff(ctx, householdID, id)
}

// --- Confirm ------------------------------------------------------------------

// ConfirmedPurchase is the pantry purchase for one confirmed line. Created
// is false when the line was already confirmed.
type ConfirmedPurchase struct {
	LineID string
	pantry.PurchaseResult
}

// ConfirmResult is returned by Service.Confirm.
type ConfirmResult struct {
	Handoff   Handoff
	Purchases []ConfirmedPurchase
}

// Confirm records what a member says was ordered from a handoff. Each
// confirmed line becomes one pantry purchase with source provider, written
// from the stored line: a package count with the product's size, or an
// exact count when the size is itself a count. Confirming is idempotent per
// line: a confirmed line keeps its first purchase, which is returned again.
// SkipRest marks the other pending lines as not ordered.
func (s *Service) Confirm(ctx context.Context, actor households.Membership, handoffID string, in ConfirmInput) (ConfirmResult, error) {
	if err := authorize(actor, households.PermPantryEdit); err != nil {
		return ConfirmResult{}, err
	}
	h, err := s.store.GetHandoff(ctx, actor.HouseholdID, handoffID)
	if err != nil {
		return ConfirmResult{}, err
	}
	targets, err := confirmTargets(h, in)
	if err != nil {
		return ConfirmResult{}, err
	}

	var res ConfirmResult
	payload := events.ShoppingOrderConfirmed{HandoffID: h.ID, Provider: string(h.Provider)}
	for _, t := range targets {
		purchase, created, err := s.confirmLine(ctx, actor, h, t)
		if err != nil {
			return ConfirmResult{}, err
		}
		if purchase != nil {
			res.Purchases = append(res.Purchases, ConfirmedPurchase{LineID: t.LineID, PurchaseResult: *purchase})
		}
		if created {
			payload.Confirmed++
			payload.Packages += t.Packages
		}
	}
	if in.SkipRest {
		chosen := map[string]bool{}
		for _, t := range targets {
			chosen[t.LineID] = true
		}
		for _, l := range h.Lines {
			if l.Status != LinePending || chosen[l.ID] {
				continue
			}
			skipped, err := s.store.SkipLine(ctx, h.HouseholdID, h.ID, l.ID, actor.UserID, s.timestamp())
			if err != nil {
				return ConfirmResult{}, fmt.Errorf("skip handoff line: %w", err)
			}
			if skipped {
				payload.Skipped++
			}
		}
	}
	if res.Handoff, err = s.store.GetHandoff(ctx, h.HouseholdID, h.ID); err != nil {
		return ConfirmResult{}, err
	}
	if payload.Confirmed > 0 || payload.Skipped > 0 {
		events.RecordOrLog(ctx, s.events, s.logger, events.Event{
			HouseholdID: h.HouseholdID, UserID: actor.UserID, Type: events.TypeShoppingOrderConfirmed, Week: h.Week,
			OccurredAt: s.timestamp(), Payload: payload,
		})
		s.logger.InfoContext(ctx, "shopping order confirmed", "householdId", h.HouseholdID, "handoffId", h.ID,
			"confirmed", payload.Confirmed, "skipped", payload.Skipped)
	}
	return res, nil
}

// confirmTargets resolves what to confirm, in handoff line order, with
// package counts filled in.
func confirmTargets(h Handoff, in ConfirmInput) ([]ConfirmLine, error) {
	switch {
	case in.All && len(in.Lines) > 0:
		return nil, invalid("send all or lines, not both")
	case !in.All && len(in.Lines) == 0 && !in.SkipRest:
		return nil, invalid("send all: true, the lines that were ordered, or skipRest: true when nothing was")
	case len(in.Lines) > len(h.Lines):
		return nil, invalid("lines lists more lines than the handoff has")
	}
	requested := map[string]int{}
	for _, l := range in.Lines {
		if _, dup := requested[l.LineID]; dup {
			return nil, invalid("lines lists %s more than once", l.LineID)
		}
		if _, ok := h.Line(l.LineID); !ok {
			return nil, invalid("line %q isn't in this handoff", l.LineID)
		}
		if l.Packages < 0 || l.Packages > providers.MaxPackages {
			return nil, invalid("packages must be between 1 and %d", providers.MaxPackages)
		}
		requested[l.LineID] = l.Packages
	}
	var out []ConfirmLine
	for _, l := range h.Lines {
		packages, ok := requested[l.ID]
		if in.All {
			ok = l.Status != LineSkipped
		}
		if !ok {
			continue
		}
		if packages == 0 {
			packages = l.Packages
		}
		out = append(out, ConfirmLine{LineID: l.ID, Packages: packages})
	}
	return out, nil
}

// confirmLine records one line's purchase. It returns the purchase (nil
// only when a confirmed line's purchase is missing) and whether this call
// confirmed the line.
func (s *Service) confirmLine(ctx context.Context, actor households.Membership, h Handoff, t ConfirmLine) (*pantry.PurchaseResult, bool, error) {
	line, _ := h.Line(t.LineID)
	existing := func() (*pantry.PurchaseResult, bool, error) {
		found, ok, err := s.pantry.FindProviderPurchase(ctx, h.HouseholdID, h.ID, line.ID)
		if err != nil || !ok {
			return nil, false, err
		}
		return &found, false, nil
	}
	if line.Status == LineConfirmed {
		return existing()
	}
	now := s.timestamp()
	claimed, err := s.store.ClaimLine(ctx, h.HouseholdID, h.ID, line.ID, now, now.Add(-claimTimeout))
	if err != nil {
		return nil, false, fmt.Errorf("claim handoff line: %w", err)
	}
	if !claimed {
		current, err := s.store.GetHandoff(ctx, h.HouseholdID, h.ID)
		if err != nil {
			return nil, false, err
		}
		if l, ok := current.Line(line.ID); ok && l.Status == LineConfirmed {
			return existing()
		}
		return nil, false, ErrConflict
	}
	recorded, err := s.pantry.RecordProviderPurchase(ctx, actor, providerPurchase(h, line, t.Packages))
	if err != nil {
		if releaseErr := s.store.ReleaseLine(context.WithoutCancel(ctx), h.HouseholdID, h.ID, line.ID); releaseErr != nil {
			s.logger.WarnContext(ctx, "release handoff line failed", "handoffId", h.ID, "lineId", line.ID, "error", releaseErr)
		}
		return nil, false, err
	}
	line.ConfirmedPackages, line.PurchaseID, line.ConfirmedBy, line.ConfirmedAt = t.Packages, recorded.Purchase.ID, actor.UserID, now
	if err := s.store.ConfirmLine(ctx, h.HouseholdID, h.ID, line, now); err != nil {
		return nil, false, fmt.Errorf("confirm handoff line: %w", err)
	}
	return &recorded, true, nil
}

// providerPurchase maps a confirmed line to the pantry purchase: packages
// with the product's measured size ("2 package, 1 package = 16 oz"), an
// exact amount when the size is a discrete unit (2 × 12 count = 24 count),
// or a bare package count without a size.
func providerPurchase(h Handoff, line HandoffLine, packages int) pantry.ProviderPurchaseInput {
	in := pantry.ProviderPurchaseInput{
		Quantity: strconv.Itoa(packages), Unit: "package", Week: h.Week,
		Provider: pantry.ProviderRef{Key: string(h.Provider), HandoffID: h.ID, LineID: line.ID, ProductID: line.ProductID},
	}
	if id := line.IngredientID(); id != "" {
		in.IngredientID = id
	} else {
		name := strings.TrimPrefix(line.IngredientKey, unnamedKeyPrefix)
		in.Name = line.Name
		if ingredients.NormalizeName(line.Name) != name {
			in.Name = name
		}
	}
	if size := amountFromSize(line.PackageSize); size != nil {
		if size.Unit.Discrete() {
			in.Quantity = size.Quantity.Mul(ingredients.NewQuantity(int64(packages), 1)).String()
			in.Unit = size.Unit.Code
		} else {
			in.UnitSize = &pantry.UnitSize{Unit: "package", Quantity: size.Quantity.String(), SizeUnit: size.Unit.Code}
		}
	}
	return in
}
