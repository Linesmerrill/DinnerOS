package recommendations

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

// Collections owned by this package.
const (
	ProfilesCollection     = "autopilot_profiles"
	WeekContextsCollection = "autopilot_week_contexts"
	ProposalsCollection    = "autopilot_proposals"
	OverridesCollection    = "autopilot_recipe_overrides"
)

// Indexes returns the indexes MongoStore relies on.
func Indexes() []mongodb.IndexSet {
	hhWeek := bson.D{{Key: "householdId", Value: 1}, {Key: "week", Value: 1}}
	return []mongodb.IndexSet{
		{Collection: ProfilesCollection, Indexes: []mongo.IndexModel{{
			Keys: bson.D{{Key: "householdId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("householdId_unique"),
		}}},
		{Collection: WeekContextsCollection, Indexes: []mongo.IndexModel{{
			Keys: hhWeek, Options: options.Index().SetUnique(true).SetName("householdId_week_unique"),
		}}},
		{Collection: ProposalsCollection, Indexes: []mongo.IndexModel{{
			Keys: hhWeek, Options: options.Index().SetUnique(true).SetName("householdId_week_unique"),
		}}},
		{Collection: OverridesCollection, Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "recipeId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_recipeId_unique"),
		}}},
		weekPairingsIndexes(),
	}
}

// MongoStore is the MongoDB implementation of Store. Documents are replaced
// whole, conditioned on their version.
type MongoStore struct {
	profiles  *mongo.Collection
	contexts  *mongo.Collection
	proposals *mongo.Collection
	overrides *mongo.Collection
	pairings  *mongo.Collection
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore returns a store using db.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{
		profiles: db.Collection(ProfilesCollection), contexts: db.Collection(WeekContextsCollection),
		proposals: db.Collection(ProposalsCollection), overrides: db.Collection(OverridesCollection),
		pairings: db.Collection(WeekPairingsCollection),
	}
}

// --- documents ------------------------------------------------------------------

// Documents leave _id to MongoDB, so replacements never touch it.

type choicesDoc struct {
	Cuisines []string `bson:"cuisines"`
	Tags     []string `bson:"tags"`
	Proteins []string `bson:"proteins"`
}

type changeDoc struct {
	UpdatedBy bson.ObjectID `bson:"updatedBy"`
	UpdatedAt time.Time     `bson:"updatedAt"`
}

type ruleDoc struct {
	Day       string   `bson:"day"`
	Label     string   `bson:"label"`
	Cuisines  []string `bson:"cuisines,omitempty"`
	Tags      []string `bson:"tags,omitempty"`
	Proteins  []string `bson:"proteins,omitempty"`
	Methods   []string `bson:"methods,omitempty"`
	TimeBand  string   `bson:"timeBand,omitempty"`
	Frequency string   `bson:"frequency"`
}

type profileDoc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	Taste       struct {
		Likes    choicesDoc `bson:"likes"`
		Dislikes choicesDoc `bson:"dislikes"`
	} `bson:"taste"`
	Restrictions struct {
		Diets               []string `bson:"diets"`
		Allergens           []string `bson:"allergens"`
		ExcludedIngredients []string `bson:"excludedIngredients"`
		ExcludedCuisines    []string `bson:"excludedCuisines"`
		ExcludedProteins    []string `bson:"excludedProteins"`
		ExcludedTags        []string `bson:"excludedTags"`
		NoSpicy             bool     `bson:"noSpicy"`
	} `bson:"restrictions"`
	Schedule struct {
		PlanDays            []string `bson:"planDays"`
		Weeknights          []string `bson:"weeknights"`
		MealsPerWeek        int      `bson:"mealsPerWeek"`
		DefaultServings     int      `bson:"defaultServings,omitempty"`
		WeeknightMaxMinutes int      `bson:"weeknightMaxMinutes,omitempty"`
	} `bson:"schedule"`
	CookTime struct {
		QuickMaxMinutes      int  `bson:"quickMaxMinutes"`
		MediumMaxMinutes     int  `bson:"mediumMaxMinutes"`
		MaxLongPerWeek       int  `bson:"maxLongPerWeek"`
		MinQuickPerWeek      int  `bson:"minQuickPerWeek"`
		AvoidConsecutiveLong bool `bson:"avoidConsecutiveLong"`
	} `bson:"cookTime"`
	Novelty      string               `bson:"novelty"`
	Equipment    []string             `bson:"equipment"`
	WeekdayRules []ruleDoc            `bson:"weekdayRules"`
	Pairings     []pairingRuleDoc     `bson:"pairings,omitempty"`
	Sections     map[string]changeDoc `bson:"sections"`
	Version      int64                `bson:"version"`
	CreatedBy    bson.ObjectID        `bson:"createdBy"`
	CreatedAt    time.Time            `bson:"createdAt"`
	UpdatedBy    bson.ObjectID        `bson:"updatedBy"`
	UpdatedAt    time.Time            `bson:"updatedAt"`
}

