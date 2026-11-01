package shopping

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file is the shopping_provider_accounts half of MongoStore: a
// household's signed-in provider account, its tokens sealed by the service
// (accounts.go). Nothing here can read them.

// AccountsCollection holds households' provider accounts.
const AccountsCollection = "shopping_provider_accounts"

type accountDoc struct {
	HouseholdID  bson.ObjectID `bson:"householdId"`
	Provider     string        `bson:"provider"`
	AccessToken  string        `bson:"accessToken"`
	RefreshToken string        `bson:"refreshToken"`
	ExpiresAt    time.Time     `bson:"expiresAt"`
	ConnectedBy  bson.ObjectID `bson:"connectedBy"`
	ConnectedAt  time.Time     `bson:"connectedAt"`
	UpdatedAt    time.Time     `bson:"updatedAt"`
}

func accountFilter(householdID string, provider providers.Key) (bson.D, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	return bson.D{{Key: "householdId", Value: hid}, {Key: "provider", Value: string(provider)}}, nil
}

// GetProviderAccount implements Store.
func (s *MongoStore) GetProviderAccount(ctx context.Context, householdID string, provider providers.Key) (StoredAccount, error) {
	filter, err := accountFilter(householdID, provider)
	if err != nil {
		return StoredAccount{}, err
	}
	var d accountDoc
	if err := s.accounts.FindOne(ctx, filter).Decode(&d); err != nil {
		return StoredAccount{}, translate(err)
	}
	return StoredAccount{
		HouseholdID: d.HouseholdID.Hex(), Provider: providers.Key(d.Provider), AccessToken: d.AccessToken, RefreshToken: d.RefreshToken,
		ExpiresAt: d.ExpiresAt.UTC(), ConnectedBy: hexOrEmpty(d.ConnectedBy), ConnectedAt: d.ConnectedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}, nil
}

// PutProviderAccount implements Store: it creates or replaces the
// household's account for the provider.
func (s *MongoStore) PutProviderAccount(ctx context.Context, a StoredAccount) error {
	filter, err := accountFilter(a.HouseholdID, a.Provider)
	if err != nil {
		return err
	}
	uid, err := userOID(a.ConnectedBy)
	if err != nil {
		return err
	}
	d := accountDoc{
		HouseholdID: filter[0].Value.(bson.ObjectID), Provider: string(a.Provider), AccessToken: a.AccessToken, RefreshToken: a.RefreshToken,
		ExpiresAt: a.ExpiresAt, ConnectedBy: uid, ConnectedAt: a.ConnectedAt, UpdatedAt: a.UpdatedAt,
	}
	_, err = s.accounts.ReplaceOne(ctx, filter, d, options.Replace().SetUpsert(true))
	return translate(err)
}

// DeleteProviderAccount implements Store.
func (s *MongoStore) DeleteProviderAccount(ctx context.Context, householdID string, provider providers.Key) error {
	filter, err := accountFilter(householdID, provider)
	if err != nil {
		return err
	}
	res, err := s.accounts.DeleteOne(ctx, filter)
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
