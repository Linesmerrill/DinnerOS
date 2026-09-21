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

// Collections holding meal-kit links and the import job queue.
const (
	LinkCollection = "meal_kit_links"
	JobCollection  = "meal_kit_jobs"
)

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{
			Collection: LinkCollection,
			Indexes: []mongo.IndexModel{
				{
					// One link per household per meal-kit service.
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "source", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("householdId_source_unique"),
				},
				{
					// Account deletion removes the links a user made.
					Keys:    bson.D{{Key: "userId", Value: 1}},
					Options: options.Index().SetName("userId"),
				},
			},
		},
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
	links *mongo.Collection
	jobs  *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{links: db.Collection(LinkCollection), jobs: db.Collection(JobCollection)}
}

// --- Documents ----------------------------------------------------------------

// envelopeDoc is the stored envelope. Neither field is usable without the
// configured RECIPE_IMPORT_ENCRYPTION_KEY.
type envelopeDoc struct {
	KeyID      string `bson:"keyId"`
	Key        []byte `bson:"key"`
	Ciphertext []byte `bson:"ciphertext"`
}

type linkDoc struct {
	ID           bson.ObjectID `bson:"_id"`
	HouseholdID  bson.ObjectID `bson:"householdId"`
	UserID       bson.ObjectID `bson:"userId"`
	Source       string        `bson:"source"`
	Status       string        `bson:"status"`
	AccountLabel string        `bson:"accountLabel"`
	Secret       envelopeDoc   `bson:"secret"`
	ExpiresAt    *time.Time    `bson:"expiresAt,omitempty"`
	CreatedAt    time.Time     `bson:"createdAt"`
	UpdatedAt    time.Time     `bson:"updatedAt"`
	LastUsedAt   *time.Time    `bson:"lastUsedAt,omitempty"`
}

func (d linkDoc) toLink() Link {
	l := Link{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), UserID: d.UserID.Hex(),
		Source: d.Source, Status: LinkStatus(d.Status), AccountLabel: d.AccountLabel,
		Secret:    Envelope(d.Secret),
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	if d.ExpiresAt != nil {
		l.ExpiresAt = d.ExpiresAt.UTC()
	}
	if d.LastUsedAt != nil {
		l.LastUsedAt = d.LastUsedAt.UTC()
	}
	return l
}

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
	LinkID         bson.ObjectID `bson:"linkId"`
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
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), UserID: d.UserID.Hex(), LinkID: d.LinkID.Hex(),
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
		d.Phase = string(PhaseOrders)
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
		c.Phase = PhaseOrders
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

// --- Links --------------------------------------------------------------------

// UpsertLink implements Store.
func (s *MongoStore) UpsertLink(ctx context.Context, l Link) (Link, error) {
	hid, err := oid("household id", l.HouseholdID)
	if err != nil {
		return Link{}, err
	}
	uid, err := oid("user id", l.UserID)
	if err != nil {
		return Link{}, err
	}
	set := bson.D{
		{Key: "userId", Value: uid},
		{Key: "status", Value: string(l.Status)},
		{Key: "accountLabel", Value: l.AccountLabel},
		{Key: "secret", Value: envelopeDoc(l.Secret)},
		{Key: "updatedAt", Value: l.UpdatedAt},
	}
	unset := bson.D{}
	if l.ExpiresAt.IsZero() {
		unset = append(unset, bson.E{Key: "expiresAt", Value: ""})
	} else {
		set = append(set, bson.E{Key: "expiresAt", Value: l.ExpiresAt})
	}
	update := bson.D{
		{Key: "$set", Value: set},
		{Key: "$setOnInsert", Value: bson.D{{Key: "createdAt", Value: l.CreatedAt}}},
	}
	if len(unset) > 0 {
		update = append(update, bson.E{Key: "$unset", Value: unset})
	}
	var d linkDoc
	err = s.links.FindOneAndUpdate(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "source", Value: l.Source}},
		update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&d)
	if err != nil {
		return Link{}, translate(err)
	}
	return d.toLink(), nil
}

