// Package account deletes a user's account and the data that goes with it
// (DELETE /api/v1/me). It owns no collection: every module that stores
// household- or user-owned documents deletes its own through the small purge
// interfaces declared here, and cmd/server wires them all in.
//
// Order matters, because MongoDB gives no transaction across modules here.
// Households are handled first, the user's own records last, so a failure
// part-way leaves an account that can still sign in and be deleted again;
// every step is idempotent.
package account

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// HouseholdPurger deletes everything a module stores for one household.
type HouseholdPurger interface {
	PurgeHousehold(ctx context.Context, householdID string) error
}

// UserPurger deletes (or de-identifies, for household history) everything a
// module stores for one user.
type UserPurger interface {
	PurgeUser(ctx context.Context, userID string) error
}

// Households is what account deletion needs from the households module.
type Households interface {
	AccountDepartures(ctx context.Context, userID string) ([]households.Departure, error)
	DeleteEmptiedHousehold(ctx context.Context, householdID, userID string) error
	LeaveForAccountDeletion(ctx context.Context, householdID, userID string) (promoted string, err error)
}

// Options configures a Service.
type Options struct {
	Households Households
	// HouseholdData are purged for each household the user was the last member
	// of, before the household itself is deleted.
	HouseholdData []HouseholdPurger
	// UserData are purged in order after the households. The users store, which
	// deletes the user record, must come last.
	UserData []UserPurger
	Logger   *slog.Logger
}

// Service deletes accounts.
type Service struct {
	opts   Options
	logger *slog.Logger
}

// NewService returns a Service.
func NewService(opts Options) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{opts: opts, logger: logger}
}

// Result reports what deleting an account did to its households.
type Result struct {
	// DeletedHouseholds were left empty and deleted with their data.
	DeletedHouseholds []string
	// LeftHouseholds still have other members.
	LeftHouseholds []string
	// PromotedUsers maps a left household to the member made admin in the
	// user's place, when the user was its only admin.
	PromotedUsers map[string]string
}

// Delete deletes the user's account.
//
//   - A household the user was the last member of is deleted with everything
//     stored for it.
//   - A household others still share is left; if the user was its only admin,
//     the longest-standing other member becomes admin. Its shared data (plans,
//     pantry, recipes) stays. The user's own ratings are deleted, and their ID
//     is removed from the household's events, cook usage, and notification
//     read receipts.
//   - Sessions, device tokens, sign-in identities, and the user are deleted.
func (s *Service) Delete(ctx context.Context, userID string) (Result, error) {
	res := Result{PromotedUsers: map[string]string{}}
	departures, err := s.opts.Households.AccountDepartures(ctx, userID)
	if err != nil {
		return res, err
	}
	for _, d := range departures {
		if d.LastMember {
			if err := s.deleteHousehold(ctx, d.HouseholdID, userID); err != nil {
				return res, err
			}
			res.DeletedHouseholds = append(res.DeletedHouseholds, d.HouseholdID)
			continue
		}
		promoted, err := s.opts.Households.LeaveForAccountDeletion(ctx, d.HouseholdID, userID)
		if err != nil {
			return res, fmt.Errorf("leave household %s: %w", d.HouseholdID, err)
		}
		res.LeftHouseholds = append(res.LeftHouseholds, d.HouseholdID)
		if promoted != "" {
			res.PromotedUsers[d.HouseholdID] = promoted
		}
	}
	for _, p := range s.opts.UserData {
		if err := p.PurgeUser(ctx, userID); err != nil {
			return res, fmt.Errorf("purge user data: %w", err)
		}
	}
	s.logger.InfoContext(ctx, "account deleted", "userId", userID,
		"deletedHouseholds", len(res.DeletedHouseholds), "leftHouseholds", len(res.LeftHouseholds),
		"promotedAdmins", len(res.PromotedUsers))
	return res, nil
}

func (s *Service) deleteHousehold(ctx context.Context, householdID, userID string) error {
	for _, p := range s.opts.HouseholdData {
		if err := p.PurgeHousehold(ctx, householdID); err != nil {
			return fmt.Errorf("purge household %s: %w", householdID, err)
		}
	}
	if err := s.opts.Households.DeleteEmptiedHousehold(ctx, householdID, userID); err != nil {
		if errors.Is(err, households.ErrConflict) {
			// Someone joined between the check and the purge. Their household
			// is already empty of data; leaving is all that's left to do.
			if _, err := s.opts.Households.LeaveForAccountDeletion(ctx, householdID, userID); err != nil {
				return fmt.Errorf("leave household %s: %w", householdID, err)
			}
			return nil
		}
		return fmt.Errorf("delete household %s: %w", householdID, err)
	}
	return nil
}
