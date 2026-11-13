package recipes

import (
	"strings"

	"golang.org/x/net/html"
)

// Reading a recipe from a web page, in order of how much the page itself
// tells us:
//
//  1. schema.org Recipe in JSON-LD, which most recipe sites and every recipe
//     card plugin publish for search engines.
//  2. schema.org Recipe in microdata (itemprop attributes), on older sites.
//  3. A recipe card's own markup (WP Recipe Maker, Tasty Recipes, Mediavine
//     Create), then an "Ingredients" heading followed by a list.
//
// Only the title, the servings, and the ingredient lines are kept (decision
// 670): a page's photos and written steps are the publisher's, and the saved
// recipe links back to the page for those.

// Page parse methods, reported so the review screen and tests know which
// reader found the recipe.
const (
	PageJSONLD    = "json-ld"
	PageMicrodata = "microdata"
	PageCard      = "recipe-card"
	PageText      = "page-text"
)

// ParsePage reads a recipe from a page's HTML. ok is false when no reader
// found a title and at least one ingredient.
func ParsePage(page string) (d Draft, method string, ok bool) {
	if len(page) > MaxFetchBytes {
		page = page[:MaxFetchBytes]
	}
	if d, ok := recipeFromJSONLD(page); ok && len(d.Ingredients) > 0 {
		return linkDraft(d), PageJSONLD, true
	}
	root, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return Draft{}, "", false
	}
	if d, ok := recipeFromMicrodata(root); ok {
		return linkDraft(d), PageMicrodata, true
	}
	if d, ok := recipeFromCard(root); ok {
		return linkDraft(d), PageCard, true
	}
	if d, ok := recipeFromHeading(root); ok {
		return linkDraft(d), PageText, true
	}
	return Draft{}, "", false
}

// ImportedRecipeNote is shown on the review screen and on recipes read from
// a page.
const ImportedRecipeNote = "Imported recipes can have mistakes. Please check the ingredients before you shop."

// linkDraft keeps only what a recipe read from a page may save: its title,
// servings, ingredient lines, and the link back. Everything else, the
// publisher's photo, steps, and description among it, is dropped here, so it
// never reaches the phone or the database.
func linkDraft(d Draft) Draft {
	out := Draft{Name: d.Name, Servings: d.Servings, SourceURL: d.SourceURL, Ingredients: d.Ingredients}
	if len(out.Ingredients) == 0 {
		out.Warnings = append(out.Warnings, "No ingredients were found. Add them before saving.")
	}
	for _, line := range out.Ingredients {
		if line.Quantity == "" && line.Unit == "" && !strings.Contains(strings.ToLower(line.Note), "taste") {
			out.Warnings = append(out.Warnings, "Some lines have no amount. Add one so the list buys enough.")
			break
		}
	}
	out.Warnings = append(out.Warnings, ImportedRecipeNote)
	return out
}

// --- Microdata ------------------------------------------------------------------

func recipeFromMicrodata(root *html.Node) (Draft, bool) {
	scope := findNode(root, func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasAttr(n, "itemscope") &&
			strings.Contains(strings.ToLower(attr(n, "itemtype")), "schema.org/recipe")
	})
	if scope == nil {
		return Draft{}, false
	}
	var d Draft
	walk(scope, func(n *html.Node) bool {
		// Another item nested inside (the author, a rating, nutrition): its
		// properties are its own, so its "name" is not the recipe's.
		if n != scope && hasAttr(n, "itemscope") {
			return false
		}
		for _, prop := range strings.Fields(attr(n, "itemprop")) {
			switch prop {
			case "name":
				if d.Name == "" {
					d.Name = clip(propValue(n), maxDraftName)
				}
			case "recipeYield", "yield":
				if d.Servings == 0 {
					d.Servings = atoiClamped(firstNumberIn(propValue(n)), maxDraftServings)
				}
			case "recipeIngredient", "ingredients":
				addLine(&d, propValue(n), "")
			}
		}
		return true
	})
	return d, d.Name != "" && len(d.Ingredients) > 0
}

func propValue(n *html.Node) string {
	if v, ok := attrOK(n, "content"); ok {
		return stripTags(v)
	}
	return nodeText(n)
}

// --- Recipe cards ---------------------------------------------------------------

// cardKind is one recipe card plugin's markup.
type cardKind struct {
	container, title, servings, ingredient, group string
}

