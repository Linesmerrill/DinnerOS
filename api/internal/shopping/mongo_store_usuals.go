package shopping

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// This file is the "Your usuals" half of MongoStore (usuals.go): items added
// to a week by hand, pins and "not a usual" marks, and answered prompts.
// Every document carries householdId and nothing from a store account.

// Collections for added items and usuals.
const (
	// AddedItemsCollection holds items members added to a week's list. They
	// expire AddedItemRetention after they were added.
	AddedItemsCollection = "shopping_added_items"
	// UsualPrefsCollection holds pins and "not a usual" marks.
	UsualPrefsCollection = "shopping_usual_prefs"
	// UsualsPromptsCollection holds the weeks whose "Add your usuals?" was
	// answered.
	UsualsPromptsCollection = "shopping_usuals_prompts"
)

func usualsIndexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{
			Collection: AddedItemsCollection,
			Indexes: []mongo.IndexModel{
				{
					// One line per item per week, which is also how adding
					// it again (or a retried offline add) updates it.
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "week", Value: 1}, {Key: "ingredientKey", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("householdId_week_ingredientKey_unique"),
				},
				{
					Keys:    bson.D{{Key: "addedAt", Value: 1}},
					Options: options.Index().SetExpireAfterSeconds(int32(AddedItemRetention / time.Second)).SetName("addedAt_ttl"),
				},
			},
		},
		{
			Collection: UsualPrefsCollection,
			Indexes: []mongo.IndexModel{{
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "ingredientKey", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("householdId_ingredientKey_unique"),
			}},
		},
		{
			Collection: UsualsPromptsCollection,
			Indexes: []mongo.IndexModel{{
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "week", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("householdId_week_unique"),
			}},
		},
	}
}

type addedItemDoc struct {
	ID            bson.ObjectID `bson:"_id"`
	HouseholdID   bson.ObjectID `bson:"householdId"`
	Week          string        `bson:"week"`
	IngredientKey string        `bson:"ingredientKey"`
	Name          string        `bson:"name"`
	Count         int           `bson:"count"`
	FromUsuals    bool          `bson:"fromUsuals,omitempty"`
	AddedBy       bson.ObjectID `bson:"addedBy"`
	AddedAt       time.Time     `bson:"addedAt"`
}

func (d addedItemDoc) item() AddedItem {
	return AddedItem{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), Week: d.Week, IngredientKey: d.IngredientKey, Name: d.Name,
		Count: d.Count, FromUsuals: d.FromUsuals, AddedBy: hexOrEmpty(d.AddedBy), AddedAt: d.AddedAt.UTC(),
	}
}

// ListAddedItems implements Store.
func (s *MongoStore) ListAddedItems(ctx context.Context, householdID, week string) ([]AddedItem, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	filter := bson.D{{Key: "householdId", Value: hid}}
	if week != "" {
		filter = append(filter, bson.E{Key: "week", Value: week})
	}
	cur, err := s.addedItems.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "addedAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []addedItemDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]AddedItem, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.item())
	}
	return out, nil
}

// UpsertAddedItem implements Store. Adding an item the week already has
// keeps its ID, name, and first adder, and takes the new count.
func (s *MongoStore) UpsertAddedItem(ctx context.Context, a AddedItem) (AddedItem, bool, error) {
	hid, err := householdOID(a.HouseholdID)
	if err != nil {
		return AddedItem{}, false, err
	}
	uid, err := userOID(a.AddedBy)
	if err != nil {
		return AddedItem{}, false, err
	}
	filter := bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: a.Week}, {Key: "ingredientKey", Value: a.IngredientKey}}
	update := bson.D{
		{Key: "$setOnInsert", Value: bson.D{
			{Key: "_id", Value: bson.NewObjectID()}, {Key: "addedBy", Value: uid}, {Key: "fromUsuals", Value: a.FromUsuals},
			{Key: "name", Value: a.Name},
		}},
		{Key: "$set", Value: bson.D{{Key: "count", Value: a.Count}, {Key: "addedAt", Value: a.AddedAt}}},
	}
	res, err := s.addedItems.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return AddedItem{}, false, translate(err)
	}
	var d addedItemDoc
	if err := s.addedItems.FindOne(ctx, filter).Decode(&d); err != nil {
		return AddedItem{}, false, translate(err)
	}
	return d.item(), res.UpsertedCount > 0, nil
}

