package substitutes

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

// Collections this package owns.
const (
	SpecialtiesCollection = "specialty_ingredients"
	OptionsCollection     = "specialty_options"
	ChoicesCollection     = "specialty_choices"
)

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{
			Collection: SpecialtiesCollection,
			Indexes: []mongo.IndexModel{
				{Keys: bson.D{{Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true).SetName("slug_unique")},
				{Keys: bson.D{{Key: "key", Value: 1}}, Options: options.Index().SetUnique(true).SetName("key_unique")},
			},
		},
		{
			Collection: OptionsCollection,
			Indexes: []mongo.IndexModel{{
				Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "specialtyId", Value: 1}, {Key: "createdAt", Value: 1}},
				Options: options.Index().SetName("householdId_specialtyId_createdAt"),
			}},
		},
		{
			Collection: ChoicesCollection,
			Indexes: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "specialtyId", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("householdId_specialtyId_unique"),
				},
				{
					Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "optionId", Value: 1}},
					Options: options.Index().SetName("householdId_optionId"),
				},
			},
		},
	}
}

// MongoStore is the MongoDB implementation of Store.
type MongoStore struct {
	specialties *mongo.Collection
	options     *mongo.Collection
	choices     *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{
		specialties: db.Collection(SpecialtiesCollection),
		options:     db.Collection(OptionsCollection),
		choices:     db.Collection(ChoicesCollection),
	}
}

// --- Documents ----------------------------------------------------------------

// Amounts store the exact fraction string and a float copy for ad-hoc
// queries; the string is authoritative.
type measureDoc struct {
	Quantity      string  `bson:"quantity"`
	QuantityValue float64 `bson:"quantityValue"`
	Unit          string  `bson:"unit"`
}

type unitSizeDoc struct {
	Per           string  `bson:"per"`
	Quantity      string  `bson:"quantity"`
	QuantityValue float64 `bson:"quantityValue"`
	Unit          string  `bson:"unit"`
}

type componentDoc struct {
	Name          string   `bson:"name"`
	Quantity      string   `bson:"quantity,omitempty"`
	QuantityValue *float64 `bson:"quantityValue,omitempty"`
	Unit          string   `bson:"unit,omitempty"`
	Category      string   `bson:"category,omitempty"`
}

// optionBody is the content shared by curated and household options.
type optionBody struct {
	Type          string         `bson:"type"`
	Name          string         `bson:"name"`
	Notes         string         `bson:"notes,omitempty"`
	Per           *measureDoc    `bson:"per,omitempty"`
	Ingredients   []componentDoc `bson:"ingredients"`
	Steps         []string       `bson:"steps,omitempty"`
	Yield         *measureDoc    `bson:"yield,omitempty"`
	ShelfLifeDays int            `bson:"shelfLifeDays,omitempty"`
}

type curatedOptionDoc struct {
	ID   string     `bson:"id"`
	Body optionBody `bson:",inline"`
}

type specialtyDoc struct {
	ID              bson.ObjectID      `bson:"_id,omitempty"`
	Slug            string             `bson:"slug"`
	Key             string             `bson:"key"`
	Name            string             `bson:"name"`
	Aliases         []string           `bson:"aliases"`
	AliasKeys       []string           `bson:"aliasKeys"`
	Category        string             `bson:"category"`
	UnitSizes       []unitSizeDoc      `bson:"unitSizes"`
	DefaultOptionID string             `bson:"defaultOptionId"`
	Options         []curatedOptionDoc `bson:"options"`
	SeedVersion     int                `bson:"seedVersion"`
	ContentHash     string             `bson:"contentHash"`
	Retired         bool               `bson:"retired"`
	CreatedAt       time.Time          `bson:"createdAt"`
	UpdatedAt       time.Time          `bson:"updatedAt"`
}

