package plus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// Qualifying referrals (decision 698). Entering a code makes a pending
// referral. It qualifies, and both households get their free month, only
// when all of these hold:
//
//   - the new household's creator had been in no other household before
//     creating it, by sign-in identity (Apple or Google), whatever accounts
//     were deleted since (HistoryStore);
//   - the device that created it, or entered the code, wasn't already tied
//     to another household, by Apple DeviceCheck (DeviceTieStore);
//   - it never shared a member with the sharer's household;
//   - within REFERRAL_QUALIFY_DAYS of the code it planned REFERRAL_MIN_DINNERS
//     dinners and saved REFERRAL_MIN_RECIPES recipes (SetupStore);
//   - the sharer had fewer than REFERRAL_MAX_PER_30_DAYS referrals qualify
//     in the last 30 days. One over waits ("ready") until a slot frees.
//
// It's evaluated when the new household plans a dinner or saves a recipe,
// and by a sweep at least hourly. Qualifying is one conditional update, and
// each month and finishing are claimed flags, so retries and races never pay
// or notify twice.

// ReferralRules are the thresholds, from config.
type ReferralRules struct {
	// QualifyDays is the setup window after the code (REFERRAL_QUALIFY_DAYS).
	QualifyDays int
	// MinDinners and MinRecipes are the setup (REFERRAL_MIN_DINNERS,
	// REFERRAL_MIN_RECIPES).
	MinDinners int
	MinRecipes int
	// MaxPer30Days caps one sharer's qualified referrals in any 30 days
	// (REFERRAL_MAX_PER_30_DAYS).
	MaxPer30Days int
}

// Referral rule defaults.
const (
	DefaultQualifyDays  = 14
	DefaultMinDinners   = 1
	DefaultMinRecipes   = 3
	DefaultMaxPer30Days = 3
)

// DefaultReferralRules are the rules without config.
func DefaultReferralRules() ReferralRules {
	return ReferralRules{QualifyDays: DefaultQualifyDays, MinDinners: DefaultMinDinners, MinRecipes: DefaultMinRecipes, MaxPer30Days: DefaultMaxPer30Days}
}

func (r ReferralRules) window() time.Duration { return time.Duration(r.QualifyDays) * 24 * time.Hour }

// met reports whether s is the setup the rules ask for.
func (r ReferralRules) met(s Setup) bool {
	return s.Dinners >= r.MinDinners && s.Recipes >= r.MinRecipes
}

// Timing.
const (
	// RewardWithin is how soon a qualified referral pays, as the copy
	// promises: the sweep runs every SweepEvery, well inside it.
	RewardWithin = 24 * time.Hour
	SweepEvery   = time.Hour
	// referrerWindow is the rolling window MaxPer30Days counts in.
	referrerWindow = 30 * 24 * time.Hour
	// lockFor bounds how long one evaluation holds a sharer's lock.
	lockFor = 30 * time.Second
	// deviceCheckTimeout bounds a DeviceCheck call in a request.
	deviceCheckTimeout = 5 * time.Second
	sweepPage          = 200
)

// Setup is what a household has done toward a referral.
type Setup struct {
	// Dinners counts meals planned (not add-ons); Recipes counts recipes it
	// imported or typed. Neither goes down when something is removed.
	Dinners int
	Recipes int
}

// SetupStore counts every household's setup.
type SetupStore interface {
	Add(ctx context.Context, householdID string, dinners, recipes int) error
	Get(ctx context.Context, householdID string) (Setup, error)
	PurgeHousehold(ctx context.Context, householdID string) error
}

// MembershipRecord says a sign-in identity was in a household from At.
type MembershipRecord struct {
	// Identity is IdentityKey of the provider and subject, never the subject.
	Identity    string
	HouseholdID string
	UserID      string
	At          time.Time
}

