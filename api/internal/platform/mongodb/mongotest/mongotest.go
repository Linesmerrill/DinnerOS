// Package mongotest provides a throwaway MongoDB database for integration
// tests. Tests skip unless MONGODB_TEST_URI is set.
package mongotest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Client connects to MONGODB_TEST_URI with a uniquely named database that is
// dropped when the test finishes. It skips the test when no URI is configured.
func Client(t testing.TB) *mongodb.Client {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set; skipping MongoDB integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongodb.Connect(ctx, mongodb.Config{
		URI:      uri,
		Database: fmt.Sprintf("dinneros_test_%d", time.Now().UnixNano()),
		AppName:  "dinneros-test",
	})
	if err != nil {
		t.Fatalf("mongodb.Connect() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Database().Drop(ctx)
		_ = client.Close(ctx)
	})
	return client
}
