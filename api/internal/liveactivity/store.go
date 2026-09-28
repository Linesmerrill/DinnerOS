package liveactivity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
)

// Registration is the Live Activity attached to one import job.
type Registration struct {
	JobID       string
	HouseholdID string
	// Token is the activity's push token. It addresses one activity on one
	// phone; it is never logged and never returned by the API.
	Token       string
	Environment push.Environment
	// LastSent and LastSentAt are the last update pushed, for the Limiter.
	LastSent   ContentState
	LastSentAt time.Time
}

// Store keeps activity tokens on import jobs.
type Store interface {
	// Register attaches token to the household's job if it is still queued or
	// running, replacing any earlier token (a rotated one). ErrNotFound
	// otherwise.
	Register(ctx context.Context, householdID, source, jobID, token string, env push.Environment, at time.Time) error
	// Unregister removes the household's job's token. Idempotent.
	Unregister(ctx context.Context, householdID, source, jobID string) error
	// Get returns the registration on a job, or ErrNotFound when it has no
	// token.
	Get(ctx context.Context, jobID string) (Registration, error)
	// MarkSent records the update just pushed. It does nothing once the token
	// is gone.
	MarkSent(ctx context.Context, jobID string, state ContentState, at time.Time) error
	// Clear removes a job's token. The Pusher calls it when the job ends.
	Clear(ctx context.Context, jobID string) error
}

// MongoStore keeps the registration in a `liveActivity` subdocument of the
// job in meal_kit_jobs, which is what "the token lives on the job" means: it
// goes when the job is purged with its household, and the mealkit package's
// own documents never read it.
type MongoStore struct {
	jobs *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store over db's meal-kit job collection.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{jobs: db.Collection(mealkit.JobCollection)}
}

type registrationDoc struct {
	Token        string     `bson:"token"`
	Environment  string     `bson:"environment"`
	RegisteredAt time.Time  `bson:"registeredAt"`
	LastSentAt   *time.Time `bson:"lastSentAt,omitempty"`
	LastPhase    string     `bson:"lastPhase,omitempty"`
	LastDone     int        `bson:"lastDone,omitempty"`
	LastTotal    int        `bson:"lastTotal,omitempty"`
	LastFailed   int        `bson:"lastFailed,omitempty"`
}

type jobWithRegistration struct {
	ID           bson.ObjectID    `bson:"_id"`
	HouseholdID  bson.ObjectID    `bson:"householdId"`
	LiveActivity *registrationDoc `bson:"liveActivity"`
}

func activeJobFilter(householdID, source, jobID string) (bson.D, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	jid, err := mongodb.ParseID(jobID)
	if err != nil {
		return nil, ErrNotFound
	}
	return bson.D{
		{Key: "_id", Value: jid},
		{Key: "householdId", Value: hid},
		{Key: "source", Value: source},
	}, nil
}

// Register implements Store.
func (s *MongoStore) Register(ctx context.Context, householdID, source, jobID, token string, env push.Environment, at time.Time) error {
	filter, err := activeJobFilter(householdID, source, jobID)
	if err != nil {
		return err
	}
	filter = append(filter, bson.E{Key: "status", Value: bson.D{{Key: "$in", Value: bson.A{
		string(mealkit.JobQueued), string(mealkit.JobRunning),
	}}}})
	res, err := s.jobs.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.D{
		{Key: "liveActivity.token", Value: token},
		{Key: "liveActivity.environment", Value: string(env)},
		{Key: "liveActivity.registeredAt", Value: at},
	}}})
	if err != nil {
		return fmt.Errorf("register live activity: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// Unregister implements Store.
func (s *MongoStore) Unregister(ctx context.Context, householdID, source, jobID string) error {
	filter, err := activeJobFilter(householdID, source, jobID)
	if err != nil {
		return nil
	}
	if _, err := s.jobs.UpdateOne(ctx, filter, bson.D{{Key: "$unset", Value: bson.D{{Key: "liveActivity", Value: ""}}}}); err != nil {
		return fmt.Errorf("unregister live activity: %w", err)
	}
	return nil
}

// Get implements Store.
func (s *MongoStore) Get(ctx context.Context, jobID string) (Registration, error) {
	jid, err := mongodb.ParseID(jobID)
	if err != nil {
		return Registration{}, ErrNotFound
	}
	var d jobWithRegistration
	err = s.jobs.FindOne(ctx, bson.D{
		{Key: "_id", Value: jid},
		{Key: "liveActivity.token", Value: bson.D{{Key: "$exists", Value: true}}},
	}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Registration{}, ErrNotFound
	}
	if err != nil {
		return Registration{}, fmt.Errorf("get live activity: %w", err)
	}
	if d.LiveActivity == nil || d.LiveActivity.Token == "" {
		return Registration{}, ErrNotFound
	}
	r := Registration{
		JobID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(),
		Token: d.LiveActivity.Token, Environment: push.Environment(d.LiveActivity.Environment),
		LastSent: ContentState{
			Phase: Phase(d.LiveActivity.LastPhase), Done: d.LiveActivity.LastDone,
			Total: d.LiveActivity.LastTotal, Failed: d.LiveActivity.LastFailed,
		},
	}
	if d.LiveActivity.LastSentAt != nil {
		r.LastSentAt = d.LiveActivity.LastSentAt.UTC()
	}
	return r, nil
}

// MarkSent implements Store.
func (s *MongoStore) MarkSent(ctx context.Context, jobID string, state ContentState, at time.Time) error {
	jid, err := mongodb.ParseID(jobID)
	if err != nil {
		return nil
	}
	_, err = s.jobs.UpdateOne(ctx, bson.D{
		{Key: "_id", Value: jid},
		{Key: "liveActivity.token", Value: bson.D{{Key: "$exists", Value: true}}},
	}, bson.D{{Key: "$set", Value: bson.D{
		{Key: "liveActivity.lastSentAt", Value: at},
		{Key: "liveActivity.lastPhase", Value: string(state.Phase)},
		{Key: "liveActivity.lastDone", Value: state.Done},
		{Key: "liveActivity.lastTotal", Value: state.Total},
		{Key: "liveActivity.lastFailed", Value: state.Failed},
	}}})
	if err != nil {
		return fmt.Errorf("mark live activity sent: %w", err)
	}
	return nil
}

// Clear implements Store.
func (s *MongoStore) Clear(ctx context.Context, jobID string) error {
	jid, err := mongodb.ParseID(jobID)
	if err != nil {
		return nil
	}
	if _, err := s.jobs.UpdateOne(ctx, bson.D{{Key: "_id", Value: jid}},
		bson.D{{Key: "$unset", Value: bson.D{{Key: "liveActivity", Value: ""}}}}); err != nil {
		return fmt.Errorf("clear live activity: %w", err)
	}
	return nil
}
