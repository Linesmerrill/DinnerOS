package catalog

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Moderation (decision 744), on top of reports (decision 686). Apple asks
// apps where people share content to let them block abusive users and to act
// on reports:
//
//   - A member can hide every recipe from the cook who shared one. The
//     catalog never knows the household, only an opaque publisher hash
//     (recipes.CatalogPublisherKey), so the block is kept against that hash.
//     An importer's recipe has no cook, so hiding it hides that recipe.
//   - An operator lists reported recipes and keeps or removes each one. A kept
//     recipe is not pulled for everyone by reports any more (its reporters
//     still never see it); a removed one is hidden for everyone at once.

// Block is a household hiding a cook's recipes.
type Block struct {
	HouseholdID string
	UserID      string
	// CatalogID is the recipe the member hid the cook from.
	CatalogID string
	// Publisher is that recipe's Recipe.Publisher, empty for an importer's.
	Publisher string
	At        time.Time
}

// BlockStore keeps blocks.
type BlockStore interface {
	// Add stores b; hiding from the same recipe twice keeps the first.
	Add(ctx context.Context, b Block) error
	// Blocks returns the household's blocks.
	Blocks(ctx context.Context, householdID string) ([]Block, error)
}

// Review decisions.
const (
	DecisionKeep   = "keep"
	DecisionRemove = "remove"
)

// ErrInvalidReview means the decision is not DecisionKeep or DecisionRemove.
var ErrInvalidReview = errors.New("catalog: invalid review")

// Review is an operator's decision on a reported recipe.
type Review struct {
	CatalogID string
	Decision  string
	UserID    string
	At        time.Time
}

// ReviewStore keeps operators' decisions, one per catalog entry.
type ReviewStore interface {
	// Decide stores r, replacing an earlier decision on the same entry.
	Decide(ctx context.Context, r Review) error
	// Decisions maps each reviewed catalog ID to its decision.
	Decisions(ctx context.Context) (map[string]string, error)
}

// WithModeration turns on hiding cooks and operator reviews and returns s.
func (s *Service) WithModeration(b BlockStore, r ReviewStore) *Service {
	s.blocks, s.reviews = b, r
	return s
}

// HidesCook reports whether hiding e hides a cook (every recipe it shared)
// rather than just e.
func HidesCook(e Recipe) bool { return e.Publisher != "" }

// Block hides the cook of a catalog recipe from the household, or just the
// recipe when it has no cook, and returns the recipe.
func (s *Service) Block(ctx context.Context, householdID, userID, catalogID string) (Recipe, error) {
	if s.blocks == nil {
		return Recipe{}, errors.New("catalog: blocks are not wired")
	}
	e, err := s.store.Get(ctx, catalogID)
	if err != nil {
		return Recipe{}, err
	}
	b := Block{HouseholdID: householdID, UserID: userID, CatalogID: e.ID, Publisher: e.Publisher, At: s.now().UTC()}
	if err := s.blocks.Add(ctx, b); err != nil {
		return Recipe{}, err
	}
	return e, nil
}

// blockedIDs are the catalog entries the household's blocks hide: each
// blocked recipe and every entry from a blocked cook. A failed read hides
// nothing extra.
func (s *Service) blockedIDs(ctx context.Context, householdID string) []string {
	if s.blocks == nil {
		return nil
	}
	blocks, err := s.blocks.Blocks(ctx, householdID)
	if err != nil || len(blocks) == 0 {
		return nil
	}
	var ids, publishers []string
	for _, b := range blocks {
		ids = append(ids, b.CatalogID)
		if b.Publisher != "" && !slices.Contains(publishers, b.Publisher) {
			publishers = append(publishers, b.Publisher)
		}
	}
	if len(publishers) > 0 {
		if more, err := s.store.IDsByPublisher(ctx, publishers); err == nil {
			ids = append(ids, more...)
		}
	}
	return ids
}

// Review records an operator's decision on a catalog recipe.
func (s *Service) Review(ctx context.Context, r Review) error {
	if s.reviews == nil {
		return errors.New("catalog: reviews are not wired")
	}
	r.Decision = strings.ToLower(strings.TrimSpace(r.Decision))
	if r.Decision != DecisionKeep && r.Decision != DecisionRemove {
		return fmt.Errorf("%w: decision must be keep or remove", ErrInvalidReview)
	}
	if _, err := s.store.Get(ctx, r.CatalogID); err != nil {
		return err
	}
	r.At = s.now().UTC()
	return s.reviews.Decide(ctx, r)
}

// ReportedRecipe is one reported catalog recipe on the operator's list.
type ReportedRecipe struct {
	CatalogID string
	Name      string
	Reports   []Report
	// Households is how many different households reported it.
	Households int
	// Decision is the operator's, empty while it waits for review.
	Decision string
	// HiddenForEveryone is true when no household that didn't report it
	// sees it.
	HiddenForEveryone bool
}

