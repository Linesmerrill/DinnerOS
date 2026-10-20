package recipes

import (
	"math/big"
	"regexp"
	"strconv"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file finds the pantry staples a meal-kit card measures out in its
// steps but leaves off its ingredient list: "stir in 1 TBSP butter (2 TBSP for
// 4 servings)". The cooking screens read them as the recipe's own lines, so
// each step bolds its share and the checklist has the total ready.

// stepStaples are the staples a card measures only in its steps, by the name
// the line gets.
var stepStaples = []struct {
	name string
	re   *regexp.Regexp
}{
	{"Butter", regexp.MustCompile(`(?i)\bbutter\b`)},
	{"Sugar", regexp.MustCompile(`(?i)\bsugar\b`)},
	{"Flour", regexp.MustCompile(`(?i)\bflour\b`)},
	{"Olive Oil", regexp.MustCompile(`(?i)\bolive oil\b`)},
}

// withStepStaples returns r with a line for each staple its steps measure
// that its ingredient list doesn't name, after the recipe's own lines so
// their positions don't move. r itself is never changed.
func withStepStaples(r Recipe) Recipe {
	base := smallest(r.Servings)
	if base == 0 {
		return r
	}
	var extra []RecipeIngredient
	for _, staple := range stepStaples {
		if listsStaple(r.Ingredients, staple.re) {
			continue
		}
		if line, ok := stapleLine(r, base, staple.name, staple.re); ok {
			extra = append(extra, line)
		}
	}
	if len(extra) == 0 {
		return r
	}
	r.Ingredients = append(append([]RecipeIngredient(nil), r.Ingredients...), extra...)
	return r
}

// listsStaple reports whether a line already names the staple ("Unsalted
// Butter", "Peanut Butter"): the steps' mentions are that line's.
func listsStaple(lines []RecipeIngredient, re *regexp.Regexp) bool {
	for _, ing := range lines {
		if re.MatchString(ing.Name) {
			return true
		}
	}
	return false
}

// stepPartRe splits a step into its lines and bullets; sizeBulletRe opens
// one that is a card's note for another box size: "• 4 SERVINGS: Use 2 TBSP
// butter".
var stepPartRe = regexp.MustCompile(`[•\n]`)
var sizeBulletRe = regexp.MustCompile(`(?i)^\s*(\d+)\s+servings?\s*:`)

// stapleLine adds up what the steps measure of one staple, for the card's
// smallest box and each other size it's written for. A step's note for a
// size ("(2 TBSP for 4 servings)", "• 4 SERVINGS: Use 2 TBSP butter") gives
// that size's amount; without one, the step's amount scales. Every mention
// must be measured in the same unit, right before the name: a bare "butter"
// or "brown sugar" isn't this staple's amount.
func stapleLine(r Recipe, base int, name string, re *regexp.Regexp) (RecipeIngredient, bool) {
	unit := ""
	type measure struct {
		q     ingredients.Quantity
		notes map[int]ingredients.Quantity
	}
	// measured reads the staple's amounts in text, each with its size note;
	// ok is false for one in a unit that can't be added up.
	measured := func(text string) (out []measure, ok bool) {
		runes := []rune(text)
		for _, loc := range re.FindAllStringIndex(text, -1) {
			start, end := len([]rune(text[:loc[0]])), len([]rune(text[:loc[1]]))
			m, _, found := statedAmount(runes[:start])
			if !found {
				continue
			}
			if m.Unit == "" || m.Unit == "count" || unit != "" && m.Unit != unit {
				return nil, false
			}
			unit = m.Unit
			me := measure{q: m.Quantity, notes: map[int]ingredients.Quantity{}}
			if other, size, _, has := otherServings(runes[end:]); has && (other.Unit == "" || other.Unit == unit) {
				me.notes[size] = other.Quantity
			}
			out = append(out, me)
		}
		return out, true
	}
	totals := map[int]ingredients.Quantity{}
	count := 0
	for _, step := range r.Steps {
		var main []measure
		bullets := map[int]ingredients.Quantity{}
		for _, part := range stepPartRe.Split(CleanStepText(step.Text), -1) {
			found, ok := measured(part)
			if !ok {
				return RecipeIngredient{}, false
			}
			if m := sizeBulletRe.FindStringSubmatch(part); m != nil {
				if size, err := strconv.Atoi(m[1]); err == nil && len(found) > 0 {
					for _, f := range found {
						bullets[size] = bullets[size].Add(f.q)
					}
				}
				continue
			}
			main = append(main, found...)
		}
		count += len(main)
		for _, size := range r.Servings {
			if b, ok := bullets[size]; ok && size != base {
				totals[size] = totals[size].Add(b)
				continue
			}
			for _, f := range main {
				q, ok := f.notes[size]
				if size == base || !ok {
					q = f.q.MulRat(big.NewRat(int64(size), int64(base)))
				}
				totals[size] = totals[size].Add(q)
			}
		}
	}
	if count == 0 {
		return RecipeIngredient{}, false
	}
	line := RecipeIngredient{Name: name, PantryStaple: true}
	for _, size := range r.Servings {
		line.Amounts = append(line.Amounts, Amount{Servings: size, Quantity: totals[size].String(), Unit: unit})
	}
	return line, true
}
