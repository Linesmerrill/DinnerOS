package catalog

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Errors returned by the store and the service.
var (
	ErrNotFound = errors.New("catalog: not found")
	// ErrInvalidQuery means browse or search parameters, including the
	// cursor, are invalid. Its message after the prefix is safe to show.
	ErrInvalidQuery = errors.New("catalog: invalid query")
	// ErrNotPublishable means a recipe may not enter the catalog: its source
	// is not public and its household has not shared it.
	ErrNotPublishable = errors.New("catalog: not publishable")
)

// Recipe is one entry in the global catalog.
type Recipe struct {
	ID string
	// CatalogKey is the entry's identity (recipes.CatalogKey). It is unique.
	CatalogKey string
	// Content is the recipe itself. Its HouseholdID, OrderWeeks,
	// TimesOrdered, LastOrderedWeek, SharedToCatalog, and CatalogKey are
	// always zero: the catalog holds content, never a household's history.
	// Its ID is the catalog entry's ID.
	Content recipes.Recipe
	// FirstPublishedAt is when the first household published this recipe;
	// UpdatedAt is the last time any household's copy refreshed it.
	FirstPublishedAt time.Time
	UpdatedAt        time.Time
}

// Content returns a household-free copy of a library recipe, ready to store
// as a catalog entry or to copy into another household.
func Content(r recipes.Recipe) recipes.Recipe {
	r.ID = ""
	r.HouseholdID = ""
	r.OrderWeeks = nil
	r.TimesOrdered = 0
	r.LastOrderedWeek = ""
	r.SharedToCatalog = false
	r.CatalogKey = ""
	// Categories and images are filled from the ingredient catalog on read,
	// never stored on a recipe, so they are dropped here too.
	for i := range r.Ingredients {
		r.Ingredients[i].Category = ""
		r.Ingredients[i].ImageURL = ""
	}
	return r
}

// Result is one catalog recipe as a browse or search result.
type Result struct {
	Recipe
	// LibraryRecipeID is the household's own copy of this recipe, empty when
	// it has none. Discovery never returns results with one set; search
	// marks them.
	LibraryRecipeID string
	// Score is the discovery ranking score (ranking.go). It is 0 for search.
	Score float64
	// Reasons explain the score in the household's own words ("you like
	// Thai"). Empty for search and for a household with no taste profile.
	Reasons []string
}

// Page is one page of results.
type Page struct {
	Items []Result
	// NextCursor is empty on the last page.
	NextCursor string
	// Total is the number of catalog entries the query matched before
	// paging, -1 when it was not counted.
	Total int
}

// Paging limits. The catalog is a few thousand entries, so browse and search
// read a bounded prefix rather than the whole collection.
const (
	DefaultLimit = 24
	MaxLimit     = 100
	// ScanLimit bounds how many catalog entries one discovery request ranks.
	// Ranking is in-memory (ranking.go) because it reuses Autopilot's taste
	// signals, which Mongo cannot express; the bound is what keeps that
	// affordable. Past a few thousand entries this becomes a stored,
	// periodically recomputed ranking instead.
	ScanLimit = 2000
	// MaxOffset caps how deep search paging goes. Relevance order is not a
	// stable key, so search pages by offset rather than by keyset, and deep
	// offsets are both slow and meaningless.
	MaxOffset       = 1000
	maxQueryLength  = 100
	maxFilterLength = 100
)

// Query selects a page of the catalog.
type Query struct {
	// Text matches name, headline, ingredient names, cuisines, and tags. It
	// is literal text, not a pattern. Empty means "everything".
	Text string
	// Cuisine, Tag, and Ingredient narrow the result set exactly,
	// case-insensitively. Ingredient matches an ingredient line's name.
	Cuisine    string
	Tag        string
	Ingredient string
	// ExcludeLibrary drops entries the household already has. Discovery sets
	// it; search does not, and marks them instead.
	ExcludeLibrary bool
	Cursor         string
	Limit          int
}

// Filter is a validated Query as the store receives it.
type Filter struct {
	Text       string
	Cuisine    string
	Tag        string
	Ingredient string
	Offset     int
	Limit      int
}

// validate checks q and resolves its cursor to an offset.
func (q Query) validate() (Filter, error) {
	f := Filter{
		Text:       strings.TrimSpace(q.Text),
		Cuisine:    strings.TrimSpace(q.Cuisine),
		Tag:        strings.TrimSpace(q.Tag),
		Ingredient: strings.TrimSpace(q.Ingredient),
		Limit:      q.Limit,
	}
	if utf8.RuneCountInString(f.Text) > maxQueryLength {
		return Filter{}, fmt.Errorf("%w: q must be at most %d characters", ErrInvalidQuery, maxQueryLength)
	}
	for _, v := range []string{f.Cuisine, f.Tag, f.Ingredient} {
		if utf8.RuneCountInString(v) > maxFilterLength {
			return Filter{}, fmt.Errorf("%w: cuisine, tag, and ingredient must be at most %d characters", ErrInvalidQuery, maxFilterLength)
		}
	}
	switch {
	case f.Limit < 0:
		return Filter{}, fmt.Errorf("%w: limit must not be negative", ErrInvalidQuery)
	case f.Limit == 0:
		f.Limit = DefaultLimit
	case f.Limit > MaxLimit:
		f.Limit = MaxLimit
	}
	offset, err := decodeCursor(q.Cursor)
	if err != nil {
		return Filter{}, err
	}
	f.Offset = offset
	return f, nil
}

// encodeCursor returns the cursor for the page starting at offset, or "" when
// there is no next page.
func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o" + strconv.Itoa(offset)))
}

func decodeCursor(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	invalid := fmt.Errorf("%w: cursor is invalid", ErrInvalidQuery)
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) < 2 || b[0] != 'o' {
		return 0, invalid
	}
	offset, err := strconv.Atoi(string(b[1:]))
	if err != nil || offset < 0 {
		return 0, invalid
	}
	if offset > MaxOffset {
		return 0, fmt.Errorf("%w: cursor is past the end of the catalog", ErrInvalidQuery)
	}
	return offset, nil
}
