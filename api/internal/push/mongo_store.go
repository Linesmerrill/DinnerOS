package push

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

// Collection is the device tokens collection.
const Collection = "device_tokens"

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{
			{
				// A token belongs to one user at a time.
				Keys:    bson.D{{Key: "token", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("token_unique"),
			},
			{
				// The sweep's lookup: the devices of a household's members.
				Keys:    bson.D{{Key: "userId", Value: 1}},
				Options: options.Index().SetName("userId"),
			},
		},
	}}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	tokens *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{tokens: db.Collection(Collection)}
}

type tokenDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	Token       string        `bson:"token"`
	UserID      bson.ObjectID `bson:"userId"`
	Environment string        `bson:"environment"`
	Platform    string        `bson:"platform"`
	CreatedAt   time.Time     `bson:"createdAt"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
}

func (d tokenDoc) toDeviceToken() DeviceToken {
	return DeviceToken{
		Token: d.Token, UserID: d.UserID.Hex(), Environment: Environment(d.Environment), Platform: d.Platform,
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
}

// Upsert implements Store with one findOneAndUpdate. Two first
// registrations of the same token can race on the unique index; the loser
// retries once as an update.
func (s *MongoStore) Upsert(ctx context.Context, t DeviceToken) (DeviceToken, error) {
	d, err := s.upsert(ctx, t)
	if mongo.IsDuplicateKeyError(err) {
		d, err = s.upsert(ctx, t)
	}
	if err != nil {
		return DeviceToken{}, err
	}
	return d.toDeviceToken(), nil
}

func (s *MongoStore) upsert(ctx context.Context, t DeviceToken) (tokenDoc, error) {
	uid, err := mongodb.ParseID(t.UserID)
	if err != nil {
		return tokenDoc{}, fmt.Errorf("push: user id: %w", err)
	}
	var d tokenDoc
	err = s.tokens.FindOneAndUpdate(ctx,
		bson.D{{Key: "token", Value: t.Token}},
		bson.D{
			{Key: "$set", Value: bson.D{
				{Key: "userId", Value: uid}, {Key: "environment", Value: string(t.Environment)},
				{Key: "platform", Value: t.Platform}, {Key: "updatedAt", Value: t.UpdatedAt},
			}},
			{Key: "$setOnInsert", Value: bson.D{{Key: "createdAt", Value: t.CreatedAt}}},
		},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&d)
	if mongo.IsDuplicateKeyError(err) {
		return tokenDoc{}, err
	}
	return d, translate(err)
}

// Delete implements Store.
func (s *MongoStore) Delete(ctx context.Context, userID, token string) error {
	uid, err := mongodb.ParseID(userID)
	if err != nil {
		return nil
	}
	_, err = s.tokens.DeleteOne(ctx, bson.D{{Key: "token", Value: token}, {Key: "userId", Value: uid}})
	return translate(err)
}

// DeleteToken implements Store.
func (s *MongoStore) DeleteToken(ctx context.Context, token string) error {
	_, err := s.tokens.DeleteOne(ctx, bson.D{{Key: "token", Value: token}})
	return translate(err)
}

// ListByUsers implements Store.
func (s *MongoStore) ListByUsers(ctx context.Context, userIDs []string) ([]DeviceToken, error) {
	oids := make([]bson.ObjectID, 0, len(userIDs))
	for _, id := range userIDs {
		if oid, err := mongodb.ParseID(id); err == nil {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return nil, nil
	}
	cur, err := s.tokens.Find(ctx, bson.D{{Key: "userId", Value: bson.D{{Key: "$in", Value: oids}}}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []tokenDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]DeviceToken, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toDeviceToken())
	}
	return out, nil
}

func translate(err error) error {
	err = mongodb.TranslateError(err)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mongodb.ErrNotFound):
		return ErrNotFound
	default:
		return err
	}
}
