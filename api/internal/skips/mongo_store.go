package skips

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Collection is the collection this package owns.
const Collection = "grocery_skips"

// Indexes returns the indexes MongoStore relies on. The unique key is what
// makes a second skip for one ingredient impossible, so "skip once" can become
// "skip forever" by replacing the stored skip rather than stacking a new one.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "ingredientKey", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_ingredientKey_unique"),
		}},
	}}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	skips *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{skips: db.Collection(Collection)}
}

type skipDoc struct {
	ID            bson.ObjectID `bson:"_id"`
	HouseholdID   bson.ObjectID `bson:"householdId"`
	IngredientKey string        `bson:"ingredientKey"`
	Key           string        `bson:"key"`
	Name          string        `bson:"name"`
	Scope         string        `bson:"scope"`
	Week          string        `bson:"week,omitempty"`
	CreatedBy     bson.ObjectID `bson:"createdBy"`
	CreatedAt     time.Time     `bson:"createdAt"`
	UpdatedBy     bson.ObjectID `bson:"updatedBy"`
	UpdatedAt     time.Time     `bson:"updatedAt"`
}

func (d skipDoc) toSkip() Skip {
	return Skip{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), IngredientKey: d.IngredientKey,
		Key: d.Key, Name: d.Name, Scope: Scope(d.Scope), Week: d.Week,
		CreatedBy: d.CreatedBy.Hex(), CreatedAt: d.CreatedAt.UTC(),
		UpdatedBy: d.UpdatedBy.Hex(), UpdatedAt: d.UpdatedAt.UTC(),
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
		return bson.ObjectID{}, fmt.Errorf("skips: household id: %w", err)
	}
	return hid, nil
}

// ListSkips implements Store.
func (s *MongoStore) ListSkips(ctx context.Context, householdID string) ([]Skip, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}})
	cur, err := s.skips.Find(ctx, bson.D{{Key: "householdId", Value: hid}}, opts)
	if err != nil {
		return nil, translate(err)
	}
	var docs []skipDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Skip, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toSkip())
	}
	return out, nil
}

// CountSkips implements Store.
func (s *MongoStore) CountSkips(ctx context.Context, householdID string) (int, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return 0, err
	}
	n, err := s.skips.CountDocuments(ctx, bson.D{{Key: "householdId", Value: hid}})
	if err != nil {
		return 0, translate(err)
	}
	return int(n), nil
}

// PutSkip implements Store with an upsert on the unique key; when two first
// skips for one ingredient race, the loser retries once as an update.
func (s *MongoStore) PutSkip(ctx context.Context, skip Skip) (Skip, bool, error) {
	hid, err := householdOID(skip.HouseholdID)
	if err != nil {
		return Skip{}, false, err
	}
	by, err := mongodb.ParseID(skip.UpdatedBy)
	if err != nil {
		return Skip{}, false, fmt.Errorf("skips: updatedBy: %w", err)
	}
	filter := bson.D{{Key: "householdId", Value: hid}, {Key: "ingredientKey", Value: skip.IngredientKey}}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "key", Value: skip.Key},
			{Key: "name", Value: skip.Name},
			{Key: "scope", Value: string(skip.Scope)},
			{Key: "week", Value: skip.Week},
			{Key: "updatedBy", Value: by},
			{Key: "updatedAt", Value: skip.UpdatedAt},
		}},
		{Key: "$setOnInsert", Value: bson.D{
			{Key: "_id", Value: bson.NewObjectID()},
			{Key: "createdBy", Value: by},
			{Key: "createdAt", Value: skip.CreatedAt},
		}},
	}
	res, err := s.skips.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	if errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		res, err = s.skips.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	}
	if err != nil {
		return Skip{}, false, translate(err)
	}
	var d skipDoc
	if err := s.skips.FindOne(ctx, filter).Decode(&d); err != nil {
		return Skip{}, false, translate(err)
	}
	return d.toSkip(), res.UpsertedCount > 0, nil
}

// DeleteSkip implements Store.
func (s *MongoStore) DeleteSkip(ctx context.Context, householdID, id string) error {
	hid, err1 := mongodb.ParseID(householdID)
	oid, err2 := mongodb.ParseID(id)
	if err1 != nil || err2 != nil {
		return ErrNotFound
	}
	res, err := s.skips.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