type dayDoc struct {
	Day        string `bson:"day"`
	Skip       bool   `bson:"skip,omitempty"`
	MaxMinutes int    `bson:"maxMinutes,omitempty"`
	Servings   int    `bson:"servings,omitempty"`
}

type contextDoc struct {
	HouseholdID  bson.ObjectID `bson:"householdId"`
	Week         string        `bson:"week"`
	Skip         bool          `bson:"skip"`
	Busy         bool          `bson:"busy"`
	MealsPerWeek int           `bson:"mealsPerWeek,omitempty"`
	MaxMinutes   int           `bson:"maxMinutes,omitempty"`
	Servings     int           `bson:"servings,omitempty"`
	Days         []dayDoc      `bson:"days"`
	Note         string        `bson:"note,omitempty"`
	Version      int64         `bson:"version"`
	UpdatedBy    bson.ObjectID `bson:"updatedBy"`
	UpdatedAt    time.Time     `bson:"updatedAt"`
}

type textDoc struct {
	Day  string `bson:"day,omitempty"`
	Code string `bson:"code"`
	Text string `bson:"text"`
}

type slotDoc struct {
	ID                string             `bson:"id"`
	Day               string             `bson:"day"`
	RecipeID          bson.ObjectID      `bson:"recipeId"`
	RecipeName        string             `bson:"recipeName"`
	RecipeImageURL    string             `bson:"recipeImageUrl,omitempty"`
	CookMinutes       int                `bson:"cookMinutes,omitempty"`
	TimeBand          string             `bson:"timeBand"`
	Servings          int                `bson:"servings"`
	Score             float64            `bson:"score"`
	Signals           map[string]float64 `bson:"signals"`
	Reasons           []textDoc          `bson:"reasons"`
	SwapCount         int                `bson:"swapCount"`
	RejectedRecipeIDs []bson.ObjectID    `bson:"rejectedRecipeIds,omitempty"`
	Pairings          []pairingDoc       `bson:"pairings,omitempty"`
}

type objectiveDoc struct {
	Meals    float64 `bson:"meals"`
	Variety  float64 `bson:"variety"`
	CookTime float64 `bson:"cookTime"`
	Rules    float64 `bson:"rules"`
	Novelty  float64 `bson:"novelty"`
	Total    float64 `bson:"total"`
}

