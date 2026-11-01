package providers

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// HandoffCartWrite means DinnerOS adds the products to the member's
// signed-in cart itself (Kroger's cart API); the handoff's link only opens
// that cart.
const HandoffCartWrite HandoffKind = "cart_write"

// KrogerBanner is one of the Kroger family's store brands. The Kroger
// Public API serves them all; only the name, the website, and the chain the
// store picker filters by differ.
type KrogerBanner struct {
	Key  string
	Name string
	// Host is the banner's website, where product pages and the cart live.
	Host string
	// Chain is the banner's code in the Locations API ("FRYS").
	Chain string
	// CatalogKey is the banner's entry in the shopping store catalog.
	CatalogKey string
}

// krogerBanners are the banners DinnerOS knows. Fry's is the default. Each
// chain code was checked against the Locations API. Harris Teeter is left out:
// the API lists only its fuel centers, not its grocery stores.
var krogerBanners = []KrogerBanner{
	{Key: "frys", Name: "Fry's", Host: "www.frysfood.com", Chain: "FRYS", CatalogKey: "frys"},
	{Key: "kroger", Name: "Kroger", Host: "www.kroger.com", Chain: "KROGER", CatalogKey: "kroger"},
	{Key: "ralphs", Name: "Ralphs", Host: "www.ralphs.com", Chain: "RALPHS", CatalogKey: "ralphs"},
	{Key: "kingsoopers", Name: "King Soopers", Host: "www.kingsoopers.com", Chain: "KINGSOOPERS", CatalogKey: "king-soopers"},
	{Key: "smiths", Name: "Smith's", Host: "www.smithsfoodanddrug.com", Chain: "SMITHS", CatalogKey: "smiths"},
	{Key: "fredmeyer", Name: "Fred Meyer", Host: "www.fredmeyer.com", Chain: "FRED", CatalogKey: "fred-meyer"},
	{Key: "qfc", Name: "QFC", Host: "www.qfc.com", Chain: "QFC", CatalogKey: "qfc"},
	{Key: "dillons", Name: "Dillons", Host: "www.dillons.com", Chain: "DILLONS", CatalogKey: "dillons"},
	{Key: "citymarket", Name: "City Market", Host: "www.citymarket.com", Chain: "CITYMARKET", CatalogKey: "city-market"},
	{Key: "marianos", Name: "Mariano's", Host: "www.marianos.com", Chain: "MARIANOS", CatalogKey: "marianos"},
	{Key: "picknsave", Name: "Pick 'n Save", Host: "www.picknsave.com", Chain: "PICK N SAVE", CatalogKey: "pick-n-save"},
	{Key: "metromarket", Name: "Metro Market", Host: "www.metromarket.net", Chain: "METRO MARKET", CatalogKey: "metro-market"},
	{Key: "bakers", Name: "Baker's", Host: "www.bakersplus.com", Chain: "BAKERS", CatalogKey: "bakers"},
	{Key: "food4less", Name: "Food 4 Less", Host: "www.food4less.com", Chain: "FOOD4LESS", CatalogKey: "food-4-less"},
	{Key: "foodsco", Name: "Foods Co", Host: "www.foodsco.net", Chain: "FOODSCO", CatalogKey: "foods-co"},
}

// KrogerBannerFor returns the banner named key, or Fry's when key is unknown
// or empty.
func KrogerBannerFor(key string) KrogerBanner {
	for _, b := range krogerBanners {
		if b.Key == strings.ToLower(strings.TrimSpace(key)) {
			return b
		}
	}
	return krogerBanners[0]
}

var (
	// krogerProductID is a Kroger product ID: the 13-digit UPC the cart API
	// takes.
	krogerProductID = regexp.MustCompile(`^[0-9]{13}$`)
	// krogerUPC is a UPC as printed or scanned, 11 to 13 digits.
	krogerUPC = regexp.MustCompile(`^[0-9]{11,13}$`)
	// krogerLocationID is a Kroger store's location ID.
	krogerLocationID = regexp.MustCompile(`^[0-9A-Za-z]{8}$`)
)

// Kroger is the pure half of the Kroger provider: product IDs, links, and
// stores, with no I/O. internal/kroger adds search, stores, the member's
// account, and cart writes around it.
type Kroger struct {
	banner KrogerBanner
}

// NewKroger returns the Kroger provider for a banner key ("frys").
func NewKroger(banner string) *Kroger { return &Kroger{banner: KrogerBannerFor(banner)} }

// Banner is the store brand the provider shows.
func (k *Kroger) Banner() KrogerBanner { return k.banner }

// Key implements GroceryProvider.
func (*Kroger) Key() Key { return KeyKroger }

// Name implements GroceryProvider: the banner's name ("Fry's").
func (k *Kroger) Name() string { return k.banner.Name }

