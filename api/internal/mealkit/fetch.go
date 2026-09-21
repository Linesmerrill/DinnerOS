package mealkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// Politeness defaults for every request a Source makes
// (docs/architecture.md, the rate-limit policy decision).
const (
	// DefaultUserAgent says who we are and why. It is honest on purpose: no
	// browser impersonation, and a contact address the service can use.
	DefaultUserAgent = "DinnerOS/1.0 (+https://api.tlps.dev; personal meal-kit order history import)"
	// DefaultMinInterval is the floor between two requests to one service.
	// One request at a time, never in parallel.
	DefaultMinInterval = 2500 * time.Millisecond
	// DefaultJitter is the fraction of MinInterval added at random, so runs
	// from different households do not land in lockstep.
	DefaultJitter = 0.4
	// DefaultRequestAttempts is how many times one request is tried before
	// it is given up as transient.
	DefaultRequestAttempts = 4
	// DefaultRetryAfterMax caps how long a Retry-After is honoured for. A
	// longer wait is left to the job's own backoff instead of holding a dyno.
	DefaultRetryAfterMax = 60 * time.Second
	// MaxResponseBytes bounds any single response we read.
	MaxResponseBytes = 8 << 20
)

// Fetcher makes conservative HTTP requests to one meal-kit service: serially,
// with a minimum interval plus jitter, bounded retries with exponential
// backoff on 429 and 5xx, Retry-After honoured, and a hard stop on 403.
//
// It is not safe for concurrent use, which is the point: one Fetcher is one
// polite conversation with one service.
type Fetcher struct {
	Client      *http.Client
	UserAgent   string
	MinInterval time.Duration
	Jitter      float64
	Attempts    int
	// Now, Sleep, and Random are injected so tests run instantly and
	// deterministically.
	Now    func() time.Time
	Sleep  func(context.Context, time.Duration) error
	Random func() float64

	last time.Time
	// requests counts every request made, for the run's log line.
	requests int
}

// NewFetcher returns a Fetcher with the production politeness settings.
func NewFetcher() *Fetcher {
	return &Fetcher{
		Client:      &http.Client{Timeout: 30 * time.Second},
		UserAgent:   DefaultUserAgent,
		MinInterval: DefaultMinInterval,
		Jitter:      DefaultJitter,
		Attempts:    DefaultRequestAttempts,
		Now:         time.Now,
		Sleep:       SleepContext,
		Random:      rand.Float64,
	}
}

// Requests is how many HTTP requests this Fetcher has made.
func (f *Fetcher) Requests() int { return f.requests }

// Response is a fetched body and where it finally came from.
type Response struct {
	Body []byte
	// FinalURL is the URL after redirects. Callers check it against their own
	// allow-list before trusting the body.
	FinalURL string
	Header   http.Header
}

// Do performs one request politely and returns its body.
//
// It returns ErrBlocked on 401 and 403, and a plain error for anything else.
// The body is read under MaxResponseBytes.
func (f *Fetcher) Do(ctx context.Context, req *http.Request) (Response, error) {
	var lastErr error
	attempts := f.Attempts
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		wait := f.pause()
		if attempt > 1 {
			// Exponential backoff on top of the normal interval.
			wait += f.MinInterval * time.Duration(1<<(attempt-1))
		}
		if err := f.sleep(ctx, wait); err != nil {
			return Response{}, err
		}

		clone := req.Clone(ctx)
		clone.Header.Set("User-Agent", f.userAgent())
		f.last = f.now()
		f.requests++
		resp, err := f.client().Do(clone)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
		_ = resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusUnauthorized:
			// Nothing a Source fetches needs a session, so being turned away
			// is the service refusing us, not an expired credential. Either
			// way the run stops rather than retrying around it.
			return Response{}, ErrBlocked
		case resp.StatusCode == http.StatusTooManyRequests:
			if d, ok := retryAfter(resp.Header.Get("Retry-After")); ok {
				if err := f.sleep(ctx, d); err != nil {
					return Response{}, err
				}
			}
			lastErr = fmt.Errorf("the meal-kit service asked us to slow down (HTTP %d)", resp.StatusCode)
			continue
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("the meal-kit service returned HTTP %d", resp.StatusCode)
			continue
		case readErr != nil:
			lastErr = fmt.Errorf("reading the response failed: %w", readErr)
			continue
		case resp.StatusCode != http.StatusOK:
			// A 4xx that is not 401/403/429 will not fix itself.
			return Response{}, &ParseError{
				Subject: "the response",
				Detail:  fmt.Sprintf("the meal-kit service answered HTTP %d", resp.StatusCode),
			}
		}

		final := clone.URL.String()
		if resp.Request != nil && resp.Request.URL != nil {
			final = resp.Request.URL.String()
		}
		return Response{Body: body, FinalURL: final, Header: resp.Header}, nil
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
	elapsed := f.now().Sub(f.last)
	if remaining := interval - elapsed; remaining > 0 {
		return remaining + jitter
	}
	return jitter
}

func (f *Fetcher) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	if f.Sleep != nil {
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

func (f *Fetcher) userAgent() string {
	if f.UserAgent != "" {
		return f.UserAgent
	}
	return DefaultUserAgent
}

// retryAfter reads a Retry-After header in either of its forms, capped at
// DefaultRetryAfterMax.
func retryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return min(time.Duration(secs)*time.Second, DefaultRetryAfterMax), true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
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

// IsRetryable reports whether err is worth another attempt later. A refusal, a
// parse failure, and a canceled job are not.
func IsRetryable(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrBlocked), errors.Is(err, ErrJobGone), errors.Is(err, ErrDisabled):
		return false
	}
	var parse *ParseError
	return !errors.As(err, &parse)
}
