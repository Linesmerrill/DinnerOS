package recommendations

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// WeekPairingsCollection holds each week's pairing decisions and grocery
// items.
const WeekPairingsCollection = "autopilot_week_pairings"

// PairingStore persists a week's pairing state, with the same optimistic
// concurrency as Store: Version 0 inserts, other versions replace only while
// the stored version matches, and a save returns Version+1.
type PairingStore interface {
	// GetWeekPairings returns a week's pairing state, or ErrNotFound.
	GetWeekPairings(ctx context.Context, householdID, week string) (WeekPairings, error)
	SaveWeekPairings(ctx context.Context, wp WeekPairings) (WeekPairings, error)
}

var _ PairingStore = (*MongoStore)(nil)

func weekPairingsIndexes() mongodb.IndexSet {
	return mongodb.IndexSet{Collection: WeekPairingsCollection, Indexes: []mongo.IndexModel{{
		Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "week", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("householdId_week_unique"),
	}}}
}

// --- documents ------------------------------------------------------------------

type groceryItemDoc struct {
	Name     string `bson:"name"`
	Quantity string `bson:"quantity,omitempty"`
	Unit     string `bson:"unit,omitempty"`
}

type pairingWhenDoc struct {
	MealCategories []string `bson:"mealCategories,omitempty"`
	Cuisines       []string `bson:"cuisines,omitempty"`
	Tags           []string `bson:"tags,omitempty"`
	Proteins       []string `bson:"proteins,omitempty"`
}

type pairingRuleDoc struct {
	ID          bson.ObjectID   `bson:"id"`
	Label       string          `bson:"label,omitempty"`
	When        pairingWhenDoc  `bson:"when"`
	RecipeID    *bson.ObjectID  `bson:"recipeId,omitempty"`
	RecipeName  string          `bson:"recipeName,omitempty"`
	GroceryItem *groceryItemDoc `bson:"groceryItem,omitempty"`
	Frequency   string          `bson:"frequency"`
}

type pairingRecipeDoc struct {
	ID              bson.ObjectID `bson:"id"`
	Name            string        `bson:"name"`
	Headline        string        `bson:"headline,omitempty"`
	ImageURL        string        `bson:"imageUrl,omitempty"`
	CookMinutes     int           `bson:"cookMinutes,omitempty"`
	TimesOrdered    int           `bson:"timesOrdered,omitempty"`
	LastOrderedWeek string        `bson:"lastOrderedWeek,omitempty"`
}

type learnedDoc struct {
	WeeksTogether int     `bson:"weeksTogether"`
	CategoryWeeks int     `bson:"categoryWeeks"`
	OtherWeeks    int     `bson:"otherWeeks"`
	OtherRate     float64 `bson:"otherRate"`
}

// pairingDoc is a pairing snapshot on a proposal slot.
type pairingDoc struct {
	Key          string            `bson:"key"`
	Kind         string            `bson:"kind"`
	Recipe       *pairingRecipeDoc `bson:"recipe,omitempty"`
	GroceryItem  *groceryItemDoc   `bson:"groceryItem,omitempty"`
	Servings     int               `bson:"servings,omitempty"`
	Source       string            `bson:"source"`
	Frequency    string            `bson:"frequency"`
	MealCategory string            `bson:"mealCategory,omitempty"`
	RuleID       string            `bson:"ruleId,omitempty"`
	Confidence   float64           `bson:"confidence,omitempty"`
	Learned      *learnedDoc       `bson:"learned,omitempty"`
	Reason       string            `bson:"reason"`
	Included     bool              `bson:"included"`
}

type decisionDoc struct {
	EntryID       string        `bson:"entryId"`
	Key           string        `bson:"key"`
	Status        string        `bson:"status"`
	AddedEntryID  string        `bson:"addedEntryId,omitempty"`
	GroceryItemID string        `bson:"groceryItemId,omitempty"`
	DecidedBy     bson.ObjectID `bson:"decidedBy"`
	DecidedAt     time.Time     `bson:"decidedAt"`
}

type weekGroceryItemDoc struct {
	ID            bson.ObjectID `bson:"id"`
	Key           string        `bson:"key"`
	Name          string        `bson:"name"`
	Quantity      string        `bson:"quantity,omitempty"`
	Unit          string        `bson:"unit,omitempty"`
	ForEntryID    string        `bson:"forEntryId"`
	ForRecipeID   bson.ObjectID `bson:"forRecipeId"`
	ForRecipeName string        `bson:"forRecipeName"`
	Source        string        `bson:"source"`
	RuleID        string        `bson:"ruleId,omitempty"`
	AddedBy       bson.ObjectID `bson:"addedBy"`
	AddedAt       time.Time     `bson:"addedAt"`
}

