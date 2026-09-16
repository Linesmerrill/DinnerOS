package pantry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// PurchaseSource says where a purchase record came from.
type PurchaseSource string

// Purchase sources. Clients may record grocery_list and manual purchases.
// provider is a member-confirmed order from a shopping handoff, recorded only
// by server code through Service.RecordProviderPurchase. house_made is a
// batch of a specialty ingredient the household made, recorded through
// Service.RecordHouseMade.
const (
	PurchaseGroceryList PurchaseSource = PurchaseSource(CycleGroceryList)
	PurchaseManual      PurchaseSource = PurchaseSource(CycleManual)
	PurchaseProvider    PurchaseSource = PurchaseSource(CycleProvider)
	PurchaseHouseMade   PurchaseSource = PurchaseSource(CycleHouseMade)
)

// Purchase is one recorded purchase of a pantry item. Its ID is also the ID
// of the usage cycle it started.
type Purchase struct {
	ID          string
	HouseholdID string
	ItemID      string
	ItemKey     string
	Source      PurchaseSource
	// Quantity and Unit are what was bought; both empty when no amount was
	// recorded.
	Quantity string
	Unit     string
	UnitSize *UnitSize
	// Week is the ISO week of the grocery list it was checked off, or empty.
	Week string
	// ClientPurchaseID is the app's idempotency key, unique per member.
	ClientPurchaseID string
	// Provider is set on provider purchases: the handoff line it confirms.
	Provider *ProviderRef
	// PriceCents is what the household paid for the whole purchase, in US
	// cents, or nil when nobody entered a price (prices.go).
	PriceCents  *int64
	RecordedBy  string
	PurchasedAt time.Time
}

// ProviderRef links a provider purchase to the shopping handoff line it
// confirms. (HouseholdID, HandoffID, LineID) is unique.
type ProviderRef struct {
	// Key is the provider ("walmart").
	Key       string
	HandoffID string
	LineID    string
	// ProductID is the provider's product (a Walmart item ID).
	ProductID string
}

// PurchaseInput is the input for Service.RecordPurchase. ItemID, or
// IngredientID or Name, identifies the item; a new ingredient is added.
type PurchaseInput struct {
	ItemID       string
	IngredientID string
	Name         string
	Source       PurchaseSource
	Quantity     string
	Unit         string
	// UnitSizeQuantity and UnitSizeUnit say how much one Unit holds, when
	// Unit is a discrete unit such as package.
	UnitSizeQuantity string
	UnitSizeUnit     string
	Week             string
	ClientPurchaseID string
	// PriceCents is optional: what the whole purchase cost.
	PriceCents *int64
}

// PurchaseResult is returned by Service.RecordPurchase. Created is false when
// ClientPurchaseID matched an earlier purchase, which is returned instead.
type PurchaseResult struct {
	Purchase Purchase
	Item     Item
	Created  bool
}

// Settings are a household's pantry settings.
type Settings struct {
	HouseholdID string
	// LowThresholdPercent is how much of an item's starting amount may be
	// used before the estimate marks it low.
	LowThresholdPercent int
	// UpdatedBy and UpdatedAt are empty until someone changes the defaults.
	UpdatedBy string
	UpdatedAt time.Time
}

// UsageStore persists purchases, cook deductions, and settings. Every method
// is scoped by householdID. Missing records are ErrNotFound; unique key
// violations are ErrDuplicate (wrapped).
type UsageStore interface {
	// InsertPurchase stores p with its given ID. A second purchase with the
	// same (householdId, recordedBy, clientPurchaseId) is ErrDuplicate.
	InsertPurchase(ctx context.Context, p Purchase) (Purchase, error)
	// FindPurchaseByClientID returns a member's purchase by its client ID.
	FindPurchaseByClientID(ctx context.Context, householdID, userID, clientPurchaseID string) (Purchase, error)
	// FindPurchaseByProviderLine returns the provider purchase that confirms
	// a handoff line. A second purchase for the same (householdId,
	// handoffId, lineId) is ErrDuplicate on insert.
	FindPurchaseByProviderLine(ctx context.Context, householdID, handoffID, lineID string) (Purchase, error)
	// ListPurchases returns an item's purchases, newest first, at most limit.
	ListPurchases(ctx context.Context, householdID, itemID string, limit int) ([]Purchase, error)
	// InsertCookUsage stores u, assigning its ID. A second record with the
	// same (householdId, sourceKey) is ErrDuplicate: that meal was already
	// deducted.
	InsertCookUsage(ctx context.Context, u CookUsage) (CookUsage, error)
	// GetSettings returns the household's settings, or ErrNotFound when
	// they were never changed.
	GetSettings(ctx context.Context, householdID string) (Settings, error)
	// PutSettings creates or replaces the household's settings.
	PutSettings(ctx context.Context, s Settings) (Settings, error)
	// SetPurchasePrice sets (or, with nil, clears) a purchase's price and
	// returns the purchase.
	SetPurchasePrice(ctx context.Context, householdID, purchaseID string, priceCents *int64) (Purchase, error)
	// PurchasesByIDs returns the household's purchases with these IDs, in no
	// particular order; unknown IDs are left out.
	PurchasesByIDs(ctx context.Context, householdID string, ids []string) ([]Purchase, error)
	// ListCookUsageByEntries returns the cook records of these plan entries.
	ListCookUsageByEntries(ctx context.Context, householdID string, entryIDs []string) ([]CookUsage, error)
}

