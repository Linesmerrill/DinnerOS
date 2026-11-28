// Package migrations runs DinnerOS's one-time data changes. Each is recorded
// in the schema_migrations collection under its id, so it runs once per
// database however often the API starts (README.md).
package migrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Collection records each migration: its status ("running", "done" or
// "failed"), when it started and finished, and what it did.
const Collection = "schema_migrations"

// staleAfter is how long a "running" record waits before another start takes
// it over (the dyno that held it stopped).
const staleAfter = 15 * time.Minute

// Migration is one data change. Run must be safe to repeat after a failure.
type Migration struct {
	ID string
	// NotReady says why this database can't run it yet; it is then skipped
	// and not recorded, so it runs on a later start.
	NotReady string
	Run      func(ctx context.Context, db *mongo.Database, rec *Record, log *slog.Logger) (summary string, err error)
}

// Record is a migration's row in schema_migrations.
type Record struct {
	coll *mongo.Collection
	id   string
	// Doc is the row as it was when this run claimed it.
	Doc bson.M
}

// Set stores a field on the row, so a run after a failure can see it.
func (r *Record) Set(ctx context.Context, key string, value any) error {
	_, err := r.coll.UpdateByID(ctx, r.id, bson.D{{Key: "$set", Value: bson.D{{Key: key, Value: value}}}})
	if err == nil {
		r.Doc[key] = value
	}
	return err
}

// Run runs each migration not yet done on db, in order, and logs one line
// for each. It stops at the first failure, which is recorded so the next
// start tries again.
func Run(ctx context.Context, db *mongo.Database, list []Migration, log *slog.Logger, now func() time.Time) error {
	coll := db.Collection(Collection)
	for _, m := range list {
		if m.NotReady != "" {
			log.Warn(fmt.Sprintf("migration %s skipped: %s", m.ID, m.NotReady))
			continue
		}
		rec, why, err := claim(ctx, coll, m.ID, now())
		if err != nil {
			return fmt.Errorf("migration %s: %w", m.ID, err)
		}
		if rec == nil {
			log.Info(fmt.Sprintf("migration %s not run: %s", m.ID, why))
			continue
		}
		summary, err := m.Run(ctx, db, rec, log)
		finish := bson.D{{Key: "finishedAt", Value: now()}}
		if err != nil {
			finish = append(finish, bson.E{Key: "status", Value: "failed"}, bson.E{Key: "error", Value: err.Error()})
			_, _ = coll.UpdateByID(context.WithoutCancel(ctx), m.ID, bson.D{{Key: "$set", Value: finish}})
			log.Error(fmt.Sprintf("migration %s failed, will retry on the next start: %v", m.ID, err))
			return fmt.Errorf("migration %s: %w", m.ID, err)
		}
		finish = append(finish, bson.E{Key: "status", Value: "done"}, bson.E{Key: "summary", Value: summary})
		if _, err := coll.UpdateByID(context.WithoutCancel(ctx), m.ID, bson.D{{Key: "$set", Value: finish}}); err != nil {
			return fmt.Errorf("migration %s: recording done: %w", m.ID, err)
		}
		log.Info(fmt.Sprintf("migration %s done: %s", m.ID, summary))
	}
	return nil
}

// claim takes the migration's row for this run: a new row, a failed one, or
// one left running by a start that stopped. It returns nil and why when the
// migration is done or another start is running it.
func claim(ctx context.Context, coll *mongo.Collection, id string, now time.Time) (*Record, string, error) {
	_, err := coll.InsertOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "status", Value: "running"}, {Key: "startedAt", Value: now}})
	if err == nil {
		return &Record{coll: coll, id: id, Doc: bson.M{"_id": id, "status": "running"}}, "", nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return nil, "", err
	}
	var doc bson.M
	err = coll.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: id}, {Key: "$or", Value: bson.A{
			bson.D{{Key: "status", Value: "failed"}},
			bson.D{{Key: "status", Value: "running"}, {Key: "startedAt", Value: bson.D{{Key: "$lt", Value: now.Add(-staleAfter)}}}},
		}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "status", Value: "running"}, {Key: "startedAt", Value: now}}}},
	).Decode(&doc)
	if err == nil {
		doc["status"] = "running"
		return &Record{coll: coll, id: id, Doc: doc}, "", nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, "", err
	}
	if err := coll.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&doc); err != nil {
		return nil, "", err
	}
	if doc["status"] == "done" {
		finished := fmt.Sprint(doc["finishedAt"])
		if t, ok := doc["finishedAt"].(bson.DateTime); ok {
			finished = t.Time().UTC().Format(time.RFC3339)
		}
		return nil, "already done (finished " + finished + ")", nil
	}
	return nil, "another start is running it", nil
}
