package mealkit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testFetcher is a Fetcher with a clock and sleeper the test drives, so the
// politeness rules are checked without any real waiting.
func testFetcher(t *testing.T) (*Fetcher, *[]time.Duration) {
	t.Helper()
	var slept []time.Duration
	clock := time.Now()
	f := NewFetcher()
	f.MinInterval = time.Second
	f.Jitter = 0.4
	f.Now = func() time.Time { return clock }
	f.Random = func() float64 { return 0.5 }
	f.Sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		clock = clock.Add(d)
		return nil
	}
	return f, &slept
}

func TestFetcherWaitsBetweenRequestsWithJitter(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f, slept := testFetcher(t)
	for range 3 {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		if _, err := f.Do(context.Background(), req); err != nil {
			t.Fatalf("Do() error = %v", err)
		}
	}
	if hits.Load() != 3 {
		t.Fatalf("requests = %d", hits.Load())
	}
	if len(*slept) != 3 {
		t.Fatalf("waits = %v", *slept)
	}
	// The first wait is jitter only; later ones are the interval plus jitter.
	if (*slept)[0] != 200*time.Millisecond {
		t.Errorf("first wait = %v, want jitter only", (*slept)[0])
	}
	for _, d := range (*slept)[1:] {
		if d < f.MinInterval || d > f.MinInterval+time.Duration(float64(f.MinInterval)*f.Jitter) {
			t.Errorf("wait = %v, outside the interval plus jitter", d)
		}
	}
	if f.Requests() != 3 {
		t.Errorf("Requests() = %d", f.Requests())
	}
}

func TestFetcherSendsAnHonestUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f, _ := testFetcher(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	if _, err := f.Do(context.Background(), req); err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if !strings.Contains(got, "DinnerOS") || !strings.Contains(got, "api.tlps.dev") {
		t.Errorf("User-Agent = %q; it should say who we are and how to reach us", got)
	}
	for _, browser := range []string{"Mozilla", "Chrome", "Safari"} {
		if strings.Contains(got, browser) {
			t.Errorf("User-Agent impersonates a browser: %q", got)
		}
	}
}

func TestFetcherBacksOffOn429AndHonoursRetryAfter(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f, slept := testFetcher(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := f.Do(context.Background(), req)
	if err != nil || string(resp.Body) != "ok" {
		t.Fatalf("Do() = %q, %v", resp.Body, err)
	}
	var honoured bool
	for _, d := range *slept {
		if d == 5*time.Second {
			honoured = true
		}
	}
	if !honoured {
		t.Errorf("waits = %v; Retry-After was not honoured", *slept)
	}
}

func TestFetcherRetries5xxAndGivesUpCleanly(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	f, _ := testFetcher(t)
	f.Attempts = 3
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	if _, err := f.Do(context.Background(), req); err == nil {
		t.Fatal("Do() succeeded against a failing server")
	} else if !IsRetryable(err) {
		t.Errorf("a 5xx should stay retryable, got %v", err)
	}
	if hits.Load() != 3 {
		t.Errorf("attempts = %d, want 3", hits.Load())
	}
}

// Nothing a Source fetches needs a session, so being turned away is a refusal
// whichever status it comes as, and neither is retried.
func TestFetcherStopsOnARefusal(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusUnauthorized} {
		want := ErrBlocked
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(status)
		}))
		f, _ := testFetcher(t)
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		_, err := f.Do(context.Background(), req)
		if !errors.Is(err, want) {
			t.Errorf("HTTP %d = %v, want %v", status, err, want)
		}
		if hits.Load() != 1 {
			t.Errorf("HTTP %d was retried %d times; it must not be", status, hits.Load())
		}
		if IsRetryable(err) {
			t.Errorf("HTTP %d was treated as retryable", status)
		}
		srv.Close()
	}
}

func TestFetcherTreatsAnUnexpected4xxAsALayoutProblem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f, _ := testFetcher(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := f.Do(context.Background(), req)
	var parse *ParseError
	if !errors.As(err, &parse) {
		t.Fatalf("Do() = %v, want a ParseError", err)
	}
	if IsRetryable(err) {
		t.Error("a 404 was treated as retryable")
	}
}

// The politeness budget, in numbers rather than prose: this is the arithmetic
// docs/meal-kit-import.md quotes for a measured 740-recipe history, and the
// reason it takes hours instead of minutes.
func TestThePolitenessBudgetSpreadsALongHistoryOverHours(t *testing.T) {
	// One request at a time, 2.5 s apart plus up to 40% jitter: three seconds
	// on average.
	average := DefaultMinInterval + time.Duration(float64(DefaultMinInterval)*DefaultJitter/2)
	if average < 2500*time.Millisecond || average > 4*time.Second {
		t.Fatalf("the average interval is %v; the policy is a real one, not a token one", average)
	}

	// A run fetches at most DefaultRecipesPerRun pages, so its own fetching
	// fits inside the worker's 6-minute run timeout and well inside the
	// 8-minute lease. If a future build raises the cap, this fails.
	perRun := time.Duration(DefaultRecipesPerRun) * average
	if perRun > 6*time.Minute {
		t.Errorf("a full run spends %v fetching, longer than the run timeout", perRun)
	}
	if perRun > DefaultLease {
		t.Errorf("a full run spends %v fetching, longer than its lease %v", perRun, DefaultLease)
	}

	// The measured history: 740 recipes at 40 a run is 19 scheduled runs,
	// which at the scheduler's ten minutes is about three hours.
	const measured = 740
	runs := (measured + DefaultRecipesPerRun - 1) / DefaultRecipesPerRun
	if runs != 19 {
		t.Errorf("a %d-recipe history needs %d runs; the docs say 19", measured, runs)
	}
	wall := time.Duration(runs) * 10 * time.Minute
	if wall < 2*time.Hour || wall > 5*time.Hour {
		t.Errorf("end to end is %v; the docs say roughly three hours", wall)
	}
}
