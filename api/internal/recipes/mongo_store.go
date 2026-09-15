package recipes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Collection names owned by this package.
const (
	RecipesCollection       = "recipes"
	IngredientsCollection   = "ingredients"
	ImportReviewsCollection = "import_reviews"
)

// Review item statuses.
const reviewStatusOpen = "open"

// listCollation makes list sorting and tag/cuisine filters case-insensitive.
// List queries and the indexes that serve them must use the same collation.
var listCollation = &options.Collation{Locale: "en", Strength: 2}

// Indexes returns the indexes this package's MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	listIndex := func(name string, keys bson.D) mongo.IndexModel {
		return mongo.IndexModel{Keys: keys, Options: options.Index().SetCollation(listCollation).SetName(name)}
	}
	return []mongodb.IndexSet{
		{
			Collection: RecipesCollection,
			Indexes: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "source", Value: 1}, {Key: "sourceRecipeId", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("householdId_source_sourceRecipeId_unique"),
				},
				{
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "source", Value: 1}, {Key: "sourceAliases", Value: 1}},
					Options: options.Index().SetName("householdId_source_sourceAliases"),
				},
				listIndex("householdId_name", bson.D{{Key: "householdId", Value: 1}, {Key: "name", Value: 1}, {Key: "_id", Value: 1}}),
				listIndex("householdId_recent", bson.D{{Key: "householdId", Value: 1}, {Key: "lastOrderedWeek", Value: -1}, {Key: "name", Value: 1}, {Key: "_id", Value: 1}}),
				listIndex("householdId_popular", bson.D{{Key: "householdId", Value: 1}, {Key: "timesOrdered", Value: -1}, {Key: "name", Value: 1}, {Key: "_id", Value: 1}}),
				listIndex("householdId_tags", bson.D{{Key: "householdId", Value: 1}, {Key: "tags", Value: 1}}),
				listIndex("householdId_cuisines", bson.D{{Key: "householdId", Value: 1}, {Key: "cuisines", Value: 1}}),
			},
		},
		{
			Collection: IngredientsCollection,
			Indexes: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "key", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("key_unique"),
				},
				{
					Keys:    bson.D{{Key: "sourceRefs.source", Value: 1}, {Key: "sourceRefs.sourceIngredientId", Value: 1}},
					Options: options.Index().SetName("sourceRefs"),
				},
				{
					// Finds ingredients whose category needs review.
					Keys:    bson.D{{Key: "categoryConfident", Value: 1}, {Key: "key", Value: 1}},
					Options: options.Index().SetName("categoryConfident_key"),
				},
			},
		},
		{
			Collection: ImportReviewsCollection,
			Indexes: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "key", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("householdId_key_unique"),
				},
				{
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "status", Value: 1}, {Key: "createdAt", Value: 1}},
					Options: options.Index().SetName("householdId_status_createdAt"),
				},
			},
		},
	}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	recipes     *mongo.Collection
	ingredients *mongo.Collection
	reviews     *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{
		recipes:     db.Collection(RecipesCollection),
		ingredients: db.Collection(IngredientsCollection),
		reviews:     db.Collection(ImportReviewsCollection),
	}
}

// --- Documents ----------------------------------------------------------------

