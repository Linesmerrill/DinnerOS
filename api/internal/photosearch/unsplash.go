package photosearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Unsplash's API guidelines (decision 705): photos are hotlinked from the
// addresses the API returns, every photo shown names its photographer and
// Unsplash with links back to both tagged utm_source=dinneros and
// utm_medium=referral, and using a photo calls its download_location.

const (
	// DefaultUnsplashAPIBase is the Unsplash API origin.
	DefaultUnsplashAPIBase = "https://api.unsplash.com"
	// DefaultUnsplashImageHost is the only host photos are shown from.
	DefaultUnsplashImageHost = "images.unsplash.com"
	// SourceName is how the provider is named in a credit.
	SourceName = "Unsplash"

	utmSource = "dinneros"
	utmMedium = "referral"

	perPage      = 30
	cacheTTL     = time.Hour
	cacheEntries = 500
	timeout      = 10 * time.Second
)

// SourceURL is Unsplash's home page, tagged as a referral from DinnerOS.
func SourceURL() string { return withReferral("https://unsplash.com/") }

// UnsplashOptions configures an Unsplash client.
type UnsplashOptions struct {
	// AccessKey is the Unsplash access key. It is a secret: never log it.
	AccessKey string
	// APIBase defaults to DefaultUnsplashAPIBase. Tests point it elsewhere.
	APIBase string
	// ImageHost defaults to DefaultUnsplashImageHost. Tests point it elsewhere.
	ImageHost string
	// ImageScheme defaults to https. Tests may use http.
	ImageScheme string
	// Client defaults to an http.Client with a 10 second timeout.
	Client *http.Client
	// Now defaults to time.Now.
	Now func() time.Time
}

// Unsplash searches Unsplash.
type Unsplash struct {
	opts UnsplashOptions

	mu    sync.Mutex
	cache map[string]cachedPage
}

type cachedPage struct {
	page    Page
	expires time.Time
}

// NewUnsplash returns an Unsplash client.
func NewUnsplash(opts UnsplashOptions) *Unsplash {
	if opts.APIBase == "" {
		opts.APIBase = DefaultUnsplashAPIBase
	}
	opts.APIBase = strings.TrimRight(opts.APIBase, "/")
	if opts.ImageHost == "" {
		opts.ImageHost = DefaultUnsplashImageHost
	}
	if opts.ImageScheme == "" {
		opts.ImageScheme = "https"
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: timeout}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Unsplash{opts: opts, cache: map[string]cachedPage{}}
}

type unsplashPhoto struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URLs   struct {
		Regular string `json:"regular"`
		Small   string `json:"small"`
	} `json:"urls"`
	Links struct {
		DownloadLocation string `json:"download_location"`
	} `json:"links"`
	User struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Links    struct {
			HTML string `json:"html"`
		} `json:"links"`
	} `json:"user"`
}

type unsplashSearch struct {
	TotalPages int             `json:"total_pages"`
	Results    []unsplashPhoto `json:"results"`
}

// Search returns a page of photos for query. Pages are cached for an hour,
// since the same recipe names are searched again and Unsplash limits requests.
func (u *Unsplash) Search(ctx context.Context, query string, page int) (Page, error) {
	query = CleanQuery(query)
	if query == "" {
		return Page{}, fmt.Errorf("photosearch: empty query")
	}
	page = min(max(page, 1), MaxPage)
	key := strings.ToLower(query) + "\x00" + strconv.Itoa(page)
	if cached, ok := u.cached(key); ok {
		return cached, nil
	}
	v := url.Values{
		"query": {query}, "page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)},
		"content_filter": {"high"},
	}
	var body unsplashSearch
	if err := u.getJSON(ctx, u.opts.APIBase+"/search/photos?"+v.Encode(), &body); err != nil {
		return Page{}, err
	}
	out := Page{Photos: make([]Result, 0, len(body.Results))}
	for _, ph := range body.Results {
		thumb, ok := u.imageURL(ph.URLs.Small)
		credit, credited := u.credit(ph)
		if !ok || !credited || !unsplashIDRe.MatchString(ph.ID) {
			continue
		}
		out.Photos = append(out.Photos, Result{
			ID: ph.ID, ThumbnailURL: thumb, Width: ph.Width, Height: ph.Height, Credit: credit,
		})
	}
	if page < body.TotalPages && page < MaxPage {
		out.NextPage = page + 1
	}
	u.store(key, out)
	return out, nil
}

var unsplashIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Pick looks the photo up by its ID, so the address and credit come from
// Unsplash and never from the caller, then calls its download_location as
// the API guidelines ask when a photo is used.
func (u *Unsplash) Pick(ctx context.Context, id string) (Picked, error) {
	if !unsplashIDRe.MatchString(id) {
		return Picked{}, ErrNotFound
	}
	var ph unsplashPhoto
	if err := u.getJSON(ctx, u.opts.APIBase+"/photos/"+id, &ph); err != nil {
		return Picked{}, err
	}
	image, ok := u.imageURL(ph.URLs.Regular)
	if !ok {
		return Picked{}, fmt.Errorf("photosearch: photo %s has no usable address", id)
	}
	credit, ok := u.credit(ph)
	if !ok {
		return Picked{}, fmt.Errorf("photosearch: photo %s has no photographer", id)
	}
	if err := u.trackDownload(ctx, ph.Links.DownloadLocation); err != nil {
		return Picked{}, err
	}
	return Picked{ImageURL: image, Credit: credit}, nil
}

// trackDownload calls a photo's download_location, which must be on the API's
// own host.
func (u *Unsplash) trackDownload(ctx context.Context, raw string) error {
	loc, err := url.Parse(raw)
	base, _ := url.Parse(u.opts.APIBase)
	if err != nil || base == nil || loc.User != nil || loc.Scheme != base.Scheme || !strings.EqualFold(loc.Host, base.Host) {
		return fmt.Errorf("photosearch: unexpected download_location")
	}
	var ignored json.RawMessage
	return u.getJSON(ctx, loc.String(), &ignored)
}

// credit is the photographer's name and their Unsplash page with the referral tags.
func (u *Unsplash) credit(ph unsplashPhoto) (Credit, bool) {
	name := strings.Join(strings.Fields(ph.User.Name), " ")
	if name == "" {
		name = strings.TrimSpace(ph.User.Username)
	}
	profile, err := url.Parse(ph.User.Links.HTML)
	if name == "" || err != nil || profile.Scheme != "https" || profile.User != nil ||
		!strings.EqualFold(profile.Host, "unsplash.com") {
		return Credit{}, false
	}
	profile.RawQuery, profile.Fragment = "", ""
	return Credit{Name: name, ProfileURL: withReferral(profile.String())}, true
}

func withReferral(raw string) string {
	return raw + "?" + url.Values{"utm_source": {utmSource}, "utm_medium": {utmMedium}}.Encode()
}

// imageURL checks raw is on the image host and returns it as given: Unsplash's
// own address, query and all, is what gets hotlinked.
func (u *Unsplash) imageURL(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != u.opts.ImageScheme || parsed.User != nil ||
		!strings.EqualFold(parsed.Host, u.opts.ImageHost) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

// IsUnsplashImageURL reports whether raw is an https address on Unsplash's
// image host, the only outside address a picked recipe photo may have.
func IsUnsplashImageURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.User == nil &&
		strings.EqualFold(parsed.Host, DefaultUnsplashImageHost) && parsed.Path != "" && parsed.Path != "/"
}

func (u *Unsplash) getJSON(ctx context.Context, address string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Client-ID "+u.opts.AccessKey)
	req.Header.Set("Accept-Version", "v1")
	req.Header.Set("Accept", "application/json")
	res, err := u.opts.Client.Do(req)
	if err != nil {
		return fmt.Errorf("photosearch: unsplash: %w", err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("photosearch: unsplash status %d", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(out); err != nil {
		return fmt.Errorf("photosearch: unsplash: %w", err)
	}
	return nil
}

func (u *Unsplash) cached(key string) (Page, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.cache[key]
	if !ok || !u.opts.Now().Before(c.expires) {
		return Page{}, false
	}
	return c.page, true
}

func (u *Unsplash) store(key string, page Page) {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := u.opts.Now()
	if len(u.cache) >= cacheEntries {
		var oldest string
		var oldestAt time.Time
		for k, c := range u.cache {
			if !now.Before(c.expires) {
				delete(u.cache, k)
				continue
			}
			if oldest == "" || c.expires.Before(oldestAt) {
				oldest, oldestAt = k, c.expires
			}
		}
		if len(u.cache) >= cacheEntries {
			delete(u.cache, oldest)
		}
	}
	u.cache[key] = cachedPage{page: page, expires: now.Add(cacheTTL)}
}
