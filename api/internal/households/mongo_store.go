package households

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

// Collection names owned by this package.
const (
	HouseholdsCollection  = "households"
	MembershipsCollection = "household_memberships"
)

// Indexes returns the indexes this package's MongoStore relies on.
// households is looked up by _id only.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{
			Collection: MembershipsCollection,
			Indexes: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "userId", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("householdId_userId_unique"),
				},
				{
					Keys:    bson.D{{Key: "userId", Value: 1}},
					Options: options.Index().SetName("userId"),
				},
			},
		},
	}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	households  *mongo.Collection
	memberships *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{
		households:  db.Collection(HouseholdsCollection),
		memberships: db.Collection(MembershipsCollection),
	}
}

type householdDoc struct {
	ID              bson.ObjectID `bson:"_id"`
	Name            string        `bson:"name"`
	DefaultServings int           `bson:"defaultServings"`
	TimeZone        string        `bson:"timeZone"`
	// OrderDay is absent for a household that never set one.
	OrderDay  string        `bson:"orderDay,omitempty"`
	CreatedBy bson.ObjectID `bson:"createdBy"`
	// AdminCount backs the last-admin guard; see DecrementAdminCount.
	AdminCount int       `bson:"adminCount"`
	CreatedAt  time.Time `bson:"createdAt"`
	UpdatedAt  time.Time `bson:"updatedAt"`
}

func (d householdDoc) toHousehold() Household {
	return Household{
		ID:              d.ID.Hex(),
		Name:            d.Name,
		DefaultServings: d.DefaultServings,
		TimeZone:        d.TimeZone,
		OrderDay:        d.OrderDay,
		CreatedBy:       d.CreatedBy.Hex(),
		CreatedAt:       d.CreatedAt.UTC(),
		UpdatedAt:       d.UpdatedAt.UTC(),
	}
}

type membershipDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	UserID      bson.ObjectID `bson:"userId"`
	Role        string        `bson:"role"`
	CreatedAt   time.Time     `bson:"createdAt"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
}

func (d membershipDoc) toMembership() Membership {
	return Membership{
		ID:          d.ID.Hex(),
		HouseholdID: d.HouseholdID.Hex(),
		UserID:      d.UserID.Hex(),
		Role:        Role(d.Role),
		CreatedAt:   d.CreatedAt.UTC(),
		UpdatedAt:   d.UpdatedAt.UTC(),
	}
}

// CreateHousehold implements Store.
func (s *MongoStore) CreateHousehold(ctx context.Context, h Household) (Household, error) {
	createdBy, err := mongodb.ParseID(h.CreatedBy)
	if err != nil {
		return Household{}, fmt.Errorf("households: createdBy: %w", err)
	}
	doc := householdDoc{
		ID:              bson.NewObjectID(),
		Name:            h.Name,
		DefaultServings: h.DefaultServings,
		TimeZone:        h.TimeZone,
		CreatedBy:       createdBy,
		AdminCount:      1,
		CreatedAt:       h.CreatedAt,
		UpdatedAt:       h.UpdatedAt,
	}
	if _, err := s.households.InsertOne(ctx, doc); err != nil {
		return Household{}, translate(err)
	}
	return doc.toHousehold(), nil
}

// DeleteHousehold implements Store.
func (s *MongoStore) DeleteHousehold(ctx context.Context, id string) error {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	_, err = s.households.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}})
	return translate(err)
}

// GetHousehold implements Store.
func (s *MongoStore) GetHousehold(ctx context.Context, id string) (Household, error) {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return Household{}, ErrNotFound
	}
	var doc householdDoc
	if err := s.households.FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc); err != nil {
		return Household{}, translate(err)
	}
	return doc.toHousehold(), nil
}

// ListHouseholds implements Store.
func (s *MongoStore) ListHouseholds(ctx context.Context, ids []string) ([]Household, error) {
	oids := make([]bson.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, err := mongodb.ParseID(id); err == nil {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return []Household{}, nil
	}
	cur, err := s.households.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}})
	if err != nil {
		return nil, translate(err)
	}
	var docs []householdDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Household, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toHousehold())
	}
	return out, nil
}

// ListHouseholdIDs returns up to limit household IDs greater than after, in
// ID order; pass the last ID of one page as after for the next. It is for
// background sweeps over every household (cmd/sendreminders), which is why
// it isn't part of Store: nothing serving a request lists all households.
func (s *MongoStore) ListHouseholdIDs(ctx context.Context, after string, limit int) ([]string, error) {
	filter := bson.D{}
	if after != "" {
		oid, err := mongodb.ParseID(after)
		if err != nil {
			return nil, fmt.Errorf("households: after: %w", err)
		}
		filter = bson.D{{Key: "_id", Value: bson.D{{Key: "$gt", Value: oid}}}}
	}
	cur, err := s.households.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)).SetProjection(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.ID.Hex())
	}
	return out, nil
}

// UpdateHousehold implements Store.
func (s *MongoStore) UpdateHousehold(ctx context.Context, id string, patch HouseholdPatch, at time.Time) (Household, error) {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return Household{}, ErrNotFound
	}
	set := bson.D{{Key: "updatedAt", Value: at}}
	if patch.Name != nil {
		set = append(set, bson.E{Key: "name", Value: *patch.Name})
	}
	if patch.TimeZone != nil {
		set = append(set, bson.E{Key: "timeZone", Value: *patch.TimeZone})
	}
	if patch.DefaultServings != nil {
		set = append(set, bson.E{Key: "defaultServings", Value: *patch.DefaultServings})
	}
	if patch.OrderDay != nil {
		set = append(set, bson.E{Key: "orderDay", Value: *patch.OrderDay})
	}
	var doc householdDoc
	err = s.households.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: oid}},
		bson.D{{Key: "$set", Value: set}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		return Household{}, translate(err)
	}
	return doc.toHousehold(), nil
}

// DecrementAdminCount implements Store. The filter makes the check and the
// decrement one atomic operation.
func (s *MongoStore) DecrementAdminCount(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return ErrNotFound
	}
	res, err := s.households.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: oid}, {Key: "adminCount", Value: bson.D{{Key: "$gt", Value: 1}}}},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "adminCount", Value: -1}}}},
	)
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		if _, err := s.GetHousehold(ctx, householdID); err != nil {
			return err
		}
		return ErrLastAdmin
	}
	return nil
}

// IncrementAdminCount implements Store.
func (s *MongoStore) IncrementAdminCount(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return ErrNotFound
	}
	res, err := s.households.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: oid}},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "adminCount", Value: 1}}}},
	)
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateMembership implements Store.
func (s *MongoStore) CreateMembership(ctx context.Context, m Membership) (Membership, error) {
	householdID, userID, err := parseMembershipIDs(m.HouseholdID, m.UserID)
	if err != nil {
		return Membership{}, err
	}
	doc := membershipDoc{
		ID:          bson.NewObjectID(),
		HouseholdID: householdID,
		UserID:      userID,
		Role:        string(m.Role),
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	if _, err := s.memberships.InsertOne(ctx, doc); err != nil {
		return Membership{}, translate(err)
	}
	return doc.toMembership(), nil
}

// GetMembership implements Store.
func (s *MongoStore) GetMembership(ctx context.Context, householdID, userID string) (Membership, error) {
	hid, uid, err := parseMembershipIDs(householdID, userID)
	if err != nil {
		return Membership{}, ErrNotFound
	}
	var doc membershipDoc
	filter := bson.D{{Key: "householdId", Value: hid}, {Key: "userId", Value: uid}}
	if err := s.memberships.FindOne(ctx, filter).Decode(&doc); err != nil {
		return Membership{}, translate(err)
	}
	return doc.toMembership(), nil
}

// ListMembershipsByHousehold implements Store.
func (s *MongoStore) ListMembershipsByHousehold(ctx context.Context, householdID string) ([]Membership, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return []Membership{}, nil
	}
	return s.listMemberships(ctx, bson.D{{Key: "householdId", Value: oid}})
}

// ListMembershipsByUser implements Store.
func (s *MongoStore) ListMembershipsByUser(ctx context.Context, userID string) ([]Membership, error) {
	oid, err := mongodb.ParseID(userID)
	if err != nil {
		return []Membership{}, nil
	}
	return s.listMemberships(ctx, bson.D{{Key: "userId", Value: oid}})
}

func (s *MongoStore) listMemberships(ctx context.Context, filter bson.D) ([]Membership, error) {
	cur, err := s.memberships.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []membershipDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Membership, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toMembership())
	}
	return out, nil
}

// UpdateMembershipRole implements Store.
func (s *MongoStore) UpdateMembershipRole(ctx context.Context, householdID, userID string, from, to Role, at time.Time) (Membership, error) {
	hid, uid, err := parseMembershipIDs(householdID, userID)
	if err != nil {
		return Membership{}, ErrNotFound
	}
	var doc membershipDoc
	err = s.memberships.FindOneAndUpdate(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "userId", Value: uid}, {Key: "role", Value: string(from)}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "role", Value: string(to)}, {Key: "updatedAt", Value: at}}}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		return Membership{}, translate(err)
	}
	return doc.toMembership(), nil
}

// DeleteMembership implements Store.
func (s *MongoStore) DeleteMembership(ctx context.Context, householdID, userID string, role Role) error {
	hid, uid, err := parseMembershipIDs(householdID, userID)
	if err != nil {
		return ErrNotFound
	}
	res, err := s.memberships.DeleteOne(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "userId", Value: uid}, {Key: "role", Value: string(role)}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func parseMembershipIDs(householdID, userID string) (bson.ObjectID, bson.ObjectID, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return bson.ObjectID{}, bson.ObjectID{}, fmt.Errorf("households: household id: %w", err)
	}
	uid, err := mongodb.ParseID(userID)
	if err != nil {
		return bson.ObjectID{}, bson.ObjectID{}, fmt.Errorf("households: user id: %w", err)
	}
	return hid, uid, nil
}

// translate maps platform errors to this package's sentinels.
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
