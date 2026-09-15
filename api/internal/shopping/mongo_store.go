package shopping

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// Collections this package owns.
const (
	SettingsCollection    = "shopping_settings"
	PreferencesCollection = "shopping_product_preferences"
	HandoffsCollection    = "shopping_handoffs"
)

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{
			Collection: SettingsCollection,
			Indexes: []mongo.IndexModel{{
				Keys:    bson.D{{Key: "householdId", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("householdId_unique"),
			}},
		},
		{
			Collection: PreferencesCollection,
			Indexes: []mongo.IndexModel{{
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "provider", Value: 1}, {Key: "ingredientKey", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("householdId_provider_ingredientKey_unique"),
			}},
		},
		{
			Collection: HandoffsCollection,
			Indexes: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "createdAt", Value: -1}},
					Options: options.Index().SetName("householdId_createdAt"),
				},
				{
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "week", Value: 1}, {Key: "createdAt", Value: -1}},
					Options: options.Index().SetName("householdId_week_createdAt"),
				},
			},
		},
	}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	settings    *mongo.Collection
	preferences *mongo.Collection
	handoffs    *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{
		settings:    db.Collection(SettingsCollection),
		preferences: db.Collection(PreferencesCollection),
		handoffs:    db.Collection(HandoffsCollection),
	}
}

func translate(err error) error {
	err = mongodb.TranslateError(err)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mongodb.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, mongodb.ErrDuplicate):
		return fmt.Errorf("%w: %w", ErrDuplicate, err)
	default:
		return err
	}
}

func householdOID(householdID string) (bson.ObjectID, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return bson.ObjectID{}, errHouseholdRequired
	}
	return hid, nil
}

func userOID(id string) (bson.ObjectID, error) {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return bson.ObjectID{}, fmt.Errorf("shopping: user id %q: %w", id, err)
	}
	return oid, nil
}

func hexOrEmpty(id bson.ObjectID) string {
	if id.IsZero() {
		return ""
	}
	return id.Hex()
}

// optionalUser parses an optional user ID.
func optionalUser(id string) (bson.ObjectID, error) {
	if id == "" {
		return bson.ObjectID{}, nil
	}
	return userOID(id)
}

// Exact amounts store the fraction string and a double copy for ad-hoc
// queries; the string is authoritative.
type amountDoc struct {
	Quantity      string  `bson:"quantity"`
	QuantityValue float64 `bson:"quantityValue"`
	Unit          string  `bson:"unit"`
}

func newAmountDoc(quantity, unit string) amountDoc {
	d := amountDoc{Quantity: quantity, Unit: unit}
	if q, err := ingredients.ParseQuantity(quantity); err == nil {
		d.QuantityValue = q.Float64()
	}
	return d
}

func sizeDoc(s *PackageSize) *amountDoc {
	if s == nil {
		return nil
	}
	d := newAmountDoc(s.Quantity, s.Unit)
	return &d
}

func (d *amountDoc) packageSize() *PackageSize {
	if d == nil {
		return nil
	}
	return &PackageSize{Quantity: d.Quantity, Unit: d.Unit}
}

// --- Settings -----------------------------------------------------------------

