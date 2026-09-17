package planning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// A household's first day of the week decides which dates each week key
// covers (Week.DateOn), and an entry stores only its week and weekday. So when
// the first day changes, an entry whose weekday now lands on another date is
// moved to the neighboring week where its weekday is the same date again:
// Monday-first 2026-W38's Sunday is Sep 20, which with Sunday-first weeks is
// Sunday of 2026-W39. Nothing else about the entry changes, and no meal
// changes date (docs/architecture.md, decision 503).

// WeekStartMover moves entries when a household's first day of the week
// changes. *MongoStore implements it.
type WeekStartMover interface {
	// MoveEntriesForWeekStart moves every scheduled entry whose date under
	// from-first weeks falls in another week under to-first weeks into that
	// week, keeping its ID, day, and everything else. Destination plans are
	// created as drafts when needed, and finalized plans and the entry limit
	// don't stop a move. Moved entries are marked, so running it again for the
	// same change after a failure never moves an entry twice. It returns how
	// many entries moved.
	MoveEntriesForWeekStart(ctx context.Context, householdID string, from, to Day, now time.Time) (int, error)
	// ClearWeekStartMarks removes the marks MoveEntriesForWeekStart left, once
	// the household's new first day is saved.
	ClearWeekStartMarks(ctx context.Context, householdID string) error
}

var errNoMover = errors.New("planning: the store can't move entries between weeks")

// ChangeWeekStart moves the household's scheduled entries so they keep their
// dates when weeks start on to instead of from. Call it before saving the
// household's new first day, then FinishWeekStart after. Codes are "sun" to
// "sat"; an empty from means Monday.
func (s *Service) ChangeWeekStart(ctx context.Context, householdID, from, to string) (int, error) {
	if householdID == "" {
		return 0, errHouseholdRequired
	}
	if from == "" {
		from = string(LegacyWeekStart)
	}
	f, err := ParseDay(from)
	if err != nil {
		return 0, fmt.Errorf("planning: week start %q: %w", from, err)
	}
	t, err := ParseDay(to)
	if err != nil {
		return 0, fmt.Errorf("planning: week start %q: %w", to, err)
	}
	if f == t {
		return 0, nil
	}
	mover, ok := s.store.(WeekStartMover)
	if !ok {
		return 0, errNoMover
	}
	return mover.MoveEntriesForWeekStart(ctx, householdID, f, t, s.now().UTC())
}

// FinishWeekStart clears the marks ChangeWeekStart left.
func (s *Service) FinishWeekStart(ctx context.Context, householdID string) error {
	mover, ok := s.store.(WeekStartMover)
	if !ok {
		return errNoMover
	}
	return mover.ClearWeekStartMarks(ctx, householdID)
}

// weekStartMark identifies one change of first day on a moved entry.
func weekStartMark(from, to Day) string { return string(from) + ">" + string(to) }

var _ WeekStartMover = (*MongoStore)(nil)

// MoveEntriesForWeekStart implements WeekStartMover. Each move adds the entry
// to its new week (unless a previous run already did) before pulling it from
// the old one, so a failure part way leaves an entry in both weeks rather than
// in neither, and the next run finishes the job.
func (s *MongoStore) MoveEntriesForWeekStart(ctx context.Context, householdID string, from, to Day, now time.Time) (int, error) {
	moves, err := s.weekStartMoves(ctx, householdID, from, to)
	if err != nil {
		return 0, err
	}
	for i, m := range moves {
		if err := s.moveEntry(ctx, m.householdID, m.sourceID, m.dest, m.entry, weekStartMark(from, to), now); err != nil {
			return i, err
		}
	}
	return len(moves), nil
}

// CountEntriesForWeekStart returns how many entries MoveEntriesForWeekStart
// would move, without moving any.
func (s *MongoStore) CountEntriesForWeekStart(ctx context.Context, householdID string, from, to Day) (int, error) {
	moves, err := s.weekStartMoves(ctx, householdID, from, to)
	return len(moves), err
}

type weekStartMove struct {
	householdID, sourceID bson.ObjectID
	dest                  Week
	entry                 entryDoc
}

// weekStartMoves decides every move from one snapshot of the household's
// plans, so an entry moved into a later plan is never looked at again in the
// same run.
func (s *MongoStore) weekStartMoves(ctx context.Context, householdID string, from, to Day) ([]weekStartMove, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	mark := weekStartMark(from, to)
	cur, err := s.plans.Find(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "entries.day", Value: bson.D{{Key: "$exists", Value: true}}}},
		options.Find().SetSort(bson.D{{Key: "week", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []planDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	var moves []weekStartMove
	for _, d := range docs {
		w, err := ParseWeek(d.Week)
		if err != nil {
			return nil, fmt.Errorf("planning: stored plan %s: %w", d.ID.Hex(), err)
		}
		for _, e := range d.Entries {
			if e.Day == "" || e.WeekStartMark == mark {
				continue
			}
			date, err := time.Parse(time.DateOnly, w.DateOn(from, Day(e.Day)))
			if err != nil {
				continue // an unknown stored day has no date to keep
			}
			if dest := WeekOfOn(date, to); dest != w {
				moves = append(moves, weekStartMove{householdID: hid, sourceID: d.ID, dest: dest, entry: e})
			}
		}
	}
	return moves, nil
}

func (s *MongoStore) moveEntry(ctx context.Context, hid, sourceID bson.ObjectID, dest Week, e entryDoc, mark string, now time.Time) error {
	ensure := bson.D{{Key: "$setOnInsert", Value: append(insertOnly(dest, now),
		bson.E{Key: "status", Value: StatusDraft},
		bson.E{Key: "updatedAt", Value: now},
	)}}
	err := retryDuplicate(func() error {
		_, err := s.plans.UpdateOne(ctx, planFilter(hid, dest), ensure, options.UpdateOne().SetUpsert(true))
		return err
	})
	if err != nil {
		return translate(err)
	}
	e.WeekStartMark = mark
	_, err = s.plans.UpdateOne(ctx,
		append(planFilter(hid, dest), bson.E{Key: "entries.id", Value: bson.D{{Key: "$ne", Value: e.ID}}}),
		bson.D{
			{Key: "$push", Value: bson.D{{Key: "entries", Value: e}}},
			{Key: "$set", Value: bson.D{{Key: "updatedAt", Value: now}}},
		})
	if err != nil {
		return translate(err)
	}
	_, err = s.plans.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: sourceID}},
		bson.D{
			{Key: "$pull", Value: bson.D{{Key: "entries", Value: bson.D{{Key: "id", Value: e.ID}}}}},
			{Key: "$set", Value: bson.D{{Key: "updatedAt", Value: now}}},
		})
	return translate(err)
}

// ClearWeekStartMarks implements WeekStartMover.
func (s *MongoStore) ClearWeekStartMarks(ctx context.Context, householdID string) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return ErrNotFound
	}
	_, err = s.plans.UpdateMany(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "entries.weekStartMark", Value: bson.D{{Key: "$exists", Value: true}}}},
		bson.D{{Key: "$unset", Value: bson.D{{Key: "entries.$[].weekStartMark", Value: ""}}}})
	return translate(err)
}
