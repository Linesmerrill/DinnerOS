package shopping

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

// This file holds the curated catalog of grocers and delivery services
// households ask for, and the pure text handling that searches it. It is
// demand data, not a provider: nothing here implies DinnerOS can hand a list
// to any of these. internal/providers stays the registry of what actually
// works.

// StoreKind groups a catalog entry for display.
type StoreKind string

// Store kinds.
const (
	// KindGrocer is a grocery chain a household shops at.
	KindGrocer StoreKind = "grocer"
	// KindDelivery is a delivery or marketplace service that shops a list.
	KindDelivery StoreKind = "delivery"
	// KindWarehouse is a membership warehouse club.
	KindWarehouse StoreKind = "warehouse"
	// KindOther is anything else.
	KindOther StoreKind = "other"
)

// StoreStatus says how far DinnerOS has got with a store. It comes from
// docs/shopping-providers.md, not from live data.
type StoreStatus string

// Store statuses, in the order the catalog lists them.
const (
	// StatusAvailable: a household can hand a list to it today.
	StatusAvailable StoreStatus = "available"
	// StatusResearched: docs/shopping-providers.md assessed it and recorded
	// what its APIs allow.
	StatusResearched StoreStatus = "researched"
	// StatusUnsupported: no integration and no research yet. Requests for
	// these are the demand signal that decides what gets researched next.
	StatusUnsupported StoreStatus = "unsupported"
)

// statusRank orders statuses for the catalog listing.
func statusRank(s StoreStatus) int {
	switch s {
	case StatusAvailable:
		return 0
	case StatusResearched:
		return 1
	case StatusUnsupported:
		return 2
	}
	return 3
}

// CatalogEntry is one curated store or service.
type CatalogEntry struct {
	// Key identifies the entry in requests and stored documents.
	Key string
	// Name is the display name.
	Name string
	Kind StoreKind
	// Status is what docs/shopping-providers.md concluded.
	Status StoreStatus
	// Aliases are other spellings people type, normalized like Name. They are
	// searched alongside Name and match free-text requests.
	Aliases []string
	// Note is a one-line summary of what the research found, or "" when there
	// is nothing to say.
	Note string
}

