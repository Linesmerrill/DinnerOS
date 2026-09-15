package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func TestLimiterBurstRefillAndIsolation(t *testing.T) {
	c := &clock{t: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)}
	l := New(Options{Burst: 3, Every: 6 * time.Second, Now: c.Now})

	for i := range 3 {
		if !l.Allow("1.1.1.1") {
			t.Fatalf("request %d denied within burst", i+1)
		}
	}
	if l.Allow("1.1.1.1") {
		t.Fatal("request over burst allowed")
	}
	if !l.Allow("2.2.2.2") {
		t.Fatal("other client limited by first client's usage")
	}

	c.t = c.t.Add(6 * time.Second)
	if !l.Allow("1.1.1.1") {
		t.Fatal("request denied after one refill interval")
	}
	if l.Allow("1.1.1.1") {
		t.Fatal("more than one token refilled")
	}
}

func TestLimiterEvictsIdleClients(t *testing.T) {
	c := &clock{t: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)}
	l := New(Options{IdleTTL: time.Minute, Now: c.Now})

	l.Allow("1.1.1.1")
	c.t = c.t.Add(30 * time.Second)
	l.Allow("2.2.2.2")
	if l.Len() != 2 {
		t.Fatalf("Len = %d, want 2", l.Len())
	}

	c.t = c.t.Add(45 * time.Second) // 1.1.1.1 idle 75s, 2.2.2.2 idle 45s
	l.Allow("3.3.3.3")
	if l.Len() != 2 {
		t.Fatalf("Len after sweep = %d, want 2 (1.1.1.1 evicted)", l.Len())
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        []string
		want       string
	}{
		{name: "remote addr", remoteAddr: "10.0.0.1:5555", want: "10.0.0.1"},
		{name: "heroku single entry", remoteAddr: "10.0.0.1:5555", xff: []string{"203.0.113.7"}, want: "203.0.113.7"},
		{name: "spoofed prefix ignored", remoteAddr: "10.0.0.1:5555", xff: []string{"1.2.3.4, 203.0.113.7"}, want: "203.0.113.7"},
		{name: "multiple headers uses last", remoteAddr: "10.0.0.1:5555", xff: []string{"1.2.3.4", "198.51.100.2"}, want: "198.51.100.2"},
		{name: "ipv6", remoteAddr: "10.0.0.1:5555", xff: []string{"2001:db8::1"}, want: "2001:db8::1"},
		{name: "invalid entry falls back", remoteAddr: "[::1]:5555", xff: []string{"not-an-ip"}, want: "::1"},
		{name: "remote addr without port", remoteAddr: "10.0.0.9", want: "10.0.0.9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}
			if got := ClientIP(req); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMiddlewareReturns429(t *testing.T) {
	l := New(Options{Burst: 1})
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	do := func(ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/apple", nil)
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("203.0.113.1"); rec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec.Code)
	}
	rec := do("203.0.113.1")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("second status = %d Retry-After=%q, want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	var body httpx.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != "rate_limited" {
		t.Errorf("body = %+v, %v", body, err)
	}
	if rec := do("203.0.113.2"); rec.Code != http.StatusNoContent {
		t.Errorf("other client status = %d, want 204", rec.Code)
	}
}
