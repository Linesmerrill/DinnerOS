package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/ratelimit"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// Review sign-in (decision 752): App Review signs in to one dedicated account
// with an email and a password given in the review notes. Only the configured
// email works; every other email, and a wrong password, gets the same answer.

// Review sign-in limits. Each client address may try reviewRateBurst times,
// then once a minute. reviewLockAfter wrong tries from one address within
// reviewLockWindow lock it out for reviewLockFor; reviewGlobalLockAfter wrong
// tries from everyone together lock the account for reviewLockFor, so no
// spread of addresses can keep guessing.
const (
	reviewRateBurst       = 5
	reviewRateEvery       = time.Minute
	reviewLockAfter       = 5
	reviewGlobalLockAfter = 50
	reviewLockWindow      = 15 * time.Minute
	reviewLockFor         = 15 * time.Minute
	maxReviewPassword     = 256
	reviewDisplayName     = "App Review"
)

// Messages people see. They're plain and never say which half was wrong.
const (
	reviewFailedMessage = "That email and password didn’t work."
	reviewLockedMessage = "Too many tries. Wait 15 minutes and try again."
	reviewMissingFields = "Enter your email and password."
)

// ReviewSeeder sets the review account up after it signs in: a household with
// recipes of our own the first time, nothing after that. An error fails the
// sign-in, so the next try seeds again.
type ReviewSeeder interface {
	ReviewAccountSignedIn(ctx context.Context, userID string) error
}

// ReviewOptions configures review sign-in.
type ReviewOptions struct {
	// Email and Password are REVIEW_ACCOUNT_EMAIL and REVIEW_ACCOUNT_PASSWORD.
	// Either empty turns review sign-in off.
	Email    string
	Password string
	Seeder   ReviewSeeder
	// Now is the clock. Default time.Now.
	Now func() time.Time
	// DevAccounts turns on email accounts a local development server's UI
	// tests register through POST /auth/dev/email-account, so a screenshot
	// can show a person signed in with email without the one review account.
	// Only for a development server with developer sign-in on.
	DevAccounts bool
	// RateBurst is how many tries one address may make at once. Default
	// reviewRateBurst. Local QA raises it: every simulator is localhost.
	RateBurst int
}

// ReviewSignIn checks the review account's email and password and remembers
// recent wrong tries. State is per process, like ratelimit.
type ReviewSignIn struct {
	email   string
	pwHash  [sha256.Size]byte
	seeder  ReviewSeeder
	now     func() time.Time
	limiter *ratelimit.Limiter
	perIP   *failureLock
	global  *failureLock
	dev     *devAccounts
}

// devAccounts are the email accounts UI tests register on a development
// server, by normalized email. They live in memory: each test run registers
// its own again.
type devAccounts struct {
	mu      sync.Mutex
	byEmail map[string]devAccount
}

type devAccount struct {
	pwHash      [sha256.Size]byte
	displayName string
}

// devAccountsMax bounds memory; past it, registering another is refused.
const devAccountsMax = 10_000

