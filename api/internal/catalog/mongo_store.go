package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// RecipesCollection is the global recipe catalog. It is a global collection:
// account deletion never touches it, because no document in it belongs to a
// household.
const RecipesCollection = "recipe_catalog"

// Indexes returns the indexes this package's MongoStore relies on.
//
// No index here carries a collation. A $text query cannot use one, and the
// same listing has to work with and without a search term, so the catalog
// stores folded copies of the fields it filters and sorts on (nameKey,
// cuisineKeys, tagKeys, ingredientKeys) and compares them exactly. One
// ordering, one set of indexes, no rule about which query may use which.
//
// The text index is the search surface (docs/database.md). Weights put a name
// match first, then the headline, then an ingredient, then a cuisine or tag,
// which is the order a person means when they type "harissa".
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: RecipesCollection,
		Indexes: []mongo.IndexModel{
			{
				Keys:    bson.D{{Key: "catalogKey", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("catalogKey_unique"),
			},
			{
				Keys: bson.D{
					{Key: "name", Value: "text"},
					{Key: "headline", Value: "text"},
					{Key: "ingredientNames", Value: "text"},
					{Key: "cuisines", Value: "text"},
					{Key: "tags", Value: "text"},
				},
				Options: options.Index().
					SetName("catalog_text").
					SetDefaultLanguage("english").
					SetWeights(bson.D{
						{Key: "name", Value: 10},
						{Key: "headline", Value: 4},
						{Key: "ingredientNames", Value: 3},
						{Key: "cuisines", Value: 2},
						{Key: "tags", Value: 2},
					}),
			},
			{
				// Browse and the un-searched listing order.
				Keys:    bson.D{{Key: "nameKey", Value: 1}, {Key: "_id", Value: 1}},
				Options: options.Index().SetName("nameKey"),
			},
			{
				Keys:    bson.D{{Key: "cuisineKeys", Value: 1}},
				Options: options.Index().SetName("cuisineKeys"),
			},
			{
				Keys:    bson.D{{Key: "tagKeys", Value: 1}},
				Options: options.Index().SetName("tagKeys"),
			},
			{
				Keys:    bson.D{{Key: "ingredientKeys", Value: 1}},
				Options: options.Index().SetName("ingredientKeys"),
			},
		},
	}}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	entries *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{entries: db.Collection(RecipesCollection)}
}

// --- Documents ----------------------------------------------------------------

// entryDoc is deliberately spelled out rather than shared with the recipes
// package: there is no householdId, orderWeeks, timesOrdered,
// lastOrderedWeek, or sharedToCatalog field here, so no future change to a
// household recipe can carry household data into the catalog by accident.
type entryDoc struct {
	ID          bson.ObjectID   `bson:"_id"`
	CatalogKey  string          `bson:"catalogKey"`
	ContentHash string          `bson:"contentHash"`
	Source      string          `bson:"source"`
	SourceID    string          `bson:"sourceRecipeId,omitempty"`
	SourceURL   string          `bson:"sourceUrl,omitempty"`
	Name        string          `bson:"name"`
	NameKey     string          `bson:"nameKey"`
	Headline    string          `bson:"headline,omitempty"`
	Description string          `bson:"description,omitempty"`
	ImageURL    string          `bson:"imageUrl,omitempty"`
	IsAddon     bool            `bson:"isAddon"`
	Servings    []int           `bson:"servings,omitempty"`
	PrepMinutes int             `bson:"prepMinutes,omitempty"`
	TotalMin    int             `bson:"totalMinutes,omitempty"`
	Difficulty  int             `bson:"difficulty,omitempty"`
	Cuisines    []string        `bson:"cuisines,omitempty"`
	Tags        []string        `bson:"tags,omitempty"`
	Utensils    []string        `bson:"utensils,omitempty"`
	Allergens   []string        `bson:"allergens,omitempty"`
	Nutrition   []nutrientDoc   `bson:"nutritionPerServing,omitempty"`
	Ingredients []ingredientDoc `bson:"ingredients,omitempty"`
	Steps       []stepDoc       `bson:"steps,omitempty"`
	// IngredientNames feeds the text index. The *Keys fields are the folded
	// forms the exact filters and the listing order compare against.
	IngredientNames  []string  `bson:"ingredientNames,omitempty"`
	IngredientKeys   []string  `bson:"ingredientKeys,omitempty"`
	CuisineKeys      []string  `bson:"cuisineKeys,omitempty"`
	TagKeys          []string  `bson:"tagKeys,omitempty"`
	FirstPublishedAt time.Time `bson:"firstPublishedAt"`
	UpdatedAt        time.Time `bson:"updatedAt"`
}

type nutrientDoc struct {
	Name   string  `bson:"name"`
	Amount float64 `bson:"amount"`
	Unit   string  `bson:"unit,omitempty"`
}

type stepDoc struct {
	Index    int    `bson:"index"`
	Text     string `bson:"text"`
	ImageURL string `bson:"imageUrl,omitempty"`
}

