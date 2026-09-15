// Package mongodb manages the MongoDB client lifecycle and the small set of
// conventions repositories share: index declarations and error translation.
//
// Each domain package owns its collections. It declares an IndexSet for them
// and implements its own store on top of Database().
package mongodb

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Errors returned by TranslateError and ParseID. Domain stores map these to
// their own domain errors.
var (
	ErrNotFound  = errors.New("document not found")
	ErrDuplicate = errors.New("duplicate key")
	ErrInvalidID = errors.New("invalid id")
)

// Config describes how to connect.
type Config struct {
	URI      string
	Database string
	AppName  string
}

// Client wraps a connected MongoDB client bound to one database.
type Client struct {
	client *mongo.Client
	db     *mongo.Database
}

// Connect creates a client and verifies connectivity with a ping, bounded by
// ctx. Errors never include the connection string, which may hold credentials.
func Connect(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Database == "" {
		return nil, errors.New("mongodb: database name is required")
	}

	opts := options.Client().ApplyURI(cfg.URI)
	if cfg.AppName != "" {
		opts.SetAppName(cfg.AppName)
	}
	client, err := mongo.Connect(opts)
	if err != nil {
		// Driver option errors can echo parts of the URI, so don't wrap them.
		return nil, errors.New("mongodb: invalid client configuration (check MONGODB_URI)")
	}

	c := &Client{client: client, db: client.Database(cfg.Database)}
	if err := c.Ping(ctx); err != nil {
		_ = client.Disconnect(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("mongodb: ping: %w", err)
	}
	return c, nil
}

// Database returns the application database.
func (c *Client) Database() *mongo.Database { return c.db }

// Ping checks connectivity to the primary.
func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx, readpref.Primary())
}

// Close disconnects the client.
func (c *Client) Close(ctx context.Context) error {
	return c.client.Disconnect(ctx)
}

// IndexSet declares the indexes a repository relies on for one collection.
type IndexSet struct {
	Collection string
	Indexes    []mongo.IndexModel
}

// EnsureIndexes creates any missing indexes. It is idempotent: existing
// identical indexes are left untouched.
func (c *Client) EnsureIndexes(ctx context.Context, sets ...IndexSet) error {
	for _, set := range sets {
		if len(set.Indexes) == 0 {
			continue
		}
		if _, err := c.db.Collection(set.Collection).Indexes().CreateMany(ctx, set.Indexes); err != nil {
			return fmt.Errorf("mongodb: ensure indexes on %s: %w", set.Collection, err)
		}
	}
	return nil
}

// TranslateError maps driver errors to package sentinels, keeping the original
// error in the chain.
func TranslateError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mongo.ErrNoDocuments):
		return ErrNotFound
	case mongo.IsDuplicateKeyError(err):
		return fmt.Errorf("%w: %w", ErrDuplicate, err)
	default:
		return err
	}
}

// ParseID converts an API-facing hex string into an ObjectID.
func ParseID(hex string) (bson.ObjectID, error) {
	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		return bson.ObjectID{}, ErrInvalidID
	}
	return id, nil
}
