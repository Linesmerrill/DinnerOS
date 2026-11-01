// Package kroger is the Kroger Public API client behind the Kroger provider
// (docs/shopping-providers.md#kroger-public-api): stores, product search with
// the store's price and stock, a member's sign-in, and adding to their cart.
// It serves every Kroger banner; Fry's is the default.
//
// It never logs tokens, the client secret, query strings, or response
// bodies.
package kroger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// Scopes. The app token reads products and stores; a member's token writes
// their cart.
const (
	scopeProducts = "product.compact"
	scopeCustomer = "cart.basic:write"
)

// API limits.
const (
	// MaxSearchResults is the most products one search returns.
	MaxSearchResults = 25
	// maxCartItems is the most items one cart request carries.
	maxCartItems = 50
	// maxResponseBytes bounds a response body.
	maxResponseBytes = 4 << 20
	// tokenSlack refreshes a token this long before it expires.
	tokenSlack = time.Minute
	// maxStores is the most stores a store search returns.
	maxStores = 10
)

var (
	// ErrUnauthorized means the member's sign-in is no longer valid; they
	// need to sign in again.
	ErrUnauthorized = fmt.Errorf("kroger: sign-in expired: %w", providers.ErrCustomerSignedOut)
	// ErrUnavailable means the API failed or is rate limiting.
	ErrUnavailable = errors.New("kroger: service unavailable")
	// ErrStoreNotOffered means a store belongs to a banner DinnerOS hasn't
	// turned on.
	ErrStoreNotOffered = providers.ErrStoreNotOffered
)

// Options configures the client.
type Options struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// APIBase is the API origin (https://api.kroger.com).
	APIBase string
	// Banner is the default store brand ("frys").
	Banner string
	// Banners are other brands to enable beside it; a household's brand
	// follows the store it picks.
	Banners []string
	// HTTPClient defaults to one with a 15-second timeout.
	HTTPClient *http.Client
	Now        func() time.Time
}

// Provider is the Kroger provider: the pure providers.Kroger plus search,
// stores, sign-in, and cart writes.
type Provider struct {
	*providers.Kroger
	opts Options
	http *http.Client
	now  func() time.Time
	// app is shared by every banner's view of the provider.
	app *appToken
}

// appToken caches the application token for products and stores.
type appToken struct {
	mu    sync.Mutex
	token string
	until time.Time
}

var (
	_ providers.GroceryProvider   = (*Provider)(nil)
	_ providers.ProductSearcher   = (*Provider)(nil)
	_ providers.StoreFinder       = (*Provider)(nil)
	_ providers.CartWriter        = (*Provider)(nil)
	_ providers.CustomerAccount   = (*Provider)(nil)
	_ providers.Bannered          = (*Provider)(nil)
	_ providers.StoreBannerLookup = (*Provider)(nil)
)

// New returns the Kroger provider.
func New(o Options) *Provider {
	p := &Provider{Kroger: providers.NewKroger(o.Banner, o.Banners...), opts: o, http: o.HTTPClient, now: o.Now, app: &appToken{}}
	p.opts.APIBase = strings.TrimRight(o.APIBase, "/")
	if p.http == nil {
		p.http = &http.Client{Timeout: 15 * time.Second}
	}
	if p.now == nil {
		p.now = time.Now
	}
	return p
}

// ForBanner implements providers.Bannered: the provider as a household at
// banner sees it, sharing this one's client and tokens.
func (p *Provider) ForBanner(banner string) providers.GroceryProvider {
	view := *p
	view.Kroger = p.Kroger.WithBanner(banner)
	return &view
}

