package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
)

// Description is one starter's description in DinnerOS's own words (build
// 124, Hark's review: "Pork Sausage & Roasted Pepper Pasta" still read "Are
// you the type of person who only goes to baseball games..." and "This week,
// our chefs..."). It names the starter by its name, so one file covers every
// starter of that name (the library has a few dishes twice) on any database.
// The words live in git (descriptions/).
type Description struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// mealKitPhrases are words a starter's description never uses: the meal
// kit's voice (its chefs, its weekly menu, the box at the door), sales
// pitches, and dashes. Each pattern is case-insensitive.
var mealKitPhrases = []struct {
	Name    string
	Pattern *regexp.Regexp
}{
	{"our chefs", regexp.MustCompile(`(?i)\bour\s+chefs?\b`)},
	{"chef", regexp.MustCompile(`(?i)\bchefs?\b`)},
	{"this week", regexp.MustCompile(`(?i)\bthis\s+week\b`)},
	{"shipping", regexp.MustCompile(`(?i)\bship(s|ped|ping|ment)?\b`)},
	{"delivery", regexp.MustCompile(`(?i)\bdeliver`)},
	{"box", regexp.MustCompile(`(?i)\bbox(es)?\b`)},
	{"meal kit", regexp.MustCompile(`(?i)\bmeal[\s-]*kits?\b`)},
	{"HelloFresh", regexp.MustCompile(`(?i)hello\s*fresh`)},
	{"em dash", regexp.MustCompile(`—`)},
	{"en dash", regexp.MustCompile(`–`)},
	{"hyphen used as a dash", regexp.MustCompile(`\s-+\s`)},
	{"Are you the type", regexp.MustCompile(`(?i)\bare\s+you\s+the\s+type\b`)},
	{"we or our", regexp.MustCompile(`(?i)\b(we|we['’]re|we['’]ve|we['’]ll|our|ours|us)\b`)},
	{"you or your", regexp.MustCompile(`(?i)\b(you|you['’]ll|you['’]re|you['’]ve|your|yours)\b`)},
	{"exclamation", regexp.MustCompile(`!`)},
	{"subscription", regexp.MustCompile(`(?i)\bsubscri`)},
	{"pre-portioned", regexp.MustCompile(`(?i)\bpre[\s-]*(portioned|measured)\b`)},
	{"recipe card", regexp.MustCompile(`(?i)\brecipe\s+cards?\b`)},
	{"on the menu", regexp.MustCompile(`(?i)\bon\s+the\s+menu\b`)},
	{"fan favorite", regexp.MustCompile(`(?i)\b(fan|customer)[\s-]+favou?rites?\b`)},
	{"knocks it out of the park", regexp.MustCompile(`(?i)out\s+of\s+the\s+park`)},
	{"taste buds", regexp.MustCompile(`(?i)\btaste\s*buds\b`)},
	{"to your door", regexp.MustCompile(`(?i)\b(your|the)\s+door(step)?\b`)},
	{"unboxing", regexp.MustCompile(`(?i)\bunbox`)},
}

// mealKitPhrase names the first meal-kit phrase s uses, or "".
func mealKitPhrase(s string) string {
	for _, p := range mealKitPhrases {
		if p.Pattern.MatchString(s) {
			return p.Name
		}
	}
	return ""
}

func nameKey(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// loadDescriptions reads a file of [Description], refusing an empty field, a
// name given twice, or words from mealKitPhrases.
func loadDescriptions(path string) ([]Description, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list []Description
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, d := range list {
		if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Description) == "" {
			return nil, fmt.Errorf("%s: description %d needs a name and a description", path, i+1)
		}
		if seen[nameKey(d.Name)] {
			return nil, fmt.Errorf("%s: %q is listed twice", path, d.Name)
		}
		seen[nameKey(d.Name)] = true
		if p := mealKitPhrase(d.Description); p != "" {
			return nil, fmt.Errorf("%s: %q uses %q", path, d.Name, p)
		}
	}
	return list, nil
}