// storeCatalog is the curated list. Keys are stable: they are stored on every
// request, so renaming one would orphan the requests that point at it.
//
// Status follows docs/shopping-providers.md: Walmart ships today; the
// providers that document assessed are "researched"; everything else is
// "unsupported" until someone asks for it and the research happens. Banners
// of an assessed chain (Kroger's and Albertsons') inherit the parent's
// status, because the same API and the same terms cover them.
var storeCatalog = []CatalogEntry{
	{
		Key: "walmart", Name: "Walmart", Kind: KindGrocer, Status: StatusAvailable,
		Aliases: []string{"wal mart", "walmart supercenter", "walmart neighborhood market", "walmart plus", "walmart grocery"},
		Note:    "Add-to-cart link handoff, built from products the household saved",
	},

	// Kroger and its banners: one public API with cart write.
	{
		Key: "kroger", Name: "Kroger", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"kroger co", "krogers"},
		Note:    "Public API with cart write; needs customer sign-in",
	},
	{
		Key: "frys", Name: "Fry's", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"frys food", "frys food and drug"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "ralphs", Name: "Ralphs", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"ralphs grocery"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "king-soopers", Name: "King Soopers", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"kingsoopers"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "smiths", Name: "Smith's", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"smiths food and drug"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "fred-meyer", Name: "Fred Meyer", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"fredmeyer"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "qfc", Name: "QFC", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"quality food centers"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "harris-teeter", Name: "Harris Teeter", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"harristeeter"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "dillons", Name: "Dillons", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"dillons food"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "city-market", Name: "City Market", Kind: KindGrocer, Status: StatusResearched,
		Note: "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "marianos", Name: "Mariano's", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"marianos fresh market"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "pick-n-save", Name: "Pick 'n Save", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"picknsave"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "metro-market", Name: "Metro Market", Kind: KindGrocer, Status: StatusResearched,
		Note: "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "bakers", Name: "Baker's", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"bakers supermarkets"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "food-4-less", Name: "Food 4 Less", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"food four less", "food4less"},
		Note:    "Kroger banner; covered by Kroger's cart-write API",
	},
	{
		Key: "foods-co", Name: "Foods Co", Kind: KindGrocer, Status: StatusResearched,
		Note: "Kroger banner; covered by Kroger's cart-write API",
	},

	// Albertsons and its banners: no public cart API, reachable through
	// Instacart.
	{
		Key: "albertsons", Name: "Albertsons", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"albertsons market"},
		Note:    "No public cart API; reachable through Instacart",
	},
	{
		Key: "safeway", Name: "Safeway", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"safeway grocery"},
		Note:    "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "vons", Name: "Vons", Kind: KindGrocer, Status: StatusResearched,
		Note: "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "jewel-osco", Name: "Jewel-Osco", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"jewel", "osco"},
		Note:    "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "acme", Name: "Acme Markets", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"acme", "acme market"},
		Note:    "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "shaws", Name: "Shaw's", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"shaws supermarket"},
		Note:    "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "star-market", Name: "Star Market", Kind: KindGrocer, Status: StatusResearched,
		Note: "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "randalls", Name: "Randalls", Kind: KindGrocer, Status: StatusResearched,
		Note: "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "tom-thumb", Name: "Tom Thumb", Kind: KindGrocer, Status: StatusResearched,
		Note: "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "pavilions", Name: "Pavilions", Kind: KindGrocer, Status: StatusResearched,
		Note: "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "haggen", Name: "Haggen", Kind: KindGrocer, Status: StatusResearched,
		Note: "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "carrs", Name: "Carrs", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"carrs safeway"},
		Note:    "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "united-supermarkets", Name: "United Supermarkets", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"united"},
		Note:    "Albertsons banner; no public cart API, reachable through Instacart",
	},
	{
		Key: "market-street", Name: "Market Street", Kind: KindGrocer, Status: StatusResearched,
		Note: "Albertsons banner (United Supermarkets); no public cart API, reachable through Instacart",
	},

	// Assessed, and not a fit today.
	{
		Key: "target", Name: "Target", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"target grocery"},
		Note:    "Basket transfer is partner-only; no public cart API",
	},
	{
		Key: "whole-foods", Name: "Whole Foods Market", Kind: KindGrocer, Status: StatusResearched,
		Aliases: []string{"whole foods", "wholefoods", "amazon whole foods"},
		Note:    "No public grocery cart API; Amazon's Creators API needs Associates sales history",
	},

	// Grocers no one has assessed yet. These carry no note: "unsupported"
	// means nobody has looked, and a note would imply otherwise.
	{Key: "publix", Name: "Publix", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"publix super markets", "publix supermarket", "publix grocery"}},
	{Key: "heb", Name: "H-E-B", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"heb", "heb plus", "central market"}},
	{Key: "meijer", Name: "Meijer", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"meijers"}},
	{Key: "hy-vee", Name: "Hy-Vee", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"hyvee"}},
	{Key: "wegmans", Name: "Wegmans", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"wegmans food markets"}},
	{Key: "giant", Name: "Giant", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"giant food", "giant food stores", "giant martins"}},
	{Key: "stop-and-shop", Name: "Stop & Shop", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"stop shop", "stop n shop", "stopandshop"}},
	{Key: "food-lion", Name: "Food Lion", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"foodlion"}},
	{Key: "hannaford", Name: "Hannaford", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"hannafords"}},
	{Key: "winn-dixie", Name: "Winn-Dixie", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"winndixie"}},
	{Key: "harveys", Name: "Harveys Supermarket", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"harveys"}},
	{Key: "sprouts", Name: "Sprouts Farmers Market", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"sprouts"}},
	{Key: "trader-joes", Name: "Trader Joe's", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"traderjoes", "trader joe", "tj", "tjs"}},
	{Key: "aldi", Name: "Aldi", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"aldis"}},
	{Key: "lidl", Name: "Lidl", Kind: KindGrocer, Status: StatusUnsupported},
	{Key: "giant-eagle", Name: "Giant Eagle", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"gianteagle", "market district"}},
	{Key: "fresh-market", Name: "The Fresh Market", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"fresh market"}},
	{Key: "natural-grocers", Name: "Natural Grocers", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"vitamin cottage"}},
	{Key: "grocery-outlet", Name: "Grocery Outlet", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"bargain market"}},
	{Key: "smart-and-final", Name: "Smart & Final", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"smart final", "smartandfinal"}},
	{Key: "winco", Name: "WinCo Foods", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"winco"}},
	{Key: "stater-bros", Name: "Stater Bros", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"stater brothers", "staters"}},
	{Key: "raleys", Name: "Raley's", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"bel air"}},
	{Key: "save-mart", Name: "Save Mart", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"savemart", "lucky supermarkets"}},
	{Key: "market-basket", Name: "Market Basket", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"demoulas"}},
	{Key: "wakefern", Name: "ShopRite", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"shoprite", "shop rite", "price rite"}},
	{Key: "weis", Name: "Weis Markets", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"weis"}},
	{Key: "price-chopper", Name: "Price Chopper", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"pricechopper", "market 32"}},
	{Key: "tops", Name: "Tops Friendly Markets", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"tops", "tops markets"}},
	{Key: "schnucks", Name: "Schnucks", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"schnuck markets"}},
	{Key: "ingles", Name: "Ingles Markets", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"ingles"}},
	{Key: "save-a-lot", Name: "Save A Lot", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"savealot"}},
	{Key: "piggly-wiggly", Name: "Piggly Wiggly", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"pigglywiggly"}},
	{Key: "cub-foods", Name: "Cub Foods", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"cub"}},
	{Key: "lowes-foods", Name: "Lowes Foods", Kind: KindGrocer, Status: StatusUnsupported},
	{Key: "brookshires", Name: "Brookshire's", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"brookshire grocery"}},
	{Key: "bashas", Name: "Bashas'", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"bashas food"}},
	{Key: "festival-foods", Name: "Festival Foods", Kind: KindGrocer, Status: StatusUnsupported},
	{Key: "sedanos", Name: "Sedano's", Kind: KindGrocer, Status: StatusUnsupported, Aliases: []string{"sedanos supermarkets"}},

	// Warehouse clubs.
	{Key: "costco", Name: "Costco", Kind: KindWarehouse, Status: StatusUnsupported, Aliases: []string{"costco wholesale"}},
	{Key: "sams-club", Name: "Sam's Club", Kind: KindWarehouse, Status: StatusUnsupported, Aliases: []string{"samsclub", "sams"}},
	{Key: "bjs", Name: "BJ's Wholesale Club", Kind: KindWarehouse, Status: StatusUnsupported, Aliases: []string{"bjs", "bjs wholesale"}},

	// Delivery and marketplace services.
	{
		Key: "instacart", Name: "Instacart", Kind: KindDelivery, Status: StatusResearched,
		Aliases: []string{"instacart shopping"},
		Note:    "Developer Platform shopping-list link; production keys take about 30-40 days",
	},
	{
		Key: "amazon-fresh", Name: "Amazon Fresh", Kind: KindDelivery, Status: StatusResearched,
		Aliases: []string{"amazon", "amazonfresh", "amazon grocery"},
		Note:    "No public grocery cart handoff; PA-API 5 was retired 2026-05-15",
	},
	{
		Key: "shipt", Name: "Shipt", Kind: KindDelivery, Status: StatusResearched,
		Aliases: []string{"shipt delivery"},
		Note:    "Partner-only developer program; no public cart handoff",
	},
	{Key: "doordash", Name: "DoorDash", Kind: KindDelivery, Status: StatusUnsupported, Aliases: []string{"door dash", "dashmart"}},
	{Key: "uber-eats", Name: "Uber Eats", Kind: KindDelivery, Status: StatusUnsupported, Aliases: []string{"ubereats", "uber"}},
	{Key: "gopuff", Name: "Gopuff", Kind: KindDelivery, Status: StatusUnsupported, Aliases: []string{"go puff"}},
	{Key: "freshdirect", Name: "FreshDirect", Kind: KindDelivery, Status: StatusUnsupported, Aliases: []string{"fresh direct"}},
	{Key: "weee", Name: "Weee!", Kind: KindDelivery, Status: StatusUnsupported, Aliases: []string{"weee asian market", "sayweee"}},
	{Key: "misfits-market", Name: "Misfits Market", Kind: KindDelivery, Status: StatusUnsupported, Aliases: []string{"misfits"}},
	{Key: "thrive-market", Name: "Thrive Market", Kind: KindDelivery, Status: StatusUnsupported, Aliases: []string{"thrive"}},
}

