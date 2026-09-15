package ratings

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

// Collection is the ratings collection.
const Collection = "recipe_ratings"

// Indexes returns the indexes this package's MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{{
			// One rating per member and recipe. Its prefix also serves a
			// recipe's rating list and the per-page aggregate.
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "recipeId", Value: 1}, {Key: "userId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_recipeId_userId_unique"),
		}},
	}}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	ratings *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{ratings: db.Collection(Collection)}
}

type ratingDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	RecipeID    bson.ObjectID `bson:"recipeId"`
	UserID      bson.ObjectID `bson:"userId"`
	Score       int           `bson:"score"`
	Comment     string        `bson:"comment"`
	Tags        []string      `bson:"tags"`
	CreatedAt   time.Time     `bson:"createdAt"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
}

func (d ratingDoc) toRating() Rating {
	r := Rating{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), RecipeID: d.RecipeID.Hex(), UserID: d.UserID.Hex(),
		Score: d.Score, Comment: d.Comment, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	for _, t := range d.Tags {
		r.Tags = append(r.Tags, Tag(t))
	}
	return r
}

type ratingKey struct {
	household, recipe, user bson.ObjectID
}

func parseKey(householdID, recipeID, userID string) (ratingKey, error) {
	var k ratingKey
	var err error
	if k.household, err = mongodb.ParseID(householdID); err != nil {
		return k, fmt.Errorf("ratings: household id: %w", err)
	}
	if k.recipe, err = mongodb.ParseID(recipeID); err != nil {
		return k, fmt.Errorf("ratings: recipe id: %w", err)
	}
	if k.user, err = mongodb.ParseID(userID); err != nil {
		return k, fmt.Errorf("ratings: user id: %w", err)
	}
	return k, nil
}

func (k ratingKey) filter() bson.D {
	return bson.D{{Key: "householdId", Value: k.household}, {Key: "recipeId", Value: k.recipe}, {Key: "userId", Value: k.user}}
}

// Upsert implements Store with a single findOneAndUpdate that returns the
// replaced rating. When two first ratings by the same member race, the loser
// hits the unique index and retries as an update.
func (s *MongoStore) Upsert(ctx context.Context, r Rating) (Rating, *Rating, error) {
	k, err := parseKey(r.HouseholdID, r.RecipeID, r.UserID)
	if err != nil {
		return Rating{}, nil, err
	}
	tags := make([]string, 0, len(r.Tags))
	for _, t := range r.Tags {
		tags = append(tags, string(t))
	}
	for attempt := 0; ; attempt++ {
		id := bson.NewObjectID()
		update := bson.D{
			{Key: "$set", Value: bson.D{
				{Key: "score", Value: r.Score},
				{Key: "comment", Value: r.Comment},
				{Key: "tags", Value: tags},
				{Key: "updatedAt", Value: r.UpdatedAt},
			}},
			{Key: "$setOnInsert", Value: bson.D{{Key: "_id", Value: id}, {Key: "createdAt", Value: r.CreatedAt}}},
		}
		var prev ratingDoc
		err := s.ratings.FindOneAndUpdate(ctx, k.filter(), update,
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.Before)).Decode(&prev)
		saved := r
		saved.Tags = nilIfEmpty(r.Tags)
		switch {
		case errors.Is(err, mongo.ErrNoDocuments):
			saved.ID = id.Hex()
			return saved, nil, nil
		case err == nil:
			previous := prev.toRating()
			saved.ID, saved.CreatedAt = previous.ID, previous.CreatedAt
			return saved, &previous, nil
		case mongo.IsDuplicateKeyError(err) && attempt == 0:
			continue
		default:
			return Rating{}, nil, mongodb.TranslateError(err)
		}
	}
}

// Delete implements Store.
func (s *MongoStore) Delete(ctx context.Context, householdID, recipeID, userID string) (Rating, error) {
	k, err := parseKey(householdID, recipeID, userID)
	if err != nil {
		return Rating{}, ErrNotFound
	}
	var doc ratingDoc
	if err := s.ratings.FindOneAndDelete(ctx, k.filter()).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return Rating{}, ErrNotFound
		}
		return Rating{}, mongodb.TranslateError(err)
	}
	return doc.toRating(), nil
}

// ListForRecipe implements Store.
func (s *MongoStore) ListForRecipe(ctx context.Context, householdID, recipeID string) ([]Rating, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	rid, err := mongodb.ParseID(recipeID)
	if err != nil {
		return nil, nil
	}
	cur, err := s.ratings.Find(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "recipeId", Value: rid}},
		options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}, {Key: "_id", Value: -1}}))
	if err != nil {
		return nil, mongodb.TranslateError(err)
	}
	var docs []ratingDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, mongodb.TranslateError(err)
	}
	var out []Rating
	for _, d := range docs {
		out = append(out, d.toRating())
	}
	return out, nil
}

// ListForHousehold implements Store. The unique index's
// {householdId, recipeId, userId} prefix serves both the filter and the sort.
func (s *MongoStore) ListForHousehold(ctx context.Context, householdID string, limit int) ([]Rating, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	cur, err := s.ratings.Find(ctx, bson.D{{Key: "householdId", Value: hid}}, options.Find().
		SetSort(bson.D{{Key: "recipeId", Value: 1}, {Key: "userId", Value: 1}}).
		SetLimit(int64(limit)).
		SetProjection(bson.D{{Key: "comment", Value: 0}}))
	if err != nil {
		return nil, mongodb.TranslateError(err)
	}
	var docs []ratingDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, mongodb.TranslateError(err)
	}
	var out []Rating
	for _, d := range docs {
		out = append(out, d.toRating())
	}
	return out, nil
}

// Summaries implements Store with one aggregation for the counts and one find
// for the user's own ratings. Malformed recipe IDs are skipped.
func (s *MongoStore) Summaries(ctx context.Context, householdID, userID string, recipeIDs []string) (map[string]Summary, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, fmt.Errorf("ratings: household id: %w", err)
	}
	oids := make([]bson.ObjectID, 0, len(recipeIDs))
	for _, id := range recipeIDs {
		if oid, err := mongodb.ParseID(id); err == nil {
			oids = append(oids, oid)
		}
	}
	out := map[string]Summary{}
	if len(oids) == 0 {
		return out, nil
	}
	match := bson.D{{Key: "householdId", Value: hid}, {Key: "recipeId", Value: bson.D{{Key: "$in", Value: oids}}}}

	cur, err := s.ratings.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$recipeId"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "sum", Value: bson.D{{Key: "$sum", Value: "$score"}}},
		}}},
	})
	if err != nil {
		return nil, mongodb.TranslateError(err)
	}
	var groups []struct {
		RecipeID bson.ObjectID `bson:"_id"`
		Count    int           `bson:"count"`
		Sum      int           `bson:"sum"`
	}
	if err := cur.All(ctx, &groups); err != nil {
		return nil, mongodb.TranslateError(err)
	}
	for _, g := range groups {
		out[g.RecipeID.Hex()] = Summary{Count: g.Count, Sum: g.Sum}
	}

	uid, err := mongodb.ParseID(userID)
	if err != nil || len(groups) == 0 {
		return out, nil
	}
	cur, err = s.ratings.Find(ctx, append(match, bson.E{Key: "userId", Value: uid}))
	if err != nil {
		return nil, mongodb.TranslateError(err)
	}
	var mine []ratingDoc
	if err := cur.All(ctx, &mine); err != nil {
		return nil, mongodb.TranslateError(err)
	}
	for _, d := range mine {
		r := d.toRating()
		summary := out[r.RecipeID]
		summary.Mine = &r
		out[r.RecipeID] = summary
	}
	return out, nil
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}
