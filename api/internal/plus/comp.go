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

// Comps (decision 682): Plus given by DinnerOS, not bought from Apple. Every
// household that tested the beta gets Plus on the house, with no end date.
// Apple never hears of it, so nothing is billed, and the webhook never
// touches it.

// CompReasonBeta is the comp for households that tested the beta.
const CompReasonBeta = "beta"

// Comp is one household's free Plus.
type Comp struct {
	HouseholdID string
	Reason      string
	GrantedAt   time.Time
	// Until is when it ends; zero for never.
	Until time.Time
}

// Active reports whether the comp gives Plus at now.
func (c Comp) Active(now time.Time) bool {
	return c.Until.IsZero() || now.Before(c.Until)
}

// CompStore keeps comps, one per household.
type CompStore interface {
	// Get returns the household's comp, or ok false without one.
	Get(ctx context.Context, householdID string) (c Comp, ok bool, err error)
	// Grant stores c unless the household already has a comp; granted is
	// false then, so granting twice changes nothing.
	Grant(ctx context.Context, c Comp) (granted bool, err error)
	PurgeHousehold(ctx context.Context, householdID string) error
}

// WithComps turns on comps. It returns s.
func (s *Service) WithComps(c CompStore) *Service {
	s.comps = c
	return s
}

// The thank-you comped households see once.
const (
	CompThanksTitle = "Thanks for testing DinnerOS"
	CompThanksText  = "You helped shape DinnerOS before anyone else had it. Plus is on the house for your household, for good."
	CompStateText   = "On the house"
	// CompBenefitsTitle heads the list of what Plus includes.
	CompBenefitsTitle = "What you get"
)

// CompsCollection holds comps.
const CompsCollection = "plus_comps"

// CompIndexes returns the indexes MongoComps relies on.
func CompIndexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{Collection: CompsCollection, Indexes: []mongo.IndexModel{{
		Keys: bson.D{{Key: "householdId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("householdId_unique"),
	}}}}
}

// MongoComps is the MongoDB CompStore.
type MongoComps struct{ coll *mongo.Collection }

var _ CompStore = (*MongoComps)(nil)

// NewMongoComps returns a store using db.
func NewMongoComps(db *mongo.Database) *MongoComps {
	return &MongoComps{coll: db.Collection(CompsCollection)}
}

type compDoc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	Reason      string        `bson:"reason"`
	GrantedAt   time.Time     `bson:"grantedAt"`
	Until       *time.Time    `bson:"until,omitempty"`
}

// Get implements CompStore.
func (s *MongoComps) Get(ctx context.Context, householdID string) (Comp, bool, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Comp{}, false, nil
	}
	var d compDoc
	err = s.coll.FindOne(ctx, bson.D{{Key: "householdId", Value: oid}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Comp{}, false, nil
	}
	if err != nil {
		return Comp{}, false, fmt.Errorf("get plus comp: %w", err)
	}
	c := Comp{HouseholdID: d.HouseholdID.Hex(), Reason: d.Reason, GrantedAt: d.GrantedAt.UTC()}
	if d.Until != nil {
		c.Until = d.Until.UTC()
	}
	return c, true, nil
}

// Grant implements CompStore.
func (s *MongoComps) Grant(ctx context.Context, c Comp) (bool, error) {
	oid, err := mongodb.ParseID(c.HouseholdID)
	if err != nil {
		return false, fmt.Errorf("plus: bad household id")
	}
	d := compDoc{HouseholdID: oid, Reason: c.Reason, GrantedAt: c.GrantedAt.UTC()}
	if !c.Until.IsZero() {
		t := c.Until.UTC()
		d.Until = &t
	}
	_, err = s.coll.InsertOne(ctx, d)
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("grant plus comp: %w", err)
	}
	return true, nil
}

// PurgeHousehold implements account.HouseholdPurger.
func (s *MongoComps) PurgeHousehold(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.DeleteOne(ctx, bson.D{{Key: "householdId", Value: oid}})
	return err
}
