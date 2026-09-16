package pantry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Usage collections this package owns.
const (
	PurchasesCollection = "pantry_purchases"
	CookUsageCollection = "pantry_cook_usage"
	SettingsCollection  = "pantry_settings"
)

var _ UsageStore = (*MongoStore)(nil)

func usageIndexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{
			Collection: PurchasesCollection,
			Indexes: []mongo.IndexModel{
				{
					// An item's purchase history, newest first.
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "itemId", Value: 1}, {Key: "purchasedAt", Value: -1}},
					Options: options.Index().SetName("householdId_itemId_purchasedAt"),
				},
				{
					// App retries of the same purchase are stored once.
					Keys: bson.D{{Key: "householdId", Value: 1}, {Key: "recordedBy", Value: 1}, {Key: "clientPurchaseId", Value: 1}},
					Options: options.Index().
						SetUnique(true).
						SetPartialFilterExpression(bson.D{{Key: "clientPurchaseId", Value: bson.D{{Key: "$type", Value: "string"}}}}).
						SetName("householdId_recordedBy_clientPurchaseId_unique"),
				},
				{
					// A shopping handoff line is recorded as bought at most once.
					Keys: bson.D{{Key: "householdId", Value: 1}, {Key: "provider.handoffId", Value: 1}, {Key: "provider.lineId", Value: 1}},
					Options: options.Index().
						SetUnique(true).
						SetPartialFilterExpression(bson.D{{Key: "provider.handoffId", Value: bson.D{{Key: "$type", Value: "string"}}}}).
						SetName("householdId_provider_handoffId_lineId_unique"),
				},
			},
		},
		{
			Collection: CookUsageCollection,
			Indexes: []mongo.IndexModel{{
				// One deduction per cooked meal.
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "sourceKey", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("householdId_sourceKey_unique"),
			}},
		},
		{
			Collection: SettingsCollection,
			Indexes: []mongo.IndexModel{{
				Keys:    bson.D{{Key: "householdId", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("householdId_unique"),
			}},
		},
	}
}

// --- Item usage fields ----------------------------------------------------------

type trackingDoc struct {
	CycleID           string    `bson:"cycleId"`
	CycleSource       string    `bson:"cycleSource"`
	CycleStartedAt    time.Time `bson:"cycleStartedAt"`
	Unit              string    `bson:"unit"`
	Reference         string    `bson:"reference"`
	SegmentStart      string    `bson:"segmentStart"`
	SegmentStartedAt  time.Time `bson:"segmentStartedAt"`
	SegmentRecipeUsed string    `bson:"segmentRecipeUsed"`
	RecipeUsed        string    `bson:"recipeUsed"`
	RecipeUses        int       `bson:"recipeUses"`
	SkippedUses       int       `bson:"skippedUses"`
}

type unitSizeDoc struct {
	Unit     string `bson:"unit"`
	Quantity string `bson:"quantity"`
	SizeUnit string `bson:"sizeUnit"`
}

type segmentDoc struct {
	StartedAt  time.Time `bson:"startedAt"`
	EndedAt    time.Time `bson:"endedAt"`
	Unit       string    `bson:"unit"`
	Start      string    `bson:"start"`
	RecipeUsed string    `bson:"recipeUsed"`
	Remaining  string    `bson:"remaining"`
	Observed   bool      `bson:"observed"`
	Carried    bool      `bson:"carried,omitempty"`
}

type rateDoc struct {
	PerDay     string    `bson:"perDay"`
	Unit       string    `bson:"unit"`
	Segments   int       `bson:"segments"`
	ComputedAt time.Time `bson:"computedAt"`
}

// usageItemFields are the usage fields of itemDoc.
type usageItemFields struct {
	StatusSource        string       `bson:"statusSource,omitempty"`
	StatusSetAt         *time.Time   `bson:"statusSetAt,omitempty"`
	Tracking            *trackingDoc `bson:"tracking,omitempty"`
	UnitSize            *unitSizeDoc `bson:"unitSize,omitempty"`
	History             []segmentDoc `bson:"history,omitempty"`
	Rate                *rateDoc     `bson:"rate,omitempty"`
	LowThresholdPercent int          `bson:"lowThresholdPercent,omitempty"`
	LowAlertCycleID     string       `bson:"lowAlertCycleId,omitempty"`
}