type proposalDoc struct {
	HouseholdID   bson.ObjectID  `bson:"householdId"`
	Week          string         `bson:"week"`
	ProposalID    bson.ObjectID  `bson:"proposalId"`
	Status        string         `bson:"status"`
	Version       int64          `bson:"version"`
	Attempt       int            `bson:"attempt"`
	ModelVersion  string         `bson:"modelVersion"`
	InputsHash    string         `bson:"inputsHash"`
	Requested     int            `bson:"requested"`
	Planned       int            `bson:"planned"`
	Candidates    int            `bson:"candidates"`
	ColdStart     bool           `bson:"coldStart"`
	Slots         []slotDoc      `bson:"slots"`
	Unfilled      []textDoc      `bson:"unfilled"`
	Messages      []textDoc      `bson:"messages"`
	Objective     objectiveDoc   `bson:"objective"`
	SwapCount     int            `bson:"swapCount"`
	ExcludedSlots []string       `bson:"excludedSlots,omitempty"`
	GeneratedBy   bson.ObjectID  `bson:"generatedBy"`
	GeneratedAt   time.Time      `bson:"generatedAt"`
	UpdatedAt     time.Time      `bson:"updatedAt"`
	DecidedBy     *bson.ObjectID `bson:"decidedBy,omitempty"`
	DecidedAt     *time.Time     `bson:"decidedAt,omitempty"`
}

type overrideDoc struct {
	HouseholdID    bson.ObjectID   `bson:"householdId"`
	RecipeID       bson.ObjectID   `bson:"recipeId"`
	Methods        map[string]bool `bson:"methods"`
	MealCategories map[string]bool `bson:"mealCategories,omitempty"`
	UpdatedBy      bson.ObjectID   `bson:"updatedBy"`
	UpdatedAt      time.Time       `bson:"updatedAt"`
}

// --- conversions ----------------------------------------------------------------

// ids parses hex IDs; the first malformed one fails with its field name.
type ids struct{ err error }

func (p *ids) parse(field, hex string) bson.ObjectID {
	if p.err != nil {
		return bson.ObjectID{}
	}
	id, err := mongodb.ParseID(hex)
	if err != nil {
		p.err = fmt.Errorf("recommendations: %s: %w", field, err)
	}
	return id
}

func hexOrEmpty(id bson.ObjectID) string {
	if id.IsZero() {
		return ""
	}
	return id.Hex()
}

func newProfileDoc(p Profile) (profileDoc, error) {
	var in ids
	d := profileDoc{
		HouseholdID: in.parse("household id", p.HouseholdID), Novelty: p.Novelty, Equipment: orEmpty(p.Equipment),
		Version: p.Version, CreatedBy: in.parse("created by", p.CreatedBy), CreatedAt: p.CreatedAt,
		UpdatedBy: in.parse("updated by", p.UpdatedBy), UpdatedAt: p.UpdatedAt, Sections: map[string]changeDoc{},
	}
	d.Taste.Likes = choicesDoc{orEmpty(p.Taste.Likes.Cuisines), orEmpty(p.Taste.Likes.Tags), orEmpty(p.Taste.Likes.Proteins)}
	d.Taste.Dislikes = choicesDoc{orEmpty(p.Taste.Dislikes.Cuisines), orEmpty(p.Taste.Dislikes.Tags), orEmpty(p.Taste.Dislikes.Proteins)}
	r := p.Restrictions
	d.Restrictions.Diets, d.Restrictions.Allergens = orEmpty(r.Diets), orEmpty(r.Allergens)
	d.Restrictions.ExcludedIngredients, d.Restrictions.ExcludedCuisines = orEmpty(r.ExcludedIngredients), orEmpty(r.ExcludedCuisines)
	d.Restrictions.ExcludedProteins, d.Restrictions.ExcludedTags, d.Restrictions.NoSpicy = orEmpty(r.ExcludedProteins), orEmpty(r.ExcludedTags), r.NoSpicy
	s := p.Schedule
	d.Schedule.PlanDays, d.Schedule.Weeknights, d.Schedule.MealsPerWeek = orEmpty(s.PlanDays), orEmpty(s.Weeknights), s.MealsPerWeek
	d.Schedule.DefaultServings, d.Schedule.WeeknightMaxMinutes = s.DefaultServings, s.WeeknightMaxMinutes
	c := p.CookTime
	d.CookTime.QuickMaxMinutes, d.CookTime.MediumMaxMinutes = c.QuickMaxMinutes, c.MediumMaxMinutes
	d.CookTime.MaxLongPerWeek, d.CookTime.MinQuickPerWeek, d.CookTime.AvoidConsecutiveLong = c.MaxLongPerWeek, c.MinQuickPerWeek, c.AvoidConsecutiveLong
	d.WeekdayRules = []ruleDoc{}
	for _, rule := range p.WeekdayRules {
		d.WeekdayRules = append(d.WeekdayRules, ruleDoc(rule))
	}
	d.Pairings = newPairingRuleDocs(&in, p.Pairings)
	for section, change := range p.Sections {
		d.Sections[string(section)] = changeDoc{UpdatedBy: in.parse("section updated by", change.UpdatedBy), UpdatedAt: change.UpdatedAt}
	}
	return d, in.err
}

