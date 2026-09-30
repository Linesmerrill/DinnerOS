package recipes

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// The findings log: when a member opens a recipe's cooking instructions, the
// server runs CheckSteps on what it rendered and records anything that reads
// wrong, so a phrasing the step reader doesn't know yet (a new meal kit, a
// blog, a typed-in family recipe) shows up to be fixed instead of confusing
// someone at the stove. It never changes what the member sees.
//
// Review it with:
//
//	db.instruction_findings.find().sort({lastSeen: -1})
//
// Each fix adds the phrasing to testdata/step_corpus.json so it stays fixed.

// FindingRecorder records CheckSteps findings for a recipe.
// *MongoStore implements it.
type FindingRecorder interface {
	RecordFindings(ctx context.Context, householdID string, r Recipe, servings int, findings []Finding, at time.Time) error
}

// RecordFindings implements FindingRecorder: one document per finding,
// counted up when it recurs. The step's text is kept so a finding can be read
// without the recipe; it is the household's own data and is deleted with it.
func (s *MongoStore) RecordFindings(ctx context.Context, householdID string, r Recipe, servings int, findings []Finding, at time.Time) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return fmt.Errorf("findings: household id: %w", err)
	}
	for _, f := range findings {
		stepText := ""
		if f.Step > 0 && f.Step <= len(r.Steps) {
			stepText = CleanStepText(r.Steps[f.Step-1].Text)
			if len(stepText) > 1000 {
				stepText = stepText[:1000]
			}
		}
		detail := f.Detail
		if len(detail) > 200 {
			detail = detail[:200]
		}
		_, err := s.findings.UpdateOne(ctx,
			bson.D{
				{Key: "householdId", Value: hid}, {Key: "recipeId", Value: r.ID}, {Key: "code", Value: f.Code},
				{Key: "step", Value: f.Step}, {Key: "detail", Value: detail},
			},
			bson.D{
				{Key: "$inc", Value: bson.D{{Key: "count", Value: 1}}},
				{Key: "$set", Value: bson.D{
					{Key: "lastSeen", Value: at}, {Key: "servings", Value: servings}, {Key: "recipeName", Value: r.Name},
					{Key: "source", Value: r.Source}, {Key: "stepText", Value: stepText},
				}},
				{Key: "$setOnInsert", Value: bson.D{{Key: "firstSeen", Value: at}}},
			},
			options.UpdateOne().SetUpsert(true))
		if err != nil {
			return fmt.Errorf("findings: record: %w", err)
		}
	}
	return nil
}

// findingLog checks rendered instructions and records what it finds, at most
// once an hour per recipe and size, off the request's path.
type findingLog struct {
	recorder FindingRecorder
	logger   *slog.Logger
	now      func() time.Time
	mu       sync.Mutex
	recent   map[string]time.Time
}

func newFindingLog(recorder FindingRecorder, logger *slog.Logger) *findingLog {
	if recorder == nil {
		return nil
	}
	return &findingLog{recorder: recorder, logger: logger, now: time.Now, recent: map[string]time.Time{}}
}

const findingLogEvery = time.Hour

// check runs CheckSteps and records findings in the background. It returns
// the findings for tests.
func (l *findingLog) check(ctx context.Context, householdID string, r Recipe, in Instructions) []Finding {
	if l == nil {
		return nil
	}
	key := fmt.Sprintf("%s/%s/%d", householdID, r.ID, in.Servings)
	now := l.now()
	l.mu.Lock()
	if last, ok := l.recent[key]; ok && now.Sub(last) < findingLogEvery {
		l.mu.Unlock()
		return nil
	}
	l.recent[key] = now
	if len(l.recent) > 10000 {
		l.recent = map[string]time.Time{key: now}
	}
	l.mu.Unlock()
	findings := CheckSteps(in)
	if len(findings) == 0 {
		return nil
	}
	l.logger.InfoContext(ctx, "instruction findings", "recipe", r.ID, "servings", in.Servings, "count", len(findings))
	go func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := l.recorder.RecordFindings(bg, householdID, r, in.Servings, findings, now); err != nil {
			l.logger.WarnContext(bg, "record instruction findings", "error", err)
		}
	}()
	return findings
}
