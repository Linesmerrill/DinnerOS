package notifications

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

// Collection is the notifications collection.
const Collection = "notifications"

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{
			{
				// One notification per household per dedupe key.
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "dedupeKey", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("householdId_dedupeKey_unique"),
			},
			{
				// A household's list, newest first, and unread counts.
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "_id", Value: -1}},
				Options: options.Index().SetName("householdId_id"),
			},
			{
				// The push outbox: pending notifications, oldest first.
				Keys: bson.D{{Key: "push.status", Value: 1}, {Key: "createdAt", Value: 1}},
				Options: options.Index().
					SetPartialFilterExpression(bson.D{{Key: "push.status", Value: string(PushPending)}}).
					SetName("push_pending_createdAt"),
			},
		},
	}}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	notifications *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{notifications: db.Collection(Collection)}
}

type subjectDoc struct {
	Kind string `bson:"kind"`
	ID   string `bson:"id"`
}

type pushDoc struct {
	Status        string     `bson:"status"`
	Attempts      int        `bson:"attempts"`
	LastAttemptAt *time.Time `bson:"lastAttemptAt,omitempty"`
	SentAt        *time.Time `bson:"sentAt,omitempty"`
}

type notificationDoc struct {
	ID          bson.ObjectID   `bson:"_id"`
	HouseholdID bson.ObjectID   `bson:"householdId"`
	Type        string          `bson:"type"`
	Title       string          `bson:"title"`
	Body        string          `bson:"body"`
	Subject     subjectDoc      `bson:"subject"`
	DedupeKey   string          `bson:"dedupeKey"`
	ReadBy      []bson.ObjectID `bson:"readBy"`
	Push        pushDoc         `bson:"push"`
	CreatedAt   time.Time       `bson:"createdAt"`
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (d notificationDoc) toNotification() Notification {
	n := Notification{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), Type: Type(d.Type), Title: d.Title, Body: d.Body,
		Subject:   Subject{Kind: SubjectKind(d.Subject.Kind), ID: d.Subject.ID},
		DedupeKey: d.DedupeKey,
		Push:      Push{Status: PushStatus(d.Push.Status), Attempts: d.Push.Attempts},
		CreatedAt: d.CreatedAt.UTC(),
	}
	for _, id := range d.ReadBy {
		n.ReadBy = append(n.ReadBy, id.Hex())
	}
	if d.Push.LastAttemptAt != nil {
		n.Push.LastAttemptAt = d.Push.LastAttemptAt.UTC()
	}
	if d.Push.SentAt != nil {
		n.Push.SentAt = d.Push.SentAt.UTC()
	}
	return n
}

func householdOID(householdID string) (bson.ObjectID, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return bson.ObjectID{}, fmt.Errorf("notifications: household id: %w", err)
	}
	return hid, nil
}

// Insert implements Store.
func (s *MongoStore) Insert(ctx context.Context, n Notification) (Notification, error) {
	hid, err := householdOID(n.HouseholdID)
	if err != nil {
		return Notification{}, err
	}
	d := notificationDoc{
		ID: bson.NewObjectID(), HouseholdID: hid, Type: string(n.Type), Title: n.Title, Body: n.Body,
		Subject:   subjectDoc{Kind: string(n.Subject.Kind), ID: n.Subject.ID},
		DedupeKey: n.DedupeKey, ReadBy: []bson.ObjectID{},
		Push: pushDoc{
			Status: string(n.Push.Status), Attempts: n.Push.Attempts,
			LastAttemptAt: optionalTime(n.Push.LastAttemptAt), SentAt: optionalTime(n.Push.SentAt),
		},
		CreatedAt: n.CreatedAt,
	}
	for _, id := range n.ReadBy {
		oid, err := mongodb.ParseID(id)
		if err != nil {
			return Notification{}, fmt.Errorf("notifications: readBy: %w", err)
		}
		d.ReadBy = append(d.ReadBy, oid)
	}
	if _, err := s.notifications.InsertOne(ctx, d); err != nil {
		return Notification{}, translate(err)
	}
	return d.toNotification(), nil
}

