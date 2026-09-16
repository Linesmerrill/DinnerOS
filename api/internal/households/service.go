package households

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// UserDirectory is what households needs from the users module.
type UserDirectory interface {
	GetUser(ctx context.Context, id string) (users.User, error)
}

// CreatedListener hears about new households. It must return quickly and
// cannot fail household creation: slow or fallible work (the starter recipe
// library, *recipes.Starter) runs in the background.
type CreatedListener interface {
	HouseholdCreated(ctx context.Context, householdID string)
}

// Service implements household and membership use cases. Methods that act on
// an existing household take the caller's Membership, loaded by Authorize (or
// the RequirePermission middleware), and check its permissions themselves.
type Service struct {
	store     Store
	users     UserDirectory
	onCreated CreatedListener
	now       func() time.Time
	logger    *slog.Logger
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Store  Store
	Users  UserDirectory
	Logger *slog.Logger
	// OnCreated, when set, is told about each household Create makes.
	OnCreated CreatedListener
	// Now is the clock. Default time.Now.
	Now func() time.Time
}

// NewService returns a Service.
func NewService(opts ServiceOptions) *Service {
	s := &Service{store: opts.Store, users: opts.Users, onCreated: opts.OnCreated, now: opts.Now, logger: opts.Logger}
	if s.now == nil {
		s.now = time.Now
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	return s
}

// Create makes a household with userID as its first admin.
func (s *Service) Create(ctx context.Context, userID string, in CreateInput) (Household, Membership, error) {
	name, err := normalizeName(in.Name)
	if err != nil {
		return Household{}, Membership{}, err
	}
	tz, err := normalizeTimeZone(in.TimeZone)
	if err != nil {
		return Household{}, Membership{}, err
	}
	servings := DefaultServings
	if in.DefaultServings != nil {
		servings = *in.DefaultServings
	}
	if err := validateServings(servings); err != nil {
		return Household{}, Membership{}, err
	}

	now := s.now().UTC()
	h, err := s.store.CreateHousehold(ctx, Household{
		Name:            name,
		DefaultServings: servings,
		TimeZone:        tz,
		CreatedBy:       userID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return Household{}, Membership{}, fmt.Errorf("create household: %w", err)
	}
	m, err := s.store.CreateMembership(ctx, Membership{
		HouseholdID: h.ID, UserID: userID, Role: RoleAdmin, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		// Without transactions, remove the household so no admin-less
		// household is left behind.
		if delErr := s.store.DeleteHousehold(ctx, h.ID); delErr != nil {
			s.logger.ErrorContext(ctx, "delete household after failed membership creation", "householdId", h.ID, "error", delErr)
		}
		return Household{}, Membership{}, fmt.Errorf("create admin membership: %w", err)
	}
	s.logger.InfoContext(ctx, "household created", "householdId", h.ID, "userId", userID)
	if s.onCreated != nil {
		s.onCreated.HouseholdCreated(ctx, h.ID)
	}
	return h, m, nil
}

// ListForUser returns every household the user belongs to, oldest membership
// first.
func (s *Service) ListForUser(ctx context.Context, userID string) ([]UserHousehold, error) {
	memberships, err := s.store.ListMembershipsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	if len(memberships) == 0 {
		return []UserHousehold{}, nil
	}
	ids := make([]string, 0, len(memberships))
	for _, m := range memberships {
		ids = append(ids, m.HouseholdID)
	}
	list, err := s.store.ListHouseholds(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list households: %w", err)
	}
	byID := make(map[string]Household, len(list))
	for _, h := range list {
		byID[h.ID] = h
	}
	out := make([]UserHousehold, 0, len(memberships))
	for _, m := range memberships {
		if h, ok := byID[m.HouseholdID]; ok {
			out = append(out, UserHousehold{Household: h, Membership: m})
		}
	}
	return out, nil
}

// Authorize loads userID's membership in the household and checks perm. It
// returns ErrNotFound when the household does not exist or the user is not a
// member (so existence is never revealed), and ErrForbidden when the member's
// role lacks perm.
func (s *Service) Authorize(ctx context.Context, householdID, userID string, perm Permission) (Membership, error) {
	m, err := s.store.GetMembership(ctx, householdID, userID)
	if err != nil {
		return Membership{}, err
	}
	if !m.Role.Can(perm) {
		return m, ErrForbidden
	}
	return m, nil
}

// GetMembership returns the user's membership in the household, or ErrNotFound.
func (s *Service) GetMembership(ctx context.Context, householdID, userID string) (Membership, error) {
	return s.store.GetMembership(ctx, householdID, userID)
}

// GetHousehold returns a household by ID without any authorization. Callers
// must already have authorized access to it.
func (s *Service) GetHousehold(ctx context.Context, id string) (Household, error) {
	return s.store.GetHousehold(ctx, id)
}

// Details returns the actor's household and, when the actor may view members,
// its member list (oldest first). Otherwise members is nil.
func (s *Service) Details(ctx context.Context, actor Membership) (Household, []Member, error) {
	if !actor.Role.Can(PermHouseholdView) {
		return Household{}, nil, ErrForbidden
	}
	h, err := s.store.GetHousehold(ctx, actor.HouseholdID)
	if err != nil {
		return Household{}, nil, err
	}
	if !actor.Role.Can(PermMembersView) {
		return h, nil, nil
	}
	memberships, err := s.store.ListMembershipsByHousehold(ctx, actor.HouseholdID)
	if err != nil {
		return Household{}, nil, fmt.Errorf("list members: %w", err)
	}
	members := make([]Member, 0, len(memberships))
	for _, m := range memberships {
		members = append(members, s.member(ctx, m))
	}
	return h, members, nil
}

// Update changes household settings. It requires household.update.
func (s *Service) Update(ctx context.Context, actor Membership, in UpdateInput) (Household, error) {
	if !actor.Role.Can(PermHouseholdUpdate) {
		return Household{}, ErrForbidden
	}
	var patch HouseholdPatch
	if in.Name == nil && in.TimeZone == nil && in.DefaultServings == nil && in.OrderDay == nil {
		return Household{}, invalid("at least one of name, timeZone, defaultServings, or orderDay is required")
	}
	if in.Name != nil {
		name, err := normalizeName(*in.Name)
		if err != nil {
			return Household{}, err
		}
		patch.Name = &name
	}
	if in.TimeZone != nil {
		tz, err := normalizeTimeZone(*in.TimeZone)
		if err != nil {
			return Household{}, err
		}
		patch.TimeZone = &tz
	}
	if in.DefaultServings != nil {
		if err := validateServings(*in.DefaultServings); err != nil {
			return Household{}, err
		}
		servings := *in.DefaultServings
		patch.DefaultServings = &servings
	}
	if in.OrderDay != nil {
		day, err := normalizeOrderDay(*in.OrderDay)
		if err != nil {
			return Household{}, err
		}
		patch.OrderDay = &day
	}
	h, err := s.store.UpdateHousehold(ctx, actor.HouseholdID, patch, s.now().UTC())
	if err != nil {
		return Household{}, err
	}
	return h, nil
}

// ChangeRole sets targetUserID's role. It requires members.changeRole, and the
// actor's role must cover both the target's current role and the new one.
// Demoting the last admin returns ErrLastAdmin. Setting the current role is a
// no-op.
func (s *Service) ChangeRole(ctx context.Context, actor Membership, targetUserID string, role Role) (Member, error) {
	if !role.Valid() {
		return Member{}, invalid(fmt.Sprintf("role must be one of %v", roleOrder))
	}
	if !actor.Role.Can(PermMembersChangeRole) {
		return Member{}, ErrForbidden
	}
	target, err := s.store.GetMembership(ctx, actor.HouseholdID, targetUserID)
	if err != nil {
		return Member{}, err
	}
	if !actor.Role.Covers(target.Role) || !actor.Role.Covers(role) {
		return Member{}, ErrForbidden
	}
	if target.Role == role {
		return s.member(ctx, target), nil
	}

	demotingAdmin := target.Role == RoleAdmin
	if demotingAdmin {
		if err := s.store.DecrementAdminCount(ctx, actor.HouseholdID); err != nil {
			return Member{}, err
		}
	}
	updated, err := s.store.UpdateMembershipRole(ctx, actor.HouseholdID, targetUserID, target.Role, role, s.now().UTC())
	if err != nil {
		if demotingAdmin {
			s.restoreAdminCount(ctx, actor.HouseholdID)
		}
		if errors.Is(err, ErrNotFound) {
			// The membership changed or disappeared since we read it.
			return Member{}, ErrConflict
		}
		return Member{}, fmt.Errorf("update role: %w", err)
	}
	if role == RoleAdmin {
		if err := s.store.IncrementAdminCount(ctx, actor.HouseholdID); err != nil {
			// The count is now lower than the real number of admins, which
			// only makes the last-admin guard stricter, never looser.
			s.logger.ErrorContext(ctx, "increment admin count after promotion", "householdId", actor.HouseholdID, "error", err)
		}
	}
	s.logger.InfoContext(ctx, "member role changed",
		"householdId", actor.HouseholdID, "actorId", actor.UserID, "userId", targetUserID,
		"from", string(target.Role), "to", string(role))
	return s.member(ctx, updated), nil
}

// RemoveMember removes targetUserID from the household. Members may always
// remove themselves (leave); removing someone else requires members.remove and
// a role that covers theirs. Removing the last admin returns ErrLastAdmin,
// including when that admin is the only member: households cannot be deleted
// or left empty yet.
func (s *Service) RemoveMember(ctx context.Context, actor Membership, targetUserID string) error {
	self := targetUserID == actor.UserID
	if !self && !actor.Role.Can(PermMembersRemove) {
		return ErrForbidden
	}
	target := actor
	if !self {
		var err error
		if target, err = s.store.GetMembership(ctx, actor.HouseholdID, targetUserID); err != nil {
			return err
		}
		if !actor.Role.Covers(target.Role) {
			return ErrForbidden
		}
	}

	removingAdmin := target.Role == RoleAdmin
	if removingAdmin {
		if err := s.store.DecrementAdminCount(ctx, actor.HouseholdID); err != nil {
			return err
		}
	}
	if err := s.store.DeleteMembership(ctx, actor.HouseholdID, targetUserID, target.Role); err != nil {
		if removingAdmin {
			s.restoreAdminCount(ctx, actor.HouseholdID)
		}
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		return fmt.Errorf("delete membership: %w", err)
	}
	s.logger.InfoContext(ctx, "member removed",
		"householdId", actor.HouseholdID, "actorId", actor.UserID, "userId", targetUserID, "self", self)
	return nil
}

// AddMember adds userID to the household with role, for accepted invitations.
// If the user is already a member, their existing membership is returned
// unchanged and created is false.
func (s *Service) AddMember(ctx context.Context, householdID, userID string, role Role) (m Membership, created bool, err error) {
	if !role.Valid() {
		return Membership{}, false, invalid(fmt.Sprintf("role must be one of %v", roleOrder))
	}
	if _, err := s.store.GetHousehold(ctx, householdID); err != nil {
		return Membership{}, false, err
	}
	now := s.now().UTC()
	m, err = s.store.CreateMembership(ctx, Membership{
		HouseholdID: householdID, UserID: userID, Role: role, CreatedAt: now, UpdatedAt: now,
	})
	if errors.Is(err, ErrDuplicate) {
		existing, getErr := s.store.GetMembership(ctx, householdID, userID)
		if getErr != nil {
			return Membership{}, false, fmt.Errorf("get existing membership: %w", getErr)
		}
		return existing, false, nil
	}
	if err != nil {
		return Membership{}, false, fmt.Errorf("create membership: %w", err)
	}
	if role == RoleAdmin {
		if err := s.store.IncrementAdminCount(ctx, householdID); err != nil {
			s.logger.ErrorContext(ctx, "increment admin count after join", "householdId", householdID, "error", err)
		}
	}
	s.logger.InfoContext(ctx, "member added", "householdId", householdID, "userId", userID, "role", string(role))
	return m, true, nil
}

func (s *Service) restoreAdminCount(ctx context.Context, householdID string) {
	if err := s.store.IncrementAdminCount(context.WithoutCancel(ctx), householdID); err != nil {
		s.logger.ErrorContext(ctx, "restore admin count", "householdId", householdID, "error", err)
	}
}

// member resolves a membership's display name. A missing user yields an empty
// name rather than failing the whole list.
func (s *Service) member(ctx context.Context, m Membership) Member {
	out := Member{UserID: m.UserID, Role: m.Role, JoinedAt: m.CreatedAt}
	if s.users == nil {
		return out
	}
	u, err := s.users.GetUser(ctx, m.UserID)
	switch {
	case err == nil:
		out.DisplayName = u.DisplayName
	case !errors.Is(err, users.ErrNotFound):
		s.logger.WarnContext(ctx, "look up member display name", "userId", m.UserID, "error", err)
	}
	return out
}
