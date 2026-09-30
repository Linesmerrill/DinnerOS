package recipes

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file builds the cooking screen's ingredient checklist from the rendered
// steps: every ingredient with its amount, its shares when steps split it,
// and the same list grouped by the step that uses it, with how each step
// cuts it ("halved, cored, and thinly sliced into strips"). The app shows it
// as sent, so the wording improves with an API deploy rather than an app
// release (decision 591).

// Checklist is the cooking screen's ingredient checklist.
type Checklist struct {
	// All is every ingredient in the recipe's order.
	All []CookIngredient
	// ByStep groups the ingredients by the step that uses them. Index 0,
	// "Have Ready", holds what no step names, blends to mix first, and what
	// to get ready ahead for a later step.
	ByStep []CookStepGroup
}

// CookIngredient is one ingredient on the all-together checklist.
type CookIngredient struct {
	// ID is "<index>-<ingredientId>", the row's ID on every device.
	ID            string
	Index         int
	Name          string
	AmountText    string
	LeftOut       bool
	IngredientKey string
	// Parts are its per-step shares, when two or more steps each say how
	// much they use.
	Parts []CookPart
	// Components are what a home-made blend is mixed from ("1 tsp Chili
	// Powder").
	Components []string
}

// CookPart is one step's share of an ingredient: "2 Tbsp" in step 1.
type CookPart struct {
	ID         string
	AmountText string
	StepIndex  int
}

// CookStepGroup is everything one step needs.
type CookStepGroup struct {
	Index int
	Items []CookStepItem
}

// CookStepItem is one ingredient as a step uses it: "2 | Scallions, sliced".
type CookStepItem struct {
	ID              string
	IngredientIndex int
	Name            string
	AmountText      string
	// Prep is how the step readies it ("peeled, cored, and diced"), or empty.
	Prep string
	// Parts are sub-rows: "Scallion whites" and "Scallion greens", or a
	// blend's spices.
	Parts         []string
	LeftOut       bool
	IngredientKey string
}

func buildChecklist(r Recipe, in Instructions) Checklist {
	all := checklistIngredients(r, in)
	return Checklist{All: all, ByStep: checklistByStep(in.Steps, all)}
}

func checklistIngredients(r Recipe, in Instructions) []CookIngredient {
	out := make([]CookIngredient, 0, len(in.Ingredients))
	for _, st := range in.Ingredients {
		ci := CookIngredient{
			ID: fmt.Sprintf("%d-%s", st.Index, r.Ingredients[st.Index].IngredientID), Index: st.Index,
			Name: st.Name, LeftOut: st.LeftOut != nil, IngredientKey: st.IngredientKey,
		}
		if st.SwapName != "" {
			ci.Name = st.SwapName
		}
		if st.Amount != nil {
			ci.AmountText = st.Amount.Text()
		}
		if st.Component != nil && !ci.LeftOut {
			ci.Components = st.Component.Parts
		}
		var parts []CookPart
		for _, step := range in.Steps {
			for _, seg := range step.Segments {
				if seg.Kind != SegmentIngredient || !seg.Part || seg.LeftOut || seg.Amount == nil || seg.Ingredient != st.Index {
					continue
				}
				parts = append(parts, CookPart{
					ID: fmt.Sprintf("%s#%d-%d", ci.ID, step.Index, len(parts)), AmountText: seg.Amount.Text(), StepIndex: step.Index,
				})
			}
		}
		if len(parts) >= 2 {
			ci.Parts = parts
		}
		out = append(out, ci)
	}
	return out
}