// DeleteAddedItem implements Store.
func (s *MongoStore) DeleteAddedItem(ctx context.Context, householdID, id string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	res, err := s.addedItems.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAddedItemByKey implements Store.
func (s *MongoStore) DeleteAddedItemByKey(ctx context.Context, householdID, week, ingredientKey string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	res, err := s.addedItems.DeleteOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}, {Key: "ingredientKey", Value: ingredientKey}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// SentWeeks implements Store.
func (s *MongoStore) SentWeeks(ctx context.Context, householdID, before string) ([]string, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	res := s.handoffs.Distinct(ctx, "week", bson.D{
		{Key: "householdId", Value: hid}, {Key: "week", Value: bson.D{{Key: "$lt", Value: before}}},
		// A send the store refused put nothing in the cart.
		{Key: "closedReason", Value: bson.D{{Key: "$ne", Value: string(ClosedCartFailed)}}},
	})
	var weeks []string
	if err := res.Decode(&weeks); err != nil {
		return nil, translate(err)
	}
	return weeks, nil
}

type usualPrefDoc struct {
	HouseholdID   bson.ObjectID `bson:"householdId"`
	IngredientKey string        `bson:"ingredientKey"`
	Name          string        `bson:"name"`
	Count         int           `bson:"count,omitempty"`
	Pinned        bool          `bson:"pinned,omitempty"`
	NotUsual      bool          `bson:"notUsual,omitempty"`
	UpdatedBy     bson.ObjectID `bson:"updatedBy"`
	UpdatedAt     time.Time     `bson:"updatedAt"`
}

// ListUsualPrefs implements Store.
func (s *MongoStore) ListUsualPrefs(ctx context.Context, householdID string) ([]UsualPref, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	cur, err := s.usualPrefs.Find(ctx, bson.D{{Key: "householdId", Value: hid}}, options.Find().SetSort(bson.D{{Key: "ingredientKey", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []usualPrefDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]UsualPref, 0, len(docs))
	for _, d := range docs {
		out = append(out, UsualPref{
			HouseholdID: d.HouseholdID.Hex(), IngredientKey: d.IngredientKey, Name: d.Name, Count: d.Count,
			Pinned: d.Pinned, NotUsual: d.NotUsual, UpdatedBy: hexOrEmpty(d.UpdatedBy), UpdatedAt: d.UpdatedAt.UTC(),
		})
	}
	return out, nil
}

// PutUsualPref implements Store.
func (s *MongoStore) PutUsualPref(ctx context.Context, p UsualPref) error {
	hid, err := householdOID(p.HouseholdID)
	if err != nil {
		return err
	}
	uid, err := userOID(p.UpdatedBy)
	if err != nil {
		return err
	}
	doc := usualPrefDoc{
		HouseholdID: hid, IngredientKey: p.IngredientKey, Name: p.Name, Count: p.Count, Pinned: p.Pinned,
		NotUsual: p.NotUsual, UpdatedBy: uid, UpdatedAt: p.UpdatedAt,
	}
	_, err = s.usualPrefs.ReplaceOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "ingredientKey", Value: p.IngredientKey}},
		doc, options.Replace().SetUpsert(true))
	return translate(err)
}

// DeleteUsualPref implements Store. Deleting one that isn't there is fine.
func (s *MongoStore) DeleteUsualPref(ctx context.Context, householdID, ingredientKey string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	_, err = s.usualPrefs.DeleteOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "ingredientKey", Value: ingredientKey}})
	return translate(err)
}

type usualsPromptDoc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	Week        string        `bson:"week"`
	AnsweredBy  bson.ObjectID `bson:"answeredBy"`
	AnsweredAt  time.Time     `bson:"answeredAt"`
}

// GetUsualsPrompt implements Store.
func (s *MongoStore) GetUsualsPrompt(ctx context.Context, householdID, week string) (UsualsPrompt, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return UsualsPrompt{}, err
	}
	var d usualsPromptDoc
	if err := s.usualsPrompts.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}}).Decode(&d); err != nil {
		return UsualsPrompt{}, translate(err)
	}
	return UsualsPrompt{HouseholdID: d.HouseholdID.Hex(), Week: d.Week, AnsweredBy: hexOrEmpty(d.AnsweredBy), AnsweredAt: d.AnsweredAt.UTC()}, nil
}

// AnswerUsualsPrompt implements Store. The first answer is kept.
func (s *MongoStore) AnswerUsualsPrompt(ctx context.Context, p UsualsPrompt) error {
	hid, err := householdOID(p.HouseholdID)
	if err != nil {
		return err
	}
	uid, err := userOID(p.AnsweredBy)
	if err != nil {
		return err
	}
	_, err = s.usualsPrompts.UpdateOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: p.Week}},
		bson.D{{Key: "$setOnInsert", Value: bson.D{{Key: "answeredBy", Value: uid}, {Key: "answeredAt", Value: p.AnsweredAt}}}},
		options.UpdateOne().SetUpsert(true))
	return translate(err)
}