type ingredientDoc struct {
	IngredientID bson.ObjectID `bson:"ingredientId"`
	Name         string        `bson:"name"`
	PantryStaple bool          `bson:"pantryStaple"`
	Amounts      []amountDoc   `bson:"amounts,omitempty"`
}

type amountDoc struct {
	Servings   int    `bson:"servings"`
	Quantity   string `bson:"quantity,omitempty"`
	Unit       string `bson:"unit,omitempty"`
	SourceUnit string `bson:"sourceUnit,omitempty"`
	RawText    string `bson:"rawText,omitempty"`
}

func newEntryDoc(e Recipe) (entryDoc, error) {
	c := Content(e.Content)
	d := entryDoc{
		CatalogKey: e.CatalogKey, ContentHash: contentHash(c),
		Source: c.Source, SourceID: c.SourceRecipeID, SourceURL: c.SourceURL,
		Name: c.Name, NameKey: fold(c.Name), Headline: c.Headline, Description: c.Description, ImageURL: c.ImageURL,
		IsAddon: c.IsAddon, Servings: c.Servings, PrepMinutes: c.PrepMinutes, TotalMin: c.TotalMinutes,
		Difficulty: c.Difficulty, Cuisines: c.Cuisines, Tags: c.Tags, Utensils: c.Utensils, Allergens: c.Allergens,
		CuisineKeys: foldAll(c.Cuisines), TagKeys: foldAll(c.Tags),
	}
	for _, n := range c.Nutrition {
		d.Nutrition = append(d.Nutrition, nutrientDoc(n))
	}
	for _, s := range c.Steps {
		d.Steps = append(d.Steps, stepDoc(s))
	}
	for _, line := range c.Ingredients {
		id, err := mongodb.ParseID(line.IngredientID)
		if err != nil {
			return entryDoc{}, fmt.Errorf("catalog: ingredient id for %q: %w", line.Name, err)
		}
		ld := ingredientDoc{IngredientID: id, Name: line.Name, PantryStaple: line.PantryStaple}
		for _, a := range line.Amounts {
			ld.Amounts = append(ld.Amounts, amountDoc{
				Servings: a.Servings, Quantity: a.Quantity, Unit: a.Unit, SourceUnit: a.SourceUnit, RawText: a.RawText,
			})
		}
		d.Ingredients = append(d.Ingredients, ld)
		if !slices.Contains(d.IngredientNames, line.Name) {
			d.IngredientNames = append(d.IngredientNames, line.Name)
		}
		if k := ingredients.NormalizeName(line.Name); k != "" && !slices.Contains(d.IngredientKeys, k) {
			d.IngredientKeys = append(d.IngredientKeys, k)
		}
	}
	return d, nil
}

func (d entryDoc) toRecipe() Recipe {
	c := recipes.Recipe{
		ID: d.ID.Hex(), Source: d.Source, SourceRecipeID: d.SourceID, SourceURL: d.SourceURL,
		Name: d.Name, Headline: d.Headline, Description: d.Description, ImageURL: d.ImageURL,
		IsAddon: d.IsAddon, Servings: d.Servings, PrepMinutes: d.PrepMinutes, TotalMinutes: d.TotalMin,
		Difficulty: d.Difficulty, Cuisines: d.Cuisines, Tags: d.Tags, Utensils: d.Utensils, Allergens: d.Allergens,
	}
	for _, n := range d.Nutrition {
		c.Nutrition = append(c.Nutrition, recipes.Nutrient(n))
	}
	for _, s := range d.Steps {
		c.Steps = append(c.Steps, recipes.Step(s))
	}
	for _, ld := range d.Ingredients {
		line := recipes.RecipeIngredient{IngredientID: ld.IngredientID.Hex(), Name: ld.Name, PantryStaple: ld.PantryStaple}
		for _, a := range ld.Amounts {
			line.Amounts = append(line.Amounts, recipes.Amount{
				Servings: a.Servings, Quantity: a.Quantity, Unit: a.Unit, SourceUnit: a.SourceUnit, RawText: a.RawText,
			})
		}
		c.Ingredients = append(c.Ingredients, line)
	}
	return Recipe{
		ID: d.ID.Hex(), CatalogKey: d.CatalogKey, Content: c,
		FirstPublishedAt: d.FirstPublishedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
}

// contentHash identifies a version of a recipe's content, so re-publishing
// something unchanged writes nothing and UpdatedAt means what it says.
func contentHash(c recipes.Recipe) string {
	c.CreatedAt, c.UpdatedAt = time.Time{}, time.Time{}
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// --- Writes -------------------------------------------------------------------

// Upsert implements Store with one bulk write. Each entry is an upsert on its
// catalogKey whose update is a pipeline, so firstPublishedAt survives and
// updatedAt only moves when the content actually changed.
func (s *MongoStore) Upsert(ctx context.Context, entries []Recipe, now time.Time) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	models := make([]mongo.WriteModel, 0, len(entries))
	for _, e := range entries {
		if e.CatalogKey == "" {
			continue
		}
		d, err := newEntryDoc(e)
		if err != nil {
			return 0, err
		}
		set, err := bson.Marshal(d)
		if err != nil {
			return 0, fmt.Errorf("catalog: marshal entry: %w", err)
		}
		var fields bson.D
		if err := bson.Unmarshal(set, &fields); err != nil {
			return 0, fmt.Errorf("catalog: marshal entry: %w", err)
		}
		// _id, firstPublishedAt, and updatedAt are decided by the pipeline,
		// not by the marshalled document.
		stage := bson.D{}
		for _, f := range fields {
			switch f.Key {
			case "_id", "firstPublishedAt", "updatedAt":
			default:
				stage = append(stage, f)
			}
		}
		stage = append(stage,
			bson.E{Key: "firstPublishedAt", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$firstPublishedAt", now}}}},
			bson.E{Key: "updatedAt", Value: bson.D{{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$eq", Value: bson.A{"$contentHash", d.ContentHash}}}, "$updatedAt", now,
			}}}},
		)
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.D{{Key: "catalogKey", Value: e.CatalogKey}}).
			SetUpdate(mongo.Pipeline{bson.D{{Key: "$set", Value: stage}}}).
			SetUpsert(true))
	}
	if len(models) == 0 {
		return 0, nil
	}
	res, err := s.entries.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		// A concurrent publish of the same key can lose the upsert race; the
		// winner stored the same content, so the loss is not an error.
		if res == nil || !errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
			return 0, fmt.Errorf("catalog: upsert: %w", err)
		}
	}
	return int(res.UpsertedCount + res.ModifiedCount), nil
}