func newUsageItemFields(item Item) usageItemFields {
	f := usageItemFields{
		StatusSource: string(item.StatusSource), LowThresholdPercent: item.LowThresholdPercent, LowAlertCycleID: item.LowAlertCycleID,
	}
	if !item.StatusSetAt.IsZero() {
		at := item.StatusSetAt
		f.StatusSetAt = &at
	}
	if t := item.Tracking; t != nil {
		f.Tracking = &trackingDoc{
			CycleID: t.CycleID, CycleSource: string(t.CycleSource), CycleStartedAt: t.CycleStartedAt, Unit: t.Unit,
			Reference: t.Reference, SegmentStart: t.SegmentStart, SegmentStartedAt: t.SegmentStartedAt,
			SegmentRecipeUsed: t.SegmentRecipeUsed, RecipeUsed: t.RecipeUsed, RecipeUses: t.RecipeUses, SkippedUses: t.SkippedUses,
		}
	}
	if s := item.UnitSize; s != nil {
		f.UnitSize = &unitSizeDoc{Unit: s.Unit, Quantity: s.Quantity, SizeUnit: s.SizeUnit}
	}
	for _, seg := range item.History {
		f.History = append(f.History, segmentDoc(seg))
	}
	if r := item.Rate; r != nil {
		f.Rate = &rateDoc{PerDay: r.PerDay, Unit: r.Unit, Segments: r.Segments, ComputedAt: r.ComputedAt}
	}
	return f
}

func (f usageItemFields) applyTo(item *Item) {
	item.StatusSource, item.LowThresholdPercent, item.LowAlertCycleID = StatusSource(f.StatusSource), f.LowThresholdPercent, f.LowAlertCycleID
	if f.StatusSetAt != nil {
		item.StatusSetAt = f.StatusSetAt.UTC()
	}
	if t := f.Tracking; t != nil {
		item.Tracking = &Tracking{
			CycleID: t.CycleID, CycleSource: CycleSource(t.CycleSource), CycleStartedAt: t.CycleStartedAt.UTC(), Unit: t.Unit,
			Reference: t.Reference, SegmentStart: t.SegmentStart, SegmentStartedAt: t.SegmentStartedAt.UTC(),
			SegmentRecipeUsed: t.SegmentRecipeUsed, RecipeUsed: t.RecipeUsed, RecipeUses: t.RecipeUses, SkippedUses: t.SkippedUses,
		}
	}
	if s := f.UnitSize; s != nil {
		item.UnitSize = &UnitSize{Unit: s.Unit, Quantity: s.Quantity, SizeUnit: s.SizeUnit}
	}
	for _, seg := range f.History {
		seg.StartedAt, seg.EndedAt = seg.StartedAt.UTC(), seg.EndedAt.UTC()
		item.History = append(item.History, Segment(seg))
	}
	if r := f.Rate; r != nil {
		item.Rate = &Rate{PerDay: r.PerDay, Unit: r.Unit, Segments: r.Segments, ComputedAt: r.ComputedAt.UTC()}
	}
}

// usageSet adds the usage fields to an UpdateItem $set, or to $unset when
// absent.
func (f usageItemFields) usageSet(optional func(field string, value any, present bool)) {
	optional("statusSource", f.StatusSource, f.StatusSource != "")
	optional("statusSetAt", f.StatusSetAt, f.StatusSetAt != nil)
	optional("tracking", f.Tracking, f.Tracking != nil)
	optional("unitSize", f.UnitSize, f.UnitSize != nil)
	optional("history", f.History, len(f.History) > 0)
	optional("rate", f.Rate, f.Rate != nil)
	optional("lowThresholdPercent", f.LowThresholdPercent, f.LowThresholdPercent != 0)
	optional("lowAlertCycleId", f.LowAlertCycleID, f.LowAlertCycleID != "")
}

// --- Purchases ----------------------------------------------------------------

type purchaseDoc struct {
	ID               bson.ObjectID `bson:"_id"`
	HouseholdID      bson.ObjectID `bson:"householdId"`
	ItemID           bson.ObjectID `bson:"itemId"`
	ItemKey          string        `bson:"itemKey"`
	Source           string        `bson:"source"`
	Quantity         string        `bson:"quantity,omitempty"`
	QuantityValue    *float64      `bson:"quantityValue,omitempty"`
	Unit             string        `bson:"unit,omitempty"`
	UnitSize         *unitSizeDoc  `bson:"unitSize,omitempty"`
	Week             string        `bson:"week,omitempty"`
	ClientPurchaseID string        `bson:"clientPurchaseId,omitempty"`
	// Provider is a string handoff ID so the partial unique index covers
	// only provider purchases.
	Provider    *purchaseProviderDoc `bson:"provider,omitempty"`
	PriceCents  *int64               `bson:"priceCents,omitempty"`
	RecordedBy  bson.ObjectID        `bson:"recordedBy"`
	PurchasedAt time.Time            `bson:"purchasedAt"`
}