func (d profileDoc) toProfile() Profile {
	p := Profile{
		HouseholdID: d.HouseholdID.Hex(), Novelty: d.Novelty, Equipment: nilIfEmpty(d.Equipment), Version: d.Version,
		CreatedBy: hexOrEmpty(d.CreatedBy), CreatedAt: d.CreatedAt.UTC(), UpdatedBy: hexOrEmpty(d.UpdatedBy), UpdatedAt: d.UpdatedAt.UTC(),
		Sections: map[Section]Change{},
	}
	p.Taste.Likes = Choices{nilIfEmpty(d.Taste.Likes.Cuisines), nilIfEmpty(d.Taste.Likes.Tags), nilIfEmpty(d.Taste.Likes.Proteins)}
	p.Taste.Dislikes = Choices{nilIfEmpty(d.Taste.Dislikes.Cuisines), nilIfEmpty(d.Taste.Dislikes.Tags), nilIfEmpty(d.Taste.Dislikes.Proteins)}
	r := d.Restrictions
	p.Restrictions = Restrictions{
		Diets: nilIfEmpty(r.Diets), Allergens: nilIfEmpty(r.Allergens), ExcludedIngredients: nilIfEmpty(r.ExcludedIngredients),
		ExcludedCuisines: nilIfEmpty(r.ExcludedCuisines), ExcludedProteins: nilIfEmpty(r.ExcludedProteins),
		ExcludedTags: nilIfEmpty(r.ExcludedTags), NoSpicy: r.NoSpicy,
	}
	s := d.Schedule
	p.Schedule = Schedule{
		PlanDays: nilIfEmpty(s.PlanDays), Weeknights: nilIfEmpty(s.Weeknights), MealsPerWeek: s.MealsPerWeek,
		DefaultServings: s.DefaultServings, WeeknightMaxMinutes: s.WeeknightMaxMinutes,
	}
	c := d.CookTime
	p.CookTime = CookTime{c.QuickMaxMinutes, c.MediumMaxMinutes, c.MaxLongPerWeek, c.MinQuickPerWeek, c.AvoidConsecutiveLong}
	for _, rule := range d.WeekdayRules {
		rule.Cuisines, rule.Tags, rule.Proteins, rule.Methods = nilIfEmpty(rule.Cuisines), nilIfEmpty(rule.Tags), nilIfEmpty(rule.Proteins), nilIfEmpty(rule.Methods)
		p.WeekdayRules = append(p.WeekdayRules, WeekdayRule(rule))
	}
	p.Pairings = pairingRulesFromDocs(d.Pairings)
	for section, change := range d.Sections {
		p.Sections[Section(section)] = Change{UpdatedBy: hexOrEmpty(change.UpdatedBy), UpdatedAt: change.UpdatedAt.UTC()}
	}
	return p
}

func newContextDoc(c WeekContext) (contextDoc, error) {
	var in ids
	d := contextDoc{
		HouseholdID: in.parse("household id", c.HouseholdID), Week: c.Week, Skip: c.Skip, Busy: c.Busy,
		MealsPerWeek: c.MealsPerWeek, MaxMinutes: c.MaxMinutes, Servings: c.Servings, Days: []dayDoc{}, Note: c.Note,
		Version: c.Version, UpdatedBy: in.parse("updated by", c.UpdatedBy), UpdatedAt: c.UpdatedAt,
	}
	for _, day := range c.Days {
		d.Days = append(d.Days, dayDoc(day))
	}
	return d, in.err
}