var cardKinds = []cardKind{
	// WP Recipe Maker.
	{container: "wprm-recipe-container", title: "wprm-recipe-name", servings: "wprm-recipe-servings",
		ingredient: "wprm-recipe-ingredient", group: "wprm-recipe-group-name"},
	// Tasty Recipes.
	{container: "tasty-recipes", title: "tasty-recipes-title", servings: "tasty-recipes-yield",
		ingredient: "tasty-recipes-ingredients", group: ""},
	// Mediavine Create.
	{container: "mv-create-card", title: "mv-create-title", servings: "mv-create-yield",
		ingredient: "mv-create-ingredients", group: ""},
}

func recipeFromCard(root *html.Node) (Draft, bool) {
	for _, kind := range cardKinds {
		card := findNode(root, func(n *html.Node) bool { return hasClass(n, kind.container) })
		if card == nil {
			continue
		}
		var d Draft
		if t := findNode(card, func(n *html.Node) bool { return hasClass(n, kind.title) }); t != nil {
			d.Name = clip(nodeText(t), maxDraftName)
		}
		if s := findNode(card, func(n *html.Node) bool { return hasClass(n, kind.servings) }); s != nil {
			d.Servings = atoiClamped(firstNumberIn(nodeText(s)), maxDraftServings)
		}
		group := ""
		walk(card, func(n *html.Node) bool {
			switch {
			case kind.group != "" && hasClass(n, kind.group):
				group = groupName(nodeText(n))
				return false
			case kind.group == "" && isHeading(n) && insideClass(n, kind.ingredient):
				// The section's own "Ingredients" title isn't a group.
				if !strings.EqualFold(strings.TrimSuffix(nodeText(n), ":"), "ingredients") {
					group = groupName(nodeText(n))
				}
				return false
			case n.Type == html.ElementNode && n.Data == "li" &&
				(hasClass(n, kind.ingredient) || insideClass(n, kind.ingredient)):
				addLine(&d, cardLineText(n), group)
				return false
			}
			return true
		})
		if d.Name == "" {
			d.Name = pageTitle(root)
		}
		if d.Name != "" && len(d.Ingredients) > 0 {
			return d, true
		}
	}
	return Draft{}, false
}

// cardLineText is a card line's text. WP Recipe Maker puts a line's notes in
// their own span right after the name ("cherry tomatoes" "halved"), so they
// are set off with a comma, the way a person writes them, to read as a note.
func cardLineText(li *html.Node) string {
	var notes *html.Node
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		if hasClass(c, "wprm-recipe-ingredient-notes") {
			notes = c
		}
	}
	if notes == nil {
		return nodeText(li)
	}
	var parts []string
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		if c != notes {
			if t := nodeText(c); t != "" {
				parts = append(parts, t)
			}
		}
	}
	text, note := strings.Join(parts, " "), nodeText(notes)
	if note == "" || strings.HasPrefix(note, "(") || strings.HasPrefix(note, ",") {
		return strings.TrimSpace(text + " " + note)
	}
	return text + ", " + note
}

// --- Page text ------------------------------------------------------------------

// recipeFromHeading finds an "Ingredients" heading and reads the lists after
// it, until the next heading that isn't a group ("For the sauce").
func recipeFromHeading(root *html.Node) (Draft, bool) {
	heading := findNode(root, func(n *html.Node) bool {
		if !isHeading(n) && !(n.Type == html.ElementNode && (n.Data == "strong" || n.Data == "b" || n.Data == "p")) {
			return false
		}
		t := strings.ToLower(strings.TrimSuffix(nodeText(n), ":"))
		return t == "ingredients" || t == "ingredient list" || t == "what you'll need" || t == "you will need"
	})
	if heading == nil {
		return Draft{}, false
	}
	d := Draft{Name: pageTitle(root)}
	group := ""
	// Walk forward through the document from the heading.
	for n := nextInDocument(heading, true); n != nil; n = nextInDocument(n, false) {
		if isHeading(n) || (n.Type == html.ElementNode && (n.Data == "strong" || n.Data == "b") && n.Parent != nil && n.Parent.Data == "p") {
			text := nodeText(n)
			lower := strings.ToLower(text)
			if strings.HasPrefix(lower, "for ") || strings.HasSuffix(text, ":") {
				group = groupName(text)
				continue
			}
			if len(d.Ingredients) > 0 {
				break
			}
			continue
		}
		if n.Type == html.ElementNode && n.Data == "li" {
			addLine(&d, nodeText(n), group)
			if len(d.Ingredients) >= maxDraftIngredients {
				break
			}
		}
	}
	return d, d.Name != "" && len(d.Ingredients) > 0
}

