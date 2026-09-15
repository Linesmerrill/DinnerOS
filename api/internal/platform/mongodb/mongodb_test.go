package mongodb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// testClient connects to MONGODB_TEST_URI using a throwaway database, or skips
// the test when no test server is configured.
func testClient(t *testing.T) *Client {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set; skipping MongoDB integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := Connect(ctx, Config{
		URI:      uri,
		Database: fmt.Sprintf("dinneros_test_%d", time.Now().UnixNano()),
		AppName:  "dinneros-test",
	})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Database().Drop(ctx)
		_ = client.Close(ctx)
	})
	return client
}

func TestConnectRequiresDatabase(t *testing.T) {
	_, err := Connect(context.Background(), Config{URI: "mongodb://localhost:27017"})
	if err == nil || !strings.Contains(err.Error(), "database name") {
		t.Fatalf("Connect() error = %v, want database name requirement", err)
	}
}

func TestConnectUnreachableServerFailsWithinContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := Connect(ctx, Config{URI: "mongodb://user:hunter2@127.0.0.1:1/?directConnection=true", Database: "x"})
	if err == nil {
		t.Fatal("Connect() error = nil, want failure")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Connect took %v, want it bounded by the context", elapsed)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks credentials: %v", err)
	}
}

func TestTranslateError(t *testing.T) {
	if TranslateError(nil) != nil {
		t.Error("TranslateError(nil) != nil")
	}
	if !errors.Is(TranslateError(mongo.ErrNoDocuments), ErrNotFound) {
		t.Error("ErrNoDocuments not translated to ErrNotFound")
	}
	other := errors.New("boom")
	if TranslateError(other) != other {
		t.Error("unrelated error was changed")
	}
}

func TestParseID(t *testing.T) {
	id := bson.NewObjectID()
	got, err := ParseID(id.Hex())
	if err != nil || got != id {
		t.Errorf("ParseID(%q) = %v, %v", id.Hex(), got, err)
	}
	for _, bad := range []string{"", "nope", "zzzzzzzzzzzzzzzzzzzzzzzz", id.Hex() + "00"} {
		if _, err := ParseID(bad); !errors.Is(err, ErrInvalidID) {
			t.Errorf("ParseID(%q) error = %v, want ErrInvalidID", bad, err)
		}
	}
}

func TestIntegrationPing(t *testing.T) {
	client := testClient(t)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestIntegrationEnsureIndexesAndErrorTranslation(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	set := IndexSet{
		Collection: "identities",
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "provider", Value: 1}, {Key: "subject", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("provider_subject_unique"),
		}},
	}
	for i := range 2 {
		if err := client.EnsureIndexes(ctx, set, IndexSet{Collection: "empty"}); err != nil {
			t.Fatalf("EnsureIndexes() run %d error = %v", i+1, err)
		}
	}

	coll := client.Database().Collection("identities")
	doc := bson.D{{Key: "provider", Value: "apple"}, {Key: "subject", Value: "001"}}
	if _, err := coll.InsertOne(ctx, doc); err != nil {
		t.Fatalf("InsertOne() error = %v", err)
	}
	_, err := coll.InsertOne(ctx, doc)
	if !errors.Is(TranslateError(err), ErrDuplicate) {
		t.Errorf("duplicate insert error = %v, want ErrDuplicate", TranslateError(err))
	}

	err = coll.FindOne(ctx, bson.D{{Key: "provider", Value: "google"}}).Err()
	if !errors.Is(TranslateError(err), ErrNotFound) {
		t.Errorf("missing document error = %v, want ErrNotFound", TranslateError(err))
	}
}
