package pantry

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// maxWriteAttempts bounds retries when a concurrent write wins a race. An
// attempt only fails because another write to the same item committed, so
// this many members can add the same ingredient at once and all succeed.
const maxWriteAttempts = 5

// Catalog resolves global ingredient catalog entries. *recipes.Service
// implements it.
type Catalog interface {
	IngredientsByID(ctx context.Context, ids []string) ([]recipes.Ingredient, error)
	IngredientsByKey(ctx context.Context, keys []string) ([]recipes.Ingredient, error)
}

// Service implements the pantry use cases.
//
// Reads take a household ID; the HTTP routes authorize them with
// household.view. Writes take the actor's membership and re-check
// pantry.edit, so authorization holds for non-HTTP callers too.
type Service struct {
	store   Store
	catalog Catalog
	// usage, recipes, and notifier are set by WithUsage.
	usage    UsageStore
	recipes  RecipeReader
	notifier Notifier
	logger   *slog.Logger
	now      func() time.Time
	// newID generates purchase and cycle IDs.
	newID func() string
}

// NewService returns a Service backed by store and catalog. Without WithUsage,
// estimates use the default threshold and nothing is purchased, deducted, or
// alerted.
func NewService(store Store, catalog Catalog) *Service {
	return &Service{
		store: store, catalog: catalog, logger: slog.New(slog.DiscardHandler), now: time.Now,
		newID: func() string { return bson.NewObjectID().Hex() },
	}
}

// timestamp returns the current time at the precision MongoDB stores, so
// returned items equal what a later read returns.
func (s *Service) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Millisecond)
}

func authorizeEdit(actor households.Membership) error {
	if actor.HouseholdID == "" {
		return errHouseholdRequired
	}
	if !actor.Role.Can(households.PermPantryEdit) {
		return ErrForbidden
	}
	return nil
}

// List returns the household's items matching q, in aisle order
// (grocery.CategoryOrder), then by name. It first runs the low-stock check, so
// an item whose estimate crossed the threshold as time passed comes back low
// (docs/pantry-usage.md).
func (s *Service) List(ctx context.Context, householdID string, q ListQuery) ([]Item, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	f, err := q.filter()
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListItems(ctx, householdID, f)
	if err != nil {
		return nil, fmt.Errorf("list pantry items: %w", err)
	}
	items = s.checkAlerts(ctx, householdID, items)
	sortItems(items)
	return items, nil
}

