package shopping

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// This file is the shopping_order_weeks half of MongoStore: the weeks a
// household said it ordered. It is the only stored half of the order
// reminder; everything else about a reminder is derived on read
// (order_reminder.go).

type orderedWeekDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	Week        string        `bson:"week"`
	OrderedBy   bson.ObjectID `bson:"orderedBy"`
	OrderedAt   time.Time     `bson:"orderedAt"`
}

func (d orderedWeekDoc) toOrderedWeek() OrderedWeek {
	return OrderedWeek{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), Week: d.Week,
		OrderedBy: hexOrEmpty(d.OrderedBy), OrderedAt: d.OrderedAt.UTC(),
	}
}

// GetOrderedWeek implements Store.
func (s *MongoStore) GetOrderedWeek(ctx context.Context, householdID, week string) (OrderedWeek, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return OrderedWeek{}, err
	}
	var d orderedWeekDoc
	err = s.orderWeeks.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}}).Decode(&d)
	if err != nil {
		return OrderedWeek{}, translate(err)
	}
	return d.toOrderedWeek(), nil
}

// MarkWeekOrdered implements Store. Everything is $setOnInsert, so marking an
// already-marked week is a no-op that keeps the first member and time.
func (s *MongoStore) MarkWeekOrdered(ctx context.Context, w OrderedWeek) (OrderedWeek, error) {
	hid, err := householdOID(w.HouseholdID)
	if err != nil {
		return OrderedWeek{}, err
	}
	uid, err := userOID(w.OrderedBy)
	if err != nil {
		return OrderedWeek{}, err
	}
	update := bson.D{{Key: "$setOnInsert", Value: bson.D{
		{Key: "_id", Value: bson.NewObjectID()},
		{Key: "orderedBy", Value: uid},
		{Key: "orderedAt", Value: w.OrderedAt},
	}}}
	if _, err := s.orderWeeks.UpdateOne(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: w.Week}},
		update, options.UpdateOne().SetUpsert(true),
	); err != nil {
		return OrderedWeek{}, translate(err)
	}
	return s.GetOrderedWeek(ctx, w.HouseholdID, w.Week)
}

// UnmarkWeekOrdered implements Store.
func (s *MongoStore) UnmarkWeekOrdered(ctx context.Context, householdID, week string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	res, err := s.orderWeeks.DeleteOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