type recipeDoc struct {
	ID              bson.ObjectID         `bson:"_id"`
	HouseholdID     bson.ObjectID         `bson:"householdId"`
	Source          string                `bson:"source"`
	SourceRecipeID  string                `bson:"sourceRecipeId"`
	SourceAliases   []string              `bson:"sourceAliases,omitempty"`
	SourceURL       string                `bson:"sourceUrl,omitempty"`
	Name            string                `bson:"name"`
	Headline        string                `bson:"headline,omitempty"`
	Description     string                `bson:"description,omitempty"`
	ImageURL        string                `bson:"imageUrl,omitempty"`
	IsAddon         bool                  `bson:"isAddon"`
	Servings        []int                 `bson:"servings,omitempty"`
	PrepMinutes     int                   `bson:"prepMinutes,omitempty"`
	TotalMinutes    int                   `bson:"totalMinutes,omitempty"`
	Difficulty      int                   `bson:"difficulty,omitempty"`
	Cuisines        []string              `bson:"cuisines,omitempty"`
	Tags            []string              `bson:"tags,omitempty"`
	Utensils        []string              `bson:"utensils,omitempty"`
	Allergens       []string              `bson:"allergens,omitempty"`
	Nutrition       []nutrientDoc         `bson:"nutritionPerServing,omitempty"`
	Ingredients     []recipeIngredientDoc `bson:"ingredients,omitempty"`
	Steps           []stepDoc             `bson:"steps,omitempty"`
	OrderWeeks      []string              `bson:"orderWeeks,omitempty"`
	TimesOrdered    int                   `bson:"timesOrdered"`
	LastOrderedWeek string                `bson:"lastOrderedWeek"` // "" when never ordered, so it sorts last
	CreatedAt       time.Time             `bson:"createdAt"`
	UpdatedAt       time.Time             `bson:"updatedAt"`
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

type recipeIngredientDoc struct {
	IngredientID bson.ObjectID `bson:"ingredientId"`
	Name         string        `bson:"name"`
	PantryStaple bool          `bson:"pantryStaple"`
	Amounts      []amountDoc   `bson:"amounts,omitempty"`
}

// amountDoc stores the exact quantity as a fraction string and a float copy
// for ad-hoc queries and display. The string is authoritative.
type amountDoc struct {
	Servings      int      `bson:"servings"`
	Quantity      string   `bson:"quantity,omitempty"`
	QuantityValue *float64 `bson:"quantityValue,omitempty"`
	Unit          string   `bson:"unit,omitempty"`
	SourceUnit    string   `bson:"sourceUnit,omitempty"`
	RawText       string   `bson:"rawText,omitempty"`
}

type ingredientDoc struct {
	ID                bson.ObjectID  `bson:"_id"`
	Key               string         `bson:"key"`
	Name              string         `bson:"name"`
	Category          string         `bson:"category"`
	CategoryConfident bool           `bson:"categoryConfident"`
	SourceRefs        []sourceRefDoc `bson:"sourceRefs,omitempty"`
	ImageURL          string         `bson:"imageUrl,omitempty"`
	CreatedAt         time.Time      `bson:"createdAt"`
	UpdatedAt         time.Time      `bson:"updatedAt"`
}

type sourceRefDoc struct {
	Source             string `bson:"source"`
	SourceIngredientID string `bson:"sourceIngredientId"`
}

type summaryDoc struct {
	ID              bson.ObjectID `bson:"_id"`
	Name            string        `bson:"name"`
	Headline        string        `bson:"headline"`
	ImageURL        string        `bson:"imageUrl"`
	TotalMinutes    int           `bson:"totalMinutes"`
	TimesOrdered    int           `bson:"timesOrdered"`
	LastOrderedWeek string        `bson:"lastOrderedWeek"`
	IsAddon         bool          `bson:"isAddon"`
	Tags            []string      `bson:"tags"`
}

func newRecipeDoc(r Recipe, householdID, id bson.ObjectID) (recipeDoc, error) {
	d := recipeDoc{
		ID: id, HouseholdID: householdID,
		Source: r.Source, SourceRecipeID: r.SourceRecipeID, SourceAliases: r.SourceAliases, SourceURL: r.SourceURL,
		Name: r.Name, Headline: r.Headline, Description: r.Description, ImageURL: r.ImageURL, IsAddon: r.IsAddon,
		Servings: r.Servings, PrepMinutes: r.PrepMinutes, TotalMinutes: r.TotalMinutes, Difficulty: r.Difficulty,
		Cuisines: r.Cuisines, Tags: r.Tags, Utensils: r.Utensils, Allergens: r.Allergens,
		OrderWeeks: r.OrderWeeks, TimesOrdered: r.TimesOrdered, LastOrderedWeek: r.LastOrderedWeek,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	for _, n := range r.Nutrition {
		d.Nutrition = append(d.Nutrition, nutrientDoc(n))
	}
	for _, s := range r.Steps {
		d.Steps = append(d.Steps, stepDoc(s))
	}
	for _, line := range r.Ingredients {
		ingredientID, err := mongodb.ParseID(line.IngredientID)
		if err != nil {
			return recipeDoc{}, fmt.Errorf("recipes: ingredient id for %q: %w", line.Name, err)
		}
		ld := recipeIngredientDoc{IngredientID: ingredientID, Name: line.Name, PantryStaple: line.PantryStaple}
		for _, a := range line.Amounts {
			ad := amountDoc{Servings: a.Servings, Quantity: a.Quantity, Unit: a.Unit, SourceUnit: a.SourceUnit, RawText: a.RawText}
			if q, ok := a.ExactQuantity(); ok {
				v := q.Float64()
				ad.QuantityValue = &v
			}
			ld.Amounts = append(ld.Amounts, ad)
		}
		d.Ingredients = append(d.Ingredients, ld)
	}
	return d, nil
}

func (d recipeDoc) toRecipe() Recipe {
	r := Recipe{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(),
		Source: d.Source, SourceRecipeID: d.SourceRecipeID, SourceAliases: nilIfEmpty(d.SourceAliases), SourceURL: d.SourceURL,
		Name: d.Name, Headline: d.Headline, Description: d.Description, ImageURL: d.ImageURL, IsAddon: d.IsAddon,
		Servings: nilIfEmpty(d.Servings), PrepMinutes: d.PrepMinutes, TotalMinutes: d.TotalMinutes, Difficulty: d.Difficulty,
		Cuisines: nilIfEmpty(d.Cuisines), Tags: nilIfEmpty(d.Tags), Utensils: nilIfEmpty(d.Utensils), Allergens: nilIfEmpty(d.Allergens),
		OrderWeeks: nilIfEmpty(d.OrderWeeks), TimesOrdered: d.TimesOrdered, LastOrderedWeek: d.LastOrderedWeek,
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	for _, n := range d.Nutrition {
		r.Nutrition = append(r.Nutrition, Nutrient(n))
	}
	for _, s := range d.Steps {
		r.Steps = append(r.Steps, Step(s))
	}
	for _, ld := range d.Ingredients {
		line := RecipeIngredient{IngredientID: ld.IngredientID.Hex(), Name: ld.Name, PantryStaple: ld.PantryStaple}
		for _, ad := range ld.Amounts {
			line.Amounts = append(line.Amounts, Amount{Servings: ad.Servings, Quantity: ad.Quantity, Unit: ad.Unit, SourceUnit: ad.SourceUnit, RawText: ad.RawText})
		}
		r.Ingredients = append(r.Ingredients, line)
	}
	return r
}

func (d ingredientDoc) toIngredient() Ingredient {
	ing := Ingredient{
		ID: d.ID.Hex(), Key: d.Key, Name: d.Name, Category: d.Category, CategoryConfident: d.CategoryConfident,
		ImageURL: d.ImageURL, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	for _, ref := range d.SourceRefs {
		ing.SourceRefs = append(ing.SourceRefs, SourceRef(ref))
	}
	return ing
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// --- Ingredients --------------------------------------------------------------

// FindIngredients implements Store.
func (s *MongoStore) FindIngredients(ctx context.Context, refs []SourceRef, keys []string) ([]Ingredient, error) {
	idsBySource := map[string][]string{}
	for _, ref := range refs {
		idsBySource[ref.Source] = append(idsBySource[ref.Source], ref.SourceIngredientID)
	}
	var or bson.A
	for _, source := range sortedKeys(idsBySource) {
		or = append(or, bson.D{{Key: "sourceRefs", Value: bson.D{{Key: "$elemMatch", Value: bson.D{
			{Key: "source", Value: source},
			{Key: "sourceIngredientId", Value: bson.D{{Key: "$in", Value: idsBySource[source]}}},
		}}}}})
	}
	if len(keys) > 0 {
		or = append(or, bson.D{{Key: "key", Value: bson.D{{Key: "$in", Value: keys}}}})
	}
	if len(or) == 0 {
		return nil, nil
	}
	return s.findIngredients(ctx, bson.D{{Key: "$or", Value: or}}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
}

// SearchIngredients implements Store. An anchored pattern ("^oli") is bounded
// by the unique key index; others scan the index keys, which is fine at
// catalog size.
func (s *MongoStore) SearchIngredients(ctx context.Context, keyPattern string, limit int) ([]Ingredient, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.findIngredients(ctx,
		bson.D{{Key: "key", Value: bson.Regex{Pattern: keyPattern}}},
		options.Find().SetSort(bson.D{{Key: "key", Value: 1}}).SetLimit(int64(limit)))
}

// GetIngredients implements Store.
func (s *MongoStore) GetIngredients(ctx context.Context, ids []string) ([]Ingredient, error) {
	oids := make([]bson.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, err := mongodb.ParseID(id); err == nil {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return nil, nil
	}
	return s.findIngredients(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
}

func (s *MongoStore) findIngredients(ctx context.Context, filter bson.D, opts *options.FindOptionsBuilder) ([]Ingredient, error) {
	cur, err := s.ingredients.Find(ctx, filter, opts)
	if err != nil {
		return nil, translate(err)
	}
	var docs []ingredientDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	var out []Ingredient
	for _, d := range docs {
		out = append(out, d.toIngredient())
	}
	return out, nil
}

// UpsertIngredients implements Store. It is one unordered bulk write; the
// unique key index settles concurrent inserts of the same ingredient.
func (s *MongoStore) UpsertIngredients(ctx context.Context, list []Ingredient) (int, error) {
	if len(list) == 0 {
		return 0, nil
	}
	models := make([]mongo.WriteModel, 0, len(list))
	for _, ing := range list {
		refs := bson.A{}
		for _, ref := range ing.SourceRefs {
			refs = append(refs, sourceRefDoc(ref))
		}
		insertOnly := bson.D{
			{Key: "name", Value: ing.Name},
			{Key: "category", Value: ing.Category},
			{Key: "categoryConfident", Value: ing.CategoryConfident},
			{Key: "createdAt", Value: ing.CreatedAt},
		}
		if ing.ImageURL != "" {
			insertOnly = append(insertOnly, bson.E{Key: "imageUrl", Value: ing.ImageURL})
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.D{{Key: "key", Value: ing.Key}}).
			SetUpdate(bson.D{
				{Key: "$setOnInsert", Value: insertOnly},
				{Key: "$set", Value: bson.D{{Key: "updatedAt", Value: ing.UpdatedAt}}},
				{Key: "$addToSet", Value: bson.D{{Key: "sourceRefs", Value: bson.D{{Key: "$each", Value: refs}}}}},
			}).
			SetUpsert(true))
	}
	res, err := s.ingredients.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		return 0, translate(err)
	}
	return int(res.UpsertedCount), nil
}

// --- Recipes ------------------------------------------------------------------

// FindRecipesBySourceIDs implements Store.
func (s *MongoStore) FindRecipesBySourceIDs(ctx context.Context, householdID, source string, ids []string) ([]Recipe, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, fmt.Errorf("recipes: household id: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	filter := bson.D{
		{Key: "householdId", Value: hid},
		{Key: "source", Value: source},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "sourceRecipeId", Value: bson.D{{Key: "$in", Value: ids}}}},
			bson.D{{Key: "sourceAliases", Value: bson.D{{Key: "$in", Value: ids}}}},
		}},
	}
	cur, err := s.recipes.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []recipeDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	var out []Recipe
	for _, d := range docs {
		out = append(out, d.toRecipe())
	}
	return out, nil
}

// SaveRecipes implements Store with one unordered bulk write.
func (s *MongoStore) SaveRecipes(ctx context.Context, householdID string, list []Recipe) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return fmt.Errorf("recipes: household id: %w", err)
	}
	if len(list) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(list))
	for _, r := range list {
		if r.ID == "" {
			doc, err := newRecipeDoc(r, hid, bson.NewObjectID())
			if err != nil {
				return err
			}
			models = append(models, mongo.NewInsertOneModel().SetDocument(doc))
			continue
		}
		id, err := mongodb.ParseID(r.ID)
		if err != nil {
			return fmt.Errorf("recipes: recipe id: %w", err)
		}
		doc, err := newRecipeDoc(r, hid, id)
		if err != nil {
			return err
		}
		models = append(models, mongo.NewReplaceOneModel().
			SetFilter(bson.D{{Key: "_id", Value: id}, {Key: "householdId", Value: hid}}).
			SetReplacement(doc))
	}
	_, err = s.recipes.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	return translate(err)
}

