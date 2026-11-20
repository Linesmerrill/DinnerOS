package plus

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"time"
)

// Referrals (decisions 675 and 698): every household has a code to share. A
// household new to Plus that enters another household's code starts a
// pending referral. It qualifies once the new household is really set up
// (qualify.go), and then both households get a free month, the sharer up to
// MaxReferralMonths. Since decision 755 each month is an Apple offer code
// (offer_codes.go); months granted before that are dates in
// plus_free_time, honored until they end.

// Referral errors. Each has a message a person can read in the app.
var (
	ErrCodeNotFound    = errors.New("plus: referral code not found")
	ErrOwnCode         = errors.New("plus: own referral code")
	ErrAlreadyRedeemed = errors.New("plus: household already used a code")
	ErrNotNewToPlus    = errors.New("plus: household already had Plus")
	ErrReferralsOff    = errors.New("plus: referrals off")
)

// RedeemMessage is what the app shows for a referral error.
func RedeemMessage(err error) string {
	switch {
	case errors.Is(err, ErrCodeNotFound):
		return "We couldn’t find that code. Check it and try again."
	case errors.Is(err, ErrCodeRetired):
		return codeRetiredText
	case errors.Is(err, ErrOwnCode):
		return "That’s your own code. Share it with another household."
	case errors.Is(err, ErrAlreadyRedeemed):
		return "Your household already used a code."
	case errors.Is(err, ErrNotNewToPlus):
		return "Codes are for households new to Plus."
	}
	return "Something went wrong. Try again."
}

// FreeTimeStore keeps each household's free Plus time from referrals.
type FreeTimeStore interface {
	// FreeUntil is when the household's free time ends; zero if it has none.
	FreeUntil(ctx context.Context, householdID string) (time.Time, error)
	// Extend adds d to the household's free time, starting at the later of
	// its current end and from, and returns the new end.
	Extend(ctx context.Context, householdID string, from time.Time, d time.Duration) (time.Time, error)
	PurgeHousehold(ctx context.Context, householdID string) error
}

// ReferralState is where a referral is.
type ReferralState string

// Referral states. Ready is shown as pending: every condition is met and it
// waits for the sharer's 30-day limit or the next run.
const (
	StatePending   ReferralState = "pending"
	StateReady     ReferralState = "ready"
	StateQualified ReferralState = "qualified"
	StateExpired   ReferralState = "expired"
)

// Open reports whether the referral can still qualify.
func (s ReferralState) Open() bool { return s == StatePending || s == StateReady }

// Checks is what the anti-farming checks last found, never shown to anyone.
type Checks string

// Check results. Unknown means a check couldn't be made yet (no DeviceCheck
// token, or no key): the referral can't qualify until it can.
const (
	ChecksUnknown Checks = ""
	ChecksPassed  Checks = "passed"
	ChecksBlocked Checks = "blocked"
)

// Referral is one household entering another's code.
type Referral struct {
	ID         string
	ReferrerID string
	ReferredID string
	// At is when the code was entered.
	At    time.Time
	State ReferralState
	// ExpiresAt ends the setup window, At plus REFERRAL_QUALIFY_DAYS.
	ExpiresAt time.Time
	// SetupMetAt is when the new household first had the setup the referral
	// needs, within the window.
	SetupMetAt  time.Time
	QualifiedAt time.Time
	// ReferrerRewarded is false once the referrer has earned
	// MaxReferralMonths.
	ReferrerRewarded bool
	Checks           Checks
	// Settled is set once both free months and both notifications are done.
	Settled bool
}

// EarnedAt is when the referral paid its months: QualifiedAt, or At for one
// from before qualifying existed.
func (r Referral) EarnedAt() time.Time {
	if !r.QualifiedAt.IsZero() {
		return r.QualifiedAt
	}
	return r.At
}

// ReferralCounts counts one sharer's referrals by state; Pending includes
// ready.
type ReferralCounts struct {
	Pending, Qualified, Expired int
}

func (c *ReferralCounts) add(state ReferralState, n int) {
	switch state {
	case StateQualified:
		c.Qualified += n
	case StateExpired:
		c.Expired += n
	default:
		c.Pending += n
	}
}

// Total is every household that used the code.
func (c ReferralCounts) Total() int { return c.Pending + c.Qualified + c.Expired }

// Grant flags, each set once by ReferralStore.Claim.
const (
	FlagReferredGranted = "referredGranted"
	FlagReferrerGranted = "referrerGranted"
	FlagSettled         = "settled"
)