// --- Reads --------------------------------------------------------------------

// Get implements Store.
func (s *MongoStore) Get(ctx context.Context, id string) (Recipe, error) {
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return Recipe{}, ErrNotFound
	}
	var d entryDoc
	if err := s.entries.FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		return Recipe{}, translate(err)
	}
	return d.toRecipe(), nil
}

// Search implements Store. With text it uses the catalog_text index and sorts
// by relevance; without it, the name index and alphabetical order.
func (s *MongoStore) Search(ctx context.Context, f Filter) ([]Recipe, int, error) {
	filter := narrowing(f)
	opts := options.Find().SetSkip(int64(f.Offset)).SetLimit(int64(f.Limit))
	if f.Text != "" {
		filter = append(filter, bson.E{Key: "$text", Value: bson.D{{Key: "$search", Value: f.Text}}})
		opts.SetSort(bson.D{{Key: "score", Value: bson.D{{Key: "$meta", Value: "textScore"}}}, {Key: "_id", Value: 1}})
	} else {
		opts.SetSort(bson.D{{Key: "nameKey", Value: 1}, {Key: "_id", Value: 1}})
	}
	total, err := s.entries.CountDocuments(ctx, filter, options.Count().SetLimit(int64(MaxOffset+MaxLimit)))
	if err != nil {
		return nil, 0, translate(err)
	}
	cur, err := s.entries.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, translate(err)
	}
	var docs []entryDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, 0, translate(err)
	}
	out := make([]Recipe, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toRecipe())
	}
	return out, int(total), nil
}

// narrowing builds the exact filters shared by search and counting, all of
// them against folded fields so they need no collation.
func narrowing(f Filter) bson.D {
	out := bson.D{}
	if key := fold(f.Cuisine); key != "" {
		out = append(out, bson.E{Key: "cuisineKeys", Value: key})
	}
	if key := fold(f.Tag); key != "" {
		out = append(out, bson.E{Key: "tagKeys", Value: key})
	}
	if key := ingredients.NormalizeName(f.Ingredient); key != "" {
		out = append(out, bson.E{Key: "ingredientKeys", Value: key})
	}
	return out
}

// fold is the comparison form of a label: trimmed and lowercased. Cuisines
// and tags are short labels from a source, so case is the only difference
// worth ignoring; ingredient names go through ingredients.NormalizeName
// instead, which the rest of the system already uses as their identity.
func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func foldAll(values []string) []string {
	var out []string
	for _, v := range values {
		if k := fold(v); k != "" && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// Scan implements Store with a projection that drops steps and descriptions,
// which discovery never shows.
func (s *MongoStore) Scan(ctx context.Context, limit int) ([]Recipe, error) {
	if limit <= 0 {
		return nil, nil
	}
	cur, err := s.entries.Find(ctx, bson.D{},
		options.Find().
			SetSort(bson.D{{Key: "_id", Value: 1}}).
			SetLimit(int64(limit)).
			SetProjection(bson.D{{Key: "steps", Value: 0}, {Key: "description", Value: 0}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []entryDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Recipe, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toRecipe())
	}
	return out, nil
}

// PurgeHousehold is deliberately absent: the catalog holds no household data,
// so deleting an account changes nothing in it.

func translate(err error) error {
	err = mongodb.TranslateError(err)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mongodb.ErrNotFound):
		return ErrNotFound
	default:
		return err
	}
}
