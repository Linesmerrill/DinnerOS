package providers

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Walmart link format and limits.
const (
	// WalmartCartURL is the consolidated add-to-cart link
	// (https://walmart.io/docs/atc/v1/add-to-cart).
	WalmartCartURL = "https://www.walmart.com/sc/cart/addToCart"
	// WalmartProductURL prefixes a canonical product page.
	WalmartProductURL = "https://www.walmart.com/ip/"
	// WalmartImpactURL prefixes an Impact affiliate tracking link.
	WalmartImpactURL = "https://goto.walmart.com/m/"

	// MaxCartURLLength bounds one handoff link, including an affiliate
	// wrapper. Walmart documents no limit; 2,000 characters is the
	// conservative length every browser, app, and proxy accepts.
	MaxCartURLLength = 2000
	// MaxItemsPerCartLink bounds the products in one link, so a failure
	// Walmart reports for one product affects a small cart.
	MaxItemsPerCartLink = 40
	// MaxPackages is the most packages of one product a link asks for.
	MaxPackages = 99
	// MaxProductInputLength bounds pasted product text.
	MaxProductInputLength = 2048
)

var (
	// walmartItemID is a Walmart item ID: digits, no leading zero.
	walmartItemID = regexp.MustCompile(`^[1-9][0-9]{4,14}$`)
	digitsOnly    = regexp.MustCompile(`^[0-9]+$`)
	// walmartStoreID is a Walmart store number.
	walmartStoreID = regexp.MustCompile(`^[1-9][0-9]{0,5}$`)
	// impactID is one numeric Impact identifier.
	impactID = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// walmartHosts are the only hosts a pasted product link may name.
var walmartHosts = map[string]bool{"walmart.com": true, "www.walmart.com": true}

// ImpactAffiliate wraps handoff links in Impact tracking links. All three
// IDs come from the Impact dashboard (docs/shopping-providers.md#credentials).
type ImpactAffiliate struct {
	PublisherID string
	AdID        string
	CampaignID  string
}

// Valid reports whether every ID is set and numeric.
func (a ImpactAffiliate) Valid() bool {
	return impactID.MatchString(a.PublisherID) && impactID.MatchString(a.AdID) && impactID.MatchString(a.CampaignID)
}

// Wrap returns the Impact tracking link for target.
func (a ImpactAffiliate) Wrap(target string) string {
	return WalmartImpactURL + a.PublisherID + "/" + a.AdID + "/" + a.CampaignID + "?veh=aff&u=" + url.QueryEscape(target)
}

// WalmartOptions configures the Walmart provider.
type WalmartOptions struct {
	// Affiliate, when set and valid, wraps every cart link. Nil leaves links
	// untracked, which is the Phase 8a default.
	Affiliate *ImpactAffiliate
}

// Walmart is the Walmart provider: products saved from pasted walmart.com
// links, handed off as add-to-cart links.
type Walmart struct {
	affiliate *ImpactAffiliate
}

var _ GroceryProvider = (*Walmart)(nil)

// NewWalmart returns the Walmart provider. An invalid affiliate config is
// ignored (config.Load rejects one first).
func NewWalmart(o WalmartOptions) *Walmart {
	w := &Walmart{}
	if o.Affiliate != nil && o.Affiliate.Valid() {
		a := *o.Affiliate
		w.affiliate = &a
	}
	return w
}

// Key implements GroceryProvider.
func (*Walmart) Key() Key { return KeyWalmart }

// Name implements GroceryProvider.
func (*Walmart) Name() string { return "Walmart" }

// HandoffKind implements GroceryProvider.
func (*Walmart) HandoffKind() HandoffKind { return HandoffCartLink }

// AffiliateTracked implements GroceryProvider.
func (w *Walmart) AffiliateTracked() bool { return w.affiliate != nil }

// ProductURL implements GroceryProvider.
func (*Walmart) ProductURL(productID string) string { return WalmartProductURL + productID }

const walmartLinkHelp = "paste a Walmart product link such as https://www.walmart.com/ip/product-name/123456789, or its item ID"

// ParseProduct implements GroceryProvider. It accepts a Walmart item ID, or
// a walmart.com product link (/ip/<id> or /ip/<slug>/<id>, with any query
// string or fragment). The link is never fetched: short links (walmrt.us)
// and other shapes are rejected.
func (w *Walmart) ParseProduct(input string) (ProductRef, error) {
	s := strings.TrimSpace(input)
	switch {
	case s == "":
		return ProductRef{}, invalid("%s", walmartLinkHelp)
	case len(s) > MaxProductInputLength:
		return ProductRef{}, invalid("a product link must be at most %d characters", MaxProductInputLength)
	case digitsOnly.MatchString(s):
		if !walmartItemID.MatchString(s) {
			return ProductRef{}, invalid("a Walmart item ID is 5 to 15 digits")
		}
		return ProductRef{ProductID: s, URL: w.ProductURL(s)}, nil
	}
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return ProductRef{}, invalid("%s", walmartLinkHelp)
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "walmart.com/") || strings.HasPrefix(lower, "www.walmart.com/") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Opaque != "" {
		return ProductRef{}, invalid("%s", walmartLinkHelp)
	}
	if u.User != nil || u.Port() != "" || !walmartHosts[strings.ToLower(u.Hostname())] {
		return ProductRef{}, invalid("only walmart.com product links are supported; %s", walmartLinkHelp)
	}
	segments := strings.Split(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), "/"), "/")
	var id, slug string
	switch {
	case len(segments) == 2 && segments[0] == "ip":
		id = segments[1]
	case len(segments) == 3 && segments[0] == "ip" && segments[1] != "":
		id, slug = segments[2], segments[1]
	default:
		return ProductRef{}, invalid("that walmart.com link isn't a product page; %s", walmartLinkHelp)
	}
	if !walmartItemID.MatchString(id) {
		return ProductRef{}, invalid("that walmart.com link has no item ID; %s", walmartLinkHelp)
	}
	ref := ProductRef{ProductID: id, URL: w.ProductURL(id)}
	if words := slugWords(slug); len(words) > 0 {
		ref.Name = strings.Join(words, " ")
		ref.Size = slugSize(words)
	}
	return ref, nil
}