func sortItems(items []Item) {
	slices.SortFunc(items, func(a, b Item) int {
		if c := cmp.Compare(categoryRank(a.Category), categoryRank(b.Category)); c != 0 {
			return c
		}
		if c := cmp.Compare(strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

func categoryRank(c string) int {
	if i := slices.Index(grocery.CategoryOrder, c); i >= 0 {
		return i
	}
	return len(grocery.CategoryOrder)
}

// addition is a validated AddInput.
type addition struct {
	ingredientID  string
	key           string
	displayName   string
	category      string
	categoryGiven bool
	quantity      string
	unit          string
	status        Status
	isStaple      *bool
	expiresOn     string
	note          string
}

// Add adds an ingredient to the pantry, or merges into the item that already
// has its key. created reports which happened.
//
// Merging sets the status (in_stock unless given) and every field the input
// provides; fields it omits keep their stored values, and the display name is
// kept. Retrying the same Add is therefore harmless.
func (s *Service) Add(ctx context.Context, actor households.Membership, in AddInput) (item Item, created bool, err error) {
	if err := authorizeEdit(actor); err != nil {
		return Item{}, false, err
	}
	a, err := s.validateAddition(ctx, in)
	if err != nil {
		return Item{}, false, err
	}
	hh := actor.HouseholdID
	for range maxWriteAttempts {
		existing, err := s.store.FindItemsByKeys(ctx, hh, []string{a.key})
		if err != nil {
			return Item{}, false, fmt.Errorf("find pantry item: %w", err)
		}
		now := s.timestamp()
		if len(existing) == 0 {
			if err := s.checkCapacity(ctx, hh, 1); err != nil {
				return Item{}, false, err
			}
			item := newItem(hh, a, actor.UserID, now)
			applyPersonEdit(Item{}, &item, now, s.newID)
			saved, err := s.store.InsertItem(ctx, item)
			if errors.Is(err, ErrDuplicate) {
				continue // someone added it concurrently; merge into theirs
			}
			if err != nil {
				return Item{}, false, fmt.Errorf("insert pantry item: %w", err)
			}
			return saved, true, nil
		}
		merged := mergeAddition(cloneItem(existing[0]), a)
		applyPersonEdit(existing[0], &merged, now, s.newID)
		merged.UpdatedBy, merged.UpdatedAt = actor.UserID, now
		saved, err := s.store.UpdateItem(ctx, merged)
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
			continue // changed or deleted concurrently; start over
		}
		if err != nil {
			return Item{}, false, fmt.Errorf("update pantry item: %w", err)
		}
		return saved, false, nil
	}
	return Item{}, false, ErrConflict
}

func (s *Service) validateAddition(ctx context.Context, in AddInput) (addition, error) {
	var a addition
	name := strings.TrimSpace(in.Name)
	switch id := strings.TrimSpace(in.IngredientID); {
	case id != "":
		found, err := s.catalog.IngredientsByID(ctx, []string{id})
		if err != nil {
			return addition{}, fmt.Errorf("look up catalog ingredient: %w", err)
		}
		if len(found) == 0 {
			return addition{}, invalid("ingredientId does not match a catalog ingredient")
		}
		ing := found[0]
		a.ingredientID, a.key, a.category = ing.ID, ing.Key, catalogCategory(ing.Category)
		if name == "" {
			name = ing.Name
		}
	case name == "":
		return addition{}, invalid("name or ingredientId is required")
	default:
		a.key = ingredients.NormalizeName(name)
		if a.key == "" {
			return addition{}, invalid("name must contain letters or digits")
		}
		// Link free text to the catalog when the name is a catalog ingredient,
		// so grocery lists keyed by catalog ID find it.
		found, err := s.catalog.IngredientsByKey(ctx, []string{a.key})
		if err != nil {
			return addition{}, fmt.Errorf("look up catalog ingredient: %w", err)
		}
		if len(found) > 0 {
			a.ingredientID, a.category = found[0].ID, catalogCategory(found[0].Category)
		} else {
			a.category, _ = ingredients.Categorize(name)
		}
	}

	var err error
	if a.displayName, err = normalizeDisplayName(name); err != nil {
		return addition{}, err
	}
	if strings.TrimSpace(in.Category) != "" {
		if a.category, err = normalizeCategory(in.Category); err != nil {
			return addition{}, err
		}
		a.categoryGiven = true
	}
	a.status = in.Status
	if a.status == "" {
		a.status = StatusInStock
	}
	if !a.status.Valid() {
		return addition{}, invalid("status must be in_stock, low, or out")
	}
	if a.quantity, a.unit, err = normalizeAmount(in.Quantity, in.Unit); err != nil {
		return addition{}, err
	}
	if a.status == StatusOut && a.quantity != "" {
		return addition{}, invalid("an item that is out can't have a quantity")
	}
	if a.expiresOn, err = normalizeDate(in.ExpiresOn); err != nil {
		return addition{}, err
	}
	if a.note, err = normalizeNote(in.Note); err != nil {
		return addition{}, err
	}
	a.isStaple = in.IsStaple
	return a, nil
}

func newItem(householdID string, a addition, userID string, now time.Time) Item {
	item := Item{
		HouseholdID: householdID, IngredientID: a.ingredientID, Key: a.key,
		DisplayName: a.displayName, Category: a.category,
		Quantity: a.quantity, Unit: a.unit, Status: a.status,
		ExpiresOn: a.expiresOn, Note: a.note,
		CreatedAt: now, UpdatedBy: userID, UpdatedAt: now,
	}
	if a.isStaple != nil {
		item.IsStaple = *a.isStaple
	}
	return item
}

func mergeAddition(item Item, a addition) Item {
	item.Status = a.status
	if item.IngredientID == "" {
		item.IngredientID = a.ingredientID
	}
	if a.categoryGiven {
		item.Category = a.category
	}
	if a.isStaple != nil {
		item.IsStaple = *a.isStaple
	}
	if a.quantity != "" {
		item.Quantity, item.Unit = a.quantity, a.unit
	}
	if a.status == StatusOut {
		item.Quantity, item.Unit = "", ""
	}
	if a.expiresOn != "" {
		item.ExpiresOn = a.expiresOn
	}
	if a.note != "" {
		item.Note = a.note
	}
	return item
}

func (s *Service) checkCapacity(ctx context.Context, householdID string, adding int) error {
	n, err := s.store.CountItems(ctx, householdID)
	if err != nil {
		return fmt.Errorf("count pantry items: %w", err)
	}
	if n+adding > MaxItems {
		return invalid("a pantry holds at most %d items", MaxItems)
	}
	return nil
}

// Update applies a partial update to one item. Marking an item out clears its
// quantity and unit. A new amount becomes the usage estimate's starting point,
// and a status sent by a person replaces one the estimate set. The low-stock
// check then runs for the item.
func (s *Service) Update(ctx context.Context, actor households.Membership, id string, in UpdateInput) (Item, error) {
	if err := authorizeEdit(actor); err != nil {
		return Item{}, err
	}
	for range maxWriteAttempts {
		item, err := s.store.GetItem(ctx, actor.HouseholdID, id)
		if err != nil {
			return Item{}, err
		}
		next, err := applyUpdate(cloneItem(item), in)
		if err != nil {
			return Item{}, err
		}
		now := s.timestamp()
		applyPersonEdit(item, &next, now, s.newID)
		if in.Status != nil && next.StatusSource == StatusSourceEstimate {
			next.StatusSource, next.StatusSetAt = StatusSourcePerson, now
		}
		next.UpdatedBy, next.UpdatedAt = actor.UserID, now
		saved, err := s.store.UpdateItem(ctx, next)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return Item{}, err
		}
		return s.checkItemAlert(ctx, saved), nil
	}
	return Item{}, ErrConflict
}

func applyUpdate(item Item, in UpdateInput) (Item, error) {
	var err error
	if in.DisplayName != nil {
		if item.DisplayName, err = normalizeDisplayName(*in.DisplayName); err != nil {
			return Item{}, err
		}
	}
	if in.Category != nil {
		if item.Category, err = normalizeCategory(*in.Category); err != nil {
			return Item{}, err
		}
	}
	if in.Status != nil {
		if !in.Status.Valid() {
			return Item{}, invalid("status must be in_stock, low, or out")
		}
		item.Status = *in.Status
	}
	if in.IsStaple != nil {
		item.IsStaple = *in.IsStaple
	}
	if in.LowThresholdPercent != nil {
		v := *in.LowThresholdPercent
		if v != 0 && (v < 1 || v > 100) {
			return Item{}, invalid("lowThresholdPercent must be between 1 and 100")
		}
		item.LowThresholdPercent = v
	}
	if in.ExpiresOn != nil {
		if item.ExpiresOn, err = normalizeDate(*in.ExpiresOn); err != nil {
			return Item{}, err
		}
	}
	if in.Note != nil {
		if item.Note, err = normalizeNote(*in.Note); err != nil {
			return Item{}, err
		}
	}

	quantity, unit := item.Quantity, item.Unit
	if in.Quantity != nil {
		quantity = *in.Quantity
		if strings.TrimSpace(quantity) == "" {
			unit = ""
		}
	}
	if in.Unit != nil {
		unit = *in.Unit
	}
	if item.Status == StatusOut {
		if (in.Quantity != nil && strings.TrimSpace(*in.Quantity) != "") || (in.Unit != nil && strings.TrimSpace(*in.Unit) != "") {
			return Item{}, invalid("an item that is out can't have a quantity")
		}
		quantity, unit = "", ""
	}
	if item.Quantity, item.Unit, err = normalizeAmount(quantity, unit); err != nil {
		return Item{}, err
	}
	return item, nil
}

// Delete removes one item.
func (s *Service) Delete(ctx context.Context, actor households.Membership, id string) error {
	if err := authorizeEdit(actor); err != nil {
		return err
	}
	return s.store.DeleteItem(ctx, actor.HouseholdID, id)
}

// SetStatuses sets several items' statuses at once, for example after a
// shopping trip. IDs that aren't in the household's pantry (another member
// deleted them) are reported in Missing rather than failing the batch.
func (s *Service) SetStatuses(ctx context.Context, actor households.Membership, updates []StatusUpdate) (BulkResult, error) {
	if err := authorizeEdit(actor); err != nil {
		return BulkResult{}, err
	}
	switch {
	case len(updates) == 0:
		return BulkResult{}, invalid("items must not be empty")
	case len(updates) > MaxBulkUpdates:
		return BulkResult{}, invalid("items must have at most %d entries", MaxBulkUpdates)
	}
	ids := make([]string, 0, len(updates))
	byStatus := map[Status][]string{}
	for i, u := range updates {
		id := strings.TrimSpace(u.ItemID)
		switch {
		case id == "":
			return BulkResult{}, invalid("items[%d].id is required", i)
		case slices.Contains(ids, id):
			return BulkResult{}, invalid("items[%d].id repeats an earlier item", i)
		case !u.Status.Valid():
			return BulkResult{}, invalid("items[%d].status must be in_stock, low, or out", i)
		}
		ids = append(ids, id)
		byStatus[u.Status] = append(byStatus[u.Status], id)
	}

	now := s.timestamp()
	for _, status := range []Status{StatusInStock, StatusLow, StatusOut} {
		if len(byStatus[status]) == 0 {
			continue
		}
		if err := s.store.SetStatus(ctx, actor.HouseholdID, byStatus[status], status, actor.UserID, now); err != nil {
			return BulkResult{}, fmt.Errorf("set pantry statuses: %w", err)
		}
	}
	found, err := s.store.GetItems(ctx, actor.HouseholdID, ids)
	if err != nil {
		return BulkResult{}, fmt.Errorf("get pantry items: %w", err)
	}
	byID := make(map[string]Item, len(found))
	for _, item := range found {
		byID[item.ID] = item
	}
	res := BulkResult{Items: make([]Item, 0, len(found))}
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			res.Items = append(res.Items, item)
		} else {
			res.Missing = append(res.Missing, id)
		}
	}
	return res, nil
}
