package events

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

// Collection is the events collection.
const Collection = "events"

// duplicateKeyCode is MongoDB's E11000 duplicate key error code.
const duplicateKeyCode = 11000

// Indexes returns the indexes this package's MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{
			{
				// A household's history in time order (Autopilot feature windows).
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "occurredAt", Value: 1}},
				Options: options.Index().SetName("householdId_occurredAt"),
			},
			{
				// One recipe's events of a type ("when did we last cook this?").
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "recipeId", Value: 1}, {Key: "type", Value: 1}, {Key: "occurredAt", Value: 1}},
				Options: options.Index().SetName("householdId_recipeId_type_occurredAt"),
			},
			{
				// Client retries of the same event are stored once.
				Keys: bson.D{{Key: "householdId", Value: 1}, {Key: "userId", Value: 1}, {Key: "clientEventId", Value: 1}},
				Options: options.Index().
					SetUnique(true).
					SetPartialFilterExpression(bson.D{{Key: "clientEventId", Value: bson.D{{Key: "$type", Value: "string"}}}}).
					SetName("householdId_userId_clientEventId_unique"),
			},
		},
	}}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	events *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{events: db.Collection(Collection)}
}

type eventDoc struct {
	ID            bson.ObjectID `bson:"_id"`
	HouseholdID   bson.ObjectID `bson:"householdId"`
	UserID        bson.ObjectID `bson:"userId,omitempty"`
	Type          string        `bson:"type"`
	RecipeID      bson.ObjectID `bson:"recipeId,omitempty"`
	Week          string        `bson:"week,omitempty"`
	Payload       any           `bson:"payload"`
	ClientEventID string        `bson:"clientEventId,omitempty"`
	OccurredAt    time.Time     `bson:"occurredAt"`
	RecordedAt    time.Time     `bson:"recordedAt"`
	Source        string        `bson:"source"`
}

// storedEventDoc is eventDoc as read back, with the payload left raw until the
// type is known.
type storedEventDoc struct {
	ID            bson.ObjectID `bson:"_id"`
	HouseholdID   bson.ObjectID `bson:"householdId"`
	UserID        bson.ObjectID `bson:"userId"`
	Type          string        `bson:"type"`
	RecipeID      bson.ObjectID `bson:"recipeId"`
	Week          string        `bson:"week"`
	Payload       bson.Raw      `bson:"payload"`
	ClientEventID string        `bson:"clientEventId"`
	OccurredAt    time.Time     `bson:"occurredAt"`
	RecordedAt    time.Time     `bson:"recordedAt"`
	Source        string        `bson:"source"`
}

func newEventDoc(e Event) (eventDoc, error) {
	d := eventDoc{
		ID: bson.NewObjectID(), Type: string(e.Type), Week: e.Week, Payload: e.Payload,
		ClientEventID: e.ClientEventID, OccurredAt: e.OccurredAt, RecordedAt: e.RecordedAt, Source: string(e.Source),
	}
	var err error
	if d.HouseholdID, err = mongodb.ParseID(e.HouseholdID); err != nil {
		return eventDoc{}, invalid("householdId is malformed")
	}
	if e.UserID != "" {
		if d.UserID, err = mongodb.ParseID(e.UserID); err != nil {
			return eventDoc{}, invalid("userId is malformed")
		}
	}
	if e.RecipeID != "" {
		if d.RecipeID, err = mongodb.ParseID(e.RecipeID); err != nil {
			return eventDoc{}, invalid("recipeId is malformed")
		}
	}
	return d, nil
}

func (d storedEventDoc) toEvent() (Event, error) {
	e := Event{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), Type: Type(d.Type), Week: d.Week,
		ClientEventID: d.ClientEventID, OccurredAt: d.OccurredAt.UTC(), RecordedAt: d.RecordedAt.UTC(), Source: Source(d.Source),
	}
	if !d.UserID.IsZero() {
		e.UserID = d.UserID.Hex()
	}
	if !d.RecipeID.IsZero() {
		e.RecipeID = d.RecipeID.Hex()
	}
	// Events of types this build doesn't know keep a nil payload rather than
	// failing the whole read.
	if spec, ok := typeSpecs[e.Type]; ok {
		p, err := spec.decode(d.Payload, bson.Unmarshal)
		if err != nil {
			return Event{}, fmt.Errorf("events: decode %s payload of %s: %w", e.Type, e.ID, err)
		}
		e.Payload = p
	}
	return e, nil
}

// Insert implements Store with one unordered InsertMany. Duplicate client
// event IDs fail individually, and the other documents are still inserted.
func (s *MongoStore) Insert(ctx context.Context, list []Event) (inserted, duplicates int, err error) {
	if len(list) == 0 {
		return 0, 0, nil
	}
	docs := make([]any, 0, len(list))
	for _, e := range list {
		d, err := newEventDoc(e)
		if err != nil {
			return 0, 0, err
		}
		docs = append(docs, d)
	}
	_, err = s.events.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	if err == nil {
		return len(docs), 0, nil
	}
	var bwe mongo.BulkWriteException
	if !errors.As(err, &bwe) {
		return 0, 0, err
	}
	for _, we := range bwe.WriteErrors {
		if we.Code == duplicateKeyCode {
			duplicates++
		}
	}
	inserted = len(docs) - len(bwe.WriteErrors)
	if duplicates == len(bwe.WriteErrors) && bwe.WriteConcernError == nil {
		return inserted, duplicates, nil
	}
	return inserted, duplicates, err
}

// List implements Store.
func (s *MongoStore) List(ctx context.Context, q Query) ([]Event, error) {
	hid, err := mongodb.ParseID(q.HouseholdID)
	if err != nil {
		return nil, invalid("householdId is malformed")
	}
	filter := bson.D{{Key: "householdId", Value: hid}}
	if q.RecipeID != "" {
		rid, err := mongodb.ParseID(q.RecipeID)
		if err != nil {
			return nil, nil
		}
		filter = append(filter, bson.E{Key: "recipeId", Value: rid})
	}
	if len(q.Types) > 0 {
		types := make([]string, 0, len(q.Types))
		for _, t := range q.Types {
			types = append(types, string(t))
		}
		filter = append(filter, bson.E{Key: "type", Value: bson.D{{Key: "$in", Value: types}}})
	}
	if !q.Since.IsZero() {
		filter = append(filter, bson.E{Key: "occurredAt", Value: bson.D{{Key: "$gte", Value: q.Since}}})
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	dir := 1
	if q.Newest {
		dir = -1
	}
	cur, err := s.events.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "occurredAt", Value: dir}, {Key: "_id", Value: dir}}).
		SetLimit(int64(limit)))
	if err != nil {
		return nil, mongodb.TranslateError(err)
	}
	var docs []storedEventDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, mongodb.TranslateError(err)
	}
	out := make([]Event, 0, len(docs))
	for _, d := range docs {
		e, err := d.toEvent()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
