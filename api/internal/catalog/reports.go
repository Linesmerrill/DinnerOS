package catalog

import (
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

// Reports (decision 686): anyone in a household can report a recipe in
// Shared Recipes. The household never sees it again, and once PullAfter
// different households report one it is hidden for everyone until an
// operator looks at it. A report counts against every copy of the recipe
// (decision 689). Apple asks this of apps where people share content.

// ReportReasons are the reasons a member can pick.
var ReportReasons = []string{"inappropriate", "copyright", "spam", "wrong", "other"}

// PullAfter is how many households must report a recipe before it is hidden
// for everyone.
const PullAfter = 3

// MaxReportNote is the longest note kept with a report.
const MaxReportNote = 500

// ErrInvalidReport means the reason is not one of ReportReasons.
var ErrInvalidReport = errors.New("catalog: invalid report")

// Report is one household's report of one catalog recipe.
type Report struct {
	CatalogID   string
	HouseholdID string
	UserID      string
	Reason      string
	Note        string
	At          time.Time
}

// ReportStore keeps reports.
type ReportStore interface {
	// Add stores r; a household reporting the same recipe twice keeps its
	// first report and created is false.
	Add(ctx context.Context, r Report) (created bool, err error)
	// Reporters maps each reported catalog ID to the households that
	// reported it.
	Reporters(ctx context.Context) (map[string][]string, error)
}

// WithReports turns on reporting and returns s.
func (s *Service) WithReports(r ReportStore) *Service {
	s.reports = r
	return s
}

// Report records a household's report and hides the recipe from it.
func (s *Service) Report(ctx context.Context, r Report) (bool, error) {
	if s.reports == nil {
		return false, errors.New("catalog: reports are not wired")
	}
	r.Reason = strings.ToLower(strings.TrimSpace(r.Reason))
	if !slices.Contains(ReportReasons, r.Reason) {
		return false, fmt.Errorf("%w: pick a reason", ErrInvalidReport)
	}
	r.Note = strings.TrimSpace(r.Note)
	if len([]rune(r.Note)) > MaxReportNote {
		r.Note = string([]rune(r.Note)[:MaxReportNote])
	}
	if _, err := s.store.Get(ctx, r.CatalogID); err != nil {
		return false, err
	}
	r.At = s.now().UTC()
	return s.reports.Add(ctx, r)
}

// hidden is the set of groups (groupKey) not shown to the household. A report
// is kept against the entry the member saw, but it counts against the whole
// group that entry belongs to (decision 689): the household's own report
// hides every copy of the recipe from it, and PullAfter different households
// reporting any copies hides the recipe for everyone. A failed read hides
// nothing extra rather than failing the page.
//
// Each reported entry's own ID group is hidden alongside its fingerprint
// group, so an entry is still hidden if its group cannot be read.
func (s *Service) hidden(ctx context.Context, householdID string) map[string]bool {
	out := map[string]bool{}
	if s.reports == nil {
		return out
	}
	reporters, err := s.reports.Reporters(ctx)
	if err != nil || len(reporters) == 0 {
		return out
	}
	ids := make([]string, 0, len(reporters))
	for id := range reporters {
		ids = append(ids, id)
	}
	groups, err := s.store.Groups(ctx, ids)
	if err != nil {
		groups = map[string]string{}
	}
	byGroup := map[string]map[string]bool{}
	for id, households := range reporters {
		for _, g := range []string{idGroup(id), groups[id]} {
			if g == "" {
				continue
			}
			if byGroup[g] == nil {
				byGroup[g] = map[string]bool{}
			}
			for _, hh := range households {
				byGroup[g][hh] = true
			}
		}
	}
	for g, households := range byGroup {
		if households[householdID] || len(households) >= PullAfter {
			out[g] = true
		}
	}
	return out
}

// isHidden reports whether e's group, or e itself, is hidden.
func isHidden(e Recipe, hidden map[string]bool) bool {
	return hidden[groupKey(e)] || hidden[idGroup(e.ID)]
}

func withoutHidden(entries []Recipe, hidden map[string]bool) []Recipe {
	if len(hidden) == 0 {
		return entries
	}
	out := entries[:0:0]
	for _, e := range entries {
		if !isHidden(e, hidden) {
			out = append(out, e)
		}
	}
	return out
}

func hiddenList(hidden map[string]bool) []string {
	out := make([]string, 0, len(hidden))
	for g := range hidden {
		out = append(out, g)
	}
	slices.Sort(out)
	return out
}

// ReportsCollection holds reports.
const ReportsCollection = "catalog_reports"

// ReportIndexes returns the indexes MongoReports relies on.
func ReportIndexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{Collection: ReportsCollection, Indexes: []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "catalogId", Value: 1}, {Key: "householdId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("catalogId_householdId_unique"),
		},
		{Keys: bson.D{{Key: "householdId", Value: 1}}, Options: options.Index().SetName("householdId")},
	}}}
}

// MongoReports is the MongoDB ReportStore.
type MongoReports struct{ coll *mongo.Collection }

var _ ReportStore = (*MongoReports)(nil)

// NewMongoReports returns a store using db.
func NewMongoReports(db *mongo.Database) *MongoReports {
	return &MongoReports{coll: db.Collection(ReportsCollection)}
}

type reportDoc struct {
	CatalogID   bson.ObjectID `bson:"catalogId"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	UserID      string        `bson:"userId,omitempty"`
	Reason      string        `bson:"reason"`
	Note        string        `bson:"note,omitempty"`
	At          time.Time     `bson:"at"`
}

// Add implements ReportStore.
func (s *MongoReports) Add(ctx context.Context, r Report) (bool, error) {
	cid, err := mongodb.ParseID(r.CatalogID)
	if err != nil {
		return false, ErrNotFound
	}
	hid, err := mongodb.ParseID(r.HouseholdID)
	if err != nil {
		return false, fmt.Errorf("catalog: bad household id")
	}
	_, err = s.coll.InsertOne(ctx, reportDoc{CatalogID: cid, HouseholdID: hid, UserID: r.UserID, Reason: r.Reason, Note: r.Note, At: r.At})
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("add catalog report: %w", err)
	}
	return true, nil
}

// Reporters implements ReportStore.
func (s *MongoReports) Reporters(ctx context.Context) (map[string][]string, error) {
	cur, err := s.coll.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$catalogId"},
			{Key: "households", Value: bson.D{{Key: "$addToSet", Value: "$householdId"}}},
		}}},
	})
	if err != nil {
		return nil, fmt.Errorf("list catalog reports: %w", err)
	}
	var rows []struct {
		ID         bson.ObjectID   `bson:"_id"`
		Households []bson.ObjectID `bson:"households"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode catalog reports: %w", err)
	}
	out := make(map[string][]string, len(rows))
	for _, r := range rows {
		households := make([]string, 0, len(r.Households))
		for _, hh := range r.Households {
			households = append(households, hh.Hex())
		}
		slices.Sort(households)
		out[r.ID.Hex()] = households
	}
	return out, nil
}

// PurgeHousehold implements account.HouseholdPurger: a deleted household's
// reports go too, so recipes it alone hid come back for no one else.
func (s *MongoReports) PurgeHousehold(ctx context.Context, householdID string) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.DeleteMany(ctx, bson.D{{Key: "householdId", Value: hid}})
	return err
}
