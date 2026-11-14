package providers

import (
	"context"
	"math"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// HandoffListPage means the provider turns the list into a page of its own,
// matching products by name; the member picks the store, fills the cart, and
// checks out there (Instacart's shopping list page). Lines need no saved
// product.
const HandoffListPage HandoffKind = "list_page"

// ListMeasurement is one way to measure a list item for a list page:
// Instacart's line_item_measurements entry.
type ListMeasurement struct {
	Quantity float64
	// Unit is the provider's unit ("cup", "each").
	Unit string
}

// ListItem is one ingredient on a list page.
type ListItem struct {
	// Name is the search term the provider matches products with.
	Name         string
	Measurements []ListMeasurement
}

// ListPageMaker is a provider whose handoff is a hosted list page. It makes
// the page for items and returns its link. It does I/O, unlike the rest of
// GroceryProvider.
type ListPageMaker interface {
	MakeListPage(ctx context.Context, title string, items []ListItem) (string, error)
}

// Instacart is the pure half of the Instacart provider: no product IDs and
// no stores, since Instacart matches products by name and the member picks
// the store on Instacart. internal/instacart adds the API call that makes the
// page.
type Instacart struct {
	affiliate bool
}

var _ GroceryProvider = (*Instacart)(nil)

// NewInstacart returns the Instacart provider. affiliate says the API key is
// tied to an Impact account, so Instacart adds affiliate tracking to every
// page link.
func NewInstacart(affiliate bool) *Instacart { return &Instacart{affiliate: affiliate} }

// Key implements GroceryProvider.
func (*Instacart) Key() Key { return KeyInstacart }

// Name implements GroceryProvider.
func (*Instacart) Name() string { return "Instacart" }

// CatalogKeys implements CatalogListed.
func (*Instacart) CatalogKeys() []string { return []string{"instacart"} }

// HandoffKind implements GroceryProvider.
func (*Instacart) HandoffKind() HandoffKind { return HandoffListPage }

// AffiliateTracked implements GroceryProvider.
func (i *Instacart) AffiliateTracked() bool { return i.affiliate }

// ParseProduct implements GroceryProvider: Instacart picks products itself,
// so there is nothing to paste.
func (*Instacart) ParseProduct(string) (ProductRef, error) {
	return ProductRef{}, invalid("Instacart picks the products for you, so there's no product to save.")
}

// ProductURL implements GroceryProvider: there are no product pages.
func (*Instacart) ProductURL(string) string { return "" }

// NormalizeStoreID implements GroceryProvider: the member picks the store on
// Instacart.
func (*Instacart) NormalizeStoreID(string) (string, error) {
	return "", invalid("You pick the store on Instacart when you shop.")
}

// BuildCartLinks implements GroceryProvider. Every item goes on one page,
// whose URL the service fills in once Instacart has made it, so the link's
// URL here is empty. Items aren't merged: each is a line, matched by name.
func (*Instacart) BuildCartLinks(items []CartItem, store StoreRef) ([]CartLink, error) {
	if store.StoreID != "" {
		return nil, invalid("You pick the store on Instacart when you shop.")
	}
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]CartItem, 0, len(items))
	for _, it := range items {
		if it.Quantity < 1 || it.Quantity > MaxPackages {
			return nil, invalid("quantity must be between 1 and %d", MaxPackages)
		}
		out = append(out, CartItem{ProductID: it.ProductID, Quantity: it.Quantity, LineIDs: append([]string(nil), it.LineIDs...)})
	}
	return []CartLink{{Items: out}}, nil
}

// instacartUnits maps DinnerOS units Instacart measures the same way to
// Instacart's unit names
// (https://docs.instacart.com/developer_platform_api/api/units_of_measurement).
var instacartUnits = map[string]string{
	"count": "each", "can": "can", "package": "package", "bag": "package", "bunch": "bunch",
	"tsp": "teaspoon", "tbsp": "tablespoon", "cup": "cup", "ml": "milliliter", "l": "liter",
	"oz": "ounce", "lb": "pound", "g": "gram", "kg": "kilogram",
}

// InstacartMeasurements turns a line's amounts into Instacart measurements.
// Units Instacart has no name for are bought whole: fluid ounces become
// cups, cloves become heads ("each", about 10 cloves to a head), wedges
// become whole fruit (4 to one), a slice is a package, and a bag is a package. A pinch or a thumb
// is one of the item. Amounts with no usable quantity are dropped, so an
// unmeasured item ("salt") sends none and Instacart picks a size.
func InstacartMeasurements(amounts []ingredients.Amount) []ListMeasurement {
	out := make([]ListMeasurement, 0, len(amounts))
	for _, a := range amounts {
		q := a.Quantity.Float64()
		if q <= 0 || math.IsNaN(q) || math.IsInf(q, 0) {
			continue
		}
		var m ListMeasurement
		switch code := a.Unit.Code; code {
		case "floz":
			m = ListMeasurement{Quantity: q / 8, Unit: "cup"}
		case "clove":
			m = ListMeasurement{Quantity: math.Ceil(q / 10), Unit: "each"}
		case "wedge":
			m = ListMeasurement{Quantity: math.Ceil(q / 4), Unit: "each"}
		case "slice":
			m = ListMeasurement{Quantity: 1, Unit: "package"}
		case "pinch", "thumb":
			m = ListMeasurement{Quantity: 1, Unit: "each"}
		default:
			unit, ok := instacartUnits[code]
			if !ok {
				continue
			}
			m = ListMeasurement{Quantity: q, Unit: unit}
		}
		m.Quantity = math.Round(m.Quantity*100) / 100
		if m.Quantity <= 0 {
			m.Quantity = 0.01
		}
		out = append(out, m)
	}
	return out
}

// InstacartItemName is the search term for an ingredient: its name with
// spacing tidied.
func InstacartItemName(name string) string { return strings.Join(strings.Fields(name), " ") }
