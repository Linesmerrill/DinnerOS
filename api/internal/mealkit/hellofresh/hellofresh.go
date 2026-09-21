// Package hellofresh is the HelloFresh implementation of mealkit.Source: it
// signs in once, reads the account's own order history, and fetches the public
// recipe page of each recipe the household actually received.
//
// It never browses. The only pages it requests are the account's deliveries
// endpoint and the recipe pages whose IDs that endpoint named, and every URL
// is checked against RecipeURLPrefix before a request is made, so data from
// the service can never point the fetcher somewhere else.
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

// Paths on the account API.
const (
	tokenPath     = "/gw/auth/token"
	deliveryPath  = "/gw/api/customers/me/deliveries"
	deliveryLimit = 50
	// maxDeliveryPages bounds the history walk, so a paging bug can never
	// turn into an unbounded crawl.
	maxDeliveryPages = 40
)

// Options configures a Client.
type Options struct {
	// Fetcher makes every request. Nil means mealkit.NewFetcher().
	Fetcher *mealkit.Fetcher
	// BaseURL overrides DefaultBaseURL.
	BaseURL string
	// RecipeURLPrefix overrides RecipeURLPrefix, for tests.
	RecipeURLPrefix string
	// Now is the clock, for turning delivery dates into ISO weeks.
	Now func() time.Time
}

// Client is the HelloFresh Source.
type Client struct {
	fetcher *mealkit.Fetcher
	baseURL string
	prefix  string
	now     func() time.Time
}

var _ mealkit.Source = (*Client)(nil)

// New returns a Client.
func New(opts Options) *Client {
	c := &Client{
		fetcher: opts.Fetcher,
		baseURL: strings.TrimSuffix(opts.BaseURL, "/"),
		prefix:  opts.RecipeURLPrefix,
		now:     opts.Now,
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

// tokenResponse is the shape of the token endpoint's answer.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// SignIn implements mealkit.Source. The password is used here and never
// leaves this function: it is not returned, stored, or logged.
func (c *Client) SignIn(ctx context.Context, email, password string) (mealkit.Tokens, error) {
	form := url.Values{
		"grant_type": {"password"},
		"username":   {email},
		"password":   {password},
	}
	return c.token(ctx, form)
}

// Refresh implements mealkit.Source.
func (c *Client) Refresh(ctx context.Context, t mealkit.Tokens) (mealkit.Tokens, error) {
	if strings.TrimSpace(t.RefreshToken) == "" {
		return mealkit.Tokens{}, mealkit.ErrAuthExpired
	}
	return c.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {t.RefreshToken},
	})
}

func (c *Client) token(ctx context.Context, form url.Values) (mealkit.Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+tokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return mealkit.Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// The body carries the credential; nothing logs the request.
	resp, err := c.fetcher.Do(ctx, req)
	if err != nil {
		return mealkit.Tokens{}, err
	}
	var body tokenResponse
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return mealkit.Tokens{}, &mealkit.ParseError{Subject: "the sign-in response", Detail: "it was not the JSON this build expects"}
	}
	if strings.TrimSpace(body.AccessToken) == "" {
		return mealkit.Tokens{}, mealkit.ErrAuthExpired
	}
	out := mealkit.Tokens{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken}
	if body.ExpiresIn > 0 {
		out.ExpiresAt = c.now().UTC().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	return out, nil
}

// deliveryPage is the shape of one page of the account's own deliveries.
type deliveryPage struct {
	Items []deliveryItem `json:"items"`
	Total int            `json:"total"`
}

type deliveryItem struct {
	// Week is an ISO week when the service gives one; otherwise DeliveryDate
	// is used.
	Week         string           `json:"week"`
	DeliveryDate string           `json:"deliveryDate"`
	Meals        []deliveryRecipe `json:"meals"`
	Recipes      []deliveryRecipe `json:"recipes"`
}

