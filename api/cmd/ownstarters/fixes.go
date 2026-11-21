package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Fix makes a starter recipe agree with its photo (build 122, Hark's review
// item 12): new words and, when the photo shows other vegetables, new
// ingredients. It names the recipe by catalog key, so one file works on any
// database. The words are DinnerOS's own and live in git (fixes/).
type Fix struct {
	CatalogKey  string `json:"catalogKey"`
	Name        string `json:"name,omitempty"`
	Headline    string `json:"headline"`
	Description string `json:"description"`
	// Ingredients, when given, replace the recipe's list. A line named like
	// one the recipe has, with no amounts, keeps that line as it is.
	Ingredients []FixIngredient `json:"ingredients,omitempty"`
	Steps       []string        `json:"steps"`
}

// FixIngredient is one line of a fix.
type FixIngredient struct {
	Name         string      `json:"name"`
	Note         string      `json:"note,omitempty"`
	PantryStaple bool        `json:"pantryStaple,omitempty"`
	Amounts      []FixAmount `json:"amounts,omitempty"`
}

// FixAmount is a line's amount for one serving size.
type FixAmount struct {
	Servings int    `json:"servings"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit,omitempty"`
}

func loadFixes(path string) ([]Fix, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fixes []Fix
	if err := json.Unmarshal(raw, &fixes); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i, f := range fixes {
		if f.CatalogKey == "" || len(f.Steps) == 0 || f.Headline == "" || f.Description == "" {
			return nil, fmt.Errorf("%s: fix %d needs a catalogKey, headline, description and steps", path, i+1)
		}
	}
	return fixes, nil
}

// fixTarget is a recipe a fix applies to.
type fixTarget struct {
	ID          bson.ObjectID `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	Name        string        `bson:"name"`
	Steps       []step        `bson:"steps"`
	EditedAt    any           `bson:"editedAt"`
}

// editIngredients is the fix's list in the shape recipes.Service.Edit takes.
func editIngredients(r recipes.Recipe, lines []FixIngredient) []recipes.EditIngredient {
	own := map[string]recipes.RecipeIngredient{}
	for _, l := range r.Ingredients {
		own[strings.ToLower(strings.TrimSpace(l.Name))] = l
	}
	out := make([]recipes.EditIngredient, 0, len(lines))
	for _, l := range lines {
		e := recipes.EditIngredient{Name: l.Name, Note: l.Note, PantryStaple: l.PantryStaple}
		if prev, ok := own[strings.ToLower(strings.TrimSpace(l.Name))]; ok && len(l.Amounts) == 0 {
			e.IngredientID, e.Name, e.Note, e.PantryStaple = prev.IngredientID, prev.Name, prev.Note, prev.PantryStaple
			for _, a := range prev.Amounts {
				e.Amounts = append(e.Amounts, recipes.EditAmount{Servings: a.Servings, Quantity: a.Quantity, Unit: a.Unit})
			}
		}
		for _, a := range l.Amounts {
			e.Amounts = append(e.Amounts, recipes.EditAmount{Servings: a.Servings, Quantity: a.Quantity, Unit: a.Unit})
		}
		out = append(out, e)
	}
	return out
}

// fixedSteps numbers the fix's steps from 1, with no step photos.
func fixedSteps(text []string) bson.A {
	steps := bson.A{}
	for i, t := range text {
		steps = append(steps, bson.D{{Key: "index", Value: i + 1}, {Key: "text", Value: t}})
	}
	return steps
}

func sameText(a []step, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i].Text) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

// applyFixes applies each fix to the starter household's recipe with that
// catalog key and to copies whose steps still read as the starter's (or
// already read as the fix), then publishes the fixed starter to Shared
// Recipes. A household's own edits are never overwritten.
func applyFixes(ctx context.Context, db *mongo.Database, starter bson.ObjectID, fixes []Fix, apply bool, res *Result) error {
	coll := db.Collection("recipes")
	svc := recipes.NewService(recipes.NewMongoStore(db))
	for _, f := range fixes {
		var orig fixTarget
		err := coll.FindOne(ctx, bson.D{{Key: "householdId", Value: starter}, {Key: "catalogKey", Value: f.CatalogKey}}).Decode(&orig)
		if errors.Is(err, mongo.ErrNoDocuments) {
			res.Missing = append(res.Missing, f.CatalogKey)
			continue
		}
		if err != nil {
			return err
		}
		cur, err := coll.Find(ctx, bson.D{{Key: "catalogKey", Value: f.CatalogKey}})
		if err != nil {
			return err
		}
		var all []fixTarget
		if err := cur.All(ctx, &all); err != nil {
			return err
		}
		for _, t := range all {
			isStarter := t.ID == orig.ID
			if sameText(t.Steps, f.Steps) && (f.Name == "" || t.Name == f.Name) {
				continue // already fixed
			}
			if !isStarter && !sameSteps(t.Steps, orig.Steps) {
				res.SkippedEdited++
				continue
			}
			if isStarter {
				res.Starters++
			} else {
				res.Copies++
			}
			if !apply {
				continue
			}
			if err := fixOne(ctx, coll, svc, t, f); err != nil {
				return fmt.Errorf("%s in %s: %w", f.CatalogKey, t.HouseholdID.Hex(), err)
			}
		}
		n, err := refreshCatalog(ctx, db, svc, starter, orig.ID, f.CatalogKey, apply)
		if err != nil {
			return err
		}
		res.CatalogEntries += n
	}
	return nil
}

// refreshCatalog rewrites the Shared Recipes entries already published under
// the fix's catalog key from the fixed starter, keeping each entry's origin
// and publisher. Nothing new is published: only a household's sharing does
// that.
func refreshCatalog(ctx context.Context, db *mongo.Database, svc *recipes.Service, starter, id bson.ObjectID, key string, apply bool) (int, error) {
	cur, err := db.Collection(catalog.RecipesCollection).Find(ctx, bson.D{{Key: "catalogKey", Value: key}})
	if err != nil {
		return 0, err
	}
	var entries []struct {
		Origin    string `bson:"origin"`
		Publisher string `bson:"publisher"`
		Name      string `bson:"name"`
		Headline  string `bson:"headline"`
	}
	if err := cur.All(ctx, &entries); err != nil || len(entries) == 0 {
		return 0, err
	}
	if !apply {
		return len(entries), nil
	}
	fixed, err := svc.Get(ctx, starter.Hex(), id.Hex())
	if err != nil {
		return 0, err
	}
	out := make([]catalog.Recipe, 0, len(entries))
	for _, e := range entries {
		out = append(out, catalog.Recipe{CatalogKey: key, Origin: e.Origin, Publisher: e.Publisher, Content: catalog.Content(fixed)})
	}
	return catalog.NewMongoStore(db).Upsert(ctx, out, time.Now().UTC())
}

func fixOne(ctx context.Context, coll *mongo.Collection, svc *recipes.Service, t fixTarget, f Fix) error {
	hid := t.HouseholdID.Hex()
	if len(f.Ingredients) > 0 {
		r, err := svc.Get(ctx, hid, t.ID.Hex())
		if err != nil {
			return err
		}
		if _, err := svc.Edit(ctx, hid, t.ID.Hex(), recipes.EditInput{Ingredients: editIngredients(r, f.Ingredients), Steps: f.Steps}); err != nil {
			return err
		}
	}
	set := bson.D{{Key: "headline", Value: f.Headline}, {Key: "description", Value: f.Description}, {Key: "steps", Value: fixedSteps(f.Steps)}}
	if f.Name != "" {
		set = append(set, bson.E{Key: "name", Value: f.Name})
	}
	unset := bson.D{{Key: "sourceUrl", Value: ""}}
	// A starter fix isn't the household's own edit.
	if t.EditedAt == nil {
		unset = append(unset, bson.E{Key: "editedAt", Value: ""})
	}
	_, err := coll.UpdateByID(ctx, t.ID, bson.D{{Key: "$set", Value: set}, {Key: "$unset", Value: unset}})
	return err
}
