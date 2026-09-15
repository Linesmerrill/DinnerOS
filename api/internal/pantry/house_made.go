package pantry

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file holds what specialty ingredients (internal/substitutes) need from
// the pantry: recording a house-made batch as a purchase, reading batch stock
// for grocery lists, and matching recipe names to a batch when cooking.
// docs/specialty-ingredients.md explains the model.

// HouseMadeInput is the input for Service.RecordHouseMade.
type HouseMadeInput struct {
	// Key is the pantry key of the batch: the specialty ingredient's
	// normalized name ("southwest spice blend"), so recipe lines that name it
	// match the item.
	Key string
	// DisplayName names the item when it's new ("Southwest Spice Blend
	// (house-made)"). An existing item keeps its name.
	DisplayName string
	// Category defaults to the catalog's, or ingredients.Categorize.
	Category string
	// Quantity and Unit are what the batch made; the cycle starts there.
	Quantity string
	Unit     string
	// UnitSize, when set, says how much one discrete unit of the specialty
	// holds ("1 count = 1 tbsp"), so recipe lines that count packets deduct.
	// It replaces the item's size only when the item has none or one for the
	// same unit.
	UnitSize *UnitSize
	// ShelfLifeDays, when positive, sets ExpiresOn to the UTC date it was made
	// plus that many days.
	ShelfLifeDays    int
	ClientPurchaseID string
}

// MaxShelfLifeDays bounds HouseMadeInput.ShelfLifeDays.
const MaxShelfLifeDays = 730

// RecordHouseMade records that the household made a batch of a specialty
// ingredient: a purchase with source house_made. The batch item (keyed by
// the specialty ingredient, added when missing) is in stock with the batch's
// amount, and a new usage cycle starts from it, so cooked recipes deduct from
// it and the low-stock alert works as for anything bought. Retrying with the
// same ClientPurchaseID returns the first record and changes nothing.
func (s *Service) RecordHouseMade(ctx context.Context, actor households.Membership, in HouseMadeInput) (PurchaseResult, error) {
	if err := authorizeEdit(actor); err != nil {
		return PurchaseResult{}, err
	}
	if s.usage == nil {
		return PurchaseResult{}, errUsageNotConfigured
	}
	key := ingredients.NormalizeName(in.Key)
	if key == "" || key != in.Key {
		return PurchaseResult{}, invalid("key must be a normalized ingredient name")
	}
	p := purchase{source: PurchaseHouseMade, clientID: in.ClientPurchaseID}
	var err error
	if p.quantity, p.unit, err = normalizeAmount(in.Quantity, in.Unit); err != nil {
		return PurchaseResult{}, err
	}
	switch {
	case p.quantity == "":
		return PurchaseResult{}, invalid("a batch needs a quantity")
	case in.ShelfLifeDays < 0 || in.ShelfLifeDays > MaxShelfLifeDays:
		return PurchaseResult{}, invalid("shelfLifeDays must be between 0 and %d", MaxShelfLifeDays)
	case len(p.clientID) > MaxClientPurchaseIDLength:
		return PurchaseResult{}, invalid("clientPurchaseId must be at most %d characters", MaxClientPurchaseIDLength)
	case strings.TrimSpace(p.clientID) != p.clientID:
		return PurchaseResult{}, invalid("clientPurchaseId must not have surrounding whitespace")
	}
	size, err := validHouseMadeSize(in.UnitSize)
	if err != nil {
		return PurchaseResult{}, err
	}
	if p.clientID != "" {
		if res, found, err := s.existingPurchase(ctx, actor, p.clientID); found || err != nil {
			return res, err
		}
	}

	a := addition{key: key, status: StatusInStock}
	if a.displayName, err = normalizeDisplayName(in.DisplayName); err != nil {
		return PurchaseResult{}, err
	}
	found, err := s.catalog.IngredientsByKey(ctx, []string{key})
	if err != nil {
		return PurchaseResult{}, fmt.Errorf("look up catalog ingredient: %w", err)
	}
	if len(found) > 0 {
		a.ingredientID, a.category = found[0].ID, catalogCategory(found[0].Category)
	} else {
		a.category, _ = ingredients.Categorize(key)
	}
	if strings.TrimSpace(in.Category) != "" {
		if a.category, err = normalizeCategory(in.Category); err != nil {
			return PurchaseResult{}, err
		}
	}

	return s.recordPurchase(ctx, actor, p, "", a, func(item *Item, now time.Time) {
		if item.IngredientID == "" {
			item.IngredientID = a.ingredientID
		}
		if size != nil && (item.UnitSize == nil || item.UnitSize.Unit == size.Unit) {
			item.UnitSize = size
		}
		if in.ShelfLifeDays > 0 {
			item.ExpiresOn = now.AddDate(0, 0, in.ShelfLifeDays).Format(DateLayout)
		}
	})
}

