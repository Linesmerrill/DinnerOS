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

// Collections for qualifying referrals (decision 698).
const (
	SetupCollection      = "plus_household_setup"
	HistoryCollection    = "plus_identity_households"
	DevicesCollection    = "plus_device_ties"
	LocksCollection      = "plus_referral_locks"
	MigrationsCollection = "plus_migrations"
)

// QualifyIndexes returns the indexes the qualify stores rely on.
func QualifyIndexes() []mongodb.IndexSet {
	byHousehold := mongo.IndexModel{Keys: bson.D{{Key: "householdId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("householdId_unique")}
	return []mongodb.IndexSet{
		{Collection: SetupCollection, Indexes: []mongo.IndexModel{byHousehold}},
		{Collection: DevicesCollection, Indexes: []mongo.IndexModel{byHousehold}},
		{Collection: HistoryCollection, Indexes: []mongo.IndexModel{
			{Keys: bson.D{{Key: "identity", Value: 1}, {Key: "householdId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("identity_householdId_unique")},
			{Keys: bson.D{{Key: "householdId", Value: 1}}, Options: options.Index().SetName("householdId")},
		}},
	}
}

// MongoSetup is the MongoDB SetupStore.
type MongoSetup struct{ coll *mongo.Collection }

var _ SetupStore = (*MongoSetup)(nil)

// NewMongoSetup returns a store using db.
func NewMongoSetup(db *mongo.Database) *MongoSetup {
	return &MongoSetup{coll: db.Collection(SetupCollection)}
}

// Add implements SetupStore in one upsert.
func (s *MongoSetup) Add(ctx context.Context, householdID string, dinners, recipes int) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.UpdateOne(ctx, bson.D{{Key: "householdId", Value: oid}}, bson.D{
		{Key: "$inc", Value: bson.D{{Key: "dinners", Value: dinners}, {Key: "recipes", Value: recipes}}},
		{Key: "$set", Value: bson.D{{Key: "updatedAt", Value: time.Now().UTC()}}},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("count setup: %w", err)
	}
	return nil
}

// Get implements SetupStore.
func (s *MongoSetup) Get(ctx context.Context, householdID string) (Setup, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Setup{}, nil
	}
	var d struct {
		Dinners int `bson:"dinners"`
		Recipes int `bson:"recipes"`
	}
	err = s.coll.FindOne(ctx, bson.D{{Key: "householdId", Value: oid}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Setup{}, nil
	}
	if err != nil {
		return Setup{}, fmt.Errorf("get setup: %w", err)
	}
	return Setup{Dinners: d.Dinners, Recipes: d.Recipes}, nil
}

// PurgeHousehold implements account.HouseholdPurger.
func (s *MongoSetup) PurgeHousehold(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.DeleteOne(ctx, bson.D{{Key: "householdId", Value: oid}})
	return err
}

// MongoHistory is the MongoDB HistoryStore. Nothing purges it: it holds
// hashed identities and IDs only, and remembering them is its job.
type MongoHistory struct{ coll *mongo.Collection }

var _ HistoryStore = (*MongoHistory)(nil)

// NewMongoHistory returns a store using db.
func NewMongoHistory(db *mongo.Database) *MongoHistory {
	return &MongoHistory{coll: db.Collection(HistoryCollection)}
}

type historyDoc struct {
	Identity    string        `bson:"identity"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	UserID      string        `bson:"userId"`
	At          time.Time     `bson:"at"`
}

// Add implements HistoryStore with one bulk upsert keeping the earliest at.
func (s *MongoHistory) Add(ctx context.Context, recs []MembershipRecord) error {
	var models []mongo.WriteModel
	for _, r := range recs {
		oid, err := mongodb.ParseID(r.HouseholdID)
		if err != nil {
			continue
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.D{{Key: "identity", Value: r.Identity}, {Key: "householdId", Value: oid}}).
			SetUpdate(bson.D{
				{Key: "$min", Value: bson.D{{Key: "at", Value: r.At.UTC()}}},
				{Key: "$setOnInsert", Value: bson.D{{Key: "userId", Value: r.UserID}}},
			}).SetUpsert(true))
	}
	if len(models) == 0 {
		return nil
	}
	_, err := s.coll.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("record membership history: %w", err)
	}
	return nil
}

func (s *MongoHistory) find(ctx context.Context, filter bson.D) ([]MembershipRecord, error) {
	cur, err := s.coll.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("read membership history: %w", err)
	}
	var docs []historyDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode membership history: %w", err)
	}
	out := make([]MembershipRecord, 0, len(docs))
	for _, d := range docs {
		out = append(out, MembershipRecord{Identity: d.Identity, HouseholdID: d.HouseholdID.Hex(), UserID: d.UserID, At: d.At.UTC()})
	}
	return out, nil
}

// ForIdentities implements HistoryStore.
func (s *MongoHistory) ForIdentities(ctx context.Context, identities []string) ([]MembershipRecord, error) {
	if len(identities) == 0 {
		return nil, nil
	}
	return s.find(ctx, bson.D{{Key: "identity", Value: bson.D{{Key: "$in", Value: identities}}}})
}

// ForHousehold implements HistoryStore.
func (s *MongoHistory) ForHousehold(ctx context.Context, householdID string) ([]MembershipRecord, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	return s.find(ctx, bson.D{{Key: "householdId", Value: oid}})
}

// MongoDevices is the MongoDB DeviceTieStore.
type MongoDevices struct{ coll *mongo.Collection }

var _ DeviceTieStore = (*MongoDevices)(nil)

// NewMongoDevices returns a store using db.
func NewMongoDevices(db *mongo.Database) *MongoDevices {
	return &MongoDevices{coll: db.Collection(DevicesCollection)}
}

func (s *MongoDevices) mark(ctx context.Context, householdID, field string, at time.Time) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.UpdateOne(ctx, bson.D{{Key: "householdId", Value: oid}},
		bson.D{{Key: "$set", Value: bson.D{{Key: field, Value: true}, {Key: "updatedAt", Value: at.UTC()}}}},
		options.UpdateOne().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		// Two marks raced to insert; the other one made the document.
		_, err = s.coll.UpdateOne(ctx, bson.D{{Key: "householdId", Value: oid}},
			bson.D{{Key: "$set", Value: bson.D{{Key: field, Value: true}, {Key: "updatedAt", Value: at.UTC()}}}})
	}
	if err != nil {
		return fmt.Errorf("record device: %w", err)
	}
	return nil
}

// MarkOwned implements DeviceTieStore.
func (s *MongoDevices) MarkOwned(ctx context.Context, householdID string, at time.Time) error {
	return s.mark(ctx, householdID, "owns", at)
}

// MarkElsewhere implements DeviceTieStore.
func (s *MongoDevices) MarkElsewhere(ctx context.Context, householdID string, at time.Time) error {
	return s.mark(ctx, householdID, "elsewhere", at)
}

// Get implements DeviceTieStore.
func (s *MongoDevices) Get(ctx context.Context, householdID string) (DeviceTie, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return DeviceTie{}, nil
	}
	var d struct {
		Owns      bool `bson:"owns"`
		Elsewhere bool `bson:"elsewhere"`
	}
	err = s.coll.FindOne(ctx, bson.D{{Key: "householdId", Value: oid}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return DeviceTie{}, nil
	}
	if err != nil {
		return DeviceTie{}, fmt.Errorf("read device: %w", err)
	}
	return DeviceTie{Owns: d.Owns, Elsewhere: d.Elsewhere}, nil
}

// PurgeHousehold implements account.HouseholdPurger. The device keeps its
// DeviceCheck bit at Apple, so a new household on it still isn't new.
func (s *MongoDevices) PurgeHousehold(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.DeleteOne(ctx, bson.D{{Key: "householdId", Value: oid}})
	return err
}

// MongoLocks is the MongoDB LockStore: one document per key, taken by an
// upsert that only matches an expired lease.
type MongoLocks struct{ coll *mongo.Collection }

var _ LockStore = (*MongoLocks)(nil)

// NewMongoLocks returns a store using db.
func NewMongoLocks(db *mongo.Database) *MongoLocks {
	return &MongoLocks{coll: db.Collection(LocksCollection)}
}

// Acquire implements LockStore. A held lock makes the upsert collide on _id.
func (s *MongoLocks) Acquire(ctx context.Context, key string, now, until time.Time) (bool, error) {
	_, err := s.coll.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: key}, {Key: "until", Value: bson.D{{Key: "$lte", Value: now.UTC()}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "until", Value: until.UTC()}}}},
		options.UpdateOne().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("take referral lock: %w", err)
	}
	return true, nil
}

// Release implements LockStore.
func (s *MongoLocks) Release(ctx context.Context, key string) error {
	_, err := s.coll.DeleteOne(ctx, bson.D{{Key: "_id", Value: key}})
	return err
}

// Migrations remember one-off jobs that are done.
type Migrations struct{ coll *mongo.Collection }

// NewMigrations returns migrations kept in db.
func NewMigrations(db *mongo.Database) *Migrations {
	return &Migrations{coll: db.Collection(MigrationsCollection)}
}

// Done reports whether name already ran.
func (m *Migrations) Done(ctx context.Context, name string) (bool, error) {
	n, err := m.coll.CountDocuments(ctx, bson.D{{Key: "_id", Value: name}})
	return n > 0, err
}

// MarkDone records that name ran.
func (m *Migrations) MarkDone(ctx context.Context, name string, at time.Time) error {
	_, err := m.coll.UpdateOne(ctx, bson.D{{Key: "_id", Value: name}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "at", Value: at.UTC()}}}}, options.UpdateOne().SetUpsert(true))
	return err
}
