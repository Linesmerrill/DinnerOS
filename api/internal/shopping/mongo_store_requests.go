package shopping

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// This file is the shopping_store_requests half of MongoStore: which stores
// each household asked for, and the counts behind the catalog's demand
// signal.

type storeRequestDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	Key         string        `bson:"key"`
	Name        string        `bson:"name"`
	Note        string        `bson:"note,omitempty"`
	RequestedBy bson.ObjectID `bson:"requestedBy"`
	RequestedAt time.Time     `bson:"requestedAt"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
}

func (d storeRequestDoc) toStoreRequest() StoreRequest {
	return StoreRequest{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), Key: d.Key, Name: d.Name, Note: d.Note,
		RequestedBy: hexOrEmpty(d.RequestedBy), RequestedAt: d.RequestedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
}

// ListStoreRequests implements Store.
func (s *MongoStore) ListStoreRequests(ctx context.Context, householdID string) ([]StoreRequest, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	cur, err := s.storeRequests.Find(ctx, bson.D{{Key: "householdId", Value: hid}},
		options.Find().SetSort(bson.D{{Key: "requestedAt", Value: -1}, {Key: "_id", Value: -1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []storeRequestDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]StoreRequest, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toStoreRequest())
	}
	return out, nil
}

// GetStoreRequestByKey implements Store.
func (s *MongoStore) GetStoreRequestByKey(ctx context.Context, householdID, key string) (StoreRequest, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return StoreRequest{}, err
	}
	var d storeRequestDoc
	if err := s.storeRequests.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "key", Value: key}}).Decode(&d); err != nil {
		return StoreRequest{}, translate(err)
	}
	return d.toStoreRequest(), nil
}

// CountStoreRequests implements Store.
func (s *MongoStore) CountStoreRequests(ctx context.Context, householdID string) (int, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return 0, err
	}
	n, err := s.storeRequests.CountDocuments(ctx, bson.D{{Key: "householdId", Value: hid}})
	return int(n), translate(err)
}

// CountStoreRequestsByKey implements Store. It groups the whole collection,
// which the key index covers; the catalog is small and the counts are a
// coarse signal, so this isn't cached.
func (s *MongoStore) CountStoreRequestsByKey(ctx context.Context) (map[string]int, error) {
	cur, err := s.storeRequests.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$key"}, {Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}}}}},
	})
	if err != nil {
		return nil, translate(err)
	}
	var rows []struct {
		Key   string `bson:"_id"`
		Count int    `bson:"count"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, translate(err)
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Count
	}
	return out, nil
}

// UpsertStoreRequest implements Store. A second request for the same store
// updates the note only: _id, requestedBy, and requestedAt are $setOnInsert,
// so the first asker and time survive.
func (s *MongoStore) UpsertStoreRequest(ctx context.Context, r StoreRequest) (StoreRequest, bool, error) {
	hid, err := householdOID(r.HouseholdID)
	if err != nil {
		return StoreRequest{}, false, err
	}
	uid, err := userOID(r.RequestedBy)
	if err != nil {
		return StoreRequest{}, false, err
	}
	update := bson.D{
		{Key: "$setOnInsert", Value: bson.D{
			{Key: "_id", Value: bson.NewObjectID()}, {Key: "requestedBy", Value: uid}, {Key: "requestedAt", Value: r.RequestedAt},
		}},
		{Key: "$set", Value: bson.D{
			{Key: "name", Value: r.Name}, {Key: "note", Value: r.Note}, {Key: "updatedAt", Value: r.UpdatedAt},
		}},
	}
	res, err := s.storeRequests.UpdateOne(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "key", Value: r.Key}}, update, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return StoreRequest{}, false, translate(err)
	}
	saved, err := s.GetStoreRequestByKey(ctx, r.HouseholdID, r.Key)
	return saved, res.UpsertedCount > 0, err
}

// DeleteStoreRequest implements Store.
func (s *MongoStore) DeleteStoreRequest(ctx context.Context, householdID, id string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	res, err := s.storeRequests.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
