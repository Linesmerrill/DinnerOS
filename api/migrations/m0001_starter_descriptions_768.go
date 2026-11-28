package migrations

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/starterdescs"
)

// StarterDescriptionsID is the starter descriptions rewrite's id in
// schema_migrations.
const StarterDescriptionsID = "starter-descriptions-768"

// All is every migration, in the order they run.
func All(starterHouseholdID string, now func() time.Time) []Migration {
	return []Migration{StarterDescriptions(starterHouseholdID, now)}
}

// StarterDescriptions gives every starter its description in DinnerOS's own
// words (decision 768, widening decision 684): starterdescs.Build122 on the
// starters STARTER_RECIPES_HOUSEHOLD_ID owns, their unedited copies with no
// order history, and their Shared Recipes entries. A household's own imports
// and anything someone shared are never touched.
//
// Before changing anything it copies recipes and recipe_catalog into
// <name>_backup_<yyyymmdd>_desc768 in the same database and checks each
// copy's count.
func StarterDescriptions(starterHouseholdID string, now func() time.Time) Migration {
	m := Migration{ID: StarterDescriptionsID}
	starter, err := bson.ObjectIDFromHex(strings.TrimSpace(starterHouseholdID))
	if err != nil {
		m.NotReady = "STARTER_RECIPES_HOUSEHOLD_ID is not set"
		return m
	}
	m.Run = func(ctx context.Context, db *mongo.Database, rec *Record, log *slog.Logger) (string, error) {
		descs, err := starterdescs.Parse(starterdescs.Build122)
		if err != nil {
			return "", fmt.Errorf("descriptions: %w", err)
		}
		var backups [2]backup
		for i, name := range []string{"recipes", catalog.RecipesCollection} {
			b, err := backUp(ctx, db, rec, name, now())
			if err != nil {
				return "", fmt.Errorf("backing up %s: %w", name, err)
			}
			backups[i] = b
		}
		log.Info(fmt.Sprintf("migration %s: backed up %d recipes to %s and %d Shared Recipes entries to %s",
			StarterDescriptionsID, backups[0].count, backups[0].name, backups[1].count, backups[1].name))
		c, err := starterdescs.Apply(ctx, db, starter, descs, true)
		if err != nil {
			return "", err
		}
		if len(c.NoDescription) > 0 {
			log.Warn(fmt.Sprintf("migration %s: starters with no new description: %s",
				StarterDescriptionsID, strings.Join(c.NoDescription, "; ")))
		}
		return fmt.Sprintf("backups %s (%d docs) and %s (%d docs); updated %d starters, %d copies and %d Shared Recipes entries; "+
			"left alone %d imported or shared recipes and %d Shared Recipes entries; %d starters have no new description",
			backups[0].name, backups[0].count, backups[1].name, backups[1].count,
			c.Starters, c.Copies, c.Entries, c.LeftAloneRecipes, c.LeftAloneEntries, len(c.NoDescription)), nil
	}
	return m
}

type backup struct {
	name  string
	count int64
}

// backUp copies collection name into a dated backup collection, once: a
// finished copy is noted on rec, and a run after a failure keeps it rather
// than copying words this migration may already have changed. A copy cut
// short is dropped and made again.
func backUp(ctx context.Context, db *mongo.Database, rec *Record, name string, at time.Time) (backup, error) {
	key := "backup_" + name
	if done, ok := rec.Doc[key].(string); ok && done != "" {
		n, err := db.Collection(done).CountDocuments(ctx, bson.D{})
		return backup{done, n}, err
	}
	b := backup{name: fmt.Sprintf("%s_backup_%s_desc768", name, at.UTC().Format("20060102"))}
	dst := db.Collection(b.name)
	if err := dst.Drop(ctx); err != nil {
		return b, err
	}
	src := db.Collection(name)
	before, err := src.CountDocuments(ctx, bson.D{})
	if err != nil {
		return b, err
	}
	cur, err := src.Find(ctx, bson.D{}, options.Find().SetBatchSize(500))
	if err != nil {
		return b, err
	}
	defer cur.Close(ctx)
	batch := make([]any, 0, 500)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		_, err := dst.InsertMany(ctx, batch)
		b.count += int64(len(batch))
		batch = batch[:0]
		return err
	}
	for cur.Next(ctx) {
		batch = append(batch, bson.Raw(append([]byte(nil), cur.Current...)))
		if len(batch) == cap(batch) {
			if err := flush(); err != nil {
				return b, err
			}
		}
	}
	if err := cur.Err(); err != nil {
		return b, err
	}
	if err := flush(); err != nil {
		return b, err
	}
	stored, err := dst.CountDocuments(ctx, bson.D{})
	if err != nil {
		return b, err
	}
	// Nothing is deleted while this runs, so a short copy means a bad one.
	if stored != b.count || b.count < before {
		return b, fmt.Errorf("%s has %d docs, copied %d of %d", b.name, stored, b.count, before)
	}
	return b, rec.Set(ctx, key, b.name)
}