func (d contextDoc) toContext() WeekContext {
	c := WeekContext{
		HouseholdID: d.HouseholdID.Hex(), Week: d.Week, Skip: d.Skip, Busy: d.Busy, MealsPerWeek: d.MealsPerWeek,
		MaxMinutes: d.MaxMinutes, Servings: d.Servings, Note: d.Note, Version: d.Version,
		UpdatedBy: hexOrEmpty(d.UpdatedBy), UpdatedAt: d.UpdatedAt.UTC(),
	}
	for _, day := range d.Days {
		c.Days = append(c.Days, DayOverride(day))
	}
	return c
}

func newProposalDoc(p Proposal) (proposalDoc, error) {
	var in ids
	d := proposalDoc{
		HouseholdID: in.parse("household id", p.HouseholdID), Week: p.Week, ProposalID: in.parse("proposal id", p.ID),
		Status: string(p.Status), Version: p.Version, Attempt: p.Attempt, ModelVersion: p.ModelVersion, InputsHash: p.InputsHash,
		Requested: p.Requested, Planned: p.Planned, Candidates: p.Candidates, ColdStart: p.ColdStart,
		Slots: []slotDoc{}, Unfilled: []textDoc{}, Messages: []textDoc{}, Objective: objectiveDoc(p.Objective),
		SwapCount: p.SwapCount, ExcludedSlots: p.ExcludedSlots,
		GeneratedBy: in.parse("generated by", p.GeneratedBy), GeneratedAt: p.GeneratedAt, UpdatedAt: p.UpdatedAt,
	}
	for _, s := range p.Slots {
		sd := slotDoc{
			ID: s.ID, Day: s.Day, RecipeID: in.parse("slot recipe id", s.RecipeID), RecipeName: s.RecipeName,
			RecipeImageURL: s.RecipeImageURL, CookMinutes: s.CookMinutes, TimeBand: s.TimeBand, Servings: s.Servings,
			Score: s.Score, Signals: s.Signals, Reasons: []textDoc{}, SwapCount: s.SwapCount,
		}
		for _, r := range s.Reasons {
			sd.Reasons = append(sd.Reasons, textDoc{Code: r.Code, Text: r.Text})
		}
		for _, id := range s.RejectedRecipeIDs {
			sd.RejectedRecipeIDs = append(sd.RejectedRecipeIDs, in.parse("rejected recipe id", id))
		}
		sd.Pairings = newPairingDocs(&in, s.Pairings)
		d.Slots = append(d.Slots, sd)
	}
	for _, u := range p.Unfilled {
		d.Unfilled = append(d.Unfilled, textDoc(u))
	}
	for _, m := range p.Messages {
		d.Messages = append(d.Messages, textDoc{Code: m.Code, Text: m.Text})
	}
	if p.DecidedBy != "" {
		id := in.parse("decided by", p.DecidedBy)
		at := p.DecidedAt
		d.DecidedBy, d.DecidedAt = &id, &at
	}
	return d, in.err
}