type weekPairingsDoc struct {
	HouseholdID  bson.ObjectID        `bson:"householdId"`
	Week         string               `bson:"week"`
	Decisions    []decisionDoc        `bson:"decisions"`
	GroceryItems []weekGroceryItemDoc `bson:"groceryItems"`
	Version      int64                `bson:"version"`
	UpdatedAt    time.Time            `bson:"updatedAt"`
}

// --- conversions ----------------------------------------------------------------

func newGroceryItemDoc(g *GroceryItem) *groceryItemDoc {
	if g == nil {
		return nil
	}
	return &groceryItemDoc{Name: g.Name, Quantity: g.Quantity, Unit: g.Unit}
}

func (d *groceryItemDoc) toGroceryItem() *GroceryItem {
	if d == nil {
		return nil
	}
	return &GroceryItem{Name: d.Name, Quantity: d.Quantity, Unit: d.Unit}
}

func newPairingRuleDocs(in *ids, rules []PairingRule) []pairingRuleDoc {
	out := make([]pairingRuleDoc, 0, len(rules))
	for _, r := range rules {
		d := pairingRuleDoc{
			ID: in.parse("pairing rule id", r.ID), Label: r.Label, Frequency: r.Frequency,
			When:        pairingWhenDoc{MealCategories: r.When.MealCategories, Cuisines: r.When.Cuisines, Tags: r.When.Tags, Proteins: r.When.Proteins},
			GroceryItem: newGroceryItemDoc(r.Add.GroceryItem), RecipeName: r.Add.RecipeName,
		}
		if r.Add.RecipeID != "" {
			id := in.parse("pairing recipe id", r.Add.RecipeID)
			d.RecipeID = &id
		}
		out = append(out, d)
	}
	return out
}

func pairingRulesFromDocs(docs []pairingRuleDoc) []PairingRule {
	var out []PairingRule
	for _, d := range docs {
		r := PairingRule{
			ID: d.ID.Hex(), Label: d.Label, Frequency: d.Frequency,
			When: PairingWhen{
				MealCategories: nilIfEmpty(d.When.MealCategories), Cuisines: nilIfEmpty(d.When.Cuisines),
				Tags: nilIfEmpty(d.When.Tags), Proteins: nilIfEmpty(d.When.Proteins),
			},
			Add: PairingTarget{RecipeName: d.RecipeName, GroceryItem: d.GroceryItem.toGroceryItem()},
		}
		if d.RecipeID != nil {
			r.Add.RecipeID = d.RecipeID.Hex()
		}
		out = append(out, r)
	}
	return out
}

func newPairingDocs(in *ids, pairings []Pairing) []pairingDoc {
	var out []pairingDoc
	for _, p := range pairings {
		d := pairingDoc{
			Key: p.Key, Kind: p.Kind, GroceryItem: newGroceryItemDoc(p.GroceryItem), Servings: p.Servings, Source: p.Source,
			Frequency: p.Frequency, MealCategory: p.MealCategory, RuleID: p.RuleID, Confidence: p.Confidence, Reason: p.Reason,
			Included: p.Included,
		}
		if r := p.Recipe; r != nil {
			d.Recipe = &pairingRecipeDoc{
				ID: in.parse("pairing recipe id", r.ID), Name: r.Name, Headline: r.Headline, ImageURL: r.ImageURL,
				CookMinutes: r.CookMinutes, TimesOrdered: r.TimesOrdered, LastOrderedWeek: r.LastOrderedWeek,
			}
		}
		if l := p.Learned; l != nil {
			learned := learnedDoc(*l)
			d.Learned = &learned
		}
		out = append(out, d)
	}
	return out
}

