// Package ratelimit provides in-memory, per-client-IP token-bucket rate
// limiting for abuse-prone endpoints (authentication, invitations).
//
// State is per process. With several dynos each keeps its own buckets, which
// is acceptable for coarse abuse protection.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// Options configures a Limiter. Zero values use defaults.
type Options struct {
	// Every is the refill interval for one token. Default 6s.
	Every time.Duration
	// Burst is the bucket size. Default 10.
	Burst int
	// IdleTTL is how long an unused client's bucket is kept. Default 10m.
	IdleTTL time.Duration
	// Now is the clock. Default time.Now.
	Now func() time.Time
}

// Limiter rate-limits requests per client IP.
type Limiter struct {
	limit   rate.Limit
	burst   int
	idleTTL time.Duration
	now     func() time.Time

	mu        sync.Mutex
	clients   map[string]*client
	lastSweep time.Time
}

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// New returns a Limiter.
func New(opts Options) *Limiter {
	if opts.Every <= 0 {
		opts.Every = 6 * time.Second
	}
	if opts.Burst <= 0 {
		opts.Burst = 10
	}
	if opts.IdleTTL <= 0 {
		opts.IdleTTL = 10 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Limiter{
		limit:   rate.Every(opts.Every),
		burst:   opts.Burst,
		idleTTL: opts.IdleTTL,
		now:     opts.Now,
		clients: map[string]*client{},
	}
}

// Allow reports whether a request from key may proceed, consuming a token.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	c, ok := l.clients[key]
	if !ok {
		c = &client{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.clients[key] = c
	}
	c.lastSeen = now
	return c.limiter.AllowN(now, 1)
}

// sweepLocked evicts idle clients at most once per IdleTTL, keeping memory
// bounded without a background goroutine.
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < l.idleTTL {
		return
	}
	l.lastSweep = now
	for key, c := range l.clients {
		if now.Sub(c.lastSeen) >= l.idleTTL {
			delete(l.clients, key)
		}
	}
}

// Len returns the number of tracked clients.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}

// Middleware rejects requests over the limit with 429 rate_limited, keyed by
// client IP.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return l.MiddlewareBy(ClientIP)(next)
}

// MiddlewareBy is Middleware with a caller-chosen key, such as the
// authenticated user for routes that run after authentication.
func (l *Limiter) MiddlewareBy(key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.Allow(key(r)) {
				w.Header().Set("Retry-After", "6")
				httpx.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "too many requests; try again shortly")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the client address for rate limiting.
//
// Heroku's router appends the connecting client's IP as the last
// X-Forwarded-For entry, so that entry is trustworthy there even when a client
// sends its own (spoofed) header. Earlier entries are client-controlled and
// ignored. Without a usable header, the connection's remote address is used.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		last := xff[len(xff)-1]
		if i := strings.LastIndexByte(last, ','); i >= 0 {
			last = last[i+1:]
		}
		if ip := net.ParseIP(strings.TrimSpace(last)); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