type purchaseProviderDoc struct {
	Key       string `bson:"key"`
	HandoffID string `bson:"handoffId"`
	LineID    string `bson:"lineId"`
	ProductID string `bson:"productId"`
}

func (d purchaseDoc) toPurchase() Purchase {
	p := Purchase{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), ItemID: d.ItemID.Hex(), ItemKey: d.ItemKey, Source: PurchaseSource(d.Source),
		Quantity: d.Quantity, Unit: d.Unit, Week: d.Week, ClientPurchaseID: d.ClientPurchaseID,
		RecordedBy: d.RecordedBy.Hex(), PurchasedAt: d.PurchasedAt.UTC(), PriceCents: d.PriceCents,
	}
	if s := d.UnitSize; s != nil {
		p.UnitSize = &UnitSize{Unit: s.Unit, Quantity: s.Quantity, SizeUnit: s.SizeUnit}
	}
	if pr := d.Provider; pr != nil {
		p.Provider = &ProviderRef{Key: pr.Key, HandoffID: pr.HandoffID, LineID: pr.LineID, ProductID: pr.ProductID}
	}
	return p
}

// InsertPurchase implements UsageStore.
func (s *MongoStore) InsertPurchase(ctx context.Context, p Purchase) (Purchase, error) {
	ids, err := parseRequiredIDs(map[string]string{"id": p.ID, "householdId": p.HouseholdID, "itemId": p.ItemID, "recordedBy": p.RecordedBy})
	if err != nil {
		return Purchase{}, err
	}
	d := purchaseDoc{
		ID: ids["id"], HouseholdID: ids["householdId"], ItemID: ids["itemId"], ItemKey: p.ItemKey, Source: string(p.Source),
		Quantity: p.Quantity, Unit: p.Unit, Week: p.Week, ClientPurchaseID: p.ClientPurchaseID,
		RecordedBy: ids["recordedBy"], PurchasedAt: p.PurchasedAt, PriceCents: p.PriceCents,
	}
	if p.Quantity != "" {
		q, err := ingredients.ParseQuantity(p.Quantity)
		if err != nil {
			return Purchase{}, fmt.Errorf("pantry: purchase quantity: %w", err)
		}
		v := q.Float64()
		d.QuantityValue = &v
	}
	if size := p.UnitSize; size != nil {
		d.UnitSize = &unitSizeDoc{Unit: size.Unit, Quantity: size.Quantity, SizeUnit: size.SizeUnit}
	}
	if pr := p.Provider; pr != nil {
		d.Provider = &purchaseProviderDoc{Key: pr.Key, HandoffID: pr.HandoffID, LineID: pr.LineID, ProductID: pr.ProductID}
	}
	if _, err := s.purchases.InsertOne(ctx, d); err != nil {
		return Purchase{}, translate(err)
	}
	return d.toPurchase(), nil
}

// FindPurchaseByProviderLine implements UsageStore.
func (s *MongoStore) FindPurchaseByProviderLine(ctx context.Context, householdID, handoffID, lineID string) (Purchase, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil || handoffID == "" || lineID == "" {
		return Purchase{}, ErrNotFound
	}
	var d purchaseDoc
	err = s.purchases.FindOne(ctx, bson.D{
		{Key: "householdId", Value: hid}, {Key: "provider.handoffId", Value: handoffID}, {Key: "provider.lineId", Value: lineID},
	}).Decode(&d)
	if err != nil {
		return Purchase{}, translate(err)
	}
	return d.toPurchase(), nil
}

// FindPurchaseByClientID implements UsageStore.
func (s *MongoStore) FindPurchaseByClientID(ctx context.Context, householdID, userID, clientPurchaseID string) (Purchase, error) {
	hid, err1 := mongodb.ParseID(householdID)
	uid, err2 := mongodb.ParseID(userID)
	if err1 != nil || err2 != nil || clientPurchaseID == "" {
		return Purchase{}, ErrNotFound
	}
	var d purchaseDoc
	err := s.purchases.FindOne(ctx, bson.D{
		{Key: "householdId", Value: hid}, {Key: "recordedBy", Value: uid}, {Key: "clientPurchaseId", Value: clientPurchaseID},
	}).Decode(&d)
	if err != nil {
		return Purchase{}, translate(err)
	}
	return d.toPurchase(), nil
}