type deliveryRecipe struct {
	ID         string `json:"id"`
	RecipeID   string `json:"recipeId"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	WebsiteURL string `json:"websiteUrl"`
	IsAddon    bool   `json:"isAddon"`
}

// recipeIDRe is HelloFresh's recipe ID: 24 hex characters.
var recipeIDRe = regexp.MustCompile(`^[a-f0-9]{24}$`)

// OrderHistory implements mealkit.Source. It reads only this account's
// deliveries; no catalog, menu, or browse endpoint is ever requested.
//
// Recipes delivered in more than one week merge into one entry carrying every
// week, which is what the import contract's orderWeeks means.
func (c *Client) OrderHistory(ctx context.Context, t mealkit.Tokens) ([]mealkit.OrderedRecipe, error) {
	byID := map[string]*mealkit.OrderedRecipe{}
	var order []string

	for page := 0; page < maxDeliveryPages; page++ {
		endpoint := fmt.Sprintf("%s%s?limit=%d&offset=%d", c.baseURL, deliveryPath, deliveryLimit, page*deliveryLimit)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+t.AccessToken)
		resp, err := c.fetcher.Do(ctx, req)
		if err != nil {
			return nil, err
		}
		var body deliveryPage
		if err := json.Unmarshal(resp.Body, &body); err != nil {
			return nil, &mealkit.ParseError{
				Subject: "the order history",
				Detail:  "HelloFresh answered with something this build cannot read; the page layout has probably changed",
			}
		}
		if len(body.Items) == 0 {
			break
		}
		for _, item := range body.Items {
			week := isoWeek(item.Week, item.DeliveryDate)
			for _, r := range append(append([]deliveryRecipe{}, item.Meals...), item.Recipes...) {
				id := strings.TrimSpace(firstNonEmpty(r.RecipeID, r.ID))
				if !recipeIDRe.MatchString(id) {
					continue
				}
				page := c.recipeURL(r, id)
				if page == "" {
					continue
				}
				existing, ok := byID[id]
				if !ok {
					byID[id] = &mealkit.OrderedRecipe{
						SourceRecipeID: id,
						Name:           strings.TrimSpace(r.Name),
						URL:            page,
						IsAddon:        r.IsAddon,
					}
					order = append(order, id)
					existing = byID[id]
				}
				if week != "" && !contains(existing.Weeks, week) {
					existing.Weeks = append(existing.Weeks, week)
				}
			}
		}
		if len(body.Items) < deliveryLimit {
			break
		}
	}

	out := make([]mealkit.OrderedRecipe, 0, len(order))
	for _, id := range order {
		r := byID[id]
		sort.Strings(r.Weeks)
		out = append(out, *r)
	}
	return out, nil
}

// recipeURL returns the page to fetch for a delivered recipe, or "" when the
// delivery record points anywhere but a HelloFresh recipe page.
//
// The service's own websiteUrl is only used when it already has the allowed
// prefix; otherwise the URL is built from the ID we validated ourselves. Data
// from the account can therefore never choose the host we talk to.
func (c *Client) recipeURL(r deliveryRecipe, id string) string {
	if u := strings.TrimSpace(r.WebsiteURL); strings.HasPrefix(u, c.prefix) {
		return u
	}
	slug := slugPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(r.Slug)), "-")
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

var isoWeekRe = regexp.MustCompile(`^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$`)

// isoWeek returns the ISO week of a delivery: the service's own value when it
// already is one, else the delivery date's week. An unreadable date yields ""
// and the delivery simply contributes no week, which is better than inventing
// one that would fail import validation.
func isoWeek(week, deliveryDate string) string {
	if w := strings.TrimSpace(week); isoWeekRe.MatchString(w) {
		return w
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, strings.TrimSpace(deliveryDate)); err == nil {
			y, w := t.ISOWeek()
			return fmt.Sprintf("%04d-W%02d", y, w)
		}
	}
	return ""
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