type householdOptionDoc struct {
	ID              bson.ObjectID `bson:"_id"`
	HouseholdID     bson.ObjectID `bson:"householdId"`
	SpecialtyID     string        `bson:"specialtyId"`
	Body            optionBody    `bson:",inline"`
	BasedOnOptionID string        `bson:"basedOnOptionId,omitempty"`
	CreatedBy       bson.ObjectID `bson:"createdBy"`
	UpdatedBy       bson.ObjectID `bson:"updatedBy"`
	CreatedAt       time.Time     `bson:"createdAt"`
	UpdatedAt       time.Time     `bson:"updatedAt"`
}

type choiceDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	SpecialtyID string        `bson:"specialtyId"`
	OptionID    string        `bson:"optionId"`
	ChosenBy    bson.ObjectID `bson:"chosenBy"`
	ChosenAt    time.Time     `bson:"chosenAt"`
}

func value(quantity string) float64 {
	q, err := ingredients.ParseQuantity(quantity)
	if err != nil {
		return 0
	}
	return q.Float64()
}

func newMeasureDoc(m *Measure) *measureDoc {
	if m == nil {
		return nil
	}
	return &measureDoc{Quantity: m.Quantity, QuantityValue: value(m.Quantity), Unit: m.Unit}
}

func (d *measureDoc) toMeasure() *Measure {
	if d == nil {
		return nil
	}
	return &Measure{Quantity: d.Quantity, Unit: d.Unit}
}

func newOptionBody(o Option) optionBody {
	b := optionBody{
		Type: string(o.Type), Name: o.Name, Notes: o.Notes, Per: newMeasureDoc(o.Per), Yield: newMeasureDoc(o.Yield),
		Steps: o.Steps, ShelfLifeDays: o.ShelfLifeDays, Ingredients: make([]componentDoc, 0, len(o.Ingredients)),
	}
	for _, c := range o.Ingredients {
		d := componentDoc{Name: c.Name, Quantity: c.Quantity, Unit: c.Unit, Category: c.Category}
		if c.Quantity != "" {
			v := value(c.Quantity)
			d.QuantityValue = &v
		}
		b.Ingredients = append(b.Ingredients, d)
	}
	return b
}

func (b optionBody) applyTo(o *Option) {
	o.Type, o.Name, o.Notes, o.Per, o.Yield = OptionType(b.Type), b.Name, b.Notes, b.Per.toMeasure(), b.Yield.toMeasure()
	o.ShelfLifeDays = b.ShelfLifeDays
	if len(b.Steps) > 0 {
		o.Steps = b.Steps
	}
	for _, c := range b.Ingredients {
		o.Ingredients = append(o.Ingredients, Component{Name: c.Name, Quantity: c.Quantity, Unit: c.Unit, Category: c.Category})
	}
}

func (d specialtyDoc) toSpecialty() Specialty {
	sp := Specialty{
		ID: d.Slug, Key: d.Key, Name: d.Name, Category: d.Category, DefaultOptionID: d.DefaultOptionID,
		SeedVersion: d.SeedVersion, ContentHash: d.ContentHash, Retired: d.Retired,
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	if len(d.Aliases) > 0 {
		sp.Aliases, sp.AliasKeys = d.Aliases, d.AliasKeys
	}
	for _, u := range d.UnitSizes {
		sp.UnitSizes = append(sp.UnitSizes, UnitSize{Per: u.Per, Quantity: u.Quantity, Unit: u.Unit})
	}
	for _, od := range d.Options {
		o := Option{ID: od.ID, SpecialtyID: d.Slug, Source: SourceCurated}
		od.Body.applyTo(&o)
		sp.Options = append(sp.Options, o)
	}
	return sp
}

func (d householdOptionDoc) toOption() Option {
	o := Option{
		ID: d.ID.Hex(), SpecialtyID: d.SpecialtyID, Source: SourceHousehold, HouseholdID: d.HouseholdID.Hex(),
		BasedOnOptionID: d.BasedOnOptionID, CreatedBy: d.CreatedBy.Hex(), UpdatedBy: d.UpdatedBy.Hex(),
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	d.Body.applyTo(&o)
	return o
}

func (d choiceDoc) toChoice() Choice {
	return Choice{
		HouseholdID: d.HouseholdID.Hex(), SpecialtyID: d.SpecialtyID, OptionID: d.OptionID,
		ChosenBy: d.ChosenBy.Hex(), ChosenAt: d.ChosenAt.UTC(),
	}
}

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

func householdOID(householdID string) (bson.ObjectID, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return bson.ObjectID{}, fmt.Errorf("substitutes: household id: %w", err)
	}
	return hid, nil
}

// --- Specialties ----------------------------------------------------------------

// ListSpecialties implements Store.
func (s *MongoStore) ListSpecialties(ctx context.Context, includeRetired bool) ([]Specialty, error) {
	filter := bson.D{}
	if !includeRetired {
		filter = bson.D{{Key: "retired", Value: false}}
	}
	cur, err := s.specialties.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "slug", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []specialtyDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Specialty, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toSpecialty())
	}
	return out, nil
}

