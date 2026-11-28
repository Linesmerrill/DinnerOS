// Package starterdescs holds every starter's description in DinnerOS's own
// words (decision 768, which widens decision 684 for descriptions) and the
// code that sets them. cmd/ownstarters runs it by hand; the
// starter-descriptions-768 migration runs it once on deploy.
package starterdescs

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
)

// Build122 is descriptions of every starter, written for build 122.
//
//go:embed build-122.json
var Build122 []byte

// Description is one starter's description in DinnerOS's own words (build
// 124, Hark's review: "Pork Sausage & Roasted Pepper Pasta" still read "Are
// you the type of person who only goes to baseball games..." and "This week,
// our chefs..."). It names the starter by its name, so one file covers every
// starter of that name (the library has a few dishes twice) on any database.
type Description struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MealKitPhrases are words a starter's description never uses: the meal
// kit's voice (its chefs, its weekly menu, the box at the door), sales
// pitches, and dashes. Each pattern is case-insensitive.
var MealKitPhrases = []struct {
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

// MealKitPhrase names the first meal-kit phrase s uses, or "".
func MealKitPhrase(s string) string {
	for _, p := range MealKitPhrases {
		if p.Pattern.MatchString(s) {
			return p.Name
		}
	}
	return ""
}

func nameKey(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// Parse reads a JSON list of [Description], refusing an empty field, a name
// given twice, or words from MealKitPhrases.
func Parse(raw []byte) ([]Description, error) {
	var list []Description
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("no descriptions")
	}
	seen := map[string]bool{}
	for i, d := range list {
		if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Description) == "" {
			return nil, fmt.Errorf("description %d needs a name and a description", i+1)
		}
		if seen[nameKey(d.Name)] {
			return nil, fmt.Errorf("%q is listed twice", d.Name)
		}
		seen[nameKey(d.Name)] = true
		if p := MealKitPhrase(d.Description); p != "" {
			return nil, fmt.Errorf("%q uses %q", d.Name, p)
		}
	}
	return list, nil
}

// Counts is what Apply changed, or would.
type Counts struct {
	// Starters, Copies and Entries count the starters, starter copies and
	// Shared Recipes entries given their new description.
	Starters, Copies, Entries int
	// LeftAloneRecipes and LeftAloneEntries count the other households'
	// recipes and the Shared Recipes entries of the same dishes (same
	// source or the same name) that kept their words: a household's own
	// import, a copy it edited or ordered, and anything someone shared.
	LeftAloneRecipes, LeftAloneEntries int
	// NoDescription names starters (deleted ones aside) with no description.
	NoDescription []string
}

type starterDoc struct {
	ID             bson.ObjectID `bson:"_id"`
	Name           string        `bson:"name"`
	Source         string        `bson:"source"`
	SourceRecipeID string        `bson:"sourceRecipeId"`
	CatalogKey     string        `bson:"catalogKey"`
	Description    string        `bson:"description"`
	DeletedAt      any           `bson:"deletedAt"`
}

