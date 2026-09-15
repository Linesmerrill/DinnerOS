package pantry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// ItemsCollection is the collection this package owns.
const ItemsCollection = "pantry_items"

// Indexes returns the indexes MongoStore relies on. The unique key index also
// serves every household-scoped list and lookup.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: ItemsCollection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "key", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_key_unique"),
		}},
	}}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	items *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{items: db.Collection(ItemsCollection)}
}

// itemDoc stores the exact quantity as a fraction string and a float copy for
// ad-hoc queries. The string is authoritative.
type itemDoc struct {
	ID            bson.ObjectID  `bson:"_id"`
	HouseholdID   bson.ObjectID  `bson:"householdId"`
	IngredientID  *bson.ObjectID `bson:"ingredientId,omitempty"`
	Key           string         `bson:"key"`
	DisplayName   string         `bson:"displayName"`
	Category      string         `bson:"category"`
	Quantity      string         `bson:"quantity,omitempty"`
	QuantityValue *float64       `bson:"quantityValue,omitempty"`
	Unit          string         `bson:"unit,omitempty"`
	Status        string         `bson:"status"`
	IsStaple      bool           `bson:"isStaple"`
	ExpiresOn     string         `bson:"expiresOn,omitempty"`
	Note          string         `bson:"note,omitempty"`
	Version       int64          `bson:"version"`
	CreatedAt     time.Time      `bson:"createdAt"`
	UpdatedBy     bson.ObjectID  `bson:"updatedBy"`
	UpdatedAt     time.Time      `bson:"updatedAt"`
}

func newItemDoc(item Item, id, householdID bson.ObjectID) (itemDoc, error) {
	updatedBy, err := mongodb.ParseID(item.UpdatedBy)
	if err != nil {
		return itemDoc{}, fmt.Errorf("pantry: updatedBy: %w", err)
	}
	d := itemDoc{
		ID: id, HouseholdID: householdID, Key: item.Key, DisplayName: item.DisplayName, Category: item.Category,
		Quantity: item.Quantity, Unit: item.Unit, Status: string(item.Status), IsStaple: item.IsStaple,
		ExpiresOn: item.ExpiresOn, Note: item.Note, Version: item.Version,
		CreatedAt: item.CreatedAt, UpdatedBy: updatedBy, UpdatedAt: item.UpdatedAt,
	}
	if item.IngredientID != "" {
		ingredientID, err := mongodb.ParseID(item.IngredientID)
		if err != nil {
			return itemDoc{}, fmt.Errorf("pantry: ingredientId: %w", err)
		}
		d.IngredientID = &ingredientID
	}
	if item.Quantity != "" {
		q, err := ingredients.ParseQuantity(item.Quantity)
		if err != nil {
			return itemDoc{}, fmt.Errorf("pantry: quantity: %w", err)
		}
		v := q.Float64()
		d.QuantityValue = &v
	}
	return d, nil
}

func (d itemDoc) toItem() Item {
	item := Item{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), Key: d.Key, DisplayName: d.DisplayName, Category: d.Category,
		Quantity: d.Quantity, Unit: d.Unit, Status: Status(d.Status), IsStaple: d.IsStaple,
		ExpiresOn: d.ExpiresOn, Note: d.Note, Version: d.Version,
		CreatedAt: d.CreatedAt.UTC(), UpdatedBy: d.UpdatedBy.Hex(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	if d.IngredientID != nil {
		item.IngredientID = d.IngredientID.Hex()
	}
	return item
}

func parseIDs(ids []string) []bson.ObjectID {
	out := make([]bson.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, err := mongodb.ParseID(id); err == nil {
			out = append(out, oid)
		}
	}
	return out
}

func householdOID(householdID string) (bson.ObjectID, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return bson.ObjectID{}, fmt.Errorf("pantry: household id: %w", err)
	}
	return hid, nil
}

func (s *MongoStore) find(ctx context.Context, filter bson.D) ([]Item, error) {
	cur, err := s.items.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "householdId", Value: 1}, {Key: "key", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []itemDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Item, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toItem())
	}
	return out, nil
}

// ListItems implements Store.
func (s *MongoStore) ListItems(ctx context.Context, householdID string, f ListFilter) ([]Item, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	filter := bson.D{{Key: "householdId", Value: hid}}
	if f.Status != "" {
		filter = append(filter, bson.E{Key: "status", Value: string(f.Status)})
	}
	if f.Category != "" {
		filter = append(filter, bson.E{Key: "category", Value: f.Category})
	}
	if f.Staple != nil {
		filter = append(filter, bson.E{Key: "isStaple", Value: *f.Staple})
	}
	if f.NamePattern != "" || f.KeyPattern != "" {
		or := bson.A{}
		if f.NamePattern != "" {
			or = append(or, bson.D{{Key: "displayName", Value: bson.Regex{Pattern: f.NamePattern, Options: "i"}}})
		}
		if f.KeyPattern != "" {
			or = append(or, bson.D{{Key: "key", Value: bson.Regex{Pattern: f.KeyPattern}}})
		}
		filter = append(filter, bson.E{Key: "$or", Value: or})
	}
	return s.find(ctx, filter)
}

