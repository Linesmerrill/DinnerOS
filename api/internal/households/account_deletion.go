package households

import (
	"context"
	"errors"
	"fmt"
)

// Departure is how deleting a user's account ends one of their memberships.
type Departure struct {
	HouseholdID string
	// LastMember means nobody else belongs to the household, so the household
	// and everything stored for it are deleted with the account.
	LastMember bool
}

// AccountDepartures lists the user's memberships and whether each household
// would be left empty.
func (s *Service) AccountDepartures(ctx context.Context, userID string) ([]Departure, error) {
	memberships, err := s.store.ListMembershipsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	out := make([]Departure, 0, len(memberships))
	for _, m := range memberships {
		all, err := s.store.ListMembershipsByHousehold(ctx, m.HouseholdID)
		if err != nil {
			return nil, fmt.Errorf("list household memberships: %w", err)
		}
		out = append(out, Departure{HouseholdID: m.HouseholdID, LastMember: len(all) <= 1})
	}
	return out, nil
}

// DeleteEmptiedHousehold removes the user's membership and the household
// itself, after the caller has purged the household's data. It refuses with
// ErrConflict if someone else has joined since AccountDepartures. A household
// or membership that is already gone is not an error, so a retry is safe.
func (s *Service) DeleteEmptiedHousehold(ctx context.Context, householdID, userID string) error {
	all, err := s.store.ListMembershipsByHousehold(ctx, householdID)
	if err != nil {
		return fmt.Errorf("list household memberships: %w", err)
	}
	for _, m := range all {
		if m.UserID != userID {
			return ErrConflict
		}
		if err := s.store.DeleteMembership(ctx, householdID, userID, m.Role); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("delete membership: %w", err)
		}
	}
	if err := s.store.DeleteHousehold(ctx, householdID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete household: %w", err)
	}
	s.logger.InfoContext(ctx, "household deleted with account", "householdId", householdID, "userId", userID)
	return nil
}

// LeaveForAccountDeletion removes the user from a household other members
// still share. Unlike leaving, it never refuses: when the user is the only
// admin, the longest-standing other member is made an admin first, so the
// household keeps one and the account can always be deleted. It returns the
// promoted user's ID, or "" when nobody was promoted. A membership that is
// already gone is not an error.
func (s *Service) LeaveForAccountDeletion(ctx context.Context, householdID, userID string) (promoted string, err error) {
	m, err := s.store.GetMembership(ctx, householdID, userID)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get membership: %w", err)
	}

	if m.Role == RoleAdmin {
		err := s.store.DecrementAdminCount(ctx, householdID)
		if errors.Is(err, ErrLastAdmin) {
			if promoted, err = s.promoteSuccessor(ctx, householdID, userID); err != nil {
				return "", err
			}
			err = s.store.DecrementAdminCount(ctx, householdID)
		}
		if err != nil {
			return promoted, fmt.Errorf("decrement admin count: %w", err)
		}
	}
	if err := s.store.DeleteMembership(ctx, householdID, userID, m.Role); err != nil {
		if m.Role == RoleAdmin {
			s.restoreAdminCount(ctx, householdID)
		}
		if errors.Is(err, ErrNotFound) {
			return promoted, ErrConflict
		}
		return promoted, fmt.Errorf("delete membership: %w", err)
	}
	s.logger.InfoContext(ctx, "member left with account deletion",
		"householdId", householdID, "userId", userID, "promotedUserId", promoted)
	return promoted, nil
}

// promoteSuccessor makes the longest-standing other member an admin.
func (s *Service) promoteSuccessor(ctx context.Context, householdID, leavingUserID string) (string, error) {
	all, err := s.store.ListMembershipsByHousehold(ctx, householdID) // oldest first
	if err != nil {
		return "", fmt.Errorf("list household memberships: %w", err)
	}
	for _, m := range all {
		if m.UserID == leavingUserID || m.Role == RoleAdmin {
			continue
		}
		if _, err := s.store.UpdateMembershipRole(ctx, householdID, m.UserID, m.Role, RoleAdmin, s.now().UTC()); err != nil {
			if errors.Is(err, ErrNotFound) {
				return "", ErrConflict
			}
			return "", fmt.Errorf("promote successor: %w", err)
		}
		if err := s.store.IncrementAdminCount(ctx, householdID); err != nil {
			return "", fmt.Errorf("increment admin count: %w", err)
		}
		return m.UserID, nil
	}
	return "", ErrLastAdmin
}