// ListPurchases implements UsageStore.
func (s *MongoStore) ListPurchases(ctx context.Context, householdID, itemID string, limit int) ([]Purchase, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	iid, err := mongodb.ParseID(itemID)
	if err != nil {
		return nil, nil
	}
	cur, err := s.purchases.Find(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "itemId", Value: iid}},
		options.Find().SetSort(bson.D{{Key: "purchasedAt", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, translate(err)
	}
	var docs []purchaseDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Purchase, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toPurchase())
	}
	return out, nil
}

// --- Cook usage ---------------------------------------------------------------

type cookLineDoc struct {
	ItemID       bson.ObjectID `bson:"itemId"`
	Ingredient   string        `bson:"ingredient"`
	Quantity     string        `bson:"quantity,omitempty"`
	Unit         string        `bson:"unit,omitempty"`
	Deducted     string        `bson:"deducted,omitempty"`
	TrackingUnit string        `bson:"trackingUnit,omitempty"`
	CycleID      string        `bson:"cycleId,omitempty"`
	Estimated    bool          `bson:"estimated,omitempty"`
	SkipReason   string        `bson:"skipReason,omitempty"`
}

type cookUsageDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	SourceKey   string        `bson:"sourceKey"`
	RecipeID    bson.ObjectID `bson:"recipeId"`
	EntryID     string        `bson:"entryId,omitempty"`
	UserID      bson.ObjectID `bson:"userId,omitempty"`
	Servings    int           `bson:"servings"`
	ScaledFrom  int           `bson:"scaledFrom,omitempty"`
	OccurredAt  time.Time     `bson:"occurredAt"`
	CreatedAt   time.Time     `bson:"createdAt"`
	Lines       []cookLineDoc `bson:"lines"`
}

// InsertCookUsage implements UsageStore.
func (s *MongoStore) InsertCookUsage(ctx context.Context, u CookUsage) (CookUsage, error) {
	ids, err := parseRequiredIDs(map[string]string{"householdId": u.HouseholdID, "recipeId": u.RecipeID})
	if err != nil {
		return CookUsage{}, err
	}
	d := cookUsageDoc{
		ID: bson.NewObjectID(), HouseholdID: ids["householdId"], SourceKey: u.SourceKey, RecipeID: ids["recipeId"],
		EntryID: u.EntryID, Servings: u.Servings, ScaledFrom: u.ScaledFrom, OccurredAt: u.OccurredAt, CreatedAt: u.CreatedAt,
		Lines: make([]cookLineDoc, 0, len(u.Lines)),
	}
	if u.UserID != "" {
		if d.UserID, err = mongodb.ParseID(u.UserID); err != nil {
			return CookUsage{}, fmt.Errorf("pantry: userId: %w", err)
		}
	}
	for _, line := range u.Lines {
		itemID, err := mongodb.ParseID(line.ItemID)
		if err != nil {
			return CookUsage{}, fmt.Errorf("pantry: line itemId: %w", err)
		}
		d.Lines = append(d.Lines, cookLineDoc{
			ItemID: itemID, Ingredient: line.Ingredient, Quantity: line.Quantity, Unit: line.Unit,
			Deducted: line.Deducted, TrackingUnit: line.TrackingUnit, CycleID: line.CycleID, Estimated: line.Estimated,
			SkipReason: string(line.SkipReason),
		})
	}
	if _, err := s.cookUsage.InsertOne(ctx, d); err != nil {
		return CookUsage{}, translate(err)
	}
	u.ID = d.ID.Hex()
	return u, nil
}

func (d cookUsageDoc) toCookUsage() CookUsage {
	u := CookUsage{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), SourceKey: d.SourceKey, RecipeID: d.RecipeID.Hex(), EntryID: d.EntryID,
		UserID: hexOrEmpty(d.UserID), Servings: d.Servings, ScaledFrom: d.ScaledFrom, OccurredAt: d.OccurredAt.UTC(), CreatedAt: d.CreatedAt.UTC(),
	}
	for _, l := range d.Lines {
		u.Lines = append(u.Lines, CookLine{
			ItemID: l.ItemID.Hex(), Ingredient: l.Ingredient, Quantity: l.Quantity, Unit: l.Unit, Deducted: l.Deducted,
			TrackingUnit: l.TrackingUnit, CycleID: l.CycleID, Estimated: l.Estimated, SkipReason: CookSkipReason(l.SkipReason),
		})
	}
	return u
}

