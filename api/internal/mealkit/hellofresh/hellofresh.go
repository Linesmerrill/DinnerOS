// Package hellofresh is the HelloFresh implementation of mealkit.Source: it
// reads the account's own order history with tokens the member's own browser
// session produced, and fetches the public recipe page of each recipe the
// household actually received.
//
// There is deliberately no sign-in here. The member signs in on HelloFresh's
// own page inside the app (a web view, HelloFresh's real domain, their
// password manager), and the app sends us the session the login produced. No
// password ever reaches this process, so there is nothing here to exchange
// one with.
//
// It never browses. The only things it requests are the account's own plans,
// its past-deliveries endpoint, and the recipe pages those deliveries named,
// and every recipe URL is checked against RecipeURLPrefix before a request is
// made, so data from the service can never point the fetcher somewhere else.
//
// Everything it reads is data. A response that does not have the shape this
// build expects is a *mealkit.ParseError — a layout change, reported as one —
// and never a partially-read recipe.
package hellofresh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Where HelloFresh lives. Both are overridable in Options so tests (and a
// future endpoint change) do not need a code change.
const (
	// DefaultBaseURL is the account API's origin.
	DefaultBaseURL = "https://www.hellofresh.com"
	// RecipeURLPrefix is the only prefix a recipe page URL may have. A
	// delivery record that names anything else is skipped, not fetched.
	RecipeURLPrefix = "https://www.hellofresh.com/recipes/"
	// imageBaseURL turns a stored image path into a URL, as the offline
	// importer does (importers/hellofresh/normalize.go).
	imageBaseURL = "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3"
)

// Paths on the account API. Both were captured from a signed-in browser
// session; see docs/meal-kit-import.md for what is verified and what is not.
const (
	// plansPath lists the account's subscriptions. The one we want is the
	// "subscription" query parameter past-deliveries needs.
	plansPath = "/gw/api/plans"
	// pastDeliveriesPath is the account's own delivered weeks.
	pastDeliveriesPath = "/gw/my-deliveries/past-deliveries"
	// maxDeliveryPages bounds the history walk, so a paging bug can never
	// turn into an unbounded crawl.
	maxDeliveryPages = 40
	// defaultCountry and defaultLocale are what the endpoints want when the
	// deployment does not say. A household outside the US needs these set.
	defaultCountry = "US"
	defaultLocale  = "en-US"
	// ratingScale is a required query parameter; its value does not change
	// which deliveries come back.
	ratingScale = "5"
	// maxExcerptBytes bounds the redacted body excerpt a debug log may carry.
	maxExcerptBytes = 300
)

// Options configures a Client.
type Options struct {
	// Fetcher makes every request. Nil means mealkit.NewFetcher().
	Fetcher *mealkit.Fetcher
	// BaseURL overrides DefaultBaseURL.
	BaseURL string
	// RecipeURLPrefix overrides RecipeURLPrefix, for tests.
	RecipeURLPrefix string
	// Country and Locale are the account API's query parameters. Empty means
	// defaultCountry and defaultLocale.
	Country string
	Locale  string
	// Now is the clock: the history walk starts at the current ISO week.
	Now func() time.Time
	// Logger, when set, receives debug-level diagnostics about a response
	// that could not be read. It never receives a token, a cookie, or more
	// than a short redacted excerpt.
	Logger *slog.Logger
}

// Client is the HelloFresh Source.
type Client struct {
	fetcher *mealkit.Fetcher
	baseURL string
	prefix  string
	country string
	locale  string
	now     func() time.Time
	logger  *slog.Logger
}

var _ mealkit.Source = (*Client)(nil)

// New returns a Client.
func New(opts Options) *Client {
	c := &Client{
		fetcher: opts.Fetcher,
		baseURL: strings.TrimSuffix(opts.BaseURL, "/"),
		prefix:  opts.RecipeURLPrefix,
		country: strings.TrimSpace(opts.Country),
		locale:  strings.TrimSpace(opts.Locale),
		now:     opts.Now,
		logger:  opts.Logger,
	}
	if c.country == "" {
		c.country = defaultCountry
	}
	if c.locale == "" {
		c.locale = defaultLocale
	}
	if c.logger == nil {
		c.logger = slog.New(slog.DiscardHandler)
	}
	if c.fetcher == nil {
		c.fetcher = mealkit.NewFetcher()
	}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	if c.prefix == "" {
		c.prefix = RecipeURLPrefix
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// Name implements mealkit.Source.
func (c *Client) Name() string { return mealkit.SourceHelloFresh }

// Requests is how many HTTP requests this client has made, for the run log.
func (c *Client) Requests() int { return c.fetcher.Requests() }

// Refresh implements mealkit.Source.
//
// HelloFresh's refresh request has NOT been observed, so this build does not
// guess at one: inventing a contract would mean firing an unknown request at
// someone else's auth service and, when it failed, retrying it. Returning
// ErrAuthExpired is the honest answer — the worker pauses the job at
// paused_auth and the member re-links through the web login, which is a few
// taps and always works.
//
// When the refresh request is captured the same way the rest of this file was,
// it belongs here and nothing else changes.
func (c *Client) Refresh(_ context.Context, _ mealkit.Tokens) (mealkit.Tokens, error) {
	return mealkit.Tokens{}, mealkit.ErrAuthExpired
}

// plansResponse is the account's own subscriptions. The field names are
// unverified, so every plausible envelope is tried before giving up; what
// matters is finding one subscription id.
type plansResponse struct {
	Items []plan `json:"items"`
	Plans []plan `json:"plans"`
}

type plan struct {
	ID             string `json:"id"`
	SubscriptionID string `json:"subscriptionId"`
	Status         string `json:"status"`
}

func (p plan) id() string { return strings.TrimSpace(firstNonEmpty(p.SubscriptionID, p.ID)) }

// pastDeliveriesResponse is one page of the account's own delivered weeks,
// captured from a signed-in session.
type pastDeliveriesResponse struct {
	Weeks []deliveryWeek `json:"weeks"`
}

type deliveryWeek struct {
	// Week is an ISO week, e.g. "2026-W38".
	Week   string           `json:"week"`
	MenuID string           `json:"menuId"`
	Meals  []deliveryRecipe `json:"meals"`
	// Addons are the sides and extras delivered alongside the meals. They
	// were ordered and eaten too, so they are imported, flagged as add-ons.
	Addons []deliveryRecipe `json:"addons"`
}

type deliveryRecipe struct {
	ID string `json:"id"`
	// Name is the recipe's title; it is used for a fallback slug only.
	Name string `json:"name"`
	// WebsiteURL is the recipe page. It is followed only when it already has
	// the allowed prefix.
	WebsiteURL string `json:"websiteURL"`
}

// subscriptionIDRe is what a subscription id may look like. It is applied
// before the value reaches a query string, so account data cannot smuggle
// anything into the request we make.
var subscriptionIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// subscriptionID reads the account's own plans and returns the subscription
// past-deliveries needs.
func (c *Client) subscriptionID(ctx context.Context, t mealkit.Tokens) (string, error) {
	endpoint := c.baseURL + plansPath + "?includeCanceled=false"
	resp, err := c.get(ctx, endpoint, t)
	if err != nil {
		return "", err
	}
	var envelope plansResponse
	candidates := []plan{}
	if err := json.Unmarshal(resp.Body, &envelope); err == nil {
		candidates = append(candidates, envelope.Items...)
		candidates = append(candidates, envelope.Plans...)
	}
	if len(candidates) == 0 {
		// Some of these gateway endpoints answer with a bare array.
		var bare []plan
		if err := json.Unmarshal(resp.Body, &bare); err == nil {
			candidates = bare
		}
	}
	for _, p := range candidates {
		if id := p.id(); subscriptionIDRe.MatchString(id) {
			return id, nil
		}
	}
	c.diagnose("plans", resp)
	return "", &mealkit.ParseError{
		Subject: "the account's meal-kit plan",
		Detail:  "HelloFresh did not name a subscription this build can read; the account API has probably changed",
	}
}

// get makes one authorized JSON request.
func (c *Client) get(ctx context.Context, endpoint string, t mealkit.Tokens) (mealkit.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return mealkit.Response{}, err
	}
	req.Header.Set("Accept", "application/json")
	// The session came from the member's own browser login. Nothing logs it.
	req.Header.Set("Authorization", "Bearer "+t.AccessToken)
	return c.fetcher.Do(ctx, req)
}

// recipeIDRe is HelloFresh's recipe ID: 24 hex characters.
var recipeIDRe = regexp.MustCompile(`^[a-f0-9]{24}$`)

// OrderHistory implements mealkit.Source. It reads only this account's
// deliveries; no catalog, menu, or browse endpoint is ever requested.
//
// Recipes delivered in more than one week merge into one entry carrying every
// week, which is what the import contract's orderWeeks means.
// The history is walked backwards a page at a time: `from` starts at the
// current ISO week and moves to the week before the earliest week each page
// returned. It stops at an empty page, at maxDeliveryPages, or as soon as a
// page fails to move `from` backwards, so a paging change cannot become a
// loop.
func (c *Client) OrderHistory(ctx context.Context, t mealkit.Tokens) ([]mealkit.OrderedRecipe, error) {
	subscription, err := c.subscriptionID(ctx, t)
	if err != nil {
		return nil, err
	}

	byID := map[string]*mealkit.OrderedRecipe{}
	var order []string
	seenWeek := map[string]bool{}
	from := c.now().UTC()

	for page := 0; page < maxDeliveryPages; page++ {
		resp, err := c.get(ctx, c.pastDeliveriesURL(subscription, from), t)
		if err != nil {
			return nil, err
		}
		var body pastDeliveriesResponse
		if err := json.Unmarshal(resp.Body, &body); err != nil {
			c.diagnose("past-deliveries", resp)
			return nil, &mealkit.ParseError{
				Subject: "the order history",
				Detail:  "HelloFresh answered with something this build cannot read; the account API has probably changed",
			}
		}
		if len(body.Weeks) == 0 {
			break
		}

		earliest := time.Time{}
		for _, w := range body.Weeks {
			week := strings.TrimSpace(w.Week)
			if !isoWeekRe.MatchString(week) {
				week = ""
			} else if monday, ok := parseISOWeek(week); ok && (earliest.IsZero() || monday.Before(earliest)) {
				earliest = monday
			}
			if week != "" {
				if seenWeek[week] {
					continue
				}
				seenWeek[week] = true
			}
			c.collect(w, week, byID, &order)
		}

		if earliest.IsZero() {
			// No readable week to step back from; stop rather than ask for
			// the same page again.
			break
		}
		next := earliest.AddDate(0, 0, -7)
		if !next.Before(from) {
			break
		}
		from = next
	}

	out := make([]mealkit.OrderedRecipe, 0, len(order))
	for _, id := range order {
		r := byID[id]
		sort.Strings(r.Weeks)
		out = append(out, *r)
	}
	return out, nil
}

// pastDeliveriesURL builds the request for the week `at` falls in. Every value
// goes through url.Values, so nothing can be smuggled into the query.
func (c *Client) pastDeliveriesURL(subscription string, at time.Time) string {
	q := url.Values{
		"country":      {c.country},
		"from":         {isoWeekString(at)},
		"locale":       {c.locale},
		"rating-scale": {ratingScale},
		"subscription": {subscription},
	}
	return c.baseURL + pastDeliveriesPath + "?" + q.Encode()
}

// collect folds one delivered week's meals and add-ons into the result.
//
// Add-ons are imported, flagged IsAddon: the household paid for them, ate
// them, and will want to cook them again; the flag is already carried through
// the import contract, so the library can tell a side from a main without us
// dropping half of what was delivered.
func (c *Client) collect(w deliveryWeek, week string, byID map[string]*mealkit.OrderedRecipe, order *[]string) {
	add := func(r deliveryRecipe, isAddon bool) {
		id := strings.TrimSpace(r.ID)
		if !recipeIDRe.MatchString(id) {
			return
		}
		page := c.recipeURL(r, id)
		if page == "" {
			return
		}
		existing, ok := byID[id]
		if !ok {
			byID[id] = &mealkit.OrderedRecipe{
				SourceRecipeID: id,
				Name:           strings.TrimSpace(r.Name),
				URL:            page,
				IsAddon:        isAddon,
			}
			*order = append(*order, id)
			existing = byID[id]
		}
		if week != "" && !contains(existing.Weeks, week) {
			existing.Weeks = append(existing.Weeks, week)
		}
	}
	for _, r := range w.Meals {
		add(r, false)
	}
	for _, r := range w.Addons {
		add(r, true)
	}
}

// diagnose records, at debug level only, what came back when a response could
// not be read — enough to tell a redirect from a layout change on the next
// live attempt, and never a credential.
//
// The excerpt is a RESPONSE body, so it cannot contain the member's password:
// they never typed one into this process. It is redacted anyway, because a
// body from an auth-adjacent endpoint can carry a token.
func (c *Client) diagnose(what string, resp mealkit.Response) {
	c.logger.Debug("meal-kit response was unreadable",
		"source", mealkit.SourceHelloFresh,
		"endpoint", what,
		"contentType", resp.Header.Get("Content-Type"),
		"finalPath", finalPath(resp.FinalURL),
		"bytes", len(resp.Body),
		"excerpt", redactExcerpt(resp.Body))
}

// recipeURL returns the page to fetch for a delivered recipe, or "" when the
// delivery record points anywhere but a HelloFresh recipe page.
//
// The service's own websiteURL is only used when it already has the allowed
// prefix; otherwise the URL is built from the ID we validated ourselves and
// the recipe's own name. Data from the account can therefore never choose the
// host we talk to.
func (c *Client) recipeURL(r deliveryRecipe, id string) string {
	if u := strings.TrimSpace(r.WebsiteURL); strings.HasPrefix(u, c.prefix) {
		return u
	}
	slug := slugPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(r.Name)), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return c.prefix + id
	}
	return c.prefix + slug + "-" + id
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

// nextDataRe finds the JSON the recipe page embeds.
var nextDataRe = regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json"[^>]*>([\s\S]*?)</script>`)

// Recipe implements mealkit.Source: it fetches one recipe page and normalizes
// it into the shared import contract.
func (c *Client) Recipe(ctx context.Context, t mealkit.Tokens, o mealkit.OrderedRecipe) (recipes.ImportRecipe, []recipes.ImportReviewItem, error) {
	if !strings.HasPrefix(o.URL, c.prefix) {
		return recipes.ImportRecipe{}, nil, &mealkit.ParseError{
			Subject: "recipe " + o.SourceRecipeID,
			Detail:  "its page link does not point at a HelloFresh recipe page",
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.URL, nil)
	if err != nil {
		return recipes.ImportRecipe{}, nil, err
	}
	req.Header.Set("Accept", "text/html")
	resp, err := c.fetcher.Do(ctx, req)
	if err != nil {
		return recipes.ImportRecipe{}, nil, err
	}
	if !strings.HasPrefix(resp.FinalURL, c.prefix) {
		return recipes.ImportRecipe{}, nil, &mealkit.ParseError{
			Subject: "recipe " + o.SourceRecipeID,
			Detail:  "the request was redirected away from HelloFresh's recipe pages",
		}
	}
	raw, err := extractRecipe(resp.Body, o.SourceRecipeID)
	if err != nil {
		return recipes.ImportRecipe{}, nil, err
	}
	out, review := normalize(o, raw, resp.FinalURL)
	return out, review, nil
}

// hfRecipe is the subset of HelloFresh's recipe object this build reads. It
// mirrors importers/hellofresh/normalize.go, which is the tested reference for
// this shape.
type hfRecipe struct {
	Name          string         `json:"name"`
	Headline      string         `json:"headline"`
	Description   string         `json:"description"`
	ImagePath     string         `json:"imagePath"`
	WebsiteURL    string         `json:"websiteUrl"`
	CanonicalLink string         `json:"canonicalLink"`
	PrepTime      string         `json:"prepTime"`
	TotalTime     string         `json:"totalTime"`
	Difficulty    int            `json:"difficulty"`
	IsAddon       bool           `json:"isAddon"`
	Cuisines      []named        `json:"cuisines"`
	Tags          []named        `json:"tags"`
	Utensils      []named        `json:"utensils"`
	Allergens     []named        `json:"allergens"`
	Nutrition     []hfNutrient   `json:"nutrition"`
	Ingredients   []hfIngredient `json:"ingredients"`
	Yields        []hfYield      `json:"yields"`
	Steps         []hfStep       `json:"steps"`
}

type named struct {
	Name string `json:"name"`
}

type hfNutrient struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

type hfIngredient struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	ImagePath string `json:"imagePath"`
	Shipped   bool   `json:"shipped"`
}

type hfYield struct {
	Yields      int `json:"yields"`
	Ingredients []struct {
		ID     string   `json:"id"`
		Amount *float64 `json:"amount"`
		Unit   string   `json:"unit"`
	} `json:"ingredients"`
}

type hfStep struct {
	Index        int    `json:"index"`
	Instructions string `json:"instructions"`
	Images       []struct {
		Path string `json:"path"`
	} `json:"images"`
}

// extractRecipe pulls the recipe object out of a page. Every failure is a
// ParseError: the page is not what this build knows how to read, and nothing
// is guessed from the surrounding HTML.
func extractRecipe(html []byte, id string) (hfRecipe, error) {
	subject := "recipe " + id
	m := nextDataRe.FindSubmatch(html)
	if m == nil {
		return hfRecipe{}, &mealkit.ParseError{Subject: subject, Detail: "the recipe page no longer embeds the data this build reads; HelloFresh has changed its page layout"}
	}
	var page struct {
		Props struct {
			PageProps struct {
				SSRPayload struct {
					Recipe json.RawMessage `json:"recipe"`
				} `json:"ssrPayload"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(m[1], &page); err != nil {
		return hfRecipe{}, &mealkit.ParseError{Subject: subject, Detail: "the data embedded in the recipe page is not the JSON this build expects"}
	}
	raw := bytes.TrimSpace(page.Props.PageProps.SSRPayload.Recipe)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return hfRecipe{}, &mealkit.ParseError{Subject: subject, Detail: "the page carried no recipe"}
	}
	var out hfRecipe
	if err := json.Unmarshal(raw, &out); err != nil {
		return hfRecipe{}, &mealkit.ParseError{Subject: subject, Detail: "the recipe in the page is not the shape this build expects"}
	}
	if strings.TrimSpace(out.Name) == "" {
		return hfRecipe{}, &mealkit.ParseError{Subject: subject, Detail: "the recipe in the page has no name"}
	}
	return out, nil
}

// sourceUnits maps HelloFresh units to DinnerOS unit codes. It is the same
// table the offline importer uses; an unknown unit is flagged for review
// rather than guessed.
var sourceUnits = map[string]string{
	"unit": "count", "units": "count", "piece": "count", "pieces": "count",
	"clove": "clove", "cloves": "clove",
	"can": "can", "cans": "can",
	"package": "package", "packages": "package", "packet": "package", "packets": "package",
	"slice": "slice", "slices": "slice",
	"bunch": "bunch", "bunches": "bunch",
	"thumb": "thumb", "thumbs": "thumb",
	"teaspoon": "tsp", "teaspoons": "tsp", "tsp": "tsp", "teaspoon (tsp)": "tsp",
	"tablespoon": "tbsp", "tablespoons": "tbsp", "tbsp": "tbsp", "tablespoon (tbsp)": "tbsp",
	"cup": "cup", "cups": "cup",
	"fluid ounce": "floz", "fluid ounces": "floz", "fl oz": "floz",
	"ounce": "oz", "ounces": "oz", "oz": "oz",
	"pound": "lb", "pounds": "lb", "lb": "lb",
	"gram": "g", "grams": "g", "g": "g",
	"kilogram": "kg", "kilograms": "kg", "kg": "kg",
	"milliliter": "ml", "milliliters": "ml", "millilitre": "ml", "millilitres": "ml", "ml": "ml",
	"liter": "l", "liters": "l", "l": "l",
	"pinch": "pinch",
	"pick":  "count", "picks": "count",
}

// normalize converts a fetched recipe into the import contract, flagging
// everything it could not map confidently instead of inventing a value.
func normalize(o mealkit.OrderedRecipe, r hfRecipe, finalURL string) (recipes.ImportRecipe, []recipes.ImportReviewItem) {
	var review []recipes.ImportReviewItem
	flag := func(field, value, reason string) {
		review = append(review, recipes.ImportReviewItem{
			SourceRecipeID: o.SourceRecipeID, RecipeName: strings.TrimSpace(r.Name),
			Field: field, Value: value, Reason: reason,
		})
	}

	out := recipes.ImportRecipe{
		Source:         mealkit.SourceHelloFresh,
		SourceRecipeID: o.SourceRecipeID,
		SourceURL:      firstNonEmpty(r.CanonicalLink, r.WebsiteURL, finalURL),
		Name:           strings.TrimSpace(r.Name),
		Headline:       strings.TrimSpace(r.Headline),
		Description:    strings.TrimSpace(r.Description),
		ImageURL:       imageURL(r.ImagePath),
		IsAddon:        r.IsAddon || o.IsAddon,
		Difficulty:     r.Difficulty,
		Cuisines:       names(r.Cuisines),
		Tags:           names(r.Tags),
		Utensils:       names(r.Utensils),
		Allergens:      names(r.Allergens),
		OrderWeeks:     append([]string(nil), o.Weeks...),
	}

	if m, ok := parseISODurationMinutes(r.PrepTime); ok {
		out.PrepMinutes = m
	} else if r.PrepTime != "" {
		flag("prepTime", r.PrepTime, "unparseable duration")
	}
	if m, ok := parseISODurationMinutes(r.TotalTime); ok {
		out.TotalMinutes = m
	} else if r.TotalTime != "" {
		flag("totalTime", r.TotalTime, "unparseable duration")
	}
	if out.PrepMinutes <= 0 && out.TotalMinutes <= 0 {
		flag("cookTime", "", "recipe has neither a prep nor a total time; its cook time is unknown")
	}

	for _, n := range r.Nutrition {
		out.Nutrition = append(out.Nutrition, recipes.ImportNutrient(n))
	}

	type amountRef struct {
		servings int
		amount   *float64
		unit     string
	}
	servingSet := map[int]bool{}
	amounts := map[string][]amountRef{}
	for _, y := range r.Yields {
		if y.Yields <= 0 {
			continue
		}
		servingSet[y.Yields] = true
		for _, ya := range y.Ingredients {
			amounts[ya.ID] = append(amounts[ya.ID], amountRef{y.Yields, ya.Amount, ya.Unit})
		}
	}
	for s := range servingSet {
		out.Servings = append(out.Servings, s)
	}
	sort.Ints(out.Servings)
	if len(out.Servings) == 0 {
		flag("yields", "", "recipe has no serving sizes")
	}

	for _, ing := range r.Ingredients {
		line := recipes.ImportIngredient{
			SourceIngredientID: ing.ID,
			Name:               strings.TrimSpace(ing.Name),
			Slug:               ing.Slug,
			ImageURL:           imageURL(ing.ImagePath),
			PantryStaple:       !ing.Shipped,
		}
		refs := amounts[ing.ID]
		sort.SliceStable(refs, func(i, j int) bool { return refs[i].servings < refs[j].servings })
		for _, ref := range refs {
			sourceUnit := strings.TrimSpace(strings.ToLower(ref.unit))
			a := recipes.ImportAmount{Servings: ref.servings, SourceUnit: ref.unit}
			if ref.amount != nil && *ref.amount > 0 {
				q := *ref.amount
				a.Quantity = &q
			}
			if code, ok := sourceUnits[sourceUnit]; ok {
				a.Unit = code
			} else if sourceUnit != "" {
				flag("ingredients."+line.Name+".unit", ref.unit, "unknown unit")
			}
			a.RawText = rawText(a.Quantity, ref.unit, line.Name)
			line.Amounts = append(line.Amounts, a)
		}
		if len(refs) == 0 {
			flag("ingredients."+line.Name, "", "ingredient has no amounts in any yield")
		}
		out.Ingredients = append(out.Ingredients, line)
	}

	steps := append([]hfStep(nil), r.Steps...)
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Index < steps[j].Index })
	for i, s := range steps {
		st := recipes.ImportStep{Index: i + 1, Text: cleanInstructions(s.Instructions)}
		if len(s.Images) > 0 {
			st.ImageURL = imageURL(s.Images[0].Path)
		}
		out.Steps = append(out.Steps, st)
	}
	if len(out.Steps) == 0 {
		flag("steps", "", "recipe has no steps")
	}
	return out, review
}

var isoDurationRe = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// parseISODurationMinutes parses durations like "PT1H15M" into whole minutes.
func parseISODurationMinutes(s string) (int, bool) {
	s = strings.TrimSpace(s)
	m := isoDurationRe.FindStringSubmatch(s)
	if m == nil || s == "P" || s == "PT" {
		return 0, false
	}
	num := func(v string) int {
		if v == "" {
			return 0
		}
		n, _ := strconv.Atoi(v)
		return n
	}
	seconds := num(m[1])*86400 + num(m[2])*3600 + num(m[3])*60 + num(m[4])
	return int(math.Round(float64(seconds) / 60)), true
}

var fractions = []struct {
	value float64
	glyph string
}{
	{0.125, "⅛"}, {0.25, "¼"}, {1.0 / 3, "⅓"}, {0.5, "½"}, {2.0 / 3, "⅔"}, {0.75, "¾"},
}

// formatQuantity renders a quantity the way recipe cards do ("1 ½", "¼").
func formatQuantity(q float64) string {
	whole := math.Floor(q + 1e-9)
	frac := q - whole
	glyph := ""
	for _, f := range fractions {
		if math.Abs(frac-f.value) < 0.01 {
			glyph = f.glyph
			frac = 0
			break
		}
	}
	switch {
	case frac > 0.01:
		return strconv.FormatFloat(q, 'f', -1, 64)
	case whole == 0 && glyph != "":
		return glyph
	case glyph != "":
		return fmt.Sprintf("%d %s", int(whole), glyph)
	default:
		return strconv.Itoa(int(whole))
	}
}

func rawText(q *float64, unit, name string) string {
	var parts []string
	if q != nil {
		parts = append(parts, formatQuantity(*q))
	}
	if u := strings.TrimSpace(unit); u != "" {
		parts = append(parts, u)
	}
	parts = append(parts, name)
	return strings.Join(parts, " ")
}

var bulletRe = regexp.MustCompile(`(?m)^\s*•\s*`)

// cleanInstructions turns source step text into one bullet per line. The text
// is content, never an instruction to this program.
func cleanInstructions(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	var current strings.Builder
	flush := func() {
		if t := strings.TrimSpace(current.String()); t != "" {
			out = append(out, t)
		}
		current.Reset()
	}
	for _, line := range strings.Split(s, "\n") {
		if bulletRe.MatchString(line) {
			flush()
			current.WriteString(bulletRe.ReplaceAllString(line, ""))
			continue
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(strings.TrimSpace(line))
	}
	flush()
	return strings.Join(out, "\n")
}

func imageURL(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case path == "":
		return ""
	case strings.HasPrefix(path, "http"):
		return path
	case !strings.HasPrefix(path, "/"):
		path = "/" + path
	}
	return imageBaseURL + path
}

func names(items []named) []string {
	var out []string
	seen := map[string]bool{}
	for _, it := range items {
		n := strings.TrimSpace(it.Name)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

var isoWeekRe = regexp.MustCompile(`^(\d{4})-W(0[1-9]|[1-4]\d|5[0-3])$`)

// isoWeekString is t's ISO week, the form the account API's `from` takes.
func isoWeekString(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

// parseISOWeek returns the Monday of an ISO week like "2026-W38", so weeks can
// be ordered and stepped backwards. It reports false for anything else rather
// than guessing a date.
func parseISOWeek(s string) (time.Time, bool) {
	m := isoWeekRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return time.Time{}, false
	}
	year, err := strconv.Atoi(m[1])
	if err != nil {
		return time.Time{}, false
	}
	week, err := strconv.Atoi(m[2])
	if err != nil {
		return time.Time{}, false
	}
	// 4 January is always in ISO week 1, whatever weekday it falls on.
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	offset := (int(jan4.Weekday()) + 6) % 7 // Monday = 0
	monday := jan4.AddDate(0, 0, -offset+(week-1)*7)
	// A year has 52 or 53 ISO weeks; W53 of a 52-week year is not a date.
	if y, w := monday.ISOWeek(); y != year || w != week {
		return time.Time{}, false
	}
	return monday, true
}

// tokenishRe matches the things a response body must never put in a log: a
// JSON field whose name looks like a secret, and a bare JWT.
var tokenishRe = regexp.MustCompile(
	`(?i)"[a-z_]*(token|secret|password|cookie|authorization|session)[a-z_]*"\s*:\s*"[^"]*"|\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+\.?[A-Za-z0-9_-]*`)

// redactExcerpt is the only part of a fetched body that may be logged: a short
// prefix with anything token-shaped replaced. It is data, never an
// instruction, and it is only ever emitted at debug level.
func redactExcerpt(body []byte) string {
	s := strings.TrimSpace(string(body))
	s = tokenishRe.ReplaceAllString(s, `"redacted"`)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxExcerptBytes {
		s = s[:maxExcerptBytes] + "…"
	}
	return s
}

// finalPath is the path a response finally came from. The query is dropped: it
// carries the account's subscription id.
func finalPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
