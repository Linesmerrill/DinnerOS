package companions

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

// Collection is the collection this package owns.
const Collection = "companions"

// Indexes returns the indexes MongoStore relies on. The partial unique key
// gives an add-on recipe at most one companion per household, so migration
// stays idempotent when two requests run it at once.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{
			{
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "createdAt", Value: 1}},
				Options: options.Index().SetName("householdId_createdAt"),
			},
			{
				Keys: bson.D{{Key: "householdId", Value: 1}, {Key: "linkedRecipe.id", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("householdId_linkedRecipe_unique").
					SetPartialFilterExpression(bson.D{{Key: "linkedRecipe.id", Value: bson.D{{Key: "$exists", Value: true}}}}),
			},
		},
	}}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	companions *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{companions: db.Collection(Collection)}
}

type recipeRefDoc struct {
	ID   bson.ObjectID `bson:"id"`
	Name string        `bson:"name"`
}

type companionDoc struct {
	ID             bson.ObjectID  `bson:"_id"`
	HouseholdID    bson.ObjectID  `bson:"householdId"`
	Name           string         `bson:"name"`
	StoreItem      string         `bson:"storeItem,omitempty"`
	Quantity       string         `bson:"quantity,omitempty"`
	Unit           string         `bson:"unit,omitempty"`
	ProductURL     string         `bson:"productUrl,omitempty"`
	LinkedRecipe   *recipeRefDoc  `bson:"linkedRecipe,omitempty"`
	Cuisines       []string       `bson:"cuisines"`
	Recipes        []recipeRefDoc `bson:"recipes"`
	MealCategories []string       `bson:"mealCategories,omitempty"`
	Frequency      string         `bson:"frequency"`
	Mode           string         `bson:"mode"`
	LastListedWeek string         `bson:"lastListedWeek,omitempty"`
	LastListedAt   *time.Time     `bson:"lastListedAt,omitempty"`
	CreatedBy      *bson.ObjectID `bson:"createdBy,omitempty"`
	CreatedAt      time.Time      `bson:"createdAt"`
	UpdatedAt      time.Time      `bson:"updatedAt"`
	HiddenAt       *time.Time     `bson:"hiddenAt,omitempty"`
}

func refsToDocs(refs []RecipeRef) ([]recipeRefDoc, error) {
	out := make([]recipeRefDoc, 0, len(refs))
	for _, r := range refs {
		id, err := mongodb.ParseID(r.ID)
		if err != nil {
			return nil, fmt.Errorf("companions: recipe id: %w", err)
		}
		out = append(out, recipeRefDoc{ID: id, Name: r.Name})
	}
	return out, nil
}