// nextInDocument is the next node in document order, skipping n's children
// when skipChildren is set and the subtree of any li (its text is one line).
func nextInDocument(n *html.Node, skipChildren bool) *html.Node {
	if !skipChildren && n.FirstChild != nil && !(n.Type == html.ElementNode && n.Data == "li") {
		return n.FirstChild
	}
	for ; n != nil; n = n.Parent {
		if n.NextSibling != nil {
			return n.NextSibling
		}
	}
	return nil
}

// pageTitle is the page's own name for the recipe: its og:title, without the
// site's name tacked on ("Sesame Noodles - Tiny Kitchen"), else its first h1,
// else its title.
func pageTitle(root *html.Node) string {
	meta := func(property string) string {
		if n := findNode(root, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "meta" && attr(n, "property") == property
		}); n != nil {
			return stripTags(attr(n, "content"))
		}
		return ""
	}
	if t := meta("og:title"); t != "" {
		if site := meta("og:site_name"); site != "" {
			for _, sep := range []string{" - ", " | ", " – ", " — "} {
				if trimmed, ok := strings.CutSuffix(t, sep+site); ok && strings.TrimSpace(trimmed) != "" {
					t = trimmed
					break
				}
			}
		}
		if t = clip(t, maxDraftName); t != "" {
			return t
		}
	}
	for _, tag := range []string{"h1", "title"} {
		if n := findNode(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == tag }); n != nil {
			if t := clip(nodeText(n), maxDraftName); t != "" {
				return t
			}
		}
	}
	return ""
}

// --- Shared ---------------------------------------------------------------------

// addLine adds one ingredient line, or starts a group when the line is a
// heading ("For the sauce:").
func addLine(d *Draft, text, group string) {
	text = strings.TrimSpace(whitespaceRe.ReplaceAllString(text, " "))
	if text == "" || len(d.Ingredients) >= maxDraftIngredients {
		return
	}
	if ingredientHeading(text) {
		return
	}
	line := parseIngredientLine(text)
	line.Group = group
	d.Ingredients = append(d.Ingredients, line)
}

func isHeading(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	}
	return false
}

func findNode(n *html.Node, match func(*html.Node) bool) *html.Node {
	var found *html.Node
	walk(n, func(c *html.Node) bool {
		if found != nil {
			return false
		}
		if match(c) {
			found = c
			return false
		}
		return true
	})
	return found
}

// walk visits n and its descendants in order; visit returns false to skip a
// node's children.
func walk(n *html.Node, visit func(*html.Node) bool) {
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript" || n.Data == "svg") {
		return
	}
	if !visit(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

// nodeText is a node's visible text, spaced and trimmed. Screen-reader-only
// and hidden parts (a card's "Check" boxes, a unit-switcher's other units)
// are left out.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var add func(*html.Node)
	add = func(c *html.Node) {
		switch c.Type {
		case html.TextNode:
			b.WriteString(c.Data)
			b.WriteByte(' ')
		case html.ElementNode:
			switch c.Data {
			case "script", "style", "noscript", "svg", "button", "input":
				return
			}
			if hasAttr(c, "hidden") || attr(c, "aria-hidden") == "true" || hasClass(c, "screen-reader-text") ||
				hasClass(c, "sr-only") || hasClass(c, "wprm-checkbox-container") ||
				strings.Contains(strings.ReplaceAll(attr(c, "style"), " ", ""), "display:none") {
				return
			}
			for k := c.FirstChild; k != nil; k = k.NextSibling {
				add(k)
			}
		}
	}
	add(n)
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(strings.ReplaceAll(b.String(), "\n", " "), " "))
}

func attr(n *html.Node, key string) string {
	v, _ := attrOK(n, key)
	return v
}

func attrOK(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func hasAttr(n *html.Node, key string) bool {
	_, ok := attrOK(n, key)
	return ok
}

func hasClass(n *html.Node, class string) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func insideClass(n *html.Node, class string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if hasClass(p, class) {
			return true
		}
	}
	return false
}
