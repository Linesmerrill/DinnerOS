// Command cleansteps rewrites recipe steps stored as HTML into plain lines,
// the way the importer now cleans them. It is idempotent: a recipe with no
// HTML in its steps is left alone. Set DRY_RUN=1 to count without writing.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func main() {
	uri, name := os.Getenv("MONGODB_URI"), os.Getenv("MONGODB_DATABASE")
	if uri == "" || name == "" {
		log.Fatal("MONGODB_URI and MONGODB_DATABASE are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)
	coll := client.Database(name).Collection(recipes.RecipesCollection)
	filter := bson.D{{Key: "steps.text", Value: bson.D{{Key: "$regex", Value: `<(p|li|strong|ul|span|br)[ >/]`}}}}
	cur, err := coll.Find(ctx, filter, options.Find().SetProjection(bson.D{{Key: "steps", Value: 1}}))
	if err != nil {
		log.Fatal(err)
	}
	var docs []struct {
		ID    bson.ObjectID `bson:"_id"`
		Steps []bson.M      `bson:"steps"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		log.Fatal(err)
	}
	changed := 0
	for _, d := range docs {
		for _, st := range d.Steps {
			if text, ok := st["text"].(string); ok {
				st["text"] = recipes.CleanStepText(text)
			}
		}
		if os.Getenv("DRY_RUN") == "1" {
			changed++
			continue
		}
		res, err := coll.UpdateOne(ctx, bson.D{{Key: "_id", Value: d.ID}}, bson.D{{Key: "$set", Value: bson.D{{Key: "steps", Value: d.Steps}}}})
		if err != nil {
			log.Fatal(err)
		}
		changed += int(res.ModifiedCount)
	}
	fmt.Printf("recipes with HTML steps: %d, cleaned: %d\n", len(docs), changed)
}
