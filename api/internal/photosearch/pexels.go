package photosearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Pexels photos are free to use, with no attribution required, so a picked
// photo can be shared with other households along with the recipe.

const (
	// DefaultPexelsAPIBase is the Pexels API origin.
	DefaultPexelsAPIBase = "https://api.pexels.com"
	// DefaultPexelsImageHost is the only host photos are downloaded from.
	DefaultPexelsImageHost = "images.pexels.com"

	perPage = 30
	// MaxPage keeps a runaway scroll from paging forever.
	MaxPage = 50
	// maxQueryRunes is longer than any recipe name worth searching for.
	maxQueryRunes = 100
	// photoWidth is the width asked for, in pixels: what the app keeps for
	// its own photos.
	photoWidth = "1600"

	cacheTTL     = time.Hour
	cacheEntries = 500
	timeout      = 10 * time.Second
)

// PexelsOptions configures a Pexels client.
type PexelsOptions struct {
	// APIKey is the Pexels key. It is a secret: never log it.
	APIKey string
	// APIBase defaults to DefaultPexelsAPIBase. Tests point it elsewhere.
	APIBase string
	// ImageHost defaults to DefaultPexelsImageHost. Tests point it elsewhere.
	ImageHost string
	// ImageScheme defaults to https. Tests may use http.
	ImageScheme string
	// MaxBytes is the largest photo Fetch returns. Required.
	MaxBytes int
	// Client defaults to an http.Client with a 10 second timeout.
	Client *http.Client
	// Now defaults to time.Now.
	Now func() time.Time
}

// Pexels searches Pexels.
type Pexels struct {
	opts PexelsOptions

	mu    sync.Mutex
	cache map[string]cachedPage
}

type cachedPage struct {
	page    Page
	expires time.Time
}

// NewPexels returns a Pexels client.
func NewPexels(opts PexelsOptions) *Pexels {
	if opts.APIBase == "" {
		opts.APIBase = DefaultPexelsAPIBase
	}
	opts.APIBase = strings.TrimRight(opts.APIBase, "/")
	if opts.ImageHost == "" {
		opts.ImageHost = DefaultPexelsImageHost
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
	return &Pexels{opts: opts, cache: map[string]cachedPage{}}
}

type pexelsPhoto struct {
	ID           int64  `json:"id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Photographer string `json:"photographer"`
	Src          struct {
		Original string `json:"original"`
		Medium   string `json:"medium"`
	} `json:"src"`
}

type pexelsSearch struct {
	Page     int           `json:"page"`
	Photos   []pexelsPhoto `json:"photos"`
	NextPage string        `json:"next_page"`
}

// CleanQuery is the query as it is searched: single-spaced and not too long.
func CleanQuery(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	for utf8.RuneCountInString(q) > maxQueryRunes {
		_, size := utf8.DecodeLastRuneInString(q)
		q = q[:len(q)-size]
	}
	return strings.TrimSpace(q)
}

// Search returns a page of photos for query. Pages are cached for an hour,
// since the same recipe names are searched again and Pexels limits requests.
func (p *Pexels) Search(ctx context.Context, query string, page int) (Page, error) {
	query = CleanQuery(query)
	if query == "" {
		return Page{}, errors.New("photosearch: empty query")
	}
	page = min(max(page, 1), MaxPage)
	key := strings.ToLower(query) + "\x00" + strconv.Itoa(page)
	if cached, ok := p.cached(key); ok {
		return cached, nil
	}

	v := url.Values{"query": {query}, "page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}
	var body pexelsSearch
	if err := p.getJSON(ctx, p.opts.APIBase+"/v1/search?"+v.Encode(), &body); err != nil {
		return Page{}, err
	}
	out := Page{Photos: make([]Result, 0, len(body.Photos))}
	for _, ph := range body.Photos {
		thumb, ok := p.imageURL(ph.Src.Medium, nil)
		if !ok || ph.ID <= 0 {
			continue
		}
		out.Photos = append(out.Photos, Result{
			ID: strconv.FormatInt(ph.ID, 10), ThumbnailURL: thumb, Width: ph.Width, Height: ph.Height, Credit: ph.Photographer,
		})
	}
	if body.NextPage != "" && page < MaxPage {
		out.NextPage = page + 1
	}
	p.store(key, out)
	return out, nil
}

var photoIDRe = regexp.MustCompile(`^[0-9]{1,20}$`)

// Fetch downloads the photo with this ID at the width the app keeps. The
// address comes from Pexels' own answer about the photo, never from the
// caller, and must be on the Pexels image host.
func (p *Pexels) Fetch(ctx context.Context, id string) (Image, error) {
	if !photoIDRe.MatchString(id) {
		return Image{}, ErrNotFound
	}
	var ph pexelsPhoto
	if err := p.getJSON(ctx, p.opts.APIBase+"/v1/photos/"+id, &ph); err != nil {
		return Image{}, err
	}
	src, ok := p.imageURL(ph.Src.Original, url.Values{"auto": {"compress"}, "cs": {"tinysrgb"}, "w": {photoWidth}})
	if !ok {
		return Image{}, fmt.Errorf("photosearch: photo %s has no usable address", id)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return Image{}, err
	}
	res, err := p.opts.Client.Do(req)
	if err != nil {
		return Image{}, fmt.Errorf("photosearch: download: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("photosearch: download status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, int64(p.opts.MaxBytes)+1))
	if err != nil {
		return Image{}, fmt.Errorf("photosearch: download: %w", err)
	}
	if len(data) > p.opts.MaxBytes {
		return Image{}, ErrTooBig
	}
	return Image{Data: data, Credit: ph.Photographer}, nil
}

// imageURL checks raw is on the image host and returns it, with query in
// place of its own when given.
func (p *Pexels) imageURL(raw string, query url.Values) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != p.opts.ImageScheme || u.User != nil || !strings.EqualFold(u.Host, p.opts.ImageHost) {
		return "", false
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	u.Fragment = ""
	return u.String(), true
}

func (p *Pexels) getJSON(ctx context.Context, address string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", p.opts.APIKey)
	req.Header.Set("Accept", "application/json")
	res, err := p.opts.Client.Do(req)
	if err != nil {
		return fmt.Errorf("photosearch: pexels: %w", err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("photosearch: pexels status %d", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("photosearch: pexels: %w", err)
	}
	return nil
}

func (p *Pexels) cached(key string) (Page, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.cache[key]
	if !ok || !p.opts.Now().Before(c.expires) {
		return Page{}, false
	}
	return c.page, true
}

func (p *Pexels) store(key string, page Page) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.opts.Now()
	if len(p.cache) >= cacheEntries {
		var oldest string
		var oldestAt time.Time
		for k, c := range p.cache {
			if !now.Before(c.expires) {
				delete(p.cache, k)
				continue
			}
			if oldest == "" || c.expires.Before(oldestAt) {
				oldest, oldestAt = k, c.expires
			}
		}
		if len(p.cache) >= cacheEntries {
			delete(p.cache, oldest)
		}
	}
	p.cache[key] = cachedPage{page: page, expires: now.Add(cacheTTL)}
}
