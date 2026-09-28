package shopping

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/politefetch"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file reads a saved product's page on the provider's site, politely
// (decision #548): one request at a time per process, a minimum interval
// with jitter, an honest User-Agent, Retry-After honoured, and a stop — and a
// pause for every later check — the moment the site refuses us (403) or asks
// us to slow down (429). Only walmart.com /ip/<id> pages are ever requested,
// and redirects may not leave the site.

// ProductCheckUserAgent says who is asking and why. It is honest on purpose:
// no browser impersonation.
const ProductCheckUserAgent = "DinnerOS/1.0 (+https://api.tlps.dev; checks that a household's saved products are still listed)"

// Politeness for product checks.
const (
	// SweepCheckInterval is the gap between two checks in the scheduled
	// sweep: the meal-kit importer's interval, plus jitter.
	SweepCheckInterval = 2500 * time.Millisecond
	// HandoffCheckInterval is the gap for the handful of checks a waiting
	// member's hand-off makes (at most HandoffCheckMax).
	HandoffCheckInterval = time.Second
	checkJitter          = 0.4
	// sweepRequestTimeout and handoffRequestTimeout bound one request.
	sweepRequestTimeout   = 20 * time.Second
	handoffRequestTimeout = 6 * time.Second
	// maxProductPageBytes bounds a product page read. A Walmart product page
	// with its embedded data is well under this.
	maxProductPageBytes = 6 << 20
)

// WalmartCheckerOptions configures a WalmartChecker.
type WalmartCheckerOptions struct {
	// BaseURL is https://www.walmart.com; tests point it at an httptest
	// server.
	BaseURL        string
	MinInterval    time.Duration
	RequestTimeout time.Duration
	// Attempts per product: 1 in a hand-off, where a member is waiting; 2 in
	// the sweep, for a network error or 5xx. A 429 is never retried past the
	// attempts, and a 403 never at all.
	Attempts int
	// Sleep, Now, and Random are for tests.
	Sleep  func(context.Context, time.Duration) error
	Now    func() time.Time
	Random func() float64
}

// WalmartChecker checks Walmart product pages. It is safe for concurrent use;
// requests from one checker are serialized.
type WalmartChecker struct {
	base    *url.URL
	fetcher *politefetch.Fetcher
}

var _ ProductChecker = (*WalmartChecker)(nil)

// NewWalmartChecker returns a checker.
func NewWalmartChecker(o WalmartCheckerOptions) (*WalmartChecker, error) {
	if o.BaseURL == "" {
		o.BaseURL = "https://www.walmart.com"
	}
	base, err := url.Parse(o.BaseURL)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("shopping: product check base URL %q is invalid", o.BaseURL)
	}
	if o.MinInterval <= 0 {
		o.MinInterval = SweepCheckInterval
	}
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = sweepRequestTimeout
	}
	host := base.Host
	client := &http.Client{
		Timeout: o.RequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Walmart redirects /ip/<id> to /ip/<slug>/<id> (or, for a dead
			// item, /ip/seort/<id>). Anything that leaves the site is not an
			// answer about the item.
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Host != host {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	random := o.Random
	if random == nil {
		random = rand.Float64
	}
	return &WalmartChecker{base: base, fetcher: &politefetch.Fetcher{
		Client: client, UserAgent: ProductCheckUserAgent, MinInterval: o.MinInterval, Jitter: checkJitter,
		Attempts: max(o.Attempts, 1), MaxBytes: maxProductPageBytes, Sleep: o.Sleep, Now: o.Now, Random: random,
	}}, nil
}

// NewHandoffChecker returns the checker the API server uses for saves and
// hand-offs: a short interval and timeout, and no retries, because a member
// is waiting.
func NewHandoffChecker() *WalmartChecker {
	c, _ := NewWalmartChecker(WalmartCheckerOptions{MinInterval: HandoffCheckInterval, RequestTimeout: handoffRequestTimeout, Attempts: 1})
	return c
}

// NewSweepChecker returns the checker the scheduled sweep uses.
func NewSweepChecker() *WalmartChecker {
	c, _ := NewWalmartChecker(WalmartCheckerOptions{MinInterval: SweepCheckInterval, RequestTimeout: sweepRequestTimeout, Attempts: 2})
	return c
}

// Requests is how many requests this checker has made.
func (c *WalmartChecker) Requests() int { return c.fetcher.Requests() }

// CheckProduct implements ProductChecker.
func (c *WalmartChecker) CheckProduct(ctx context.Context, provider providers.Key, productID string) (providers.ProductCheck, error) {
	unknown := func(detail string) providers.ProductCheck {
		return providers.ProductCheck{Status: providers.ProductUnknown, Detail: detail}
	}
	if provider != providers.KeyWalmart {
		return unknown(providers.DetailUnexpected), nil
	}
	if _, err := providers.NewWalmart(providers.WalmartOptions{}).ParseProduct(productID); err != nil {
		return unknown(providers.DetailUnexpected), nil
	}
	target := c.base.JoinPath("ip", productID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return unknown(providers.DetailUnexpected), nil
	}
	req.Header.Set("Accept", "text/html")
	resp, err := c.fetcher.Do(ctx, req)
	switch {
	case errors.Is(err, politefetch.ErrBlocked):
		return unknown(providers.DetailBlocked), &CheckStop{Blocked: true}
	case errors.Is(err, politefetch.ErrThrottled):
		return unknown(providers.DetailThrottled), &CheckStop{}
	case errors.Is(err, politefetch.ErrServer):
		return unknown(providers.DetailServerError), nil
	case err != nil:
		return unknown(providers.DetailNetwork), nil
	}
	final, err := url.Parse(resp.FinalURL)
	if err != nil || final.Host != c.base.Host {
		return unknown(providers.DetailOffSite), nil
	}
	if strings.HasPrefix(final.Path, "/blocked") {
		// Walmart's bot wall answers with a redirect to /blocked rather than
		// a 403. It is a refusal all the same.
		return unknown(providers.DetailBlocked), &CheckStop{Blocked: true}
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return unknown(providers.DetailOffSite), nil
	}
	return providers.ParseWalmartProductPage(productID, resp.StatusCode, final.Path, resp.Body), nil
}
