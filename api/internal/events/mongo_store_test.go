package events

import (
	"context"
	"reflect"
	"slices"
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

func TestIntegrationRecordAndList(t *testing.T) {
	store, db := newTestMongoStore(t)
	svc := NewService(ServiceOptions{Store: store, Now: func() time.Time { return testNow }})
	ctx := context.Background()
	base := testNow.Add(-24 * time.Hour)

	record := []Event{
		{HouseholdID: hhA, UserID: userA, Type: TypeRecipeRated, RecipeID: recipeA, OccurredAt: base, Payload: RecipeRated{Score: 4, PreviousScore: 2, Tags: []string{"kid-favorite"}}},
		{HouseholdID: hhA, Type: TypeImportCompleted, OccurredAt: base.Add(time.Minute), Payload: ImportCompleted{Source: "hellofresh", Created: 2, Unchanged: 1}},
		{HouseholdID: hhA, UserID: userA, Type: TypeGroceryItemChecked, Week: "2026-W38", OccurredAt: base.Add(2 * time.Minute), Payload: GroceryItemChecked{Name: "Limes", Checked: false}},
		{HouseholdID: hhA, UserID: userA, Type: TypeRecipeCooked, RecipeID: recipeA, OccurredAt: base.Add(3 * time.Minute), Source: SourceClient},
		{HouseholdID: hhB, UserID: userA, Type: TypeRecipeCooked, RecipeID: recipeB, OccurredAt: base.Add(4 * time.Minute)},
	}
	for _, e := range record {
		if err := svc.Record(ctx, e); err != nil {
			t.Fatalf("Record(%s) error = %v", e.Type, err)
		}
	}

	all, err := store.List(ctx, Query{HouseholdID: hhA})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("household A events = %+v", all)
	}
	for i, e := range all {
		want := record[i]
		if want.Source == "" {
			want.Source = SourceAPI
		}
		if want.Payload == nil {
			p, _ := typeSpecs[want.Type].decode(nil, nil)
			want.Payload = p
		}
		want.ID, want.RecordedAt = e.ID, testNow
		if !reflect.DeepEqual(e, want) {
			t.Errorf("event %d round trip:\n got %+v\nwant %+v", i, e, want)
		}
	}

	// Operator events store no userId and no recipeId rather than zero IDs.
	raw, err := db.Collection(Collection).FindOne(ctx, bson.D{{Key: "type", Value: string(TypeImportCompleted)}}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"userId", "recipeId", "clientEventId", "week"} {
		if _, err := raw.LookupErr(field); err == nil {
			t.Errorf("import event stores %s: %s", field, raw)
		}
	}

	byRecipe, err := store.List(ctx, Query{HouseholdID: hhA, RecipeID: recipeA, Types: []Type{TypeRecipeCooked}})
	if err != nil || len(byRecipe) != 1 || byRecipe[0].Type != TypeRecipeCooked {
		t.Errorf("recipe query = %+v, %v", byRecipe, err)
	}
	since, err := store.List(ctx, Query{HouseholdID: hhA, Since: base.Add(2 * time.Minute), Limit: 1})
	if err != nil || len(since) != 1 || since[0].Type != TypeGroceryItemChecked {
		t.Errorf("since query = %+v, %v", since, err)
	}
	if none, err := store.List(ctx, Query{HouseholdID: hhA, RecipeID: "not-an-id"}); err != nil || len(none) != 0 {
		t.Errorf("malformed recipe query = %+v, %v", none, err)
	}
}

func TestIntegrationClientEventIDsAreUnique(t *testing.T) {
	store, _ := newTestMongoStore(t)
	ctx := context.Background()
	event := func(userID, key string) Event {
		return Event{HouseholdID: hhA, UserID: userID, Type: TypeRecipeViewed, RecipeID: recipeA, Payload: RecipeViewed{},
			ClientEventID: key, OccurredAt: testNow, RecordedAt: testNow, Source: SourceClient}
	}

	inserted, dups, err := store.Insert(ctx, []Event{event(userA, "k1"), event(userA, "k1"), event(userA, ""), event(userA, ""), event(userA2, "k1")})
	if err != nil || inserted != 4 || dups != 1 {
		t.Fatalf("Insert() = %d inserted, %d duplicates, %v; want 4, 1", inserted, dups, err)
	}
	inserted, dups, err = store.Insert(ctx, []Event{event(userA, "k1"), event(userA, "k2")})
	if err != nil || inserted != 1 || dups != 1 {
		t.Fatalf("retry Insert() = %d inserted, %d duplicates, %v; want 1, 1", inserted, dups, err)
	}
	if all, _ := store.List(ctx, Query{HouseholdID: hhA}); len(all) != 5 {
		t.Errorf("stored %d events, want 5", len(all))
	}

	if _, _, err := store.Insert(ctx, []Event{event("not-a-user", "")}); err == nil {
		t.Error("malformed userId was stored")
	}
}

func TestIntegrationIngest(t *testing.T) {
	store, _ := newTestMongoStore(t)
	svc, _ := newTestService(store)
	ctx := context.Background()
	cooked := clientCooked(-time.Minute)
	cooked.ClientEventID = "retry-me"

	for attempt, want := range []IngestResult{{Accepted: 1}, {Duplicates: 1}} {
		res, err := svc.Ingest(ctx, memberOf(hhA, userA), []ClientEvent{cooked})
		if err != nil || res.Accepted != want.Accepted || res.Duplicates != want.Duplicates || len(res.Rejected) != 0 {
			t.Fatalf("attempt %d = %+v, %v; want %+v", attempt, res, err, want)
		}
	}
	got, err := store.List(ctx, Query{HouseholdID: hhA})
	if err != nil || len(got) != 1 || got[0].Payload != (RecipeCooked{Date: "2026-09-14", Servings: 4}) || got[0].UserID != userA {
		t.Errorf("stored = %+v, %v", got, err)
	}
}
