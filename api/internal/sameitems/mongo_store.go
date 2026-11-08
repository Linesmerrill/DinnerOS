package sameitems

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
const Collection = "same_items"

// Indexes returns the indexes MongoStore relies on. The unique key makes a
// name the same as one other item at most.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "variantKey", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_variantKey_unique"),
		}},
	}}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	links *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{links: db.Collection(Collection)}
}

type linkDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	VariantKey  string        `bson:"variantKey"`
	VariantName string        `bson:"variantName"`
	KeptKey     string        `bson:"keptKey"`
	KeptName    string        `bson:"keptName"`
	CreatedBy   bson.ObjectID `bson:"createdBy"`
	CreatedAt   time.Time     `bson:"createdAt"`
}

func (d linkDoc) toLink() Link {
	return Link{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), VariantKey: d.VariantKey, VariantName: d.VariantName,
		KeptKey: d.KeptKey, KeptName: d.KeptName, CreatedBy: d.CreatedBy.Hex(), CreatedAt: d.CreatedAt.UTC(),
	}
}

func translate(err error) error {
	err = mongodb.TranslateError(err)
	if errors.Is(err, mongodb.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func householdOID(householdID string) (bson.ObjectID, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return bson.ObjectID{}, fmt.Errorf("sameitems: household id: %w", err)
	}
	return hid, nil
}

// ListLinks implements Store.
func (s *MongoStore) ListLinks(ctx context.Context, householdID string) ([]Link, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}})
	cur, err := s.links.Find(ctx, bson.D{{Key: "householdId", Value: hid}}, opts)
	if err != nil {
		return nil, translate(err)
	}
	var docs []linkDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Link, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toLink())
	}
	return out, nil
}

// PutLink implements Store with an upsert on the unique key; when two first
// links for one name race, the loser retries once as an update.
func (s *MongoStore) PutLink(ctx context.Context, l Link) (Link, bool, error) {
	hid, err := householdOID(l.HouseholdID)
	if err != nil {
		return Link{}, false, err
	}
	by, err := mongodb.ParseID(l.CreatedBy)
	if err != nil {
		return Link{}, false, fmt.Errorf("sameitems: createdBy: %w", err)
	}
	filter := bson.D{{Key: "householdId", Value: hid}, {Key: "variantKey", Value: l.VariantKey}}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "variantName", Value: l.VariantName},
			{Key: "keptKey", Value: l.KeptKey},
			{Key: "keptName", Value: l.KeptName},
		}},
		{Key: "$setOnInsert", Value: bson.D{
			{Key: "_id", Value: bson.NewObjectID()},
			{Key: "createdBy", Value: by},
			{Key: "createdAt", Value: l.CreatedAt},
		}},
	}
	res, err := s.links.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	if errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		res, err = s.links.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	}
	if err != nil {
		return Link{}, false, translate(err)
	}
	var d linkDoc
	if err := s.links.FindOne(ctx, filter).Decode(&d); err != nil {
		return Link{}, false, translate(err)
	}
	return d.toLink(), res.UpsertedCount > 0, nil
}

// RepointKept implements Store.
func (s *MongoStore) RepointKept(ctx context.Context, householdID, fromKey, keptKey, keptName string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	_, err = s.links.UpdateMany(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "keptKey", Value: fromKey}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "keptKey", Value: keptKey}, {Key: "keptName", Value: keptName}}}})
	return translate(err)
}

// DeleteLink implements Store.
func (s *MongoStore) DeleteLink(ctx context.Context, householdID, id string) error {
	hid, err1 := mongodb.ParseID(householdID)
	oid, err2 := mongodb.ParseID(id)
	if err1 != nil || err2 != nil {
		return ErrNotFound
	}
	res, err := s.links.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeHousehold deletes every link the household has. Account deletion
// calls it when the household's last member deletes their account. It is
// idempotent.
func (s *MongoStore) PurgeHousehold(ctx context.Context, householdID string) error {
	return mongodb.DeleteByID(ctx, "householdId", householdID, s.links)
}
