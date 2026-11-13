package plus

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"time"
)

// Referrals (decision 675): every household has a code to share. A household
// new to Plus that enters another household's code gets a free month, and so
// does the household that shared it, up to MaxReferralMonths. Free months are
// kept here, not at Apple: Apple keeps billing a paying household, so a free
// month for one is added after the time it has paid for.

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
		return "We couldn't find that code. Check it and try again."
	case errors.Is(err, ErrOwnCode):
		return "That's your own code. Share it with another household."
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

// Referral is one household joining Plus with another's code.
type Referral struct {
	ReferrerID string
	ReferredID string
	At         time.Time
	// ReferrerRewarded is false once the referrer has earned
	// MaxReferralMonths.
	ReferrerRewarded bool
}

// ReferralStore keeps codes and who used them.
type ReferralStore interface {
	// Code returns the household's code, making one the first time.
	Code(ctx context.Context, householdID string, newCode func() string) (string, error)
	// Owner returns the household a code belongs to, or ErrCodeNotFound.
	Owner(ctx context.Context, code string) (string, error)
	// Record stores r, or returns ErrAlreadyRedeemed if r.ReferredID already
	// used a code.
	Record(ctx context.Context, r Referral) error
	// Used reports whether householdID already used a code.
	Used(ctx context.Context, householdID string) (bool, error)
	// Rewarded counts the referrals that earned referrerID a month.
	Rewarded(ctx context.Context, referrerID string) (int, error)
	PurgeHousehold(ctx context.Context, householdID string) error
}

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

// NormalizeCode reads a code the way a person might type or paste it:
// lowercase, with spaces or dashes, or inside an invite link.
func NormalizeCode(s string) string {
	if i := strings.LastIndexAny(s, "/="); i >= 0 {
		s = s[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if strings.ContainsRune(codeAlphabet, r) {
			b.WriteRune(r)
		}
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
}

// Referral returns the household's code and what it has earned.
func (s *Service) Referral(ctx context.Context, householdID string) (ReferralInfo, error) {
	if !s.EnabledFor(householdID) || s.referrals == nil {
		return ReferralInfo{}, ErrReferralsOff
	}
	code, err := s.referrals.Code(ctx, householdID, NewCode)
	if err != nil {
		return ReferralInfo{}, err
	}
	n, err := s.referrals.Rewarded(ctx, householdID)
	if err != nil {
		return ReferralInfo{}, err
	}
	return ReferralInfo{Code: code, Rewarded: n, MonthsLeft: max(0, MaxReferralMonths-n)}, nil
}

// Redeem gives householdID and the code's owner a free month each.
func (s *Service) Redeem(ctx context.Context, householdID, code string) (Status, error) {
	if !s.EnabledFor(householdID) || s.referrals == nil || s.free == nil {
		return Status{}, ErrReferralsOff
	}
	code = NormalizeCode(code)
	if len(code) != CodeLength {
		return Status{}, ErrCodeNotFound
	}
	owner, err := s.referrals.Owner(ctx, code)
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
	rewarded, err := s.referrals.Rewarded(ctx, owner)
	if err != nil {
		return Status{}, err
	}
	now := s.now().UTC()
	ref := Referral{ReferrerID: owner, ReferredID: householdID, At: now, ReferrerRewarded: rewarded < MaxReferralMonths}
	// Recording first means two codes entered at once can't both count.
	if err := s.referrals.Record(ctx, ref); err != nil {
		return Status{}, err
	}
	month := FreeMonthDays * 24 * time.Hour
	if _, err := s.free.Extend(ctx, householdID, now, month); err != nil {
		return Status{}, err
	}
	if ref.ReferrerRewarded {
		if _, err := s.free.Extend(ctx, owner, s.paidUntil(ctx, owner, now), month); err != nil {
			return Status{}, err
		}
	}
	return s.Status(ctx, householdID)
}

// paidUntil is when a household's paid Plus ends, or now if it isn't paying.
func (s *Service) paidUntil(ctx context.Context, householdID string, now time.Time) time.Time {
	e, err := s.store.Get(ctx, householdID)
	if err != nil || !e.Active(now) || e.ExpiresAt.IsZero() || e.ExpiresAt.Before(now) {
		return now
	}
	return e.ExpiresAt
}