// GetRecipe implements Store.
func (s *MongoStore) GetRecipe(ctx context.Context, householdID, id string) (Recipe, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Recipe{}, ErrNotFound
	}
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return Recipe{}, ErrNotFound
	}
	var doc recipeDoc
	if err := s.recipes.FindOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}}).Decode(&doc); err != nil {
		return Recipe{}, translate(err)
	}
	return doc.toRecipe(), nil
}

// GetRecipes implements Store with one query.
func (s *MongoStore) GetRecipes(ctx context.Context, householdID string, ids []string) ([]Recipe, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	oids := make([]bson.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, err := mongodb.ParseID(id); err == nil {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return nil, nil
	}
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}, {Key: "householdId", Value: hid}}
	cur, err := s.recipes.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []recipeDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	var out []Recipe
	for _, d := range docs {
		out = append(out, d.toRecipe())
	}
	return out, nil
}

// ListRecipes implements Store.
func (s *MongoStore) ListRecipes(ctx context.Context, householdID string, f ListFilter) ([]RecipeSummary, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, fmt.Errorf("recipes: household id: %w", err)
	}
	filter := bson.D{{Key: "householdId", Value: hid}}
	if f.SearchPattern != "" {
		filter = append(filter, bson.E{Key: "name", Value: bson.Regex{Pattern: f.SearchPattern, Options: "i"}})
	}
	if f.Addons != nil {
		filter = append(filter, bson.E{Key: "isAddon", Value: *f.Addons})
	}
	if f.Tag != "" {
		filter = append(filter, bson.E{Key: "tags", Value: f.Tag})
	}
	if f.Cuisine != "" {
		filter = append(filter, bson.E{Key: "cuisines", Value: f.Cuisine})
	}

	fields, err := sortFields(f.Sort, f.After)
	if err != nil {
		return nil, err
	}
	sort := bson.D{}
	for _, field := range fields {
		dir := 1
		if field.desc {
			dir = -1
		}
		sort = append(sort, bson.E{Key: field.name, Value: dir})
	}
	if f.After != nil {
		filter = append(filter, bson.E{Key: "$or", Value: afterFilter(fields)})
	}

	opts := options.Find().
		SetCollation(listCollation).
		SetSort(sort).
		SetLimit(int64(f.Limit)).
		SetProjection(bson.D{
			{Key: "name", Value: 1}, {Key: "headline", Value: 1}, {Key: "imageUrl", Value: 1},
			{Key: "totalMinutes", Value: 1}, {Key: "timesOrdered", Value: 1}, {Key: "lastOrderedWeek", Value: 1},
			{Key: "isAddon", Value: 1}, {Key: "tags", Value: 1},
		})
	cur, err := s.recipes.Find(ctx, filter, opts)
	if err != nil {
		return nil, translate(err)
	}
	var docs []summaryDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]RecipeSummary, 0, len(docs))
	for _, d := range docs {
		out = append(out, RecipeSummary{
			ID: d.ID.Hex(), Name: d.Name, Headline: d.Headline, ImageURL: d.ImageURL, TotalMinutes: d.TotalMinutes,
			TimesOrdered: d.TimesOrdered, LastOrderedWeek: d.LastOrderedWeek, IsAddon: d.IsAddon, Tags: nilIfEmpty(d.Tags),
		})
	}
	return out, nil
}

