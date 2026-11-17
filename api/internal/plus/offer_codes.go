package plus

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
)

// The invite free month as an Apple offer code (decision 755). A referral
// that qualifies no longer writes a date into plus_free_time. Each household
// it pays gets a FreeMonth, and each FreeMonth takes one unused one-time-use
// offer code from a pool staff import from App Store Connect. The household
// redeems the code in Apple's sheet, which starts DinnerOS Plus with the
// first month free, so Apple and RevenueCat see a real subscription. With
// the pool empty the month is owed, an alert goes to REPORT_ALERT_EMAIL once,
// and the next import hands codes to owed months.
//
// Codes are secrets: never logged, never in a list or stock answer. Only
// the household's own reveal call returns its code.

// FreeMonthState is where one earned month is.
type FreeMonthState string

// Free month states.
const (
	// MonthOwed is waiting for a code: the pool was empty.
	MonthOwed FreeMonthState = "owed"
	// MonthReady has a code the household can redeem.
	MonthReady FreeMonthState = "ready"
	// MonthRedeemed is a code RevenueCat says went through Apple.
	MonthRedeemed FreeMonthState = "redeemed"
)

// Who a month is for in its referral.
const (
	RoleReferred = "referred"
	RoleReferrer = "referrer"
)

// FreeMonth is one month a household earned from a referral.
type FreeMonth struct {
	ID          string
	HouseholdID string
	ReferralID  string
	// Role is RoleReferred (used a code) or RoleReferrer (shared it).
	Role     string
	EarnedAt time.Time
	State    FreeMonthState
	// GivenAt, CodeExpiresAt are set once a code is given; RevealedAt once
	// the household asked for it; RedeemedAt once it went through.
	GivenAt       time.Time
	CodeExpiresAt time.Time
	RevealedAt    time.Time
	RedeemedAt    time.Time
}

// MonthKey is the one key a month is stored under, so settling a referral
// twice records it once.
func MonthKey(referralID, role string) string { return referralID + ":" + role }

// OfferBatch names where imported codes came from.
type OfferBatch struct {
	// Offer is the offer code's reference name in App Store Connect.
	Offer string
	// Batch tells one download apart from the next.
	Batch string
	// ExpiresAt is when Apple stops taking the batch's codes.
	ExpiresAt time.Time
}

// ImportResult says what an import added.
type ImportResult struct {
	Added      int `json:"added"`
	Duplicates int `json:"duplicates"`
	// Skipped counts lines that weren't a code.
	Skipped int `json:"skipped"`
	// Filled counts owed months that got a code from it.
	Filled int `json:"filled"`
}

// BatchStock counts one batch.
type BatchStock struct {
	Offer     string    `json:"offer"`
	Batch     string    `json:"batch"`
	ExpiresAt time.Time `json:"expiresAt"`
	Available int       `json:"available"`
	Given     int       `json:"given"`
	Redeemed  int       `json:"redeemed"`
}

// OfferStock counts the pool without naming a code.
type OfferStock struct {
	// Available codes can still be given: unused and not expiring within
	// CodeMinLife.
	Available int `json:"available"`
	// Given is codes handed to a household, Revealed those it asked for,
	// Redeemed those that went through.
	Given    int `json:"given"`
	Revealed int `json:"revealed"`
	Redeemed int `json:"redeemed"`
	// Expired counts unused codes past (or too near) their end.
	Expired int `json:"expired"`
	// Owed counts months waiting for a code.
	Owed    int          `json:"owed"`
	Batches []BatchStock `json:"batches"`
}

// CodeMinLife is the least time a code must have left to be given, so a
// household has time to redeem it.
const CodeMinLife = 14 * 24 * time.Hour