// ReferralStore keeps codes and who used them.
type ReferralStore interface {
	// Code returns the household's current code, making one the first time.
	Code(ctx context.Context, householdID string, newCode func() string) (string, error)
	// Owner returns the household whose current code is code, compared
	// without case; ErrCodeRetired if it was replaced; else ErrCodeNotFound.
	Owner(ctx context.Context, code string) (string, error)
	// Taken reports whether any household has or had code, compared without
	// case (decision 699).
	Taken(ctx context.Context, code string) (bool, error)
	// CodeChanges returns when the household last changed its code, at
	// least the last CodeChangeLimit times.
	CodeChanges(ctx context.Context, householdID string) ([]time.Time, error)
	// Replace makes code the household's code in place of old, if old is
	// still its code (else ErrCodeChangedNow), and retires old for good.
	// A code any household has or had is ErrCodeTaken.
	Replace(ctx context.Context, householdID, old, code string, at time.Time) error
	// Record stores r as given (pending) and sets its ID, or returns
	// ErrAlreadyRedeemed if r.ReferredID already used a code.
	Record(ctx context.Context, r *Referral) error
	// Used reports whether householdID already used a code.
	Used(ctx context.Context, householdID string) (bool, error)
	// Rewarded counts the qualified referrals that earned referrerID a month.
	Rewarded(ctx context.Context, referrerID string) (int, error)
	// QualifiedSince counts referrerID's referrals that qualified at or
	// after since.
	QualifiedSince(ctx context.Context, referrerID string, since time.Time) (int, error)
	// Joins counts the households that used referrerID's code by state and
	// returns up to limit of them, newest first.
	Joins(ctx context.Context, referrerID string, limit int) (ReferralCounts, []Referral, error)
	// UsedBy returns the referral where householdID used a code, if any.
	UsedBy(ctx context.Context, householdID string) (Referral, bool, error)
	// Open returns up to limit referrals still to work on, in ID order
	// after the ID after (from the first when empty): pending or ready, or
	// qualified and not settled.
	Open(ctx context.Context, after string, limit int) ([]Referral, error)
	// SetSetupMet records when the referred household met setup, once.
	SetSetupMet(ctx context.Context, referredID string, at time.Time) error
	// SetChecks records what the anti-farming checks found.
	SetChecks(ctx context.Context, referredID string, c Checks) error
	// Move changes an open referral's state from one of from to to, and
	// reports whether it did. It is one conditional update, so two callers
	// can't both move it.
	Move(ctx context.Context, referredID string, from []ReferralState, to ReferralState) (bool, error)
	// Qualify moves an open referral to qualified at at, with rewarded,
	// and reports whether this call did. Only one caller ever wins.
	Qualify(ctx context.Context, referredID string, at time.Time, rewarded bool) (bool, error)
	// Claim sets one of the Flag* flags and reports whether this call set
	// it: the guard that grants each month and finishes each referral once.
	Claim(ctx context.Context, referredID, flag string) (bool, error)
	// Report sums referrals across every household, for the people who run
	// DinnerOS.
	Report(ctx context.Context, since time.Time, top int) (ReferralReport, error)
	PurgeHousehold(ctx context.Context, householdID string) error
}

// ReferralReport is referrals across every household.
type ReferralReport struct {
	// Codes counts households whose code was used at least once.
	Codes int
	// Joined counts households that used a code; JoinedSince those that did
	// from the report's since on.
	Joined, JoinedSince int
	// Counts splits Joined by state.
	Counts ReferralCounts
	// Top is the households whose codes were used most, most first.
	Top []ReferrerCount
}

// ReferrerCount is how many households used one household's code.
type ReferrerCount struct {
	HouseholdID string
	// Name is the household's name, for the people who run DinnerOS only.
	Name  string
	Count int
}

// RecentJoins is how many joins the invite screen lists.
const RecentJoins = 20

// codeAlphabet leaves out 0, O, 1, I and L, which are easy to misread.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// CodeLength is the length of a referral code.
const CodeLength = 8

// NewCode returns a random referral code.
func NewCode() string {
	var b strings.Builder
	max := big.NewInt(int64(len(codeAlphabet)))
	for range CodeLength {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err) // crypto/rand doesn't fail on supported platforms
		}
		b.WriteByte(codeAlphabet[n.Int64()])
	}
	return b.String()
}

// WithReferrals turns on free months and referral codes.
func (s *Service) WithReferrals(free FreeTimeStore, referrals ReferralStore) *Service {
	s.free, s.referrals = free, referrals
	return s
}

// ReferralInfo is what a household sees about its own code.
type ReferralInfo struct {
	Code string
	// Rewarded counts households that joined with the code and earned a
	// month; MonthsLeft is how many more months the code can still earn.
	Rewarded   int
	MonthsLeft int
	// Counts splits the households that used the code by state; Recent
	// lists up to RecentJoins of them, newest first.
	Counts ReferralCounts
	Recent []InviteItem
	// FreeUntil is when the household's free months from before decision
	// 755 end, if they haven't.
	FreeUntil time.Time
	// FreeMonths are the months it earned as offer codes, oldest first.
	FreeMonths []FreeMonth
	// UsedCode is set when this household entered another's code;
	// Progress says how far its own referral is.
	UsedCode *Referral
	Progress *Progress
	// CodeChanges is how many more times the code can change (decision
	// 699); Location is the household's time zone for its date.
	CodeChanges CodeChanges
	Location    *time.Location
}