// NewReviewSignIn returns review sign-in, or nil when it isn't configured.
func NewReviewSignIn(opts ReviewOptions) *ReviewSignIn {
	email := normalizeReviewEmail(opts.Email)
	if email == "" || opts.Password == "" {
		// The review account needs both halves.
		email = ""
	}
	if email == "" && !opts.DevAccounts {
		return nil
	}
	burst := opts.RateBurst
	if burst <= 0 {
		burst = reviewRateBurst
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	var dev *devAccounts
	if opts.DevAccounts {
		dev = &devAccounts{byEmail: map[string]devAccount{}}
	}
	return &ReviewSignIn{
		dev:    dev,
		email:  email,
		pwHash: sha256.Sum256([]byte(opts.Password)),
		seeder: opts.Seeder,
		now:    now,
		limiter: ratelimit.New(ratelimit.Options{
			Burst: burst, Every: reviewRateEvery, Now: now, Message: "Too many tries. Wait a minute and try again.",
		}),
		perIP:  newFailureLock(reviewLockAfter, reviewLockWindow, reviewLockFor),
		global: newFailureLock(reviewGlobalLockAfter, reviewLockWindow, reviewLockFor),
	}
}

// Enabled reports whether email sign-in is on: for the review account, or for
// development accounts. A nil ReviewSignIn is off.
func (r *ReviewSignIn) Enabled() bool { return r != nil }

func (r *ReviewSignIn) devAccountsOn() bool { return r != nil && r.dev != nil }

// account returns the email and display name of the account an email and
// password sign in to, if any: the review account, else (development only) a
// registered development account.
func (r *ReviewSignIn) account(email, password string) (string, string, bool) {
	if r.email != "" && r.matches(email, password) {
		return r.email, reviewDisplayName, true
	}
	if r.dev == nil {
		return "", "", false
	}
	given := normalizeReviewEmail(email)
	r.dev.mu.Lock()
	acct, ok := r.dev.byEmail[given]
	r.dev.mu.Unlock()
	got := sha256.Sum256([]byte(password))
	if !ok || subtle.ConstantTimeCompare(acct.pwHash[:], got[:]) != 1 {
		return "", "", false
	}
	return given, acct.displayName, true
}

var (
	errDevAccountTaken = errors.New("auth: that email is the review account")
	errDevAccountsFull = errors.New("auth: too many development accounts")
)

// register adds or replaces a development account. The review account's
// email can't be taken.
func (r *ReviewSignIn) register(email, password, displayName string) error {
	email = normalizeReviewEmail(email)
	if email == r.email {
		return errDevAccountTaken
	}
	r.dev.mu.Lock()
	defer r.dev.mu.Unlock()
	if _, ok := r.dev.byEmail[email]; !ok && len(r.dev.byEmail) >= devAccountsMax {
		return errDevAccountsFull
	}
	r.dev.byEmail[email] = devAccount{pwHash: sha256.Sum256([]byte(password)), displayName: displayName}
	return nil
}

// ReviewSubject is the stable subject the review account's identity is
// stored under: derived from the email, so the email itself isn't the key.
func ReviewSubject(email string) string {
	sum := sha256.Sum256([]byte(normalizeReviewEmail(email)))
	return "review-" + hex.EncodeToString(sum[:16])
}

func normalizeReviewEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// matches compares in constant time. Both halves are always compared, so the
// time taken doesn't say whether the email was the right one.
func (r *ReviewSignIn) matches(email, password string) bool {
	given := normalizeReviewEmail(email)
	wantEmail := sha256.Sum256([]byte(r.email))
	gotEmail := sha256.Sum256([]byte(given))
	gotPW := sha256.Sum256([]byte(password))
	emailOK := subtle.ConstantTimeCompare(wantEmail[:], gotEmail[:])
	pwOK := subtle.ConstantTimeCompare(r.pwHash[:], gotPW[:])
	return emailOK&pwOK == 1
}

// SignInReview signs the review account in through the same session code as
// every other provider, after the seeder has set it up.
func (s *Service) SignInReview(ctx context.Context, email, displayName string, seeder ReviewSeeder) (SignInResult, error) {
	identity := users.VerifiedIdentity{
		Provider:      users.ProviderReview,
		Subject:       ReviewSubject(email),
		Email:         normalizeReviewEmail(email),
		EmailVerified: true,
		DisplayName:   displayName,
	}
	user, created, err := s.users.FindOrCreateByIdentity(ctx, identity)
	if err != nil {
		return SignInResult{}, fmt.Errorf("auth: find or create review user: %w", err)
	}
	if seeder != nil {
		if err := seeder.ReviewAccountSignedIn(ctx, user.ID); err != nil {
			return SignInResult{}, fmt.Errorf("auth: set up review account: %w", err)
		}
	}
	pair, err := s.tokens.IssueForNewSignIn(ctx, user.ID)
	if err != nil {
		return SignInResult{}, err
	}
	s.logger.InfoContext(ctx, "user signed in", "provider", string(users.ProviderReview), "userId", user.ID, "isNewUser", created)
	return SignInResult{Tokens: pair, User: user, IsNewUser: created}, nil
}

// --- HTTP -----------------------------------------------------------------------

// OptionsResponse is GET /auth/options: what the sign-in screen offers,
// read before anyone is signed in.
type OptionsResponse struct {
	// ReviewSignIn shows "Sign in with email" for the App Review account.
	ReviewSignIn bool `json:"reviewSignIn"`
}

type reviewSignInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) options(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, OptionsResponse{ReviewSignIn: h.opts.Review.Enabled()})
}