// GetSpecialty implements Store.
func (s *MongoStore) GetSpecialty(ctx context.Context, id string) (Specialty, error) {
	var d specialtyDoc
	if err := s.specialties.FindOne(ctx, bson.D{{Key: "slug", Value: id}}).Decode(&d); err != nil {
		return Specialty{}, translate(err)
	}
	return d.toSpecialty(), nil
}

// UpsertSpecialty implements Store with one upsert on the unique slug. When
// two first writes race, the loser retries once as an update.
func (s *MongoStore) UpsertSpecialty(ctx context.Context, sp Specialty) error {
	d := specialtyDoc{
		Slug: sp.ID, Key: sp.Key, Name: sp.Name, Aliases: sp.Aliases, AliasKeys: sp.AliasKeys, Category: sp.Category,
		DefaultOptionID: sp.DefaultOptionID, SeedVersion: sp.SeedVersion, ContentHash: sp.ContentHash,
		UnitSizes: make([]unitSizeDoc, 0, len(sp.UnitSizes)), Options: make([]curatedOptionDoc, 0, len(sp.Options)),
	}
	if d.Aliases == nil {
		d.Aliases, d.AliasKeys = []string{}, []string{}
	}
	for _, u := range sp.UnitSizes {
		d.UnitSizes = append(d.UnitSizes, unitSizeDoc{Per: u.Per, Quantity: u.Quantity, QuantityValue: value(u.Quantity), Unit: u.Unit})
	}
	for _, o := range sp.Options {
		d.Options = append(d.Options, curatedOptionDoc{ID: o.ID, Body: newOptionBody(o)})
	}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "key", Value: d.Key}, {Key: "name", Value: d.Name}, {Key: "aliases", Value: d.Aliases},
			{Key: "aliasKeys", Value: d.AliasKeys}, {Key: "category", Value: d.Category}, {Key: "unitSizes", Value: d.UnitSizes},
			{Key: "defaultOptionId", Value: d.DefaultOptionID}, {Key: "options", Value: d.Options},
			{Key: "seedVersion", Value: d.SeedVersion}, {Key: "contentHash", Value: d.ContentHash},
			{Key: "retired", Value: false}, {Key: "updatedAt", Value: sp.UpdatedAt},
		}},
		{Key: "$setOnInsert", Value: bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "createdAt", Value: sp.CreatedAt}}},
	}
	filter := bson.D{{Key: "slug", Value: sp.ID}}
	_, err := s.specialties.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	if errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		_, err = s.specialties.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	}
	return translate(err)
}

// RetireSpecialties implements Store.
func (s *MongoStore) RetireSpecialties(ctx context.Context, ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.specialties.UpdateMany(ctx,
		bson.D{{Key: "slug", Value: bson.D{{Key: "$in", Value: ids}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "retired", Value: true}, {Key: "updatedAt", Value: at}}}})
	return translate(err)
}

