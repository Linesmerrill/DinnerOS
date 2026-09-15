package substitutes

import (
	"context"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newTestMongoClient(t *testing.T) *mongodb.Client {
	t.Helper()
	client := mongotest.Client(t)
	for range 2 { // applying indexes twice is a no-op
		if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	return client
}

func TestIntegrationIndexes(t *testing.T) {
	db := newTestMongoClient(t).Database()
	ctx := context.Background()
	for _, set := range Indexes() {
		specs, err := db.Collection(set.Collection).Indexes().ListSpecifications(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, s := range specs {
			names = append(names, s.Name)
		}
		for _, model := range set.Indexes {
			var opts options.IndexOptions
			for _, apply := range model.Options.List() {
				if err := apply(&opts); err != nil {
					t.Fatal(err)
				}
			}
			if opts.Name == nil || !slices.Contains(names, *opts.Name) {
				t.Errorf("%s: index %v missing (have %v)", set.Collection, model.Keys, names)
			}
		}
	}
}

func TestIntegrationStoreContract(t *testing.T) {
	runStoreContract(t, NewMongoStore(newTestMongoClient(t).Database()))
}

// countingStore counts specialty upserts so the sync test can prove an
// unchanged sync writes nothing.
type countingStore struct {
	*MongoStore
	upserts int
}

func (c *countingStore) UpsertSpecialty(ctx context.Context, sp Specialty) error {
	c.upserts++
	return c.MongoStore.UpsertSpecialty(ctx, sp)
}

func TestIntegrationSeedSyncIsIdempotent(t *testing.T) {
	client := newTestMongoClient(t)
	store := &countingStore{MongoStore: NewMongoStore(client.Database())}
	runSeedSync(t, store, func() int { return store.upserts })

	// The stored document keeps exact amounts with a number copy.
	var doc struct {
		Key       string    `bson:"key"`
		CreatedAt time.Time `bson:"createdAt"`
		Options   []struct {
			ID  string `bson:"id"`
			Per struct {
				Quantity      string  `bson:"quantity"`
				QuantityValue float64 `bson:"quantityValue"`
			} `bson:"per"`
		} `bson:"options"`
	}
	err := client.Database().Collection(SpecialtiesCollection).FindOne(context.Background(), bson.D{{Key: "slug", Value: "tex-mex-paste"}}).Decode(&doc)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Key != "tex mex paste" || doc.CreatedAt.IsZero() || len(doc.Options) == 0 || doc.Options[0].ID != "tex-mex-paste.store" ||
		doc.Options[0].Per.Quantity != "1" || doc.Options[0].Per.QuantityValue != 1 {
		t.Errorf("stored document = %+v", doc)
	}
}

func TestIntegrationEnsureSeed(t *testing.T) {
	db := newTestMongoClient(t).Database()
	store := NewMongoStore(db)
	ctx := context.Background()
	for range 2 {
		if err := EnsureSeed(ctx, store, nil); err != nil {
			t.Fatalf("EnsureSeed() error = %v", err)
		}
	}
	seed, _ := LoadSeed()
	n, err := db.Collection(SpecialtiesCollection).CountDocuments(ctx, bson.D{})
	if err != nil || int(n) != len(seed.Specialties) {
		t.Errorf("specialties = %d, %v; want %d", n, err, len(seed.Specialties))
	}
}