func (h *Handler) signInReview(w http.ResponseWriter, r *http.Request) {
	review := h.opts.Review
	ip := ratelimit.ClientIP(r)
	if !review.limiter.Allow(ip) {
		w.Header().Set("Retry-After", "60")
		httpx.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "Too many tries. Wait a minute and try again.")
		return
	}
	now := review.now()
	if review.perIP.locked(ip, now) || review.global.locked("", now) {
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", reviewLockedMessage)
		return
	}
	var req reviewSignInRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Email) == "" || req.Password == "" {
		validationFailed(w, r, reviewMissingFields)
		return
	}
	var (
		email, displayName string
		ok                 bool
	)
	if len(req.Email) <= maxEmailLength && len(req.Password) <= maxReviewPassword {
		email, displayName, ok = review.account(req.Email, req.Password)
	}
	if !ok {
		review.perIP.fail(ip, now)
		review.global.fail("", now)
		// Neither the email nor the password is logged.
		h.logger.InfoContext(r.Context(), "review sign-in rejected")
		httpx.WriteError(w, r, http.StatusUnauthorized, "sign_in_failed", reviewFailedMessage)
		return
	}
	review.perIP.clear(ip)
	res, err := h.opts.Service.SignInReview(r.Context(), email, displayName, review.seeder)
	h.writeSignIn(w, r, users.ProviderReview, res, err)
}

// --- Development email accounts -------------------------------------------------

type devEmailAccountRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

// devAccountDomain reports whether an email is on a domain kept for examples
// and tests (RFC 2606), so no real person's address is ever used.
func devAccountDomain(email string) bool {
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return false
	}
	switch domain := email[at+1:]; domain {
	case "example.com", "example.org", "example.net":
		return true
	default:
		return strings.HasSuffix(domain, ".test")
	}
}

// registerDevEmailAccount is POST /auth/dev/email-account, on a development
// server only: it adds an email and password that /auth/review-sign-in then
// accepts, as a person of its own with the display name given. Registering an
// email again replaces its password; the name is kept from the first sign-in.
func (h *Handler) registerDevEmailAccount(w http.ResponseWriter, r *http.Request) {
	var req devEmailAccountRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	email := normalizeReviewEmail(req.Email)
	displayName := strings.TrimSpace(req.DisplayName)
	switch {
	case email == "" || len(email) > maxEmailLength || !validEmail(email):
		validationFailed(w, r, "email is invalid")
		return
	case !devAccountDomain(email):
		validationFailed(w, r, "email must be on example.com, example.org, example.net or a .test domain")
		return
	case len(req.Password) < 8 || len(req.Password) > maxReviewPassword:
		validationFailed(w, r, "password must be 8 to 256 characters")
		return
	case displayName == "" || utf8.RuneCountInString(displayName) > maxDisplayNameLength:
		validationFailed(w, r, "displayName is required")
		return
	}
	switch err := h.opts.Review.register(email, req.Password, displayName); {
	case errors.Is(err, errDevAccountTaken):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "that email is the review account")
		return
	case err != nil:
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "unavailable", "too many development accounts; restart the server")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Lockout ----------------------------------------------------------------------

// failureLock counts wrong tries per key in a window and locks a key that
// reaches max until lockFor has passed.
type failureLock struct {
	max     int
	window  time.Duration
	lockFor time.Duration

	mu   sync.Mutex
	keys map[string]*failures
}

type failures struct {
	count       int
	since       time.Time
	lockedUntil time.Time
}

// failureLockMaxKeys bounds memory: past it, entries with nothing left to
// remember are dropped.
const failureLockMaxKeys = 10_000

func newFailureLock(maxFailures int, window, lockFor time.Duration) *failureLock {
	return &failureLock{max: maxFailures, window: window, lockFor: lockFor, keys: map[string]*failures{}}
}

func (l *failureLock) locked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.keys[key]
	return ok && now.Before(f.lockedUntil)
}

func (l *failureLock) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.keys) >= failureLockMaxKeys {
		for k, f := range l.keys {
			if now.Sub(f.since) >= l.window && !now.Before(f.lockedUntil) {
				delete(l.keys, k)
			}
		}
	}
	f, ok := l.keys[key]
	if !ok || now.Sub(f.since) >= l.window {
		f = &failures{since: now, lockedUntil: timeOrZero(f)}
		l.keys[key] = f
	}
	f.count++
	if f.count >= l.max {
		f.lockedUntil = now.Add(l.lockFor)
		f.count, f.since = 0, now
	}
}

func (l *failureLock) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.keys, key)
}

func timeOrZero(f *failures) time.Time {
	if f == nil {
		return time.Time{}
	}
	return f.lockedUntil
}