type starterDesc struct {
	ID             bson.ObjectID `bson:"_id"`
	Name           string        `bson:"name"`
	Source         string        `bson:"source"`
	SourceRecipeID string        `bson:"sourceRecipeId"`
	CatalogKey     string        `bson:"catalogKey"`
	Description    string        `bson:"description"`
	DeletedAt      any           `bson:"deletedAt"`
}

// applyDescriptions gives each starter its new description, matched by name.
// Only the starter household's recipes and what came from them change:
//   - a copy in another household (same source and sourceRecipeId) whose
//     description still reads as the starter's did and that has no order
//     history, so a household's own import of the same dish keeps its words;
//   - a Shared Recipes entry under the starter's catalog key whose
//     description still reads as the starter's did.
//
// A recipe a household imported or shared on its own has another source or
// key, or other words, and is never touched. Starters with no new description
// (deleted ones aside) are listed in res.NoDescription.
func applyDescriptions(ctx context.Context, db *mongo.Database, starter bson.ObjectID, descs []Description, apply bool, res *Result) error {
	byName := make(map[string]string, len(descs))
	for _, d := range descs {
		byName[nameKey(d.Name)] = strings.TrimSpace(d.Description)
	}
	coll := db.Collection("recipes")
	entries := db.Collection(catalog.RecipesCollection)
	cur, err := coll.Find(ctx, bson.D{{Key: "householdId", Value: starter}})
	if err != nil {
		return err
	}
	var starters []starterDesc
	if err := cur.All(ctx, &starters); err != nil {
		return err
	}
	set := func(desc string) bson.D {
		return bson.D{{Key: "$set", Value: bson.D{{Key: "description", Value: desc}}}}
	}
	for _, s := range starters {
		desc, ok := byName[nameKey(s.Name)]
		if !ok {
			// A deleted starter is never copied, so it needs no words.
			if s.DeletedAt == nil {
				res.NoDescription = append(res.NoDescription, s.Name)
			}
			continue
		}
		if s.Description == desc {
			continue
		}
		res.DescStarters++
		if apply {
			if _, err := coll.UpdateOne(ctx, bson.D{{Key: "_id", Value: s.ID}, {Key: "description", Value: s.Description}}, set(desc)); err != nil {
				return err
			}
		}
		if s.SourceRecipeID != "" {
			copies := bson.D{{Key: "householdId", Value: bson.D{{Key: "$ne", Value: starter}}},
				{Key: "source", Value: s.Source}, {Key: "sourceRecipeId", Value: s.SourceRecipeID},
				{Key: "description", Value: s.Description},
				// A starter copy never has order history (starterCopy clears it).
				{Key: "orderWeeks.0", Value: bson.D{{Key: "$exists", Value: false}}},
				{Key: "timesOrdered", Value: bson.D{{Key: "$not", Value: bson.D{{Key: "$gt", Value: 0}}}}}}
			n, err := update(ctx, coll, copies, set(desc), apply)
			if err != nil {
				return err
			}
			res.DescCopies += n
		}
		if s.CatalogKey != "" {
			n, err := update(ctx, entries, bson.D{{Key: "catalogKey", Value: s.CatalogKey}, {Key: "description", Value: s.Description}}, set(desc), apply)
			if err != nil {
				return err
			}
			res.DescEntries += n
		}
	}
	return nil
}

// update counts what filter matches and, when apply is set, updates it.
func update(ctx context.Context, coll *mongo.Collection, filter, change bson.D, apply bool) (int, error) {
	if !apply {
		n, err := coll.CountDocuments(ctx, filter)
		return int(n), err
	}
	r, err := coll.UpdateMany(ctx, filter, change)
	if err != nil {
		return 0, err
	}
	return int(r.ModifiedCount), nil
}
