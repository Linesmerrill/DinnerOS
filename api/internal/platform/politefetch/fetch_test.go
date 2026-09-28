package politefetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testFetcher(attempts int) (*Fetcher, *[]time.Duration) {
	var slept []time.Duration
	clock := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	f := &Fetcher{
		UserAgent: "DinnerOS/1.0 (+https://api.tlps.dev)", MinInterval: time.Second, Jitter: 0.4, Attempts: attempts,
		Now:    func() time.Time { return clock },
		Random: func() float64 { return 0.5 },
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			clock = clock.Add(d)
			return nil
		},
	}
	return f, &slept
}

func get(t *testing.T, f *Fetcher, url string) (Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f.Do(context.Background(), req)
}

func TestFetcherWaitsAndSendsItsUserAgent(t *testing.T) {
	var agents []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents = append(agents, r.Header.Get("User-Agent"))
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f, slept := testFetcher(1)
	for range 3 {
		if resp, err := get(t, f, srv.URL); err != nil || resp.StatusCode != 200 || string(resp.Body) != "ok" {
			t.Fatalf("Do() = %+v, %v", resp, err)
		}
	}
	if len(*slept) != 3 || (*slept)[0] != 200*time.Millisecond {
		t.Fatalf("waits = %v, want jitter first", *slept)
	}
	for _, d := range (*slept)[1:] {
		if d < time.Second || d > 1400*time.Millisecond {
			t.Errorf("wait %v is outside the interval plus jitter", d)
		}
	}
	for _, a := range agents {
		if a != "DinnerOS/1.0 (+https://api.tlps.dev)" {
			t.Errorf("User-Agent = %q", a)
		}
	}
	if f.Requests() != 3 {
		t.Errorf("Requests() = %d", f.Requests())
	}
}

func TestFetcherReturnsA404AsAnAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()
	f, _ := testFetcher(3)
	resp, err := get(t, f, srv.URL)
	if err != nil || resp.StatusCode != http.StatusNotFound || f.Requests() != 1 {
		t.Fatalf("Do() = %+v, %v after %d requests; a 404 is an answer, never retried", resp, err, f.Requests())
	}
}

func TestFetcherStopsOnARefusal(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	f, _ := testFetcher(4)
	if _, err := get(t, f, srv.URL); !errors.Is(err, ErrBlocked) || hits.Load() != 1 {
		t.Fatalf("err = %v after %d requests, want ErrBlocked after one", err, hits.Load())
	}
}

func TestFetcherHonoursRetryAfterAndGivesUpThrottled(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	f, slept := testFetcher(2)
	resp, err := get(t, f, srv.URL)
	if !errors.Is(err, ErrThrottled) || resp.RetryAfter != 7*time.Second || hits.Load() != 2 {
		t.Fatalf("Do() = %+v, %v after %d requests", resp, err, hits.Load())
	}
	found := false
	for _, d := range *slept {
		found = found || d == 7*time.Second
	}
	if !found {
		t.Errorf("waits = %v, want the Retry-After honoured between attempts", *slept)
	}
}

func TestFetcherRetriesServerErrorsThenGivesUp(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f, _ := testFetcher(2)
	if resp, err := get(t, f, srv.URL); err != nil || resp.StatusCode != 200 {
		t.Fatalf("Do() = %+v, %v", resp, err)
	}
	srv.Close()
	if _, err := get(t, f, srv.URL); err == nil || errors.Is(err, ErrBlocked) {
		t.Errorf("closed server err = %v, want a plain network error", err)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	if d, ok := RetryAfter("3600", now); !ok || d != DefaultRetryAfterMax {
		t.Errorf("RetryAfter(3600) = %v, %v", d, ok)
	}
	if d, ok := RetryAfter(now.Add(10*time.Second).Format(http.TimeFormat), now); !ok || d != 10*time.Second {
		t.Errorf("RetryAfter(date) = %v, %v", d, ok)
	}
	if _, ok := RetryAfter("soon", now); ok {
		t.Error("RetryAfter(soon) parsed")
	}
}