// RecipeReader loads one of a household's recipes with its ingredient lines.
// *recipes.Service implements it.
type RecipeReader interface {
	// Get returns recipes.ErrNotFound for recipes outside the household.
	Get(ctx context.Context, householdID, id string) (recipes.Recipe, error)
}

// Notifier creates household notifications. *notifications.Service
// implements it.
type Notifier interface {
	Create(ctx context.Context, in notifications.New) (notifications.Notification, bool, error)
}

// UsageOptions configures usage tracking (Service.WithUsage).
type UsageOptions struct {
	Store UsageStore
	// Recipes is needed to deduct cooked recipes.
	Recipes RecipeReader
	// Notifier, when set, receives low-stock alerts.
	Notifier Notifier
	Logger   *slog.Logger
}

// Limits for usage input.
const (
	MaxPurchaseHistory        = 20
	MaxClientPurchaseIDLength = 64
)

var errUsageNotConfigured = errors.New("pantry: usage tracking is not configured")

var isoWeek = regexp.MustCompile(`^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$`)

// WithUsage enables purchases, cook deductions, estimates with a household
// threshold, and low-stock alerts, and returns s.
func (s *Service) WithUsage(o UsageOptions) *Service {
	s.usage, s.recipes, s.notifier = o.Store, o.Recipes, o.Notifier
	if o.Logger != nil {
		s.logger = o.Logger
	}
	return s
}

// --- Settings -----------------------------------------------------------------

// Settings returns the household's settings, or the defaults.
func (s *Service) Settings(ctx context.Context, householdID string) (Settings, error) {
	if householdID == "" {
		return Settings{}, errHouseholdRequired
	}
	defaults := Settings{HouseholdID: householdID, LowThresholdPercent: DefaultLowThresholdPercent}
	if s.usage == nil {
		return defaults, nil
	}
	settings, err := s.usage.GetSettings(ctx, householdID)
	if errors.Is(err, ErrNotFound) {
		return defaults, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("get pantry settings: %w", err)
	}
	return settings, nil
}

// UpdateSettings sets the household's low-stock threshold (1–100 percent of
// the starting amount used). New estimates use it right away.
func (s *Service) UpdateSettings(ctx context.Context, actor households.Membership, lowThresholdPercent int) (Settings, error) {
	if err := authorizeEdit(actor); err != nil {
		return Settings{}, err
	}
	if s.usage == nil {
		return Settings{}, errUsageNotConfigured
	}
	if lowThresholdPercent < 1 || lowThresholdPercent > 100 {
		return Settings{}, invalid("lowThresholdPercent must be between 1 and 100")
	}
	saved, err := s.usage.PutSettings(ctx, Settings{
		HouseholdID: actor.HouseholdID, LowThresholdPercent: lowThresholdPercent,
		UpdatedBy: actor.UserID, UpdatedAt: s.timestamp(),
	})
	if err != nil {
		return Settings{}, fmt.Errorf("put pantry settings: %w", err)
	}
	return saved, nil
}

// Estimate estimates item's remaining amount now under settings, or returns
// nil when the item isn't tracked.
func (s *Service) Estimate(item Item, settings Settings) *Estimate {
	return estimateItem(item, settings.LowThresholdPercent, s.timestamp())
}

// --- Purchases ----------------------------------------------------------------

type purchase struct {
	price    *int64
	source   PurchaseSource
	quantity string
	unit     string
	unitSize *UnitSize
	week     string
	clientID string
	provider *ProviderRef
}