func checklistByStep(steps []InstructionStep, all []CookIngredient) []CookStepGroup {
	byIndex := map[int]CookIngredient{}
	for _, ci := range all {
		byIndex[ci.Index] = ci
	}
	used := map[string]bool{}
	var groups []CookStepGroup
	for _, step := range steps {
		var items []CookStepItem
		clause := ""
		for i, seg := range step.Segments {
			if seg.Kind != SegmentIngredient {
				clause = lastClause(clause + seg.Text)
				continue
			}
			ing, ok := byIndex[seg.Ingredient]
			if !ok {
				continue
			}
			// "While pork cooks, warm tortillas": the pork isn't part of this step.
			if strings.TrimSpace(strings.ToLower(clause)) == "while" {
				clause = ""
				continue
			}
			used[ing.ID] = true
			after := ""
			if i+1 < len(step.Segments) && step.Segments[i+1].Kind != SegmentIngredient {
				after = step.Segments[i+1].Text
			}
			amountText := ""
			if seg.Amount != nil {
				amountText = seg.Amount.Text()
			}
			// "6 wedges" of lime reads as "6 | Lime wedges".
			partWord := leadingPart(after)
			if seg.Amount != nil && seg.Amount.Unit == "wedge" {
				partWord = "wedges"
				if seg.Amount.Quantity.Cmp(oneQuantity) == 0 {
					partWord = "wedge"
				}
				amountText = seg.Amount.Quantity.Format()
			}
			name := ing.Name
			if partWord != "" {
				name = singularName(ing.Name) + " " + partWord
			}
			// "Crushed Tomatoes, crushed" says nothing.
			prep := PrepWords(clause)
			if prep != "" && strings.Contains(strings.ToLower(ing.Name), prep) {
				prep = ""
			}
			// "thinly slice green pepper into strips": how it's cut comes after the name.
			if tail := TrailingPrep(after); tail != "" {
				if prep != "" {
					prep += " " + tail
				} else {
					prep = tail
				}
			}
			existing := slices.IndexFunc(items, func(it CookStepItem) bool { return it.Name == name })
			// "Add remaining onion": what's left from an earlier step.
			if prep == "" && endsWithRemaining(clause) && seg.Amount == nil && existing < 0 {
				prep = "the rest"
			}
			parts := splitParts(after, ing.Name)
			if existing >= 0 {
				// Named again in the same step: add only what's new.
				old := items[existing]
				if prep != "" && !strings.Contains(old.Prep, prep) {
					if old.Prep != "" {
						old.Prep += ", " + prep
					} else {
						old.Prep = prep
					}
				}
				if len(old.Parts) == 0 {
					old.Parts = parts
				}
				items[existing] = old
			} else {
				items = append(items, CookStepItem{
					ID: fmt.Sprintf("%s@%d-%d", ing.ID, step.Index, len(items)), IngredientIndex: ing.Index,
					Name: name, AmountText: amountText, Prep: prep, Parts: parts,
					LeftOut: ing.LeftOut || seg.LeftOut, IngredientKey: ing.IngredientKey,
				})
			}
			clause = ""
		}
		if len(items) > 0 {
			groups = append(groups, CookStepGroup{Index: step.Index, Items: items})
		}
	}
	var ready []CookStepItem
	for _, ci := range all {
		if !used[ci.ID] {
			ready = append(ready, readyItem(ci))
		}
	}
	// A sauce or blend made at home: mix it before cooking, spice by spice.
	for _, ci := range all {
		if used[ci.ID] && len(ci.Components) > 0 {
			ready = append(ready, CookStepItem{
				ID: ci.ID + "@mix", IngredientIndex: ci.Index, Name: ci.Name, AmountText: ci.AmountText,
				Prep: "mix together first", Parts: ci.Components, IngredientKey: ci.IngredientKey,
			})
		}
	}
	// Things to get ready before cooking starts, for a later step. They stay
	// in their own step too, to check off when they go in.
	for _, g := range groups {
		if g.Index <= 1 {
			continue
		}
		for _, it := range g.Items {
			if it.LeftOut {
				continue
			}
			if note := AheadNote(it.Name, g.Index); note != "" {
				ready = append(ready, CookStepItem{
					ID: it.ID + "@ahead", IngredientIndex: it.IngredientIndex, Name: it.Name, AmountText: it.AmountText,
					Prep: note, IngredientKey: it.IngredientKey,
				})
			}
		}
	}
	if len(ready) > 0 {
		groups = append([]CookStepGroup{{Index: 0, Items: ready}}, groups...)
	}
	return groups
}

var oneQuantity = ingredients.NewQuantity(1, 1)

func readyItem(ci CookIngredient) CookStepItem {
	it := CookStepItem{
		ID: ci.ID + "@0", IngredientIndex: ci.Index, Name: ci.Name, AmountText: ci.AmountText,
		LeftOut: ci.LeftOut, IngredientKey: ci.IngredientKey, Parts: ci.Components,
	}
	if len(ci.Components) > 0 {
		it.Prep = "mix together first"
	}
	return it
}