// FindByDedupeKey implements Store.
func (s *MongoStore) FindByDedupeKey(ctx context.Context, householdID, key string) (Notification, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Notification{}, ErrNotFound
	}
	var d notificationDoc
	if err := s.notifications.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "dedupeKey", Value: key}}).Decode(&d); err != nil {
		return Notification{}, translate(err)
	}
	return d.toNotification(), nil
}

// List implements Store.
func (s *MongoStore) List(ctx context.Context, householdID string, f ListFilter) ([]Notification, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	filter := bson.D{{Key: "householdId", Value: hid}}
	if f.UnreadBy != "" {
		uid, err := mongodb.ParseID(f.UnreadBy)
		if err != nil {
			return nil, fmt.Errorf("notifications: user id: %w", err)
		}
		filter = append(filter, bson.E{Key: "readBy", Value: bson.D{{Key: "$ne", Value: uid}}})
	}
	if f.Before != "" {
		before, err := mongodb.ParseID(f.Before)
		if err != nil {
			return nil, nil
		}
		filter = append(filter, bson.E{Key: "_id", Value: bson.D{{Key: "$lt", Value: before}}})
	}
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	cur, err := s.notifications.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, translate(err)
	}
	var docs []notificationDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Notification, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toNotification())
	}
	return out, nil
}

// CountUnread implements Store.
func (s *MongoStore) CountUnread(ctx context.Context, householdID, userID string) (int, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return 0, err
	}
	uid, err := mongodb.ParseID(userID)
	if err != nil {
		return 0, fmt.Errorf("notifications: user id: %w", err)
	}
	n, err := s.notifications.CountDocuments(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "readBy", Value: bson.D{{Key: "$ne", Value: uid}}}})
	return int(n), translate(err)
}

// MarkRead implements Store with one updateMany.
func (s *MongoStore) MarkRead(ctx context.Context, householdID, userID string, ids []string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	uid, err := mongodb.ParseID(userID)
	if err != nil {
		return fmt.Errorf("notifications: user id: %w", err)
	}
	filter := bson.D{{Key: "householdId", Value: hid}}
	if ids != nil {
		oids := make([]bson.ObjectID, 0, len(ids))
		for _, id := range ids {
			if oid, err := mongodb.ParseID(id); err == nil {
				oids = append(oids, oid)
			}
		}
		if len(oids) == 0 {
			return nil
		}
		filter = append(filter, bson.E{Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}})
	}
	_, err = s.notifications.UpdateMany(ctx, filter, bson.D{{Key: "$addToSet", Value: bson.D{{Key: "readBy", Value: uid}}}})
	return translate(err)
}

var _ Outbox = (*MongoStore)(nil)

// PendingPush implements Outbox using the push_pending_createdAt index.
func (s *MongoStore) PendingPush(ctx context.Context, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	cur, err := s.notifications.Find(ctx, bson.D{{Key: "push.status", Value: string(PushPending)}},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, translate(err)
	}
	var docs []notificationDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Notification, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toNotification())
	}
	return out, nil
}

// ClaimPush implements Outbox with one conditional update.
func (s *MongoStore) ClaimPush(ctx context.Context, id string, at time.Time) (bool, error) {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return false, nil
	}
	res, err := s.notifications.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: oid}, {Key: "push.status", Value: string(PushPending)}},
		bson.D{
			{Key: "$set", Value: bson.D{{Key: "push.status", Value: string(PushSending)}, {Key: "push.lastAttemptAt", Value: at}}},
			{Key: "$inc", Value: bson.D{{Key: "push.attempts", Value: 1}}},
		})
	if err != nil {
		return false, translate(err)
	}
	return res.ModifiedCount == 1, nil
}

// FinishPush implements Outbox.
func (s *MongoStore) FinishPush(ctx context.Context, id string, status PushStatus, at time.Time) error {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	set := bson.D{{Key: "push.status", Value: string(status)}}
	if status == PushSent {
		set = append(set, bson.E{Key: "push.sentAt", Value: at})
	}
	res, err := s.notifications.UpdateOne(ctx, bson.D{{Key: "_id", Value: oid}}, bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
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
