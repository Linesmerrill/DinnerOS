package recipes

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// --- Marked lists ---------------------------------------------------------------
//
// Some recipe pages publish no structured data at all and build the recipe
// with their scripts: Kroger and its other stores among them (decision 727).
// Once a browser has run the page, the recipe is there in the page's own
// markup: a schema.org Recipe scope with no properties inside it, list items
// marked as ingredients and steps by their test ids or classes, labelled
// times ("Prep:" "1 hour 20 minutes"), and a nutrition table. This reads that
// shape wherever it appears, not one store's page.

// recipeFromMarkedLists reads a recipe from list items the page marks as
// ingredients. Inside a schema.org Recipe scope a test id, class, or itemprop
// naming ingredients is enough; elsewhere only a test id naming both the
// recipe and its ingredients ("RecipeDetail-ingredients") counts, so a
// product's ingredient list on a store page isn't read as a recipe.
func recipeFromMarkedLists(root *html.Node) (Draft, bool) {
	scope := findNode(root, isRecipeScope)
	container := scope
	if container == nil {
		container = root
	}
	var d Draft
	walk(container, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "li" {
			return true
		}
		switch listItemKind(n, scope != nil) {
		case "ingredient":
			addLine(&d, nodeText(n), "")
			return false
		case "step":
			if text := stepText(n); text != "" && len(d.Steps) < maxDraftSteps {
				d.Steps = append(d.Steps, clip(text, MaxDraftTextBytes))
			}
			return false
		}
		return true
	})
	if len(d.Ingredients) == 0 {
		return Draft{}, false
	}
	d.Name = markedTitle(root, scope)
	readLabelledDetails(container, &d)
	d.Nutrition = nutritionTable(container)
	d.ImageURL = scopeImage(root, scope)
	return d, d.Name != ""
}

func isRecipeScope(n *html.Node) bool {
	return n.Type == html.ElementNode && hasAttr(n, "itemscope") &&
		strings.Contains(strings.ToLower(attr(n, "itemtype")), "schema.org/recipe")
}

// listItemKind is what a list item is marked as: "ingredient", "step", or "".
// A group's heading or title ("ingredient-group-title") is neither.
func listItemKind(li *html.Node, inScope bool) string {
	testID := strings.ToLower(attr(li, "data-testid"))
	marks := testID
	if inScope {
		marks += " " + strings.ToLower(attr(li, "class")+" "+attr(li, "itemprop"))
	} else if !strings.Contains(testID, "recipe") {
		return ""
	}
	for _, word := range []string{"group", "heading", "header", "title"} {
		if strings.Contains(marks, word) {
			return ""
		}
	}
	switch {
	case strings.Contains(marks, "ingredient"):
		return "ingredient"
	case strings.Contains(marks, "direction"), strings.Contains(marks, "instruction"),
		strings.Contains(marks, "recipestep"), strings.Contains(marks, "recipe-step"):
		return "step"
	}
	return ""
}

var stepLabelRe = regexp.MustCompile(`(?i)^step\s*\d+\s*[:.)-]?\s*`)

// stepText is a step's text without its "Step 1" label, which pages put in
// the checkbox's label beside the text.
func stepText(li *html.Node) string {
	var parts []string
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "label" {
			continue
		}
		if t := nodeText(c); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.TrimSpace(stepLabelRe.ReplaceAllString(strings.Join(parts, " "), ""))
}

// markedTitle is the recipe's name: the scope's own name property, else its
// first heading, else the page's title, without a trailing " Recipe".
func markedTitle(root, scope *html.Node) string {
	name := ""
	if scope != nil {
		if n := findNode(scope, func(n *html.Node) bool {
			return n != scope && strings.Contains(" "+attr(n, "itemprop")+" ", " name ") && !insideItemScope(n, scope)
		}); n != nil {
			name = clip(propValue(n), maxDraftName)
		}
		for _, tag := range []string{"h1", "h2"} {
			if name != "" {
				break
			}
			if n := findNode(scope, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == tag }); n != nil {
				name = clip(nodeText(n), maxDraftName)
			}
		}
	}
	if name == "" {
		name = pageTitle(root)
	}
	if trimmed := strings.TrimSpace(name); len(trimmed) > len(" recipe") &&
		strings.EqualFold(trimmed[len(trimmed)-len(" recipe"):], " recipe") {
		name = strings.TrimSpace(trimmed[:len(trimmed)-len(" recipe")])
	}
	return name
}

// insideItemScope reports whether n belongs to an item nested in scope (an
// author, a rating), whose name is not the recipe's.
func insideItemScope(n, scope *html.Node) bool {
	for p := n.Parent; p != nil && p != scope; p = p.Parent {
		if hasAttr(p, "itemscope") {
			return true
		}
	}
	return hasAttr(n, "itemscope")
}

