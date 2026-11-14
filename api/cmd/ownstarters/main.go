// Command ownstarters replaces the starter recipe library's imported text and
// photos with DinnerOS's own (decision 684). It is a dry run unless -apply is
// given, and running it again changes nothing.
//
// Text: -rewrites names JSON files holding [{id, headline, description,
// steps}] for the starter household's recipes (kept out of git). Each recipe's
// headline, description and step text are replaced, its step photos and source
// link removed. Copies of the recipe in other households (same source and
// sourceRecipeId) get the same text when their steps still read as the
// original, so a household's own edits are never overwritten.
//
// Images: -images <base URL> with -image-ids <file of ids, one per line> sets
// each listed starter's photo to <base>/<id>.jpg, on the starter and on copies
// still showing the starter's original photo. -drop-photos removes the photo of
// any starter without a new one.
//
//	MONGODB_URI=... MONGODB_DATABASE=dinneros STARTER_RECIPES_HOUSEHOLD_ID=<id> \
//	  go run ./cmd/ownstarters -rewrites 'out-*.json'                  # dry run
//	  go run ./cmd/ownstarters -images https://dinneros.app/img/recipes -image-ids ids.txt -apply
//
// It never prints the URI or any recipe text.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ownstarters:", err)
		os.Exit(1)
	}
}