// slugWords splits a product-page slug into its words. A Walmart slug is the
// product title with spaces and punctuation replaced by hyphens
// ("Garlic-Bulb-Fresh-Whole-Each"), so the words come back but the original
// punctuation does not: the name is "Garlic Bulb Fresh Whole Each", not
// "Garlic Bulb Fresh Whole, Each". Empty words are dropped, and a slug of
// nothing but separators gives no words.
func slugWords(slug string) []string {
	if slug == "" || len(slug) > MaxProductInputLength {
		return nil
	}
	words := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' || r == '+' })
	out := make([]string, 0, len(words))
	for _, word := range words {
		if word = strings.TrimSpace(word); word != "" {
			out = append(out, word)
		}
	}
	return out
}

// slugUnits maps the size words a Walmart slug uses to DinnerOS unit codes.
// Every result is checked against ingredients.LookupUnit, so a code this map
// gets wrong yields no size rather than a wrong one.
var slugUnits = map[string]string{
	"oz": "oz", "ounce": "oz", "ounces": "oz",
	"floz": "floz",
	"lb":   "lb", "lbs": "lb", "pound": "lb", "pounds": "lb",
	"g": "g", "gram": "g", "grams": "g",
	"ml": "ml", "milliliter": "ml", "milliliters": "ml",
	"ct": "count", "count": "count", "each": "count",
}