func (d proposalDoc) toProposal() Proposal {
	p := Proposal{
		ID: d.ProposalID.Hex(), HouseholdID: d.HouseholdID.Hex(), Week: d.Week, Status: ProposalStatus(d.Status),
		Version: d.Version, Attempt: d.Attempt, ModelVersion: d.ModelVersion, InputsHash: d.InputsHash,
		Requested: d.Requested, Planned: d.Planned, Candidates: d.Candidates, ColdStart: d.ColdStart,
		Objective: Objective(d.Objective), SwapCount: d.SwapCount, ExcludedSlots: nilIfEmpty(d.ExcludedSlots),
		GeneratedBy: hexOrEmpty(d.GeneratedBy), GeneratedAt: d.GeneratedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	for _, sd := range d.Slots {
		s := Slot{
			ID: sd.ID, Day: sd.Day, RecipeID: sd.RecipeID.Hex(), RecipeName: sd.RecipeName, RecipeImageURL: sd.RecipeImageURL,
			CookMinutes: sd.CookMinutes, TimeBand: sd.TimeBand, Servings: sd.Servings, Score: sd.Score, Signals: sd.Signals,
			SwapCount: sd.SwapCount,
		}
		for _, r := range sd.Reasons {
			s.Reasons = append(s.Reasons, Reason{Code: r.Code, Text: r.Text})
		}
		for _, id := range sd.RejectedRecipeIDs {
			s.RejectedRecipeIDs = append(s.RejectedRecipeIDs, id.Hex())
		}
		s.Pairings = pairingsFromDocs(sd.Pairings)
		p.Slots = append(p.Slots, s)
	}
	for _, u := range d.Unfilled {
		p.Unfilled = append(p.Unfilled, Unfilled(u))
	}
	for _, m := range d.Messages {
		p.Messages = append(p.Messages, Message{Code: m.Code, Text: m.Text})
	}
	if d.DecidedBy != nil {
		p.DecidedBy = d.DecidedBy.Hex()
	}
	if d.DecidedAt != nil {
		p.DecidedAt = d.DecidedAt.UTC()
	}
	return p
}

// --- Store ----------------------------------------------------------------------

func translate(err error) error {
	err = mongodb.TranslateError(err)
	switch {
	case errors.Is(err, mongodb.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, mongodb.ErrDuplicate):
		return ErrConflict
	}
	return err
}

// save inserts doc when version is 0 and otherwise replaces the document
// matching filter at that version.
func save(ctx context.Context, coll *mongo.Collection, filter bson.D, version int64, doc any) error {
	if version == 0 {
		_, err := coll.InsertOne(ctx, doc)
		return translate(err)
	}
	res, err := coll.ReplaceOne(ctx, append(filter, bson.E{Key: "version", Value: version}), doc)
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 0 {
		return ErrConflict
	}
	return nil
}

// GetProfile implements Store.
func (s *MongoStore) GetProfile(ctx context.Context, householdID string) (Profile, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Profile{}, ErrNotFound
	}
	var d profileDoc
	if err := s.profiles.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}}).Decode(&d); err != nil {
		return Profile{}, translate(err)
	}
	return d.toProfile(), nil
}

// SaveProfile implements Store.
func (s *MongoStore) SaveProfile(ctx context.Context, p Profile) (Profile, error) {
	d, err := newProfileDoc(p)
	if err != nil {
		return Profile{}, err
	}
	d.Version = p.Version + 1
	if err := save(ctx, s.profiles, bson.D{{Key: "householdId", Value: d.HouseholdID}}, p.Version, d); err != nil {
		return Profile{}, err
	}
	p.Version = d.Version
	return p, nil
}

// GetWeekContext implements Store.
func (s *MongoStore) GetWeekContext(ctx context.Context, householdID, week string) (WeekContext, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return WeekContext{}, ErrNotFound
	}
	var d contextDoc
	if err := s.contexts.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}}).Decode(&d); err != nil {
		return WeekContext{}, translate(err)
	}
	return d.toContext(), nil
}

// SaveWeekContext implements Store.
func (s *MongoStore) SaveWeekContext(ctx context.Context, c WeekContext) (WeekContext, error) {
	d, err := newContextDoc(c)
	if err != nil {
		return WeekContext{}, err
	}
	d.Version = c.Version + 1
	filter := bson.D{{Key: "householdId", Value: d.HouseholdID}, {Key: "week", Value: c.Week}}
	if err := save(ctx, s.contexts, filter, c.Version, d); err != nil {
		return WeekContext{}, err
	}
	c.Version = d.Version
	return c, nil
}

