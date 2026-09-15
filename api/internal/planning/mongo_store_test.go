package planning

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
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

func objectID(t *testing.T, hex string) bson.ObjectID {
	t.Helper()
	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// rawPlan is the stored document, decoded loosely to check its shape.
type rawPlan struct {
	Week      string   `bson:"week"`
	StartDate string   `bson:"startDate"`
	Status    string   `bson:"status"`
	Entries   []bson.M `bson:"entries"`
}

func loadRawPlan(t *testing.T, db *mongo.Database, householdID, week string) rawPlan {
	t.Helper()
	var raw rawPlan
	filter := bson.D{{Key: "householdId", Value: objectID(t, householdID)}, {Key: "week", Value: week}}
	if err := db.Collection(PlansCollection).FindOne(context.Background(), filter).Decode(&raw); err != nil {
		t.Fatalf("load raw plan: %v", err)
	}
	return raw
}

func TestIntegrationPlanUniqueIndex(t *testing.T) {
	_, db := newTestMongoStore(t)
	ctx := context.Background()
	specs, err := db.Collection(PlansCollection).Indexes().ListSpecifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range specs {
		if s.Name == "householdId_week_unique" {
			found = s.Unique != nil && *s.Unique
		}
	}
	if !found {
		t.Fatalf("unique householdId_week index missing: %+v", specs)
	}

	doc := bson.D{{Key: "householdId", Value: objectID(t, hhAda)}, {Key: "week", Value: testWeek}}
	if _, err := db.Collection(PlansCollection).InsertOne(ctx, doc); err != nil {
		t.Fatal(err)
	}
	_, err = db.Collection(PlansCollection).InsertOne(ctx, doc)
	if !errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		t.Errorf("second plan for the same week: error = %v, want duplicate key", err)
	}
	other := bson.D{{Key: "householdId", Value: objectID(t, hhBob)}, {Key: "week", Value: testWeek}}
	if _, err := db.Collection(PlansCollection).InsertOne(ctx, other); err != nil {
		t.Errorf("another household's plan for the same week: %v", err)
	}
}