// readLabelledDetails reads the times and servings a page shows as a label
// and a value side by side: "Prep: " then "1 hour 20 minutes".
func readLabelledDetails(container *html.Node, d *Draft) {
	var prep, cook, total int
	walk(container, func(n *html.Node) bool {
		// A label is an element holding only text, so reading each one's
		// text stays cheap on a large page.
		if n.Type != html.ElementNode || !onlyText(n) {
			return true
		}
		label := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(nodeText(n)), ":")))
		if label == "" || len(label) > 20 {
			return true
		}
		value := nextElementSibling(n)
		if value == nil {
			return true
		}
		text := nodeText(value)
		switch label {
		case "prep", "prep time":
			prep = spokenMinutes(text)
		case "cook", "cook time":
			cook = spokenMinutes(text)
		case "total", "total time":
			total = spokenMinutes(text)
		case "servings", "serves", "yield", "makes":
			if d.Servings == 0 {
				d.Servings = atoiClamped(firstNumberIn(text), maxDraftServings)
			}
		default:
			return true
		}
		return false
	})
	d.PrepMinutes = prep
	if total == 0 {
		total = prep + cook
	}
	d.TotalMinutes = min(total, 24*60)
}

func onlyText(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return false
		}
	}
	return n.FirstChild != nil
}

func nextElementSibling(n *html.Node) *html.Node {
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		if s.Type == html.ElementNode {
			return s
		}
		if s.Type == html.TextNode && strings.TrimSpace(s.Data) != "" {
			return nil
		}
	}
	return nil
}

var (
	spokenHoursRe   = regexp.MustCompile(`(?i)(\d+)\s*(?:hours?|hrs?|h)\b`)
	spokenMinutesRe = regexp.MustCompile(`(?i)(\d+)\s*(?:minutes?|mins?|m)\b`)
)

// spokenMinutes reads a time as a page writes it ("1 hour 20 minutes",
// "45 mins", "2 hrs") as whole minutes.
func spokenMinutes(s string) int {
	minutes := 0
	if m := spokenHoursRe.FindStringSubmatch(s); m != nil {
		minutes += atoiClamped(m[1], 48) * 60
	}
	if m := spokenMinutesRe.FindStringSubmatch(s); m != nil {
		minutes += atoiClamped(m[1], 60*48)
	}
	return min(minutes, 24*60)
}

var amountRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(mg|g|kcal|cal)?`)

// nutritionTable reads a table captioned as nutrition, one row a nutrient
// ("Sodium" "1280mg"), as the typed nutrients (decision 694) it names.
func nutritionTable(container *html.Node) []Nutrient {
	table := findNode(container, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "table" {
			return false
		}
		caption := findNode(n, func(c *html.Node) bool { return c.Type == html.ElementNode && c.Data == "caption" })
		return caption != nil && strings.Contains(strings.ToLower(nodeText(caption)), "nutrition")
	})
	if table == nil {
		return nil
	}
	var out []Nutrient
	seen := map[string]bool{}
	walk(table, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "tr" {
			return true
		}
		var cells []*html.Node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.Data == "th" || c.Data == "td") {
				cells = append(cells, c)
			}
		}
		if len(cells) < 2 {
			return false
		}
		t, ok := typedNutrientNamed(nodeText(cells[0]))
		m := amountRe.FindStringSubmatch(nodeText(cells[1]))
		if !ok || m == nil || seen[t.name] {
			return false
		}
		amount, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return false
		}
		switch {
		case m[2] == "g" && t.unit == "mg":
			amount *= 1000
		case m[2] == "mg" && t.unit == "g":
			amount /= 1000
		}
		if amount <= 0 || amount > t.max {
			return false
		}
		seen[t.name] = true
		out = append(out, Nutrient{Name: t.name, Amount: amount, Unit: t.unit})
		return false
	})
	return out
}

// scopeImage is the recipe's first picture, made absolute against the page's
// own address (the scope's url property, its canonical link, or og:url).
func scopeImage(root, scope *html.Node) string {
	if scope == nil {
		return ""
	}
	img := findNode(scope, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && strings.TrimSpace(attr(n, "src")) != ""
	})
	if img == nil {
		return ""
	}
	ref, err := url.Parse(strings.TrimSpace(attr(img, "src")))
	if err != nil {
		return ""
	}
	if ref.IsAbs() {
		return safeHTTPURL(ref.String())
	}
	base := ""
	if n := findNode(scope, func(n *html.Node) bool { return attr(n, "itemprop") == "url" && hasAttr(n, "content") }); n != nil {
		base = attr(n, "content")
	}
	if base == "" {
		if n := findNode(root, func(n *html.Node) bool {
			return (n.Data == "link" && attr(n, "rel") == "canonical") || (n.Data == "meta" && attr(n, "property") == "og:url")
		}); n != nil {
			base = attr(n, "href") + attr(n, "content")
		}
	}
	baseURL, err := url.Parse(base)
	if err != nil || !baseURL.IsAbs() {
		return ""
	}
	return safeHTTPURL(baseURL.ResolveReference(ref).String())
}
