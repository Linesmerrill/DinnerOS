package main

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Card is what a printed recipe card says about the recipe actually delivered.
type Card struct {
	Name        string
	Headline    string
	PrepMinutes int
	CookMinutes int
	Calories    int
	// Servings are the person counts the card gives amounts for, in column
	// order (e.g. [2, 4]).
	Servings    []int
	Ingredients []CardIngredient
	Utensils    []string
	// Steps are the instructions, one bullet per line prefixed "• ".
	Steps []string
}

// CardIngredient is one ingredient line. Pantry items are the ones the card
// asks you to have at home ("Bust out").
type CardIngredient struct {
	Name      string
	Pantry    bool
	Amounts   []CardAmount // one per Card.Servings, or none
	Allergens []string
}

// CardAmount is a printed amount for a serving count. A nil Quantity means the
// card printed none.
type CardAmount struct {
	Servings int
	Quantity *float64
	Unit     string
}

// ErrCardLayout means the PDF's text doesn't have the recipe card layout the
// parser understands. The card is not used rather than read partially.
var ErrCardLayout = errors.New("unrecognized recipe card layout")

func layoutErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCardLayout, fmt.Sprintf(format, args...))
}

// ParseCard reads a recipe card PDF.
func ParseCard(data []byte) (Card, error) {
	runs, err := ExtractPDFText(data)
	if err != nil {
		return Card{}, err
	}
	return parseCardRuns(runs)
}

func parseCardRuns(runs []TextRun) (Card, error) {
	for i := range runs {
		runs[i].Text = strings.ReplaceAll(runs[i].Text, "\u00a0", " ")
	}
	segs := segments(runs)
	var card Card

	if err := card.parseTitle(segs); err != nil {
		return Card{}, err
	}
	card.parseTimes(segs)
	if err := card.parseIngredients(runs, segs); err != nil {
		return Card{}, err
	}
	card.parseBustOut(segs)
	if err := card.parseSteps(segs); err != nil {
		return Card{}, err
	}
	return card, nil
}

// segment is a run of text on one line with no visual gap: adjacent runs in
// different fonts ("Dice " + "potatoes") joined.
type segment struct {
	Page       int
	X, Y, EndX float64
	Size       float64
	Text       string
}

func (s segment) centerX() float64 { return (s.X + s.EndX) / 2 }

// segments joins runs on the same baseline that touch horizontally.
func segments(runs []TextRun) []segment {
	sorted := append([]TextRun(nil), runs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Page != b.Page {
			return a.Page < b.Page
		}
		if math.Abs(a.Y-b.Y) > 1 {
			return a.Y > b.Y
		}
		return a.X < b.X
	})
	var out []segment
	for _, r := range sorted {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		if n := len(out); n > 0 {
			last := &out[n-1]
			gap := r.X - last.EndX
			if last.Page == r.Page && math.Abs(last.Y-r.Y) <= 1 && gap > -2 && gap < 6 {
				if gap > 1.2 && !strings.HasSuffix(last.Text, " ") && !strings.HasPrefix(r.Text, " ") {
					last.Text += " "
				}
				last.Text += r.Text
				last.EndX = max(last.EndX, r.EndX)
				last.Size = max(last.Size, r.Size)
				continue
			}
		}
		out = append(out, segment{Page: r.Page, X: r.X, Y: r.Y, EndX: r.EndX, Size: r.Size, Text: r.Text})
	}
	for i := range out {
		out[i].Text = collapseSpaces(out[i].Text)
	}
	return out
}

var spacesRe = regexp.MustCompile(`\s+`)

func collapseSpaces(s string) string {
	return strings.TrimSpace(spacesRe.ReplaceAllString(s, " "))
}

// --- title and times ------------------------------------------------------------