// OfferCodeStore keeps the code pool and the months it pays.
type OfferCodeStore interface {
	// AddMonth records m as owed under MonthKey(m.ReferralID, m.Role), once:
	// a second call returns the stored month.
	AddMonth(ctx context.Context, m FreeMonth) (FreeMonth, error)
	// Months lists a household's months, oldest first.
	Months(ctx context.Context, householdID string) ([]FreeMonth, error)
	// Owed lists up to limit owed months, oldest first.
	Owed(ctx context.Context, limit int) ([]FreeMonth, error)
	// Give hands monthID one unused code that lasts past notBefore, in one
	// atomic update, and reports whether it did. A month never gets two.
	Give(ctx context.Context, monthID string, now, notBefore time.Time) (bool, error)
	// ExpiredGiven lists ready months whose code ended before now.
	ExpiredGiven(ctx context.Context, now time.Time, limit int) ([]FreeMonth, error)
	// Requeue retires a ready month's expired code and makes it owed again.
	Requeue(ctx context.Context, monthID string, now time.Time) error
	// Reveal marks the household's ready month revealed and returns its code.
	Reveal(ctx context.Context, householdID, monthID string, now time.Time) (string, FreeMonth, error)
	// MarkRedeemed marks the household's oldest revealed ready month
	// redeemed and forgets its code, and reports whether there was one.
	MarkRedeemed(ctx context.Context, householdID string, at time.Time) (bool, error)
	// Import adds codes to the pool, skipping any it has ever had.
	Import(ctx context.Context, codes []string, b OfferBatch, now time.Time) (ImportResult, error)
	Stock(ctx context.Context, now time.Time) (OfferStock, error)
	// ClaimShortageAlert reports whether this caller sends the one alert for
	// the pool running dry since the last import.
	ClaimShortageAlert(ctx context.Context, now time.Time) (bool, error)
	PurgeHousehold(ctx context.Context, householdID string) error
}

// Alerter emails the people who run DinnerOS. *invitations.ResendProvider
// implements it.
type Alerter interface {
	SendPlain(ctx context.Context, to, subject, text string) error
}

// OfferCodeOptions turns on offer code free months.
type OfferCodeOptions struct {
	Store OfferCodeStore
	// Alerts and AlertTo email an empty pool (REPORT_ALERT_EMAIL); nil or
	// empty sends none.
	Alerts  Alerter
	AlertTo string
	// AppStoreID is the app's Apple ID (APP_STORE_APP_ID), for the redeem
	// link that fills in the code; empty sends no link.
	AppStoreID string
}

// WithOfferCodes pays qualified referrals in offer codes. It returns s.
func (s *Service) WithOfferCodes(o OfferCodeOptions) *Service {
	s.offerCodes, s.alerts, s.alertTo, s.appStoreID = o.Store, o.Alerts, o.AlertTo, strings.TrimSpace(o.AppStoreID)
	return s
}

// Free month errors, each with a message a person can read.
var (
	ErrFreeMonthNotFound = errors.New("plus: free month not found")
	ErrFreeMonthOwed     = errors.New("plus: free month has no code yet")
	ErrFreeMonthRedeemed = errors.New("plus: free month already redeemed")
)

// Free month words (decision 755).
const (
	FreeMonthReadyTitle    = "Your free month is ready"
	FreeMonthReadyText     = "Redeem it to start DinnerOS Plus with the first month free."
	FreeMonthOwedTitle     = "Your free month is on the way"
	FreeMonthOwedText      = "We'll let you know when it's ready."
	FreeMonthRedeemedTitle = "Free month redeemed"
	FreeMonthRedeemedText  = "Your free month went through Apple."
	FreeMonthRedeemText    = "Redeem"
	// FreeMonthTerms sits above Redeem: redeeming starts a subscription.
	FreeMonthTerms = "Redeeming starts DinnerOS Plus with the first month free. After that it renews at the normal price unless you cancel in your Apple account settings."
	// FreeMonthCopiedText is shown when the code goes to the clipboard.
	FreeMonthCopiedText = "Your code is copied. Paste it in Apple's screen."
	// FreeMonthNotFoundText and the others answer the reveal call.
	FreeMonthNotFoundText = "We couldn't find that free month."
	FreeMonthOwedError    = FreeMonthOwedTitle + ". " + FreeMonthOwedText
	FreeMonthRedeemedErr  = "You already redeemed this free month."
)

// RevealedCode is a month's code for its own household.
type RevealedCode struct {
	Code string
	// RedeemURL opens Apple's redeem page with the code filled in; empty
	// without APP_STORE_APP_ID.
	RedeemURL string
}

// RedeemURL is Apple's redeem link for code in the app with Apple ID appID.
func RedeemURL(appID, code string) string {
	if appID == "" {
		return ""
	}
	q := url.Values{"ctx": {"offercodes"}, "id": {appID}, "code": {code}}
	return "https://apps.apple.com/redeem?" + q.Encode()
}

