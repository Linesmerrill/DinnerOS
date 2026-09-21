package mealkit

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

// JobCollection holds the import job queue. It is the only collection this
// package has: nothing about a meal-kit account is stored anywhere.
const JobCollection = "meal_kit_jobs"

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{
			Collection: JobCollection,
			Indexes: []mongo.IndexModel{
				{
					// A household's history, newest first, and its active job.
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "_id", Value: -1}},
					Options: options.Index().SetName("householdId_id"),
				},
				{
					// The claim: runnable jobs, oldest due first. It covers
					// both halves of the claim filter (queued and due, or
					// running with an expired lease).
					Keys:    bson.D{{Key: "status", Value: 1}, {Key: "availableAt", Value: 1}},
					Options: options.Index().SetName("status_availableAt"),
				},
			},
		},
	}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	jobs *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{jobs: db.Collection(JobCollection)}
}

// --- Documents ----------------------------------------------------------------

type orderedRecipeDoc struct {
	SourceRecipeID string   `bson:"sourceRecipeId"`
	Name           string   `bson:"name"`
	URL            string   `bson:"url"`
	Weeks          []string `bson:"weeks"`
	IsAddon        bool     `bson:"isAddon"`
}

type failedRecipeDoc struct {
	SourceRecipeID string `bson:"sourceRecipeId"`
	Name           string `bson:"name"`
	Reason         string `bson:"reason"`
}

type checkpointDoc struct {
	Phase       string             `bson:"phase"`
	Orders      []orderedRecipeDoc `bson:"orders"`
	Done        []string           `bson:"done"`
	Failures    []failedRecipeDoc  `bson:"failures"`
	Imported    int                `bson:"imported"`
	Updated     int                `bson:"updated"`
	Unchanged   int                `bson:"unchanged"`
	ReviewItems int                `bson:"reviewItems"`
}

type jobErrorDoc struct {
	Code    string    `bson:"code"`
	Message string    `bson:"message"`
	At      time.Time `bson:"at"`
}

type jobDoc struct {
	ID             bson.ObjectID `bson:"_id"`
	HouseholdID    bson.ObjectID `bson:"householdId"`
	UserID         bson.ObjectID `bson:"userId"`
	Source         string        `bson:"source"`
	Status         string        `bson:"status"`
	Attempts       int           `bson:"attempts"`
	MaxAttempts    int           `bson:"maxAttempts"`
	AvailableAt    time.Time     `bson:"availableAt"`
	LeaseOwner     string        `bson:"leaseOwner,omitempty"`
	LeaseExpiresAt *time.Time    `bson:"leaseExpiresAt,omitempty"`
	Checkpoint     checkpointDoc `bson:"checkpoint"`
	LastError      *jobErrorDoc  `bson:"lastError,omitempty"`
	CreatedAt      time.Time     `bson:"createdAt"`
	UpdatedAt      time.Time     `bson:"updatedAt"`
	StartedAt      *time.Time    `bson:"startedAt,omitempty"`
	FinishedAt     *time.Time    `bson:"finishedAt,omitempty"`
}

func (d jobDoc) toJob() Job {
	j := Job{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), UserID: d.UserID.Hex(),
		Source: d.Source, Status: JobStatus(d.Status), Attempts: d.Attempts, MaxAttempts: d.MaxAttempts,
		AvailableAt: d.AvailableAt.UTC(), LeaseOwner: d.LeaseOwner,
		Checkpoint: fromCheckpointDoc(d.Checkpoint),
		CreatedAt:  d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	if d.LeaseExpiresAt != nil {
		j.LeaseExpiresAt = d.LeaseExpiresAt.UTC()
	}
	if d.StartedAt != nil {
		j.StartedAt = d.StartedAt.UTC()
	}
	if d.FinishedAt != nil {
		j.FinishedAt = d.FinishedAt.UTC()
	}
	if d.LastError != nil {
		j.LastError = &JobError{Code: d.LastError.Code, Message: d.LastError.Message, At: d.LastError.At.UTC()}
	}
	return j
}

func toCheckpointDoc(c Checkpoint) checkpointDoc {
	d := checkpointDoc{
		Phase: string(c.Phase), Done: c.Done, Imported: c.Imported, Updated: c.Updated,
		Unchanged: c.Unchanged, ReviewItems: c.ReviewItems,
	}
	if d.Phase == "" {
		d.Phase = string(PhaseRecipes)
	}
	for _, o := range c.Orders {
		d.Orders = append(d.Orders, orderedRecipeDoc(o))
	}
	for _, f := range c.Failures {
		d.Failures = append(d.Failures, failedRecipeDoc(f))
	}
	return d
}

func fromCheckpointDoc(d checkpointDoc) Checkpoint {
	c := Checkpoint{
		Phase: Phase(d.Phase), Done: d.Done, Imported: d.Imported, Updated: d.Updated,
		Unchanged: d.Unchanged, ReviewItems: d.ReviewItems,
	}
	if c.Phase == "" {
		c.Phase = PhaseRecipes
	}
	for _, o := range d.Orders {
		c.Orders = append(c.Orders, OrderedRecipe(o))
	}
	for _, f := range d.Failures {
		c.Failures = append(c.Failures, FailedRecipe(f))
	}
	return c
}