func (c *Card) parseTitle(segs []segment) error {
	var title *segment
	for i := range segs {
		s := &segs[i]
		if s.Size >= 14 && (title == nil || s.Size > title.Size+0.5 || (math.Abs(s.Size-title.Size) <= 0.5 && s.Page < title.Page)) {
			title = s
		}
	}
	if title == nil {
		return layoutErr("no title")
	}
	name := title.Text
	bottom := title.Y
	// A long title wraps onto further lines of the same size.
	for _, s := range segs {
		if s.Page == title.Page && math.Abs(s.Size-title.Size) <= 0.5 && s.Y < bottom && bottom-s.Y < title.Size*1.6 && math.Abs(s.X-title.X) < 40 {
			name += " " + s.Text
			bottom = s.Y
		}
	}
	c.Name = collapseSpaces(name)
	for _, s := range segs {
		if s.Page == title.Page && s.Y < bottom && bottom-s.Y < 30 && s.Size >= 9 && s.Size < title.Size-2 && math.Abs(s.X-title.X) < 40 {
			c.Headline = s.Text
			break
		}
	}
	return nil
}

var (
	prepRe     = regexp.MustCompile(`(?i)\bPREP:\s*(\d+)\s*MIN`)
	cookRe     = regexp.MustCompile(`(?i)\b(?:COOK|TOTAL):\s*(\d+)\s*MIN`)
	caloriesRe = regexp.MustCompile(`(?i)\bCALORIES:\s*(\d+)`)
)

func (c *Card) parseTimes(segs []segment) {
	for _, s := range segs {
		if !prepRe.MatchString(s.Text) && !strings.HasPrefix(strings.ToUpper(s.Text), "PREP:") {
			continue
		}
		line := lineText(segs, s)
		if m := prepRe.FindStringSubmatch(line); m != nil {
			c.PrepMinutes, _ = strconv.Atoi(m[1])
		}
		if m := cookRe.FindStringSubmatch(line); m != nil {
			c.CookMinutes, _ = strconv.Atoi(m[1])
		}
		if m := caloriesRe.FindStringSubmatch(line); m != nil {
			c.Calories, _ = strconv.Atoi(m[1])
		}
		return
	}
}

// lineText joins every segment on s's baseline, left to right.
func lineText(segs []segment, s segment) string {
	var parts []segment
	for _, o := range segs {
		if o.Page == s.Page && math.Abs(o.Y-s.Y) <= 2 {
			parts = append(parts, o)
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].X < parts[j].X })
	texts := make([]string, len(parts))
	for i, p := range parts {
		texts[i] = p.Text
	}
	return strings.Join(texts, " ")
}

// --- ingredients ------------------------------------------------------------------

var (
	personRe  = regexp.MustCompile(`(?i)(\d+)\s*PERSON`)
	amountRe  = regexp.MustCompile(`^(\d+(?:\.\d+)?)?\s*(?:([½¼¾⅓⅔⅛⅜⅝⅞])|(\d+)/(\d+))?\s*([A-Za-z][A-Za-z.]*(?: [A-Za-z.]+)?)?$`)
	glyphFrac = map[string]float64{"½": 0.5, "¼": 0.25, "¾": 0.75, "⅓": 1.0 / 3, "⅔": 2.0 / 3, "⅛": 0.125, "⅜": 0.375, "⅝": 0.625, "⅞": 0.875}
)

// parseAmount reads a printed amount such as "12 oz", "1½ TBSP", "¼ Cup", or
// "2". A bare number counts units.
func parseAmount(s string) (float64, string, bool) {
	s = collapseSpaces(s)
	m := amountRe.FindStringSubmatch(s)
	if m == nil || (m[1] == "" && m[2] == "" && m[3] == "") {
		return 0, "", false
	}
	var q float64
	if m[1] != "" {
		q, _ = strconv.ParseFloat(m[1], 64)
	}
	if m[2] != "" {
		q += glyphFrac[m[2]]
	}
	if m[3] != "" {
		n, _ := strconv.ParseFloat(m[3], 64)
		d, _ := strconv.ParseFloat(m[4], 64)
		if d == 0 {
			return 0, "", false
		}
		q += n / d
	}
	unit := strings.ToLower(strings.TrimSuffix(m[5], "."))
	if strings.EqualFold(unit, "person") || strings.EqualFold(unit, "persons") {
		return 0, "", false
	}
	if unit == "" {
		unit = "unit"
	}
	return q, unit, q > 0
}

type amountCell struct {
	amounts []segment
	y       float64
	names   []segment
}

