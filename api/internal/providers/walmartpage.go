package providers

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// This file reads a Walmart product page into a ProductCheck. It is pure: the
// HTTP request lives in internal/shopping (productcheck_fetch.go), and this
// only interprets what came back.
//
// The rule it is built around: a page it cannot fully understand is
// ProductUnknown, never ProductFound. Walmart can change its page at any time,
// and treating a changed page as "fine" is exactly how a dead item would slip
// into a cart. So "found" needs every one of: a 200 on a walmart.com /ip/ URL,
// the embedded __NEXT_DATA__ JSON, a product object whose usItemId is the item
// asked for, a non-empty name, and a fulfillmentOptions array to read
// availability from. "gone" needs a 404 on a walmart.com /ip/ URL (Walmart
// redirects a dead item to /ip/seort/<id> and answers 404). Everything else is
// unknown, with a detail saying why.

// ProductStatus is what a product check found.
type ProductStatus string

// Product statuses.
const (
	// ProductFound: the item exists and is in stock for pickup or delivery at
	// the store the page was read for.
	ProductFound ProductStatus = "found"
	// ProductUnavailable: the item exists, but neither pickup nor delivery has
	// it in stock. It may come back.
	ProductUnavailable ProductStatus = "unavailable"
	// ProductGone: Walmart says the item doesn't exist (404).
	ProductGone ProductStatus = "gone"
	// ProductUnknown: the check couldn't tell — a network error, a refusal, or
	// a page it couldn't read. Never treated as good or as gone.
	ProductUnknown ProductStatus = "unknown"
)

// Conclusive reports whether s is an answer about the item (found,
// unavailable, or gone) rather than a failure to get one.
func (s ProductStatus) Conclusive() bool {
	return s == ProductFound || s == ProductUnavailable || s == ProductGone
}

// Why a check is unknown. Stable strings, stored and logged.
const (
	DetailNetwork       = "network"
	DetailBlocked       = "blocked"
	DetailThrottled     = "throttled"
	DetailServerError   = "server_error"
	DetailUnexpected    = "unexpected_status"
	DetailOffSite       = "off_site"
	DetailNoPageData    = "no_page_data"
	DetailNoProduct     = "no_product"
	DetailDifferentItem = "different_item"
	DetailNoFulfillment = "no_fulfillment"
	DetailPaused        = "paused"
)

// Channel availability values Walmart uses that mean "can be bought". Any
// other value (OUT_OF_STOCK, NOT_AVAILABLE, or one we haven't seen) is not.
var inStockValues = map[string]bool{"IN_STOCK": true, "LIMITED_STOCK": true, "AVAILABLE": true}

// ProductCheck is the result of reading one product page.
type ProductCheck struct {
	Status ProductStatus
	// Detail says why the status is unknown, or "".
	Detail string
	// Name is the product's title as Walmart shows it now; "" unless found or
	// unavailable.
	Name string
	// PriceCents is the current price, or nil when the page had none we could
	// read exactly.
	PriceCents *int64
	// Pickup and Delivery are Walmart's availability for each channel
	// ("IN_STOCK", "OUT_OF_STOCK"), or "" when the page didn't list it.
	Pickup   string
	Delivery string
	// Store is the pickup store the page was read for, as Walmart names it
	// ("Example Supercenter"), or "". It is Walmart's default for
	// the request, not necessarily the household's store.
	Store string
}

// nextData finds the page's embedded Next.js data.
var nextData = regexp.MustCompile(`(?s)<script[^>]*\bid="__NEXT_DATA__"[^>]*>(.*?)</script>`)

// walmartPrice is a price string exactly as Walmart prints one: "$0.85",
// "$12", "$1,024.50". Anything else ("$1.24/lb", "From $3") is not read.
var walmartPrice = regexp.MustCompile(`^\$([0-9]{1,3}(?:,[0-9]{3})*|[0-9]+)(?:\.([0-9]{2}))?$`)

type walmartPageData struct {
	Props struct {
		PageProps struct {
			InitialData struct {
				Data struct {
					Product *walmartPageProduct `json:"product"`
				} `json:"data"`
			} `json:"initialData"`
		} `json:"pageProps"`
	} `json:"props"`
}

type walmartPageProduct struct {
	UsItemID  string `json:"usItemId"`
	Name      string `json:"name"`
	PriceInfo *struct {
		CurrentPrice *struct {
			PriceString string `json:"priceString"`
		} `json:"currentPrice"`
	} `json:"priceInfo"`
	// FulfillmentOptions is a pointer so a missing array (a changed page) is
	// told apart from an empty one.
	FulfillmentOptions *[]struct {
		Type               string `json:"type"`
		AvailabilityStatus string `json:"availabilityStatus"`
		LocationText       string `json:"locationText"`
	} `json:"fulfillmentOptions"`
}

// ParseWalmartProductPage interprets one answered request for productID's
// page: the status code, the path of the URL after redirects, and the body.
// The caller has already checked that the redirects stayed on walmart.com (or
// its test server); a path outside /ip/ is unknown here.
func ParseWalmartProductPage(productID string, status int, finalPath string, body []byte) ProductCheck {
	unknown := func(detail string) ProductCheck { return ProductCheck{Status: ProductUnknown, Detail: detail} }
	if !strings.HasPrefix(finalPath, "/ip/") {
		return unknown(DetailOffSite)
	}
	switch {
	case status == 404:
		return ProductCheck{Status: ProductGone}
	case status != 200:
		return unknown(DetailUnexpected)
	}
	m := nextData.FindSubmatch(body)
	if m == nil {
		return unknown(DetailNoPageData)
	}
	var data walmartPageData
	dec := json.NewDecoder(bytes.NewReader(m[1]))
	if err := dec.Decode(&data); err != nil {
		return unknown(DetailNoPageData)
	}
	p := data.Props.PageProps.InitialData.Data.Product
	if p == nil || strings.TrimSpace(p.Name) == "" || p.UsItemID == "" {
		return unknown(DetailNoProduct)
	}
	if p.UsItemID != productID {
		return unknown(DetailDifferentItem)
	}
	if p.FulfillmentOptions == nil {
		return unknown(DetailNoFulfillment)
	}
	out := ProductCheck{Status: ProductUnavailable, Name: strings.Join(strings.Fields(p.Name), " ")}
	for _, f := range *p.FulfillmentOptions {
		switch f.Type {
		case "PICKUP":
			out.Pickup = f.AvailabilityStatus
			out.Store = strings.TrimSpace(f.LocationText)
		case "DELIVERY":
			out.Delivery = f.AvailabilityStatus
		}
	}
	if inStockValues[out.Pickup] || inStockValues[out.Delivery] {
		out.Status = ProductFound
	}
	if p.PriceInfo != nil && p.PriceInfo.CurrentPrice != nil {
		out.PriceCents = parseWalmartPrice(p.PriceInfo.CurrentPrice.PriceString)
	}
	return out
}

// parseWalmartPrice reads a price string into cents, or nil.
func parseWalmartPrice(s string) *int64 {
	m := walmartPrice.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return nil
	}
	dollars, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
	if err != nil {
		return nil
	}
	cents := dollars * 100
	if m[2] != "" {
		c, _ := strconv.ParseInt(m[2], 10, 64)
		cents += c
	}
	return &cents
}
