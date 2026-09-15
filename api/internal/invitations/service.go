package invitations

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// DefaultAcceptURLBase is prepended to the token to form the emailed link.
const DefaultAcceptURLBase = "dinneros://invite?token="

const (
	maxEmailLength = 254
	// createAttempts bounds retries when a new invitation collides with a
	// concurrent re-invite (or, astronomically rarely, an existing secret).
	createAttempts = 3
	emailTimeout   = 15 * time.Second
)

// Households is what invitations needs from the households module.
// *households.Service implements it.
type Households interface {
	GetHousehold(ctx context.Context, id string) (households.Household, error)
	GetMembership(ctx context.Context, householdID, userID string) (households.Membership, error)
	AddMember(ctx context.Context, householdID, userID string, role households.Role) (households.Membership, bool, error)
}

// UserDirectory is what invitations needs from the users module.
type UserDirectory interface {
	GetUser(ctx context.Context, id string) (users.User, error)
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Store      Store
	Households Households
	Users      UserDirectory
	Email      EmailProvider
	// AcceptURLBase is prepended to the token (default DefaultAcceptURLBase).
	AcceptURLBase string
	Logger        *slog.Logger
	// Now is the clock. Default time.Now.
	Now func() time.Time
	// Random is the entropy source. Default crypto/rand.
	Random io.Reader
}

// Service implements invitation use cases. Household-scoped methods take the
// caller's membership, loaded by households.RequirePermission, and check its
// permissions themselves.
type Service struct {
	store         Store
	households    Households
	users         UserDirectory
	email         EmailProvider
	acceptURLBase string
	logger        *slog.Logger
	now           func() time.Time
	random        io.Reader
}

// NewService returns a Service.
func NewService(opts ServiceOptions) *Service {
	s := &Service{
		store:         opts.Store,
		households:    opts.Households,
		users:         opts.Users,
		email:         opts.Email,
		acceptURLBase: opts.AcceptURLBase,
		logger:        opts.Logger,
		now:           opts.Now,
		random:        opts.Random,
	}
	if s.acceptURLBase == "" {
		s.acceptURLBase = DefaultAcceptURLBase
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.random == nil {
		s.random = rand.Reader
	}
	return s
}

// CreateResult is returned by Create. Code is the display form of the invite
// code; it is returned only here and cannot be retrieved later.
type CreateResult struct {
	Invitation     Invitation
	Code           string
	EmailDelivered bool
}

// Create invites email to the actor's household with role, revoking any
// pending invitation for the same address first. The actor needs
// members.invite and a role that covers the invited role.
//
// If sending the email fails, the invitation is kept and EmailDelivered is
// false: the returned code still works when shared another way.
func (s *Service) Create(ctx context.Context, actor households.Membership, email string, role households.Role) (CreateResult, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return CreateResult{}, err
	}
	if !role.Valid() {
		return CreateResult{}, invalidInput(fmt.Sprintf("role must be one of %v", households.Roles()))
	}
	if !actor.Role.Can(households.PermMembersInvite) || !actor.Role.Covers(role) {
		return CreateResult{}, households.ErrForbidden
	}
	household, err := s.households.GetHousehold(ctx, actor.HouseholdID)
	if err != nil {
		return CreateResult{}, err
	}

	var (
		inv          Invitation
		token, code  string
		now          = s.now().UTC()
		lastErr      error
		createdOnTry bool
	)
	for range createAttempts {
		if token, err = newToken(s.random); err != nil {
			return CreateResult{}, err
		}
		if code, err = newCode(s.random); err != nil {
			return CreateResult{}, err
		}
		if err := s.store.RevokePending(ctx, actor.HouseholdID, email, now); err != nil {
			return CreateResult{}, fmt.Errorf("revoke previous invitations: %w", err)
		}
		inv, lastErr = s.store.Create(ctx, Invitation{
			HouseholdID: actor.HouseholdID,
			Email:       email,
			Role:        role,
			TokenHash:   hashSecret(token),
			CodeHash:    hashSecret(code),
			ExpiresAt:   now.Add(TTL),
			CreatedBy:   actor.UserID,
			CreatedAt:   now,
		})
		if !errors.Is(lastErr, ErrDuplicate) {
			createdOnTry = lastErr == nil
			break
		}
	}
	if !createdOnTry {
		return CreateResult{}, fmt.Errorf("create invitation: %w", lastErr)
	}
	s.logger.InfoContext(ctx, "invitation created",
		"invitationId", inv.ID, "householdId", inv.HouseholdID, "actorId", actor.UserID, "role", string(role))

	displayCode := FormatCode(code)
	delivered := s.sendEmail(ctx, inv, household, actor.UserID, token, displayCode)
	return CreateResult{Invitation: inv, Code: displayCode, EmailDelivered: delivered}, nil
}

