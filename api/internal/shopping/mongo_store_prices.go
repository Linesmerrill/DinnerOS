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

// WeekSpendCollection holds the order totals households entered per week.
const WeekSpendCollection = "shopping_week_spend"

func weekSpendIndexes() mongodb.IndexSet {
	return mongodb.IndexSet{
		Collection: WeekSpendCollection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "week", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_week_unique"),
		}},
	}
}

// SetLinePrices implements Store. Each line is its own positional update, so
// a price never touches another line's confirmation fields.
func (s *MongoStore) SetLinePrices(ctx context.Context, householdID, handoffID string, prices map[string]*int64, at time.Time) (Handoff, error) {
	for lineID, price := range prices {
		filter, err := lineFilter(householdID, handoffID, bson.D{{Key: "id", Value: lineID}})
		if err != nil {
			return Handoff{}, err
		}
		update := bson.D{{Key: "$set", Value: bson.D{{Key: "updatedAt", Value: at}}}, {Key: "$unset", Value: bson.D{{Key: "lines.$.priceCents", Value: ""}}}}
		if price != nil {
			update = bson.D{{Key: "$set", Value: bson.D{{Key: "lines.$.priceCents", Value: *price}, {Key: "updatedAt", Value: at}}}}
		}
		// Prices don't bump the revision: they don't change what was sent to
		// the cart, so a send in progress shouldn't conflict with them.
		if _, err := s.handoffs.UpdateOne(ctx, filter, update); err != nil {
			return Handoff{}, translate(err)
		}
	}
	return s.GetHandoff(ctx, householdID, handoffID)
}

// SetPreferencePrice implements Store.
func (s *MongoStore) SetPreferencePrice(ctx context.Context, householdID string, provider providers.Key, ingredientKey, productID string, priceCents int64, at time.Time) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	filter := append(preferenceFilter(hid, provider, ingredientKey), bson.E{Key: "productId", Value: productID})
	res, err := s.preferences.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.D{
		{Key: "priceCents", Value: priceCents}, {Key: "priceUpdatedAt", Value: at},
	}}})
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

type weekSpendDoc struct {
	HouseholdID     bson.ObjectID `bson:"householdId"`
	Week            string        `bson:"week"`
	OrderTotalCents int64         `bson:"orderTotalCents"`
	UpdatedBy       bson.ObjectID `bson:"updatedBy"`
	UpdatedAt       time.Time     `bson:"updatedAt"`
}

func (d weekSpendDoc) toWeekSpend() WeekSpend {
	return WeekSpend{HouseholdID: d.HouseholdID.Hex(), Week: d.Week, OrderTotalCents: d.OrderTotalCents, UpdatedBy: hexOrEmpty(d.UpdatedBy), UpdatedAt: d.UpdatedAt.UTC()}
}

// GetWeekSpend implements Store.
func (s *MongoStore) GetWeekSpend(ctx context.Context, householdID, week string) (WeekSpend, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return WeekSpend{}, err
	}
	var d weekSpendDoc
	if err := s.weekSpend.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}}).Decode(&d); err != nil {
		return WeekSpend{}, translate(err)
	}
	return d.toWeekSpend(), nil
}

// ListWeekSpend implements Store.
func (s *MongoStore) ListWeekSpend(ctx context.Context, householdID string, limit int) ([]WeekSpend, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	// ISO week strings sort chronologically.
	cur, err := s.weekSpend.Find(ctx, bson.D{{Key: "householdId", Value: hid}},
		options.Find().SetSort(bson.D{{Key: "week", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, translate(err)
	}
	var docs []weekSpendDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]WeekSpend, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toWeekSpend())
	}
	return out, nil
}

// PutWeekSpend implements Store.
func (s *MongoStore) PutWeekSpend(ctx context.Context, w WeekSpend) (WeekSpend, error) {
	hid, err := householdOID(w.HouseholdID)
	if err != nil {
		return WeekSpend{}, err
	}
	uid, err := userOID(w.UpdatedBy)
	if err != nil {
		return WeekSpend{}, err
	}
	d := weekSpendDoc{HouseholdID: hid, Week: w.Week, OrderTotalCents: w.OrderTotalCents, UpdatedBy: uid, UpdatedAt: w.UpdatedAt}
	filter := bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: w.Week}}
	if _, err := s.weekSpend.ReplaceOne(ctx, filter, d, options.Replace().SetUpsert(true)); err != nil {
		return WeekSpend{}, translate(err)
	}
	return s.GetWeekSpend(ctx, w.HouseholdID, w.Week)
}

// DeleteWeekSpend implements Store.
func (s *MongoStore) DeleteWeekSpend(ctx context.Context, householdID, week string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	res, err := s.weekSpend.DeleteOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
