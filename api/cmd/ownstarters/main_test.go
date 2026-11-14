package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func TestIntegrationOwnStarters(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	coll := client.Database().Collection("recipes")
	starter, home, edited := bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID()
	starterRecipe, other := bson.NewObjectID(), bson.NewObjectID()
	oldSteps := bson.A{
		bson.D{{Key: "index", Value: 1}, {Key: "text", Value: "Old step one."}, {Key: "imageUrl", Value: "https://img.example/1.jpg"}},
		bson.D{{Key: "index", Value: 2}, {Key: "text", Value: "Old step two."}, {Key: "imageUrl", Value: "https://img.example/2.jpg"}},
	}
	doc := func(id, hh bson.ObjectID, srcID string, steps bson.A) bson.D {
		return bson.D{{Key: "_id", Value: id}, {Key: "householdId", Value: hh}, {Key: "source", Value: "hellofresh"},
			{Key: "sourceRecipeId", Value: srcID}, {Key: "sourceUrl", Value: "https://old.example/r"},
			{Key: "imageUrl", Value: "https://img.example/" + srcID + ".jpg"}, {Key: "headline", Value: "old"},
			{Key: "steps", Value: steps}}
	}
	editedSteps := bson.A{bson.D{{Key: "index", Value: 1}, {Key: "text", Value: "My own step."}}, bson.D{{Key: "index", Value: 2}, {Key: "text", Value: "Old step two."}}}
	if _, err := coll.InsertMany(ctx, []any{
		doc(starterRecipe, starter, "a", oldSteps),
		doc(other, starter, "b", oldSteps),
		doc(bson.NewObjectID(), home, "a", oldSteps),
		doc(bson.NewObjectID(), edited, "a", editedSteps),
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rw, _ := json.Marshal([]Rewrite{{ID: starterRecipe.Hex(), Headline: "new head", Description: "new desc", Steps: []string{"New one.", "New two."}}})
	if err := os.WriteFile(filepath.Join(dir, "out-00.json"), rw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ids.txt"), []byte(starterRecipe.Hex()+".jpg\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"MONGODB_URI": os.Getenv("MONGODB_TEST_URI"), "MONGODB_DATABASE": client.Database().Name(), "STARTER_RECIPES_HOUSEHOLD_ID": starter.Hex()}
	getenv := func(k string) string { return env[k] }
	args := []string{"-rewrites", filepath.Join(dir, "out-*.json"), "-images", "https://dinneros.app/img/recipes/", "-image-ids", filepath.Join(dir, "ids.txt"), "-drop-photos"}

	var dry bytes.Buffer
	if err := run(ctx, args, getenv, &dry); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dry.String(), "would update text of 1 starters and 1 copies (1 edited copies left alone); photos of 1 starters and 2 copies; 1 photos dropped") {
		t.Errorf("dry run = %q", dry.String())
	}
	var got bson.M
	_ = coll.FindOne(ctx, bson.D{{Key: "_id", Value: starterRecipe}}).Decode(&got)
	if got["headline"] != "old" {
		t.Fatal("the dry run wrote")
	}

	var out bytes.Buffer
	if err := run(ctx, append(args, "-apply"), getenv, &out); err != nil {
		t.Fatal(err)
	}
	type rec struct {
		Headline  string `bson:"headline"`
		ImageURL  string `bson:"imageUrl"`
		SourceURL string `bson:"sourceUrl"`
		Steps     []step `bson:"steps"`
	}
	read := func(filter bson.D) rec {
		var r rec
		if err := coll.FindOne(ctx, filter).Decode(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	want := "https://dinneros.app/img/recipes/" + starterRecipe.Hex() + ".jpg"
	s := read(bson.D{{Key: "_id", Value: starterRecipe}})
	if s.Headline != "new head" || s.ImageURL != want || s.SourceURL != "" || s.Steps[0].Text != "New one." || s.Steps[1].ImageURL != "" || s.Steps[1].Index != 2 {
		t.Errorf("starter = %+v", s)
	}
	c := read(bson.D{{Key: "householdId", Value: home}})
	if c.Headline != "new head" || c.ImageURL != want || c.Steps[1].Text != "New two." || c.Steps[0].ImageURL != "" {
		t.Errorf("copy = %+v", c)
	}
	e := read(bson.D{{Key: "householdId", Value: edited}})
	if e.Headline != "old" || e.Steps[0].Text != "My own step." || e.ImageURL != want {
		t.Errorf("edited copy = %+v (its words stay; its photo follows the starter)", e)
	}
	if o := read(bson.D{{Key: "_id", Value: other}}); o.ImageURL != "" {
		t.Errorf("starter without a new photo kept %q", o.ImageURL)
	}
	// Running it again changes nothing new.
	var again bytes.Buffer
	if err := run(ctx, append(args[:0:0], "-images", "https://dinneros.app/img/recipes", "-image-ids", filepath.Join(dir, "ids.txt"), "-drop-photos", "-apply"), getenv, &again); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again.String(), "photos of 0 starters and 0 copies; 0 photos dropped") {
		t.Errorf("second run = %q", again.String())
	}
}
