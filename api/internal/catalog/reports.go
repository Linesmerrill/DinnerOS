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
// operator looks at it. Apple asks this of apps where people share content.

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
	// HiddenFor returns the catalog IDs the household reported.
	HiddenFor(ctx context.Context, householdID string) (map[string]bool, error)
	// Pulled returns the catalog IDs at least n households reported.
	Pulled(ctx context.Context, n int) (map[string]bool, error)
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

// hidden is the set of catalog IDs not shown to the household: the ones it
// reported, and the ones enough households reported. A failed read hides
// nothing extra rather than failing the page.
func (s *Service) hidden(ctx context.Context, householdID string) map[string]bool {
	out := map[string]bool{}
	if s.reports == nil {
		return out
	}
	if mine, err := s.reports.HiddenFor(ctx, householdID); err == nil {
		for id := range mine {
			out[id] = true
		}
	}
	if pulled, err := s.reports.Pulled(ctx, PullAfter); err == nil {
		for id := range pulled {
			out[id] = true
		}
	}
	return out
}

func withoutHidden(entries []Recipe, hidden map[string]bool) []Recipe {
	if len(hidden) == 0 {
		return entries
	}
	out := entries[:0:0]
	for _, e := range entries {
		if !hidden[e.ID] {
			out = append(out, e)
		}
	}
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

// HiddenFor implements ReportStore.
func (s *MongoReports) HiddenFor(ctx context.Context, householdID string) (map[string]bool, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return map[string]bool{}, nil
	}
	cur, err := s.coll.Find(ctx, bson.D{{Key: "householdId", Value: hid}}, options.Find().SetProjection(bson.D{{Key: "catalogId", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list catalog reports: %w", err)
	}
	var docs []reportDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode catalog reports: %w", err)
	}
	out := make(map[string]bool, len(docs))
	for _, d := range docs {
		out[d.CatalogID.Hex()] = true
	}
	return out, nil
}

// Pulled implements ReportStore.
func (s *MongoReports) Pulled(ctx context.Context, n int) (map[string]bool, error) {
	cur, err := s.coll.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$catalogId"}, {Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}}}}},
		{{Key: "$match", Value: bson.D{{Key: "n", Value: bson.D{{Key: "$gte", Value: n}}}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("count catalog reports: %w", err)
	}
	var rows []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode catalog report counts: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.ID.Hex()] = true
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