// HistoryStore keeps every membership by sign-in identity, for good: leaving
// a household, deleting it, or deleting the account doesn't erase that the
// identity was there.
type HistoryStore interface {
	// Add stores records, keeping the earliest At for an identity and
	// household.
	Add(ctx context.Context, recs []MembershipRecord) error
	ForIdentities(ctx context.Context, identities []string) ([]MembershipRecord, error)
	ForHousehold(ctx context.Context, householdID string) ([]MembershipRecord, error)
}

// IdentityKey is the stored form of a sign-in identity: a hash, so the
// provider's subject is never kept here.
func IdentityKey(provider users.Provider, subject string) string {
	sum := sha256.Sum256([]byte("dinneros referral identity\x00" + string(provider) + "\x00" + subject))
	return hex.EncodeToString(sum[:])
}

// DeviceTie is what DeviceCheck said about a household's devices.
type DeviceTie struct {
	// Owns is true once a device was first tied to a household by this one.
	Owns bool
	// Elsewhere is true once this household was created, or entered a code,
	// on a device another household had tied.
	Elsewhere bool
}

// DeviceTieStore keeps DeviceTie per household.
type DeviceTieStore interface {
	MarkOwned(ctx context.Context, householdID string, at time.Time) error
	MarkElsewhere(ctx context.Context, householdID string, at time.Time) error
	Get(ctx context.Context, householdID string) (DeviceTie, error)
	PurgeHousehold(ctx context.Context, householdID string) error
}

// LockStore is a short lease per key, so one sharer's referrals qualify one
// at a time and the 30-day cap holds under concurrency.
type LockStore interface {
	// Acquire takes key until until, unless someone holds it past now.
	Acquire(ctx context.Context, key string, now, until time.Time) (bool, error)
	Release(ctx context.Context, key string) error
}

// HouseholdReader reads a household's name, creator, and creation time.
// *households.Service implements it.
type HouseholdReader interface {
	GetHousehold(ctx context.Context, id string) (households.Household, error)
}

// IdentityLister lists a user's sign-in identities. *users.Service
// implements it.
type IdentityLister interface {
	ListIdentities(ctx context.Context, userID string) ([]users.AuthIdentity, error)
}

// Notifier creates in-app notifications, pushed by the push sweep.
// *notifications.Service implements it.
type Notifier interface {
	Create(ctx context.Context, in notifications.New) (notifications.Notification, bool, error)
}

// QualifyOptions wires qualifying. Any store left nil fails closed: a
// referral that can't be checked stays pending and expires.
type QualifyOptions struct {
	Rules      ReferralRules
	Setup      SetupStore
	History    HistoryStore
	Devices    DeviceTieStore
	Locks      LockStore
	Households HouseholdReader
	Identities IdentityLister
	Notifier   Notifier
	// DeviceCheck is Apple DeviceCheck; nil without a key.
	DeviceCheck DeviceChecker
	// AllowWithoutToken counts a request with no DeviceCheck token, or
	// with no key to check one, as a new device. Development only:
	// simulators can't make tokens.
	AllowWithoutToken bool
	Logger            *slog.Logger
}

// WithQualify turns on qualifying referrals. It returns s.
func (s *Service) WithQualify(o QualifyOptions) *Service {
	if o.Rules == (ReferralRules{}) {
		o.Rules = DefaultReferralRules()
	}
	s.rules, s.setup, s.history, s.devices, s.locks = o.Rules, o.Setup, o.History, o.Devices, o.Locks
	s.households, s.identities, s.notifier, s.deviceCheck = o.Households, o.Identities, o.Notifier, o.DeviceCheck
	s.allowWithoutToken = o.AllowWithoutToken
	if o.Logger != nil {
		s.logger = o.Logger
	}
	return s
}

// Rules returns the referral rules in force.
func (s *Service) Rules() ReferralRules { return s.rules }

// --- What feeds the checks ------------------------------------------------

// deviceEvent is why a device is checked.
type deviceEvent int

const (
	deviceAtCreate deviceEvent = iota
	deviceAtJoin
	deviceAtCodeEntry
)