func validHouseMadeSize(size *UnitSize) (*UnitSize, error) {
	if size == nil {
		return nil, nil
	}
	if u, err := ingredients.LookupUnit(size.Unit); err != nil || !u.Discrete() {
		return nil, invalid("unitSize.per must be a unit such as count, package, or can")
	}
	q, unit, err := normalizeAmount(size.Quantity, size.SizeUnit)
	if err != nil || q == "" {
		return nil, invalid("unitSize needs a positive quantity and a unit")
	}
	if u, _ := ingredients.LookupUnit(unit); u.Discrete() {
		return nil, invalid("unitSize.unit must be a volume or weight unit")
	}
	return &UnitSize{Unit: size.Unit, Quantity: q, SizeUnit: unit}, nil
}

// StockLevel is a pantry item's status and, when tracked, its estimated
// remaining amount.
type StockLevel struct {
	ItemID string
	Key    string
	Status Status
	// Remaining (rounded to hundredths) and Unit are the usage estimate's;
	// Remaining is nil when the item isn't tracked.
	Remaining        *big.Rat
	Unit             string
	PercentRemaining int
	UnitSize         *UnitSize
	ExpiresOn        string
}

// StockLevels returns the stock of the household's items with the given
// keys, by key. Keys the pantry doesn't have are absent. It doesn't run the
// low-stock check, so an item that crossed its threshold since the last
// pantry read still reports in_stock with its low estimate.
func (s *Service) StockLevels(ctx context.Context, householdID string, keys []string) (map[string]StockLevel, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	out := map[string]StockLevel{}
	if len(keys) == 0 {
		return out, nil
	}
	items, err := s.store.FindItemsByKeys(ctx, householdID, keys)
	if err != nil {
		return nil, fmt.Errorf("find pantry items: %w", err)
	}
	var settings *Settings
	for _, item := range items {
		level := StockLevel{ItemID: item.ID, Key: item.Key, Status: item.Status, UnitSize: item.UnitSize, ExpiresOn: item.ExpiresOn}
		if item.Tracking != nil {
			if settings == nil {
				loaded, err := s.Settings(ctx, householdID)
				if err != nil {
					return nil, err
				}
				settings = &loaded
			}
			if e := s.Estimate(item, *settings); e != nil {
				level.Remaining, level.Unit, level.PercentRemaining = e.Remaining, e.Unit, e.PercentRemaining
			}
		}
		out[item.Key] = level
	}
	return out, nil
}

// ResolvedKey is the pantry key that stands in for an ingredient key, with
// unit sizes that convert its discrete units ("1 count = 2 tsp").
type ResolvedKey struct {
	Key       string
	UnitSizes []UnitSize
}

// KeyResolver maps the keys cooked recipe lines carry (a catalog key or a
// normalized name) to the pantry key that stands in for them. The specialty
// ingredient catalog implements it, so a recipe naming "Sichuan Paste"
// deducts from the "szechuan paste" batch, and a line that counts packets
// converts with the specialty's packet size. Keys it doesn't know are absent.
type KeyResolver interface {
	ResolveKeys(ctx context.Context, keys []string) (map[string]ResolvedKey, error)
}

// SetKeyResolver makes cook deductions match through r, and returns s.
func (s *Service) SetKeyResolver(r KeyResolver) *Service {
	s.resolver = r
	return s
}

// resolveNeedKeys resolves every key the needs carry. A resolver failure is
// logged, and deduction continues with direct matches only.
func (s *Service) resolveNeedKeys(ctx context.Context, householdID string, needs []recipeNeed, catalogKeys map[string]string) map[string]ResolvedKey {
	if s.resolver == nil {
		return nil
	}
	var keys []string
	add := func(k string) {
		if k != "" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	for _, n := range needs {
		add(catalogKeys[n.ingredientID])
		add(ingredients.NormalizeName(n.name))
	}
	resolved, err := s.resolver.ResolveKeys(ctx, keys)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve specialty ingredient keys failed; deducting direct matches only", "householdId", householdID, "error", err)
		return nil
	}
	return resolved
}
