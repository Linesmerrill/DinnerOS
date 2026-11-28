package migrations

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/starterdescs"
)

const mealKitWords = "Are you the type of person who only goes to baseball games for that huge hoagie? This week, our chefs did it."

func TestStarterDescriptionsNeedAStarterHousehold(t *testing.T) {
	if m := StarterDescriptions("", time.Now); m.NotReady == "" || m.Run != nil {
		t.Fatalf("no household: %+v", m)
	}
	if m := StarterDescriptions("6ae7"+strings.Repeat("0", 20), time.Now); m.NotReady != "" {
		t.Fatalf("household given: %q", m.NotReady)
	}
}

// The embedded words parse, so the migration can't fail on its own file.
func TestTheShippedDescriptionsParse(t *testing.T) {
	list, err := starterdescs.Parse(starterdescs.Build122)
	if err != nil || len(list) < 400 {
		t.Fatalf("Build122: %d descriptions, %v", len(list), err)
	}
}

func TestIntegrationStarterDescriptionsMigration(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	db := client.Database()
	recipes := db.Collection("recipes")
	entries := db.Collection(catalog.RecipesCollection)
	descs, err := starterdescs.Parse(starterdescs.Build122)
	if err != nil {
		t.Fatal(err)
	}
	var name, fresh string
	for _, d := range descs {
		if d.Name == "Pork Sausage & Roasted Pepper Pasta" {
			name, fresh = d.Name, d.Description
		}
	}
	if fresh == "" {
		t.Fatal("no description for the pasta")
	}
	starter, home, ordered, edited, sharer := bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID()
	ids := map[string]bson.ObjectID{}
	rec := func(key string, hh bson.ObjectID, source, srcID, desc string, extra ...bson.E) any {
		ids[key] = bson.NewObjectID()
		return append(bson.D{{Key: "_id", Value: ids[key]}, {Key: "householdId", Value: hh}, {Key: "source", Value: source},
			{Key: "sourceRecipeId", Value: srcID}, {Key: "catalogKey", Value: source + ":" + srcID},
			{Key: "name", Value: name}, {Key: "description", Value: desc}}, extra...)
	}
	if _, err := recipes.InsertMany(ctx, []any{
		rec("starter", starter, "hellofresh", "p1", mealKitWords),
		rec("copy", home, "hellofresh", "p1", mealKitWords),
		// A household's own meal-kit import of the same dish: it has order history.
		rec("ordered", ordered, "hellofresh", "p1", mealKitWords,
			bson.E{Key: "orderWeeks", Value: bson.A{"2025-W10"}}, bson.E{Key: "timesOrdered", Value: 1}),
		rec("edited", edited, "hellofresh", "p1", "Our Friday pasta."),
		// Someone's link import of the same dish, shared to Shared Recipes.
		rec("shared", sharer, "import", "neighbor-5", mealKitWords),
	}); err != nil {
		t.Fatal(err)
	}
	ids["starterEntry"], ids["importEntry"] = bson.NewObjectID(), bson.NewObjectID()
	if _, err := entries.InsertMany(ctx, []any{
		bson.D{{Key: "_id", Value: ids["starterEntry"]}, {Key: "catalogKey", Value: "hellofresh:p1"}, {Key: "name", Value: name}, {Key: "description", Value: mealKitWords}},
		bson.D{{Key: "_id", Value: ids["importEntry"]}, {Key: "catalogKey", Value: "import:neighbor-5"}, {Key: "name", Value: name}, {Key: "description", Value: mealKitWords}},
	}); err != nil {
		t.Fatal(err)
	}

	day := time.Date(2026, 11, 28, 14, 0, 0, 0, time.UTC)
	now := func() time.Time { return day }
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	if err := Run(ctx, db, All(starter.Hex(), now), log, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"migration starter-descriptions-768: backed up 5 recipes to recipes_backup_20261128_desc768 and 2 Shared Recipes entries to recipe_catalog_backup_20261128_desc768",
		"migration starter-descriptions-768 done: backups recipes_backup_20261128_desc768 (5 docs) and recipe_catalog_backup_20261128_desc768 (2 docs); " +
			"updated 1 starters, 1 copies and 1 Shared Recipes entries; left alone 3 imported or shared recipes and 1 Shared Recipes entries; 0 starters have no new description",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs = %q\nwant %q", logs.String(), want)
		}
	}
	desc := func(coll string, id bson.ObjectID) string {
		var d struct {
			Description string `bson:"description"`
		}
		if err := db.Collection(coll).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&d); err != nil {
			t.Fatal(err)
		}
		return d.Description
	}
	for _, c := range []struct{ key, coll, want string }{
		{"starter", "recipes", fresh},
		{"copy", "recipes", fresh},
		{"starterEntry", catalog.RecipesCollection, fresh},
		{"ordered", "recipes", mealKitWords},
		{"edited", "recipes", "Our Friday pasta."},
		{"shared", "recipes", mealKitWords},
		{"importEntry", catalog.RecipesCollection, mealKitWords},
	} {
		if got := desc(c.coll, ids[c.key]); got != c.want {
			t.Errorf("%s = %q, want %q", c.key, got, c.want)
		}
	}
	// The backups hold the words from before.
	if got := desc("recipes_backup_20261128_desc768", ids["starter"]); got != mealKitWords {
		t.Errorf("backed-up starter = %q", got)
	}
	if got := desc("recipe_catalog_backup_20261128_desc768", ids["starterEntry"]); got != mealKitWords {
		t.Errorf("backed-up entry = %q", got)
	}
	var row bson.M
	if err := db.Collection(Collection).FindOne(ctx, bson.D{{Key: "_id", Value: StarterDescriptionsID}}).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row["status"] != "done" {
		t.Fatalf("row = %v", row)
	}

	// A later start, even a day later, changes nothing and makes no backup.
	if _, err := recipes.UpdateByID(ctx, ids["copy"], bson.D{{Key: "$set", Value: bson.D{{Key: "description", Value: mealKitWords}}}}); err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	later := func() time.Time { return day.Add(24 * time.Hour) }
	if err := Run(ctx, db, All(starter.Hex(), later), log, later); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "migration starter-descriptions-768 not run: already done (finished 2026-11-28T14:00:00Z)") {
		t.Errorf("second start logs = %q", logs.String())
	}
	if got := desc("recipes", ids["copy"]); got != mealKitWords {
		t.Errorf("second start wrote: %q", got)
	}
	names, err := db.ListCollectionNames(ctx, bson.D{{Key: "name", Value: bson.D{{Key: "$regex", Value: "_backup_"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("backups = %v", names)
	}
}

// A start after a failure keeps the backup it made, which holds the words
// from before, rather than copying words it may have changed.
func TestIntegrationAFailedRunRetriesAndKeepsItsBackup(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	db := client.Database()
	starter, id := bson.NewObjectID(), bson.NewObjectID()
	if _, err := db.Collection("recipes").InsertOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "householdId", Value: starter},
		{Key: "name", Value: "Pork Sausage & Roasted Pepper Pasta"}, {Key: "description", Value: "Already new."}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection("recipes_backup_20261127_desc768").InsertOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "description", Value: mealKitWords}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection(Collection).InsertOne(ctx, bson.D{{Key: "_id", Value: StarterDescriptionsID}, {Key: "status", Value: "failed"},
		{Key: "backup_recipes", Value: "recipes_backup_20261127_desc768"}}); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 11, 28, 14, 0, 0, 0, time.UTC) }
	var logs bytes.Buffer
	if err := Run(ctx, db, All(starter.Hex(), now), slog.New(slog.NewTextHandler(&logs, nil)), now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "backed up 1 recipes to recipes_backup_20261127_desc768 and 0 Shared Recipes entries to recipe_catalog_backup_20261128_desc768") {
		t.Errorf("logs = %q", logs.String())
	}
	var b struct {
		Description string `bson:"description"`
	}
	if err := db.Collection("recipes_backup_20261127_desc768").FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&b); err != nil || b.Description != mealKitWords {
		t.Errorf("kept backup = %q, %v", b.Description, err)
	}
}

// Two starts at once: the second sees the first's row and leaves it; a row
// left running by a stopped start is taken over after a while.
func TestIntegrationClaim(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	coll := client.Database().Collection(Collection)
	at := time.Date(2026, 11, 28, 14, 0, 0, 0, time.UTC)
	if rec, _, err := claim(ctx, coll, "m", at); err != nil || rec == nil {
		t.Fatalf("first claim: %v %v", rec, err)
	}
	if rec, why, err := claim(ctx, coll, "m", at.Add(time.Minute)); err != nil || rec != nil || why != "another start is running it" {
		t.Fatalf("second claim: %v %q %v", rec, why, err)
	}
	if rec, _, err := claim(ctx, coll, "m", at.Add(staleAfter+time.Minute)); err != nil || rec == nil {
		t.Fatalf("stale claim: %v %v", rec, err)
	}
}
