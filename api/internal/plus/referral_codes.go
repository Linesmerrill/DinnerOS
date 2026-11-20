package plus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Custom and new codes (decision 699): a household can swap its code for a
// new random one or one it picks, like "lines-fam". The new code works at
// once and the old one stops, for good: every code ever used is kept, so no
// one can claim it again. Invites already made with the old code still
// count, since a referral points at the household, not the code.

// Code rules.
const (
	CustomCodeMin = 3
	CustomCodeMax = 20
	// CodeChangeLimit is how many times a household can change its code in
	// any CodeChangeWindow.
	CodeChangeLimit  = 5
	CodeChangeWindow = 30 * 24 * time.Hour
)

// Code change errors. CodeMessage has the words for each.
var (
	ErrCodeInvalid    = errors.New("plus: code has the wrong length or characters")
	ErrCodeHyphenEnds = errors.New("plus: code starts or ends with a hyphen")
	ErrCodeNotAllowed = errors.New("plus: code is reserved or rude")
	ErrCodeTaken      = errors.New("plus: code is taken")
	ErrCodeSame       = errors.New("plus: code is already the household's")
	ErrCodeRetired    = errors.New("plus: code was replaced")
	ErrCodeChangedNow = errors.New("plus: code changed during the request")
	ErrCodeNoChoice   = errors.New("plus: no code given")
	// ErrCodeLimit matches any CodeLimitError.
	ErrCodeLimit = errors.New("plus: code change limit")
)

// CodeLimitError is the sixth change in CodeChangeWindow. Next is when the
// household can change its code again.
type CodeLimitError struct {
	Next time.Time
	// Location is the household's time zone, for the date in the message.
	Location *time.Location
}

func (e *CodeLimitError) Error() string { return ErrCodeLimit.Error() }

// Unwrap lets errors.Is match ErrCodeLimit.
func (e *CodeLimitError) Unwrap() error { return ErrCodeLimit }

// Code change words.
const (
	CodeHelp            = "Use 3 to 20 letters, numbers or hyphens."
	codeHyphenMessage   = "Start and end with a letter or number."
	codeNotAllowedText  = "That code isn’t allowed."
	codeTakenText       = "That code is taken."
	codeSameText        = "That’s already your code."
	codeAvailableText   = "You can use that code."
	codeRetiredText     = "That code was replaced. Ask for the new one."
	codeChangedNowText  = "Your code just changed. Check it and try again."
	codeNoChoiceText    = "Enter a code, or ask for a new random one."
	codeLimitDateLayout = "January 2"
)

// CodeMessage is what the app shows for a code change error.
func CodeMessage(err error) string {
	var limit *CodeLimitError
	switch {
	case errors.As(err, &limit):
		return "You can change your code again on " + inLocation(limit.Next, limit.Location).Format(codeLimitDateLayout) + "."
	case errors.Is(err, ErrCodeInvalid):
		return CodeHelp
	case errors.Is(err, ErrCodeHyphenEnds):
		return codeHyphenMessage
	case errors.Is(err, ErrCodeNotAllowed):
		return codeNotAllowedText
	case errors.Is(err, ErrCodeTaken):
		return codeTakenText
	case errors.Is(err, ErrCodeSame):
		return codeSameText
	case errors.Is(err, ErrCodeChangedNow):
		return codeChangedNowText
	case errors.Is(err, ErrCodeNoChoice):
		return codeNoChoiceText
	}
	return "Something went wrong. Try again."
}

func inLocation(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		return t.UTC()
	}
	return t.In(loc)
}

// reservedCodes can't be picked as they are.
var reservedCodes = map[string]bool{
	"admin": true, "dinneros": true, "support": true, "help": true, "invite": true, "plus": true,
}

// blockedWords can't appear anywhere in a code. Short words that turn up
// inside ordinary ones ("ass" in "class", "pedo" in "torpedo") are left out on purpose.
var blockedWords = []string{
	"asshole", "bastard", "bitch", "bollock", "bullshit", "chink", "cunt", "dildo", "dyke",
	"fag", "fuck", "hitler", "jizz", "kike", "molest", "motherf", "nazi", "nigga",
	"nigger", "paedo", "penis", "piss", "porn", "pussy", "rapist", "retard",
	"shit", "slut", "tranny", "twat", "vagina", "wank", "whore",
}

// rude reports whether key has a blocked word in a part between hyphens or
// in the whole code with the hyphens taken out.
func rude(key string) bool {
	parts := append(strings.Split(key, "-"), strings.ReplaceAll(key, "-", ""))
	for _, p := range parts {
		for _, w := range blockedWords {
			if strings.Contains(p, w) {
				return true
			}
		}
	}
	return false
}

// CodeKey is how codes are compared and kept unique: lowercase letters and
// numbers only. Hyphens, spaces and other dashes drop out, so "Lines-Fam",
// "LINESFAM" and "lines fam" are one code. App builds and the invite page
// from before decision 699 send codes uppercase without hyphens, and still
// find a picked code this way; two look-alike codes can't both exist.
func CodeKey(code string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(code) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidateCustomCode checks a code a household picked and returns it
// lowercase, the way it is stored and shown.
func ValidateCustomCode(s string) (string, error) {
	code := strings.ToLower(strings.TrimSpace(s))
	if len(code) < CustomCodeMin || len(code) > CustomCodeMax {
		return "", ErrCodeInvalid
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return "", ErrCodeInvalid
		}
	}
	if strings.HasPrefix(code, "-") || strings.HasSuffix(code, "-") {
		return "", ErrCodeHyphenEnds
	}
	if reservedCodes[code] || reservedCodes[CodeKey(code)] || rude(code) {
		return "", ErrCodeNotAllowed
	}
	return code, nil
}

