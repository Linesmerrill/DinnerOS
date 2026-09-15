package invitations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// InvitationsCollection is the collection owned by this package.
const InvitationsCollection = "household_invitations"

// Indexes returns the indexes this package's MongoStore relies on.
//
// A document is "pending" (field pending: true) until it is accepted or
// revoked; expiry does not clear it. The partial unique index on
// {householdId, email} therefore allows only one pending invitation per
// address, and also serves the pending list for a household.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{
			Collection: InvitationsCollection,
			Indexes: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "tokenHash", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("tokenHash_unique"),
				},
				{
					Keys:    bson.D{{Key: "codeHash", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("codeHash_unique"),
				},
				{
					Keys: bson.D{{Key: "householdId", Value: 1}, {Key: "email", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("householdId_email_pending_unique").
						SetPartialFilterExpression(bson.D{{Key: "pending", Value: true}}),
				},
			},
		},
	}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	invitations *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{invitations: db.Collection(InvitationsCollection)}
}

type invitationDoc struct {
	ID          bson.ObjectID  `bson:"_id"`
	HouseholdID bson.ObjectID  `bson:"householdId"`
	Email       string         `bson:"email"`
	Role        string         `bson:"role"`
	TokenHash   string         `bson:"tokenHash"`
	CodeHash    string         `bson:"codeHash"`
	ExpiresAt   time.Time      `bson:"expiresAt"`
	Pending     bool           `bson:"pending,omitempty"`
	AcceptedAt  *time.Time     `bson:"acceptedAt,omitempty"`
	AcceptedBy  *bson.ObjectID `bson:"acceptedBy,omitempty"`
	RevokedAt   *time.Time     `bson:"revokedAt,omitempty"`
	CreatedBy   bson.ObjectID  `bson:"createdBy"`
	CreatedAt   time.Time      `bson:"createdAt"`
}

func (d invitationDoc) toInvitation() Invitation {
	inv := Invitation{
		ID:          d.ID.Hex(),
		HouseholdID: d.HouseholdID.Hex(),
		Email:       d.Email,
		Role:        households.Role(d.Role),
		TokenHash:   d.TokenHash,
		CodeHash:    d.CodeHash,
		ExpiresAt:   d.ExpiresAt.UTC(),
		CreatedBy:   d.CreatedBy.Hex(),
		CreatedAt:   d.CreatedAt.UTC(),
	}
	if d.AcceptedAt != nil {
		inv.AcceptedAt = d.AcceptedAt.UTC()
	}
	if d.AcceptedBy != nil {
		inv.AcceptedBy = d.AcceptedBy.Hex()
	}
	if d.RevokedAt != nil {
		inv.RevokedAt = d.RevokedAt.UTC()
	}
	return inv
}

// Create implements Store.
func (s *MongoStore) Create(ctx context.Context, inv Invitation) (Invitation, error) {
	householdID, err := mongodb.ParseID(inv.HouseholdID)
	if err != nil {
		return Invitation{}, fmt.Errorf("invitations: household id: %w", err)
	}
	createdBy, err := mongodb.ParseID(inv.CreatedBy)
	if err != nil {
		return Invitation{}, fmt.Errorf("invitations: createdBy: %w", err)
	}
	doc := invitationDoc{
		ID:          bson.NewObjectID(),
		HouseholdID: householdID,
		Email:       inv.Email,
		Role:        string(inv.Role),
		TokenHash:   inv.TokenHash,
		CodeHash:    inv.CodeHash,
		ExpiresAt:   inv.ExpiresAt,
		Pending:     true,
		CreatedBy:   createdBy,
		CreatedAt:   inv.CreatedAt,
	}
	if _, err := s.invitations.InsertOne(ctx, doc); err != nil {
		return Invitation{}, translate(err)
	}
	return doc.toInvitation(), nil
}

// Get implements Store.
func (s *MongoStore) Get(ctx context.Context, householdID, id string) (Invitation, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Invitation{}, ErrNotFound
	}
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return Invitation{}, ErrNotFound
	}
	return s.findOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}})
}

// FindByTokenHash implements Store.
func (s *MongoStore) FindByTokenHash(ctx context.Context, hash string) (Invitation, error) {
	return s.findOne(ctx, bson.D{{Key: "tokenHash", Value: hash}})
}

// FindByCodeHash implements Store.
func (s *MongoStore) FindByCodeHash(ctx context.Context, hash string) (Invitation, error) {
	return s.findOne(ctx, bson.D{{Key: "codeHash", Value: hash}})
}

func (s *MongoStore) findOne(ctx context.Context, filter bson.D) (Invitation, error) {
	var doc invitationDoc
	if err := s.invitations.FindOne(ctx, filter).Decode(&doc); err != nil {
		return Invitation{}, translate(err)
	}
	return doc.toInvitation(), nil
}

// ListPending implements Store.
func (s *MongoStore) ListPending(ctx context.Context, householdID string, now time.Time) ([]Invitation, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return []Invitation{}, nil
	}
	cur, err := s.invitations.Find(ctx,
		bson.D{
			{Key: "householdId", Value: hid},
			{Key: "pending", Value: true},
			{Key: "expiresAt", Value: bson.D{{Key: "$gt", Value: now}}},
		},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}),
	)
	if err != nil {
		return nil, translate(err)
	}
	var docs []invitationDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Invitation, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toInvitation())
	}
	return out, nil
}

// RevokePending implements Store.
func (s *MongoStore) RevokePending(ctx context.Context, householdID, email string, at time.Time) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return ErrNotFound
	}
	_, err = s.invitations.UpdateMany(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "email", Value: email}, {Key: "pending", Value: true}},
		revokeUpdate(at),
	)
	return translate(err)
}

// Revoke implements Store.
func (s *MongoStore) Revoke(ctx context.Context, householdID, id string, at time.Time) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return ErrNotFound
	}
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	_, err = s.invitations.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}, {Key: "pending", Value: true}},
		revokeUpdate(at),
	)
	return translate(err)
}

func revokeUpdate(at time.Time) bson.D {
	return bson.D{
		{Key: "$set", Value: bson.D{{Key: "revokedAt", Value: at}}},
		{Key: "$unset", Value: bson.D{{Key: "pending", Value: ""}}},
	}
}

// MarkAccepted implements Store.
func (s *MongoStore) MarkAccepted(ctx context.Context, id, userID string, at time.Time) error {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	uid, err := mongodb.ParseID(userID)
	if err != nil {
		return fmt.Errorf("invitations: user id: %w", err)
	}
	res, err := s.invitations.UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: oid},
			{Key: "pending", Value: true},
			{Key: "expiresAt", Value: bson.D{{Key: "$gt", Value: at}}},
		},
		bson.D{
			{Key: "$set", Value: bson.D{{Key: "acceptedAt", Value: at}, {Key: "acceptedBy", Value: uid}}},
			{Key: "$unset", Value: bson.D{{Key: "pending", Value: ""}}},
		},
	)
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// UnmarkAccepted implements Store.
func (s *MongoStore) UnmarkAccepted(ctx context.Context, id, userID string) error {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	uid, err := mongodb.ParseID(userID)
	if err != nil {
		return ErrNotFound
	}
	res, err := s.invitations.UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: oid},
			{Key: "acceptedBy", Value: uid},
			{Key: "revokedAt", Value: bson.D{{Key: "$exists", Value: false}}},
		},
		bson.D{
			{Key: "$set", Value: bson.D{{Key: "pending", Value: true}}},
			{Key: "$unset", Value: bson.D{{Key: "acceptedAt", Value: ""}, {Key: "acceptedBy", Value: ""}}},
		},
	)
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
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