func pairingsFromDocs(docs []pairingDoc) []Pairing {
	var out []Pairing
	for _, d := range docs {
		p := Pairing{
			Key: d.Key, Kind: d.Kind, GroceryItem: d.GroceryItem.toGroceryItem(), Servings: d.Servings, Source: d.Source,
			Frequency: d.Frequency, MealCategory: d.MealCategory, RuleID: d.RuleID, Confidence: d.Confidence, Reason: d.Reason,
			Included: d.Included,
		}
		if r := d.Recipe; r != nil {
			p.Recipe = &PairingRecipe{
				ID: r.ID.Hex(), Name: r.Name, Headline: r.Headline, ImageURL: r.ImageURL, CookMinutes: r.CookMinutes,
				TimesOrdered: r.TimesOrdered, LastOrderedWeek: r.LastOrderedWeek,
			}
		}
		if l := d.Learned; l != nil {
			learned := LearnedPairing(*l)
			p.Learned = &learned
		}
		out = append(out, p)
	}
	return out
}

func newWeekPairingsDoc(wp WeekPairings) (weekPairingsDoc, error) {
	var in ids
	d := weekPairingsDoc{
		HouseholdID: in.parse("household id", wp.HouseholdID), Week: wp.Week, Decisions: []decisionDoc{},
		GroceryItems: []weekGroceryItemDoc{}, Version: wp.Version, UpdatedAt: wp.UpdatedAt,
	}
	for _, dec := range wp.Decisions {
		d.Decisions = append(d.Decisions, decisionDoc{
			EntryID: dec.EntryID, Key: dec.Key, Status: dec.Status, AddedEntryID: dec.AddedEntryID, GroceryItemID: dec.GroceryItemID,
			DecidedBy: in.parse("decided by", dec.DecidedBy), DecidedAt: dec.DecidedAt,
		})
	}
	for _, g := range wp.GroceryItems {
		d.GroceryItems = append(d.GroceryItems, weekGroceryItemDoc{
			ID: in.parse("grocery item id", g.ID), Key: g.Key, Name: g.Name, Quantity: g.Quantity, Unit: g.Unit,
			ForEntryID: g.ForEntryID, ForRecipeID: in.parse("grocery item recipe id", g.ForRecipeID), ForRecipeName: g.ForRecipeName,
			Source: g.Source, RuleID: g.RuleID, AddedBy: in.parse("added by", g.AddedBy), AddedAt: g.AddedAt,
		})
	}
	return d, in.err
}

func (d weekPairingsDoc) toWeekPairings() WeekPairings {
	wp := WeekPairings{HouseholdID: d.HouseholdID.Hex(), Week: d.Week, Version: d.Version, UpdatedAt: d.UpdatedAt.UTC()}
	for _, dec := range d.Decisions {
		wp.Decisions = append(wp.Decisions, PairingDecision{
			EntryID: dec.EntryID, Key: dec.Key, Status: dec.Status, AddedEntryID: dec.AddedEntryID, GroceryItemID: dec.GroceryItemID,
			DecidedBy: hexOrEmpty(dec.DecidedBy), DecidedAt: dec.DecidedAt.UTC(),
		})
	}
	for _, g := range d.GroceryItems {
		wp.GroceryItems = append(wp.GroceryItems, WeekGroceryItem{
			ID: g.ID.Hex(), Key: g.Key, GroceryItem: GroceryItem{Name: g.Name, Quantity: g.Quantity, Unit: g.Unit},
			ForEntryID: g.ForEntryID, ForRecipeID: g.ForRecipeID.Hex(), ForRecipeName: g.ForRecipeName, Source: g.Source,
			RuleID: g.RuleID, AddedBy: hexOrEmpty(g.AddedBy), AddedAt: g.AddedAt.UTC(),
		})
	}
	return wp
}

// --- PairingStore ---------------------------------------------------------------

// GetWeekPairings implements PairingStore.
func (s *MongoStore) GetWeekPairings(ctx context.Context, householdID, week string) (WeekPairings, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return WeekPairings{}, ErrNotFound
	}
	var d weekPairingsDoc
	if err := s.pairings.FindOne(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}}).Decode(&d); err != nil {
		return WeekPairings{}, translate(err)
	}
	return d.toWeekPairings(), nil
}

// SaveWeekPairings implements PairingStore.
func (s *MongoStore) SaveWeekPairings(ctx context.Context, wp WeekPairings) (WeekPairings, error) {
	d, err := newWeekPairingsDoc(wp)
	if err != nil {
		return WeekPairings{}, err
	}
	d.Version = wp.Version + 1
	filter := bson.D{{Key: "householdId", Value: d.HouseholdID}, {Key: "week", Value: wp.Week}}
	if err := save(ctx, s.pairings, filter, wp.Version, d); err != nil {
		return WeekPairings{}, err
	}
	wp.Version = d.Version
	return wp, nil
}