// grantMonth records one earned month and tries to give it a code now. It
// reports whether the month has a code. Only recording can fail the settle:
// a month without a code is owed and filled later.
func (s *Service) grantMonth(ctx context.Context, ref Referral, role, householdID string, now time.Time) (bool, error) {
	m, err := s.offerCodes.AddMonth(ctx, FreeMonth{
		HouseholdID: householdID, ReferralID: ref.ID, Role: role, EarnedAt: now, State: MonthOwed,
	})
	if err != nil {
		return false, fmt.Errorf("record free month: %w", err)
	}
	if m.State != MonthOwed {
		return true, nil
	}
	given, err := s.offerCodes.Give(ctx, m.ID, now, now.Add(CodeMinLife))
	if err != nil {
		s.logger.ErrorContext(ctx, "give free month code", "householdId", householdID, "error", err)
		return false, nil
	}
	if !given {
		s.logger.WarnContext(ctx, "no free month code left; month owed", "householdId", householdID)
		s.alertShortage(ctx, now)
	}
	return given, nil
}

// alertShortage emails once per run dry, until the next import.
func (s *Service) alertShortage(ctx context.Context, now time.Time) {
	if s.alerts == nil || s.alertTo == "" {
		return
	}
	ok, err := s.offerCodes.ClaimShortageAlert(ctx, now)
	if err != nil || !ok {
		if err != nil {
			s.logger.ErrorContext(ctx, "claim free month code alert", "error", err)
		}
		return
	}
	owed := 0
	if st, err := s.offerCodes.Stock(ctx, now); err == nil {
		owed = st.Owed
	}
	text := fmt.Sprintf("DinnerOS is out of free month codes. Free months waiting for a code: %d.\n\n"+
		"Make a new batch of one-time-use codes in App Store Connect and import it (docs/deployment.md, Free month codes). "+
		"Waiting months get their codes as soon as the import finishes.", owed)
	if err := s.alerts.SendPlain(context.WithoutCancel(ctx), s.alertTo, "DinnerOS: out of free month codes", text); err != nil {
		s.logger.ErrorContext(ctx, "send free month code alert", "error", err)
	}
}

// FillOwed gives codes to owed months, oldest first, and to months whose
// code ran out unredeemed. It notifies each household that got one and
// returns how many did. It runs after an import and with each sweep.
func (s *Service) FillOwed(ctx context.Context) (int, error) {
	if s.offerCodes == nil {
		return 0, nil
	}
	now := s.now().UTC()
	if err := s.requeueExpired(ctx, now); err != nil {
		return 0, err
	}
	filled := 0
	for {
		owed, err := s.offerCodes.Owed(ctx, sweepPage)
		if err != nil {
			return filled, err
		}
		progress := false
		for _, m := range owed {
			given, err := s.offerCodes.Give(ctx, m.ID, now, now.Add(CodeMinLife))
			if err != nil {
				return filled, err
			}
			if !given {
				// The pool is empty: the rest wait for the next import.
				s.alertShortage(ctx, now)
				return filled, nil
			}
			filled++
			progress = true
			s.notifyReady(ctx, m)
		}
		if len(owed) < sweepPage || !progress || ctx.Err() != nil {
			return filled, ctx.Err()
		}
	}
}

