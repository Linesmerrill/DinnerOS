package plus

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

// Collection holds one Plus entitlement per household.
const Collection = "plus_entitlements"

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "householdId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_unique"),
		}},
	}}
}

// MongoStore is the MongoDB Store.
type MongoStore struct {
	coll *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{coll: db.Collection(Collection)}
}

type entitlementDoc struct {
	HouseholdID  bson.ObjectID `bson:"householdId"`
	ProductID    string        `bson:"productId,omitempty"`
	Trial        bool          `bson:"trial"`
	ExpiresAt    *time.Time    `bson:"expiresAt,omitempty"`
	WillRenew    bool          `bson:"willRenew"`
	BillingIssue bool          `bson:"billingIssue"`
	Ended        bool          `bson:"ended"`
	Environment  string        `bson:"environment,omitempty"`
	LastEventID  string        `bson:"lastEventId"`
	LastEventAt  time.Time     `bson:"lastEventAt"`
	UpdatedAt    time.Time     `bson:"updatedAt"`
}

// Get implements Store.
func (s *MongoStore) Get(ctx context.Context, householdID string) (Entitlement, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Entitlement{}, ErrNotFound
	}
	var d entitlementDoc
	err = s.coll.FindOne(ctx, bson.D{{Key: "householdId", Value: oid}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Entitlement{}, ErrNotFound
	}
	if err != nil {
		return Entitlement{}, fmt.Errorf("get plus entitlement: %w", err)
	}
	e := Entitlement{
		HouseholdID: d.HouseholdID.Hex(), ProductID: d.ProductID, Trial: d.Trial, WillRenew: d.WillRenew,
		BillingIssue: d.BillingIssue, Ended: d.Ended, Environment: d.Environment,
		LastEventID: d.LastEventID, LastEventAt: d.LastEventAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	if d.ExpiresAt != nil {
		e.ExpiresAt = d.ExpiresAt.UTC()
	}
	return e, nil
}

// Put implements Store. The filter only matches a stored entitlement older
// than e, so two webhook deliveries racing can't put the older one last.
func (s *MongoStore) Put(ctx context.Context, e Entitlement) (bool, error) {
	oid, err := mongodb.ParseID(e.HouseholdID)
	if err != nil {
		return false, fmt.Errorf("plus: bad household id")
	}
	d := entitlementDoc{
		HouseholdID: oid, ProductID: e.ProductID, Trial: e.Trial, WillRenew: e.WillRenew,
		BillingIssue: e.BillingIssue, Ended: e.Ended, Environment: e.Environment,
		LastEventID: e.LastEventID, LastEventAt: e.LastEventAt, UpdatedAt: e.UpdatedAt,
	}
	if !e.ExpiresAt.IsZero() {
		t := e.ExpiresAt
		d.ExpiresAt = &t
	}
	filter := bson.D{
		{Key: "householdId", Value: oid},
		{Key: "lastEventAt", Value: bson.D{{Key: "$lte", Value: e.LastEventAt}}},
	}
	res, err := s.coll.ReplaceOne(ctx, filter, d, options.Replace().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		// A newer entitlement is stored: the filter missed it and the upsert
		// collided with it.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("put plus entitlement: %w", err)
	}
	return res.MatchedCount+res.UpsertedCount > 0, nil
}

// Delete implements Store.
func (s *MongoStore) Delete(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.DeleteOne(ctx, bson.D{{Key: "householdId", Value: oid}})
	return err
}

// PurgeHousehold implements account.HouseholdPurger: a deleted household's
// Plus record goes with it. The App Store subscription itself is the
// member's to cancel in their Apple account.
func (s *MongoStore) PurgeHousehold(ctx context.Context, householdID string) error {
	return s.Delete(ctx, householdID)
}