// CatalogKeys implements CatalogListed: the banner's catalog entry.
func (k *Kroger) CatalogKeys() []string { return []string{k.banner.CatalogKey} }

// HandoffKind implements GroceryProvider.
func (*Kroger) HandoffKind() HandoffKind { return HandoffCartWrite }

// AffiliateTracked implements GroceryProvider: Kroger links carry no
// tracking.
func (*Kroger) AffiliateTracked() bool { return false }

// ProductURL implements GroceryProvider: the product's page on the banner's
// site. The slug is ignored by the site, so any word works.
func (k *Kroger) ProductURL(productID string) string {
	return "https://" + k.banner.Host + "/p/item/" + productID
}

// CartURL is the banner's cart page, where the member checks out.
func (k *Kroger) CartURL() string { return "https://" + k.banner.Host + "/cart" }

func (k *Kroger) linkHelp() string {
	return "paste a " + k.banner.Name + " product link such as https://" + k.banner.Host + "/p/product-name/0001111041700, or its UPC"
}

// ParseProduct implements GroceryProvider. It accepts a UPC (11 to 13
// digits, padded to 13), or a product link on any Kroger banner's site
// (/p/<slug>/<id>). The link is never fetched.
func (k *Kroger) ParseProduct(input string) (ProductRef, error) {
	s := strings.TrimSpace(input)
	switch {
	case s == "":
		return ProductRef{}, invalid("%s", k.linkHelp())
	case len(s) > MaxProductInputLength:
		return ProductRef{}, invalid("a product link must be at most %d characters", MaxProductInputLength)
	case digitsOnly.MatchString(s):
		if !krogerUPC.MatchString(s) {
			return ProductRef{}, invalid("a UPC is 11 to 13 digits")
		}
		id := padUPC(s)
		return ProductRef{ProductID: id, URL: k.ProductURL(id)}, nil
	}
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return ProductRef{}, invalid("%s", k.linkHelp())
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Opaque != "" || u.User != nil || u.Port() != "" {
		return ProductRef{}, invalid("%s", k.linkHelp())
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	known := false
	for _, b := range krogerBanners {
		if strings.TrimPrefix(b.Host, "www.") == host {
			known = true
			break
		}
	}
	if !known {
		return ProductRef{}, invalid("only Kroger-family store links are supported; %s", k.linkHelp())
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) != 3 || segments[0] != "p" || !krogerProductID.MatchString(segments[2]) {
		return ProductRef{}, invalid("that link isn't a product page; %s", k.linkHelp())
	}
	id := segments[2]
	ref := ProductRef{ProductID: id, URL: k.ProductURL(id)}
	if words := slugWords(segments[1]); len(words) > 0 {
		ref.Name = strings.Join(words, " ")
		ref.Size = slugSize(words)
	}
	return ref, nil
}

// padUPC pads a UPC to the 13 digits Kroger uses.
func padUPC(s string) string { return strings.Repeat("0", 13-len(s)) + s }

// NormalizeStoreID implements GroceryProvider: a Kroger location ID is 8
// letters or digits.
func (*Kroger) NormalizeStoreID(storeID string) (string, error) {
	s := strings.TrimSpace(storeID)
	if !krogerLocationID.MatchString(s) {
		return "", invalid("storeId must be a Kroger location ID (8 letters or digits)")
	}
	return strings.ToUpper(s), nil
}

// BuildCartLinks implements GroceryProvider. DinnerOS writes the items to
// the cart itself, so there is one "link": the banner's cart page, carrying
// every item, merged by product. No items is no links.
func (k *Kroger) BuildCartLinks(items []CartItem, store StoreRef) ([]CartLink, error) {
	if store.StoreID != "" {
		if _, err := k.NormalizeStoreID(store.StoreID); err != nil {
			return nil, err
		}
	}
	var merged []CartItem
	index := map[string]int{}
	for _, it := range items {
		switch {
		case !krogerProductID.MatchString(it.ProductID):
			return nil, invalid("%q is not a Kroger product ID", it.ProductID)
		case it.Quantity < 1 || it.Quantity > MaxPackages:
			return nil, invalid("quantity must be between 1 and %d", MaxPackages)
		}
		if i, ok := index[it.ProductID]; ok {
			merged[i].Quantity = min(merged[i].Quantity+it.Quantity, MaxPackages)
			merged[i].LineIDs = append(merged[i].LineIDs, it.LineIDs...)
			continue
		}
		index[it.ProductID] = len(merged)
		merged = append(merged, CartItem{ProductID: it.ProductID, Quantity: it.Quantity, LineIDs: append([]string(nil), it.LineIDs...)})
	}
	if len(merged) == 0 {
		return nil, nil
	}
	return []CartLink{{URL: k.CartURL(), Items: merged}}, nil
}

var _ GroceryProvider = (*Kroger)(nil)
