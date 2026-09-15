package users

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
	UsersCollection      = "users"
	IdentitiesCollection = "auth_identities"
)

// Indexes returns the indexes this package's MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{
			Collection: IdentitiesCollection,
			Indexes: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "provider", Value: 1}, {Key: "subject", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("provider_subject_unique"),
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
	users      *mongo.Collection
	identities *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{
		users:      db.Collection(UsersCollection),
		identities: db.Collection(IdentitiesCollection),
	}
}

type userDoc struct {
	ID           bson.ObjectID `bson:"_id"`
	DisplayName  string        `bson:"displayName"`
	PrimaryEmail string        `bson:"primaryEmail,omitempty"`
	CreatedAt    time.Time     `bson:"createdAt"`
	UpdatedAt    time.Time     `bson:"updatedAt"`
}

func (d userDoc) toUser() User {
	return User{
		ID:           d.ID.Hex(),
		DisplayName:  d.DisplayName,
		PrimaryEmail: d.PrimaryEmail,
		CreatedAt:    d.CreatedAt.UTC(),
		UpdatedAt:    d.UpdatedAt.UTC(),
	}
}

type identityDoc struct {
	ID            bson.ObjectID `bson:"_id"`
	UserID        bson.ObjectID `bson:"userId"`
	Provider      string        `bson:"provider"`
	Subject       string        `bson:"subject"`
	Email         string        `bson:"email,omitempty"`
	EmailVerified bool          `bson:"emailVerified"`
	CreatedAt     time.Time     `bson:"createdAt"`
	LastUsedAt    time.Time     `bson:"lastUsedAt"`
}

func (d identityDoc) toIdentity() AuthIdentity {
	return AuthIdentity{
		ID:            d.ID.Hex(),
		UserID:        d.UserID.Hex(),
		Provider:      Provider(d.Provider),
		Subject:       d.Subject,
		Email:         d.Email,
		EmailVerified: d.EmailVerified,
		CreatedAt:     d.CreatedAt.UTC(),
		LastUsedAt:    d.LastUsedAt.UTC(),
	}
}

// CreateUser implements Store.
func (s *MongoStore) CreateUser(ctx context.Context, u User) (User, error) {
	doc := userDoc{
		ID:           bson.NewObjectID(),
		DisplayName:  u.DisplayName,
		PrimaryEmail: u.PrimaryEmail,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}
	if _, err := s.users.InsertOne(ctx, doc); err != nil {
		return User{}, translate(err)
	}
	return doc.toUser(), nil
}

// DeleteUser implements Store.
func (s *MongoStore) DeleteUser(ctx context.Context, id string) error {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	_, err = s.users.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}})
	return translate(err)
}

// GetUser implements Store.
func (s *MongoStore) GetUser(ctx context.Context, id string) (User, error) {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return User{}, ErrNotFound
	}
	var doc userDoc
	if err := s.users.FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc); err != nil {
		return User{}, translate(err)
	}
	return doc.toUser(), nil
}

// CreateIdentity implements Store.
func (s *MongoStore) CreateIdentity(ctx context.Context, identity AuthIdentity) (AuthIdentity, error) {
	userID, err := mongodb.ParseID(identity.UserID)
	if err != nil {
		return AuthIdentity{}, fmt.Errorf("users: identity user id: %w", err)
	}
	doc := identityDoc{
		ID:            bson.NewObjectID(),
		UserID:        userID,
		Provider:      string(identity.Provider),
		Subject:       identity.Subject,
		Email:         identity.Email,
		EmailVerified: identity.EmailVerified,
		CreatedAt:     identity.CreatedAt,
		LastUsedAt:    identity.LastUsedAt,
	}
	if _, err := s.identities.InsertOne(ctx, doc); err != nil {
		return AuthIdentity{}, translate(err)
	}
	return doc.toIdentity(), nil
}

// FindIdentity implements Store.
func (s *MongoStore) FindIdentity(ctx context.Context, provider Provider, subject string) (AuthIdentity, error) {
	var doc identityDoc
	filter := bson.D{{Key: "provider", Value: string(provider)}, {Key: "subject", Value: subject}}
	if err := s.identities.FindOne(ctx, filter).Decode(&doc); err != nil {
		return AuthIdentity{}, translate(err)
	}
	return doc.toIdentity(), nil
}

// TouchIdentity implements Store.
func (s *MongoStore) TouchIdentity(ctx context.Context, id string, email string, at time.Time) error {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	set := bson.D{{Key: "lastUsedAt", Value: at}}
	if email != "" {
		set = append(set, bson.E{Key: "email", Value: email}, bson.E{Key: "emailVerified", Value: true})
	}
	res, err := s.identities.UpdateByID(ctx, oid, bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// ListIdentities implements Store.
func (s *MongoStore) ListIdentities(ctx context.Context, userID string) ([]AuthIdentity, error) {
	oid, err := mongodb.ParseID(userID)
	if err != nil {
		return nil, ErrNotFound
	}
	cur, err := s.identities.Find(ctx,
		bson.D{{Key: "userId", Value: oid}},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}),
	)
	if err != nil {
		return nil, translate(err)
	}
	var docs []identityDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]AuthIdentity, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toIdentity())
	}
	return out, nil
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
