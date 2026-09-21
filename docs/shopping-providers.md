# Shopping providers (Phase 8)

Status: **Phase 8a implemented**: the API and the iOS Shop tab (Walmart cart
links without keys; see [Implemented in 8a](#implemented-in-8a)). The 8.0
spike and 8b–8d are still plans. Research as of 2026-09-15. This document says what
third-party grocery services actually allow today, and how DinnerOS should use
them, starting with Walmart.

The goal: take the week's grocery list, put the right products in the
household's Walmart cart (or an equivalent checkout) with as few taps as
possible, and record what was bought in the pantry.

## Summary

- **No public Walmart API can write a customer's cart or place an order.**
  What Walmart does document for partners is an **add-to-cart link**: one URL
  with many item IDs and quantities, optionally pinned to a store. The Walmart
  iOS app claims that path as a universal link. That link is the MVP handoff.
- **The Walmart Affiliate API** (walmart.io) provides product search, lookup
  with price and availability by ZIP code, a store locator, and taxonomy. It
  needs a walmart.io application with an RSA key pair. Its terms limit it to
  advertising Walmart.com products, and the affiliate program runs on Impact.
- **Nothing reports back what was ordered.** Walmart, Instacart, and Kroger's
  public APIs return no order contents. Pantry purchases from a handoff are
  **member-confirmed**, not order facts.
- **Instacart Developer Platform** is the best second provider: one API call
  turns a list into a shoppable Instacart page, where the user picks the
  retailer. Production keys take about 30–40 days.
- **Kroger** is the only one with a public **cart-write** API (customer OAuth).
  It's worth adding only if the household shops at a Kroger banner.
- **Amazon Fresh, Whole Foods, Target, and Albertsons** have no public grocery
  cart integration for a small app. Albertsons banners are reachable through
  Instacart.
- **No scraping, no browser automation, no signed-in session replay** for any
  provider. The HelloFresh importer's approach (capturing our own signed-in
  pages) is not acceptable here: Walmart's and Instacart's terms forbid it.
- **AnyList has no API, no URL scheme, and no share extension for items.** Its
  two documented import paths are both bulk: paste one item per line into the
  Add Item field, or let it pull from a Reminders list it created itself.
  DinnerOS uses the paste ([Exporting to a list app](#exporting-to-a-list-app-anylist)).
- **Walmart can ship before any approval arrives.** Phase 8a builds the cart
  link from products the household saved by pasting a Walmart product link.
  Search and availability (8b) are added once walmart.io keys exist.

## Provider feasibility

| Provider | Product search | Cart handoff | Writes the cart | Order data back | Access | Fit |
| --- | --- | --- | --- | --- | --- | --- |
| **Walmart** | Affiliate API: search, lookup (≤20 IDs, `zipCode`), stores, taxonomy; 5,000 calls/day | ✅ Add-to-cart URL: many items with quantities, optional `storeId`; opens the Walmart app | ❌ no public API | ❌ none (Impact reports commissions only) | walmart.io app + key pair; Impact for attribution | **MVP** |
| **Instacart** | ❌ none for partners; Instacart matches by name, UPC, or product ID | ✅ `products_link` shopping list page; user picks the retailer (`retailer_key` sets a preference) | ❌ (the user adds to cart on the page) | ❌ none (Impact conversions only) | Self-serve dev key; production review ≈30–40 days | **Second** |
| **Kroger** (Kroger, Ralphs, King Soopers, Fred Meyer, Smith's, …) | ✅ Products API with location price, stock level, fulfillment flags; 10,000/day | — | ✅ `PUT /v1/cart/add` (UPC, quantity, `PICKUP`/`DELIVERY`) with customer OAuth; 5,000/day | ❌ none on the public tier | Self-serve app registration | Third, if the household shops there |
| **Amazon Fresh / Whole Foods** | Creators API (replaced PA-API 5, which was retired 2026-05-15) needs Associates eligibility; nothing grocery-specific | Legacy "Add to Cart form" docs now redirect to the deprecation notice; untested for Fresh | ❌ | ❌ | Associates account with qualifying sales | Not now |
| **Target / Shipt** | ❌ not public | Target "commerce partners" basket transfer is partner-only (`target/cartster` sample archived 2024-07-16) | ❌ | ❌ | Business relationship | No |
| **Albertsons / Safeway** | ❌ (public APIs are retail-media only) | Through Instacart | ❌ | ❌ | — | Via Instacart |
| **AnyList** (member's own app) | ❌ not public | ✅ documented bulk paste: one item per line | ❌ | ❌ | None needed | **Shipped** (in-person shoppers) |
| **Manual** (built in) | Catalog only | Plain-text export (exists today) | — | Member check-off (exists today) | — | Always on |

## Walmart in detail

### Add-to-cart link (the handoff)

Walmart's **Consolidated Add to Cart** doc gives:

```text
https://www.walmart.com/sc/cart/addToCart?items=945193065,660768274_2&storeId=5435
```

| Parameter | Meaning |
| --- | --- |
| `items` | Comma-separated item IDs, each optionally `_<qty>` (or the legacy `\|<qty>`). Quantity 1 can omit the suffix. |
| `offers` | Same format with offer IDs (a specific seller's offer). `items` or `offers` is required. |
| `storeId` | The fulfillment store. Optional. |
| `ap` | An access point ID from Walmart's store locator. Optional. |

- **Multiple items and quantities:** yes. Quantity means **packages**, not
  ounces.
- **Affiliate:** the doc says this service is available to partners "whether
  they are onboarded to Impact Radius or not". Attribution requires wrapping
  the URL in an Impact tracking link:
  `https://goto.walmart.com/m/<publisherId>/<adId>/<campaignId>?veh=aff&sourceid=<sourceId>&u=<url-encoded add-to-cart URL>`.
- **Legacy format:** `affil.walmart.com/cart/addToCart?items=…` (the older "GM
  Add To Cart" doc, Impact publishers only) is marked for deprecation. Affiliate
  API responses still return `affiliateAddToCartUrl` in that format. Use the
  consolidated format.
- **Partial failure:** if any item can't be added, Walmart shows an error and
  sends the user to the home page when they go to the cart. **Check
  availability first, and leave unavailable items out of the link.**
- **iOS:** `walmart.com/.well-known/apple-app-site-association` lists
  `/sc/cart/addToCart` (and `/ip*`) for the Walmart app, so the direct link
  opens the app when it's installed. **Unverified:** whether the
  `goto.walmart.com` affiliate redirect still opens the app. iOS generally
  doesn't hand a server redirect to a universal link. The first spike must
  test both.
- **Where the link should open:** use `UIApplication.open` (the Walmart app,
  otherwise Safari), not `SFSafariViewController`. The in-app browser doesn't
  share Safari's cookies, so the user would usually be signed out.
- **Walmart+ and delivery:** the link fills the user's normal Walmart cart.
  Pickup, delivery from store, or shipping, the time slot, and Walmart+
  benefits (free delivery above an order minimum, reported as $35) are chosen
  in Walmart's own checkout. No API reserves a slot. Passing `storeId` keeps
  the cart on the store whose availability we checked. Prefer items that
  aren't marketplace listings (`marketplace: false`) and are sold in store
  (`offerType` `ONLINE_AND_STORE` or `STORE_ONLY`), or grocery items can end
  up as separate shipments.
- **Cart APIs:** none are public. Walmart's shoppable-recipe partners
  (SideChef, Tasty, Pinterest in 2025) add ingredients to the Walmart cart.
  Public sources don't describe their mechanism beyond these links and the
  partner relationships. Walmart's Buy Now SDK and its `/sc/cart/buynow` path
  exist, but their docs didn't render for this review.

### Affiliate API (search and lookup)

Base: `https://developer.api.walmart.com/api-proxy/service/affil/product/v2/`

| Endpoint | Use in DinnerOS | Notes |
| --- | --- | --- |
| `GET search?query=&categoryId=&numItems=&responseGroup=full` | Suggest products for an ingredient | ≤25 per page, top 1000; facets (`brand`, price range). The default `base` response has no `size`; ask for `full`. |
| `GET items?ids=a,b,…&zipCode=` | Refresh price and stock before a handoff | ≤20 IDs per call. Also `upc=`, `gtin=`. **`storeId=` needs extra business approval**, so use `zipCode`. |
| `GET stores?zip=` or `lat=&lon=` | Store picker | Returns store `no`, name, and address. Test that `no` is the `storeId` the cart link expects. |
| `GET taxonomy` | Limit search to Food | Category IDs |

Useful item fields: `itemId`, `upc`, `name`, `brandName`, `size`, `salePrice`,
`stock` (`Available`, `Limited Supply`, `Last few items`, `Not available`),
`availableOnline`, `offerType`, `marketplace`, `offerId`, `thumbnailImage`.

**Authentication:** every call is signed. The headers are `WM_CONSUMER.ID`,
`WM_CONSUMER.INTIMESTAMP` (epoch ms), `WM_SEC.KEY_VERSION`, and
`WM_SEC.AUTH_SIGNATURE`. The signature is RSA-SHA256 over the three values,
sorted by header name and each followed by `\n`, with a PKCS#8 private key.
It expires after 180 seconds. Only production consumer IDs registered on
walmart.io work.

**Onboarding:** sign in to walmart.io with a Walmart.com account, create an
application, upload the **public** key, receive a consumer ID, and accept the
Walmart I/O terms. `publisherId` (Impact) is optional on these calls but
needed for tracked URLs.

**Rate limit:** 5,000 calls a day, raised on request. One week is about one
search per newly matched ingredient plus one lookup per 20 saved products.
Even a busy household uses well under 100 calls.

### Terms to respect

The Walmart I/O terms (last updated 2020-09-30, and partly stale: they still
name LinkShare, the pre-Impact affiliate network) say:

- The API is **only for advertising Walmart.com products**. A shopping list
  that sends people to Walmart's cart fits that.
- **Don't scrape** or spider Walmart.com or the API. Don't frame Walmart.com
  or make it look like the user is on Walmart.com.
- **Don't redistribute or syndicate** product information to any third party.
  This matters for Autopilot (see [risks](#risks-and-open-questions)).
- **Don't pull traffic away from Walmart.com.** A Walmart-versus-competitor
  price comparison would violate this, so DinnerOS doesn't show one.
- **Rejected sites:** those under construction or not live, those serving
  mainly non-US audiences, and those without a clearly stated privacy policy.
  Cash-back and points programs need extra approval.
- "You should not and may not link to Walmart.com or use the API unless you
  are approved": **apply before launching** the Walmart handoff to anyone
  beyond the owner.

The affiliate program is free, approval is at Walmart's discretion, and it
pays up to 4%. The FAQ doesn't cover mobile apps or buying through your own
links; ask Impact or Walmart before counting on commissions from the owner's
household.

## Instacart Developer Platform

- `POST https://connect.instacart.com/idp/v1/products/products_link` (dev:
  `connect.dev.instacart.tools`), with `Authorization: Bearer <key>`.
  - The body is `title` and `line_items[]`.
  - Each line item has `name` (required), `display_text`,
    `line_item_measurements[]` (quantity and unit pairs), and `upcs[]` or
    `product_ids[]` (mutually exclusive).
  - Optional `filters` (`brand_filters`, `health_filters`), `link_type`
    (`shopping_list`), `expires_in` (≤365 days), and
    `landing_page_configuration.partner_linkback_url`.
  - Returns `products_link_url`, which can carry Impact affiliate parameters.
- **`quantity` and `unit` on line items were deprecated 2026-03-18.** Use
  `line_item_measurements`. Instacart's units of measurement reference lists
  the accepted units; map DinnerOS units to it.
- `GET /idp/v1/retailers?postal_code=&country_code=US` returns nearby
  retailers. Append `?retailer_key=<key>` to the link to preselect one.
  Instacart still decides the default store and the user can change it.
- The user reviews the matched products, adds them to cart, signs in, and
  checks out on Instacart. The FAQ says it deep links into the Instacart iOS
  app.
- **Coverage:** most major non-Walmart grocers, including Albertsons banners.
  Walmart has sold through Instacart in some markets since a 2021 pilot, but
  current coverage isn't documented. Check with `GET /retailers` for the
  household's ZIP once a dev key exists.
- **Access:** self-serve dashboard with development and production keys.
  Creating a production key starts a review: terms compliance, correct
  requests, error handling, and an Enterprise Help Desk account. Approval
  brings an Impact invitation. Instacart puts the average from request to
  production key at about 30–40 days.
- **Terms:** no scraping. Request the minimum data. **Don't show priced items
  from more than one retailer on the same screen**, and don't move or compare
  items between retailer baskets. Instacart can terminate at will.

## Kroger Public API

- OAuth 2.0: client credentials for products and locations, authorization
  code with refresh tokens for the customer's cart. Scopes: `product.compact`,
  `cart.basic:write`, `profile.compact`.
- Products (`/v1/products?filter.term=&filter.locationId=`): with a location
  ID it returns price, `stockLevel`, and `delivery`/`curbside` flags. Search
  is fuzzy and result order changes between calls. Limit: 10,000 calls a day.
- Locations: 1,600 calls a day per endpoint. Cart
  (`PUT /v1/cart/add`, body `items: [{upc, quantity, modality}]`): 5,000 calls
  a day.
- Cart can only be added to. It can't be read, and checkout happens on
  kroger.com or in the app. No order history on the public tier.
- Production (`api.kroger.com`) and certification (`api-ce.kroger.com`) each
  need their own registered application.

## Product matching

A grocery line is an ingredient, amounts, and a category. A cart line is one
product ID and a **package count**. Matching must stay deterministic and must
be confirmed by a person (architecture principle 4, pantry decision #98).

### Per-ingredient preferences

The household saves its choice per `(provider, ingredientKey)`, where
`ingredientKey` is the catalog ID or `name:<key>`, as the list already keys
lines.

| Field | Meaning |
| --- | --- |
| `productId` | Walmart `itemId` / Kroger UPC / Instacart UPC |
| `name`, `brand`, `imageUrl` | Display only, refreshed on lookup |
| `packageSize` | Exact amount and unit per package (`{quantity: "20", unit: "oz"}`), or `null` |
| `packageSizeSource` | `parsed` (from `size` or the name) or `person` |
| `alternates[]` | Ordered fallbacks for out-of-stock items |
| `lastPrice`, `lastStock`, `checkedAt` | From the last lookup; never shown as a promise |
| `useCount`, `lastUsedAt` | Order suggestions |

### Choosing a product

1. **A saved preference exists:** use it, and refresh it with one batched
   lookup (with `zipCode`).
2. **None, and search is available (8b):** search the ingredient's name (or
   catalog `searchTerms`) in the Food category. Drop marketplace and
   unavailable items and show the top few. The member picks one, and the
   choice is saved.
3. **No search yet (8a):** the member pastes a Walmart product link. The
   `itemId`, the product **name**, and often the **package size** are all
   read from the URL string (`/ip/<slug>/<itemId>`), without fetching the
   page, so pasting a link is the whole interaction and the member types
   nothing. The slug is the product title with hyphens for spaces
   (`/ip/Garlic-Bulb-Fresh-Whole-Each/123` → "Garlic Bulb Fresh Whole Each",
   1 ct), so the original punctuation doesn't survive and an ambiguous size
   (`-2-5-lb`, which could be 2.5 lb or 5 lb) yields no size rather than a
   wrong one. Both are defaults shown for confirmation, never facts from
   Walmart.

   DinnerOS also suggests **what to search for**, because searching an
   ingredient's bare name is the wrong search: "garlic" ranks garlic powder,
   garlic salt and garlic snacks above the bulb. The suggestion is biased by
   the line's grocery category — produce searches "fresh whole …" — and
   carries the forms to avoid. Without a search API DinnerOS cannot filter
   those out itself, so they are shown to the member as a hint and the
   search opens in Walmart's own app. A name that already states a form
   ("Garlic Powder") is searched as itself.
4. **Lines that need a person:** uncatalogued or unusual lines ("Tex-Mex paste
   alternative") and unquantified lines are never auto-matched. They show as
   "Choose a product" or can be left out of the handoff.

### Package count

| Line amount vs package size | Packages |
| --- | --- |
| Same dimension (20 oz needed, 16 oz package) | `ceil(need ÷ size)` = 2, shown as "2 × 16 oz covers 20 oz" |
| Several amounts that combine after conversion | Convert, sum, then round up |
| Discrete unit matching the package (`2 can` and a can) | The count |
| Volume against weight (2 tbsp jam, 18 oz jar; 5 tsp vinegar, 8.5 oz bottle) | Estimated with the ingredient's typical density, shown in the recipe's units: "1 × 18 oz covers 2 tbsp" (see below) |
| A count that doesn't convert (4 garlic cloves, package "3 ct heads") | **1, flagged "check amount"** for `per_amount`; 1 covering the week, unflagged, for `per_week` |
| Unquantified ("salt to taste") | 1, only if the pantry doesn't have it (the line is `toBuy`) |
| Unknown package size | 1: flagged for `per_amount`, covering the week unflagged for `per_week` (fresh categories) |

**Volume ↔ weight, for counting only.** Recipes use spoons and cups; Walmart
sells jam, spices and vinegar by the ounce of weight. Flagging "2 tbsp doesn't
convert to an 18 oz package" every week is noise with an obvious answer, so
package counting (and nothing else — recipe and pantry amounts still never
convert between volume and weight) estimates with a typical density
(`providers/density.go`): a short name table first (honey, jam and syrup
1.4 g/ml; salt 1.2; sauces, vinegar and milk about 1.05; oil 0.92; sugar 0.85;
flour 0.55; ground spices 0.5; dried herbs 0.3; fresh leafy herbs 0.15), then
a per-category default. A package in `fl oz` against a volume need is exact
already. As a check, the same need is converted with an extreme density
(1.5 g/ml, heavier than honey, for volume → weight; 0.1 the other way): when
only the category default is known and the extreme would buy more packages,
the count stands but reads "about" ("1 × 10 oz covers about 1 cup"). Only
needs that can't be estimated at all — a count or cloves against a weight —
are still flagged, and the Shop tab turns every size-related flag into a
shortcut to the product's package size.

Parsing sizes from `size` or the product name ("20 oz", "2 lb", "12 ct",
"1.5 fl oz") uses a small allowlist of exact patterns. Anything else is
`packageSize: null`. Parsed sizes are shown for confirmation before they're
saved.

### Out of stock

The handoff lookup marks each line `available`, `limited`, or `unavailable`.
Unavailable lines use the first available alternate, marked as a
substitution, or are excluded and listed ("Not added: ground pork, out of
stock at your store"). Limited-stock items are included with a warning.

## Recording purchases

No provider reports what was ordered. What DinnerOS can know:

| Signal | Known? |
| --- | --- |
| The products and package counts in the handoff link | ✅ stored on the handoff |
| That the user opened it | ✅ the app reports it |
| What was actually in the cart, substituted, removed, or delivered | ❌ |
| Price paid, order time | ❌ from Walmart; ✅ when a member enters prices or an order total ([Prices](#prices)) |
| Commission from an order (Impact) | Aggregated and delayed, not per user; don't use for pantry |

**Flow:** after the handoff, the Shop tab shows "Did you order these?" with
each handed-off line checked and its package count editable. The sheet is
grouped by meal, in plan order with each recipe's photo and day, using the
handoff line's `recipes`: an item for one meal sits under it, an item for
several meals appears once under "For Several Meals" (with the meals named),
and add-on recipes (garlic bread) and items tied to no recipe go under
"Extras". A meal whose items are all checked collapses to "4 of 4 ordered".
One button confirms: "Ordered All 43", or "Ordered 42 of 43" with the rest
marked not ordered. On confirm, the API writes one pantry purchase per line the pantry tracks
(long-lasting leftovers; fresh items used up this week are confirmed without
one, [pantry-usage.md](pantry-usage.md#what-goes-in-the-pantry)):

```json
{
  "source": "provider",
  "ingredientId": "…",
  "quantity": "2",
  "unit": "package",
  "unitSize": { "quantity": "16", "unit": "oz" },
  "week": "2026-W38",
  "provider": { "key": "walmart", "handoffId": "…", "productId": "945193065" }
}
```

- The purchase is written **server-side** from the stored handoff, so the rule
  that apps can't send `source: provider` still holds. The confirm call names
  handoff lines, not arbitrary amounts.
- `unit: package` with `unitSize` lets the pantry convert recipe use exactly
  (decision #98). Without a known size the purchase is a count of packages,
  and recipes in oz won't deduct from it, as today.
- A handoff line can be confirmed at most once (idempotent per handoff line).
- Checking the grocery line off works as today and doesn't create a second
  purchase for a line already confirmed through a handoff.
- Emailed Walmart receipts can't help with prices: the confirmation and
  delivered emails carry only the item count, the order total (fees, tax, and
  discounts included), savings, and a "View order" link, never item prices.
  iPhone apps can't read Mail either.

### Prices

Prices make weekly cost and meal kit savings possible
([grocery-engine.md](grocery-engine.md#weekly-cost)). They're optional
everywhere and stored in integer US cents.

| Way in | Where | What's sent |
| --- | --- | --- |
| Typed | "Did You Order These?" (per line), Choose Product (per package), Add Prices under This Week's Cost | `priceCents` on confirm, `POST .../handoffs/{id}/prices`, or the saved product |
| Order total | This Week's Cost: the total with fees, tax, and tip | `PUT .../shopping/weeks/{week}/spend` |
| Order screenshots | Shop → This Week's Cost → Import Prices from Order Screenshots…: the member picks screenshots of the Walmart app's order details or cart | Only the prices (and total) the member confirms on the review screen |

**Screenshot import runs on the iPhone.** The picked images stay in memory:
Vision (`RecognizeTextRequest`) reads their text on device. When Apple
Intelligence is available, the on-device Foundation Models framework
structures that text into items (name, price, quantity) and the order total,
behind an availability check and a timeout; an item it returns is kept only if
its price appears in the recognized text. Otherwise, and always as the check,
a deterministic parser (`OrderScreenshotParser`) reads item names with their
prices, quantities, and "was" prices, and the subtotal, fees, tax, tip, and
total. `OrderPriceMatcher` matches items to the week's handoff lines by word
overlap with the product and ingredient names, one item per line, with high,
medium, or low confidence. The review screen shows matched items (reassignable
or unused), unmatched items (assignable), lines still without a price, and the
detected total; saving sends only those numbers. No image, recognized text,
or unused item leaves the phone, and nothing is stored on it.

**The layout decides what belongs to what.** Recognition keeps each row's box
(`RecognizedRow`, normalized with the origin at the top left, one screen of
offset per screenshot). Three things need it:

- **Which side the price is on.** The order-details screen prints the price
  under the name; the cart prints it *above*. Every bare price votes for the
  side its nearest name sits on and the majority decides for the whole read
  (`OrderScreenshotParser.pricesComeFirst`), because a screenshot often opens
  mid-card with a name whose price scrolled off, and binding the price that
  *follows* a name then shifts every price onto the item below it.
- **Raised cents.** The cart draws a price as large dollars with small, raised
  cents, and the decimal point is drawn, not written. When they arrive as two
  pieces `OrderTextLayout` rejoins them on that implied point; usually they
  arrive as one word instead ("$244", "$64"), and then the whole read decides.
  `OrderScreenshotParser.priceStyle` reads every separator-less amount as
  ending in its cents when two or more amounts on the screen carry no
  separator, or when reading them in full puts the items over the order's own
  total (`ParsedOrder.itemsFitTotal`, which is also the check that the read
  makes sense at all). An amount written with a decimal point or a thousands
  comma — the estimated total, a unit price, a was-price — is never re-read.
- **Photos.** The card's photo is to the left of that column and the packaging
  in it is printed text, so "Sour Cream Original" off the tub lands on the name's
  row. `OrderTextLayout.withoutProductPhotos` drops pieces that end before the
  column begins, and a name only carries onto the next row when that row starts
  at the same edge.
- **Titles.** `OrderTitleCleaner` keeps the product name and drops what the app
  prints around it: a unit price above it ("88¢/lb | Final cost by weight"),
  and Subscribe, SNAP EBT eligible, Free N-day returns, Gift eligible, Remove,
  Save for later, badges ("Best seller", "Rollback", "10K+ bought since
  yesterday"), Multipack Quantity, and Count Per Pack below it. Rows that are
  only chrome aren't names at all, so they can't be glued onto the title above
  them. The quantity stepper ("- 2 +") reads as the item's quantity.

**An imported price is the whole line**, what the cart or the order charged for
the quantity in it — the same thing "Add Prices" asks for and what the week's
cost adds up. The review screen shows it as currency with the quantity and, for
a line of more than one, what one came to.

A Share extension from Photos isn't built: it needs its own app extension
target and gives nothing the in-app picker doesn't.

A confirmed line's price is also written to its pantry purchase, and the saved
product remembers the per-package price for next time.

## MVP architecture

### API (`internal/providers`, later `internal/shopping`)

```text
GroceryProvider
  Key() string                              // "walmart", "instacart", "kroger", "manual"
  Capabilities() Capabilities               // search, lookup, stores, handoff kind, cartWrite, orderImport
  SearchProducts(ctx, SearchQuery) ([]Product, error)       // term, category, store/ZIP, limit
  LookupProducts(ctx, ids, StoreRef) ([]Product, error)     // price + stock refresh
  FindStores(ctx, StoreQuery) ([]Store, error)
  BuildHandoff(ctx, HandoffRequest) (Handoff, error)         // lines → URL + included/excluded

optional interfaces (checked with type assertions, like grocery.OutPantry):
  CartWriter    AddToCart(ctx, userToken, lines) error      // Kroger
  OrderImporter ImportOrders(ctx, …)                        // none available today
```

- Each provider is enabled only when its config vars are set, the same pattern
  as the AASA file (#81). A disabled provider returns `503
  provider_unavailable`.
- The Walmart client signs each request with the key parsed once at startup.
  It never logs headers, query strings, or response bodies, and it caches
  lookups for a few minutes per `(itemId, zip)`.
- Matching math (size parsing, package count) is pure code in the providers
  package with table tests, separate from HTTP clients.

| Route | Permission | Purpose |
| --- | --- | --- |
| `GET /api/v1/shopping/providers` | signed in | Enabled providers and capabilities |
| `GET/PUT /households/{id}/shopping/settings` | `household.view` / new `shopping.edit` | Store provider, ZIP, `storeId`, store name (the planned `grocery_provider_configurations`) |
| `GET /households/{id}/shopping/{provider}/stores?zip=` | `household.view` | Store picker |
| `GET /households/{id}/shopping/{provider}/products?q=` | `household.view` | Search |
| `GET/PUT/DELETE /households/{id}/shopping/{provider}/preferences/{ingredientKey}` | view / `shopping.edit` | Saved products |
| `POST /households/{id}/plans/{week}/shopping/{provider}/match` | `household.view` | Proposal per `toBuy` line: product, packages, coverage text, stock, flags |
| `POST /households/{id}/plans/{week}/shopping/{provider}/handoffs` | `shopping.edit` | Member-confirmed lines → the week's handoff, with URLs for only what isn't in the cart yet |
| `POST /households/{id}/plans/{week}/shopping/{provider}/handoffs/start-over` · `.../send-again` | `shopping.edit` | Send everything again, or one line |
| `POST /households/{id}/shopping/handoffs/{handoffId}/confirm` | `pantry.edit` | Bought lines → `provider` pantry purchases |

New collections: `shopping_product_preferences` (unique `{householdId,
provider, ingredientKey}`) and `shopping_handoffs` (householdId, week,
provider, store, lines with `confirmedAt`, url, createdBy, createdAt;
`{householdId, createdAt: -1}`). Add `shopping.edit` to the role table (#25).

### iOS (Shop tab)

1. **Store setup** (once): choose Walmart, enter a ZIP, pick a store.
2. **Review matches:** the week's `toBuy` lines with product thumbnail, "2 ×
   16 oz", and stock. Lines needing a choice come first. Tap a line to
   change the product, see alternates, or exclude it.
3. **Open in Walmart:** a disclosure next to the button ("DinnerOS may earn a
   commission"), the list of excluded lines, then `UIApplication.open(url)`.
4. **Back in DinnerOS:** "Did you order these?" confirms purchases into the
   pantry. It can be dismissed and done later from the week's list.

Show one store's products and prices at a time. Instacart's terms forbid
mixing retailers on a screen, and Walmart's forbid pulling traffic from
Walmart.

### Credentials

| Config var | Secret? | Source |
| --- | --- | --- |
| `WALMART_CONSUMER_ID` | low | walmart.io application |
| `WALMART_PRIVATE_KEY` | **yes** | Generated locally: base64 of the PKCS#8 PEM |
| `WALMART_KEY_VERSION` | no | walmart.io (starts at `1`) |
| `WALMART_IMPACT_PUBLISHER_ID`, `WALMART_IMPACT_AD_ID`, `WALMART_IMPACT_CAMPAIGN_ID` | no | Impact dashboard. Unset means untracked links. |
| `INSTACART_API_KEY` | **yes** | Instacart developer dashboard (separate dev and production keys) |
| `KROGER_CLIENT_ID` / `KROGER_CLIENT_SECRET` | secret: **yes** | developer.kroger.com |
| `PROVIDER_TOKEN_ENCRYPTION_KEY` | **yes** | `openssl rand -base64 32`; AES-GCM for stored Kroger refresh tokens |

- Only in Heroku config vars and a local `.env`. **Never** in the iOS app, the
  repository, CI logs, or chat. Add them to `.env.example` as empty
  placeholders and to the credential reference in
  [deployment.md](deployment.md#credential-reference).
- Generate the Walmart key pair on the owner's machine
  (`openssl genrsa -out wm.pem 2048`, `openssl pkcs8 -topk8 -nocrypt -in wm.pem
  -out wm_pkcs8.pem`, `openssl rsa -in wm.pem -pubout -out wm_public.pem`).
  Upload only the public key. Set the private one with
  `heroku config:set WALMART_PRIVATE_KEY="$(base64 -i wm_pkcs8.pem)"`, keep a
  copy in a password manager, and delete the local files.
- To rotate, upload a new public key, bump `WALMART_KEY_VERSION`, and replace
  the config var.

### Disclosure and policy

- **FTC Endorsement Guides** (16 CFR 255, revised 2023): an affiliate
  relationship must be disclosed clearly and conspicuously, where the link is,
  not behind "more". Show one line beside the handoff button and repeat it in
  Settings → About.
- **Privacy policy:** a public URL is required by Walmart's terms and the App
  Store. It must say that the ZIP code or store is sent to the chosen provider
  and that links may carry affiliate tracking. Update the App Store privacy
  details to match.
- **App Review:** physical goods bought outside the app don't use in-app
  purchase (Guideline 3.1.3(e); re-check at submission).
- **Branding:** use Walmart and Instacart names and logos only as each program's
  brand rules allow. Instacart's are in its design guidelines and CTA specs.

## Implemented in 8a

The API side of 8a is built (decisions #160–168 in
[architecture.md](architecture.md#decision-log); endpoints in
[api.md](api.md#shopping); collections in [database.md](database.md#shopping)).
Nothing fetches Walmart pages, calls a Walmart API, or needs Walmart
credentials.

- **Packages:** `internal/providers` is pure: `GroceryProvider` (key, name,
  handoff kind, `ParseProduct`, `ProductURL`, `NormalizeStoreID`,
  `BuildCartLinks`) with search, lookup, stores, cart write, and order import
  as optional interfaces (`ProductSearcher`, `ProductLooker`, `StoreFinder`,
  `CartWriter`, `OrderImporter`). None is implemented yet. `internal/shopping`
  holds settings, saved products, handoffs, and confirmations. The planned
  `match` route and the provider list are there. The store picker and
  product search routes wait for 8b.
- **Store settings:** provider and an optional typed store number, no ZIP
  (ZIP matters only for 8b lookups).
- **Saved products:** from a pasted `walmart.com/ip/…` link or item ID, with
  a member-typed name and optional package size. `name`/`brand`/`imageUrl`,
  `alternates`, `packageSizeSource`, last price and stock, and use counts
  from the table above wait for 8b lookups.
- **Package counts** follow [Package count](#package-count) exactly, with
  one addition: when only some of a line's amounts convert, the count covers
  what converts and is flagged. Size parsing from product names waits for 8b
  (the member types the size).
- **Handoffs:** candidates are `toBuy` lines (or the lines the app selects),
  minus checked-off keys the app sends, house-made batches, and lines without
  a saved product. There's no stock check yet, so "leave unavailable items
  out" is a member's choice until 8b. Links are split past 2,000 characters
  or 40 products, both unverified against Walmart.
- **Confirm:** "Did you order these?" writes provider purchases server-side
  (`unit: package` + `unitSize`, or the exact count when the size is a
  count), once per handoff line, and handoffs record
  `shopping.handoff_created` / `shopping.order_confirmed` events (counts
  only).
- **Permission:** `shopping.edit` (admins and members) for settings, saved
  products, and handoffs; confirming needs `pantry.edit`.
- **Affiliate:** the Impact wrapper runs only when all three
  `WALMART_IMPACT_*` config vars are set; responses carry `affiliateTracked`
  for the disclosure.
- **iOS Shop tab** (decisions #200–#207): store setup with an optional store
  number; the week's match with "Needs a Product", "Ready" (package steppers
  and "Check amount" badges), and "Not Included"; Choose Product from a pasted
  link, with a plain "Search on Walmart" link; Saved Products; "Open in
  Walmart", stepping through several cart links; and "Did you order these?"
  when the app returns, which checks confirmed lines off on the device. The
  commission line shows only when `affiliateTracked` is true.
- **Sending again** (decision #495): Walmart's add-to-cart link adds to
  whatever the cart holds and DinnerOS can't read or clear that cart, so
  opening Walmart twice used to double every quantity. Now a household has
  one current handoff per week and provider, and every stored line's
  `packages` is what its links put in the cart. A later "Open in Walmart"
  adds only new lines and the extra packages of a line whose count went up;
  with nothing new the app opens nothing and says "Everything is already in
  your Walmart cart". A decrease or a replaced product can't be undone by a
  link, so the line says to remove it in the Walmart app. Marking the week
  ordered, "Start Over", or answering every line in "Did you order these?"
  closes the handoff, so the next send (and next week) starts with an empty
  cart. "Send Again" on one line is for a member who removed it from the
  cart. The state is server-side, so every member's phone agrees
  ([API](api.md#sending-again)).
- **Not done yet:** recording that the member opened the links, an
  out-of-stock or alternates flow, and the 8.0 spike's answers (whether the
  `goto.walmart.com` wrapper still opens the app, and the real URL limits).

## Order reminders

A household picks the weekday it means to place its grocery order, and gets one
reminder for the week once that day arrives — until someone says they ordered.

- **The order day is a household setting** (`orderDay` on the household,
  `mon`…`sun`, or unset). Unset is the default and means no reminders.
- **The reminder is derived, never stored.** "Has the order day arrived, and is
  this week still unmarked?" is a question about today, so it is answered on
  read from the order day, the household's time zone, and the ordered marker.
  There is no scheduler, no background job, and nothing that can drift or fire
  twice.
- **It belongs to its own week.** It starts on the order day and stops when the
  week ends (the week's dates follow the household's `weekStartsOn`, so with
  Sunday-first weeks a Sunday order day is the week's first day), so next week starts fresh instead of a run of old unmarked weeks
  all asking at once.
- **Only a person marks a week ordered.** Nothing infers it. A Walmart hand-off
  is not proof an order was placed — the cart link opens Walmart and DinnerOS
  never learns what happened next ([Recording purchases](#recording-purchases))
  — so marking automatically would silence the reminder for a household that
  never checked out. Marking is undoable for the same reason: a mis-tap must
  not leave a household un-remindable for a week.
- **It does not nag.** One notification per week, deduped on the week, and one
  banner that persists until it is acted on. Marking the week also marks that
  member's copy of the bell notification read.
- **Where it shows:** the Shop tab is the primary surface, because the reminder
  is about ordering and Shop is where the list is sent. It sits directly above
  "Did you order these?", so finishing a Walmart hand-off puts both in view and
  the mark is *offered* at that moment. The bell carries the same reminder
  (`shopping.order_due`) for members who aren't in the Shop tab.
- **Push:** the hourly sweep (`cmd/sendreminders`) runs the reminder for every
  household, so it is created on the order day even when nobody opens the app,
  and pushes it once to the phones of members who haven't read it, after 08:00
  in the household's time zone
  ([pantry-usage.md](pantry-usage.md#push-delivery)). Tapping it opens the week
  in Shop. If anyone marks the week ordered before the push goes out (it waits
  out the night), nobody gets it.

## Exporting to a list app (AnyList)

Not everyone orders online. A member who shops in person keeps the week's list
in their own grocery app, and AnyList is the first one DinnerOS hands off to,
alongside Apple Reminders.

**What AnyList actually supports.** Researched 2026-09-21 against AnyList's own
help site; sources at the end of this document.

- **No public API.** AnyList publishes no developer documentation, no API keys,
  and no partner program. The `anylist` npm package and the Home Assistant
  add-on built on it are third-party reverse engineering of the private sync
  protocol, which needs the member's AnyList password. That is out of the
  question here: it is unsupported, it breaks whenever AnyList ships, and it
  would mean DinnerOS holding another service's credentials.
- **No documented URL scheme and no `x-callback-url`.** AnyList's help site
  documents none, and the only public discussion of one is a 2016 user request
  on the OmniFocus forum that was never answered. Opening `anylist://` on a
  guess would take the member out of DinnerOS with no way to tell whether
  anything arrived, so DinnerOS does not do it.
- **The share sheet takes recipes, not items.** The "AnyList Recipe Import"
  extension imports a *recipe* from a web page or app. There is no share
  extension for list items.
- **Two documented item-import paths, both bulk:**
  - **Copy and paste.** Items separated by line breaks, one per line, pasted
    into the Add Item field: "tap the Add Item text field, tap on the
    pasteboard button, tap on Paste, and tap on Done". AnyList then files each
    line under its own category automatically. This is a first-class,
    documented feature, it takes the whole list in one action, and the text
    the member pastes is the text DinnerOS wrote.
  - **Reminders App Import.** The member turns it on in AnyList's Settings and
    picks which AnyList lists to sync; AnyList creates a matching list in
    Apple Reminders, and imports (and then deletes) anything found there the
    next time it opens. AnyList's own help calls this "an advanced feature"
    and steers people to Shortcuts instead.

**What DinnerOS implements: the paste.** "Send to AnyList" copies the week's
unbought lines and tells the member where to paste them.

- **Why not the Reminders route,** given DinnerOS already writes to Reminders:
  the two ends don't meet. AnyList imports only from a Reminders list *it*
  created and named after one of *its* lists; the DinnerOS export creates a
  list named for the app and the week (`DinnerOS · Sep 14 – 20`), which AnyList
  never looks at. Making it work would mean writing into a list DinnerOS did
  not create, whose name only the member knows, and whose contents AnyList
  deletes behind us — three ways for a week's list to vanish silently. A member
  who has already set up Reminders App Import can still get there by hand, and
  the explainer does not pretend otherwise.
- **The same rules as the Reminders export.** The lines come from
  `GroceryReminderPlan.drafts`, so checked-off lines and anything the pantry
  already has are left out, and the aisle order is the server's, unchanged.
- **Quantities survive**, because each line is the grocery list's own amount in
  front of its own name — `1 ½ + 8 oz Yellow Onion` — the same string the
  Reminders export uses as a reminder title.
- **No aisle headers.** Every pasted line becomes an item in AnyList, so a
  `Produce` header would arrive as something to buy. AnyList categorizes the
  items itself, and the aisle *order* still survives in the line order. A list
  app that keeps our aisles instead would get the headers; that is one flag on
  the target (`sortsIntoItsOwnAisles`), not a second code path.
- **An explainer first, once per app per device.** Tapping "Send to AnyList"
  the first time shows what will be copied, how many items, where to paste it,
  and the first few lines — *before* the clipboard is touched, because taking
  over someone's clipboard is not something to do and then explain. Seen state
  is per target (`groceryExport.listAppExplained.<id>`), so Reminders and
  AnyList each explain themselves once, and a future target does too.

**Adding another list app** is one `GroceryListApp` value in
`ios/DinnerOS/Core/Planning/GroceryListAppExport.swift`: an id, a name, an SF
Symbol, whether it sorts into its own aisles, and its paste steps. The button,
the explainer, the confirmation, and the seen-state key all read from it. That
is as far as the abstraction goes on purpose — until a second app turns up that
needs something other than a paste, there is nothing to generalize.
## Bulk packs

The store's smallest pork loin is 4 lb. Thursday's meal uses 10 oz. The week
buys 54 ounces of pork it has no plan for, and until now nothing said so.

**What counts as a bulk pack** (`shopping/bulkpack.go`) is the loud form of a
disagreement the package math already computes: a `per_week` line
([Package count](#package-count)) whose measured surplus is at least
`MinBulkSurplusPercent` = **50%** of what it bought, and which the pantry is
**not** tracking.

- **Half is the threshold** because below it the surplus is a portion, not a
  second meal. A 16 oz pack for 10 oz is ordinary shopping; a 64 oz pack for
  10 oz is not.
- **Tracked leftovers are excluded.** A tub of sour cream bought for 2 tbsp is
  already pantry stock, already counting down, and next week's list already
  knows about it ([pantry-usage.md](pantry-usage.md#what-goes-in-the-pantry)).
  There is nothing to rescue.
- **In practice this selects meat and seafood**, because the leftovers rule
  never tracks them: raw pork isn't shelf stock, so it is never "already
  handled", and it is exactly the category where the package is largest
  relative to the need. Lines that were skipped, that carry over by measure
  (`per_amount`), or whose amounts don't convert are not bulk packs either.

`GET .../shopping/handoffs/{handoffId}/bulk-packs` returns the handoff's packs,
each with what it bought, what the week needs, the surplus and its percent,
plus two flags and the suggestions below. Reading it changes nothing.

**Two offers, and a member who takes neither loses nothing.**

1. **Plan a second meal this week.** `recommendations.Service.LeftoverPicks`
   narrows Autopilot's catalog to the household's *own* recipes that use the
   ingredient and aren't already in the week, then ranks them for the week's
   first open day with the ordinary ranker — so variety, cook-time balance and
   weekday rules still apply, and whatever Autopilot has learned about this
   household a leftover suggestion learns too. It is not a new recommender.
   Accepting a pick is an ordinary plan change through the plan-entry endpoint
   ([api.md](api.md#plans)); nothing here writes the plan. A week with no
   open day, or a household with no other recipe for the ingredient, simply
   gets a pack with no suggestions, which is an ordinary outcome and not a
   failure — a ranking error is logged and the pack still comes back, because
   "you bought four pounds" is useful on its own.
2. **Portion, seal and freeze it.** The app records the amount actually sealed
   and the number of portions as a frozen pantry item
   (`POST .../pantry/freezer`, [pantry-usage.md](pantry-usage.md#the-freezer)),
   naming the handoff line it came from — which makes freezing the same line
   twice a no-op.

| Flag | Meaning |
| --- | --- |
| `freezable` | the line's category is one that freezes and comes back as food: meat and seafood, bakery, deli, frozen. **Produce and dairy are deliberately absent** — a frozen bag of spinach is a different product from the fresh bunch the recipe asked for, and telling someone to seal sour cream would be bad advice given confidently |
| `frozen` | this line's remainder is already in the freezer (some in-stock frozen item names this handoff and line), so the offer is done |

**A frozen line is never bought again.** A grocery line the freezer covers has
the status `fromFreezer`, and the match leaves it out with the new exclusion
reason `in_freezer`, shown as **"Grab from the freezer"** beside "In your
pantry" and "Usually on hand". The Walmart hand-off and every other export
build from the included lines, so a frozen-covered line is not in the cart
link, not in a second send, and not in any future export — the whole point of
sealing the remainder was to not buy another four-pound loin next month.

## The prep plan

Bulk packs say "you bought four pounds and the week needs ten ounces". The
freezer records a remainder. What was missing is the half hour between them:
Sunday at the counter with a 4 lb pork loin and a knife, deciding how many
pieces to cut it into.

The prep plan (`shopping/prep.go`) is that half hour as a checklist. It is
reachable once the order arrives, from the Shop tab and from the Pantry, and
it walks one card at a time through every bulk pack the week's hand-offs
bought.

### One card

| The card says | Where it comes from |
| --- | --- |
| What the week's meals need, and which meals by name and day | The hand-off line's measured need, joined with the week's plan for each recipe's day |
| What is left over | The pack's surplus, the same number the bulk pack reports |
| How many portions to cut the rest into, and how big each is | The heuristic below, with a stepper to change it |
| How long **one of those portions** takes to thaw | `pantry.ThawFor` on the sealed remainder at the chosen count |
| Whether a second meal this week would use it instead | The bulk pack's Autopilot suggestions, planned through the ordinary plan endpoint |

Each card has **Done** and **Skip**, and a skipped card stays in the list so
it can be done later. A remainder that is already in the freezer — sealed from
another member's phone, or before the session existed — counts as done, because
asking a household to put away something already in the drawer is exactly what
a checklist must not do.

### The portion-size heuristic

**One portion is one meal's worth.** The household's own recipes already say
how much of an ingredient a dinner takes: it is the week's need for the line
divided by the planned meals that need it. The suggested count is then the
surplus measured in those dinners, rounded to the nearest whole one, and the
per-portion size is the surplus divided by that count.

```text
typical meal = needed ÷ meals that need it
portions     = round(surplus ÷ typical meal), clamped to [1, MaxPrepPortions = 12]
portion size = surplus ÷ portions
```

- **The numbers are the household's, not a table.** A four-pound loin bought
  for a ten-ounce Thursday becomes five portions of about 10.8 oz because a
  Thursday in this house is ten ounces. There is no "a portion of pork is
  X oz" anywhere; a household that cooks for six gets bigger portions without
  telling anyone anything.
- **`basis` says which way it was worked out.** `meal` is the case above.
  `week` is the fallback for a line that records no recipes (an extra someone
  typed, or a hand-off stored before recipes were kept): the week's whole need
  stands in for one meal, and the app says so rather than pretending to know
  more.
- **Twelve is the cap** on what the card suggests and offers. It is well past
  any sensible number of dinners from one pack, and a member who really wants
  more can freeze by hand up to `pantry.MaxPortions`.
- **The thaw estimate follows the count, always.** Each offered count carries
  its own size and its own estimate, computed by describing the remainder to
  `pantry.ThawFor` exactly as the freezer would store it. That is the rough
  edge this fixes: the old **Freeze the Rest** button recorded the whole
  surplus as a single portion, so a 54 oz remainder was quoted at about
  seventeen hours instead of the three a portion actually takes
  ([pantry-usage.md](pantry-usage.md#how-long-it-takes)).

### Finishing a card

The reserved amount is **simply not frozen** — that is the whole of "keep
10 oz out for Thursday". The surplus is sealed through
`pantry.Service.Freeze`, so the record, the usage cycle, the thaw estimate and
the idempotency are all the existing ones: the card names its hand-off line,
and sealing the same line twice changes nothing. A card redone after it froze
something keeps the count that is in the freezer, because the bags in the
drawer are the truth.

### What it may promise

The copy names a reminder only where one exists, and the only reminder there
is is the thaw sweep ([pantry-usage.md](pantry-usage.md#thaw-reminders)).

| `reminder` | When | What the card says |
| --- | --- | --- |
| `thaw` | A planned meal that needs it is still ahead | "We'll remind you the morning of any day a planned meal needs it — one portion takes about 3 hours in the fridge. Thursday is the next one." |
| `list` | Nothing is planned for it yet | "Next time a meal needs it, your list will say 'Grab from the freezer' and we'll remind you that morning to move a portion over." |
| `none` | Nothing is being frozen | Nothing |

There is deliberately no value for "we'll remember". A household that plans
nothing gets the second sentence, which is true — the freezer keeps the line
on the list as `fromFreezer`, and the reminder starts the day a meal is
planned for it.

### Nothing to prep, and coming back late

- **A week that bought nothing oversized** still has a session. Its `state` is
  `nothing_to_prep` and its headline says so, because an empty checklist is a
  good outcome and not a blank screen.
- **A household that skipped it and comes back on Wednesday** finds its cards
  where it left them: the answers are stored, everything else is derived, and
  the amounts are unchanged. What does change is the copy — a meal whose date
  has gone by is marked `past`, so the card stops naming a Thursday that has
  been, and the reminder falls back from `thaw` to `list` when nothing is left
  ahead. Nothing guesses whether the fresh portion was cooked; the card keeps
  reserving the week's need, because only the household knows.

### What is not a prep card

Only bulk packs are cards today. The card's `kind` exists so that washing
herbs or making a house-made batch could join the same checklist later, but
neither is added here: a herb card would need a rule about which produce lines
are herbs, and a batch card would need to decide what the week's specialty
choices mean, and both are their own feature rather than a cheap extra.

## Demand signal

Phase 8a ships one provider, so the Shop tab's honest answer to "can I use my
store?" is usually no. Rather than guess which grocer to build next, the API
lets a household say what it wants and counts the answers (decisions #400–#404;
endpoints in [api.md](api.md#request-a-store), the collection in
[database.md](database.md#shopping)).

- **The catalog is this document's conclusions, in code.**
  `GET /api/v1/shopping/catalog` returns a curated, committed list of the
  chains and services people actually ask for, each with a `status`:
  - `available` — DinnerOS can hand a list to it today. Walmart only.
  - `researched` — assessed in [Provider feasibility](#provider-feasibility):
    Instacart, Kroger, Albertsons, Target, Amazon Fresh and Whole Foods, and
    Shipt from the Target row. A banner inherits its parent's status, because
    the same API and the same terms cover it — Fry's, Ralphs, King Soopers,
    Smith's, Fred Meyer, QFC, Harris Teeter, Dillons, City Market, Mariano's,
    Pick 'n Save, Metro Market, Baker's, Food 4 Less, and Foods Co under
    Kroger; Safeway, Vons, Jewel-Osco, Acme, Shaw's, Star Market, Randalls,
    Tom Thumb, Pavilions, Haggen, Carrs, United Supermarkets, and Market
    Street under Albertsons. Each entry's `note` is the one-line version of
    what the research found, and every researched entry carries one.
  - `unsupported` — no integration and no research yet. Everything else: the
    remaining national and regional chains (Publix, H-E-B, Meijer, Hy-Vee,
    Wegmans, Giant, Stop & Shop, Food Lion, Hannaford, Winn-Dixie, Aldi,
    Lidl, Trader Joe's, Sprouts, ShopRite, Giant Eagle and the rest), the
    warehouse clubs, and the delivery services nobody has assessed
    (DoorDash, Uber Eats, Gopuff, FreshDirect, Weee!). These carry **no**
    `note`: a chain we have not looked at gets an honest blank rather than a
    guess, and none of them is ever marked `available` on the strength of a
    chain simply being large.
- **A store that already works can't be requested.** Asking for an
  `available` store is a `400`, whether it arrives as a `key` or as a typed
  name that matches one — Walmart is supported today, so the request would
  record demand for finished work and leave the member no closer to using it.
  The Shop tab already routes those rows to store setup (decision #423); the
  API now refuses them too, because the row is not the only way in.
- **`status` is not a promise.** It says how far this document got, never what
  a member can do right now. Only `available` takes a list, and the app must
  not offer the others as usable.
- **Requests are the input to the next phase.** `requests` counts the
  households that asked for a store across all of DinnerOS, and
  `requestedByHousehold` says whether the caller's own household is one of
  them. When an `unsupported` store collects requests, the next step is to
  research it here and move it to `researched`; when a `researched` one does,
  it argues for building that integration in the phased plan below.
- **A new request records `shopping.store_requested`** (the key, and whether
  it was a catalog entry), so demand shows up in the behavior history as well
  as in the counts. Updating a note records nothing, and the note itself never
  enters an event.
- **Asking for a store never makes it usable.** `internal/providers` stays the
  registry of what works, and a request touches nothing in it. Free text that
  matches no catalog name, key, or alias is recorded under its own normalized
  key and does **not** join the catalog — the curated list changes only when
  someone edits it here and in code.

## Phased plan

| Phase | Scope | Needs from owner | Size |
| --- | --- | --- | --- |
| **8.0 Spike** | Hand-build 3–4 item cart links and open them on an iPhone with the Walmart app: quantities, `storeId`, direct versus `goto.walmart.com`, signed out, Walmart+ delivery at checkout. Write down the results here. | A Walmart.com account (existing) | ½ day |
| **8a Walmart without keys** | Provider seam, manual provider, settings (ZIP, typed store number), preferences from pasted product links, match with sizes and package counts, handoff and link, confirm into pantry `provider` purchases, disclosure, docs, and decisions | Privacy policy URL live | 1–2 weeks |
| **8b Walmart Affiliate API** | Signed client, search, lookup and stock, store locator, alternates, Impact-tracked links | walmart.io consumer ID and key; Impact approval (optional for links) | ~1 week after keys |
| **8c Instacart** | `products_link` from the same match data (name, measurements, UPCs from preferences), nearby retailers and preferred `retailer_key`, cached URLs per list version | Dev key, then production review (≈30–40 days) | ~1 week of build |
| **8d Kroger (optional)** | OAuth account link, product search by location, direct `cart/add` | App registration | ~1–2 weeks |
| Later | Order import if any provider ships one; receipt import (opt-in) | — | — |

## Owner blockers (in order)

1. **Run the 8.0 link spike** on your iPhone (15 minutes, no signup). It
   decides whether the direct link opens the Walmart app and whether the
   affiliate wrapper breaks it.
2. **Decide the household's stores** (a Walmart store and ZIP; is a Kroger
   banner or an Instacart retailer a realistic second?). Also decide whether
   affiliate revenue matters at all. If it doesn't, you can skip Impact.
3. **Publish a privacy policy and a live public page** (for example on
   `api.tlps.dev` or a product site). Walmart rejects sites that aren't live
   or lack a privacy policy, and Instacart's review and the App Store need
   one. Decide the public product name first, because affiliate and developer
   applications are filed under a name. ~1–2 days of work.
4. **walmart.io:**
   - Sign in with a Walmart.com account, create an application, and accept
     the Walmart I/O terms.
   - Generate the RSA key pair locally, upload the **public** key, and note
     the consumer ID and key version.
   - Put `WALMART_CONSUMER_ID`, `WALMART_KEY_VERSION`, and
     `WALMART_PRIVATE_KEY` in Heroku config vars. Never commit them or paste
     them into chat.
   - Lead time: same day if automatic. Store-level lookups (`storeId`) need a
     separate business approval by email; only request it if ZIP-level
     availability proves too coarse.
5. **Walmart affiliate program on Impact** (affiliates.walmart.com): apply
   with the public page and complete Impact's tax and payment profile. Then
   set the three `WALMART_IMPACT_*` config vars. Approval is discretionary and
   not time-boxed (plan for 1–3 weeks). Ask whether a native iOS app qualifies
   and whether purchases by the publisher's own household earn commission.
6. **Instacart Developer Platform:**
   - Apply at instacart.com/company/business/developers and create a
     **development** key (`INSTACART_API_KEY` in local `.env` or a staging
     config var).
   - Create an Enterprise Help Desk account, which the review requires.
   - After 8c, create the production key to start the review and record the
     demo. About 30–40 days end to end, so start now if 8c is wanted this
     year.
7. **Kroger (only if 8d):** register a **production** app at
   developer.kroger.com with scopes `product.compact` and `cart.basic:write`
   and redirect URI `https://api.tlps.dev/…/kroger/callback`.
   - Put `KROGER_CLIENT_ID` and `KROGER_CLIENT_SECRET` in Heroku.
   - Generate `PROVIDER_TOKEN_ENCRYPTION_KEY`.
   - Same day.
8. **Legal read-through before inviting other households:**
   - Walmart I/O terms, Walmart affiliate agreement, Instacart developer terms,
     and Kroger API terms.
   - The disclosure wording, and the App Store privacy details.

## Risks and open questions

- **The cart link is a documented partner convenience, not a contract.** The
  format has already changed once (`affil.walmart.com` to
  `www.walmart.com/sc/cart/addToCart`). Keep link building in one function
  with a test fixture per format.
- **Affiliate wrapper versus universal link.** If `goto.walmart.com` lands in
  Safari instead of the app, choose between attribution and opening in the
  app. The spike decides.
- **No order truth.** Pantry purchases from handoffs are what a member
  confirms. Substitutions, removals, and out-of-stock items at picking time
  are invisible. That's the existing check-off trust model, with better
  product and size data.
- **Size parsing and count conversions.** Garlic cloves, "1 bunch", and
  "1 package" don't map to package sizes without a person's confirmation or an
  explicit ingredient conversion fact. The UI must make "check amount" lines
  obvious.
- **Store-level stock** needs extra Walmart approval. ZIP-level stock may say
  an item is available when the chosen store lacks it.
- **Walmart terms are dated 2020 and partly stale.** Get written confirmation
  from Walmart affiliate ops if anything is ambiguous, especially native-app
  use and self-referral.
- **Autopilot boundary.** Walmart forbids redistributing API product data to
  third parties. Product IDs, prices, and preferences obtained through the API
  must stay in DinnerOS. They must not feed a multi-tenant Autopilot service.
- **Instacart can't be forced to a retailer**, and Walmart's presence on
  Instacart varies by market. Don't market Instacart as "Walmart via
  Instacart".
- **Kroger refresh tokens** become the first stored third-party user
  credential. Encrypt them at rest, revoke on unlink, and delete them when a
  member leaves the household.
- **Amazon:** PA-API 5 was retired 2026-05-15, and the Creators API needs
  Associates sales history. Revisit only if Amazon publishes a grocery cart
  handoff.

## Sources

Accessed 2026-09-15 unless noted. walmart.io pages render with JavaScript and
were read in a browser.

Walmart

- Consolidated Add to Cart: https://walmart.io/docs/atc/v1/add-to-cart
- GM Add To Cart (legacy, Impact-only, deprecated): https://walmart.io/docs/affiliate/gm-add-to-cart
- Affiliate API introduction: https://walmart.io/docs/affiliates/v1/introduction
- Search API: https://walmart.io/docs/affiliates/v1/search
- Product Lookup API: https://walmart.io/docs/affiliates/v1/product-lookup
- Stores API: https://walmart.io/docs/affiliates/v1/stores
- Item response groups: https://walmart.io/docs/affiliates/v1/item-response-groups
- Signature headers: https://walmart.io/docs/affiliates/v1/additional-headers
- Quick start (onboarding steps): https://walmart.io/apidocs/affiliates/quickstart
- Walmart I/O terms (last updated 2020-09-30): https://www.walmart.io/termsandcondition
- Affiliate program FAQ: https://affiliates.walmart.com/faqs
- Walmart app universal link paths: https://www.walmart.com/.well-known/apple-app-site-association
- Walmart+ delivery benefits (blocked by a bot check; not read): https://www.walmart.com/help/article/walmart-benefits-free-shipping-and-free-delivery-from-your-store/d1738a201207485c99fd53ccdbc49699
- Shoppable-recipe partners: https://www.sidechef.com/walmart-plus/, https://business.pinterest.com/blog/turning-recipe-inspiration-into-shoppable-experiences-with-walmart/, https://progressivegrocer.com/walmart-offering-shoppable-recipes-tasty-app
- Walmart on Instacart (2021): https://www.grocerydive.com/news/walmart-expands-instacart-pilot-to-new-york-city/605919/

Instacart

- Create shopping list page: https://docs.instacart.com/developer_platform_api/api/products/create_shopping_list_page
- Shopping list page concept: https://docs.instacart.com/developer_platform_api/guide/concepts/shopping_list/
- Get nearby retailers: https://docs.instacart.com/developer_platform_api/api/retailers/get_nearby_retailers/
- FAQ: https://docs.instacart.com/developer_platform_api/faq/
- Changelog: https://docs.instacart.com/developer_platform_api/api/changelog/
- Get started (timeline): https://docs.instacart.com/developer_platform_api/get_started/overview
- API keys: https://docs.instacart.com/developer_platform_api/get_started/api-keys
- Approval process: https://docs.instacart.com/developer_platform_api/guide/concepts/launch_activities/approval_process
- Developer terms (updated 2024-07-03): https://docs.instacart.com/developer_platform_api/guide/terms_and_policies/developer_terms
- Application: https://www.instacart.com/company/business/developers

Kroger

- API basics (environments, rate limiting): https://developer.kroger.com/documentation/public/getting-started/apis
- Cart API overview: https://developer-ce.kroger.com/documentation/api-products/public/cart/overview
- Products API overview: https://developer.kroger.com/documentation/api-products/public/products/overview
- Location API: https://developer.kroger.com/reference/api/location-api-public
- OAuth2 guide: https://developer.kroger.com/documentation/partner/guides/guides-oauth
- Cart request shape (third-party Go client): https://pkg.go.dev/github.com/densestvoid/krogerrecipeshopper/kroger

AnyList (accessed 2026-09-21)

- Copy and paste a list of items: https://help.anylist.com/articles/paste-items/
- Adding items (every documented way): https://help.anylist.com/topics/lists/adding-items/
- Reminders app import: https://help.anylist.com/articles/reminders-import/
- Recipe import share extension (recipes only): https://help.anylist.com/articles/recipe-extension/
- Using AnyList with Siri: https://help.anylist.com/articles/siri/
- Unofficial reverse-engineered client (not used): https://github.com/codetheweb/anylist
- 2016 request for a URL scheme, unanswered: https://discourse.omnigroup.com/t/link-to-shopping-list-in-anylisy/28251

Others

- Amazon PA-API 5 deprecation and Creators API: https://affiliate-program.amazon.com/creatorsapi/docs/en-us/paapiv5-deprecation, https://affiliate-program.amazon.com/creatorsapi/docs/en-us/introduction
- No public Amazon Fresh or Whole Foods API: https://repost.aws/questions/QUAWoPILDhS_6j458Ih6zAzQ/is-there-an-api-for-amazon-fresh-or-whole-foods
- Target basket transfer sample (archived): https://github.com/target/cartster
- Shipt partner developer portal: https://partner.shipt.com/developer/docs/deliveries
- Albertsons public APIs (retail media): https://github.com/api-evangelist/albertsons
- FTC Endorsement Guides FAQ: https://www.ftc.gov/business-guidance/resources/ftcs-endorsement-guides-what-people-are-asking
- 16 CFR Part 255: https://www.ecfr.gov/current/title-16/chapter-I/subchapter-B/part-255
- App Store Review Guidelines: https://developer.apple.com/app-store/review/guidelines/