// InviteItem is one household that used the code, as its sharer sees it.
type InviteItem struct {
	Referral
	// HouseholdName is the household's own chosen name.
	HouseholdName string
	// Setup is how far the household is, for what's left to do.
	Setup Setup
}

// Referral returns the household's code, what it has earned, and who used it.
func (s *Service) Referral(ctx context.Context, householdID string) (ReferralInfo, error) {
	if !s.EnabledFor(householdID) || s.referrals == nil {
		return ReferralInfo{}, ErrReferralsOff
	}
	code, err := s.referrals.Code(ctx, householdID, newCleanCode)
	if err != nil {
		return ReferralInfo{}, err
	}
	n, err := s.referrals.Rewarded(ctx, householdID)
	if err != nil {
		return ReferralInfo{}, err
	}
	info := ReferralInfo{Code: code, Rewarded: n, MonthsLeft: max(0, MaxReferralMonths-n)}
	changes, err := s.referrals.CodeChanges(ctx, householdID)
	if err != nil {
		return ReferralInfo{}, err
	}
	info.CodeChanges = codeChanges(changes, s.now())
	if info.CodeChanges.Left == 0 {
		info.Location = s.location(ctx, householdID)
	}
	counts, recent, err := s.referrals.Joins(ctx, householdID, RecentJoins)
	if err != nil {
		return ReferralInfo{}, err
	}
	info.Counts = counts
	for _, r := range recent {
		item := InviteItem{Referral: r}
		if s.households != nil {
			if h, err := s.households.GetHousehold(ctx, r.ReferredID); err == nil {
				item.HouseholdName = h.Name
			}
		}
		if s.setup != nil && r.State.Open() {
			if item.Setup, err = s.setup.Get(ctx, r.ReferredID); err != nil {
				return ReferralInfo{}, err
			}
		}
		info.Recent = append(info.Recent, item)
	}
	if used, ok, err := s.referrals.UsedBy(ctx, householdID); err != nil {
		return ReferralInfo{}, err
	} else if ok {
		info.UsedCode = &used
		p, err := s.progress(ctx, used)
		if err != nil {
			return ReferralInfo{}, err
		}
		info.Progress = &p
	}
	if s.free != nil {
		until, err := s.free.FreeUntil(ctx, householdID)
		if err != nil {
			return ReferralInfo{}, err
		}
		if until.After(s.now()) {
			info.FreeUntil = until
		}
	}
	if info.FreeMonths, err = s.FreeMonths(ctx, householdID); err != nil {
		return ReferralInfo{}, err
	}
	return info, nil
}

// ReferralReport sums referrals across every household: the last 7 days and
// all time, and the top 5 sharers.
func (s *Service) ReferralReport(ctx context.Context) (ReferralReport, error) {
	if s.referrals == nil {
		return ReferralReport{}, ErrReferralsOff
	}
	return s.referrals.Report(ctx, s.now().Add(-7*24*time.Hour), 5)
}

// Redeem records householdID entering code: a pending referral that pays a
// free month to each household once it qualifies (qualify.go). The request's
// DeviceCheck token, if any, is in ctx.
func (s *Service) Redeem(ctx context.Context, householdID, code string) (Status, error) {
	if !s.EnabledFor(householdID) || s.referrals == nil || s.free == nil {
		return Status{}, ErrReferralsOff
	}
	owner, err := s.owner(ctx, code)
	if err != nil {
		return Status{}, err
	}
	if owner == householdID {
		return Status{}, ErrOwnCode
	}
	if used, err := s.referrals.Used(ctx, householdID); err != nil {
		return Status{}, err
	} else if used {
		return Status{}, ErrAlreadyRedeemed
	}
	// A household that has ever subscribed, or already has free time (from
	// sharing its own code), isn't new to Plus.
	if _, err := s.store.Get(ctx, householdID); err == nil {
		return Status{}, ErrNotNewToPlus
	} else if !errors.Is(err, ErrNotFound) {
		return Status{}, err
	}
	if until, err := s.free.FreeUntil(ctx, householdID); err != nil {
		return Status{}, err
	} else if !until.IsZero() {
		return Status{}, ErrNotNewToPlus
	}
	if months, err := s.FreeMonths(ctx, householdID); err != nil {
		return Status{}, err
	} else if len(months) > 0 {
		return Status{}, ErrNotNewToPlus
	}
	now := s.now().UTC()
	ref := Referral{
		ReferrerID: owner, ReferredID: householdID, At: now, State: StatePending,
		ExpiresAt: now.Add(s.rules.window()),
	}
	// Recording first means two codes entered at once can't both count.
	if err := s.referrals.Record(ctx, &ref); err != nil {
		return Status{}, err
	}
	s.checkDevice(ctx, householdID, deviceAtCodeEntry)
	// A household already set up can qualify now.
	if err := s.Evaluate(ctx, householdID); err != nil {
		s.logger.ErrorContext(ctx, "evaluate referral after code entry", "householdId", householdID, "error", err)
	}
	return s.Status(ctx, householdID)
}