// --- Household options ----------------------------------------------------------

// ListOptions implements Store.
func (s *MongoStore) ListOptions(ctx context.Context, householdID, specialtyID string) ([]Option, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	filter := bson.D{{Key: "householdId", Value: hid}}
	if specialtyID != "" {
		filter = append(filter, bson.E{Key: "specialtyId", Value: specialtyID})
	}
	cur, err := s.options.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []householdOptionDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Option, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toOption())
	}
	return out, nil
}

// GetOption implements Store.
func (s *MongoStore) GetOption(ctx context.Context, householdID, id string) (Option, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return Option{}, err
	}
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return Option{}, ErrNotFound
	}
	var d householdOptionDoc
	if err := s.options.FindOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}}).Decode(&d); err != nil {
		return Option{}, translate(err)
	}
	return d.toOption(), nil
}

// CountOptions implements Store.
func (s *MongoStore) CountOptions(ctx context.Context, householdID, specialtyID string) (int, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return 0, err
	}
	n, err := s.options.CountDocuments(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "specialtyId", Value: specialtyID}})
	return int(n), translate(err)
}

func parseIDs(fields map[string]string) (map[string]bson.ObjectID, error) {
	out := make(map[string]bson.ObjectID, len(fields))
	for name, v := range fields {
		oid, err := mongodb.ParseID(v)
		if err != nil {
			return nil, fmt.Errorf("substitutes: %s: %w", name, err)
		}
		out[name] = oid
	}
	return out, nil
}

// InsertOption implements Store.
func (s *MongoStore) InsertOption(ctx context.Context, o Option) (Option, error) {
	ids, err := parseIDs(map[string]string{"householdId": o.HouseholdID, "createdBy": o.CreatedBy, "updatedBy": o.UpdatedBy})
	if err != nil {
		return Option{}, err
	}
	d := householdOptionDoc{
		ID: bson.NewObjectID(), HouseholdID: ids["householdId"], SpecialtyID: o.SpecialtyID, Body: newOptionBody(o),
		BasedOnOptionID: o.BasedOnOptionID, CreatedBy: ids["createdBy"], UpdatedBy: ids["updatedBy"],
		CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
	}
	if _, err := s.options.InsertOne(ctx, d); err != nil {
		return Option{}, translate(err)
	}
	return d.toOption(), nil
}

// ReplaceOption implements Store.
func (s *MongoStore) ReplaceOption(ctx context.Context, o Option) (Option, error) {
	ids, err := parseIDs(map[string]string{"householdId": o.HouseholdID, "updatedBy": o.UpdatedBy})
	if err != nil {
		return Option{}, err
	}
	oid, err := mongodb.ParseID(o.ID)
	if err != nil {
		return Option{}, ErrNotFound
	}
	body := newOptionBody(o)
	set := bson.D{
		{Key: "type", Value: body.Type}, {Key: "name", Value: body.Name}, {Key: "ingredients", Value: body.Ingredients},
		{Key: "updatedBy", Value: ids["updatedBy"]}, {Key: "updatedAt", Value: o.UpdatedAt},
	}
	unset := bson.D{}
	optional := func(field string, v any, present bool) {
		if present {
			set = append(set, bson.E{Key: field, Value: v})
		} else {
			unset = append(unset, bson.E{Key: field, Value: ""})
		}
	}
	optional("notes", body.Notes, body.Notes != "")
	optional("per", body.Per, body.Per != nil)
	optional("steps", body.Steps, len(body.Steps) > 0)
	optional("yield", body.Yield, body.Yield != nil)
	optional("shelfLifeDays", body.ShelfLifeDays, body.ShelfLifeDays != 0)
	optional("basedOnOptionId", o.BasedOnOptionID, o.BasedOnOptionID != "")
	update := bson.D{{Key: "$set", Value: set}}
	if len(unset) > 0 {
		update = append(update, bson.E{Key: "$unset", Value: unset})
	}
	var d householdOptionDoc
	err = s.options.FindOneAndUpdate(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: ids["householdId"]}}, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&d)
	if err != nil {
		return Option{}, translate(err)
	}
	return d.toOption(), nil
}