// newCleanCode is NewCode without a blocked word.
func newCleanCode() string {
	for {
		if c := NewCode(); !rude(CodeKey(c)) {
			return c
		}
	}
}

// redeemKey is the key of a typed or pasted code, or of the code in an
// invite link.
func redeemKey(s string) string {
	if i := strings.Index(s, "code="); i >= 0 {
		s = s[i+len("code="):]
	}
	if i := strings.IndexAny(s, "?#&"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return CodeKey(s)
}

// owner finds the household whose current code is typed. A replaced code
// is ErrCodeRetired.
func (s *Service) owner(ctx context.Context, typed string) (string, error) {
	key := redeemKey(typed)
	if len(key) < 2 || len(key) > CustomCodeMax {
		return "", ErrCodeNotFound
	}
	return s.referrals.Owner(ctx, key)
}

// CodeChanges is how many code changes a household has left, and when it
// gets another if it has none.
type CodeChanges struct {
	Left int
	Next time.Time
}

// codeChanges counts the changes in the window ending now.
func codeChanges(changes []time.Time, now time.Time) CodeChanges {
	since := now.Add(-CodeChangeWindow)
	var recent []time.Time
	for _, t := range changes {
		if t.After(since) {
			recent = append(recent, t)
		}
	}
	out := CodeChanges{Left: max(0, CodeChangeLimit-len(recent))}
	if out.Left == 0 {
		// The oldest change that still counts drops out of the window first.
		oldest := recent[0]
		for _, t := range recent {
			if t.Before(oldest) {
				oldest = t
			}
		}
		out.Next = oldest.Add(CodeChangeWindow)
	}
	return out
}

// CodeChangesText says how many changes are left, under the code.
func CodeChangesText(c CodeChanges, loc *time.Location) string {
	switch c.Left {
	case 0:
		return "You can change your code again on " + inLocation(c.Next, loc).Format(codeLimitDateLayout) + "."
	case 1:
		return "You can change your code 1 more time in the next 30 days."
	}
	return fmt.Sprintf("You can change your code %d more times in the next 30 days.", c.Left)
}

// location is the household's time zone, or nil.
func (s *Service) location(ctx context.Context, householdID string) *time.Location {
	if s.households == nil {
		return nil
	}
	h, err := s.households.GetHousehold(ctx, householdID)
	if err != nil || h.TimeZone == "" {
		return nil
	}
	loc, err := time.LoadLocation(h.TimeZone)
	if err != nil {
		return nil
	}
	return loc
}

// CodeAvailability says whether householdID could change its code to code,
// and in a few words why not. It only reads; ChangeCode checks again.
func (s *Service) CodeAvailability(ctx context.Context, householdID, code string) (bool, string, error) {
	if !s.EnabledFor(householdID) || s.referrals == nil {
		return false, "", ErrReferralsOff
	}
	picked, err := ValidateCustomCode(code)
	if err != nil {
		return false, CodeMessage(err), nil
	}
	current, err := s.referrals.Code(ctx, householdID, newCleanCode)
	if err != nil {
		return false, "", err
	}
	if CodeKey(current) == CodeKey(picked) {
		return false, CodeMessage(ErrCodeSame), nil
	}
	taken, err := s.referrals.Taken(ctx, picked)
	if err != nil {
		return false, "", err
	}
	if taken {
		return false, CodeMessage(ErrCodeTaken), nil
	}
	return true, codeAvailableText, nil
}

// ChangeCode gives householdID a new code: custom if it isn't empty, else a
// random one. The old code stops working at once and can never be used
// again.
func (s *Service) ChangeCode(ctx context.Context, householdID, custom string, regenerate bool) (ReferralInfo, error) {
	if !s.EnabledFor(householdID) || s.referrals == nil {
		return ReferralInfo{}, ErrReferralsOff
	}
	custom = strings.TrimSpace(custom)
	if custom == "" && !regenerate {
		return ReferralInfo{}, ErrCodeNoChoice
	}
	picked := ""
	if custom != "" {
		var err error
		if picked, err = ValidateCustomCode(custom); err != nil {
			return ReferralInfo{}, err
		}
	}
	current, err := s.referrals.Code(ctx, householdID, newCleanCode)
	if err != nil {
		return ReferralInfo{}, err
	}
	if picked != "" && CodeKey(current) == CodeKey(picked) {
		return ReferralInfo{}, ErrCodeSame
	}
	changes, err := s.referrals.CodeChanges(ctx, householdID)
	if err != nil {
		return ReferralInfo{}, err
	}
	now := s.now().UTC()
	if c := codeChanges(changes, now); c.Left == 0 {
		return ReferralInfo{}, &CodeLimitError{Next: c.Next, Location: s.location(ctx, householdID)}
	}
	if picked != "" {
		// Replace fails on a taken code too; this keeps the common case to
		// one plain answer without writing.
		if taken, err := s.referrals.Taken(ctx, picked); err != nil {
			return ReferralInfo{}, err
		} else if taken {
			return ReferralInfo{}, ErrCodeTaken
		}
		if err := s.referrals.Replace(ctx, householdID, current, picked, now); err != nil {
			return ReferralInfo{}, err
		}
		return s.Referral(ctx, householdID)
	}
	// A random code that happens to be taken is drawn again.
	for range 5 {
		err := s.referrals.Replace(ctx, householdID, current, newCleanCode(), now)
		if errors.Is(err, ErrCodeTaken) {
			continue
		}
		if err != nil {
			return ReferralInfo{}, err
		}
		return s.Referral(ctx, householdID)
	}
	return ReferralInfo{}, errors.New("plus: could not make a new referral code")
}