// Rewrite is one starter recipe's new words.
type Rewrite struct {
	ID          string   `json:"id"`
	Headline    string   `json:"headline"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
}

type step struct {
	Index    int    `bson:"index"`
	Text     string `bson:"text"`
	ImageURL string `bson:"imageUrl,omitempty"`
}

type recipeDoc struct {
	ID             bson.ObjectID `bson:"_id"`
	HouseholdID    bson.ObjectID `bson:"householdId"`
	Source         string        `bson:"source"`
	SourceRecipeID string        `bson:"sourceRecipeId"`
	ImageURL       string        `bson:"imageUrl"`
	Steps          []step        `bson:"steps"`
}

// Result counts what a run changed, or would.
type Result struct {
	Starters, Copies, SkippedEdited, Images, CopyImages, Dropped int
	Missing                                                      []string
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("ownstarters", flag.ContinueOnError)
	flags.SetOutput(out)
	rewrites := flags.String("rewrites", "", "glob of rewrite JSON files")
	images := flags.String("images", "", "base URL of the new photos, like https://dinneros.app/img/recipes")
	imageIDs := flags.String("image-ids", "", "file listing the starter ids that have a new photo")
	dropPhotos := flags.Bool("drop-photos", false, "remove the photo of starters without a new one")
	apply := flags.Bool("apply", false, "write the changes (default is a dry run)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	starterID := strings.TrimSpace(getenv("STARTER_RECIPES_HOUSEHOLD_ID"))
	starter, err := mongodb.ParseID(starterID)
	if err != nil {
		return errors.New("STARTER_RECIPES_HOUSEHOLD_ID must be the starter household's id")
	}
	if *rewrites == "" && *images == "" {
		return errors.New("give -rewrites, -images, or both")
	}
	if (*images == "") != (*imageIDs == "") {
		return errors.New("-images and -image-ids go together")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	uri := strings.TrimSpace(getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	database := strings.TrimSpace(getenv("MONGODB_DATABASE"))
	if database == "" {
		database = "dinneros"
	}
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-ownstarters"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.WithoutCancel(ctx)) }()
	coll := client.Database().Collection("recipes")

	var res Result
	if *rewrites != "" {
		rws, err := loadRewrites(*rewrites)
		if err != nil {
			return err
		}
		if err := applyText(ctx, coll, starter, rws, *apply, &res); err != nil {
			return err
		}
	}
	if *images != "" {
		ids, err := loadIDs(*imageIDs)
		if err != nil {
			return err
		}
		if err := applyImages(ctx, coll, starter, strings.TrimRight(*images, "/"), ids, *dropPhotos, *apply, &res); err != nil {
			return err
		}
	}
	verb := "would update"
	if *apply {
		verb = "updated"
	}
	fmt.Fprintf(out, "database %q: %s text of %d starters and %d copies (%d edited copies left alone); photos of %d starters and %d copies; %d photos dropped\n",
		database, verb, res.Starters, res.Copies, res.SkippedEdited, res.Images, res.CopyImages, res.Dropped)
	if len(res.Missing) > 0 {
		fmt.Fprintf(out, "  %d ids not found in the starter household: %s\n", len(res.Missing), strings.Join(res.Missing, ", "))
	}
	return nil
}

func loadRewrites(glob string) (map[string]Rewrite, error) {
	files, err := filepath.Glob(glob)
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no rewrite files match %q", glob)
	}
	out := map[string]Rewrite{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var list []Rewrite
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, r := range list {
			out[r.ID] = r
		}
	}
	return out, nil
}

func loadIDs(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ids := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if id := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sc.Text()), ".jpg")); id != "" {
			ids[id] = true
		}
	}
	return ids, sc.Err()
}

func sameSteps(a, b []step) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i].Text) != strings.TrimSpace(b[i].Text) {
			return false
		}
	}
	return true
}

// newSteps keeps each step's index and drops its photo.
func newSteps(old []step, text []string) bson.A {
	steps := bson.A{}
	for i, s := range old {
		steps = append(steps, bson.D{{Key: "index", Value: s.Index}, {Key: "text", Value: text[i]}})
	}
	return steps
}

func applyText(ctx context.Context, coll *mongo.Collection, starter bson.ObjectID, rws map[string]Rewrite, apply bool, res *Result) error {
	for id, rw := range rws {
		oid, err := mongodb.ParseID(id)
		if err != nil {
			return fmt.Errorf("bad id %q", id)
		}
		var doc recipeDoc
		err = coll.FindOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: starter}}).Decode(&doc)
		if errors.Is(err, mongo.ErrNoDocuments) {
			res.Missing = append(res.Missing, id)
			continue
		}
		if err != nil {
			return err
		}
		if len(rw.Steps) != len(doc.Steps) {
			return fmt.Errorf("%s: %d new steps for %d", id, len(rw.Steps), len(doc.Steps))
		}
		set := bson.D{{Key: "headline", Value: rw.Headline}, {Key: "description", Value: rw.Description},
			{Key: "steps", Value: newSteps(doc.Steps, rw.Steps)}}
		update := bson.D{{Key: "$set", Value: set}, {Key: "$unset", Value: bson.D{{Key: "sourceUrl", Value: ""}}}}
		res.Starters++
		if apply {
			if _, err := coll.UpdateByID(ctx, oid, update); err != nil {
				return err
			}
		}
		if doc.SourceRecipeID == "" {
			continue
		}
		cur, err := coll.Find(ctx, bson.D{{Key: "householdId", Value: bson.D{{Key: "$ne", Value: starter}}},
			{Key: "source", Value: doc.Source}, {Key: "sourceRecipeId", Value: doc.SourceRecipeID}})
		if err != nil {
			return err
		}
		var copies []recipeDoc
		if err := cur.All(ctx, &copies); err != nil {
			return err
		}
		for _, c := range copies {
			if !sameSteps(c.Steps, doc.Steps) {
				res.SkippedEdited++
				continue
			}
			res.Copies++
			if apply {
				if _, err := coll.UpdateByID(ctx, c.ID, bson.D{{Key: "$set", Value: bson.D{
					{Key: "headline", Value: rw.Headline}, {Key: "description", Value: rw.Description},
					{Key: "steps", Value: newSteps(c.Steps, rw.Steps)}}},
					{Key: "$unset", Value: bson.D{{Key: "sourceUrl", Value: ""}}}}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func applyImages(ctx context.Context, coll *mongo.Collection, starter bson.ObjectID, base string, ids map[string]bool, drop, apply bool, res *Result) error {
	cur, err := coll.Find(ctx, bson.D{{Key: "householdId", Value: starter}})
	if err != nil {
		return err
	}
	var starters []recipeDoc
	if err := cur.All(ctx, &starters); err != nil {
		return err
	}
	for _, s := range starters {
		id := s.ID.Hex()
		var update bson.D
		switch {
		case ids[id]:
			update = bson.D{{Key: "$set", Value: bson.D{{Key: "imageUrl", Value: base + "/" + id + ".jpg"}}}}
			if s.ImageURL == base+"/"+id+".jpg" {
				continue
			}
			res.Images++
		case drop && s.ImageURL != "" && !strings.HasPrefix(s.ImageURL, base+"/"):
			update = bson.D{{Key: "$unset", Value: bson.D{{Key: "imageUrl", Value: ""}}}}
			res.Dropped++
		default:
			continue
		}
		if apply {
			if _, err := coll.UpdateByID(ctx, s.ID, update); err != nil {
				return err
			}
		}
		if s.ImageURL == "" || s.SourceRecipeID == "" {
			continue
		}
		// Copies still showing the starter's original photo follow it.
		filter := bson.D{{Key: "householdId", Value: bson.D{{Key: "$ne", Value: starter}}},
			{Key: "source", Value: s.Source}, {Key: "sourceRecipeId", Value: s.SourceRecipeID}, {Key: "imageUrl", Value: s.ImageURL}}
		n, err := coll.CountDocuments(ctx, filter)
		if err != nil {
			return err
		}
		res.CopyImages += int(n)
		if apply && n > 0 {
			if _, err := coll.UpdateMany(ctx, filter, update); err != nil {
				return err
			}
		}
	}
	return nil
}