// requeueExpired makes a month owed again when its code ended before the
// household redeemed it. A revealed code may have gone through without
// RevenueCat saying so; such a month is requeued only if the household has
// never had a subscription.
func (s *Service) requeueExpired(ctx context.Context, now time.Time) error {
	months, err := s.offerCodes.ExpiredGiven(ctx, now, sweepPage)
	if err != nil {
		return err
	}
	for _, m := range months {
		if !m.RevealedAt.IsZero() {
			if _, err := s.store.Get(ctx, m.HouseholdID); err == nil {
				// Seen and subscribed since: count it as used, so it stops
				// coming back here.
				if _, err := s.offerCodes.MarkRedeemed(ctx, m.HouseholdID, now); err != nil {
					return err
				}
				continue
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		if err := s.offerCodes.Requeue(ctx, m.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// Notification words for a month that waited for a code.
const (
	FreeMonthReadyBody = "Redeem it in DinnerOS to start Plus with the first month free."
)

func (s *Service) notifyReady(ctx context.Context, m FreeMonth) {
	if s.notifier == nil {
		return
	}
	_, _, err := s.notifier.Create(ctx, notifications.New{
		HouseholdID: m.HouseholdID, Type: notifications.TypePlusReferral,
		Title: FreeMonthReadyTitle, Body: FreeMonthReadyBody,
		Subject:   notifications.Subject{Kind: notifications.SubjectPlus},
		DedupeKey: "plus.freemonth:" + m.ID + ":ready", Banner: true,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "notify free month ready", "householdId", m.HouseholdID, "error", err)
	}
}

// FreeMonths lists a household's months as it should see them. A revealed
// month shows as redeemed once the household's subscription started after
// the reveal, even if RevenueCat didn't name the offer code.
func (s *Service) FreeMonths(ctx context.Context, householdID string) ([]FreeMonth, error) {
	if s.offerCodes == nil {
		return nil, nil
	}
	months, err := s.offerCodes.Months(ctx, householdID)
	if err != nil || len(months) == 0 {
		return months, err
	}
	e, err := s.store.Get(ctx, householdID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	now := s.now()
	for i, m := range months {
		if m.State == MonthReady && !m.RevealedAt.IsZero() && err == nil && e.Active(now) && !e.Since.Before(m.RevealedAt) {
			months[i].State = MonthRedeemed
		}
	}
	return months, nil
}

// RevealFreeMonth returns the code of one of the household's ready months.
func (s *Service) RevealFreeMonth(ctx context.Context, householdID, monthID string) (RevealedCode, error) {
	if s.offerCodes == nil {
		return RevealedCode{}, ErrFreeMonthNotFound
	}
	months, err := s.FreeMonths(ctx, householdID)
	if err != nil {
		return RevealedCode{}, err
	}
	var month *FreeMonth
	for i := range months {
		if months[i].ID == monthID {
			month = &months[i]
		}
	}
	switch {
	case month == nil:
		return RevealedCode{}, ErrFreeMonthNotFound
	case month.State == MonthOwed:
		return RevealedCode{}, ErrFreeMonthOwed
	case month.State == MonthRedeemed:
		return RevealedCode{}, ErrFreeMonthRedeemed
	}
	code, _, err := s.offerCodes.Reveal(ctx, householdID, monthID, s.now().UTC())
	if err != nil {
		return RevealedCode{}, err
	}
	// The household's ID is fine to log; the code never is.
	s.logger.InfoContext(ctx, "free month code revealed", "householdId", householdID, "monthId", monthID)
	return RevealedCode{Code: code, RedeemURL: RedeemURL(s.appStoreID, code)}, nil
}

// ImportOfferCodes adds a batch to the pool, then gives owed months codes.
func (s *Service) ImportOfferCodes(ctx context.Context, codes []string, b OfferBatch) (ImportResult, error) {
	if s.offerCodes == nil {
		return ImportResult{}, ErrReferralsOff
	}
	res, err := s.offerCodes.Import(ctx, codes, b, s.now().UTC())
	if err != nil {
		return res, err
	}
	// The codes are in; a failed fill is finished by the next sweep.
	if res.Filled, err = s.FillOwed(ctx); err != nil {
		s.logger.ErrorContext(ctx, "give imported codes to waiting months", "error", err)
	}
	return res, nil
}

// OfferStock counts the pool.
func (s *Service) OfferStock(ctx context.Context) (OfferStock, error) {
	if s.offerCodes == nil {
		return OfferStock{}, ErrReferralsOff
	}
	return s.offerCodes.Stock(ctx, s.now().UTC())
}

// markOfferRedeemed records that the household redeemed a code, when
// RevenueCat names an offer code on a purchase.
func (s *Service) markOfferRedeemed(ctx context.Context, householdID string, at time.Time) error {
	if s.offerCodes == nil {
		return nil
	}
	_, err := s.offerCodes.MarkRedeemed(ctx, householdID, at)
	return err
}

// ParseOfferCodes reads Apple's download: one code per line, the code the
// first field of a comma-separated line. Blank lines, a header, and anything
// that isn't a code are skipped and counted. Codes come back in capitals,
// each once.
func ParseOfferCodes(text string) (codes []string, skipped int) {
	seen := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if line == "" {
			continue
		}
		field, _, _ := strings.Cut(line, ",")
		code := strings.ToUpper(strings.Trim(strings.TrimSpace(field), `"`))
		if !validOfferCode(code) {
			skipped++
			continue
		}
		if seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	return codes, skipped
}

// validOfferCode allows Apple's code shape: letters and digits, 6 to 64,
// and not a header word.
func validOfferCode(code string) bool {
	if len(code) < 6 || len(code) > 64 {
		return false
	}
	switch code {
	case "CODES", "OFFERCODE", "OFFERCODES", "ONETIMECODE", "ONETIMECODES":
		return false
	}
	for _, r := range code {
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// OfferCodeKey is how a code is matched for dedupe: a hash, so a used
// code's text can be forgotten and still never imported twice.
func OfferCodeKey(code string) string {
	sum := sha256.Sum256([]byte("dinneros offer code\x00" + strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}