func errorDoc(e *JobError) *jobErrorDoc {
	if e == nil {
		return nil
	}
	return &jobErrorDoc{Code: e.Code, Message: e.Message, At: e.At}
}

// --- Jobs ---------------------------------------------------------------------

// InsertJob implements Store.
func (s *MongoStore) InsertJob(ctx context.Context, j Job) (Job, error) {
	hid, err := oid("household id", j.HouseholdID)
	if err != nil {
		return Job{}, err
	}
	uid, err := oid("user id", j.UserID)
	if err != nil {
		return Job{}, err
	}
	d := jobDoc{
		ID: bson.NewObjectID(), HouseholdID: hid, UserID: uid, Source: j.Source,
		Status: string(j.Status), Attempts: j.Attempts, MaxAttempts: j.MaxAttempts,
		AvailableAt: j.AvailableAt, Checkpoint: toCheckpointDoc(j.Checkpoint),
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	if _, err := s.jobs.InsertOne(ctx, d); err != nil {
		return Job{}, translate(err)
	}
	return d.toJob(), nil
}

// GetJob implements Store.
func (s *MongoStore) GetJob(ctx context.Context, householdID, id string) (Job, error) {
	hid, err := oid("household id", householdID)
	if err != nil {
		return Job{}, ErrNotFound
	}
	jid, err := mongodb.ParseID(id)
	if err != nil {
		return Job{}, ErrNotFound
	}
	var d jobDoc
	if err := s.jobs.FindOne(ctx, bson.D{{Key: "_id", Value: jid}, {Key: "householdId", Value: hid}}).Decode(&d); err != nil {
		return Job{}, translate(err)
	}
	return d.toJob(), nil
}

// LatestJob implements Store.
func (s *MongoStore) LatestJob(ctx context.Context, householdID, source string) (Job, error) {
	return s.findOneJob(ctx, householdID, source)
}

// ActiveJob implements Store.
func (s *MongoStore) ActiveJob(ctx context.Context, householdID, source string) (Job, error) {
	return s.findOneJob(ctx, householdID, source,
		bson.E{Key: "status", Value: bson.D{{Key: "$in", Value: activeStatuses()}}})
}

func activeStatuses() []string {
	return []string{string(JobQueued), string(JobRunning)}
}

func (s *MongoStore) findOneJob(ctx context.Context, householdID, source string, extra ...bson.E) (Job, error) {
	hid, err := oid("household id", householdID)
	if err != nil {
		return Job{}, ErrNotFound
	}
	filter := bson.D{{Key: "householdId", Value: hid}, {Key: "source", Value: source}}
	for _, e := range extra {
		if e.Key != "" {
			filter = append(filter, e)
		}
	}
	var d jobDoc
	err = s.jobs.FindOne(ctx, filter, options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})).Decode(&d)
	if err != nil {
		return Job{}, translate(err)
	}
	return d.toJob(), nil
}

// ListJobs implements Store.
func (s *MongoStore) ListJobs(ctx context.Context, householdID string, limit int) ([]Job, error) {
	hid, err := oid("household id", householdID)
	if err != nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	cur, err := s.jobs.Find(ctx, bson.D{{Key: "householdId", Value: hid}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, translate(err)
	}
	var docs []jobDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Job, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toJob())
	}
	return out, nil
}

// ClaimJob implements Store with one find-and-modify.
func (s *MongoStore) ClaimJob(ctx context.Context, owner string, now, leaseUntil time.Time) (Job, error) {
	filter := bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "status", Value: string(JobQueued)}, {Key: "availableAt", Value: bson.D{{Key: "$lte", Value: now}}}},
		// A worker that died mid-run left the lease behind; taking it over
		// resumes from the checkpoint rather than starting again.
		bson.D{{Key: "status", Value: string(JobRunning)}, {Key: "leaseExpiresAt", Value: bson.D{{Key: "$lte", Value: now}}}},
	}}}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "status", Value: string(JobRunning)},
			{Key: "leaseOwner", Value: owner},
			{Key: "leaseExpiresAt", Value: leaseUntil},
			{Key: "startedAt", Value: now},
			{Key: "updatedAt", Value: now},
		}},
		{Key: "$inc", Value: bson.D{{Key: "attempts", Value: 1}}},
	}
	var d jobDoc
	err := s.jobs.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetSort(bson.D{{Key: "availableAt", Value: 1}}).SetReturnDocument(options.After),
	).Decode(&d)
	if err != nil {
		return Job{}, translate(err)
	}
	return d.toJob(), nil
}

// claimed matches a job still running under owner. Every write a worker makes
// carries it, so an unlink (which cancels) or a lease takeover stops it.
func claimed(id bson.ObjectID, owner string) bson.D {
	return bson.D{
		{Key: "_id", Value: id},
		{Key: "status", Value: string(JobRunning)},
		{Key: "leaseOwner", Value: owner},
	}
}

