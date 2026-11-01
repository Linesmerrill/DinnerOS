package shopping

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file is store and product search for providers that have it
// (Kroger): the store picker, and choosing a product from the store's own
// catalog instead of pasting a link (decision 649).

// Search limits.
const (
	// MaxSearchTermLength bounds a product search.
	MaxSearchTermLength = 100
	// minSearchTermLength is the shortest search Kroger accepts.
	minSearchTermLength = 3
	// maxSearchWords is the most words Kroger accepts in one search.
	maxSearchWords = 8
)

// ErrSearchUnavailable means the provider's search failed or is rate
// limiting; trying again later may work.
var ErrSearchUnavailable = errors.New("shopping: provider search unavailable")

var zipCode = regexp.MustCompile(`^[0-9]{5}$`)

// SearchResult is a product from a provider's search, priced and stocked at
// the household's store when it has one.
type SearchResult struct {
	providers.Product
	URL string
	// PackageSize is the size read from the product's stated size, or nil.
	PackageSize *PackageSize
}

// FindStores lists the provider's stores near a ZIP code.
func (s *Service) FindStores(ctx context.Context, householdID, provider, zip string) ([]providers.Store, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	p, err := s.Provider(provider)
	if err != nil {
		return nil, err
	}
	finder, ok := p.(providers.StoreFinder)
	if !ok {
		return nil, invalid("%s has no store finder; type the store number", p.Name())
	}
	zip = strings.TrimSpace(zip)
	if !zipCode.MatchString(zip) {
		return nil, invalid("zip must be a 5-digit ZIP code")
	}
	stores, err := finder.FindStores(ctx, providers.StoreQuery{ZIPCode: zip})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSearchUnavailable, err)
	}
	return stores, nil
}

// SearchProducts searches the provider's catalog at the household's store.
func (s *Service) SearchProducts(ctx context.Context, householdID, provider, term string) ([]SearchResult, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	p, err := s.ProviderFor(ctx, householdID, provider)
	if err != nil {
		return nil, err
	}
	searcher, ok := p.(providers.ProductSearcher)
	if !ok {
		return nil, invalid("%s has no product search; paste a product link", p.Name())
	}
	words := strings.Fields(term)
	if len(words) > maxSearchWords {
		words = words[:maxSearchWords]
	}
	term = strings.Join(words, " ")
	switch n := utf8.RuneCountInString(term); {
	case n < minSearchTermLength:
		return nil, invalid("search for at least %d letters", minSearchTermLength)
	case n > MaxSearchTermLength:
		return nil, invalid("a search must be at most %d characters", MaxSearchTermLength)
	}
	settings, err := s.Settings(ctx, householdID)
	if err != nil {
		return nil, err
	}
	q := providers.SearchQuery{Term: term}
	if store, ok := settings.Store(p.Key()); ok {
		q.StoreID = store.StoreID
	}
	products, err := searcher.SearchProducts(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSearchUnavailable, err)
	}
	out := make([]SearchResult, 0, len(products))
	for _, product := range products {
		r := SearchResult{Product: product, URL: p.ProductURL(product.ProductID)}
		if a := providers.ParseSize(product.Size); a != nil {
			r.PackageSize = &PackageSize{Quantity: a.Quantity.String(), Unit: a.Unit.Code}
		}
		out = append(out, r)
	}
	return out, nil
}