// Apply gives each starter its new description, matched by name. Only the
// starter household's recipes and what came from them change:
//   - a copy in another household (same source and sourceRecipeId) whose
//     description still reads as the starter's did and that has no order
//     history, so a household's own import of the same dish keeps its words;
//   - a Shared Recipes entry under the starter's catalog key whose
//     description still reads as the starter's did.
//
// A recipe a household imported or shared on its own has another source or
// key, or other words, and is never touched. Running it again changes
// nothing. With apply false it only counts.
func Apply(ctx context.Context, db *mongo.Database, starter bson.ObjectID, descs []Description, apply bool) (Counts, error) {
	var res Counts
	if starter.IsZero() {
		return res, errors.New("no starter household")
	}
	byName := make(map[string]string, len(descs))
	for _, d := range descs {
		byName[nameKey(d.Name)] = strings.TrimSpace(d.Description)
	}
	coll := db.Collection("recipes")
	entries := db.Collection(catalog.RecipesCollection)
	cur, err := coll.Find(ctx, bson.D{{Key: "householdId", Value: starter}})
	if err != nil {
		return res, err
	}
	var starters []starterDoc
	if err := cur.All(ctx, &starters); err != nil {
		return res, err
	}
	set := func(desc string) bson.D {
		return bson.D{{Key: "$set", Value: bson.D{{Key: "description", Value: desc}}}}
	}
	// Ids of the same dishes outside the starter household, and of those
	// changed, so a dish listed twice is counted once.
	related, relatedEntries := map[bson.ObjectID]bool{}, map[bson.ObjectID]bool{}
	changed, changedEntries := map[bson.ObjectID]bool{}, map[bson.ObjectID]bool{}
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
		res.Starters++
		same := bson.A{bson.D{{Key: "name", Value: s.Name}}}
		if s.SourceRecipeID != "" {
			same = append(same, bson.D{{Key: "source", Value: s.Source}, {Key: "sourceRecipeId", Value: s.SourceRecipeID}})
			copies := bson.D{{Key: "householdId", Value: bson.D{{Key: "$ne", Value: starter}}},
				{Key: "source", Value: s.Source}, {Key: "sourceRecipeId", Value: s.SourceRecipeID},
				{Key: "description", Value: s.Description},
				// A starter copy never has order history (starterCopy clears it).
				{Key: "orderWeeks.0", Value: bson.D{{Key: "$exists", Value: false}}},
				{Key: "timesOrdered", Value: bson.D{{Key: "$not", Value: bson.D{{Key: "$gt", Value: 0}}}}}}
			if err := update(ctx, coll, copies, set(desc), apply, changed); err != nil {
				return res, err
			}
		}
		if err := collect(ctx, coll, bson.D{{Key: "householdId", Value: bson.D{{Key: "$ne", Value: starter}}}, {Key: "$or", Value: same}}, related); err != nil {
			return res, err
		}
		sameEntry := bson.A{bson.D{{Key: "name", Value: s.Name}}}
		if s.CatalogKey != "" {
			sameEntry = append(sameEntry, bson.D{{Key: "catalogKey", Value: s.CatalogKey}})
			if err := update(ctx, entries, bson.D{{Key: "catalogKey", Value: s.CatalogKey}, {Key: "description", Value: s.Description}}, set(desc), apply, changedEntries); err != nil {
				return res, err
			}
		}
		if err := collect(ctx, entries, bson.D{{Key: "$or", Value: sameEntry}}, relatedEntries); err != nil {
			return res, err
		}
		// The starter last: its old words find its copies and entry, so a run
		// stopped part way finds them again.
		if apply {
			if _, err := coll.UpdateOne(ctx, bson.D{{Key: "_id", Value: s.ID}, {Key: "description", Value: s.Description}}, set(desc)); err != nil {
				return res, err
			}
		}
	}
	res.Copies, res.Entries = len(changed), len(changedEntries)
	res.LeftAloneRecipes = len(related) - len(changed)
	res.LeftAloneEntries = len(relatedEntries) - len(changedEntries)
	return res, nil
}

// collect adds the ids filter matches to ids.
func collect(ctx context.Context, coll *mongo.Collection, filter bson.D, ids map[bson.ObjectID]bool) error {
	cur, err := coll.Find(ctx, filter)
	if err != nil {
		return err
	}
	var docs []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return err
	}
	for _, d := range docs {
		ids[d.ID] = true
	}
	return nil
}

// update adds the ids filter matches to ids and, when apply is set, updates
// exactly those.
func update(ctx context.Context, coll *mongo.Collection, filter, change bson.D, apply bool, ids map[bson.ObjectID]bool) error {
	found := map[bson.ObjectID]bool{}
	if err := collect(ctx, coll, filter, found); err != nil {
		return err
	}
	if len(found) == 0 {
		return nil
	}
	list := make(bson.A, 0, len(found))
	for id := range found {
		ids[id] = true
		list = append(list, id)
	}
	if !apply {
		return nil
	}
	_, err := coll.UpdateMany(ctx, append(bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: list}}}}, filter...), change)
	return err
}
