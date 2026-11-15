package plus

import (
	"context"
	"strings"
)

// InvitePreview is what the app shows when someone opens an invite link,
// before they sign in: who invited them and what they'll do. It never says
// anything about the inviting household beyond the name it chose.
type InvitePreview struct {
	Code          string
	HouseholdName string
	Title         string
	Message       string
	// Steps are the setup steps, in order: the checklist the new household
	// sees once it accepts.
	Steps []string
	// ForExisting is shown instead to someone whose household is already
	// set up, and ForOwn to the household the code belongs to.
	ForExisting InviteNotice
	ForOwn      InviteNotice
}

// InviteNotice is a title and a line under it.
type InviteNotice struct {
	Title   string
	Message string
}

// Invite preview copy.
const (
	InviteMessage         = "You both get a free month of Plus once you're set up."
	InviteExistingTitle   = "This code is for new households"
	InviteExistingMessage = "Your household is already set up, so it can't use an invite code. Share your own code to give someone else a free month."
	InviteOwnTitle        = "That's your own code"
	InviteOwnMessage      = "Share it with another household. You both get a free month of Plus once they're set up."
)

// InviteTitle is "Lines Family invited you to DinnerOS".
func InviteTitle(householdName string) string {
	name := strings.TrimSpace(householdName)
	if name == "" {
		return "You're invited to DinnerOS"
	}
	return name + " invited you to DinnerOS"
}

// SetupSteps are a new household's steps toward its free month, as the
// checklist names them.
func (r ReferralRules) SetupSteps() []string {
	return []string{StepHousehold, capitalize(r.recipePhrase()), capitalize(r.dinnerPhrase())}
}

// PreviewInvite looks up an invite code for the invite screen. A replaced
// code is ErrCodeRetired, an unknown one ErrCodeNotFound, and referrals
// switched off for the code's household ErrReferralsOff.
func (s *Service) PreviewInvite(ctx context.Context, code string) (InvitePreview, error) {
	if s.referrals == nil || s.free == nil {
		return InvitePreview{}, ErrReferralsOff
	}
	owner, err := s.owner(ctx, code)
	if err != nil {
		return InvitePreview{}, err
	}
	if !s.EnabledFor(owner) {
		return InvitePreview{}, ErrReferralsOff
	}
	name := strings.TrimSpace(s.householdName(ctx, owner))
	return InvitePreview{
		Code:          strings.TrimSpace(code),
		HouseholdName: name,
		Title:         InviteTitle(name),
		Message:       InviteMessage,
		Steps:         s.rules.SetupSteps(),
		ForExisting:   InviteNotice{Title: InviteExistingTitle, Message: InviteExistingMessage},
		ForOwn:        InviteNotice{Title: InviteOwnTitle, Message: InviteOwnMessage},
	}, nil
}
