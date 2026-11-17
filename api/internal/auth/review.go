package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

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
	reviewFailedMessage = "That email and password didn't work."
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
}

// NewReviewSignIn returns review sign-in, or nil when it isn't configured.
func NewReviewSignIn(opts ReviewOptions) *ReviewSignIn {
	email := normalizeReviewEmail(opts.Email)
	if email == "" || opts.Password == "" {
		return nil
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &ReviewSignIn{
		email:  email,
		pwHash: sha256.Sum256([]byte(opts.Password)),
		seeder: opts.Seeder,
		now:    now,
		limiter: ratelimit.New(ratelimit.Options{
			Burst: reviewRateBurst, Every: reviewRateEvery, Now: now, Message: "Too many tries. Wait a minute and try again.",
		}),
		perIP:  newFailureLock(reviewLockAfter, reviewLockWindow, reviewLockFor),
		global: newFailureLock(reviewGlobalLockAfter, reviewLockWindow, reviewLockFor),
	}
}

// Enabled reports whether review sign-in is on. A nil ReviewSignIn is off.
func (r *ReviewSignIn) Enabled() bool { return r != nil }

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
func (s *Service) SignInReview(ctx context.Context, email string, seeder ReviewSeeder) (SignInResult, error) {
	identity := users.VerifiedIdentity{
		Provider:      users.ProviderReview,
		Subject:       ReviewSubject(email),
		Email:         normalizeReviewEmail(email),
		EmailVerified: true,
		DisplayName:   reviewDisplayName,
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
	if len(req.Email) > maxEmailLength || len(req.Password) > maxReviewPassword || !review.matches(req.Email, req.Password) {
		review.perIP.fail(ip, now)
		review.global.fail("", now)
		// Neither the email nor the password is logged.
		h.logger.InfoContext(r.Context(), "review sign-in rejected")
		httpx.WriteError(w, r, http.StatusUnauthorized, "sign_in_failed", reviewFailedMessage)
		return
	}
	review.perIP.clear(ip)
	res, err := h.opts.Service.SignInReview(r.Context(), review.email, review.seeder)
	h.writeSignIn(w, r, users.ProviderReview, res, err)
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
