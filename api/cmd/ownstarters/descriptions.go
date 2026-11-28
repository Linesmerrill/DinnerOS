package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/starterdescs"
)

// Description is one starter's description in DinnerOS's own words; the
// words and the code that sets them live in internal/starterdescs.
type Description = starterdescs.Description

// mealKitPhrases are words a starter's description never uses.
var mealKitPhrases = starterdescs.MealKitPhrases

func mealKitPhrase(s string) string { return starterdescs.MealKitPhrase(s) }

// loadDescriptions reads a file of [Description] (starterdescs.Parse).
func loadDescriptions(path string) ([]Description, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	list, err := starterdescs.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return list, nil
}

// applyDescriptions runs starterdescs.Apply and adds its counts to res.
func applyDescriptions(ctx context.Context, db *mongo.Database, starter bson.ObjectID, descs []Description, apply bool, res *Result) error {
	c, err := starterdescs.Apply(ctx, db, starter, descs, apply)
	if err != nil {
		return err
	}
	res.DescStarters += c.Starters
	res.DescCopies += c.Copies
	res.DescEntries += c.Entries
	res.NoDescription = append(res.NoDescription, c.NoDescription...)
	return nil
}

func nameKey(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