type settingsDoc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	Provider    string        `bson:"provider,omitempty"`
	StoreID     string        `bson:"storeId,omitempty"`
	UpdatedBy   bson.ObjectID `bson:"updatedBy"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
}

// GetSettings implements Store.
func (s *MongoStore) GetSettings(ctx context.Context, householdID string) (Settings, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return Settings{}, err
	}
	var d settingsDoc
	if err := s.settings.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}}).Decode(&d); err != nil {
		return Settings{}, translate(err)
	}
	return Settings{HouseholdID: d.HouseholdID.Hex(), Provider: providers.Key(d.Provider), StoreID: d.StoreID, UpdatedBy: hexOrEmpty(d.UpdatedBy), UpdatedAt: d.UpdatedAt.UTC()}, nil
}

// PutSettings implements Store.
func (s *MongoStore) PutSettings(ctx context.Context, in Settings) (Settings, error) {
	hid, err := householdOID(in.HouseholdID)
	if err != nil {
		return Settings{}, err
	}
	uid, err := userOID(in.UpdatedBy)
	if err != nil {
		return Settings{}, err
	}
	d := settingsDoc{HouseholdID: hid, Provider: string(in.Provider), StoreID: in.StoreID, UpdatedBy: uid, UpdatedAt: in.UpdatedAt}
	_, err = s.settings.ReplaceOne(ctx, bson.D{{Key: "householdId", Value: hid}}, d, options.Replace().SetUpsert(true))
	if err != nil {
		return Settings{}, translate(err)
	}
	return s.GetSettings(ctx, in.HouseholdID)
}

// --- Preferences --------------------------------------------------------------

type preferenceDoc struct {
	ID             bson.ObjectID `bson:"_id"`
	HouseholdID    bson.ObjectID `bson:"householdId"`
	Provider       string        `bson:"provider"`
	IngredientKey  string        `bson:"ingredientKey"`
	IngredientName string        `bson:"ingredientName"`
	ProductID      string        `bson:"productId"`
	DisplayName    string        `bson:"displayName"`
	PackageSize    *amountDoc    `bson:"packageSize,omitempty"`
	CreatedBy      bson.ObjectID `bson:"createdBy"`
	CreatedAt      time.Time     `bson:"createdAt"`
	UpdatedBy      bson.ObjectID `bson:"updatedBy"`
	UpdatedAt      time.Time     `bson:"updatedAt"`
}

func (d preferenceDoc) toPreference() Preference {
	return Preference{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), Provider: providers.Key(d.Provider),
		IngredientKey: d.IngredientKey, IngredientName: d.IngredientName, ProductID: d.ProductID, DisplayName: d.DisplayName,
		PackageSize: d.PackageSize.packageSize(),
		CreatedBy:   hexOrEmpty(d.CreatedBy), CreatedAt: d.CreatedAt.UTC(), UpdatedBy: hexOrEmpty(d.UpdatedBy), UpdatedAt: d.UpdatedAt.UTC(),
	}
}

func preferenceFilter(hid bson.ObjectID, provider providers.Key, key string) bson.D {
	return bson.D{{Key: "householdId", Value: hid}, {Key: "provider", Value: string(provider)}, {Key: "ingredientKey", Value: key}}
}

// ListPreferences implements Store.
func (s *MongoStore) ListPreferences(ctx context.Context, householdID string, provider providers.Key) ([]Preference, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	cur, err := s.preferences.Find(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "provider", Value: string(provider)}},
		options.Find().SetSort(bson.D{{Key: "ingredientName", Value: 1}, {Key: "ingredientKey", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []preferenceDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Preference, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toPreference())
	}
	return out, nil
}

// GetPreference implements Store.
func (s *MongoStore) GetPreference(ctx context.Context, householdID string, provider providers.Key, ingredientKey string) (Preference, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return Preference{}, err
	}
	var d preferenceDoc
	if err := s.preferences.FindOne(ctx, preferenceFilter(hid, provider, ingredientKey)).Decode(&d); err != nil {
		return Preference{}, translate(err)
	}
	return d.toPreference(), nil
}

// CountPreferences implements Store.
func (s *MongoStore) CountPreferences(ctx context.Context, householdID string, provider providers.Key) (int, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return 0, err
	}
	n, err := s.preferences.CountDocuments(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "provider", Value: string(provider)}})
	return int(n), translate(err)
}

// UpsertPreference implements Store.
func (s *MongoStore) UpsertPreference(ctx context.Context, p Preference) (Preference, bool, error) {
	hid, err := householdOID(p.HouseholdID)
	if err != nil {
		return Preference{}, false, err
	}
	uid, err := userOID(p.UpdatedBy)
	if err != nil {
		return Preference{}, false, err
	}
	set := bson.D{
		{Key: "ingredientName", Value: p.IngredientName}, {Key: "productId", Value: p.ProductID},
		{Key: "displayName", Value: p.DisplayName}, {Key: "updatedBy", Value: uid}, {Key: "updatedAt", Value: p.UpdatedAt},
	}
	update := bson.D{
		{Key: "$setOnInsert", Value: bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "createdBy", Value: uid}, {Key: "createdAt", Value: p.UpdatedAt}}},
	}
	if size := sizeDoc(p.PackageSize); size != nil {
		set = append(set, bson.E{Key: "packageSize", Value: size})
	} else {
		update = append(update, bson.E{Key: "$unset", Value: bson.D{{Key: "packageSize", Value: ""}}})
	}
	update = append(update, bson.E{Key: "$set", Value: set})
	res, err := s.preferences.UpdateOne(ctx, preferenceFilter(hid, p.Provider, p.IngredientKey), update, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return Preference{}, false, translate(err)
	}
	saved, err := s.GetPreference(ctx, p.HouseholdID, p.Provider, p.IngredientKey)
	return saved, res.UpsertedCount > 0, err
}

// DeletePreference implements Store.
func (s *MongoStore) DeletePreference(ctx context.Context, householdID string, provider providers.Key, ingredientKey string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	res, err := s.preferences.DeleteOne(ctx, preferenceFilter(hid, provider, ingredientKey))
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Handoffs -----------------------------------------------------------------

type sourceDoc struct {
	IngredientKey string      `bson:"ingredientKey"`
	Name          string      `bson:"name"`
	Category      string      `bson:"category"`
	Amounts       []amountDoc `bson:"amounts"`
	Unquantified  bool        `bson:"unquantified"`
	GroceryStatus string      `bson:"groceryStatus,omitempty"`
}

type lineDoc struct {
	ID               string        `bson:"id"`
	Source           sourceDoc     `bson:",inline"`
	ProductID        string        `bson:"productId"`
	ProductName      string        `bson:"productName"`
	PackageSize      *amountDoc    `bson:"packageSize,omitempty"`
	ComputedPackages int           `bson:"computedPackages"`
	Packages         int           `bson:"packages"`
	Reason           string        `bson:"reason,omitempty"`
	Status           string        `bson:"status"`
	ClaimedAt        *time.Time    `bson:"claimedAt,omitempty"`
	Confirmed        int           `bson:"confirmedPackages,omitempty"`
	PurchaseID       string        `bson:"purchaseId,omitempty"`
	ConfirmedBy      bson.ObjectID `bson:"confirmedBy,omitempty"`
	ConfirmedAt      *time.Time    `bson:"confirmedAt,omitempty"`
	SkippedBy        bson.ObjectID `bson:"skippedBy,omitempty"`
	SkippedAt        *time.Time    `bson:"skippedAt,omitempty"`
}

type excludedDoc struct {
	Source sourceDoc `bson:",inline"`
	Reason string    `bson:"reason"`
}

type linkDoc struct {
	URL       string   `bson:"url"`
	LineIDs   []string `bson:"lineIds"`
	ItemCount int      `bson:"itemCount"`
}

type handoffDoc struct {
	ID               bson.ObjectID `bson:"_id"`
	HouseholdID      bson.ObjectID `bson:"householdId"`
	Week             string        `bson:"week"`
	Provider         string        `bson:"provider"`
	StoreID          string        `bson:"storeId,omitempty"`
	Lines            []lineDoc     `bson:"lines"`
	Excluded         []excludedDoc `bson:"excluded"`
	Links            []linkDoc     `bson:"links"`
	AffiliateTracked bool          `bson:"affiliateTracked"`
	CreatedBy        bson.ObjectID `bson:"createdBy"`
	CreatedAt        time.Time     `bson:"createdAt"`
	UpdatedAt        time.Time     `bson:"updatedAt"`
}

func newSourceDoc(s LineSource) sourceDoc {
	d := sourceDoc{IngredientKey: s.IngredientKey, Name: s.Name, Category: s.Category, Unquantified: s.Unquantified,
		GroceryStatus: string(s.GroceryStatus), Amounts: make([]amountDoc, 0, len(s.Amounts))}
	for _, a := range s.Amounts {
		d.Amounts = append(d.Amounts, newAmountDoc(a.Quantity, a.Unit))
	}
	return d
}

func (d sourceDoc) source() LineSource {
	s := LineSource{IngredientKey: d.IngredientKey, Name: d.Name, Category: d.Category, Unquantified: d.Unquantified, GroceryStatus: grocery.Status(d.GroceryStatus)}
	for _, a := range d.Amounts {
		s.Amounts = append(s.Amounts, Amount{Quantity: a.Quantity, Unit: a.Unit})
	}
	return s
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func newHandoffDoc(h Handoff, id, hid bson.ObjectID) (handoffDoc, error) {
	createdBy, err := userOID(h.CreatedBy)
	if err != nil {
		return handoffDoc{}, err
	}
	d := handoffDoc{
		ID: id, HouseholdID: hid, Week: h.Week, Provider: string(h.Provider), StoreID: h.StoreID,
		Lines: make([]lineDoc, 0, len(h.Lines)), Excluded: make([]excludedDoc, 0, len(h.Excluded)), Links: make([]linkDoc, 0, len(h.Links)),
		AffiliateTracked: h.AffiliateTracked, CreatedBy: createdBy, CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
	for _, l := range h.Lines {
		confirmedBy, err := optionalUser(l.ConfirmedBy)
		if err != nil {
			return handoffDoc{}, err
		}
		skippedBy, err := optionalUser(l.SkippedBy)
		if err != nil {
			return handoffDoc{}, err
		}
		d.Lines = append(d.Lines, lineDoc{
			ID: l.ID, Source: newSourceDoc(l.LineSource), ProductID: l.ProductID, ProductName: l.ProductName,
			PackageSize: sizeDoc(l.PackageSize), ComputedPackages: l.ComputedPackages, Packages: l.Packages, Reason: string(l.Reason),
			Status: string(l.Status), Confirmed: l.ConfirmedPackages, PurchaseID: l.PurchaseID,
			ConfirmedBy: confirmedBy, ConfirmedAt: timeOrNil(l.ConfirmedAt), SkippedBy: skippedBy, SkippedAt: timeOrNil(l.SkippedAt),
		})
	}
	for _, e := range h.Excluded {
		d.Excluded = append(d.Excluded, excludedDoc{Source: newSourceDoc(e.LineSource), Reason: string(e.Reason)})
	}
	for _, l := range h.Links {
		d.Links = append(d.Links, linkDoc(l))
	}
	return d, nil
}

func (d handoffDoc) toHandoff() Handoff {
	h := Handoff{
		ID: d.ID.Hex(),
		Proposal: Proposal{
			HouseholdID: d.HouseholdID.Hex(), Week: d.Week, Provider: providers.Key(d.Provider), StoreID: d.StoreID,
			AffiliateTracked: d.AffiliateTracked,
		},
		CreatedBy: hexOrEmpty(d.CreatedBy), CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	for _, l := range d.Lines {
		h.Lines = append(h.Lines, HandoffLine{
			ID: l.ID, LineSource: l.Source.source(), ProductID: l.ProductID, ProductName: l.ProductName,
			PackageSize: l.PackageSize.packageSize(), ComputedPackages: l.ComputedPackages, Packages: l.Packages,
			Reason: providers.Reason(l.Reason), Status: LineStatus(l.Status), ConfirmedPackages: l.Confirmed, PurchaseID: l.PurchaseID,
			ConfirmedBy: hexOrEmpty(l.ConfirmedBy), ConfirmedAt: timeOrZero(l.ConfirmedAt),
			SkippedBy: hexOrEmpty(l.SkippedBy), SkippedAt: timeOrZero(l.SkippedAt),
		})
	}
	for _, e := range d.Excluded {
		h.Excluded = append(h.Excluded, Excluded{LineSource: e.Source.source(), Reason: ExclusionReason(e.Reason)})
	}
	for _, l := range d.Links {
		h.Links = append(h.Links, CartLink(l))
	}
	return h
}

// InsertHandoff implements Store.
func (s *MongoStore) InsertHandoff(ctx context.Context, h Handoff) (Handoff, error) {
	hid, err := householdOID(h.HouseholdID)
	if err != nil {
		return Handoff{}, err
	}
	d, err := newHandoffDoc(h, bson.NewObjectID(), hid)
	if err != nil {
		return Handoff{}, err
	}
	if _, err := s.handoffs.InsertOne(ctx, d); err != nil {
		return Handoff{}, translate(err)
	}
	return d.toHandoff(), nil
}

// GetHandoff implements Store.
func (s *MongoStore) GetHandoff(ctx context.Context, householdID, id string) (Handoff, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return Handoff{}, err
	}
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return Handoff{}, ErrNotFound
	}
	var d handoffDoc
	if err := s.handoffs.FindOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}}).Decode(&d); err != nil {
		return Handoff{}, translate(err)
	}
	return d.toHandoff(), nil
}

// ListHandoffs implements Store.
func (s *MongoStore) ListHandoffs(ctx context.Context, householdID string, f HandoffFilter) ([]Handoff, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	filter := bson.D{{Key: "householdId", Value: hid}}
	if f.Week != "" {
		filter = append(filter, bson.E{Key: "week", Value: f.Week})
	}
	switch f.Status {
	case HandoffOpen:
		filter = append(filter, bson.E{Key: "lines.status", Value: string(LinePending)})
	case HandoffDone:
		filter = append(filter, bson.E{Key: "lines.status", Value: bson.D{{Key: "$ne", Value: string(LinePending)}}})
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultHandoffList
	}
	cur, err := s.handoffs.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, translate(err)
	}
	var docs []handoffDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Handoff, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toHandoff())
	}
	return out, nil
}

func lineFilter(householdID, handoffID string, match bson.D) (bson.D, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	oid, err := mongodb.ParseID(handoffID)
	if err != nil {
		return nil, ErrNotFound
	}
	return bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}, {Key: "lines", Value: bson.D{{Key: "$elemMatch", Value: match}}}}, nil
}

// ClaimLine implements Store.
func (s *MongoStore) ClaimLine(ctx context.Context, householdID, handoffID, lineID string, now, staleBefore time.Time) (bool, error) {
	filter, err := lineFilter(householdID, handoffID, bson.D{
		{Key: "id", Value: lineID},
		{Key: "status", Value: bson.D{{Key: "$ne", Value: string(LineConfirmed)}}},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "claimedAt", Value: bson.D{{Key: "$exists", Value: false}}}},
			bson.D{{Key: "claimedAt", Value: bson.D{{Key: "$lt", Value: staleBefore}}}},
		}},
	})
	if err != nil {
		return false, err
	}
	res, err := s.handoffs.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.D{{Key: "lines.$.claimedAt", Value: now}}}})
	if err != nil {
		return false, translate(err)
	}
	return res.MatchedCount > 0, nil
}

// ReleaseLine implements Store.
func (s *MongoStore) ReleaseLine(ctx context.Context, householdID, handoffID, lineID string) error {
	filter, err := lineFilter(householdID, handoffID, bson.D{{Key: "id", Value: lineID}})
	if err != nil {
		return err
	}
	_, err = s.handoffs.UpdateOne(ctx, filter, bson.D{{Key: "$unset", Value: bson.D{{Key: "lines.$.claimedAt", Value: ""}}}})
	return translate(err)
}

// ConfirmLine implements Store.
func (s *MongoStore) ConfirmLine(ctx context.Context, householdID, handoffID string, line HandoffLine, at time.Time) error {
	filter, err := lineFilter(householdID, handoffID, bson.D{{Key: "id", Value: line.ID}})
	if err != nil {
		return err
	}
	by, err := userOID(line.ConfirmedBy)
	if err != nil {
		return err
	}
	res, err := s.handoffs.UpdateOne(ctx, filter, bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "lines.$.status", Value: string(LineConfirmed)}, {Key: "lines.$.confirmedPackages", Value: line.ConfirmedPackages},
			{Key: "lines.$.purchaseId", Value: line.PurchaseID}, {Key: "lines.$.confirmedBy", Value: by},
			{Key: "lines.$.confirmedAt", Value: line.ConfirmedAt}, {Key: "updatedAt", Value: at},
		}},
		{Key: "$unset", Value: bson.D{{Key: "lines.$.claimedAt", Value: ""}, {Key: "lines.$.skippedBy", Value: ""}, {Key: "lines.$.skippedAt", Value: ""}}},
	})
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// SkipLine implements Store.
func (s *MongoStore) SkipLine(ctx context.Context, householdID, handoffID, lineID, userID string, at time.Time) (bool, error) {
	filter, err := lineFilter(householdID, handoffID, bson.D{{Key: "id", Value: lineID}, {Key: "status", Value: string(LinePending)}})
	if err != nil {
		return false, err
	}
	by, err := userOID(userID)
	if err != nil {
		return false, err
	}
	res, err := s.handoffs.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.D{
		{Key: "lines.$.status", Value: string(LineSkipped)}, {Key: "lines.$.skippedBy", Value: by},
		{Key: "lines.$.skippedAt", Value: at}, {Key: "updatedAt", Value: at},
	}}})
	if err != nil {
		return false, translate(err)
	}
	return res.MatchedCount > 0, nil
}