func (s *MongoStore) updateClaimed(ctx context.Context, id, owner string, update bson.D) error {
	jid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrJobGone
	}
	res, err := s.jobs.UpdateOne(ctx, claimed(jid, owner), update)
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrJobGone
	}
	return nil
}

// ExtendLease implements Store.
func (s *MongoStore) ExtendLease(ctx context.Context, id, owner string, leaseUntil, at time.Time) error {
	return s.updateClaimed(ctx, id, owner, bson.D{{Key: "$set", Value: bson.D{
		{Key: "leaseExpiresAt", Value: leaseUntil}, {Key: "updatedAt", Value: at},
	}}})
}

// SaveCheckpoint implements Store.
func (s *MongoStore) SaveCheckpoint(ctx context.Context, id, owner string, c Checkpoint, at time.Time) error {
	return s.updateClaimed(ctx, id, owner, bson.D{{Key: "$set", Value: bson.D{
		{Key: "checkpoint", Value: toCheckpointDoc(c)}, {Key: "updatedAt", Value: at},
	}}})
}

// FinishJob implements Store.
func (s *MongoStore) FinishJob(ctx context.Context, id, owner string, status JobStatus, checkpoint Checkpoint, jobErr *JobError, at time.Time) error {
	set := bson.D{
		{Key: "status", Value: string(status)},
		{Key: "checkpoint", Value: toCheckpointDoc(checkpoint)},
		{Key: "updatedAt", Value: at},
	}
	if status.Terminal() {
		set = append(set, bson.E{Key: "finishedAt", Value: at})
	}
	if jobErr != nil {
		set = append(set, bson.E{Key: "lastError", Value: errorDoc(jobErr)})
	}
	return s.updateClaimed(ctx, id, owner, bson.D{
		{Key: "$set", Value: set},
		{Key: "$unset", Value: bson.D{{Key: "leaseOwner", Value: ""}, {Key: "leaseExpiresAt", Value: ""}}},
	})
}

// RequeueJob implements Store.
func (s *MongoStore) RequeueJob(ctx context.Context, id, owner string, availableAt time.Time, countAttempt bool, jobErr *JobError, at time.Time) error {
	set := bson.D{
		{Key: "status", Value: string(JobQueued)},
		{Key: "availableAt", Value: availableAt},
		{Key: "updatedAt", Value: at},
	}
	if jobErr != nil {
		set = append(set, bson.E{Key: "lastError", Value: errorDoc(jobErr)})
	}
	update := bson.D{{Key: "$set", Value: set}, {Key: "$unset", Value: bson.D{
		{Key: "leaseOwner", Value: ""}, {Key: "leaseExpiresAt", Value: ""},
	}}}
	if !countAttempt {
		// The run stopped on its own per-run cap, so it does not count
		// against MaxAttempts: a long history would otherwise dead-letter
		// itself just for being long.
		update = append(update, bson.E{Key: "$inc", Value: bson.D{{Key: "attempts", Value: -1}}})
	}
	return s.updateClaimed(ctx, id, owner, update)
}

// CancelJobs implements Store.
func (s *MongoStore) CancelJobs(ctx context.Context, householdID, source, reason string, at time.Time) (int, error) {
	hid, err := oid("household id", householdID)
	if err != nil {
		return 0, nil
	}
	filter := bson.D{
		{Key: "householdId", Value: hid},
		{Key: "source", Value: source},
		{Key: "status", Value: bson.D{{Key: "$in", Value: activeStatuses()}}},
	}
	res, err := s.jobs.UpdateMany(ctx, filter, bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "status", Value: string(JobCanceled)},
			{Key: "finishedAt", Value: at},
			{Key: "updatedAt", Value: at},
			{Key: "lastError", Value: errorDoc(&JobError{Code: ErrCodeInternal, Message: reason, At: at})},
		}},
		{Key: "$unset", Value: bson.D{{Key: "leaseOwner", Value: ""}, {Key: "leaseExpiresAt", Value: ""}}},
	})
	if err != nil {
		return 0, translate(err)
	}
	return int(res.ModifiedCount), nil
}

// PurgeHousehold deletes every document this package stores for the
// household. Account deletion calls it when the household's last member
// deletes their account. It is idempotent.
//
// There is no PurgeUser any more: this package stores nothing that belongs to
// one member. It used to hold their meal-kit tokens; it holds no credential at
// all now, and an import run belongs to the household that asked for it.
func (s *MongoStore) PurgeHousehold(ctx context.Context, householdID string) error {
	return mongodb.DeleteByID(ctx, "householdId", householdID, s.jobs)
}

func oid(what, id string) (bson.ObjectID, error) {
	parsed, err := mongodb.ParseID(id)
	if err != nil {
		return bson.ObjectID{}, fmt.Errorf("mealkit: %s: %w", what, err)
	}
	return parsed, nil
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