func (s *Service) sendEmail(ctx context.Context, inv Invitation, household households.Household, inviterID, token, displayCode string) bool {
	inviterName := ""
	if s.users != nil {
		if u, err := s.users.GetUser(ctx, inviterID); err == nil {
			inviterName = u.DisplayName
		}
	}
	// Finish sending even if the client disconnects: the invitation exists.
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), emailTimeout)
	defer cancel()
	err := s.email.SendHouseholdInvitation(sendCtx, HouseholdInvitationEmail{
		To:            inv.Email,
		HouseholdName: household.Name,
		InviterName:   inviterName,
		Role:          inv.Role,
		Code:          displayCode,
		AcceptURL:     s.acceptURLBase + url.QueryEscape(token),
		ExpiresAt:     inv.ExpiresAt,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "invitation email not delivered", "invitationId", inv.ID, "error", err)
		return false
	}
	return true
}

// ListPending returns the household's pending, unexpired invitations, newest
// first. It requires members.invite.
func (s *Service) ListPending(ctx context.Context, actor households.Membership) ([]Invitation, error) {
	if !actor.Role.Can(households.PermMembersInvite) {
		return nil, households.ErrForbidden
	}
	return s.store.ListPending(ctx, actor.HouseholdID, s.now().UTC())
}

// Revoke revokes an invitation of the actor's household. It requires
// members.invite. Revoking an invitation that is already revoked, accepted,
// or expired succeeds; an unknown ID returns ErrNotFound.
func (s *Service) Revoke(ctx context.Context, actor households.Membership, invitationID string) error {
	if !actor.Role.Can(households.PermMembersInvite) {
		return households.ErrForbidden
	}
	if _, err := s.store.Get(ctx, actor.HouseholdID, invitationID); err != nil {
		return err
	}
	if err := s.store.Revoke(ctx, actor.HouseholdID, invitationID, s.now().UTC()); err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}
	s.logger.InfoContext(ctx, "invitation revoked",
		"invitationId", invitationID, "householdId", actor.HouseholdID, "actorId", actor.UserID)
	return nil
}

// AcceptResult is returned by Accept.
type AcceptResult struct {
	Household  households.Household
	Membership households.Membership
	// Joined is false when the user was already a member.
	Joined bool
}