func validatePurchase(in PurchaseInput) (purchase, error) {
	var p purchase
	switch in.Source {
	case PurchaseGroceryList, PurchaseManual:
		p.source = in.Source
	case PurchaseProvider:
		return purchase{}, invalid("source provider is recorded by shopping providers, not apps")
	case PurchaseHouseMade:
		return purchase{}, invalid("source house_made is recorded by POST .../specialty-ingredients/{specialtyId}/batches")
	default:
		return purchase{}, invalid("source must be grocery_list or manual")
	}
	hasItem := strings.TrimSpace(in.ItemID) != ""
	hasIngredient := strings.TrimSpace(in.IngredientID) != "" || strings.TrimSpace(in.Name) != ""
	switch {
	case hasItem && hasIngredient:
		return purchase{}, invalid("send itemId, or ingredientId or name, not both")
	case !hasItem && !hasIngredient:
		return purchase{}, invalid("itemId, ingredientId, or name is required")
	}
	var err error
	if p.quantity, p.unit, err = normalizeAmount(in.Quantity, in.Unit); err != nil {
		return purchase{}, err
	}
	sizeQ, sizeU := strings.TrimSpace(in.UnitSizeQuantity), strings.TrimSpace(in.UnitSizeUnit)
	if sizeQ != "" || sizeU != "" {
		if p.quantity == "" {
			return purchase{}, invalid("unitSize requires a quantity")
		}
		if u, _ := ingredients.LookupUnit(p.unit); !u.Discrete() {
			return purchase{}, invalid("unitSize applies only to a unit such as count, package, or can")
		}
		q, unit, err := normalizeAmount(sizeQ, sizeU)
		if err != nil || sizeU == "" {
			return purchase{}, invalid("unitSize needs a positive quantity and a unit")
		}
		if u, _ := ingredients.LookupUnit(unit); u.Discrete() {
			return purchase{}, invalid("unitSize.unit must be a volume or weight unit")
		}
		p.unitSize = &UnitSize{Unit: p.unit, Quantity: q, SizeUnit: unit}
	}
	if p.week = strings.TrimSpace(in.Week); p.week != "" {
		if p.source != PurchaseGroceryList {
			return purchase{}, invalid("week applies only to grocery_list purchases")
		}
		if !isoWeek.MatchString(p.week) {
			return purchase{}, invalid("week must be an ISO week such as 2026-W38")
		}
	}
	if err := validatePrice(in.PriceCents); err != nil {
		return purchase{}, err
	}
	p.price = in.PriceCents
	p.clientID = in.ClientPurchaseID
	switch {
	case len(p.clientID) > MaxClientPurchaseIDLength:
		return purchase{}, invalid("clientPurchaseId must be at most %d characters", MaxClientPurchaseIDLength)
	case strings.TrimSpace(p.clientID) != p.clientID:
		return purchase{}, invalid("clientPurchaseId must not have surrounding whitespace")
	}
	return p, nil
}

// RecordPurchase records that the household bought an item, for example when
// a member checks it off a grocery list and confirms "Add to pantry?", or
// restocks it by hand. The item (added if needed) is in stock with the bought
// amount, and a new usage cycle starts from that amount. Retrying with the
// same ClientPurchaseID returns the first purchase and changes nothing.
func (s *Service) RecordPurchase(ctx context.Context, actor households.Membership, in PurchaseInput) (PurchaseResult, error) {
	if err := authorizeEdit(actor); err != nil {
		return PurchaseResult{}, err
	}
	if s.usage == nil {
		return PurchaseResult{}, errUsageNotConfigured
	}
	p, err := validatePurchase(in)
	if err != nil {
		return PurchaseResult{}, err
	}
	if p.clientID != "" {
		if res, found, err := s.existingPurchase(ctx, actor, p.clientID); found || err != nil {
			return res, err
		}
	}
	itemID := strings.TrimSpace(in.ItemID)
	var a addition
	if itemID == "" {
		if a, err = s.validateAddition(ctx, AddInput{IngredientID: in.IngredientID, Name: in.Name}); err != nil {
			return PurchaseResult{}, err
		}
	}
	return s.recordPurchase(ctx, actor, p, itemID, a, nil)
}