// MemberAdded implements households.MemberListener: it records the member's
// sign-in identities in the household's history, and checks the device the
// request came from.
func (s *Service) MemberAdded(ctx context.Context, m households.MemberAdded) {
	s.recordMember(ctx, m.HouseholdID, m.UserID, m.At)
	ev := deviceAtJoin
	if m.Created {
		ev = deviceAtCreate
	}
	s.checkDevice(ctx, m.HouseholdID, ev)
}

func (s *Service) recordMember(ctx context.Context, householdID, userID string, at time.Time) {
	if s.history == nil || s.identities == nil {
		return
	}
	ids, err := s.identities.ListIdentities(ctx, userID)
	if err != nil {
		s.logger.ErrorContext(ctx, "list identities for referral history", "userId", userID, "error", err)
		return
	}
	recs := make([]MembershipRecord, 0, len(ids))
	for _, id := range ids {
		recs = append(recs, MembershipRecord{Identity: IdentityKey(id.Provider, id.Subject), HouseholdID: householdID, UserID: userID, At: at.UTC()})
	}
	if len(recs) == 0 {
		return
	}
	if err := s.history.Add(ctx, recs); err != nil {
		s.logger.ErrorContext(ctx, "record referral history", "householdId", householdID, "error", err)
	}
}

// BackfillHistory records current memberships, for households from before
// the history existed. It is idempotent: a record keeps its earliest time.
func (s *Service) BackfillHistory(ctx context.Context, members func(yield func(householdID, userID string, at time.Time) bool) error) (int, error) {
	if s.history == nil || s.identities == nil {
		return 0, nil
	}
	n := 0
	err := members(func(householdID, userID string, at time.Time) bool {
		s.recordMember(ctx, householdID, userID, at)
		n++
		return ctx.Err() == nil
	})
	return n, err
}

// checkDevice asks DeviceCheck whether the request's device is already tied
// to a household, and ties it to this one if not. The household that ties a
// device owns it, so its own bit never counts against it.
func (s *Service) checkDevice(ctx context.Context, householdID string, ev deviceEvent) {
	if s.devices == nil {
		return
	}
	now := s.now().UTC()
	proof, ok := households.DeviceProofFromContext(ctx)
	if !ok || s.deviceCheck == nil {
		if s.allowWithoutToken {
			if err := s.devices.MarkOwned(ctx, householdID, now); err != nil {
				s.logger.ErrorContext(ctx, "record device", "householdId", householdID, "error", err)
			}
		}
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deviceCheckTimeout)
	defer cancel()
	tied, err := s.deviceCheck.Query(ctx, proof)
	if err != nil {
		s.logger.WarnContext(ctx, "devicecheck query failed", "householdId", householdID, "error", err)
		return
	}
	if !tied {
		if err := s.deviceCheck.Tie(ctx, proof); err != nil {
			s.logger.WarnContext(ctx, "devicecheck update failed", "householdId", householdID, "error", err)
			return
		}
		if err := s.devices.MarkOwned(ctx, householdID, now); err != nil {
			s.logger.ErrorContext(ctx, "record device", "householdId", householdID, "error", err)
		}
		return
	}
	tie, err := s.devices.Get(ctx, householdID)
	if err != nil {
		s.logger.ErrorContext(ctx, "read device", "householdId", householdID, "error", err)
		return
	}
	// Joining a household from a phone that has its own is normal; making
	// one, or entering a code, there is not.
	if tie.Owns || ev == deviceAtJoin {
		return
	}
	if err := s.devices.MarkElsewhere(ctx, householdID, now); err != nil {
		s.logger.ErrorContext(ctx, "record device", "householdId", householdID, "error", err)
	}
}

// DinnersPlanned implements planning.DinnerListener.
func (s *Service) DinnersPlanned(ctx context.Context, householdID string, n int) {
	s.addSetup(ctx, householdID, n, 0)
}

// RecipesSaved implements recipes.SaveListener.
func (s *Service) RecipesSaved(ctx context.Context, householdID string, n int) {
	s.addSetup(ctx, householdID, 0, n)
}

