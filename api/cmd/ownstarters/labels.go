package main

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/copytext"
)

// labeledDescription matches a description that starts with a label a
// scraper left ("Web Description: Fun fact: …", build 122, Hark's review
// item 6). copytext.Description decides what to drop; this only narrows the
// scan.
var labeledDescription = bson.D{{Key: "description", Value: bson.Regex{
	Pattern: `^\s*(?:(?:web|recipe|short|meta|page|og)\s+)?(?:description|summary|intro(?:duction)?|overview|about(?: this recipe)?)\s*:`,
	Options: "i",
}}}

// stripLabels drops those labels from every recipe's and every Shared Recipes
// entry's stored description. The app already hides them when it reads one;
// this cleans the data so an export or an older app shows the words alone.
// The words after the label are the source's own and stay. It returns how
// many recipes and entries it changed, or would.
func stripLabels(ctx context.Context, db *mongo.Database, apply bool) (recipesFixed, entriesFixed int, err error) {
	recipesFixed, err = stripLabelsIn(ctx, db.Collection("recipes"), apply)
	if err != nil {
		return 0, 0, err
	}
	entriesFixed, err = stripLabelsIn(ctx, db.Collection(catalog.RecipesCollection), apply)
	return recipesFixed, entriesFixed, err
}

func stripLabelsIn(ctx context.Context, coll *mongo.Collection, apply bool) (int, error) {
	cur, err := coll.Find(ctx, labeledDescription)
	if err != nil {
		return 0, err
	}
	var docs []struct {
		ID          bson.ObjectID `bson:"_id"`
		Description string        `bson:"description"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return 0, err
	}
	n := 0
	for _, d := range docs {
		clean := copytext.Description(d.Description)
		if clean == d.Description {
			continue
		}
		n++
		if !apply {
			continue
		}
		// Only while the description still reads as it did, so a household's
		// edit in between is never overwritten.
		filter := bson.D{{Key: "_id", Value: d.ID}, {Key: "description", Value: d.Description}}
		if _, err := coll.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.D{{Key: "description", Value: clean}}}}); err != nil {
			return n, err
		}
	}
	return n, nil
}