// recordPurchase restocks the item itemID, or the item with a's key (added
// when missing), and stores the purchase. prepare, when set, changes the item
// before the restock.
func (s *Service) recordPurchase(ctx context.Context, actor households.Membership, p purchase, itemID string, a addition, prepare func(*Item, time.Time)) (PurchaseResult, error) {
	hh := actor.HouseholdID
	purchaseID := s.newID()
	var err error
	var saved Item
	for attempt := 0; ; attempt++ {
		if attempt == maxWriteAttempts {
			return PurchaseResult{}, ErrConflict
		}
		now := s.timestamp()
		var current Item
		exists := true
		if itemID != "" {
			if current, err = s.store.GetItem(ctx, hh, itemID); err != nil {
				return PurchaseResult{}, err
			}
		} else {
			found, err := s.store.FindItemsByKeys(ctx, hh, []string{a.key})
			if err != nil {
				return PurchaseResult{}, fmt.Errorf("find pantry item: %w", err)
			}
			if len(found) > 0 {
				current = found[0]
			} else {
				exists = false
				if err := s.checkCapacity(ctx, hh, 1); err != nil {
					return PurchaseResult{}, err
				}
				current = newItem(hh, a, actor.UserID, now)
			}
		}
		next := cloneItem(current)
		if p.unitSize != nil {
			next.UnitSize = p.unitSize
		}
		if prepare != nil {
			prepare(&next, now)
		}
		restock(&next, purchaseID, CycleSource(p.source), p.quantity, p.unit, now)
		next.UpdatedBy, next.UpdatedAt = actor.UserID, now
		if exists {
			saved, err = s.store.UpdateItem(ctx, next)
			if errors.Is(err, ErrConflict) || (itemID == "" && errors.Is(err, ErrNotFound)) {
				continue
			}
		} else {
			saved, err = s.store.InsertItem(ctx, next)
			if errors.Is(err, ErrDuplicate) {
				continue
			}
		}
		if err != nil {
			return PurchaseResult{}, err
		}
		break
	}

	record, err := s.usage.InsertPurchase(ctx, Purchase{
		ID: purchaseID, HouseholdID: hh, ItemID: saved.ID, ItemKey: saved.Key, Source: p.source,
		Quantity: p.quantity, Unit: p.unit, UnitSize: p.unitSize, Week: p.week,
		ClientPurchaseID: p.clientID, Provider: p.provider, PriceCents: p.price, RecordedBy: actor.UserID, PurchasedAt: saved.UpdatedAt,
	})
	if errors.Is(err, ErrDuplicate) && p.provider != nil {
		// A concurrent confirmation of the same handoff line recorded it first.
		res, _, err := s.FindProviderPurchase(ctx, hh, p.provider.HandoffID, p.provider.LineID)
		return res, err
	}
	if errors.Is(err, ErrDuplicate) && p.clientID != "" {
		// A concurrent retry recorded it first.
		res, _, err := s.existingPurchase(ctx, actor, p.clientID)
		return res, err
	}
	if err != nil {
		return PurchaseResult{}, fmt.Errorf("insert pantry purchase: %w", err)
	}
	s.logger.InfoContext(ctx, "pantry purchase recorded",
		"householdId", hh, "itemId", saved.ID, "purchaseId", record.ID, "source", record.Source, "tracked", saved.Tracking != nil)
	return PurchaseResult{Purchase: record, Item: saved, Created: true}, nil
}

func (s *Service) existingPurchase(ctx context.Context, actor households.Membership, clientID string) (PurchaseResult, bool, error) {
	existing, err := s.usage.FindPurchaseByClientID(ctx, actor.HouseholdID, actor.UserID, clientID)
	if errors.Is(err, ErrNotFound) {
		return PurchaseResult{}, false, nil
	}
	if err != nil {
		return PurchaseResult{}, false, fmt.Errorf("find pantry purchase: %w", err)
	}
	item, err := s.store.GetItem(ctx, actor.HouseholdID, existing.ItemID)
	if err != nil {
		return PurchaseResult{}, true, err
	}
	return PurchaseResult{Purchase: existing, Item: item}, true, nil
}

// ListPurchases returns an item's most recent purchases, newest first.
func (s *Service) ListPurchases(ctx context.Context, householdID, itemID string) ([]Purchase, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if s.usage == nil {
		return nil, errUsageNotConfigured
	}
	if _, err := s.store.GetItem(ctx, householdID, itemID); err != nil {
		return nil, err
	}
	list, err := s.usage.ListPurchases(ctx, householdID, itemID, MaxPurchaseHistory)
	if err != nil {
		return nil, fmt.Errorf("list pantry purchases: %w", err)
	}
	return list, nil
}