type sortField struct {
	name  string
	desc  bool
	after any // the cursor's value for this field; nil without a cursor
}

// sortFields returns the full sort order for s. Every order ends with name
// and _id so pages are stable.
func sortFields(s Sort, after *Position) ([]sortField, error) {
	var p Position
	var id bson.ObjectID
	if after != nil {
		p = *after
		var err error
		if id, err = mongodb.ParseID(p.ID); err != nil {
			return nil, fmt.Errorf("%w: cursor is invalid", ErrInvalidQuery)
		}
	}
	var fields []sortField
	switch s {
	case SortRecent:
		fields = append(fields, sortField{name: "lastOrderedWeek", desc: true, after: p.LastOrderedWeek})
	case SortPopular:
		fields = append(fields, sortField{name: "timesOrdered", desc: true, after: p.TimesOrdered})
	}
	return append(fields, sortField{name: "name", after: p.Name}, sortField{name: "_id", after: id}), nil
}

// afterFilter matches documents that sort after the cursor: for some field
// i, every earlier field equals the cursor and field i is beyond it.
func afterFilter(fields []sortField) bson.A {
	var or bson.A
	for i, field := range fields {
		clause := bson.D{}
		for _, prev := range fields[:i] {
			clause = append(clause, bson.E{Key: prev.name, Value: prev.after})
		}
		op := "$gt"
		if field.desc {
			op = "$lt"
		}
		clause = append(clause, bson.E{Key: field.name, Value: bson.D{{Key: op, Value: field.after}}})
		or = append(or, clause)
	}
	return or
}