// GetLink implements Store.
func (s *MongoStore) GetLink(ctx context.Context, householdID, source string) (Link, error) {
	hid, err := oid("household id", householdID)
	if err != nil {
		return Link{}, ErrNotFound
	}
	var d linkDoc
	if err := s.links.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "source", Value: source}}).Decode(&d); err != nil {
		return Link{}, translate(err)
	}
	return d.toLink(), nil
}

// GetLinkByID implements Store.
func (s *MongoStore) GetLinkByID(ctx context.Context, id string) (Link, error) {
	lid, err := mongodb.ParseID(id)
	if err != nil {
		return Link{}, ErrNotFound
	}
	var d linkDoc
	if err := s.links.FindOne(ctx, bson.D{{Key: "_id", Value: lid}}).Decode(&d); err != nil {
		return Link{}, translate(err)
	}
	return d.toLink(), nil
}

// SetLinkStatus implements Store.
func (s *MongoStore) SetLinkStatus(ctx context.Context, id string, status LinkStatus, at time.Time) error {
	lid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	res, err := s.links.UpdateOne(ctx, bson.D{{Key: "_id", Value: lid}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "status", Value: string(status)}, {Key: "updatedAt", Value: at}, {Key: "lastUsedAt", Value: at},
		}}})
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveLinkTokens implements Store.
func (s *MongoStore) SaveLinkTokens(ctx context.Context, id string, secret Envelope, expiresAt, at time.Time) error {
	lid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	set := bson.D{
		{Key: "secret", Value: envelopeDoc(secret)},
		{Key: "status", Value: string(LinkActive)},
		{Key: "updatedAt", Value: at},
		{Key: "lastUsedAt", Value: at},
	}
	var update bson.D
	if expiresAt.IsZero() {
		update = bson.D{
			{Key: "$set", Value: set},
			{Key: "$unset", Value: bson.D{{Key: "expiresAt", Value: ""}}},
		}
	} else {
		set = append(set, bson.E{Key: "expiresAt", Value: expiresAt})
		update = bson.D{{Key: "$set", Value: set}}
	}
	res, err := s.links.UpdateOne(ctx, bson.D{{Key: "_id", Value: lid}}, update)
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLink implements Store.
func (s *MongoStore) DeleteLink(ctx context.Context, householdID, source string) error {
	hid, err := oid("household id", householdID)
	if err != nil {
		return nil
	}
	_, err = s.links.DeleteOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "source", Value: source}})
	return translate(err)
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
	lid, err := oid("link id", j.LinkID)
	if err != nil {
		return Job{}, err
	}
	d := jobDoc{
		ID: bson.NewObjectID(), HouseholdID: hid, UserID: uid, LinkID: lid, Source: j.Source,
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
	return []string{string(JobQueued), string(JobRunning), string(JobPausedAuth)}
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

// ResumePausedJobs implements Store.
func (s *MongoStore) ResumePausedJobs(ctx context.Context, householdID, source string, at time.Time) (int, error) {
	hid, err := oid("household id", householdID)
	if err != nil {
		return 0, nil
	}
	res, err := s.jobs.UpdateMany(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "source", Value: source}, {Key: "status", Value: string(JobPausedAuth)}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "status", Value: string(JobQueued)},
			{Key: "availableAt", Value: at},
			{Key: "updatedAt", Value: at},
		}}})
	if err != nil {
		return 0, translate(err)
	}
	return int(res.ModifiedCount), nil
}

// PurgeHousehold deletes every document this package stores for the
// household. Account deletion calls it when the household's last member
// deletes their account. It is idempotent.
func (s *MongoStore) PurgeHousehold(ctx context.Context, householdID string) error {
	return mongodb.DeleteByID(ctx, "householdId", householdID, s.links, s.jobs)
}

// PurgeUser deletes the meal-kit links a user made, so their stored tokens go
// with their account. Jobs stay with the household.
func (s *MongoStore) PurgeUser(ctx context.Context, userID string) error {
	return mongodb.DeleteByID(ctx, "userId", userID, s.links)
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