// --- Tokens -------------------------------------------------------------------

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// token posts a token request.
func (p *Provider) token(ctx context.Context, form url.Values) (providers.CustomerToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.opts.APIBase+"/v1/connect/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return providers.CustomerToken{}, err
	}
	req.SetBasicAuth(p.opts.ClientID, p.opts.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var out tokenResponse
	if err := p.do(req, &out); err != nil {
		return providers.CustomerToken{}, err
	}
	if out.AccessToken == "" {
		return providers.CustomerToken{}, fmt.Errorf("%w: token response without an access token", ErrUnavailable)
	}
	return providers.CustomerToken{
		AccessToken: out.AccessToken, RefreshToken: out.RefreshToken,
		ExpiresAt: p.now().Add(time.Duration(out.ExpiresIn) * time.Second),
	}, nil
}

// app returns the application token for products and stores, cached until
// shortly before it expires.
func (p *Provider) appTokenFor(ctx context.Context) (string, error) {
	p.app.mu.Lock()
	defer p.app.mu.Unlock()
	if p.app.token != "" && p.now().Before(p.app.until) {
		return p.app.token, nil
	}
	t, err := p.token(ctx, url.Values{"grant_type": {"client_credentials"}, "scope": {scopeProducts}})
	if err != nil {
		return "", err
	}
	p.app.token, p.app.until = t.AccessToken, t.ExpiresAt.Add(-tokenSlack)
	return p.app.token, nil
}

// AuthorizeURL implements providers.CustomerAccount.
func (p *Provider) AuthorizeURL(state string) string {
	q := url.Values{
		"scope": {scopeCustomer}, "response_type": {"code"}, "client_id": {p.opts.ClientID},
		"redirect_uri": {p.opts.RedirectURL}, "state": {state},
	}
	return p.opts.APIBase + "/v1/connect/oauth2/authorize?" + q.Encode()
}

// ExchangeCode implements providers.CustomerAccount.
func (p *Provider) ExchangeCode(ctx context.Context, code string) (providers.CustomerToken, error) {
	return p.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {p.opts.RedirectURL}})
}

// RefreshCustomer implements providers.CustomerAccount.
func (p *Provider) RefreshCustomer(ctx context.Context, refreshToken string) (providers.CustomerToken, error) {
	t, err := p.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
	if err != nil {
		return providers.CustomerToken{}, err
	}
	if t.RefreshToken == "" {
		t.RefreshToken = refreshToken
	}
	return t, nil
}

// --- Stores ---------------------------------------------------------------------

type location struct {
	LocationID string `json:"locationId"`
	Chain      string `json:"chain"`
	Name       string `json:"name"`
	Address    struct {
		AddressLine1 string `json:"addressLine1"`
		City         string `json:"city"`
		State        string `json:"state"`
		ZipCode      string `json:"zipCode"`
	} `json:"address"`
}

type locationsResponse struct {
	Data []location `json:"data"`
}

type locationResponse struct {
	Data location `json:"data"`
}

// FindStores implements providers.StoreFinder: the enabled banners' stores
// near a ZIP code, nearest first, each named for its banner. With one banner
// the API filters by its chain; with several it returns every Kroger-family
// store nearby and those of other banners are dropped.
func (p *Provider) FindStores(ctx context.Context, q providers.StoreQuery) ([]providers.Store, error) {
	banners := p.Banners()
	params := url.Values{"filter.zipCode.near": {q.ZIPCode}, "filter.limit": {strconv.Itoa(maxStores)}}
	if len(banners) == 1 {
		params.Set("filter.chain", banners[0].Chain)
	} else {
		params.Set("filter.limit", strconv.Itoa(maxStores*3))
	}
	var out locationsResponse
	if err := p.getApp(ctx, "/v1/locations", params, &out); err != nil {
		return nil, err
	}
	stores := make([]providers.Store, 0, len(out.Data))
	for _, l := range out.Data {
		banner, ok := p.BannerForChain(l.Chain)
		if !ok {
			if len(banners) > 1 {
				continue
			}
			// One banner was asked for by chain; trust the API's filter.
			banner = banners[0]
		}
		a := l.Address
		address := strings.TrimSpace(a.AddressLine1)
		if a.City != "" {
			address += ", " + a.City
		}
		if a.State != "" {
			address += ", " + a.State
		}
		stores = append(stores, providers.Store{
			StoreID: l.LocationID, Name: storeName(banner.Name, l.Name), Address: strings.TrimPrefix(address, ", "), Banner: banner.Key,
		})
		if len(stores) == maxStores {
			break
		}
	}
	return stores, nil
}

