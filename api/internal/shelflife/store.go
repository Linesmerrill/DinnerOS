package shelflife

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Collections.
const (
	EntriesCollection = "shelf_life_entries"
	MissesCollection  = "shelf_life_misses"
)

// Indexes returns the indexes the store relies on: one miss row per name and
// storage.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: MissesCollection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "key", Value: 1}, {Key: "storage", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("key_storage_unique"),
		}, {
			Keys:    bson.D{{Key: "count", Value: -1}},
			Options: options.Index().SetName("count_desc"),
		}},
	}, {
		Collection: EntriesCollection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("id_unique"),
		}},
	}}
}

// MongoStore keeps added entries and misses in MongoDB. Neither holds anything
// about a household: a miss is a food name, counted.
type MongoStore struct {
	entries *mongo.Collection
	misses  *mongo.Collection
}

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{entries: db.Collection(EntriesCollection), misses: db.Collection(MissesCollection)}
}

// Entries returns the entries added after the seed.
func (s *MongoStore) Entries(ctx context.Context) ([]Entry, error) {
	cur, err := s.entries.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("find shelf-life entries: %w", err)
	}
	var out []Entry
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode shelf-life entries: %w", err)
	}
	return out, nil
}

// Miss is a food name the library didn't know, and how often it was asked.
type Miss struct {
	Key       string    `bson:"key"`
	Name      string    `bson:"name"`
	Storage   Storage   `bson:"storage"`
	Count     int       `bson:"count"`
	FirstSeen time.Time `bson:"firstSeen"`
	LastSeen  time.Time `bson:"lastSeen"`
}

// RecordMiss counts a lookup the library couldn't answer.
func (s *MongoStore) RecordMiss(ctx context.Context, name string, storage Storage, at time.Time) error {
	key := strings.Join(words(name), " ")
	if key == "" {
		return nil
	}
	_, err := s.misses.UpdateOne(ctx,
		bson.M{"key": key, "storage": storage},
		bson.M{
			"$inc":         bson.M{"count": 1},
			"$set":         bson.M{"name": strings.TrimSpace(name), "lastSeen": at.UTC()},
			"$setOnInsert": bson.M{"firstSeen": at.UTC()},
		},
		options.UpdateOne().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("record shelf-life miss: %w", err)
	}
	return nil
}

// Misses returns the most asked-for names the library doesn't know, for
// filling in.
func (s *MongoStore) Misses(ctx context.Context, limit int64) ([]Miss, error) {
	cur, err := s.misses.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "count", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("find shelf-life misses: %w", err)
	}
	var out []Miss
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode shelf-life misses: %w", err)
	}
	return out, nil
}
