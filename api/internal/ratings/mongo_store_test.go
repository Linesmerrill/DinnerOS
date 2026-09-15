package ratings

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

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
	specs, err := db.Collection(Collection).Indexes().ListSpecifications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range Indexes()[0].Indexes {
		var opts options.IndexOptions
		for _, apply := range model.Options.List() {
			if err := apply(&opts); err != nil {
				t.Fatal(err)
			}
		}
		i := slices.IndexFunc(specs, func(s mongo.IndexSpecification) bool { return opts.Name != nil && s.Name == *opts.Name })
		if i < 0 {
			t.Fatalf("index %v missing", model.Keys)
		}
		if unique := specs[i].Unique; opts.Unique != nil && *opts.Unique && (unique == nil || !*unique) {
			t.Errorf("index %s is not unique", specs[i].Name)
		}
	}
}

func TestIntegrationUpsertAndDelete(t *testing.T) {
	store, db := newTestMongoStore(t)
	ctx := context.Background()
	created := time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)
	first := Rating{HouseholdID: hhA, RecipeID: tacos, UserID: userAda, Score: 3, Comment: "ok", Tags: []Tag{TagTooSpicy}, CreatedAt: created, UpdatedAt: created}

	saved, previous, err := store.Upsert(ctx, first)
	if err != nil || previous != nil || saved.ID == "" {
		t.Fatalf("first Upsert() = %+v, %+v, %v", saved, previous, err)
	}
	list, err := store.ListForRecipe(ctx, hhA, tacos)
	if err != nil || len(list) != 1 || !reflect.DeepEqual(list[0], saved) {
		t.Fatalf("stored = %+v, %v; want %+v", list, err, saved)
	}

	later := created.Add(time.Hour)
	saved2, previous, err := store.Upsert(ctx, Rating{HouseholdID: hhA, RecipeID: tacos, UserID: userAda, Score: 5, CreatedAt: later, UpdatedAt: later})
	if err != nil || previous == nil || !reflect.DeepEqual(*previous, saved) {
		t.Fatalf("second Upsert() previous = %+v, %v; want %+v", previous, err, saved)
	}
	want := Rating{ID: saved.ID, HouseholdID: hhA, RecipeID: tacos, UserID: userAda, Score: 5, CreatedAt: created, UpdatedAt: later}
	if !reflect.DeepEqual(saved2, want) {
		t.Errorf("second Upsert() = %+v, want %+v", saved2, want)
	}
	if list, _ := store.ListForRecipe(ctx, hhA, tacos); len(list) != 1 || !reflect.DeepEqual(list[0], want) {
		t.Errorf("after re-rating = %+v; want comment and tags cleared, ID and createdAt kept", list)
	}

	// The unique index backs one rating per member.
	_, err = db.Collection(Collection).InsertOne(ctx, bson.D{
		{Key: "householdId", Value: mustOID(t, hhA)}, {Key: "recipeId", Value: mustOID(t, tacos)}, {Key: "userId", Value: mustOID(t, userAda)},
	})
	if !mongo.IsDuplicateKeyError(err) {
		t.Errorf("duplicate insert error = %v, want duplicate key", err)
	}

	deleted, err := store.Delete(ctx, hhA, tacos, userAda)
	if err != nil || !reflect.DeepEqual(deleted, want) {
		t.Errorf("Delete() = %+v, %v", deleted, err)
	}
	if _, err := store.Delete(ctx, hhA, tacos, userAda); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete() error = %v, want ErrNotFound", err)
	}
	if _, err := store.Delete(ctx, hhA, "nope", userAda); !errors.Is(err, ErrNotFound) {
		t.Errorf("malformed Delete() error = %v, want ErrNotFound", err)
	}
}

func TestIntegrationConcurrentFirstRatings(t *testing.T) {
	store, db := newTestMongoStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := store.Upsert(ctx, Rating{HouseholdID: hhA, RecipeID: curry, UserID: userAlan, Score: i%5 + 1, CreatedAt: now, UpdatedAt: now})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Upsert() error = %v", err)
		}
	}
	if n, err := db.Collection(Collection).CountDocuments(ctx, bson.D{}); err != nil || n != 1 {
		t.Errorf("stored %d ratings (%v), want 1", n, err)
	}
}

func TestIntegrationSummariesAndService(t *testing.T) {
	store, _ := newTestMongoStore(t)
	ctx := context.Background()
	svc := NewService(ServiceOptions{Store: store, Recipes: testRecipes, Users: testUsers})

	rate := func(householdID, userID, recipeID string, score int) {
		t.Helper()
		m := member(householdID, userID)
		if _, err := svc.Rate(ctx, m, recipeID, RateInput{Score: score, Tags: []string{"make-again"}}); err != nil {
			t.Fatalf("Rate() error = %v", err)
		}
	}
	rate(hhA, userAda, tacos, 5)
	rate(hhA, userAlan, tacos, 4)
	rate(hhA, userAlan, tacos, 2) // replaces Alan's 4
	rate(hhA, userAlan, curry, 3)
	rate(hhB, userBob, bobsStew, 1)

	sums, err := svc.Summaries(ctx, hhA, userAda, []string{tacos, curry, bobsStew, "not-an-id"})
	if err != nil {
		t.Fatal(err)
	}
	if s := sums[tacos]; s.Count != 2 || s.Sum != 7 || s.Mine == nil || s.Mine.Score != 5 || !slices.Equal(s.Mine.Tags, []Tag{TagMakeAgain}) {
		t.Errorf("tacos = %+v", s)
	}
	if s := sums[curry]; s.Count != 1 || s.Mine != nil {
		t.Errorf("curry = %+v; Ada hasn't rated it", s)
	}
	if s := sums[bobsStew]; s.Count != 0 {
		t.Errorf("another household's recipe leaked into the summary: %+v", s)
	}

	list, summary, err := svc.List(ctx, member(hhA, userAlan), tacos)
	if err != nil || len(list) != 2 || list[0].DisplayName != "Alan" || list[0].Score != 2 || summary.Mine == nil || summary.Mine.UserID != userAlan {
		t.Errorf("List() = %+v, %+v, %v", list, summary, err)
	}
}

func mustOID(t *testing.T, hex string) bson.ObjectID {
	t.Helper()
	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