func newDoc(c Companion) (companionDoc, error) {
	hid, err := mongodb.ParseID(c.HouseholdID)
	if err != nil {
		return companionDoc{}, fmt.Errorf("companions: household id: %w", err)
	}
	recipes, err := refsToDocs(c.Recipes)
	if err != nil {
		return companionDoc{}, err
	}
	d := companionDoc{
		HouseholdID: hid, Name: c.Name, StoreItem: c.StoreItem, Quantity: c.Quantity, Unit: c.Unit, ProductURL: c.ProductURL,
		Cuisines: nonNil(c.Cuisines), Recipes: recipes, MealCategories: c.MealCategories, Frequency: c.Frequency, Mode: c.Mode,
		LastListedWeek: c.LastListedWeek, LastListedAt: c.LastListedAt, CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC(),
		HiddenAt: c.HiddenAt,
	}
	if c.ID != "" {
		if d.ID, err = mongodb.ParseID(c.ID); err != nil {
			return companionDoc{}, fmt.Errorf("companions: id: %w", err)
		}
	}
	if c.LinkedRecipe != nil {
		refs, err := refsToDocs([]RecipeRef{*c.LinkedRecipe})
		if err != nil {
			return companionDoc{}, err
		}
		d.LinkedRecipe = &refs[0]
	}
	if c.CreatedBy != "" {
		by, err := mongodb.ParseID(c.CreatedBy)
		if err != nil {
			return companionDoc{}, fmt.Errorf("companions: createdBy: %w", err)
		}
		d.CreatedBy = &by
	}
	return d, nil
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func (d companionDoc) toCompanion() Companion {
	c := Companion{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), Name: d.Name, StoreItem: d.StoreItem, Quantity: d.Quantity, Unit: d.Unit,
		ProductURL: d.ProductURL, Cuisines: d.Cuisines, MealCategories: d.MealCategories, Frequency: d.Frequency, Mode: d.Mode,
		LastListedWeek: d.LastListedWeek, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	for _, r := range d.Recipes {
		c.Recipes = append(c.Recipes, RecipeRef{ID: r.ID.Hex(), Name: r.Name})
	}
	if d.LinkedRecipe != nil {
		c.LinkedRecipe = &RecipeRef{ID: d.LinkedRecipe.ID.Hex(), Name: d.LinkedRecipe.Name}
	}
	if d.CreatedBy != nil {
		c.CreatedBy = d.CreatedBy.Hex()
	}
	if d.LastListedAt != nil {
		t := d.LastListedAt.UTC()
		c.LastListedAt = &t
	}
	if d.HiddenAt != nil {
		t := d.HiddenAt.UTC()
		c.HiddenAt = &t
	}
	return c
}

func translate(err error) error {
	err = mongodb.TranslateError(err)
	if errors.Is(err, mongodb.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// scope is the filter for one visible companion; ok is false for a
// malformed ID, which is never found.
func scope(householdID, id string) (bson.D, bool) {
	hid, err1 := mongodb.ParseID(householdID)
	oid, err2 := mongodb.ParseID(id)
	if err1 != nil || err2 != nil {
		return nil, false
	}
	return bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}, {Key: "hiddenAt", Value: bson.D{{Key: "$exists", Value: false}}}}, true
}

// List implements Store.
func (s *MongoStore) List(ctx context.Context, householdID string, hidden bool) ([]Companion, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, fmt.Errorf("companions: household id: %w", err)
	}
	filter := bson.D{{Key: "householdId", Value: hid}}
	if !hidden {
		filter = append(filter, bson.E{Key: "hiddenAt", Value: bson.D{{Key: "$exists", Value: false}}})
	}
	cur, err := s.companions.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []companionDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Companion, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toCompanion())
	}
	return out, nil
}

// Get implements Store.
func (s *MongoStore) Get(ctx context.Context, householdID, id string) (Companion, error) {
	filter, ok := scope(householdID, id)
	if !ok {
		return Companion{}, ErrNotFound
	}
	var d companionDoc
	if err := s.companions.FindOne(ctx, filter).Decode(&d); err != nil {
		return Companion{}, translate(err)
	}
	return d.toCompanion(), nil
}

// Insert implements Store.
func (s *MongoStore) Insert(ctx context.Context, c Companion) (Companion, error) {
	c.ID = ""
	d, err := newDoc(c)
	if err != nil {
		return Companion{}, err
	}
	d.ID = bson.NewObjectID()
	if _, err := s.companions.InsertOne(ctx, d); err != nil {
		return Companion{}, translate(err)
	}
	return d.toCompanion(), nil
}

// EnsureLinked implements Store with an upsert on the linked recipe, so a
// second migration (or a hidden companion) leaves the stored one alone.
func (s *MongoStore) EnsureLinked(ctx context.Context, c Companion) (bool, error) {
	if c.LinkedRecipe == nil {
		return false, errors.New("companions: EnsureLinked needs a linked recipe")
	}
	c.ID = ""
	d, err := newDoc(c)
	if err != nil {
		return false, err
	}
	d.ID = bson.NewObjectID()
	filter := bson.D{{Key: "householdId", Value: d.HouseholdID}, {Key: "linkedRecipe.id", Value: d.LinkedRecipe.ID}}
	// The filter's fields seed the new document; the rest comes from d.
	raw, err := bson.Marshal(d)
	if err != nil {
		return false, err
	}
	var all, insert bson.D
	if err := bson.Unmarshal(raw, &all); err != nil {
		return false, err
	}
	for _, e := range all {
		if e.Key != "householdId" && e.Key != "linkedRecipe" {
			insert = append(insert, e)
		}
	}
	insert = append(insert, bson.E{Key: "linkedRecipe.name", Value: d.LinkedRecipe.Name})
	res, err := s.companions.UpdateOne(ctx, filter, bson.D{{Key: "$setOnInsert", Value: insert}}, options.UpdateOne().SetUpsert(true))
	if errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		return false, nil
	}
	if err != nil {
		return false, translate(err)
	}
	return res.UpsertedCount > 0, nil
}