// Accept joins userID to the invitation's household. Exactly one of token or
// code must be given. Unknown, expired, revoked, and used invitations all
// return ErrInvalid; the reason is only logged.
//
// If the user is already a member, the invitation is consumed and their
// existing membership is returned unchanged. Retrying an invitation the same
// user already accepted returns their membership again, so a lost response can
// be retried safely.
func (s *Service) Accept(ctx context.Context, userID, token, code string) (AcceptResult, error) {
	token, code = strings.TrimSpace(token), strings.TrimSpace(code)
	if (token == "") == (code == "") {
		return AcceptResult{}, invalidInput("provide exactly one of token or code")
	}

	inv, err := s.find(ctx, token, code)
	if errors.Is(err, ErrNotFound) {
		s.logger.InfoContext(ctx, "invitation rejected", "reason", "unknown", "userId", userID, "via", via(token))
		return AcceptResult{}, ErrInvalid
	}
	if err != nil {
		return AcceptResult{}, fmt.Errorf("find invitation: %w", err)
	}

	now := s.now().UTC()
	if inv.AcceptedBy == userID && inv.RevokedAt.IsZero() {
		return s.acceptedRetry(ctx, inv, userID)
	}
	if reason := inv.unusableReason(now); reason != "" {
		s.reject(ctx, inv, userID, reason)
		return AcceptResult{}, ErrInvalid
	}
	if err := s.store.MarkAccepted(ctx, inv.ID, userID, now); err != nil {
		if errors.Is(err, ErrNotFound) {
			s.reject(ctx, inv, userID, "concurrently accepted or revoked")
			return AcceptResult{}, ErrInvalid
		}
		return AcceptResult{}, fmt.Errorf("mark invitation accepted: %w", err)
	}

	membership, joined, err := s.households.AddMember(ctx, inv.HouseholdID, userID, inv.Role)
	if err != nil {
		if unmarkErr := s.store.UnmarkAccepted(context.WithoutCancel(ctx), inv.ID, userID); unmarkErr != nil {
			s.logger.ErrorContext(ctx, "restore invitation after failed join", "invitationId", inv.ID, "error", unmarkErr)
		}
		if errors.Is(err, households.ErrNotFound) {
			s.reject(ctx, inv, userID, "household no longer exists")
			return AcceptResult{}, ErrInvalid
		}
		return AcceptResult{}, fmt.Errorf("add member: %w", err)
	}
	household, err := s.households.GetHousehold(ctx, inv.HouseholdID)
	if err != nil {
		return AcceptResult{}, fmt.Errorf("get household: %w", err)
	}
	s.logger.InfoContext(ctx, "invitation accepted",
		"invitationId", inv.ID, "householdId", inv.HouseholdID, "userId", userID,
		"role", string(membership.Role), "alreadyMember", !joined)
	return AcceptResult{Household: household, Membership: membership, Joined: joined}, nil
}

func (s *Service) find(ctx context.Context, token, code string) (Invitation, error) {
	if token != "" {
		if len(token) != TokenLength {
			return Invitation{}, ErrNotFound
		}
		return s.store.FindByTokenHash(ctx, hashSecret(token))
	}
	normalized, ok := NormalizeCode(code)
	if !ok {
		return Invitation{}, ErrNotFound
	}
	return s.store.FindByCodeHash(ctx, hashSecret(normalized))
}

// acceptedRetry handles a user presenting an invitation they already accepted.
func (s *Service) acceptedRetry(ctx context.Context, inv Invitation, userID string) (AcceptResult, error) {
	membership, err := s.households.GetMembership(ctx, inv.HouseholdID, userID)
	if errors.Is(err, households.ErrNotFound) {
		// They left or were removed; a used invitation must not let them back in.
		s.reject(ctx, inv, userID, "already accepted by this user, who is no longer a member")
		return AcceptResult{}, ErrInvalid
	}
	if err != nil {
		return AcceptResult{}, fmt.Errorf("get membership: %w", err)
	}
	household, err := s.households.GetHousehold(ctx, inv.HouseholdID)
	if err != nil {
		return AcceptResult{}, fmt.Errorf("get household: %w", err)
	}
	return AcceptResult{Household: household, Membership: membership}, nil
}

func (s *Service) reject(ctx context.Context, inv Invitation, userID, reason string) {
	s.logger.InfoContext(ctx, "invitation rejected",
		"reason", reason, "invitationId", inv.ID, "householdId", inv.HouseholdID, "userId", userID)
}

func via(token string) string {
	if token != "" {
		return "token"
	}
	return "code"
}

// NormalizeEmail trims and lowercases an address and checks that it is a bare
// address (no display name).
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case email == "":
		return "", invalidInput("email is required")
	case len(email) > maxEmailLength:
		return "", invalidInput("email is too long")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email[strings.LastIndexByte(email, '@')+1:], ".") {
		return "", invalidInput("email must be a valid address such as name@example.com")
	}
	return email, nil
}
