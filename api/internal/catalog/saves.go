package catalog

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Saves (decision 711): every time a household adds a shared recipe to its
// own recipes, one save is kept. Trending counts the last week's saves and
// Top Rated adds all of them to the ratings. A save names the household so
// one household saving, deleting, and saving again counts once.

// Save is one household adding one shared recipe.
type Save struct {
	CatalogID string
	// Group is the entry's group (groupKey) when it was saved, so a save
	// counts for every copy of the recipe (decision 689).
	Group       string
	HouseholdID string
	At          time.Time
}

// SaveStore keeps saves.
type SaveStore interface {
	Record(ctx context.Context, s Save) error
	// Counts maps each group to how many different households saved it at
	// or after since; the zero time counts every save.
	Counts(ctx context.Context, since time.Time) (map[string]int, error)
}

// RatingSignals reads how households rated their copies of shared recipes.
type RatingSignals interface {
	// CatalogRatings maps a catalogKey to the ratings of household recipes
	// that carry it.
	CatalogRatings(ctx context.Context) (map[string]RatingStat, error)
}

// RatingStat sums ratings of one recipe across households. Each household
// counts once, with the mean of its members' scores.
type RatingStat struct {
	Households int
	// Sum is the sum of each household's mean score (1 to 5).
	Sum float64
}

// WithSaves turns on save counting and returns s.
func (s *Service) WithSaves(saves SaveStore) *Service {
	s.saves = saves
	return s
}

// WithRatings turns on the ratings Top Rated reads and returns s.
func (s *Service) WithRatings(r RatingSignals) *Service {
	s.ratings = r
	return s
}

// SavesCollection holds saves.
const SavesCollection = "catalog_saves"

// SaveIndexes returns the indexes MongoSaves relies on.
func SaveIndexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{Collection: SavesCollection, Indexes: []mongo.IndexModel{
		// Trending reads the last 7 days.
		{Keys: bson.D{{Key: "at", Value: -1}}, Options: options.Index().SetName("at")},
		{Keys: bson.D{{Key: "group", Value: 1}, {Key: "householdId", Value: 1}}, Options: options.Index().SetName("group_householdId")},
		// Account deletion removes a household's saves.
		{Keys: bson.D{{Key: "householdId", Value: 1}}, Options: options.Index().SetName("householdId")},
	}}}
}

// MongoSaves is the MongoDB SaveStore.
type MongoSaves struct{ coll *mongo.Collection }

var _ SaveStore = (*MongoSaves)(nil)

// NewMongoSaves returns a store using db.
func NewMongoSaves(db *mongo.Database) *MongoSaves {
	return &MongoSaves{coll: db.Collection(SavesCollection)}
}

type saveDoc struct {
	CatalogID   bson.ObjectID `bson:"catalogId"`
	Group       string        `bson:"group"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	At          time.Time     `bson:"at"`
}

// Record implements SaveStore.
func (s *MongoSaves) Record(ctx context.Context, sv Save) error {
	cid, err := mongodb.ParseID(sv.CatalogID)
	if err != nil {
		return ErrNotFound
	}
	hid, err := mongodb.ParseID(sv.HouseholdID)
	if err != nil {
		return fmt.Errorf("catalog: bad household id")
	}
	if _, err := s.coll.InsertOne(ctx, saveDoc{CatalogID: cid, Group: sv.Group, HouseholdID: hid, At: sv.At}); err != nil {
		return fmt.Errorf("record catalog save: %w", err)
	}
	return nil
}

// Counts implements SaveStore.
func (s *MongoSaves) Counts(ctx context.Context, since time.Time) (map[string]int, error) {
	match := bson.D{}
	if !since.IsZero() {
		match = bson.D{{Key: "at", Value: bson.D{{Key: "$gte", Value: since}}}}
	}
	cur, err := s.coll.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: bson.D{{Key: "g", Value: "$group"}, {Key: "h", Value: "$householdId"}}}}}},
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$_id.g"}, {Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("count catalog saves: %w", err)
	}
	var rows []struct {
		Group string `bson:"_id"`
		N     int    `bson:"n"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("count catalog saves: %w", err)
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.Group] = r.N
	}
	return out, nil
}

// PurgeHousehold removes a household's saves, for account deletion.
func (s *MongoSaves) PurgeHousehold(ctx context.Context, householdID string) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	if _, err := s.coll.DeleteMany(ctx, bson.D{{Key: "householdId", Value: hid}}); err != nil {
		return fmt.Errorf("purge catalog saves: %w", err)
	}
	return nil
}

// MongoRatings reads Top Rated's ratings straight from the ratings and
// recipes collections: a household's rating is kept on its own copy of a
// recipe, and that copy's catalogKey says which shared recipe it is.
type MongoRatings struct {
	ratings *mongo.Collection
}

var _ RatingSignals = (*MongoRatings)(nil)

// RatingsCollection is the ratings package's collection, named here so the
// catalog does not import a package that depends on households.
const RatingsCollection = "recipe_ratings"

// NewMongoRatings returns a reader using db.
func NewMongoRatings(db *mongo.Database) *MongoRatings {
	return &MongoRatings{ratings: db.Collection(RatingsCollection)}
}

// CatalogRatings implements RatingSignals: each household's mean score per
// recipe, joined to the recipe's catalogKey, summed per key.
func (s *MongoRatings) CatalogRatings(ctx context.Context) (map[string]RatingStat, error) {
	cur, err := s.ratings.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "h", Value: "$householdId"}, {Key: "r", Value: "$recipeId"}}},
			{Key: "score", Value: bson.D{{Key: "$avg", Value: "$score"}}},
		}}},
		{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: recipes.RecipesCollection},
			{Key: "localField", Value: "_id.r"},
			{Key: "foreignField", Value: "_id"},
			{Key: "pipeline", Value: bson.A{bson.D{{Key: "$project", Value: bson.D{{Key: "catalogKey", Value: 1}, {Key: "deletedAt", Value: 1}}}}}},
			{Key: "as", Value: "recipe"},
		}}},
		{{Key: "$unwind", Value: "$recipe"}},
		{{Key: "$match", Value: bson.D{
			{Key: "recipe.catalogKey", Value: bson.D{{Key: "$type", Value: "string"}, {Key: "$ne", Value: ""}}},
			{Key: "recipe.deletedAt", Value: bson.D{{Key: "$exists", Value: false}}},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$recipe.catalogKey"},
			{Key: "households", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "sum", Value: bson.D{{Key: "$sum", Value: "$score"}}},
		}}},
	}, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, fmt.Errorf("read catalog ratings: %w", err)
	}
	var rows []struct {
		Key        string  `bson:"_id"`
		Households int     `bson:"households"`
		Sum        float64 `bson:"sum"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("read catalog ratings: %w", err)
	}
	out := make(map[string]RatingStat, len(rows))
	for _, r := range rows {
		out[r.Key] = RatingStat{Households: r.Households, Sum: r.Sum}
	}
	return out, nil
}
