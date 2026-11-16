// Package photosearch finds photos for a recipe by its name, so a member can
// pick one instead of taking their own (decisions 692, 705). The search runs on
// the server so no provider key ships in the app, and the phone never hands
// the server an address to use: it names a result by its ID, and the
// provider's own API says where that photo is.
package photosearch

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// Result is one photo in a page of search results.
type Result struct {
	ID           string
	ThumbnailURL string
	Width        int
	Height       int
	Credit       Credit
}

// Credit names who took a photo, for "Photo by <name> on Unsplash".
type Credit struct {
	// Name is the photographer's name.
	Name string
	// ProfileURL is the photographer's page, tagged as a referral from DinnerOS.
	ProfileURL string
}

// Picked is a photo a member chose: the address to show it from (the
// provider's own, hotlinked) and who to credit.
type Picked struct {
	ImageURL string
	Credit   Credit
}

// Page is one page of results. NextPage is 0 when there are no more.
type Page struct {
	Photos   []Result
	NextPage int
}

// Searcher finds photos and records a pick.
type Searcher interface {
	Search(ctx context.Context, query string, page int) (Page, error)
	// Pick looks the photo up by its ID, tells the provider it was used, and
	// returns where to show it from.
	Pick(ctx context.Context, id string) (Picked, error)
}

// ErrNotFound means no photo has that ID.
var ErrNotFound = errors.New("photosearch: photo not found")

const (
	// MaxPage keeps a runaway scroll from paging forever.
	MaxPage = 50
	// maxQueryRunes is longer than any recipe name worth searching for.
	maxQueryRunes = 100
)

// CleanQuery is the query as it is searched: single-spaced and not too long.
func CleanQuery(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	for utf8.RuneCountInString(q) > maxQueryRunes {
		_, size := utf8.DecodeLastRuneInString(q)
		q = q[:len(q)-size]
	}
	return strings.TrimSpace(q)
}