func (s *Service) addSetup(ctx context.Context, householdID string, dinners, recipes int) {
	if s.setup == nil {
		return
	}
	// The change is saved; this must not hang on to the request.
	ctx = context.WithoutCancel(ctx)
	if err := s.setup.Add(ctx, householdID, dinners, recipes); err != nil {
		s.logger.ErrorContext(ctx, "count referral setup", "householdId", householdID, "error", err)
		return
	}
	if err := s.Evaluate(ctx, householdID); err != nil {
		s.logger.ErrorContext(ctx, "evaluate referral", "householdId", householdID, "error", err)
	}
}

// --- Evaluating -----------------------------------------------------------

// Evaluate works on the referral householdID made by entering a code, if
// it has one: expire it, mark it ready, qualify it, or finish paying it.
func (s *Service) Evaluate(ctx context.Context, householdID string) error {
	if s.referrals == nil {
		return nil
	}
	ref, ok, err := s.referrals.UsedBy(ctx, householdID)
	if err != nil || !ok {
		return err
	}
	return s.evaluate(ctx, ref)
}

// SweepResult counts what one sweep looked at.
type SweepResult struct {
	Checked int
	Failed  int
}

// Sweep evaluates every open referral: the expiries, the referrals that
// wait for a sharer's 30-day slot, and any whose payment was cut off.
func (s *Service) Sweep(ctx context.Context) (SweepResult, error) {
	var res SweepResult
	if s.referrals == nil {
		return res, nil
	}
	after := ""
	for {
		page, err := s.referrals.Open(ctx, after, sweepPage)
		if err != nil {
			return res, err
		}
		for _, ref := range page {
			res.Checked++
			if err := s.evaluate(ctx, ref); err != nil {
				res.Failed++
				s.logger.ErrorContext(ctx, "sweep referral", "householdId", ref.ReferredID, "error", err)
			}
		}
		if len(page) < sweepPage || ctx.Err() != nil {
			return res, ctx.Err()
		}
		after = page[len(page)-1].ID
	}
}

