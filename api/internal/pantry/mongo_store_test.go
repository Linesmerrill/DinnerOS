package pantry

import (
	"context"
	"slices"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newTestMongoStore(t *testing.T) (*MongoStore, *mongo.Database) {
	t.Helper()
	client := mongotest.Client(t)
	for range 2 { // applying indexes twice is a no-op
		if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	return NewMongoStore(client.Database()), client.Database()
}

func TestIntegrationIndexes(t *testing.T) {
	_, db := newTestMongoStore(t)
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
			if opts.Name == nil {
				t.Fatalf("index %v has no explicit name", model.Keys)
			}
			if !slices.Contains(names, *opts.Name) {
				t.Errorf("%s: index %q missing (have %v)", set.Collection, *opts.Name, names)
			}
		}
	}
}

func TestIntegrationStoreContract(t *testing.T) {
	store, _ := newTestMongoStore(t)
	runStoreContract(t, store)
}

func TestIntegrationStoredDocument(t *testing.T) {
	store, db := newTestMongoStore(t)
	ctx := context.Background()
	svc := NewService(store, newFakeCatalog("Olive Oil"))

	oil := mustAdd(t, svc, testHousehold, AddInput{Name: "Olive Oil", Quantity: "1 1/2", Unit: "cup", ExpiresOn: "2027-03-01"})
	free := mustAdd(t, svc, testHousehold, AddInput{Name: "Za'atar"})

	raw := func(id string) bson.M {
		t.Helper()
		oid, _ := bson.ObjectIDFromHex(id)
		var doc bson.M
		if err := db.Collection(ItemsCollection).FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	doc := raw(oil.ID)
	if _, ok := doc["ingredientId"].(bson.ObjectID); !ok {
		t.Errorf("ingredientId = %T, want ObjectID", doc["ingredientId"])
	}
	if _, ok := doc["householdId"].(bson.ObjectID); !ok {
		t.Errorf("householdId = %T, want ObjectID", doc["householdId"])
	}
	if doc["quantity"] != "3/2" || doc["quantityValue"] != 1.5 || doc["unit"] != "cup" || doc["expiresOn"] != "2027-03-01" || doc["status"] != "in_stock" || doc["version"] != int64(1) {
		t.Errorf("stored item = %v", doc)
	}
	for _, field := range []string{"ingredientId", "quantity", "quantityValue", "unit", "expiresOn", "note"} {
		if _, ok := raw(free.ID)[field]; ok {
			t.Errorf("free-text item stores empty %s", field)
		}
	}
	// Marking out removes the stored amount fields.
	if _, err := svc.SetStatuses(ctx, member(testHousehold), []StatusUpdate{{ItemID: oil.ID, Status: StatusOut}}); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"quantity", "quantityValue", "unit"} {
		if _, ok := raw(oil.ID)[field]; ok {
			t.Errorf("out item still stores %s", field)
		}
	}
}

func TestIntegrationConcurrentAddsMerge(t *testing.T) {
	store, _ := newTestMongoStore(t)
	svc := NewService(store, newFakeCatalog())
	ctx := context.Background()

	// Every failed attempt means another writer committed and finished, so
	// maxWriteAttempts concurrent adds of one ingredient must all succeed.
	writers := maxWriteAttempts
	var wg sync.WaitGroup
	created := make(chan bool, writers)
	errs := make(chan error, writers)
	for range writers {
		wg.Go(func() {
			_, c, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "Rice"})
			created <- c
			errs <- err
		})
	}
	wg.Wait()
	close(created)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Add() error = %v", err)
		}
	}
	n := 0
	for c := range created {
		if c {
			n++
		}
	}
	if count, _ := store.CountItems(ctx, testHousehold); n != 1 || count != 1 {
		t.Errorf("created %d, stored %d; want exactly one item", n, count)
	}
}

func TestIntegrationGroceryPantry(t *testing.T) {
	store, _ := newTestMongoStore(t)
	catalog := newFakeCatalog(groceryCatalog...)
	runGroceryPantryScenario(t, NewService(store, catalog), catalog)
}
