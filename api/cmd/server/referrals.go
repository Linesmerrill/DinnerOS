package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
)

// historyBackfill names the one-off copy of current memberships into the
// referral identity history (decision 698).
const historyBackfill = "referral-identity-history-v1"

// newDeviceChecker is Apple DeviceCheck with the configured key, or nil
// without one: then no device can be checked and, in production, no
// referral can qualify.
func newDeviceChecker(cfg config.Config, logger *slog.Logger) plus.DeviceChecker {
	if !cfg.DeviceCheck.Enabled() {
		logger.Warn("no DeviceCheck key (DEVICECHECK_* or APNS_*); referrals stay pending outside development")
		return nil
	}
	return plus.NewDeviceCheckClient(plus.DeviceCheckOptions{
		KeyID: cfg.DeviceCheck.KeyID, TeamID: cfg.DeviceCheck.TeamID, PrivateKey: cfg.DeviceCheck.PrivateKey,
	})
}

// migrateReferrals marks referrals from before decision 698 qualified (they
// were paid when the code was entered), records every code from before
// decision 699 as an active code key, and in the background copies every
// current membership into the identity history, once. All are idempotent;
// a failure is logged and tried again at the next start.
func migrateReferrals(ctx context.Context, db *mongodb.Client, s *plus.Service, logger *slog.Logger) {
	refs := plus.NewMongoReferrals(db.Database())
	if n, err := refs.MigrateReferrals(ctx); err != nil {
		logger.Error("referral migration failed", "error", err)
	} else if n > 0 {
		logger.Info("referrals marked qualified", "count", n)
	}
	if n, err := refs.MigrateCodes(ctx); err != nil {
		logger.Error("referral code migration failed", "error", err)
	} else if n > 0 {
		logger.Info("referral codes recorded", "count", n)
	}
	migrations := plus.NewMigrations(db.Database())
	if done, err := migrations.Done(ctx, historyBackfill); err != nil || done {
		return
	}
	go func() {
		store := households.NewMongoStore(db.Database())
		members := func(yield func(householdID, userID string, at time.Time) bool) error {
			after := ""
			for {
				ids, err := store.ListHouseholdIDs(ctx, after, 200)
				if err != nil || len(ids) == 0 {
					return err
				}
				for _, id := range ids {
					list, err := store.ListMembershipsByHousehold(ctx, id)
					if err != nil {
						return err
					}
					for _, m := range list {
						if !yield(m.HouseholdID, m.UserID, m.CreatedAt) {
							return ctx.Err()
						}
					}
				}
				after = ids[len(ids)-1]
			}
		}
		n, err := s.BackfillHistory(ctx, members)
		if err != nil {
			logger.Error("referral history backfill failed", "error", err, "memberships", n)
			return
		}
		if err := migrations.MarkDone(ctx, historyBackfill, time.Now()); err != nil {
			logger.Warn("record referral history backfill", "error", err)
		}
		logger.Info("referral history backfilled", "memberships", n)
	}()
}