// --- Review items -------------------------------------------------------------

// SaveReviewItems implements Store with one unordered bulk upsert.
func (s *MongoStore) SaveReviewItems(ctx context.Context, householdID string, items []ReviewItem, now time.Time) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return fmt.Errorf("recipes: household id: %w", err)
	}
	if len(items) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(items))
	for _, it := range items {
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.D{{Key: "householdId", Value: hid}, {Key: "key", Value: reviewKey(it)}}).
			SetUpdate(bson.D{{Key: "$setOnInsert", Value: bson.D{
				{Key: "source", Value: it.Source},
				{Key: "sourceRecipeId", Value: it.SourceRecipeID},
				{Key: "recipeName", Value: it.RecipeName},
				{Key: "field", Value: it.Field},
				{Key: "value", Value: it.Value},
				{Key: "reason", Value: it.Reason},
				{Key: "status", Value: reviewStatusOpen},
				{Key: "createdAt", Value: now},
				{Key: "updatedAt", Value: now},
			}}}).
			SetUpsert(true))
	}
	_, err = s.reviews.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	return translate(err)
}

// translate maps platform errors to this package's sentinels.
func translate(err error) error {
	err = mongodb.TranslateError(err)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mongodb.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, mongodb.ErrDuplicate):
		return fmt.Errorf("%w: %w", ErrDuplicate, err)
	default:
		return err
	}
}
