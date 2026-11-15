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
			{
				// Groups the copies of one recipe (decision 689), most
				// complete first, then the first published.
				Keys: bson.D{
					{Key: "fingerprint", Value: 1}, {Key: "completeness", Value: -1},
					{Key: "firstPublishedAt", Value: 1}, {Key: "_id", Value: 1},
				},
				Options: options.Index().SetName("fingerprint_representative"),
			},
			{
				Keys:    bson.D{{Key: "fingerprintVersion", Value: 1}},
				Options: options.Index().SetName("fingerprintVersion"),
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
	IngredientNames []string `bson:"ingredientNames,omitempty"`
	IngredientKeys  []string `bson:"ingredientKeys,omitempty"`
	CuisineKeys     []string `bson:"cuisineKeys,omitempty"`
	TagKeys         []string `bson:"tagKeys,omitempty"`
	// Fingerprint groups the copies of one recipe, Completeness picks the
	// copy shown, and FingerprintVersion says which rules made them
	// (fingerprint.go). All three are derived from the content on every
	// write.
	Fingerprint        string    `bson:"fingerprint,omitempty"`
	FingerprintVersion int       `bson:"fingerprintVersion"`
	Completeness       int       `bson:"completeness"`
	FirstPublishedAt   time.Time `bson:"firstPublishedAt"`
	UpdatedAt          time.Time `bson:"updatedAt"`
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
		Fingerprint: Fingerprint(c), FingerprintVersion: FingerprintVersion, Completeness: Completeness(c),
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
		ID: d.ID.Hex(), CatalogKey: d.CatalogKey, Content: c, Fingerprint: d.Fingerprint,
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
	out := d.toRecipe()
	out.GroupKeys = []string{out.CatalogKey}
	if d.Fingerprint == "" {
		return out, nil
	}
	cur, err := s.entries.Find(ctx,
		bson.D{{Key: "fingerprint", Value: d.Fingerprint}, {Key: "_id", Value: bson.D{{Key: "$ne", Value: oid}}}},
		options.Find().SetSort(representativeOrder()).SetProjection(bson.D{{Key: "catalogKey", Value: 1}}))
	if err != nil {
		return Recipe{}, translate(err)
	}
	var others []struct {
		CatalogKey string `bson:"catalogKey"`
	}
	if err := cur.All(ctx, &others); err != nil {
		return Recipe{}, translate(err)
	}
	for _, o := range others {
		if !slices.Contains(out.GroupKeys, o.CatalogKey) {
			out.GroupKeys = append(out.GroupKeys, o.CatalogKey)
		}
	}
	return out, nil
}

// groupExpr is an entry's group in an aggregation: its fingerprint, or its
// own ID before the backfill reaches it (groupKey).
func groupExpr() bson.D {
	return bson.D{{Key: "$ifNull", Value: bson.A{
		"$fingerprint", bson.D{{Key: "$concat", Value: bson.A{"id:", bson.D{{Key: "$toString", Value: "$_id"}}}}},
	}}}
}

// representativeOrder sorts a group's copies best first (representsBetter).
func representativeOrder() bson.D {
	return bson.D{{Key: "completeness", Value: -1}, {Key: "firstPublishedAt", Value: 1}, {Key: "_id", Value: 1}}
}

// notHidden excludes the hidden groups: fingerprint groups by fingerprint,
// and entries standing alone by ID.
func notHidden(groups []string) bson.D {
	var fps, ids bson.A
	for _, g := range groups {
		if id, ok := strings.CutPrefix(g, "id:"); ok {
			if oid, err := mongodb.ParseID(id); err == nil {
				ids = append(ids, oid)
			}
			continue
		}
		fps = append(fps, g)
	}
	var out bson.D
	if len(fps) > 0 {
		out = append(out, bson.E{Key: "fingerprint", Value: bson.D{{Key: "$nin", Value: fps}}})
	}
	if len(ids) > 0 {
		out = append(out, bson.E{Key: "_id", Value: bson.D{{Key: "$nin", Value: ids}}})
	}
	return out
}