// Reported lists reported recipes for operators: ones waiting for review
// first, then by most households, then by oldest report.
func (s *Service) Reported(ctx context.Context) ([]ReportedRecipe, error) {
	if s.reports == nil {
		return nil, errors.New("catalog: reports are not wired")
	}
	all, err := s.reports.All(ctx)
	if err != nil {
		return nil, err
	}
	decisions := map[string]string{}
	if s.reviews != nil {
		if decisions, err = s.reviews.Decisions(ctx); err != nil {
			return nil, err
		}
	}
	// A household that reported nothing sees what the reports hid for
	// everyone. No real household has the zero ID.
	everyone := s.hidden(ctx, bson.NilObjectID.Hex())
	byID := map[string]*ReportedRecipe{}
	var order []string
	for _, r := range all {
		it := byID[r.CatalogID]
		if it == nil {
			it = &ReportedRecipe{CatalogID: r.CatalogID, Decision: decisions[r.CatalogID]}
			if e, err := s.store.Get(ctx, r.CatalogID); err == nil {
				it.Name = e.Content.Name
				it.HiddenForEveryone = isHidden(e, everyone)
			}
			byID[r.CatalogID] = it
			order = append(order, r.CatalogID)
		}
		it.Reports = append(it.Reports, r)
	}
	out := make([]ReportedRecipe, 0, len(order))
	for _, id := range order {
		it := byID[id]
		households := map[string]bool{}
		for _, r := range it.Reports {
			households[r.HouseholdID] = true
		}
		it.Households = len(households)
		out = append(out, *it)
	}
	slices.SortStableFunc(out, func(a, b ReportedRecipe) int {
		if c := cmp.Compare(boolRank(a.Decision != ""), boolRank(b.Decision != "")); c != 0 {
			return c
		}
		return cmp.Compare(b.Households, a.Households)
	})
	return out, nil
}

// reportSummary is a reported recipe's name and how many households
// reported it, for the operator's email. Failed reads give what they can.
func (s *Service) reportSummary(ctx context.Context, id string) (string, int) {
	name := ""
	if e, err := s.store.Get(ctx, id); err == nil {
		name = e.Content.Name
	}
	n := 0
	if s.reports != nil {
		if reporters, err := s.reports.Reporters(ctx); err == nil {
			n = len(reporters[id])
		}
	}
	return name, n
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- MongoDB --------------------------------------------------------------------

// Collections for moderation.
const (
	BlocksCollection  = "catalog_blocks"
	ReviewsCollection = "catalog_reviews"
)

// ModerationIndexes returns the indexes MongoBlocks and MongoReviews rely on.
func ModerationIndexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{
		{Collection: BlocksCollection, Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "catalogId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_catalogId_unique"),
		}}},
		{Collection: ReviewsCollection, Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "catalogId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("catalogId_unique"),
		}}},
	}
}

// MongoBlocks is the MongoDB BlockStore.
type MongoBlocks struct{ coll *mongo.Collection }

var _ BlockStore = (*MongoBlocks)(nil)

// NewMongoBlocks returns a store using db.
func NewMongoBlocks(db *mongo.Database) *MongoBlocks {
	return &MongoBlocks{coll: db.Collection(BlocksCollection)}
}

type blockDoc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	UserID      string        `bson:"userId,omitempty"`
	CatalogID   bson.ObjectID `bson:"catalogId"`
	Publisher   string        `bson:"publisher,omitempty"`
	At          time.Time     `bson:"at"`
}

// Add implements BlockStore.
func (s *MongoBlocks) Add(ctx context.Context, b Block) error {
	hid, err := mongodb.ParseID(b.HouseholdID)
	if err != nil {
		return fmt.Errorf("catalog: bad household id")
	}
	cid, err := mongodb.ParseID(b.CatalogID)
	if err != nil {
		return ErrNotFound
	}
	_, err = s.coll.InsertOne(ctx, blockDoc{HouseholdID: hid, UserID: b.UserID, CatalogID: cid, Publisher: b.Publisher, At: b.At})
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("add catalog block: %w", err)
	}
	return nil
}

// Blocks implements BlockStore.
func (s *MongoBlocks) Blocks(ctx context.Context, householdID string) ([]Block, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	cur, err := s.coll.Find(ctx, bson.D{{Key: "householdId", Value: hid}})
	if err != nil {
		return nil, fmt.Errorf("list catalog blocks: %w", err)
	}
	var docs []blockDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode catalog blocks: %w", err)
	}
	out := make([]Block, 0, len(docs))
	for _, d := range docs {
		out = append(out, Block{
			HouseholdID: d.HouseholdID.Hex(), UserID: d.UserID, CatalogID: d.CatalogID.Hex(), Publisher: d.Publisher, At: d.At.UTC(),
		})
	}
	return out, nil
}

// PurgeHousehold implements account.HouseholdPurger: a deleted household's
// blocks go with it.
func (s *MongoBlocks) PurgeHousehold(ctx context.Context, householdID string) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.DeleteMany(ctx, bson.D{{Key: "householdId", Value: hid}})
	return err
}

// MongoReviews is the MongoDB ReviewStore.
type MongoReviews struct{ coll *mongo.Collection }

var _ ReviewStore = (*MongoReviews)(nil)

// NewMongoReviews returns a store using db.
func NewMongoReviews(db *mongo.Database) *MongoReviews {
	return &MongoReviews{coll: db.Collection(ReviewsCollection)}
}

type reviewDoc struct {
	CatalogID bson.ObjectID `bson:"catalogId"`
	Decision  string        `bson:"decision"`
	UserID    string        `bson:"userId,omitempty"`
	At        time.Time     `bson:"at"`
}

// Decide implements ReviewStore.
func (s *MongoReviews) Decide(ctx context.Context, r Review) error {
	cid, err := mongodb.ParseID(r.CatalogID)
	if err != nil {
		return ErrNotFound
	}
	_, err = s.coll.ReplaceOne(ctx, bson.D{{Key: "catalogId", Value: cid}},
		reviewDoc{CatalogID: cid, Decision: r.Decision, UserID: r.UserID, At: r.At}, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("save catalog review: %w", err)
	}
	return nil
}

// Decisions implements ReviewStore.
func (s *MongoReviews) Decisions(ctx context.Context) (map[string]string, error) {
	cur, err := s.coll.Find(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("list catalog reviews: %w", err)
	}
	var docs []reviewDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode catalog reviews: %w", err)
	}
	out := make(map[string]string, len(docs))
	for _, d := range docs {
		out[d.CatalogID.Hex()] = d.Decision
	}
	return out, nil
}