// DeleteWeekContext implements Store.
func (s *MongoStore) DeleteWeekContext(ctx context.Context, householdID, week string) (WeekContext, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return WeekContext{}, ErrNotFound
	}
	var d contextDoc
	if err := s.contexts.FindOneAndDelete(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}}).Decode(&d); err != nil {
		return WeekContext{}, translate(err)
	}
	return d.toContext(), nil
}

// GetProposal implements Store.
func (s *MongoStore) GetProposal(ctx context.Context, householdID, week string) (Proposal, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Proposal{}, ErrNotFound
	}
	var d proposalDoc
	if err := s.proposals.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}}).Decode(&d); err != nil {
		return Proposal{}, translate(err)
	}
	return d.toProposal(), nil
}

// SaveProposal implements Store.
func (s *MongoStore) SaveProposal(ctx context.Context, p Proposal) (Proposal, error) {
	d, err := newProposalDoc(p)
	if err != nil {
		return Proposal{}, err
	}
	d.Version = p.Version + 1
	filter := bson.D{{Key: "householdId", Value: d.HouseholdID}, {Key: "week", Value: p.Week}}
	if err := save(ctx, s.proposals, filter, p.Version, d); err != nil {
		return Proposal{}, err
	}
	p.Version = d.Version
	return p, nil
}

// ListOverrides implements Store.
func (s *MongoStore) ListOverrides(ctx context.Context, householdID string) ([]RecipeOverride, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	cur, err := s.overrides.Find(ctx, bson.D{{Key: "householdId", Value: hid}}, options.Find().SetSort(bson.D{{Key: "recipeId", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []overrideDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	var out []RecipeOverride
	for _, d := range docs {
		out = append(out, d.toOverride())
	}
	return out, nil
}

func (d overrideDoc) toOverride() RecipeOverride {
	return RecipeOverride{
		HouseholdID: d.HouseholdID.Hex(), RecipeID: d.RecipeID.Hex(), Methods: d.Methods, MealCategories: d.MealCategories,
		UpdatedBy: hexOrEmpty(d.UpdatedBy), UpdatedAt: d.UpdatedAt.UTC(),
	}
}

// GetOverride implements Store.
func (s *MongoStore) GetOverride(ctx context.Context, householdID, recipeID string) (RecipeOverride, error) {
	var in ids
	filter := bson.D{{Key: "householdId", Value: in.parse("household id", householdID)}, {Key: "recipeId", Value: in.parse("recipe id", recipeID)}}
	if in.err != nil {
		return RecipeOverride{}, ErrNotFound
	}
	var d overrideDoc
	if err := s.overrides.FindOne(ctx, filter).Decode(&d); err != nil {
		return RecipeOverride{}, translate(err)
	}
	return d.toOverride(), nil
}

// SaveOverride implements Store.
func (s *MongoStore) SaveOverride(ctx context.Context, o RecipeOverride) error {
	var in ids
	d := overrideDoc{
		HouseholdID: in.parse("household id", o.HouseholdID), RecipeID: in.parse("recipe id", o.RecipeID),
		Methods: orEmptyMap(o.Methods), MealCategories: o.MealCategories, UpdatedBy: in.parse("updated by", o.UpdatedBy), UpdatedAt: o.UpdatedAt,
	}
	if in.err != nil {
		return in.err
	}
	filter := bson.D{{Key: "householdId", Value: d.HouseholdID}, {Key: "recipeId", Value: d.RecipeID}}
	if len(o.Methods) == 0 && len(o.MealCategories) == 0 {
		_, err := s.overrides.DeleteOne(ctx, filter)
		return translate(err)
	}
	_, err := s.overrides.ReplaceOne(ctx, filter, d, options.Replace().SetUpsert(true))
	if errors.Is(translate(err), ErrConflict) {
		// Two first overrides raced; the retry replaces the winner's.
		_, err = s.overrides.ReplaceOne(ctx, filter, d, options.Replace().SetUpsert(true))
	}
	return translate(err)
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func orEmptyMap(m map[string]bool) map[string]bool {
	if m == nil {
		return map[string]bool{}
	}
	return m
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}