// StoreBanner implements providers.StoreBannerLookup: the banner of the
// store with location ID storeID. A store of a banner that isn't enabled is
// ErrStoreNotOffered.
func (p *Provider) StoreBanner(ctx context.Context, storeID string) (string, error) {
	var out locationResponse
	if err := p.getApp(ctx, "/v1/locations/"+url.PathEscape(storeID), url.Values{}, &out); err != nil {
		return "", err
	}
	banner, ok := p.BannerForChain(out.Data.Chain)
	if !ok {
		return "", ErrStoreNotOffered
	}
	return banner.Key, nil
}

// storeName shortens a location's name to the banner and the part people
// know it by: "Fry's Food And Drug - 30th St Thomas" is "Fry's 30th St
// Thomas", and "Fry's Food And Drug - Frys Food And Drug Phoenix", which
// repeats the chain, is "Fry's Phoenix".
func storeName(banner, name string) string {
	chain, place, found := strings.Cut(name, " - ")
	if !found {
		return strings.TrimSpace(name)
	}
	// The chain's own names, as a place's first words may repeat them.
	repeats := []string{letters(chain), letters(banner) + "foodanddrug", letters(banner) + "marketplace", letters(banner)}
	words := strings.Fields(place)
	cut := 0
	for i := range words {
		if slices.Contains(repeats, letters(strings.Join(words[:i+1], " "))) {
			cut = i + 1
		}
	}
	words = words[cut:]
	if len(words) == 0 {
		return banner
	}
	return banner + " " + strings.Join(words, " ")
}

