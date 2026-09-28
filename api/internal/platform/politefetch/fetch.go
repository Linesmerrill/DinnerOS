// Package politefetch makes conservative HTTP requests to a site DinnerOS
// does not own: one request at a time, a minimum interval plus jitter between
// requests, an honest User-Agent, Retry-After honoured, and a hard stop when
// the site refuses us.
//
// It is the same policy as the meal-kit importer's fetcher
// (internal/mealkit/fetch.go, docs/meal-kit-import.md#rate-limit-policy),
// shaped for callers that need the status code of a non-2xx answer: a product
// page's 404 is an answer ("gone"), not a failure. The meal-kit fetcher keeps
// its own error types, which its job queue depends on.
package politefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Defaults. Callers set their own interval; these are the floor the policy
// promises.
const (
	// DefaultMinInterval is the gap between two requests to one site.
	DefaultMinInterval = 2500 * time.Millisecond
	// DefaultJitter is the fraction of MinInterval added at random.
	DefaultJitter = 0.4
	// DefaultRetryAfterMax caps how long a Retry-After is honoured for.
	DefaultRetryAfterMax = 60 * time.Second
	// DefaultMaxBytes bounds any single response body read.
	DefaultMaxBytes = 8 << 20
)

// ErrBlocked means the site refused us (401 or 403). The caller stops its run
// and backs off; it never retries around a refusal.
var ErrBlocked = errors.New("politefetch: the site refused the request")

// ErrServer means the site kept answering with a server error (5xx).
var ErrServer = errors.New("politefetch: the site kept failing")

// ErrThrottled means the site asked us to slow down (429) more times than the
// fetcher's attempts allow. The caller stops its run and backs off.
var ErrThrottled = errors.New("politefetch: the site asked us to slow down")

// Response is an answered request: any status that is not a refusal, a
// throttle, or a server error that outlasted the retries.
type Response struct {
	StatusCode int
	// FinalURL is the URL after redirects. Callers check it against their own
	// allow-list before trusting the body.
	FinalURL string
	Header   http.Header
	Body     []byte
	// RetryAfter is the site's Retry-After on a 429, capped, or 0.
	RetryAfter time.Duration
}

// Fetcher is safe for concurrent use: requests are serialized, so however
// many callers share one Fetcher, the site sees one conversation.
type Fetcher struct {
	Client      *http.Client
	UserAgent   string
	MinInterval time.Duration
	Jitter      float64
	// Attempts bounds tries per request for network errors, 5xx, and 429.
	// 1 means no retries.
	Attempts int
	MaxBytes int64
	// Now, Sleep, and Random are injected so tests run instantly and
	// deterministically.
	Now    func() time.Time
	Sleep  func(context.Context, time.Duration) error
	Random func() float64

	mu       sync.Mutex
	last     time.Time
	requests int
}

// Requests is how many HTTP requests this Fetcher has made.
func (f *Fetcher) Requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

// Do performs one request politely.
//
// It returns ErrBlocked on 401/403 and ErrThrottled when 429 outlasts the
// attempts (the returned Response then carries the capped Retry-After). A
// network error or 5xx is retried with exponential backoff and then returned
// as an error. Every other status, 404 included, is returned as a Response.
func (f *Fetcher) Do(ctx context.Context, req *http.Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	attempts := max(f.Attempts, 1)
	var lastErr error
	var throttled Response
	for attempt := 1; attempt <= attempts; attempt++ {
		wait := f.pause()
		if attempt > 1 {
			wait += f.MinInterval * time.Duration(1<<(attempt-1))
		}
		if err := f.sleep(ctx, wait); err != nil {
			return Response{}, err
		}

		clone := req.Clone(ctx)
		clone.Header.Set("User-Agent", f.UserAgent)
		f.last = f.now()
		f.requests++
		resp, err := f.client().Do(clone)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes()))
		_ = resp.Body.Close()
		final := clone.URL.String()
		if resp.Request != nil && resp.Request.URL != nil {
			final = resp.Request.URL.String()
		}

		switch {
		case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusUnauthorized:
			return Response{StatusCode: resp.StatusCode, FinalURL: final}, ErrBlocked
		case resp.StatusCode == http.StatusTooManyRequests:
			d, _ := RetryAfter(resp.Header.Get("Retry-After"), f.now())
			throttled = Response{StatusCode: resp.StatusCode, FinalURL: final, RetryAfter: d}
			lastErr = ErrThrottled
			if attempt < attempts && d > 0 {
				if err := f.sleep(ctx, d); err != nil {
					return Response{}, err
				}
			}
			continue
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("%w: HTTP %d", ErrServer, resp.StatusCode)
			continue
		case readErr != nil:
			lastErr = fmt.Errorf("reading the response failed: %w", readErr)
			continue
		}
		return Response{StatusCode: resp.StatusCode, FinalURL: final, Header: resp.Header, Body: body}, nil
	}
	if errors.Is(lastErr, ErrThrottled) {
		return throttled, ErrThrottled
	}
	return Response{}, fmt.Errorf("gave up after %d attempts: %w", attempts, lastErr)
}

// pause is how long to wait before the next request so the minimum interval
// holds, plus jitter.
func (f *Fetcher) pause() time.Duration {
	interval := f.MinInterval
	if interval <= 0 {
		return 0
	}
	jitter := time.Duration(float64(interval) * f.Jitter * f.random())
	if f.last.IsZero() {
		return jitter
	}
	if remaining := interval - f.now().Sub(f.last); remaining > 0 {
		return remaining + jitter
	}
	return jitter
}

func (f *Fetcher) sleep(ctx context.Context, d time.Duration) error {
	if f.Sleep != nil {
		if d <= 0 {
			return ctx.Err()
		}
		return f.Sleep(ctx, d)
	}
	return SleepContext(ctx, d)
}

func (f *Fetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Fetcher) random() float64 {
	if f.Random != nil {
		return f.Random()
	}
	return rand.Float64()
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return http.DefaultClient
}

func (f *Fetcher) maxBytes() int64 {
	if f.MaxBytes > 0 {
		return f.MaxBytes
	}
	return DefaultMaxBytes
}

// RetryAfter reads a Retry-After header in either of its forms, capped at
// DefaultRetryAfterMax.
func RetryAfter(v string, now time.Time) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return min(time.Duration(secs)*time.Second, DefaultRetryAfterMax), true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return min(d, DefaultRetryAfterMax), true
		}
	}
	return 0, false
}

// SleepContext waits for d or until ctx is done.
func SleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