func TestIntegrationPlanLifecycle(t *testing.T) {
	store, db := newTestMongoStore(t)
	svc, _ := newTestService(t, store)
	ctx := context.Background()

	if p, err := svc.Get(ctx, hhAda, testWeek); err != nil || !p.CreatedAt.IsZero() || p.Entries != nil {
		t.Fatalf("Get(unplanned) = %+v, %v", p, err)
	}
	p, tacos := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 2, Note: "extra lime"})
	if !p.CreatedAt.Equal(testNow) || !p.UpdatedAt.Equal(testNow) || p.Status != StatusDraft || p.HouseholdID != hhAda {
		t.Errorf("new plan = %+v", p)
	}
	if tacos.RecipeName != "Beef Tacos" || tacos.Day != Tuesday || tacos.AddedBy != userAda || !tacos.AddedAt.Equal(testNow) {
		t.Errorf("stored entry = %+v", tacos)
	}
	_, soup := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 4})

	raw := loadRawPlan(t, db, hhAda, testWeek)
	if raw.StartDate != testWeekStart || raw.Status != "draft" || len(raw.Entries) != 2 {
		t.Fatalf("raw plan = %+v", raw)
	}
	if raw.Entries[0]["day"] != "tue" || raw.Entries[0]["recipeId"] != objectID(t, recipeTacos) || raw.Entries[0]["note"] != "extra lime" {
		t.Errorf("raw tacos entry = %v", raw.Entries[0])
	}
	if _, has := raw.Entries[1]["day"]; has {
		t.Errorf("unscheduled entry stores a day: %v", raw.Entries[1])
	}

	// Clearing a field removes it; other entries are untouched.
	p, err := svc.UpdateEntry(ctx, hhAda, testWeek, tacos.ID, EntryChanges{Day: ptr(Day("")), Note: ptr("")})
	if err != nil {
		t.Fatalf("UpdateEntry() error = %v", err)
	}
	if e := p.Entries[0]; e.Day != "" || e.Note != "" || e.Servings != 2 || e.ID != tacos.ID {
		t.Errorf("updated entry = %+v", e)
	}
	raw = loadRawPlan(t, db, hhAda, testWeek)
	for _, field := range []string{"day", "note"} {
		if _, has := raw.Entries[0][field]; has {
			t.Errorf("cleared %s still stored: %v", field, raw.Entries[0])
		}
	}
	if p, err = svc.UpdateEntry(ctx, hhAda, testWeek, soup.ID, EntryChanges{Day: ptr(Saturday), Servings: ptr(2)}); err != nil || p.Entries[1].Day != Saturday || p.Entries[1].Servings != 2 {
		t.Errorf("UpdateEntry(soup) = %+v, %v", p.Entries, err)
	}
	if _, err := svc.UpdateEntry(ctx, hhAda, testWeek, "ffffffffffffffffffffffff", EntryChanges{Note: ptr("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEntry(unknown) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.UpdateEntry(ctx, hhAda, "2026-W40", soup.ID, EntryChanges{Note: ptr("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEntry(unplanned week) error = %v, want ErrNotFound", err)
	}

	// Finalizing locks entries until the plan is a draft again.
	if p, err = svc.SetStatus(ctx, hhAda, testWeek, "finalized"); err != nil || p.Status != StatusFinalized || len(p.Entries) != 2 || !p.CreatedAt.Equal(testNow) {
		t.Fatalf("SetStatus(finalized) = %+v, %v", p, err)
	}
	if _, _, err := svc.AddEntry(ctx, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Servings: 2}); !errors.Is(err, ErrFinalized) {
		t.Errorf("AddEntry(finalized) error = %v, want ErrFinalized", err)
	}
	if _, err := store.UpdateEntry(ctx, hhAda, mustWeek(t, testWeek), soup.ID, EntryChanges{Note: ptr("x")}, testNow); !errors.Is(err, ErrFinalized) {
		t.Errorf("UpdateEntry(finalized) error = %v, want ErrFinalized", err)
	}
	if _, err := svc.DeleteEntry(ctx, hhAda, userAda, testWeek, soup.ID); !errors.Is(err, ErrFinalized) {
		t.Errorf("DeleteEntry(finalized) error = %v, want ErrFinalized", err)
	}
	if _, err := svc.DeleteEntry(ctx, hhAda, userAda, testWeek, "ffffffffffffffffffffffff"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteEntry(finalized, unknown) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.SetStatus(ctx, hhAda, testWeek, "draft"); err != nil {
		t.Fatal(err)
	}

	if p, err = svc.DeleteEntry(ctx, hhAda, userAda, testWeek, tacos.ID); err != nil || len(p.Entries) != 1 || p.Entries[0].ID != soup.ID {
		t.Fatalf("DeleteEntry() = %+v, %v", p, err)
	}
	for _, id := range []string{tacos.ID, "not-an-id"} {
		if _, err := svc.DeleteEntry(ctx, hhAda, userAda, testWeek, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteEntry(%q) error = %v, want ErrNotFound", id, err)
		}
	}

	// Household isolation.
	if _, err := store.GetPlan(ctx, hhBob, mustWeek(t, testWeek)); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPlan(other household) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.DeleteEntry(ctx, hhBob, userAda, testWeek, soup.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteEntry(other household) error = %v, want ErrNotFound", err)
	}
	mustAdd(t, svc, hhBob, userBob, testWeek, NewEntry{RecipeID: recipeBobs, Servings: 2})

	// Summaries count entries per stored week.
	mustAdd(t, svc, hhAda, userAda, "2026-W40", NewEntry{RecipeID: recipeSalad, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W40", NewEntry{RecipeID: recipeTacos, Servings: 4})
	if _, err := svc.SetStatus(ctx, hhAda, "2026-W41", "finalized"); err != nil {
		t.Fatal(err)
	}
	items, err := svc.List(ctx, hhAda, "2026-W37", "2026-W41")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range items {
		got = append(got, fmt.Sprintf("%s:%s:%d:%t", s.Week, s.Status, s.EntryCount, s.UpdatedAt.IsZero()))
	}
	want := []string{"2026-W37:draft:0:true", "2026-W38:draft:1:false", "2026-W39:draft:0:true", "2026-W40:draft:2:false", "2026-W41:finalized:0:false"}
	if !slices.Equal(got, want) {
		t.Errorf("summaries = %v, want %v", got, want)
	}

	// The entry limit is part of the atomic push.
	w42 := mustWeek(t, "2026-W42")
	e := Entry{RecipeID: recipeTacos, RecipeName: "Beef Tacos", Servings: 2, AddedBy: userAda, AddedAt: testNow}
	if _, _, err := store.AddEntry(ctx, hhAda, w42, e, 1, testNow); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddEntry(ctx, hhAda, w42, e, 1, testNow); !errors.Is(err, ErrPlanFull) {
		t.Errorf("AddEntry(full) error = %v, want ErrPlanFull", err)
	}

	// The grocery list reads entries back from Mongo.
	g, err := svc.GroceryList(ctx, hhAda, "2026-W40")
	if err != nil || len(g.Categories) == 0 || len(g.Skipped) != 0 {
		t.Errorf("GroceryList() = %+v, %v", g, err)
	}
}

func TestIntegrationConcurrentEditsAllPersist(t *testing.T) {
	store, db := newTestMongoStore(t)
	ctx := context.Background()
	w := mustWeek(t, testWeek)
	const members = 16

	// Everyone adds to a week that doesn't exist yet, so the plan upserts race too.
	var wg sync.WaitGroup
	errs := make(chan error, members)
	for i := range members {
		wg.Go(func() {
			e := Entry{RecipeID: recipeTacos, RecipeName: fmt.Sprintf("Tacos %02d", i), Servings: 2, AddedBy: userAda, AddedAt: testNow}
			_, _, err := store.AddEntry(ctx, hhAda, w, e, MaxEntriesPerWeek, testNow)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent AddEntry() error = %v", err)
		}
	}
	p, err := store.GetPlan(ctx, hhAda, w)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range p.Entries {
		names = append(names, e.RecipeName)
	}
	slices.Sort(names)
	if len(names) != members || len(slices.Compact(slices.Clone(names))) != members {
		t.Fatalf("entries after concurrent adds = %v, want %d distinct", names, members)
	}
	if n, err := db.Collection(PlansCollection).CountDocuments(ctx, bson.D{}); err != nil || n != 1 {
		t.Errorf("plan documents = %d, %v; want 1", n, err)
	}

	// Concurrent edits of different entries and deletes of others all land.
	for i, e := range p.Entries {
		wg.Go(func() {
			var err error
			if i < members/2 {
				_, err = store.UpdateEntry(ctx, hhAda, w, e.ID, EntryChanges{Note: ptr(fmt.Sprintf("note %d", i)), Servings: ptr(4)}, testNow)
			} else {
				_, err = store.DeleteEntry(ctx, hhAda, w, e.ID, testNow)
			}
			if err != nil {
				t.Errorf("concurrent edit of entry %d: %v", i, err)
			}
		})
	}
	wg.Wait()
	final, err := store.GetPlan(ctx, hhAda, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Entries) != members/2 {
		t.Fatalf("entries after concurrent edits = %d, want %d", len(final.Entries), members/2)
	}
	for i, e := range final.Entries {
		if e.ID != p.Entries[i].ID || e.Note != fmt.Sprintf("note %d", i) || e.Servings != 4 {
			t.Errorf("entry %d = %+v, want note %d and 4 servings", i, e, i)
		}
	}
}