// RunSweeps sweeps now and then every every until ctx ends.
func (s *Service) RunSweeps(ctx context.Context, every time.Duration) {
	run := func() {
		res, err := s.Sweep(ctx)
		if err != nil && ctx.Err() == nil {
			s.logger.ErrorContext(ctx, "referral sweep failed", "error", err)
		}
		if res.Checked > 0 {
			s.logger.InfoContext(ctx, "referral sweep", "checked", res.Checked, "failed", res.Failed)
		}
	}
	run()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

func (s *Service) evaluate(ctx context.Context, ref Referral) error {
	now := s.now().UTC()
	switch {
	case ref.State == StateQualified:
		if ref.Settled {
			return nil
		}
		return s.settle(ctx, ref)
	case !ref.State.Open():
		return nil
	case ref.State == StatePending && !now.Before(ref.ExpiresAt):
		_, err := s.referrals.Move(ctx, ref.ReferredID, []ReferralState{StatePending}, StateExpired)
		return err
	}

	checks, err := s.runChecks(ctx, ref)
	if err != nil {
		return err
	}
	if checks != ref.Checks {
		if err := s.referrals.SetChecks(ctx, ref.ReferredID, checks); err != nil {
			return err
		}
	}
	if ref.SetupMetAt.IsZero() && s.setup != nil {
		setup, err := s.setup.Get(ctx, ref.ReferredID)
		if err != nil {
			return err
		}
		if s.rules.met(setup) {
			if err := s.referrals.SetSetupMet(ctx, ref.ReferredID, now); err != nil {
				return err
			}
			ref.SetupMetAt = now
		}
	}
	if checks != ChecksPassed || ref.SetupMetAt.IsZero() {
		if ref.State == StateReady {
			// A check that passed has stopped passing (a shared member):
			// it's pending again, and expires on time.
			if _, err := s.referrals.Move(ctx, ref.ReferredID, []ReferralState{StateReady}, StatePending); err != nil {
				return err
			}
			if !now.Before(ref.ExpiresAt) {
				_, err := s.referrals.Move(ctx, ref.ReferredID, []ReferralState{StatePending}, StateExpired)
				return err
			}
		}
		return nil
	}
	// Everything is met within the window: from here it waits for the
	// sharer's slot, however long that takes.
	if ref.State == StatePending {
		if _, err := s.referrals.Move(ctx, ref.ReferredID, []ReferralState{StatePending}, StateReady); err != nil {
			return err
		}
	}
	return s.qualify(ctx, ref)
}

// runChecks makes the anti-farming checks. Blocked wins over unknown.
func (s *Service) runChecks(ctx context.Context, ref Referral) (Checks, error) {
	if s.history == nil || s.devices == nil || s.households == nil {
		return ChecksUnknown, nil
	}
	result := ChecksPassed
	unknown := func() {
		if result == ChecksPassed {
			result = ChecksUnknown
		}
	}

	h, err := s.households.GetHousehold(ctx, ref.ReferredID)
	if errors.Is(err, households.ErrNotFound) {
		return ChecksBlocked, nil
	}
	if err != nil {
		return "", err
	}
	referred, err := s.history.ForHousehold(ctx, ref.ReferredID)
	if err != nil {
		return "", err
	}
	// The creator's identities had been in no other household before this
	// one was made.
	var creator []string
	for _, r := range referred {
		if r.UserID == h.CreatedBy {
			creator = append(creator, r.Identity)
		}
	}
	if len(creator) == 0 {
		unknown()
	} else {
		earlier, err := s.history.ForIdentities(ctx, creator)
		if err != nil {
			return "", err
		}
		for _, r := range earlier {
			if r.HouseholdID != ref.ReferredID && !r.At.After(h.CreatedAt) {
				return ChecksBlocked, nil
			}
		}
	}
	// No member, now or ever, in both households.
	referrer, err := s.history.ForHousehold(ctx, ref.ReferrerID)
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, r := range referrer {
		seen["i:"+r.Identity], seen["u:"+r.UserID] = true, true
	}
	for _, r := range referred {
		if seen["i:"+r.Identity] || seen["u:"+r.UserID] {
			return ChecksBlocked, nil
		}
	}
	// No device tied to another household first.
	tie, err := s.devices.Get(ctx, ref.ReferredID)
	if err != nil {
		return "", err
	}
	if tie.Elsewhere {
		return ChecksBlocked, nil
	}
	if !tie.Owns {
		unknown()
	}
	return result, nil
}

// qualify pays a ready referral if the sharer has a slot in its 30 days.
func (s *Service) qualify(ctx context.Context, ref Referral) error {
	now := s.now().UTC()
	if s.locks != nil {
		key := "referrer:" + ref.ReferrerID
		ok, err := s.locks.Acquire(ctx, key, now, now.Add(lockFor))
		if err != nil || !ok {
			// Someone else is qualifying this sharer's referrals; the next
			// event or sweep tries again.
			return err
		}
		defer func() {
			if err := s.locks.Release(context.WithoutCancel(ctx), key); err != nil {
				s.logger.WarnContext(ctx, "release referral lock", "error", err)
			}
		}()
	}
	recent, err := s.referrals.QualifiedSince(ctx, ref.ReferrerID, now.Add(-referrerWindow))
	if err != nil {
		return err
	}
	if recent >= s.rules.MaxPer30Days {
		return nil
	}
	rewarded, err := s.referrals.Rewarded(ctx, ref.ReferrerID)
	if err != nil {
		return err
	}
	won, err := s.referrals.Qualify(ctx, ref.ReferredID, now, rewarded < MaxReferralMonths)
	if err != nil {
		return err
	}
	if !won {
		// Another caller qualified it, or it moved; it finishes its own.
		return nil
	}
	ref.State, ref.QualifiedAt, ref.ReferrerRewarded = StateQualified, now, rewarded < MaxReferralMonths
	s.logger.InfoContext(ctx, "referral qualified", "householdId", ref.ReferredID, "referrerId", ref.ReferrerID,
		"referrerRewarded", ref.ReferrerRewarded)
	return s.settle(ctx, ref)
}

// settle pays and announces a qualified referral. Each month is claimed
// before it is added, so it is added at most once; notifications are
// deduplicated by key. Settled is set last, so a cut-off settle is finished
// by the next sweep.
func (s *Service) settle(ctx context.Context, ref Referral) error {
	if s.free == nil {
		return errors.New("plus: no free time store")
	}
	now := s.now().UTC()
	month := FreeMonthDays * 24 * time.Hour
	grant := func(flag, householdID string) error {
		ok, err := s.referrals.Claim(ctx, ref.ReferredID, flag)
		if err != nil || !ok {
			return err
		}
		if _, err := s.free.Extend(ctx, householdID, s.paidUntil(ctx, householdID, now), month); err != nil {
			s.logger.ErrorContext(ctx, "free month claimed but not added", "householdId", householdID, "referral", ref.ID, "error", err)
			return err
		}
		return nil
	}
	if err := grant(FlagReferredGranted, ref.ReferredID); err != nil {
		return err
	}
	if ref.ReferrerRewarded {
		if err := grant(FlagReferrerGranted, ref.ReferrerID); err != nil {
			return err
		}
	}
	if err := s.notifyQualified(ctx, ref); err != nil {
		return err
	}
	_, err := s.referrals.Claim(ctx, ref.ReferredID, FlagSettled)
	return err
}

// Notification words.
const (
	ReferredTitle = "Your free month of Plus is on"
	ReferrerTitle = "Your invite counted"
)

// ReferredBody is the new household's notification.
func ReferredBody(referrerName string) string {
	return "Your free month of Plus is on. Thanks for joining from " + possessive(referrerName) + " invite."
}

// ReferrerBody is the sharer's notification; atCap when it already has
// MaxReferralMonths.
func ReferrerBody(referredName string, atCap bool) string {
	if atCap {
		return nameOr(referredName, "A household") + " joined from your invite. You've already earned the most free months."
	}
	return nameOr(referredName, "A household") + " joined from your invite. You earned a free month of Plus."
}

func nameOr(name, fallback string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return fallback
}

func possessive(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return "a friend's"
	}
	return n + "'s"
}

