package providers

import (
	"context"
	"fmt"
	"slices"
)

// Key identifies a provider in routes, stored documents, and responses.
type Key string

// Provider keys. Only Walmart is implemented; the others are known so a
// request for them gets 503 provider_unavailable instead of 404.
const (
	KeyWalmart   Key = "walmart"
	KeyInstacart Key = "instacart"
	KeyKroger    Key = "kroger"
)

// KnownKeys lists every provider DinnerOS plans to support, implemented or
// not, in a stable order.
var KnownKeys = []Key{KeyWalmart, KeyInstacart, KeyKroger}

// Known reports whether k is a planned provider.
func (k Key) Known() bool { return slices.Contains(KnownKeys, k) }

// HandoffKind says how a provider takes a list.
type HandoffKind string

// Handoff kinds.
const (
	// HandoffCartLink is one or more URLs that add products to the user's
	// cart when opened (Walmart's add-to-cart link).
	HandoffCartLink HandoffKind = "cart_link"
)

// ValidationError describes invalid input. Its message is safe to show API
// clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ProductRef is a product read from a pasted link or a raw ID.
type ProductRef struct {
	// ProductID is the provider's product identifier (a Walmart item ID).
	ProductID string
	// URL is the canonical product page, built from ProductID. It is never
	// the pasted text.
	URL string
}

// CartItem is one product and a package count for a handoff. LineIDs are
// the handoff lines it covers.
type CartItem struct {
	ProductID string
	Quantity  int
	LineIDs   []string
}

// CartLink is one handoff URL and the items in it.
type CartLink struct {
	URL   string
	Items []CartItem
}

// StoreRef is the store a handoff targets. An empty StoreID means none.
type StoreRef struct {
	StoreID string
}

// GroceryProvider is a shopping service DinnerOS can hand a list to. Every
// method is pure: implementations never fetch provider pages.
type GroceryProvider interface {
	Key() Key
	// Name is the display name ("Walmart").
	Name() string
	// HandoffKind is how the provider takes a list.
	HandoffKind() HandoffKind
	// AffiliateTracked reports whether handoff links carry affiliate
	// tracking, which apps must disclose.
	AffiliateTracked() bool
	// ParseProduct reads a product from a pasted product link or a raw
	// product ID, from the text alone. Invalid input is a *ValidationError.
	ParseProduct(input string) (ProductRef, error)
	// ProductURL is the canonical product page for an ID ParseProduct
	// accepted.
	ProductURL(productID string) string
	// NormalizeStoreID validates a store identifier typed by a person and
	// returns it in canonical form, or a *ValidationError.
	NormalizeStoreID(storeID string) (string, error)
	// BuildCartLinks turns items into handoff links, split when one link
	// would be too long. Items with the same product are merged. Invalid
	// items are a *ValidationError.
	BuildCartLinks(items []CartItem, store StoreRef) ([]CartLink, error)
}

// Product is a provider catalog product (search and lookup, Phase 8b).
type Product struct {
	ProductID string
	Name      string
	Brand     string
	Size      string
	ImageURL  string
}

// SearchQuery is a product search (Phase 8b).
type SearchQuery struct {
	Term    string
	ZIPCode string
	Limit   int
}

// Store is a provider store (Phase 8b).
type Store struct {
	StoreID string
	Name    string
	Address string
}

// StoreQuery finds stores near a ZIP code (Phase 8b).
type StoreQuery struct {
	ZIPCode string
}

// Optional capabilities, checked with type assertions like
// grocery.OutPantry. No provider implements them in Phase 8a.
type (
	// ProductSearcher searches the provider's catalog.
	ProductSearcher interface {
		SearchProducts(ctx context.Context, q SearchQuery) ([]Product, error)
	}
	// ProductLooker refreshes products (price, stock) by ID.
	ProductLooker interface {
		LookupProducts(ctx context.Context, productIDs []string, store StoreRef) ([]Product, error)
	}
	// StoreFinder lists stores for a store picker.
	StoreFinder interface {
		FindStores(ctx context.Context, q StoreQuery) ([]Store, error)
	}
	// CartWriter adds items to a signed-in customer's cart directly.
	CartWriter interface {
		AddToCart(ctx context.Context, customerToken string, items []CartItem) error
	}
	// OrderImporter reads what a customer ordered.
	OrderImporter interface {
		ImportOrders(ctx context.Context, customerToken string) ([]CartItem, error)
	}
)

// Capabilities describes what a provider can do.
type Capabilities struct {
	Handoff HandoffKind
	// PasteProductLink is true when products are saved from pasted links.
	PasteProductLink bool
	// StoreID is true when handoffs can target a store.
	StoreID       bool
	ProductSearch bool
	ProductLookup bool
	StoreFinder   bool
	CartWrite     bool
	OrderImport   bool
}

// CapabilitiesOf returns p's capabilities, reading the optional ones from
// the interfaces it implements.
func CapabilitiesOf(p GroceryProvider) Capabilities {
	_, search := p.(ProductSearcher)
	_, lookup := p.(ProductLooker)
	_, stores := p.(StoreFinder)
	_, cart := p.(CartWriter)
	_, orders := p.(OrderImporter)
	return Capabilities{
		Handoff: p.HandoffKind(), PasteProductLink: true, StoreID: true,
		ProductSearch: search, ProductLookup: lookup, StoreFinder: stores, CartWrite: cart, OrderImport: orders,
	}
}

// Registry holds the enabled providers.
type Registry struct {
	order []Key
	byKey map[Key]GroceryProvider
}

// NewRegistry returns a registry of ps, in that order. A later provider with
// the same key replaces an earlier one.
func NewRegistry(ps ...GroceryProvider) *Registry {
	r := &Registry{byKey: map[Key]GroceryProvider{}}
	for _, p := range ps {
		if _, ok := r.byKey[p.Key()]; !ok {
			r.order = append(r.order, p.Key())
		}
		r.byKey[p.Key()] = p
	}
	return r
}

// Get returns the enabled provider with key.
func (r *Registry) Get(key Key) (GroceryProvider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.byKey[key]
	return p, ok
}

// Enabled returns the enabled providers in registration order.
func (r *Registry) Enabled() []GroceryProvider {
	if r == nil {
		return nil
	}
	out := make([]GroceryProvider, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.byKey[k])
	}
	return out
}