// catalogByKey and catalogByAlias index storeCatalog. Both are built once and
// never written again, so reads need no lock.
var (
	catalogByKey   map[string]CatalogEntry
	catalogByAlias map[string]string
)

func init() {
	slices.SortStableFunc(storeCatalog, func(a, b CatalogEntry) int {
		if r := cmp.Compare(statusRank(a.Status), statusRank(b.Status)); r != 0 {
			return r
		}
		return cmp.Compare(a.Name, b.Name)
	})
	catalogByKey = make(map[string]CatalogEntry, len(storeCatalog))
	catalogByAlias = make(map[string]string, len(storeCatalog)*3)
	for _, e := range storeCatalog {
		catalogByKey[e.Key] = e
		catalogByAlias[normalizeStoreText(e.Name)] = e.Key
		catalogByAlias[normalizeStoreText(e.Key)] = e.Key
		for _, a := range e.Aliases {
			catalogByAlias[normalizeStoreText(a)] = e.Key
		}
	}
}

// CatalogEntries returns the whole curated list, ordered by status then name.
func CatalogEntries() []CatalogEntry { return slices.Clone(storeCatalog) }

// CatalogEntryByKey returns the entry with key.
func CatalogEntryByKey(key string) (CatalogEntry, bool) {
	e, ok := catalogByKey[key]
	return e, ok
}