// Update implements Store.
func (s *MongoStore) Update(ctx context.Context, c Companion) (Companion, error) {
	filter, ok := scope(c.HouseholdID, c.ID)
	if !ok {
		return Companion{}, ErrNotFound
	}
	d, err := newDoc(c)
	if err != nil {
		return Companion{}, err
	}
	set := bson.D{
		{Key: "name", Value: d.Name}, {Key: "cuisines", Value: d.Cuisines}, {Key: "recipes", Value: d.Recipes},
		{Key: "frequency", Value: d.Frequency}, {Key: "mode", Value: d.Mode}, {Key: "updatedAt", Value: d.UpdatedAt},
	}
	var unset bson.D
	for _, f := range []struct {
		key   string
		value string
	}{{"storeItem", d.StoreItem}, {"quantity", d.Quantity}, {"unit", d.Unit}, {"productUrl", d.ProductURL}} {
		if f.value == "" {
			unset = append(unset, bson.E{Key: f.key, Value: ""})
		} else {
			set = append(set, bson.E{Key: f.key, Value: f.value})
		}
	}
	if len(d.MealCategories) == 0 {
		unset = append(unset, bson.E{Key: "mealCategories", Value: ""})
	} else {
		set = append(set, bson.E{Key: "mealCategories", Value: d.MealCategories})
	}
	update := bson.D{{Key: "$set", Value: set}}
	if len(unset) > 0 {
		update = append(update, bson.E{Key: "$unset", Value: unset})
	}
	var out companionDoc
	err = s.companions.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&out)
	if err != nil {
		return Companion{}, translate(err)
	}
	return out.toCompanion(), nil
}

// Hide implements Store.
func (s *MongoStore) Hide(ctx context.Context, householdID, id string, at time.Time) error {
	filter, ok := scope(householdID, id)
	if !ok {
		return ErrNotFound
	}
	res, err := s.companions.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.D{{Key: "hiddenAt", Value: at.UTC()}}}})
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete implements Store.
func (s *MongoStore) Delete(ctx context.Context, householdID, id string) error {
	filter, ok := scope(householdID, id)
	if !ok {
		return ErrNotFound
	}
	res, err := s.companions.DeleteOne(ctx, filter)
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkListed implements Store. ISO week strings sort in time order.
func (s *MongoStore) MarkListed(ctx context.Context, householdID, id, week string, at time.Time) error {
	filter, ok := scope(householdID, id)
	if !ok {
		return ErrNotFound
	}
	filter = append(filter, bson.E{Key: "$or", Value: bson.A{
		bson.D{{Key: "lastListedWeek", Value: bson.D{{Key: "$exists", Value: false}}}},
		bson.D{{Key: "lastListedWeek", Value: bson.D{{Key: "$lte", Value: week}}}},
	}})
	_, err := s.companions.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.D{
		{Key: "lastListedWeek", Value: week}, {Key: "lastListedAt", Value: at.UTC()},
	}}})
	return translate(err)
}

// PurgeHousehold deletes every companion the household has. Account deletion
// calls it when the household's last member deletes their account. It is
// idempotent.
func (s *MongoStore) PurgeHousehold(ctx context.Context, householdID string) error {
	return mongodb.DeleteByID(ctx, "householdId", householdID, s.companions)
}