// Groups implements Store.
func (s *MongoStore) Groups(ctx context.Context, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	oids := make(bson.A, 0, len(ids))
	for _, id := range ids {
		out[id] = idGroup(id)
		if oid, err := mongodb.ParseID(id); err == nil {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return out, nil
	}
	cur, err := s.entries.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}},
		options.Find().SetProjection(bson.D{{Key: "fingerprint", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []struct {
		ID          bson.ObjectID `bson:"_id"`
		Fingerprint string        `bson:"fingerprint"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	for _, d := range docs {
		if d.Fingerprint != "" {
			out[d.ID.Hex()] = d.Fingerprint
		}
	}
	return out, nil
}

// Search implements Store with one aggregation. Matching entries are grouped
// by fingerprint; the groups are counted, sorted, and paged; and each group on
// the page is shown as its most complete copy, chosen from the whole group.
// With text it uses the catalog_text index and sorts groups by their best
// relevance; without it, by name.
func (s *MongoStore) Search(ctx context.Context, f Filter) ([]Recipe, int, error) {
	match := narrowing(f)
	match = append(match, notHidden(f.HiddenGroups)...)
	group := bson.D{
		{Key: "_id", Value: groupExpr()},
		{Key: "nameKey", Value: bson.D{{Key: "$min", Value: "$nameKey"}}},
		{Key: "firstId", Value: bson.D{{Key: "$min", Value: "$_id"}}},
	}
	sort := bson.D{{Key: "nameKey", Value: 1}, {Key: "_id", Value: 1}}
	var pipeline mongo.Pipeline
	if f.Text != "" {
		match = append(match, bson.E{Key: "$text", Value: bson.D{{Key: "$search", Value: f.Text}}})
		pipeline = append(pipeline, bson.D{{Key: "$match", Value: match}},
			bson.D{{Key: "$addFields", Value: bson.D{{Key: "textScore", Value: bson.D{{Key: "$meta", Value: "textScore"}}}}}})
		group = append(group, bson.E{Key: "score", Value: bson.D{{Key: "$max", Value: "$textScore"}}})
		sort = bson.D{{Key: "score", Value: -1}, {Key: "_id", Value: 1}}
	} else {
		pipeline = append(pipeline, bson.D{{Key: "$match", Value: match}})
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$group", Value: group}},
		bson.D{{Key: "$facet", Value: bson.D{
			{Key: "total", Value: bson.A{bson.D{{Key: "$count", Value: "n"}}}},
			{Key: "page", Value: bson.A{
				bson.D{{Key: "$sort", Value: sort}},
				bson.D{{Key: "$skip", Value: int64(f.Offset)}},
				bson.D{{Key: "$limit", Value: int64(f.Limit)}},
				bson.D{{Key: "$lookup", Value: bson.D{
					{Key: "from", Value: RecipesCollection},
					{Key: "localField", Value: "_id"},
					{Key: "foreignField", Value: "fingerprint"},
					{Key: "pipeline", Value: bson.A{
						bson.D{{Key: "$match", Value: bson.D{{Key: "source", Value: bson.D{{Key: "$nin", Value: recipes.WithheldSources()}}}}}},
						bson.D{{Key: "$sort", Value: representativeOrder()}},
					}},
					{Key: "as", Value: "members"},
				}}},
			}},
		}}},
	)
	cur, err := s.entries.Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, 0, translate(err)
	}
	var rows []struct {
		Total []struct {
			N int `bson:"n"`
		} `bson:"total"`
		Page []struct {
			FirstID bson.ObjectID `bson:"firstId"`
			Members []entryDoc    `bson:"members"`
		} `bson:"page"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, 0, translate(err)
	}
	if len(rows) == 0 {
		return nil, 0, nil
	}
	total := 0
	if len(rows[0].Total) > 0 {
		total = min(rows[0].Total[0].N, MaxOffset+MaxLimit)
	}
	out := make([]Recipe, 0, len(rows[0].Page))
	for _, g := range rows[0].Page {
		if len(g.Members) == 0 {
			// An entry the backfill has not reached stands alone.
			var d entryDoc
			if err := s.entries.FindOne(ctx, bson.D{{Key: "_id", Value: g.FirstID}}).Decode(&d); err != nil {
				return nil, 0, translate(err)
			}
			g.Members = []entryDoc{d}
		}
		out = append(out, representative(g.Members))
	}
	return out, total, nil
}

// representative is a group's first copy (members are sorted best first),
// with the keys of the whole group.
func representative(members []entryDoc) Recipe {
	out := members[0].toRecipe()
	for _, m := range members {
		if !slices.Contains(out.GroupKeys, m.CatalogKey) {
			out.GroupKeys = append(out.GroupKeys, m.CatalogKey)
		}
	}
	return out
}

// narrowing builds the exact filters shared by search and counting, all of
// them against folded fields so they need no collation.
func narrowing(f Filter) bson.D {
	// A meal kit's recipes are never served (decision 672).
	out := bson.D{{Key: "source", Value: bson.D{{Key: "$nin", Value: recipes.WithheldSources()}}}}
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

// Scan implements Store with an aggregation that keeps each group's most
// complete copy and drops steps and descriptions, which discovery never
// shows. The limit counts groups.
func (s *MongoStore) Scan(ctx context.Context, limit int) ([]Recipe, error) {
	if limit <= 0 {
		return nil, nil
	}
	cur, err := s.entries.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "source", Value: bson.D{{Key: "$nin", Value: recipes.WithheldSources()}}}}}},
		{{Key: "$project", Value: bson.D{{Key: "steps", Value: 0}, {Key: "description", Value: 0}}}},
		{{Key: "$sort", Value: append(bson.D{{Key: "fingerprint", Value: 1}}, representativeOrder()...)}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: groupExpr()},
			{Key: "doc", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
			{Key: "keys", Value: bson.D{{Key: "$push", Value: "$catalogKey"}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "doc._id", Value: 1}}}},
		{{Key: "$limit", Value: int64(limit)}},
	}, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, translate(err)
	}
	var rows []struct {
		Doc  entryDoc `bson:"doc"`
		Keys []string `bson:"keys"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, translate(err)
	}
	out := make([]Recipe, 0, len(rows))
	for _, row := range rows {
		e := row.Doc.toRecipe()
		e.GroupKeys = []string{e.CatalogKey}
		for _, k := range row.Keys {
			if !slices.Contains(e.GroupKeys, k) {
				e.GroupKeys = append(e.GroupKeys, k)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// --- Fingerprints -------------------------------------------------------------

// fingerprintBatch is how many entries BackfillFingerprints writes at once.
const fingerprintBatch = 500

// BackfillFingerprints gives every entry stored before decision 689, or under
// older fingerprint rules, its fingerprint and completeness, and returns how
// many it changed. The server runs it at startup: once every entry is current
// it reads one index and writes nothing, and re-running it is safe.
func (s *MongoStore) BackfillFingerprints(ctx context.Context) (int, error) {
	cur, err := s.entries.Find(ctx, bson.D{{Key: "fingerprintVersion", Value: bson.D{{Key: "$ne", Value: FingerprintVersion}}}})
	if err != nil {
		return 0, translate(err)
	}
	defer func() { _ = cur.Close(context.WithoutCancel(ctx)) }()
	changed := 0
	var models []mongo.WriteModel
	flush := func() error {
		if len(models) == 0 {
			return nil
		}
		res, err := s.entries.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
		if err != nil {
			return fmt.Errorf("catalog: backfill fingerprints: %w", err)
		}
		changed += int(res.ModifiedCount)
		models = models[:0]
		return nil
	}
	for cur.Next(ctx) {
		var d entryDoc
		if err := cur.Decode(&d); err != nil {
			return changed, translate(err)
		}
		c := d.toRecipe().Content
		set := bson.D{
			{Key: "fingerprintVersion", Value: FingerprintVersion},
			{Key: "completeness", Value: Completeness(c)},
		}
		update := bson.D{}
		if fp := Fingerprint(c); fp != "" {
			update = append(update, bson.E{Key: "$set", Value: append(set, bson.E{Key: "fingerprint", Value: fp})})
		} else {
			update = append(update, bson.E{Key: "$set", Value: set}, bson.E{Key: "$unset", Value: bson.D{{Key: "fingerprint", Value: ""}}})
		}
		models = append(models, mongo.NewUpdateOneModel().SetFilter(bson.D{{Key: "_id", Value: d.ID}}).SetUpdate(update))
		if len(models) >= fingerprintBatch {
			if err := flush(); err != nil {
				return changed, err
			}
		}
	}
	if err := cur.Err(); err != nil {
		return changed, translate(err)
	}
	return changed, flush()
}

// GroupEntry is one stored entry and the group the current rules put it in.
type GroupEntry struct {
	ID   string
	Name string
	// Group is computed now from the content, whatever is stored: the
	// fingerprint, or the entry's own ID group when it has none.
	Group string
	// Current is true when the stored fingerprint and version are up to date.
	Current bool
}

// ListGroups reads every entry the lists can show (none from a withheld
// source) and computes its group. It writes nothing: cmd/sharedcount uses it
// to count Shared Recipes before and after decision 689 on any database,
// backfilled or not.
func (s *MongoStore) ListGroups(ctx context.Context) ([]GroupEntry, error) {
	cur, err := s.entries.Find(ctx,
		bson.D{{Key: "source", Value: bson.D{{Key: "$nin", Value: recipes.WithheldSources()}}}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetProjection(bson.D{{Key: "description", Value: 0}}))
	if err != nil {
		return nil, translate(err)
	}
	defer func() { _ = cur.Close(context.WithoutCancel(ctx)) }()
	var out []GroupEntry
	for cur.Next(ctx) {
		var d entryDoc
		if err := cur.Decode(&d); err != nil {
			return nil, translate(err)
		}
		e := d.toRecipe()
		e.Fingerprint = Fingerprint(e.Content)
		out = append(out, GroupEntry{
			ID: e.ID, Name: e.Content.Name, Group: groupKey(e),
			Current: d.FingerprintVersion == FingerprintVersion && d.Fingerprint == e.Fingerprint,
		})
	}
	if err := cur.Err(); err != nil {
		return nil, translate(err)
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
