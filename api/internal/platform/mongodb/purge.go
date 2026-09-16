package mongodb

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// DeleteByID deletes every document whose field holds the ObjectID in hex,
// from each collection in turn. Account deletion uses it for documents keyed
// by householdId or userId. A malformed ID matches nothing and is not an
// error, so a retry after a partial failure is always safe.
func DeleteByID(ctx context.Context, field, hex string, collections ...*mongo.Collection) error {
	id, err := ParseID(hex)
	if err != nil {
		return nil
	}
	for _, c := range collections {
		if _, err := c.DeleteMany(ctx, bson.D{{Key: field, Value: id}}); err != nil {
			return fmt.Errorf("delete by %s from %s: %w", field, c.Name(), err)
		}
	}
	return nil
}

// UnsetID removes field from every document where it holds the ObjectID in
// hex, keeping the documents. Household history (events, cook usage) stays
// with the household but no longer names a deleted user.
func UnsetID(ctx context.Context, c *mongo.Collection, field, hex string) error {
	id, err := ParseID(hex)
	if err != nil {
		return nil
	}
	_, err = c.UpdateMany(ctx,
		bson.D{{Key: field, Value: id}},
		bson.D{{Key: "$unset", Value: bson.D{{Key: field, Value: ""}}}})
	if err != nil {
		return fmt.Errorf("unset %s in %s: %w", field, c.Name(), err)
	}
	return nil
}