func (s *Service) householdName(ctx context.Context, id string) string {
	if s.households == nil {
		return ""
	}
	h, err := s.households.GetHousehold(ctx, id)
	if err != nil {
		return ""
	}
	return h.Name
}

func (s *Service) notifyQualified(ctx context.Context, ref Referral) error {
	if s.notifier == nil {
		return nil
	}
	referrerName, referredName := s.householdName(ctx, ref.ReferrerID), s.householdName(ctx, ref.ReferredID)
	referred := notifications.New{
		HouseholdID: ref.ReferredID, Type: notifications.TypePlusReferral,
		Title: ReferredTitle, Body: ReferredBody(referrerName),
		Subject:   notifications.Subject{Kind: notifications.SubjectPlus},
		DedupeKey: "plus.referral:" + ref.ReferredID + ":referred", Banner: true,
	}
	referrer := notifications.New{
		HouseholdID: ref.ReferrerID, Type: notifications.TypePlusReferral,
		Title: ReferrerTitle, Body: ReferrerBody(referredName, !ref.ReferrerRewarded),
		Subject:   notifications.Subject{Kind: notifications.SubjectPlus},
		DedupeKey: "plus.referral:" + ref.ReferredID + ":referrer", Banner: true,
	}
	for _, n := range []notifications.New{referred, referrer} {
		if _, _, err := s.notifier.Create(ctx, n); err != nil {
			return fmt.Errorf("notify referral: %w", err)
		}
	}
	return nil
}
