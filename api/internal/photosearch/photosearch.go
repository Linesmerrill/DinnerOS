// Package photosearch finds photos for a recipe by its name, so a member can
// pick one instead of taking their own (decision 691). The search runs on the
// server so no provider key ships in the app, and the phone never hands the
// server an address to download: it names a result by its ID, and the
// provider's own API says where that photo is.
package photosearch

import (
	"context"
	"errors"
)

// Result is one photo in a page of search results.
type Result struct {
	ID           string
	ThumbnailURL string
	Width        int
	Height       int
	// Credit names the photographer, for showing under the photo.
	Credit string
}

// Page is one page of results. NextPage is 0 when there are no more.
type Page struct {
	Photos   []Result
	NextPage int
}

// Image is a downloaded photo, ready to store.
type Image struct {
	Data   []byte
	Credit string
}

// Searcher finds photos and downloads one by its ID.
type Searcher interface {
	Search(ctx context.Context, query string, page int) (Page, error)
	Fetch(ctx context.Context, id string) (Image, error)
}

// ErrNotFound means no photo has that ID.
var ErrNotFound = errors.New("photosearch: photo not found")

// ErrTooBig means the photo is larger than the caller allows.
var ErrTooBig = errors.New("photosearch: photo too big")