// GetItem implements Store.
func (s *MongoStore) GetItem(ctx context.Context, householdID, id string) (Item, error) {
	hid, err1 := mongodb.ParseID(householdID)
	oid, err2 := mongodb.ParseID(id)
	if err1 != nil || err2 != nil {
		return Item{}, ErrNotFound
	}
	var doc itemDoc
	if err := s.items.FindOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}}).Decode(&doc); err != nil {
		return Item{}, translate(err)
	}
	return doc.toItem(), nil
}

// GetItems implements Store.
func (s *MongoStore) GetItems(ctx context.Context, householdID string, ids []string) ([]Item, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	oids := parseIDs(ids)
	if len(oids) == 0 {
		return nil, nil
	}
	return s.find(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}})
}

// FindItemsByKeys implements Store.
func (s *MongoStore) FindItemsByKeys(ctx context.Context, householdID string, keys []string) ([]Item, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}
	return s.find(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "key", Value: bson.D{{Key: "$in", Value: keys}}}})
}

// CountItems implements Store.
func (s *MongoStore) CountItems(ctx context.Context, householdID string) (int, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return 0, err
	}
	n, err := s.items.CountDocuments(ctx, bson.D{{Key: "householdId", Value: hid}})
	return int(n), translate(err)
}

// InsertItem implements Store.
func (s *MongoStore) InsertItem(ctx context.Context, item Item) (Item, error) {
	hid, err := householdOID(item.HouseholdID)
	if err != nil {
		return Item{}, err
	}
	item.Version = 1
	doc, err := newItemDoc(item, bson.NewObjectID(), hid)
	if err != nil {
		return Item{}, err
	}
	if _, err := s.items.InsertOne(ctx, doc); err != nil {
		return Item{}, translate(err)
	}
	return doc.toItem(), nil
}

// UpdateItem implements Store with a replacement conditioned on the version.
func (s *MongoStore) UpdateItem(ctx context.Context, item Item) (Item, error) {
	hid, err1 := mongodb.ParseID(item.HouseholdID)
	oid, err2 := mongodb.ParseID(item.ID)
	if err1 != nil || err2 != nil {
		return Item{}, ErrNotFound
	}
	current := item.Version
	item.Version++
	doc, err := newItemDoc(item, oid, hid)
	if err != nil {
		return Item{}, err
	}
	// Key and createdAt are identity: they are never in the update.
	set := bson.D{
		{Key: "displayName", Value: doc.DisplayName},
		{Key: "category", Value: doc.Category},
		{Key: "status", Value: doc.Status},
		{Key: "isStaple", Value: doc.IsStaple},
		{Key: "version", Value: doc.Version},
		{Key: "updatedBy", Value: doc.UpdatedBy},
		{Key: "updatedAt", Value: doc.UpdatedAt},
	}
	unset := bson.D{}
	optional := func(field string, value any, present bool) {
		if present {
			set = append(set, bson.E{Key: field, Value: value})
		} else {
			unset = append(unset, bson.E{Key: field, Value: ""})
		}
	}
	optional("ingredientId", doc.IngredientID, doc.IngredientID != nil)
	optional("quantity", doc.Quantity, doc.Quantity != "")
	optional("quantityValue", doc.QuantityValue, doc.QuantityValue != nil)
	optional("unit", doc.Unit, doc.Unit != "")
	optional("expiresOn", doc.ExpiresOn, doc.ExpiresOn != "")
	optional("note", doc.Note, doc.Note != "")
	update := bson.D{{Key: "$set", Value: set}}
	if len(unset) > 0 {
		update = append(update, bson.E{Key: "$unset", Value: unset})
	}

	var stored itemDoc
	err = s.items.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}, {Key: "version", Value: current}},
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&stored)
	if errors.Is(err, mongo.ErrNoDocuments) {
		n, countErr := s.items.CountDocuments(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}})
		switch {
		case countErr != nil:
			return Item{}, translate(countErr)
		case n == 0:
			return Item{}, ErrNotFound
		default:
			return Item{}, ErrConflict
		}
	}
	if err != nil {
		return Item{}, translate(err)
	}
	return stored.toItem(), nil
}

// DeleteItem implements Store.
func (s *MongoStore) DeleteItem(ctx context.Context, householdID, id string) error {
	hid, err1 := mongodb.ParseID(householdID)
	oid, err2 := mongodb.ParseID(id)
	if err1 != nil || err2 != nil {
		return ErrNotFound
	}
	res, err := s.items.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// SetStatus implements Store with one updateMany.
func (s *MongoStore) SetStatus(ctx context.Context, householdID string, ids []string, status Status, updatedBy string, at time.Time) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	by, err := mongodb.ParseID(updatedBy)
	if err != nil {
		return fmt.Errorf("pantry: updatedBy: %w", err)
	}
	oids := parseIDs(ids)
	if len(oids) == 0 {
		return nil
	}
	update := bson.D{
		{Key: "$set", Value: bson.D{{Key: "status", Value: string(status)}, {Key: "updatedBy", Value: by}, {Key: "updatedAt", Value: at}}},
		{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
	}
	if status == StatusOut {
		update = append(update, bson.E{Key: "$unset", Value: bson.D{{Key: "quantity", Value: ""}, {Key: "quantityValue", Value: ""}, {Key: "unit", Value: ""}}})
	}
	_, err = s.items.UpdateMany(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}}, update)
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