// AheadNote is what to do ahead with an ingredient a later step uses, or "".
func AheadNote(name string, step int) string {
	lower := strings.ToLower(name)
	if lower == "butter" || strings.HasSuffix(lower, " butter") && !strings.Contains(lower, "peanut") {
		return fmt.Sprintf("cut into pieces, for step %d", step)
	}
	if strings.Contains(lower, "cream cheese") {
		return fmt.Sprintf("let soften, for step %d", step)
	}
	return ""
}

// lastClause is the text since the last sentence or clause break.
func lastClause(text string) string {
	if i := strings.LastIndexAny(text, ".;\n:"); i >= 0 {
		return text[i+1:]
	}
	return text
}

// Prep verbs, as done to the ingredient: "Trim and slice" → "trimmed and sliced".
var prepVerbs = map[string]string{
	"slice": "sliced", "sliced": "sliced", "dice": "diced", "diced": "diced", "mince": "minced",
	"minced": "minced", "chop": "chopped", "chopped": "chopped", "quarter": "quartered",
	"quartered": "quartered", "halve": "halved", "halved": "halved", "zest": "zested", "zested": "zested",
	"grate": "grated", "grated": "grated", "peel": "peeled", "peeled": "peeled", "core": "cored",
	"cored": "cored", "trim": "trimmed", "trimmed": "trimmed", "juice": "juiced", "shred": "shredded",
	"shredded": "shredded", "cube": "cubed", "cubed": "cubed", "crush": "crushed", "crushed": "crushed",
	"pit": "pitted", "pitted": "pitted", "drain": "drained", "drained": "drained", "rinse": "rinsed",
	"rinsed": "rinsed", "melt": "melted", "melted": "melted", "soften": "softened", "tear": "torn",
	"pat": "patted dry", "julienne": "julienned", "smash": "smashed",
}

var prepAdverbs = map[string]bool{"thinly": true, "finely": true, "roughly": true, "coarsely": true, "thickly": true}

// prepFillers may sit between prep verbs and the ingredient they apply to:
// "Halve, peel, and finely dice 1 shallot". Anything else ends the run, so a
// word that belongs to something earlier in the sentence never lands on the
// wrong ingredient.
var prepFillers = map[string]bool{
	"and": true, "or": true, "the": true, "a": true, "an": true, "then": true, "from": true, "half": true,
	"of": true, "your": true, "remaining": true, "tsp": true, "tbsp": true, "cup": true, "cups": true,
	"oz": true, "clove": true, "cloves": true, "lb": true, "g": true, "can": true, "cans": true,
}

// closeFillers may sit between a participle and its ingredient: "diced 1 Tbsp butter".
var closeFillers = map[string]bool{
	"the": true, "a": true, "an": true, "half": true, "of": true, "your": true, "remaining": true,
	"tsp": true, "tbsp": true, "cup": true, "cups": true, "oz": true, "clove": true, "cloves": true,
	"lb": true, "g": true,
}

var prepCitrus = map[string]bool{"lime": true, "lemon": true, "orange": true, "pineapple": true, "apple": true, "grapefruit": true}

const fractionRunes = "½¼¾⅓⅔/⁄"

func isNumberish(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if !unicode.IsNumber(r) && !strings.ContainsRune(fractionRunes, r) {
			return false
		}
	}
	return true
}

