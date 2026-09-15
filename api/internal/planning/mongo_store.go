package planning

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

// PlansCollection is the collection owned by this package.
const PlansCollection = "weekly_plans"

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: PlansCollection,
		Indexes: []mongo.IndexModel{{
			// One plan per household and week; also serves week-range queries,
			// because "YYYY-Www" strings sort in week order.
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "week", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_week_unique"),
		}},
	}}
}

// MongoStore is the MongoDB implementation of Store. Every entry change is a
// single atomic update ($push, $pull, or a positional $set) conditioned on the
// plan being a draft, so concurrent edits never lose each other's writes.
type MongoStore struct {
	plans *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{plans: db.Collection(PlansCollection)}
}

type planDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	Week        string        `bson:"week"`
	StartDate   string        `bson:"startDate"`
	Status      string        `bson:"status"`
	Entries     []entryDoc    `bson:"entries"`
	CreatedAt   time.Time     `bson:"createdAt"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
}

type entryDoc struct {
	ID             bson.ObjectID `bson:"id"`
	RecipeID       bson.ObjectID `bson:"recipeId"`
	RecipeName     string        `bson:"recipeName"`
	RecipeImageURL string        `bson:"recipeImageUrl,omitempty"`
	Day            string        `bson:"day,omitempty"`
	Servings       int           `bson:"servings"`
	Note           string        `bson:"note,omitempty"`
	AddedBy        bson.ObjectID `bson:"addedBy"`
	AddedAt        time.Time     `bson:"addedAt"`
	// Origin is stored only for autopilot entries; absent means manual.
	Origin     string `bson:"origin,omitempty"`
	ProposalID string `bson:"proposalId,omitempty"`
}

type summaryDoc struct {
	Week       string    `bson:"week"`
	Status     string    `bson:"status"`
	EntryCount int       `bson:"entryCount"`
	UpdatedAt  time.Time `bson:"updatedAt"`
}

func (d planDoc) toPlan() (Plan, error) {
	w, err := ParseWeek(d.Week)
	if err != nil {
		return Plan{}, fmt.Errorf("planning: stored plan %s: %w", d.ID.Hex(), err)
	}
	p := Plan{
		HouseholdID: d.HouseholdID.Hex(), Week: w, Status: Status(d.Status),
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	for _, e := range d.Entries {
		origin := Origin(e.Origin)
		if origin == "" {
			origin = OriginManual
		}
		p.Entries = append(p.Entries, Entry{
			ID: e.ID.Hex(), RecipeID: e.RecipeID.Hex(), RecipeName: e.RecipeName, RecipeImageURL: e.RecipeImageURL,
			Day: Day(e.Day), Servings: e.Servings, Note: e.Note, AddedBy: e.AddedBy.Hex(), AddedAt: e.AddedAt.UTC(),
			Origin: origin, ProposalID: e.ProposalID,
		})
	}
	return p, nil
}

func planFilter(hid bson.ObjectID, w Week) bson.D {
	return bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: w.String()}}
}

// insertOnly holds the fields set when an upsert creates a plan.
func insertOnly(w Week, now time.Time) bson.D {
	return bson.D{
		{Key: "startDate", Value: w.Date(Monday)},
		{Key: "entries", Value: bson.A{}},
		{Key: "createdAt", Value: now},
	}
}

var afterUpdate = options.FindOneAndUpdate().SetReturnDocument(options.After)

// GetPlan implements Store.
func (s *MongoStore) GetPlan(ctx context.Context, householdID string, w Week) (Plan, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Plan{}, ErrNotFound
	}
	var doc planDoc
	if err := s.plans.FindOne(ctx, planFilter(hid, w)).Decode(&doc); err != nil {
		return Plan{}, translate(err)
	}
	return doc.toPlan()
}

// ListSummaries implements Store with one query that projects entry counts.
func (s *MongoStore) ListSummaries(ctx context.Context, householdID string, from, to Week) ([]Summary, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	filter := bson.D{
		{Key: "householdId", Value: hid},
		{Key: "week", Value: bson.D{{Key: "$gte", Value: from.String()}, {Key: "$lte", Value: to.String()}}},
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "week", Value: 1}}).
		SetProjection(bson.D{
			{Key: "_id", Value: 0},
			{Key: "week", Value: 1},
			{Key: "status", Value: 1},
			{Key: "updatedAt", Value: 1},
			{Key: "entryCount", Value: bson.D{{Key: "$size", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$entries", bson.A{}}}}}}},
		})
	cur, err := s.plans.Find(ctx, filter, opts)
	if err != nil {
		return nil, translate(err)
	}
	var docs []summaryDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	var out []Summary
	for _, d := range docs {
		w, err := ParseWeek(d.Week)
		if err != nil {
			return nil, fmt.Errorf("planning: stored plan week %q: %w", d.Week, err)
		}
		out = append(out, Summary{Week: w, Status: Status(d.Status), EntryCount: d.EntryCount, UpdatedAt: d.UpdatedAt.UTC()})
	}
	return out, nil
}

// AddEntry implements Store with AddEntries.
func (s *MongoStore) AddEntry(ctx context.Context, householdID string, w Week, e Entry, maxEntries int, now time.Time) (Plan, string, error) {
	p, ids, err := s.AddEntries(ctx, householdID, w, []Entry{e}, maxEntries, now)
	if err != nil {
		return Plan{}, "", err
	}
	return p, ids[0], nil
}

// AddEntries implements Store. It makes sure the plan exists, then pushes all
// entries in one update that only matches a draft with room for them.
func (s *MongoStore) AddEntries(ctx context.Context, householdID string, w Week, entries []Entry, maxEntries int, now time.Time) (Plan, []string, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Plan{}, nil, ErrNotFound
	}
	if len(entries) == 0 || len(entries) > maxEntries {
		return Plan{}, nil, ErrPlanFull
	}
	docs := make([]entryDoc, 0, len(entries))
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		recipeID, err := mongodb.ParseID(e.RecipeID)
		if err != nil {
			return Plan{}, nil, fmt.Errorf("planning: recipe id: %w", err)
		}
		addedBy, err := mongodb.ParseID(e.AddedBy)
		if err != nil {
			return Plan{}, nil, fmt.Errorf("planning: added by: %w", err)
		}
		id := bson.NewObjectID()
		doc := entryDoc{
			ID: id, RecipeID: recipeID, RecipeName: e.RecipeName, RecipeImageURL: e.RecipeImageURL,
			Day: string(e.Day), Servings: e.Servings, Note: e.Note, AddedBy: addedBy, AddedAt: e.AddedAt,
			ProposalID: e.ProposalID,
		}
		if e.Origin == OriginAutopilot {
			doc.Origin = string(OriginAutopilot)
		}
		docs = append(docs, doc)
		ids = append(ids, id.Hex())
	}
	ensure := bson.D{{Key: "$setOnInsert", Value: append(insertOnly(w, now),
		bson.E{Key: "status", Value: StatusDraft},
		bson.E{Key: "updatedAt", Value: now},
	)}}
	err = retryDuplicate(func() error {
		_, err := s.plans.UpdateOne(ctx, planFilter(hid, w), ensure, options.UpdateOne().SetUpsert(true))
		return err
	})
	if err != nil {
		return Plan{}, nil, translate(err)
	}

	filter := append(planFilter(hid, w),
		bson.E{Key: "status", Value: StatusDraft},
		bson.E{Key: fmt.Sprintf("entries.%d", maxEntries-len(entries)), Value: bson.D{{Key: "$exists", Value: false}}},
	)
	update := bson.D{
		{Key: "$push", Value: bson.D{{Key: "entries", Value: bson.D{{Key: "$each", Value: docs}}}}},
		{Key: "$set", Value: bson.D{{Key: "updatedAt", Value: now}}},
	}
	var out planDoc
	err = s.plans.FindOneAndUpdate(ctx, filter, update, afterUpdate).Decode(&out)
	if errors.Is(translate(err), ErrNotFound) {
		p, err := s.GetPlan(ctx, householdID, w)
		switch {
		case err != nil:
			return Plan{}, nil, err
		case p.Status == StatusFinalized:
			return Plan{}, nil, ErrFinalized
		default:
			return Plan{}, nil, ErrPlanFull
		}
	}
	if err != nil {
		return Plan{}, nil, translate(err)
	}
	p, err := out.toPlan()
	return p, ids, err
}

// EarliestWeek implements Store by walking the unique index in week order to
// the first plan with an entry.
func (s *MongoStore) EarliestWeek(ctx context.Context, householdID string) (Week, bool, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Week{}, false, nil
	}
	var doc struct {
		Week string `bson:"week"`
	}
	err = s.plans.FindOne(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "entries.0", Value: bson.D{{Key: "$exists", Value: true}}}},
		options.FindOne().SetSort(bson.D{{Key: "week", Value: 1}}).SetProjection(bson.D{{Key: "_id", Value: 0}, {Key: "week", Value: 1}}),
	).Decode(&doc)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		return Week{}, false, nil
	case err != nil:
		return Week{}, false, translate(err)
	}
	w, err := ParseWeek(doc.Week)
	if err != nil {
		return Week{}, false, fmt.Errorf("planning: stored plan week %q: %w", doc.Week, err)
	}
	return w, true, nil
}

// ListPlans implements Store with one range query on the unique index.
func (s *MongoStore) ListPlans(ctx context.Context, householdID string, from, to Week) ([]Plan, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	filter := bson.D{
		{Key: "householdId", Value: hid},
		{Key: "week", Value: bson.D{{Key: "$gte", Value: from.String()}, {Key: "$lte", Value: to.String()}}},
	}
	cur, err := s.plans.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "week", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []planDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	var out []Plan
	for _, d := range docs {
		p, err := d.toPlan()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// UpdateEntry implements Store with a positional update.
func (s *MongoStore) UpdateEntry(ctx context.Context, householdID string, w Week, entryID string, c EntryChanges, now time.Time) (Plan, error) {
	set := bson.D{{Key: "updatedAt", Value: now}}
	var unset bson.D
	setOrUnset := func(field string, value any, empty bool) {
		if empty {
			unset = append(unset, bson.E{Key: "entries.$." + field, Value: ""})
		} else {
			set = append(set, bson.E{Key: "entries.$." + field, Value: value})
		}
	}
	if c.Day != nil {
		setOrUnset("day", string(*c.Day), *c.Day == "")
	}
	if c.Servings != nil {
		setOrUnset("servings", *c.Servings, false)
	}
	if c.Note != nil {
		setOrUnset("note", *c.Note, *c.Note == "")
	}
	update := bson.D{{Key: "$set", Value: set}}
	if len(unset) > 0 {
		update = append(update, bson.E{Key: "$unset", Value: unset})
	}
	return s.updateEntry(ctx, householdID, w, entryID, update)
}

// DeleteEntry implements Store with $pull.
func (s *MongoStore) DeleteEntry(ctx context.Context, householdID string, w Week, entryID string, now time.Time) (Plan, error) {
	eid, err := mongodb.ParseID(entryID)
	if err != nil {
		return Plan{}, ErrNotFound
	}
	update := bson.D{
		{Key: "$pull", Value: bson.D{{Key: "entries", Value: bson.D{{Key: "id", Value: eid}}}}},
		{Key: "$set", Value: bson.D{{Key: "updatedAt", Value: now}}},
	}
	return s.updateEntry(ctx, householdID, w, entryID, update)
}

// updateEntry applies update to a draft plan containing the entry, and works
// out why nothing matched.
func (s *MongoStore) updateEntry(ctx context.Context, householdID string, w Week, entryID string, update bson.D) (Plan, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Plan{}, ErrNotFound
	}
	eid, err := mongodb.ParseID(entryID)
	if err != nil {
		return Plan{}, ErrNotFound
	}
	filter := append(planFilter(hid, w),
		bson.E{Key: "status", Value: StatusDraft},
		bson.E{Key: "entries.id", Value: eid},
	)
	var out planDoc
	err = s.plans.FindOneAndUpdate(ctx, filter, update, afterUpdate).Decode(&out)
	if errors.Is(translate(err), ErrNotFound) {
		p, err := s.GetPlan(ctx, householdID, w)
		if err != nil {
			return Plan{}, err
		}
		if _, ok := p.entry(entryID); ok && p.Status == StatusFinalized {
			return Plan{}, ErrFinalized
		}
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, translate(err)
	}
	return out.toPlan()
}

// SetStatus implements Store with an upsert.
func (s *MongoStore) SetStatus(ctx context.Context, householdID string, w Week, status Status, now time.Time) (Plan, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Plan{}, ErrNotFound
	}
	update := bson.D{
		{Key: "$set", Value: bson.D{{Key: "status", Value: status}, {Key: "updatedAt", Value: now}}},
		{Key: "$setOnInsert", Value: insertOnly(w, now)},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After).SetUpsert(true)
	var out planDoc
	err = retryDuplicate(func() error {
		return s.plans.FindOneAndUpdate(ctx, planFilter(hid, w), update, opts).Decode(&out)
	})
	if err != nil {
		return Plan{}, translate(err)
	}
	return out.toPlan()
}

// retryDuplicate runs an upsert again once if it lost a race to create the
// same plan: the unique index rejects the second insert, and the retry then
// matches the plan the winner created.
func retryDuplicate(upsert func() error) error {
	err := upsert()
	if errors.Is(mongodb.TranslateError(err), mongodb.ErrDuplicate) {
		err = upsert()
	}
	return err
}

// translate maps platform errors to this package's sentinels.
func translate(err error) error {
	err = mongodb.TranslateError(err)
	if errors.Is(err, mongodb.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