func (c amountCell) centerX() float64 {
	return (c.amounts[0].X + c.amounts[len(c.amounts)-1].EndX) / 2
}

func (c *Card) parseIngredients(runs []TextRun, segs []segment) error {
	var header *segment
	for i := range segs {
		if strings.EqualFold(segs[i].Text, "INGREDIENTS") {
			header = &segs[i]
			break
		}
	}
	if header == nil {
		return layoutErr("no ingredients heading")
	}
	page := header.Page

	// Serving columns: "2 PERSON | 4 PERSON" just under the heading.
	var servingSegs []segment
	for _, r := range runs {
		if r.Page == page && r.Y < header.Y && header.Y-r.Y < 30 {
			for _, m := range personRe.FindAllStringSubmatch(r.Text, -1) {
				n, _ := strconv.Atoi(m[1])
				c.Servings = append(c.Servings, n)
			}
			if personRe.MatchString(r.Text) {
				servingSegs = append(servingSegs, segment{Y: r.Y})
			}
		}
	}
	if len(c.Servings) == 0 {
		return layoutErr("no serving columns")
	}
	headerBottom := header.Y
	for _, s := range servingSegs {
		headerBottom = min(headerBottom, s.Y)
	}

	// The grid ends at the first footnote or the HelloCustom box beneath it.
	var titleX = math.Inf(1)
	for _, s := range segs {
		if s.Page == page && s.Size >= 14 && s.X > header.X {
			titleX = min(titleX, s.X)
		}
	}
	bottom := math.Inf(-1)
	var region []segment
	for _, r := range runs {
		t := strings.TrimSpace(r.Text)
		if r.Page != page || r.Y >= headerBottom-1 || r.X >= titleX-5 || t == "" {
			continue
		}
		if strings.HasPrefix(t, "*") || strings.HasPrefix(strings.ToLower(t), "hellocustom") {
			bottom = max(bottom, r.Y)
			continue
		}
		region = append(region, segment{Page: r.Page, X: r.X, Y: r.Y, EndX: r.EndX, Size: r.Size, Text: t})
	}

	var amounts, texts []segment
	for _, s := range region {
		if s.Y <= bottom {
			continue
		}
		if _, _, ok := parseAmount(s.Text); ok {
			amounts = append(amounts, s)
		} else {
			texts = append(texts, s)
		}
	}
	if len(amounts) == 0 {
		return layoutErr("no ingredient amounts")
	}

	// Rows of amounts; each cell holds one amount per serving column.
	sort.Slice(amounts, func(i, j int) bool { return amounts[i].Y > amounts[j].Y })
	var cells []*amountCell
	per := len(c.Servings)
	for i := 0; i < len(amounts); {
		j := i
		for j < len(amounts) && amounts[i].Y-amounts[j].Y <= 2 {
			j++
		}
		row := append([]segment(nil), amounts[i:j]...)
		sort.Slice(row, func(a, b int) bool { return row[a].X < row[b].X })
		if len(row)%per != 0 {
			return layoutErr("amount row at y=%.0f has %d amounts for %d serving columns", row[0].Y, len(row), per)
		}
		// Amounts within a cell sit closer together than neighbouring cells.
		inner, outer := 0.0, math.Inf(1)
		for k := 1; k < len(row); k++ {
			gap := row[k].X - row[k-1].EndX
			if k%per == 0 {
				outer = min(outer, gap)
			} else {
				inner = max(inner, gap)
			}
		}
		if inner > 20 || outer <= inner+2 {
			return layoutErr("amounts at y=%.0f don't group into cells of %d", row[0].Y, per)
		}
		for k := 0; k < len(row); k += per {
			cells = append(cells, &amountCell{amounts: row[k : k+per], y: row[0].Y})
		}
		i = j
	}

	// Each name or allergen line belongs to the nearest cell in the row above it.
	for _, t := range texts {
		var best *amountCell
		for _, cell := range cells {
			if cell.y <= t.Y || cell.y-t.Y > 40 {
				continue
			}
			if best != nil && cell.y > best.y+2 {
				continue
			}
			if math.Abs(cell.centerX()-t.centerX()) > 45 {
				continue
			}
			if best == nil || cell.y < best.y-2 || math.Abs(cell.centerX()-t.centerX()) < math.Abs(best.centerX()-t.centerX()) {
				best = cell
			}
		}
		if best != nil {
			best.names = append(best.names, t)
		}
	}

	for _, cell := range cells {
		sort.SliceStable(cell.names, func(i, j int) bool { return cell.names[i].Y > cell.names[j].Y })
		var name []string
		var allergens []string
		inAllergens := false
		for _, n := range cell.names {
			// "Contains:" lists can wrap; every line after it is part of the list.
			if rest, ok := strings.CutPrefix(n.Text, "Contains:"); ok || inAllergens {
				if !ok {
					rest = n.Text
				}
				inAllergens = true
				allergens = append(allergens, splitList(rest)...)
				continue
			}
			name = append(name, n.Text)
		}
		ing := CardIngredient{Name: cleanCardName(strings.Join(name, " ")), Allergens: allergens}
		if ing.Name == "" {
			return layoutErr("amounts at y=%.0f have no ingredient name", cell.y)
		}
		for k, a := range cell.amounts {
			q, unit, _ := parseAmount(a.Text)
			ing.Amounts = append(ing.Amounts, CardAmount{Servings: c.Servings[k], Quantity: &q, Unit: unit})
		}
		c.Ingredients = append(c.Ingredients, ing)
	}
	if len(c.Ingredients) < 2 {
		return layoutErr("only %d ingredients", len(c.Ingredients))
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = collapseSpaces(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func cleanCardName(s string) string {
	return collapseSpaces(strings.TrimRight(collapseSpaces(s), "*"))
}

// --- bust out (pantry and utensils) ---------------------------------------------

// pantryWords end the names of pantry items a card lists without amounts
// ("Kosher salt", "Cooking oil"). Utensils never end in them.
var pantryWords = map[string]bool{
	"salt": true, "pepper": true, "oil": true, "sugar": true, "butter": true, "flour": true,
	"honey": true, "vinegar": true, "egg": true, "eggs": true, "milk": true,
}

var pantryAmountsRe = regexp.MustCompile(`^(.*?)\s*\(([^()]*\|[^()]*)\)\s*$`)

// parseBustOut reads the "Bust out" list: utensils, plus pantry items, which
// are the lines with printed amounts and the salt and pepper lines.
func (c *Card) parseBustOut(segs []segment) {
	var heading *segment
	for i := range segs {
		if strings.EqualFold(segs[i].Text, "BUST OUT") {
			heading = &segs[i]
			break
		}
	}
	if heading == nil {
		return
	}
	isBullet := func(s segment) bool { return strings.HasPrefix(s.Text, "•") }
	// Column heads: bullets just under the heading.
	var columns []segment
	for _, s := range segs {
		if s.Page == heading.Page && isBullet(s) && s.Y < heading.Y && heading.Y-s.Y < 25 && s.X > heading.X-80 && s.X < heading.EndX+80 {
			columns = append(columns, s)
		}
	}
	sort.Slice(columns, func(i, j int) bool { return columns[i].X < columns[j].X })
	for ci, head := range columns {
		right := head.X + 150
		if ci+1 < len(columns) {
			right = columns[ci+1].X - 2
		}
		var items []segment
		lastY := head.Y + 1
		for _, s := range segs { // segments are sorted top to bottom
			if s.Page != head.Page || s.Y >= lastY || s.X < head.X-3 || s.X >= right {
				continue
			}
			if lastY-s.Y > 24 {
				break
			}
			if isBullet(s) && math.Abs(s.X-head.X) <= 2 {
				items = append(items, s)
				lastY = s.Y
				continue
			}
			if len(items) > 0 && strings.HasPrefix(s.Text, "Contains:") {
				items[len(items)-1].Text += "\n" + s.Text
				lastY = s.Y
			}
		}
		for _, it := range items {
			text, contains, _ := strings.Cut(it.Text, "\n")
			text = collapseSpaces(strings.TrimPrefix(text, "•"))
			var allergens []string
			if rest, ok := strings.CutPrefix(contains, "Contains:"); ok {
				allergens = splitList(rest)
			}
			if m := pantryAmountsRe.FindStringSubmatch(text); m != nil {
				parts := strings.Split(m[2], "|")
				ing := CardIngredient{Name: cleanCardName(m[1]), Pantry: true, Allergens: allergens}
				if len(parts) == len(c.Servings) {
					for k, p := range parts {
						a := CardAmount{Servings: c.Servings[k]}
						if q, unit, ok := parseAmount(p); ok {
							a.Quantity, a.Unit = &q, unit
						}
						ing.Amounts = append(ing.Amounts, a)
					}
				}
				c.Ingredients = append(c.Ingredients, ing)
				continue
			}
			if words := strings.Fields(strings.ToLower(text)); len(words) > 0 && pantryWords[words[len(words)-1]] {
				c.Ingredients = append(c.Ingredients, CardIngredient{Name: cleanCardName(text), Pantry: true, Allergens: allergens})
				continue
			}
			if text != "" {
				c.Utensils = append(c.Utensils, text)
			}
		}
	}
}

// --- steps ------------------------------------------------------------------------

var stepHeadingRe = regexp.MustCompile(`^(\d{1,2})\s+([A-Z0-9][A-Z0-9 &',’\-/]*)$`)

type stepHeading struct {
	n   int
	seg segment
}

func (c *Card) parseSteps(segs []segment) error {
	var heads []stepHeading
	for _, s := range segs {
		if s.Size < 9.5 {
			continue
		}
		if m := stepHeadingRe.FindStringSubmatch(s.Text); m != nil && strings.ToUpper(s.Text) == s.Text {
			n, _ := strconv.Atoi(m[1])
			heads = append(heads, stepHeading{n: n, seg: s})
		}
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].n < heads[j].n })
	if len(heads) < 3 {
		return layoutErr("only %d step headings", len(heads))
	}
	for i, h := range heads {
		if h.n != i+1 {
			return layoutErr("step headings are not numbered 1..%d", len(heads))
		}
	}

	for _, h := range heads {
		hs := h.seg
		right := hs.X + 190
		for _, o := range heads {
			if o.seg.Page == hs.Page && math.Abs(o.seg.Y-hs.Y) < 5 && o.seg.X > hs.X+30 {
				right = min(right, o.seg.X-12)
			}
		}
		floor := math.Inf(-1)
		for _, o := range heads {
			if o.seg.Page == hs.Page && o.seg.Y < hs.Y-5 && math.Abs(o.seg.X-hs.X) < 30 {
				floor = max(floor, o.seg.Y)
			}
		}

		var body []segment
		for _, s := range segs {
			if s.Page == hs.Page && s.Y < hs.Y-1 && s.Y > floor && s.X >= hs.X-15 && s.X < right {
				body = append(body, s)
			}
		}
		text, err := stepText(body)
		if err != nil {
			return layoutErr("step %d: %v", h.n, err)
		}
		c.Steps = append(c.Steps, text)
	}
	return nil
}

// stepText reads a step column top to bottom: bullet lines and their wrapped
// continuations, stopping at the first line that isn't aligned with them (a
// call-out box or footnote) or after a large vertical gap.
func stepText(body []segment) (string, error) {
	if len(body) == 0 {
		return "", errors.New("no text")
	}
	sort.SliceStable(body, func(i, j int) bool {
		if math.Abs(body[i].Y-body[j].Y) > 1 {
			return body[i].Y > body[j].Y
		}
		return body[i].X < body[j].X
	})
	var lines [][]segment
	for _, s := range body {
		if n := len(lines); n > 0 && math.Abs(lines[n-1][0].Y-s.Y) <= 1 {
			lines[n-1] = append(lines[n-1], s)
			continue
		}
		lines = append(lines, []segment{s})
	}

	first := lines[0][0]
	bulleted := strings.HasPrefix(first.Text, "•")
	textX := first.X
	if bulleted {
		textX = math.NaN() // learned from the first continuation line
	}
	var out []string
	prevY := first.Y + 1
	for _, line := range lines {
		start := line[0]
		if prevY-start.Y > 25 {
			break
		}
		parts := make([]string, len(line))
		for i, s := range line {
			parts[i] = s.Text
		}
		text := collapseSpaces(strings.Join(parts, " "))
		isBullet := strings.HasPrefix(text, "•")
		switch {
		case bulleted && isBullet && math.Abs(start.X-first.X) <= 2.5:
			out = append(out, "• "+collapseSpaces(strings.TrimPrefix(text, "•")))
		case len(out) > 0 && !isBullet && (math.IsNaN(textX) || math.Abs(start.X-textX) <= 2.5):
			if math.IsNaN(textX) {
				if start.X <= first.X+2.5 || start.X-first.X > 15 {
					return strings.Join(out, "\n"), nil
				}
				textX = start.X
			}
			out[len(out)-1] += "\n" + text
		case !bulleted && len(out) == 0:
			out = append(out, text)
		default:
			return finishStep(out)
		}
		prevY = start.Y
	}
	return finishStep(out)
}

func finishStep(lines []string) (string, error) {
	if len(lines) == 0 {
		return "", errors.New("no text")
	}
	return strings.Join(lines, "\n"), nil
}

// --- conversion -------------------------------------------------------------------

// cardIngredientIDPrefix marks ingredient IDs the card-to-recipe conversion
// made up to link amounts to lines. They are never emitted as source IDs.
const cardIngredientIDPrefix = "card-line-"

// hfRecipe shapes a card like a HelloFresh recipe object so the normalizer
// reads every origin the same way.
func (c Card) hfRecipe(deliveredID string) hfRecipe {
	r := hfRecipe{ID: deliveredID, Name: titleCase(c.Name), Headline: c.Headline}
	if c.PrepMinutes > 0 {
		r.PrepTime = fmt.Sprintf("PT%dM", c.PrepMinutes)
	}
	if c.CookMinutes > 0 {
		r.TotalTime = fmt.Sprintf("PT%dM", c.CookMinutes)
	}
	if c.Calories > 0 {
		r.Nutrition = append(r.Nutrition, hfNutrient{Name: "Calories", Amount: float64(c.Calories), Unit: "kcal"})
	}
	seenAllergen := map[string]bool{}
	for _, u := range c.Utensils {
		r.Utensils = append(r.Utensils, named{Name: u})
	}
	for _, s := range c.Servings {
		r.Yields = append(r.Yields, hfYield{Yields: s})
	}
	for i, ing := range c.Ingredients {
		id := fmt.Sprintf("%s%d", cardIngredientIDPrefix, i+1)
		r.Ingredients = append(r.Ingredients, hfIngredient{ID: id, Name: ing.Name, Shipped: !ing.Pantry})
		for _, a := range ing.Allergens {
			if !seenAllergen[strings.ToLower(a)] {
				seenAllergen[strings.ToLower(a)] = true
				r.Allergens = append(r.Allergens, named{Name: a})
			}
		}
		for yi := range r.Yields {
			ya := hfYieldIngredient{ID: id}
			for _, a := range ing.Amounts {
				if a.Servings == r.Yields[yi].Yields {
					ya.Amount, ya.Unit = a.Quantity, a.Unit
				}
			}
			r.Yields[yi].Ingredients = append(r.Yields[yi].Ingredients, ya)
		}
	}
	for i, s := range c.Steps {
		r.Steps = append(r.Steps, hfStep{Index: i + 1, Instructions: s})
	}
	return r
}

var titleSmallWords = map[string]bool{
	"a": true, "an": true, "and": true, "as": true, "at": true, "by": true, "for": true, "in": true,
	"of": true, "on": true, "or": true, "over": true, "the": true, "to": true, "with": true, "n'": true,
}

// titleCase turns an all-caps card title into title case. Titles that already
// have lower case letters are kept.
func titleCase(s string) string {
	if strings.ToUpper(s) != s {
		return s
	}
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if i > 0 && titleSmallWords[w] {
			continue
		}
		parts := strings.Split(w, "-")
		for j, p := range parts {
			rs := []rune(p)
			if len(rs) > 0 {
				rs[0] = []rune(strings.ToUpper(string(rs[0])))[0]
			}
			parts[j] = string(rs)
		}
		words[i] = strings.Join(parts, "-")
	}
	return strings.Join(words, " ")
}
