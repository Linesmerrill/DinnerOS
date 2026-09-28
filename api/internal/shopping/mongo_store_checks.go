package shopping

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// CheckPausesCollection holds, per provider, until when product checks are
// paused after the provider refused or throttled one.
const CheckPausesCollection = "shopping_check_pauses"

func checkIndexes() mongodb.IndexSet {
	return mongodb.IndexSet{
		Collection: PreferencesCollection,
		Indexes: []mongo.IndexModel{{
			// The sweep reads every household's saved products for a provider,
			// never-checked first, then oldest check first.
			Keys:    bson.D{{Key: "provider", Value: 1}, {Key: "check.checkedAt", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetName("provider_check_checkedAt"),
		}},
	}
}

// checkDoc is a saved product's last check (ProductCheckState).
type checkDoc struct {
	ProductID      string     `bson:"productId"`
	Status         string     `bson:"status"`
	Detail         string     `bson:"detail,omitempty"`
	CheckedAt      time.Time  `bson:"checkedAt"`
	VerifiedStatus string     `bson:"verifiedStatus,omitempty"`
	VerifiedAt     *time.Time `bson:"verifiedAt,omitempty"`
	VerifiedSince  *time.Time `bson:"verifiedSince,omitempty"`
	Name           string     `bson:"name,omitempty"`
	PriceCents     *int64     `bson:"priceCents,omitempty"`
	Pickup         string     `bson:"pickup,omitempty"`
	Delivery       string     `bson:"delivery,omitempty"`
	Store          string     `bson:"store,omitempty"`
}

func newCheckDoc(c *ProductCheckState) *checkDoc {
	if c == nil {
		return nil
	}
	return &checkDoc{
		ProductID: c.ProductID, Status: string(c.Status), Detail: c.Detail, CheckedAt: c.CheckedAt,
		VerifiedStatus: string(c.VerifiedStatus), VerifiedAt: timeOrNil(c.VerifiedAt), VerifiedSince: timeOrNil(c.VerifiedSince),
		Name: c.Name, PriceCents: c.PriceCents, Pickup: c.Pickup, Delivery: c.Delivery, Store: c.Store,
	}
}

func (d *checkDoc) state() *ProductCheckState {
	if d == nil {
		return nil
	}
	return &ProductCheckState{
		ProductID: d.ProductID, Status: providers.ProductStatus(d.Status), Detail: d.Detail, CheckedAt: d.CheckedAt.UTC(),
		VerifiedStatus: providers.ProductStatus(d.VerifiedStatus), VerifiedAt: timeOrZero(d.VerifiedAt), VerifiedSince: timeOrZero(d.VerifiedSince),
		Name: d.Name, PriceCents: d.PriceCents, Pickup: d.Pickup, Delivery: d.Delivery, Store: d.Store,
	}
}

// SaveProductCheck implements Store.
func (s *MongoStore) SaveProductCheck(ctx context.Context, p Preference, priceChanged bool) (bool, error) {
	id, err := mongodb.ParseID(p.ID)
	if err != nil {
		return false, ErrNotFound
	}
	set := bson.D{{Key: "check", Value: newCheckDoc(p.Check)}}
	if priceChanged && p.PriceCents != nil {
		set = append(set,
			bson.E{Key: "priceCents", Value: *p.PriceCents}, bson.E{Key: "priceSource", Value: string(p.PriceSource)},
			bson.E{Key: "priceUpdatedAt", Value: p.PriceUpdatedAt})
	}
	// Matching the product ID too means a result for a product the member
	// replaced meanwhile is dropped rather than pinned on the new one.
	res, err := s.preferences.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "productId", Value: p.ProductID}}, bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return false, translate(err)
	}
	return res.MatchedCount > 0, nil
}

// ListPreferencesToCheck implements Store.
func (s *MongoStore) ListPreferencesToCheck(ctx context.Context, provider providers.Key, before time.Time, limit int) ([]Preference, error) {
	filter := bson.D{
		{Key: "provider", Value: string(provider)},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "check.checkedAt", Value: bson.D{{Key: "$exists", Value: false}}}},
			bson.D{{Key: "check.checkedAt", Value: bson.D{{Key: "$lt", Value: before}}}},
		}},
	}
	// Missing fields sort first ascending, so never-checked products lead.
	opts := options.Find().SetSort(bson.D{{Key: "check.checkedAt", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(int64(limit))
	cur, err := s.preferences.Find(ctx, filter, opts)
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

type checkPauseDoc struct {
	Provider string    `bson:"_id"`
	Until    time.Time `bson:"until"`
	Reason   string    `bson:"reason"`
	At       time.Time `bson:"at"`
}

// GetCheckPause implements Store.
func (s *MongoStore) GetCheckPause(ctx context.Context, provider providers.Key) (CheckPause, error) {
	var d checkPauseDoc
	if err := s.checkPauses.FindOne(ctx, bson.D{{Key: "_id", Value: string(provider)}}).Decode(&d); err != nil {
		return CheckPause{}, translate(err)
	}
	return CheckPause{Provider: providers.Key(d.Provider), Until: d.Until.UTC(), Reason: d.Reason, At: d.At.UTC()}, nil
}

// SetCheckPause implements Store.
func (s *MongoStore) SetCheckPause(ctx context.Context, p CheckPause) error {
	d := checkPauseDoc{Provider: string(p.Provider), Until: p.Until, Reason: p.Reason, At: p.At}
	_, err := s.checkPauses.ReplaceOne(ctx, bson.D{{Key: "_id", Value: d.Provider}}, d, options.Replace().SetUpsert(true))
	return translate(err)
}