func allNumbers(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

// PrepWords reads the prep verbs right before an ingredient and writes them
// as done to it: "Peel, core, and dice " → "peeled, cored, and diced". It is
// "" when the clause says nothing about it.
func PrepWords(clause string) string {
	words := strings.FieldsFunc(strings.ToLower(clause), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !strings.ContainsRune(fractionRunes, r)
	})
	var run []string
	wordBeforeRun := ""
	for i := len(words) - 1; i >= 0; i-- {
		w := words[i]
		_, verb := prepVerbs[w]
		if !verb && !prepAdverbs[w] && !prepFillers[w] && !isNumberish(w) {
			wordBeforeRun = w
			break
		}
		run = append([]string{w}, run...)
	}
	// "lime zest", "pineapple juice": a noun, not something done to the next ingredient.
	for _, w := range run {
		if _, verb := prepVerbs[w]; verb {
			if (w == "zest" || w == "juice") && prepCitrus[wordBeforeRun] {
				return ""
			}
			break
		}
	}
	var out []string
	joinedByOr := map[int]bool{}
	for i, w := range run {
		done, ok := prepVerbs[w]
		if !ok || w == "trim" || w == "trimmed" {
			continue
		}
		// A participle only counts right before the ingredient ("add diced
		// butter"); only amounts and articles may sit between them.
		if done == w && slices.ContainsFunc(run[i+1:], func(x string) bool { return !closeFillers[x] && !allNumbers(x) }) {
			continue
		}
		if slices.ContainsFunc(out, func(o string) bool { return strings.HasSuffix(o, done) }) {
			continue
		}
		if i > 0 && run[i-1] == "or" {
			joinedByOr[len(out)] = true
		}
		if i > 0 && prepAdverbs[run[i-1]] {
			out = append(out, run[i-1]+" "+done)
		} else {
			out = append(out, done)
		}
	}
	if len(out) == 0 {
		return ""
	}
	// "minced or grated", "peeled, cored, and diced".
	text := out[0]
	for i := 1; i < len(out); i++ {
		joiner := "and"
		if joinedByOr[i] {
			joiner = "or"
		}
		switch {
		case i < len(out)-1:
			text += ", " + out[i]
		case len(out) > 2:
			text += ", " + joiner + " " + out[i]
		default:
			text += " " + joiner + " " + out[i]
		}
	}
	return text
}

var cutWords = map[string]bool{
	"strips": true, "pieces": true, "wedges": true, "rounds": true, "cubes": true, "chunks": true, "slices": true,
	"halves": true, "quarters": true, "florets": true, "coins": true, "matchsticks": true, "planks": true,
	"rings": true, "thirds": true, "bites": true, "segments": true,
}

// TrailingPrep is how a step cuts something, written after its name: "into
// strips", "into ½-inch pieces", "lengthwise". It is "" when the words after
// it are about something else ("into a bowl").
func TrailingPrep(text string) string {
	end := strings.IndexAny(text, ".;,:\n(")
	if end < 0 {
		end = len(text)
	}
	lower := strings.ToLower(strings.TrimSpace(text[:end]))
	for _, lead := range []string{"into ", "lengthwise", "crosswise", "in half"} {
		if !strings.HasPrefix(lower, lead) {
			continue
		}
		if lead == "into " {
			words := strings.Fields(strings.TrimPrefix(lower, lead))
			if len(words) == 0 || len(words) > 3 || !cutWords[words[len(words)-1]] {
				return ""
			}
		}
		words := strings.Fields(lower)
		if len(words) > 4 {
			words = words[:4]
		}
		return strings.Join(words, " ")
	}
	return ""
}

func endsWithRemaining(clause string) bool {
	words := strings.FieldsFunc(strings.ToLower(clause), func(r rune) bool { return !unicode.IsLetter(r) })
	n := len(words)
	return n > 0 && words[n-1] == "remaining" ||
		n >= 3 && words[n-3] == "rest" && words[n-2] == "of" && words[n-1] == "the"
}

// leadingPart is "greens" in "2 scallion greens": a word right after the name.
func leadingPart(text string) string {
	lower := strings.ToLower(text)
	for _, part := range []string{"whites", "greens", "wedges"} {
		if strings.HasPrefix(lower, " "+part) {
			return part
		}
	}
	return ""
}

// splitParts reads "separating whites from greens" as two sub-rows.
func splitParts(text, name string) []string {
	lower := strings.ToLower(text)
	if i := strings.IndexAny(lower, ".;"); i >= 0 {
		lower = lower[:i]
	}
	if strings.Contains(lower, "white") && strings.Contains(lower, "green") &&
		(strings.Contains(lower, "separat") || strings.Contains(lower, " and ")) {
		return []string{singularName(name) + " whites", singularName(name) + " greens"}
	}
	return nil
}

func singularName(name string) string {
	if strings.HasSuffix(name, "s") && !strings.HasSuffix(name, "ss") {
		return name[:len(name)-1]
	}
	return name
}