// DeleteOption implements Store.
func (s *MongoStore) DeleteOption(ctx context.Context, householdID, id string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	oid, err := mongodb.ParseID(id)
	if err != nil {
		return ErrNotFound
	}
	res, err := s.options.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}, {Key: "householdId", Value: hid}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Choices --------------------------------------------------------------------

// ListChoices implements Store.
func (s *MongoStore) ListChoices(ctx context.Context, householdID string) ([]Choice, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	cur, err := s.choices.Find(ctx, bson.D{{Key: "householdId", Value: hid}}, options.Find().SetSort(bson.D{{Key: "specialtyId", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []choiceDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Choice, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toChoice())
	}
	return out, nil
}

func (s *MongoStore) choiceWrite(c Choice) (bson.D, bson.D, error) {
	ids, err := parseIDs(map[string]string{"householdId": c.HouseholdID, "chosenBy": c.ChosenBy})
	if err != nil {
		return nil, nil, err
	}
	filter := bson.D{{Key: "householdId", Value: ids["householdId"]}, {Key: "specialtyId", Value: c.SpecialtyID}}
	fields := bson.D{{Key: "optionId", Value: c.OptionID}, {Key: "chosenBy", Value: ids["chosenBy"]}, {Key: "chosenAt", Value: c.ChosenAt}}
	return filter, fields, nil
}

// PutChoice implements Store with an upsert on the unique key; when two first
// choices race, the loser retries once as an update.
func (s *MongoStore) PutChoice(ctx context.Context, c Choice) (Choice, error) {
	filter, fields, err := s.choiceWrite(c)
	if err != nil {
		return Choice{}, err
	}
	update := bson.D{{Key: "$set", Value: fields}, {Key: "$setOnInsert", Value: bson.D{{Key: "_id", Value: bson.NewObjectID()}}}}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var d choiceDoc
	err = s.choices.FindOneAndUpdate(ctx, filter, update, opts).Decode(&d)
	if errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		err = s.choices.FindOneAndUpdate(ctx, filter, update, opts).Decode(&d)
	}
	if err != nil {
		return Choice{}, translate(err)
	}
	return d.toChoice(), nil
}

// InsertChoice implements Store with $setOnInsert, so an existing choice is
// never changed.
func (s *MongoStore) InsertChoice(ctx context.Context, c Choice) (bool, error) {
	filter, fields, err := s.choiceWrite(c)
	if err != nil {
		return false, err
	}
	fields = append(fields, bson.E{Key: "_id", Value: bson.NewObjectID()})
	res, err := s.choices.UpdateOne(ctx, filter, bson.D{{Key: "$setOnInsert", Value: fields}}, options.UpdateOne().SetUpsert(true))
	if errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		return false, nil // a concurrent first choice won
	}
	if err != nil {
		return false, translate(err)
	}
	return res.UpsertedCount == 1, nil
}

// DeleteChoice implements Store.
func (s *MongoStore) DeleteChoice(ctx context.Context, householdID, specialtyID string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	_, err = s.choices.DeleteOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "specialtyId", Value: specialtyID}})
	return translate(err)
}

// DeleteChoicesForOption implements Store.
func (s *MongoStore) DeleteChoicesForOption(ctx context.Context, householdID, optionID string) error {
	hid, err := householdOID(householdID)
	if err != nil {
		return err
	}
	_, err = s.choices.DeleteMany(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "optionId", Value: optionID}})
	return translate(err)
}
