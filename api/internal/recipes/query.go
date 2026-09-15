package recipes

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Sort orders recipe lists. Ties always break by name, then ID.
type Sort string

// Supported list orders.
const (
	// SortName is alphabetical, case-insensitive.
	SortName Sort = "name"
	// SortRecent is most recently ordered first; never-ordered recipes last.
	SortRecent Sort = "recent"
	// SortPopular is most often ordered first.
	SortPopular Sort = "popular"
)

// List limits.
const (
	DefaultListLimit = 50
	MaxListLimit     = 200
	maxFilterLength  = 100
)

// ListQuery selects a page of recipes.
type ListQuery struct {
	// Search matches names containing the text, case-insensitively. It is
	// literal text, not a pattern.
	Search string
	// Addons, when set, keeps only add-ons (true) or only main meals (false).
	Addons  *bool
	Tag     string
	Cuisine string
	Sort    Sort
	// Cursor is the NextCursor of the previous page.
	Cursor string
	// Limit defaults to DefaultListLimit and is capped at MaxListLimit.
	Limit int
}

// ListPage is one page of recipe summaries.
type ListPage struct {
	Items      []RecipeSummary
	NextCursor string
}

// ListFilter is a validated ListQuery as stores receive it.
type ListFilter struct {
	// SearchPattern is a regular expression with metacharacters escaped. Match
	// it case-insensitively against the name.
	SearchPattern string
	Addons        *bool
	// Tag and Cuisine match case-insensitively.
	Tag     string
	Cuisine string
	Sort    Sort
	// After, when set, returns only recipes after this position in Sort order.
	After *Position
	// Limit is the maximum number of summaries to return.
	Limit int
}

// Position is a recipe's place in a list order.
type Position struct {
	Name            string
	LastOrderedWeek string
	TimesOrdered    int
	ID              string
}

func positionOf(s RecipeSummary) Position {
	return Position{Name: s.Name, LastOrderedWeek: s.LastOrderedWeek, TimesOrdered: s.TimesOrdered, ID: s.ID}
}

// filter validates q. The returned Limit is the page size.
func (q ListQuery) filter() (ListFilter, error) {
	f := ListFilter{
		Addons:  q.Addons,
		Tag:     strings.TrimSpace(q.Tag),
		Cuisine: strings.TrimSpace(q.Cuisine),
		Sort:    q.Sort,
		Limit:   q.Limit,
	}
	if f.Sort == "" {
		f.Sort = SortName
	}
	switch f.Sort {
	case SortName, SortRecent, SortPopular:
	default:
		return ListFilter{}, fmt.Errorf("%w: sort must be name, recent, or popular", ErrInvalidQuery)
	}
	switch {
	case f.Limit < 0:
		return ListFilter{}, fmt.Errorf("%w: limit must not be negative", ErrInvalidQuery)
	case f.Limit == 0:
		f.Limit = DefaultListLimit
	case f.Limit > MaxListLimit:
		f.Limit = MaxListLimit
	}
	search := strings.TrimSpace(q.Search)
	if utf8.RuneCountInString(search) > maxFilterLength || utf8.RuneCountInString(f.Tag) > maxFilterLength || utf8.RuneCountInString(f.Cuisine) > maxFilterLength {
		return ListFilter{}, fmt.Errorf("%w: search, tag, and cuisine must be at most %d characters", ErrInvalidQuery, maxFilterLength)
	}
	if search != "" {
		f.SearchPattern = regexp.QuoteMeta(search)
	}
	if q.Cursor != "" {
		pos, err := decodeCursor(q.Cursor, f.Sort)
		if err != nil {
			return ListFilter{}, err
		}
		f.After = &pos
	}
	return f, nil
}

type cursorPayload struct {
	Sort  Sort   `json:"s"`
	Name  string `json:"n"`
	Week  string `json:"w,omitempty"`
	Times int    `json:"t,omitempty"`
	ID    string `json:"i"`
}

func encodeCursor(sort Sort, p Position) string {
	b, _ := json.Marshal(cursorPayload{Sort: sort, Name: p.Name, Week: p.LastOrderedWeek, Times: p.TimesOrdered, ID: p.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string, sort Sort) (Position, error) {
	invalid := fmt.Errorf("%w: cursor is invalid", ErrInvalidQuery)
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Position{}, invalid
	}
	var c cursorPayload
	if err := json.Unmarshal(b, &c); err != nil || c.ID == "" {
		return Position{}, invalid
	}
	if c.Sort != sort {
		return Position{}, fmt.Errorf("%w: cursor belongs to a different sort", ErrInvalidQuery)
	}
	return Position{Name: c.Name, LastOrderedWeek: c.Week, TimesOrdered: c.Times, ID: c.ID}, nil
}
