package siwa

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Collection holds one sealed Apple refresh token per user.
const Collection = "apple_sign_in_tokens"

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "userId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("userId_unique"),
		}},
	}}
}

// Store keeps sealed refresh tokens by user ID.
type Store interface {
	Put(ctx context.Context, userID, sealed string, at time.Time) error
	// Get returns "" when the user has none.
	Get(ctx context.Context, userID string) (string, error)
	PurgeUser(ctx context.Context, userID string) error
}

// MongoStore is the MongoDB Store.
type MongoStore struct{ c *mongo.Collection }

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{c: db.Collection(Collection)}
}

type tokenDoc struct {
	UserID       bson.ObjectID `bson:"userId"`
	RefreshToken string        `bson:"refreshToken"`
	UpdatedAt    time.Time     `bson:"updatedAt"`
}

// Put stores the user's sealed token, replacing any earlier one.
func (s *MongoStore) Put(ctx context.Context, userID, sealed string, at time.Time) error {
	id, err := mongodb.ParseID(userID)
	if err != nil {
		return err
	}
	_, err = s.c.UpdateOne(ctx, bson.D{{Key: "userId", Value: id}},
		bson.D{{Key: "$set", Value: tokenDoc{UserID: id, RefreshToken: sealed, UpdatedAt: at.UTC()}}},
		options.UpdateOne().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("siwa: store token: %w", err)
	}
	return nil
}

// Get returns the user's sealed token, or "" if there is none.
func (s *MongoStore) Get(ctx context.Context, userID string) (string, error) {
	id, err := mongodb.ParseID(userID)
	if err != nil {
		return "", nil
	}
	var doc tokenDoc
	err = s.c.FindOne(ctx, bson.D{{Key: "userId", Value: id}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("siwa: load token: %w", err)
	}
	return doc.RefreshToken, nil
}

// PurgeUser deletes the user's stored token, for account deletion.
func (s *MongoStore) PurgeUser(ctx context.Context, userID string) error {
	return mongodb.DeleteByID(ctx, "userId", userID, s.c)
}

// box seals refresh tokens at rest with AES-256-GCM, under a key derived
// from the server's token signing key.
type box struct{ aead cipher.AEAD }

func newBox(secret []byte) (*box, error) {
	if len(secret) == 0 {
		return nil, errors.New("siwa: no key to seal tokens with")
	}
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("dinneros/siwa/refresh-token"))
	block, err := aes.NewCipher(m.Sum(nil))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &box{aead: aead}, nil
}

func (b *box) seal(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

var errUnreadable = errors.New("siwa: stored token is unreadable")

func (b *box) open(sealed string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", errUnreadable
	}
	plain, err := b.aead.Open(nil, raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():], nil)
	if err != nil {
		return "", errUnreadable
	}
	return string(plain), nil
}