func hexOrEmpty(id bson.ObjectID) string {
	if id.IsZero() {
		return ""
	}
	return id.Hex()
}

// ListCookUsageByEntries implements UsageStore. Planned meals are keyed
// "entry:<entryId>", which the unique sourceKey index serves.
func (s *MongoStore) ListCookUsageByEntries(ctx context.Context, householdID string, entryIDs []string) ([]CookUsage, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entryIDs))
	for _, id := range entryIDs {
		keys = append(keys, "entry:"+id)
	}
	cur, err := s.cookUsage.Find(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "sourceKey", Value: bson.D{{Key: "$in", Value: keys}}}},
		options.Find().SetSort(bson.D{{Key: "occurredAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []cookUsageDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]CookUsage, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toCookUsage())
	}
	return out, nil
}

// SetPurchasePrice implements UsageStore.
func (s *MongoStore) SetPurchasePrice(ctx context.Context, householdID, purchaseID string, priceCents *int64) (Purchase, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return Purchase{}, err
	}
	pid, err := mongodb.ParseID(purchaseID)
	if err != nil {
		return Purchase{}, ErrNotFound
	}
	update := bson.D{{Key: "$unset", Value: bson.D{{Key: "priceCents", Value: ""}}}}
	if priceCents != nil {
		update = bson.D{{Key: "$set", Value: bson.D{{Key: "priceCents", Value: *priceCents}}}}
	}
	var d purchaseDoc
	err = s.purchases.FindOneAndUpdate(ctx, bson.D{{Key: "_id", Value: pid}, {Key: "householdId", Value: hid}}, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&d)
	if err != nil {
		return Purchase{}, translate(err)
	}
	return d.toPurchase(), nil
}

// PurchasesByIDs implements UsageStore.
func (s *MongoStore) PurchasesByIDs(ctx context.Context, householdID string, ids []string) ([]Purchase, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	oids := make([]bson.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, err := mongodb.ParseID(id); err == nil {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return nil, nil
	}
	cur, err := s.purchases.Find(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}})
	if err != nil {
		return nil, translate(err)
	}
	var docs []purchaseDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Purchase, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toPurchase())
	}
	return out, nil
}

// --- Settings -----------------------------------------------------------------

type settingsDoc struct {
	HouseholdID         bson.ObjectID `bson:"householdId"`
	LowThresholdPercent int           `bson:"lowThresholdPercent"`
	UpdatedBy           bson.ObjectID `bson:"updatedBy"`
	UpdatedAt           time.Time     `bson:"updatedAt"`
}

func (d settingsDoc) toSettings() Settings {
	return Settings{
		HouseholdID: d.HouseholdID.Hex(), LowThresholdPercent: d.LowThresholdPercent,
		UpdatedBy: d.UpdatedBy.Hex(), UpdatedAt: d.UpdatedAt.UTC(),
	}
}

// GetSettings implements UsageStore.
func (s *MongoStore) GetSettings(ctx context.Context, householdID string) (Settings, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Settings{}, ErrNotFound
	}
	var d settingsDoc
	if err := s.settings.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}}).Decode(&d); err != nil {
		return Settings{}, translate(err)
	}
	return d.toSettings(), nil
}

// PutSettings implements UsageStore with an upsert on the household.
func (s *MongoStore) PutSettings(ctx context.Context, settings Settings) (Settings, error) {
	ids, err := parseRequiredIDs(map[string]string{"householdId": settings.HouseholdID, "updatedBy": settings.UpdatedBy})
	if err != nil {
		return Settings{}, err
	}
	var d settingsDoc
	err = s.settings.FindOneAndUpdate(ctx,
		bson.D{{Key: "householdId", Value: ids["householdId"]}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "lowThresholdPercent", Value: settings.LowThresholdPercent},
			{Key: "updatedBy", Value: ids["updatedBy"]},
			{Key: "updatedAt", Value: settings.UpdatedAt},
		}}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&d)
	if errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		// Two first writes raced on the unique index; the retry updates.
		return s.PutSettings(ctx, settings)
	}
	if err != nil {
		return Settings{}, translate(err)
	}
	return d.toSettings(), nil
}

func parseRequiredIDs(fields map[string]string) (map[string]bson.ObjectID, error) {
	out := make(map[string]bson.ObjectID, len(fields))
	for name, value := range fields {
		oid, err := mongodb.ParseID(value)
		if err != nil {
			return nil, fmt.Errorf("pantry: %s: %w", name, err)
		}
		out[name] = oid
	}
	return out, nil
}