// slugSize reads a package size from a slug's trailing words: "Test-Rice-2-lb"
// is 2 lb, "Eggs-Large-12-Count" is 12 ct, and a trailing "Each" is 1 ct.
//
// It returns nil rather than a guess whenever the words are ambiguous. The
// important case is a decimal written as two numbers: "Chicken-2-5-lb" is
// 2.5 lb, not 5 lb, and a slug can't tell the two apart, so a number
// preceded by another number yields no size at all. Walmart's own page is
// never fetched, so an unreadable slug means the member fills the size in.
func slugSize(words []string) *ingredients.Amount {
	if len(words) == 0 {
		return nil
	}
	unitWord, rest := strings.ToLower(words[len(words)-1]), words[:len(words)-1]
	// "fl oz" arrives as two words.
	if unitWord == "oz" && len(rest) > 0 && strings.EqualFold(rest[len(rest)-1], "fl") {
		unitWord, rest = "floz", rest[:len(rest)-1]
	}
	code, ok := slugUnits[unitWord]
	if !ok {
		return nil
	}
	unit, err := ingredients.LookupUnit(code)
	if err != nil {
		return nil
	}
	// "Each" names a single item and carries no number of its own.
	if unitWord == "each" {
		return &ingredients.Amount{Quantity: ingredients.NewQuantity(1, 1), Unit: unit}
	}
	if len(rest) == 0 {
		return nil
	}
	number := rest[len(rest)-1]
	quantity, err := ingredients.ParseQuantity(number)
	if err != nil || quantity.IsZero() {
		return nil
	}
	// A number right before the number is an unreadable decimal ("2-5-lb").
	if len(rest) > 1 {
		if _, err := ingredients.ParseQuantity(rest[len(rest)-2]); err == nil {
			return nil
		}
	}
	return &ingredients.Amount{Quantity: quantity, Unit: unit}
}

// NormalizeStoreID implements GroceryProvider: a Walmart store number is 1
// to 6 digits. Leading zeros are dropped.
func (*Walmart) NormalizeStoreID(storeID string) (string, error) {
	s := strings.TrimSpace(storeID)
	if digitsOnly.MatchString(s) {
		s = strings.TrimLeft(s, "0")
	}
	if !walmartStoreID.MatchString(s) {
		return "", invalid("storeId must be a Walmart store number (1 to 6 digits)")
	}
	return s, nil
}

// BuildCartLinks implements GroceryProvider. Each link is
//
//	https://www.walmart.com/sc/cart/addToCart?items=ID_QTY,ID&storeId=N
//
// with a quantity suffix only above 1, in item order, merged by product.
// A new link starts when adding an item would pass MaxCartURLLength (after
// any affiliate wrapper) or MaxItemsPerCartLink. Every link carries the
// store. No items is no links.
func (w *Walmart) BuildCartLinks(items []CartItem, store StoreRef) ([]CartLink, error) {
	storeID := ""
	if store.StoreID != "" {
		var err error
		if storeID, err = w.NormalizeStoreID(store.StoreID); err != nil {
			return nil, err
		}
	}
	var merged []CartItem
	index := map[string]int{}
	for _, it := range items {
		switch {
		case !walmartItemID.MatchString(it.ProductID):
			return nil, invalid("%q is not a Walmart item ID", it.ProductID)
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

	var links []CartLink
	var current []CartItem
	for _, it := range merged {
		candidate := append(append([]CartItem(nil), current...), it)
		if len(current) > 0 && (len(candidate) > MaxItemsPerCartLink || len(w.cartURL(candidate, storeID)) > MaxCartURLLength) {
			links = append(links, CartLink{URL: w.cartURL(current, storeID), Items: current})
			candidate = []CartItem{it}
		}
		current = candidate
	}
	if len(current) > 0 {
		links = append(links, CartLink{URL: w.cartURL(current, storeID), Items: current})
	}
	return links, nil
}

// cartURL renders one link, wrapped for the affiliate when configured.
func (w *Walmart) cartURL(items []CartItem, storeID string) string {
	var b strings.Builder
	b.WriteString(WalmartCartURL)
	b.WriteString("?items=")
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(it.ProductID)
		if it.Quantity > 1 {
			b.WriteByte('_')
			b.WriteString(strconv.Itoa(it.Quantity))
		}
	}
	if storeID != "" {
		b.WriteString("&storeId=")
		b.WriteString(storeID)
	}
	if w.affiliate != nil {
		return w.affiliate.Wrap(b.String())
	}
	return b.String()
}