// --- Alerts -------------------------------------------------------------------

// LowAlertDedupeKey is the notification dedupe key for an item's low alert in
// one cycle.
func LowAlertDedupeKey(itemID, cycleID string) string {
	return "pantry.low:" + itemID + ":" + cycleID
}

// checkLow marks item low and notifies the household when the estimate says
// so (needsAutoLow). The notification is created first, idempotently, so a
// failed status write is retried by the next check without a second alert.
func (s *Service) checkLow(ctx context.Context, item Item, threshold int) (Item, error) {
	for range maxWriteAttempts {
		now := s.timestamp()
		e := estimateItem(item, threshold, now)
		if !needsAutoLow(item, e) {
			return item, nil
		}
		if s.notifier != nil {
			_, _, err := s.notifier.Create(ctx, notifications.New{
				HouseholdID: item.HouseholdID, Type: notifications.TypePantryLow,
				Title:     item.DisplayName + " is running low",
				Body:      e.Summary,
				Subject:   notifications.Subject{Kind: notifications.SubjectPantryItem, ID: item.ID},
				DedupeKey: LowAlertDedupeKey(item.ID, e.CycleID),
			})
			if err != nil {
				return item, fmt.Errorf("create low-stock notification: %w", err)
			}
		}
		next := cloneItem(item)
		next.Status, next.StatusSource, next.StatusSetAt = StatusLow, StatusSourceEstimate, now
		next.LowAlertCycleID = e.CycleID
		saved, err := s.store.UpdateItem(ctx, next)
		switch {
		case errors.Is(err, ErrConflict):
			if item, err = s.store.GetItem(ctx, item.HouseholdID, item.ID); err != nil {
				return Item{}, err
			}
			continue
		case errors.Is(err, ErrNotFound):
			return item, nil
		case err != nil:
			return item, fmt.Errorf("mark pantry item low: %w", err)
		}
		s.logger.InfoContext(ctx, "pantry item estimated low",
			"householdId", saved.HouseholdID, "itemId", saved.ID, "cycleId", e.CycleID, "percentRemaining", e.PercentRemaining)
		return saved, nil
	}
	return item, ErrConflict
}

// checkAlerts runs checkLow for every item that needs it and returns the
// items with any changes applied. Failures are logged; the item is returned
// as it was.
func (s *Service) checkAlerts(ctx context.Context, householdID string, items []Item) []Item {
	var settings *Settings
	now := s.timestamp()
	for i, item := range items {
		if item.Tracking == nil {
			continue
		}
		if settings == nil {
			loaded, err := s.Settings(ctx, householdID)
			if err != nil {
				s.logger.WarnContext(ctx, "load pantry settings for alerts failed", "householdId", householdID, "error", err)
				return items
			}
			settings = &loaded
		}
		if !needsAutoLow(item, estimateItem(item, settings.LowThresholdPercent, now)) {
			continue
		}
		updated, err := s.checkLow(ctx, item, settings.LowThresholdPercent)
		if err != nil {
			s.logger.WarnContext(ctx, "pantry low-stock check failed", "householdId", householdID, "itemId", item.ID, "error", err)
			continue
		}
		items[i] = updated
	}
	return items
}

// checkItemAlert is checkAlerts for one item.
func (s *Service) checkItemAlert(ctx context.Context, item Item) Item {
	return s.checkAlerts(ctx, item.HouseholdID, []Item{item})[0]
}

// StillRelevant reports whether a low-stock alert should still be pushed: its
// item exists and is still low or out, so a restock between the alert and the
// push silences it. Other notification types are not the pantry's to judge.
// It implements push.Relevance.
func (s *Service) StillRelevant(ctx context.Context, n notifications.Notification) (bool, error) {
	if n.Type != notifications.TypePantryLow {
		return true, nil
	}
	item, err := s.store.GetItem(ctx, n.HouseholdID, n.Subject.ID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return item.Status == StatusLow || item.Status == StatusOut, nil
}

// Refresh runs the low-stock check for the whole household, so estimates that
// crossed the threshold as time passed alert before notifications are read.
// It implements notifications.Refresher.
func (s *Service) Refresh(ctx context.Context, householdID string) error {
	if householdID == "" {
		return errHouseholdRequired
	}
	items, err := s.store.ListItems(ctx, householdID, ListFilter{})
	if err != nil {
		return fmt.Errorf("list pantry items: %w", err)
	}
	s.checkAlerts(ctx, householdID, items)
	return nil
}