// SearchCatalog returns the entries whose name, key, or one of whose aliases
// starts with q, in catalog order. An empty q returns the whole list.
func SearchCatalog(q string) []CatalogEntry {
	needle := normalizeStoreText(q)
	if needle == "" {
		return CatalogEntries()
	}
	out := make([]CatalogEntry, 0, len(storeCatalog))
	for _, e := range storeCatalog {
		if entryMatches(e, needle) {
			out = append(out, e)
		}
	}
	return out
}

// entryMatches reports whether needle, already normalized, is a prefix of
// e's name, key, or any alias.
func entryMatches(e CatalogEntry, needle string) bool {
	if strings.HasPrefix(normalizeStoreText(e.Name), needle) || strings.HasPrefix(e.Key, needle) {
		return true
	}
	for _, a := range e.Aliases {
		if strings.HasPrefix(normalizeStoreText(a), needle) {
			return true
		}
	}
	return false
}

// matchCatalogName finds the catalog entry a typed store name names, matching
// the whole normalized name against entry names, keys, and aliases. It is
// exact, not a prefix match, so "frys" records as Fry's while "f" records
// nothing.
func matchCatalogName(name string) (CatalogEntry, bool) {
	key, ok := catalogByAlias[normalizeStoreText(name)]
	if !ok {
		return CatalogEntry{}, false
	}
	return catalogByKey[key], true
}

// normalizeStoreText folds a store name for matching: lower case, "&" as
// "and", apostrophes and periods dropped, every other run of non-alphanumeric
// characters a single space, trimmed. "Fry's" becomes "frys", "Stop & Shop"
// becomes "stop and shop", and "H-E-B" becomes "h e b" (which is why such
// entries also carry a run-together alias).
func normalizeStoreText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r == '\'' || r == '’' || r == '.':
			// Dropped, so "fry's" and "frys" fold together.
		case r == '&':
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString("and")
			space = true
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		default:
			space = b.Len() > 0
		}
	}
	return strings.TrimSpace(b.String())
}

// storeKeyOf turns a typed store name into a request key: the normalized text
// with spaces as hyphens ("Some Local Market" becomes "some-local-market").
func storeKeyOf(name string) string {
	return strings.ReplaceAll(normalizeStoreText(name), " ", "-")
}

// displayStoreName collapses whitespace and title cases a typed store name.
// A word in all capitals is left alone, so "HEB" stays "HEB" while
// "some local market" becomes "Some Local Market".
func displayStoreName(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		if strings.ToUpper(w) == w {
			continue
		}
		runes := []rune(strings.ToLower(w))
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
