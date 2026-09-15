package auth

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

// SessionsCollection holds refresh-token sessions.
const SessionsCollection = "sessions"

// Indexes returns the indexes MongoSessionStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: SessionsCollection,
		Indexes: []mongo.IndexModel{
			{
				Keys:    bson.D{{Key: "tokenHash", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("tokenHash_unique"),
			},
			{
				Keys:    bson.D{{Key: "userId", Value: 1}},
				Options: options.Index().SetName("userId"),
			},
			{
				Keys:    bson.D{{Key: "familyId", Value: 1}},
				Options: options.Index().SetName("familyId"),
			},
			{
				// MongoDB deletes sessions shortly after they expire. Rotated
				// sessions keep their original expiry so reuse is detectable for
				// as long as the old token could otherwise have been used.
				Keys:    bson.D{{Key: "expiresAt", Value: 1}},
				Options: options.Index().SetExpireAfterSeconds(0).SetName("expiresAt_ttl"),
			},
		},
	}}
}

// MongoSessionStore is the MongoDB implementation of SessionStore.
type MongoSessionStore struct {
	sessions *mongo.Collection
}

var _ SessionStore = (*MongoSessionStore)(nil)

// NewMongoSessionStore returns a store using db.
func NewMongoSessionStore(db *mongo.Database) *MongoSessionStore {
	return &MongoSessionStore{sessions: db.Collection(SessionsCollection)}
}

type sessionDoc struct {
	ID         bson.ObjectID `bson:"_id"`
	UserID     bson.ObjectID `bson:"userId"`
	FamilyID   string        `bson:"familyId"`
	TokenHash  string        `bson:"tokenHash"`
	ExpiresAt  time.Time     `bson:"expiresAt"`
	CreatedAt  time.Time     `bson:"createdAt"`
	LastUsedAt time.Time     `bson:"lastUsedAt"`
	RotatedAt  *time.Time    `bson:"rotatedAt"`
	RevokedAt  *time.Time    `bson:"revokedAt"`
}

func (d sessionDoc) toSession() Session {
	return Session{
		ID:         d.ID.Hex(),
		UserID:     d.UserID.Hex(),
		FamilyID:   d.FamilyID,
		TokenHash:  d.TokenHash,
		ExpiresAt:  d.ExpiresAt.UTC(),
		CreatedAt:  d.CreatedAt.UTC(),
		LastUsedAt: d.LastUsedAt.UTC(),
		RotatedAt:  utcPtr(d.RotatedAt),
		RevokedAt:  utcPtr(d.RevokedAt),
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// CreateSession implements SessionStore.
func (s *MongoSessionStore) CreateSession(ctx context.Context, session Session) (Session, error) {
	userID, err := mongodb.ParseID(session.UserID)
	if err != nil {
		return Session{}, fmt.Errorf("auth: session user id: %w", err)
	}
	doc := sessionDoc{
		ID:         bson.NewObjectID(),
		UserID:     userID,
		FamilyID:   session.FamilyID,
		TokenHash:  session.TokenHash,
		ExpiresAt:  session.ExpiresAt,
		CreatedAt:  session.CreatedAt,
		LastUsedAt: session.LastUsedAt,
		RotatedAt:  session.RotatedAt,
		RevokedAt:  session.RevokedAt,
	}
	if _, err := s.sessions.InsertOne(ctx, doc); err != nil {
		return Session{}, mongodb.TranslateError(err)
	}
	return doc.toSession(), nil
}

// FindSessionByTokenHash implements SessionStore.
func (s *MongoSessionStore) FindSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	var doc sessionDoc
	err := s.sessions.FindOne(ctx, bson.D{{Key: "tokenHash", Value: tokenHash}}).Decode(&doc)
	if err = mongodb.TranslateError(err); err != nil {
		if errors.Is(err, mongodb.ErrNotFound) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, err
	}
	return doc.toSession(), nil
}

// MarkRotated implements SessionStore.
func (s *MongoSessionStore) MarkRotated(ctx context.Context, id string, at time.Time) (bool, error) {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return false, ErrSessionNotFound
	}
	filter := bson.D{
		{Key: "_id", Value: oid},
		{Key: "rotatedAt", Value: nil},
		{Key: "revokedAt", Value: nil},
	}
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "rotatedAt", Value: at},
		{Key: "lastUsedAt", Value: at},
	}}}
	res, err := s.sessions.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, mongodb.TranslateError(err)
	}
	return res.ModifiedCount == 1, nil
}

// RevokeFamily implements SessionStore.
func (s *MongoSessionStore) RevokeFamily(ctx context.Context, familyID string, at time.Time) error {
	filter := bson.D{{Key: "familyId", Value: familyID}, {Key: "revokedAt", Value: nil}}
	update := bson.D{{Key: "$set", Value: bson.D{{Key: "revokedAt", Value: at}}}}
	_, err := s.sessions.UpdateMany(ctx, filter, update)
	return mongodb.TranslateError(err)
}
