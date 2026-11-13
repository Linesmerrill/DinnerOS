// Package instacart is the Instacart Developer Platform client behind the
// Instacart provider (docs/shopping-providers.md#instacart-developer-platform):
// it turns a week's list into an Instacart shopping list page, where the
// member picks a store, fills the cart, and checks out.
//
// It never logs the API key or response bodies, and it sends Instacart only
// ingredient names and amounts: nothing about the household.
package instacart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// API origins.
const (
	// ProductionAPIBase is the production API.
	ProductionAPIBase = "https://connect.instacart.com"
	// DevelopmentAPIBase is the development API, for development keys.
	DevelopmentAPIBase = "https://connect.dev.instacart.tools"
)

// Limits.
const (
	// maxResponseBytes bounds a response body.
	maxResponseBytes = 1 << 20
	// MaxTitleLength bounds a page title.
	MaxTitleLength = 100
	// expiresInDays keeps a page working for the life of a week's shopping
	// and a little after, so "Open Again" works through the week.
	expiresInDays = 30
)

// ErrUnavailable means Instacart failed, refused the key, or is rate
// limiting. The member can try again later.
var ErrUnavailable = errors.New("instacart: service unavailable")

// Options configures the client.
type Options struct {
	// APIKey is the Developer Platform key. It is a secret.
	APIKey string
	// APIBase is the API origin; ProductionAPIBase by default.
	APIBase string
	// Affiliate says the key is tied to an Impact account, so Instacart adds
	// affiliate tracking to page links.
	Affiliate bool
	// LinkbackURL is where the page's "back to" link goes, or "".
	LinkbackURL string
	// HTTPClient defaults to one with a 15-second timeout.
	HTTPClient *http.Client
}

// Provider is the Instacart provider: the pure providers.Instacart plus the
// call that makes the page.
type Provider struct {
	*providers.Instacart
	opts Options
	http *http.Client
}

var (
	_ providers.GroceryProvider = (*Provider)(nil)
	_ providers.ListPageMaker   = (*Provider)(nil)
	_ providers.CatalogListed   = (*Provider)(nil)
)

// New returns the Instacart provider.
func New(o Options) *Provider {
	p := &Provider{Instacart: providers.NewInstacart(o.Affiliate), opts: o, http: o.HTTPClient}
	p.opts.APIBase = strings.TrimRight(o.APIBase, "/")
	if p.opts.APIBase == "" {
		p.opts.APIBase = ProductionAPIBase
	}
	if p.http == nil {
		p.http = &http.Client{Timeout: 15 * time.Second}
	}
	return p
}

// The request and response of POST /idp/v1/products/products_link
// (https://docs.instacart.com/developer_platform_api/api/products/create_shopping_list_page).
type (
	linkRequest struct {
		Title                    string             `json:"title"`
		LinkType                 string             `json:"link_type"`
		ExpiresIn                int                `json:"expires_in"`
		LineItems                []lineItem         `json:"line_items"`
		LandingPageConfiguration *landingPageConfig `json:"landing_page_configuration,omitempty"`
	}
	lineItem struct {
		Name                 string        `json:"name"`
		LineItemMeasurements []measurement `json:"line_item_measurements,omitempty"`
	}
	measurement struct {
		Quantity float64 `json:"quantity"`
		Unit     string  `json:"unit"`
	}
	landingPageConfig struct {
		PartnerLinkbackURL string `json:"partner_linkback_url,omitempty"`
	}
	linkResponse struct {
		ProductsLinkURL string `json:"products_link_url"`
	}
	errorResponse struct {
		Error  *apiError  `json:"error"`
		Errors []apiError `json:"errors"`
	}
	apiError struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	}
)

// MakeListPage implements providers.ListPageMaker: it makes a shopping list
// page for items and returns its link, with Instacart's affiliate tracking
// when the key carries it.
func (p *Provider) MakeListPage(ctx context.Context, title string, items []providers.ListItem) (string, error) {
	if len(items) == 0 {
		return "", errors.New("instacart: no items")
	}
	body := linkRequest{Title: trimTitle(title), LinkType: "shopping_list", ExpiresIn: expiresInDays, LineItems: make([]lineItem, 0, len(items))}
	for _, it := range items {
		li := lineItem{Name: it.Name}
		for _, m := range it.Measurements {
			li.LineItemMeasurements = append(li.LineItemMeasurements, measurement{Quantity: m.Quantity, Unit: m.Unit})
		}
		body.LineItems = append(body.LineItems, li)
	}
	if p.opts.LinkbackURL != "" {
		body.LandingPageConfiguration = &landingPageConfig{PartnerLinkbackURL: p.opts.LinkbackURL}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.opts.APIBase+"/idp/v1/products/products_link", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.opts.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("%w: read response: %w", ErrUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		// The status and Instacart's own message are safe to log; the body
		// is not kept.
		return "", fmt.Errorf("%w: status %d%s", ErrUnavailable, resp.StatusCode, errorSummary(data))
	}
	var out linkResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("%w: decode response: %w", ErrUnavailable, err)
	}
	u, err := url.Parse(out.ProductsLinkURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("%w: response without a usable link", ErrUnavailable)
	}
	// Instacart's links open in its app on a phone; https keeps them out of
	// mixed-content trouble in an in-app browser.
	u.Scheme = "https"
	return u.String(), nil
}

// errorSummary reads Instacart's error messages ("Invalid quantity: …") for
// the log.
func errorSummary(data []byte) string {
	var e errorResponse
	if json.Unmarshal(data, &e) != nil {
		return ""
	}
	var msgs []string
	if e.Error != nil && e.Error.Message != "" {
		msgs = append(msgs, e.Error.Message)
	}
	for _, x := range e.Errors {
		if x.Message != "" {
			msgs = append(msgs, x.Message)
		}
	}
	if len(msgs) == 0 {
		return ""
	}
	s := strings.Join(msgs, "; ")
	if len(s) > 300 {
		s = s[:300]
	}
	return ": " + s
}

func trimTitle(title string) string {
	t := strings.Join(strings.Fields(title), " ")
	if t == "" {
		t = "Groceries"
	}
	if r := []rune(t); len(r) > MaxTitleLength {
		t = string(r[:MaxTitleLength])
	}
	return t
}