// letters lowercases s and keeps only its letters and digits.
func letters(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// --- Products -------------------------------------------------------------------

type productsResponse struct {
	Data []struct {
		ProductID   string  `json:"productId"`
		UPC         string  `json:"upc"`
		Brand       string  `json:"brand"`
		Description string  `json:"description"`
		Images      []image `json:"images"`
		Items       []struct {
			Size  string `json:"size"`
			Price *struct {
				Regular float64 `json:"regular"`
				Promo   float64 `json:"promo"`
			} `json:"price"`
			Inventory *struct {
				StockLevel string `json:"stockLevel"`
			} `json:"inventory"`
		} `json:"items"`
	} `json:"data"`
}

// SearchProducts implements providers.ProductSearcher. With a store, results
// carry that store's price and stock.
func (p *Provider) SearchProducts(ctx context.Context, q providers.SearchQuery) ([]providers.Product, error) {
	limit := q.Limit
	if limit <= 0 || limit > MaxSearchResults {
		limit = MaxSearchResults
	}
	params := url.Values{"filter.term": {q.Term}, "filter.limit": {strconv.Itoa(limit)}}
	if q.StoreID != "" {
		params.Set("filter.locationId", q.StoreID)
	}
	// Kroger's search matches every word, so a term written for another store
	// ("fresh whole Asparagus") can find nothing. Drop words from the front,
	// keeping the noun at the end, until something matches.
	var out productsResponse
	words := strings.Fields(q.Term)
	for {
		if err := p.getApp(ctx, "/v1/products", params, &out); err != nil {
			return nil, err
		}
		if len(out.Data) > 0 || len(words) <= 1 {
			break
		}
		words = words[1:]
		params.Set("filter.term", strings.Join(words, " "))
	}
	products := make([]providers.Product, 0, len(out.Data))
	for _, d := range out.Data {
		id := d.ProductID
		if id == "" {
			id = d.UPC
		}
		if id == "" {
			continue
		}
		product := providers.Product{ProductID: id, Name: strings.TrimSpace(d.Description), Brand: strings.TrimSpace(d.Brand), ImageURL: imageURL(d.Images)}
		if len(d.Items) > 0 {
			it := d.Items[0]
			product.Size = strings.TrimSpace(it.Size)
			if it.Price != nil {
				price := it.Price.Regular
				if it.Price.Promo > 0 && it.Price.Promo < price {
					price = it.Price.Promo
				}
				if price > 0 {
					cents := int64(math.Round(price * 100))
					product.PriceCents = &cents
				}
			}
			if it.Inventory != nil {
				switch it.Inventory.StockLevel {
				case "HIGH":
					product.Stock = providers.StockHigh
				case "LOW":
					product.Stock = providers.StockLow
				case "TEMPORARILY_OUT_OF_STOCK":
					product.Stock = providers.StockOut
				}
			}
		}
		products = append(products, product)
	}
	return products, nil
}

type image struct {
	Perspective string `json:"perspective"`
	Featured    bool   `json:"featured"`
	Sizes       []struct {
		Size string `json:"size"`
		URL  string `json:"url"`
	} `json:"sizes"`
}

// imageURL picks the featured image, else the front one, medium size.
func imageURL(images []image) string {
	best := -1
	for i, img := range images {
		if best < 0 || img.Featured || (img.Perspective == "front" && !images[best].Featured) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	out := ""
	for _, s := range images[best].Sizes {
		if out == "" || s.Size == "medium" {
			out = s.URL
		}
	}
	return out
}

// --- Cart -----------------------------------------------------------------------

type cartRequest struct {
	Items []cartItem `json:"items"`
}

type cartItem struct {
	UPC      string `json:"upc"`
	Quantity int    `json:"quantity"`
	Modality string `json:"modality"`
}

// AddToCart implements providers.CartWriter: it adds items to the signed-in
// member's cart for pickup. The cart can only be added to.
func (p *Provider) AddToCart(ctx context.Context, customerToken string, items []providers.CartItem) error {
	for start := 0; start < len(items); start += maxCartItems {
		body := cartRequest{}
		for _, it := range items[start:min(start+maxCartItems, len(items))] {
			body.Items = append(body.Items, cartItem{UPC: it.ProductID, Quantity: it.Quantity, Modality: "PICKUP"})
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.opts.APIBase+"/v1/cart/add", strings.NewReader(string(raw)))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+customerToken)
		req.Header.Set("Content-Type", "application/json")
		if err := p.do(req, nil); err != nil {
			return err
		}
	}
	return nil
}

// --- HTTP -----------------------------------------------------------------------

// getApp GETs a path with the application token.
func (p *Provider) getApp(ctx context.Context, path string, params url.Values, out any) error {
	token, err := p.appTokenFor(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.opts.APIBase+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	err = p.do(req, out)
	if errors.Is(err, ErrUnauthorized) {
		// The cached app token was revoked early; the next call gets a new one.
		p.app.mu.Lock()
		p.app.token = ""
		p.app.mu.Unlock()
		return fmt.Errorf("%w: application token rejected", ErrUnavailable)
	}
	return err
}

// do sends req and decodes a JSON body into out (nil skips it). Errors name
// the status only, never the body or URL.
func (p *Provider) do(req *http.Request, out any) error {
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s %s: %v", ErrUnavailable, req.Method, req.URL.Path, errors.Unwrap(err))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("%w: read %s: %v", ErrUnavailable, req.URL.Path, err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusBadRequest && strings.HasSuffix(req.URL.Path, "/token"):
		// A refused code or refresh token means signing in again.
		return fmt.Errorf("%w: %s %s: %d", ErrUnauthorized, req.Method, req.URL.Path, resp.StatusCode)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("%w: %s %s: %d", ErrUnavailable, req.Method, req.URL.Path, resp.StatusCode)
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: decode %s: %v", ErrUnavailable, req.URL.Path, err)
	}
	return nil
}
