package recipes

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
)

// One story for an ingredient the steps don't use all of: what the steps use
// plus what Have Ready says is left over makes the whole amount, and the Save
// Some for Later card's "Use" is what the steps use (build 123, Hark's review:
// the tacos' lime read "Use 1 Lime", "1 lime for steps 1 and 3, ¾ left
// over", "Halve 1 lime" and "squeeze 1 wedge").

// inTotalUnit is m in total's unit: a light herb's "½" is half its weight,
// and a jam packet's "1 Tbsp" is a share of the packet.
func inTotalUnit(m, total Measure, name string) (*big.Rat, bool) {
	if m.Unit == "count" && !countedUnit(total.Unit) {
		return new(big.Rat).Mul(m.Quantity.Rat(), total.Quantity.Rat()), true
	}
	if q, ok := inUnitOf(m, total); ok {
		return q, true
	}
	return usedOf(m, mention{name: name, amount: &total})
}

// stepShare is what one mention says it uses, read from the step's own
// words: the amount in front of it, "a pinch of", or "half the" (all of it
// when a note says the bigger box uses it all). ok is false when it says no
// amount.
func stepShare(step InstructionStep, i int, total Measure, name string, servings int) (*big.Rat, bool) {
	seg := step.Segments[i]
	if seg.Part && seg.Amount != nil {
		return inTotalUnit(*seg.Amount, total, name)
	}
	before, after := "", ""
	if i > 0 && step.Segments[i-1].Kind == SegmentText {
		before = step.Segments[i-1].Text
	}
	for _, next := range step.Segments[i+1:] {
		after += next.Text
	}
	if pinchBefore(strings.ToLower(strings.TrimSpace(before))) {
		return inTotalUnit(Measure{Quantity: oneQuantity.MulRat(big.NewRat(1, 16)), Unit: "tsp"}, total, name)
	}
	if fraction, _, ok := fractionOf([]rune(before)); ok {
		n, _, all := allNote([]rune(after))
		if !all {
			n = useAllFor(after, name)
		}
		if n > 0 && servings >= n {
			fraction = big.NewRat(1, 1)
		}
		return new(big.Rat).Mul(total.Quantity.Rat(), fraction), true
	}
	return nil, false
}

// leftOverStory lists where a recipe's steps, Have Ready and Save card tell
// different stories about an ingredient with something left over.
func leftOverStory(in Instructions) []string {
	var problems []string
	for _, st := range in.Ingredients {
		if st.LeftOver == nil || st.LeftOut != nil {
			continue
		}
		if st.Amount == nil || st.Used == nil {
			problems = append(problems, fmt.Sprintf("%s: %s left over, but no amount or use", st.Name, st.LeftOver.Text()))
			continue
		}
		total := *st.Amount
		left, ok := inTotalUnit(*st.LeftOver, total, st.Name)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %s left over doesn't read in %s", st.Name, st.LeftOver.Text(), total.Text()))
			continue
		}
		// What the steps themselves say they use: every share they measure.
		steps := new(big.Rat)
		var shares []string
		for _, step := range in.Steps {
			for i, seg := range step.Segments {
				if seg.Kind != SegmentIngredient || seg.Ingredient != st.Index || seg.LeftOut {
					continue
				}
				q, ok := stepShare(step, i, total, st.Name, in.Servings)
				if !ok {
					continue
				}
				steps.Add(steps, q)
				shares = append(shares, fmt.Sprintf("step %d: %s", step.Index, seg.Text))
			}
		}
		used, ok := inTotalUnit(*st.Used, total, st.Name)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: uses %s, which doesn't read in %s", st.Name, st.Used.Text(), total.Text()))
			continue
		}
		// Kitchen spoons round: within an eighth of the whole is the same.
		near := func(a, b *big.Rat) bool {
			diff := new(big.Rat).Sub(a, b)
			return new(big.Rat).Abs(diff).Cmp(new(big.Rat).Mul(total.Quantity.Rat(), big.NewRat(1, 8))) <= 0
		}
		whole := total.Quantity.Rat()
		if !near(new(big.Rat).Add(steps, left), whole) {
			problems = append(problems, fmt.Sprintf("%s: the steps use %s (%s), and %s is left over, of %s",
				st.Name, steps.FloatString(3), strings.Join(shares, "; "), st.LeftOver.Text(), total.Text()))
		}
		if !near(new(big.Rat).Add(used, left), whole) {
			problems = append(problems, fmt.Sprintf("%s: uses %s and %s is left over, of %s", st.Name, st.Used.Text(), st.LeftOver.Text(), total.Text()))
		}
		// Have Ready says what's left over, in the same words.
		row := haveReadyIndex(in, st.Index)
		if !strings.Contains(row.Prep, st.LeftOver.Text()+" left over") && len(row.Parts) == 0 {
			problems = append(problems, fmt.Sprintf("%s: Have Ready says %q, not %s left over", st.Name, row.Prep, st.LeftOver.Text()))
		}
		// The Save card's "Use" is what the steps use.
		note := saveForLaterResponses([]SaveForLater{{
			Keys: []string{st.IngredientKey}, Name: st.Name, Use: total.Text(),
			Save: "Save 1 for Later Dinner.", Text: "Use " + total.Text() + ". Save 1 for Later Dinner.",
		}}, in)
		want := "Use " + usedText(st) + ". "
		if len(note) != 1 || usedText(st) == "" || !strings.HasPrefix(note[0].Text, want) {
			problems = append(problems, fmt.Sprintf("%s: the Save card says %+v, want it to start %q", st.Name, note, want))
		}
	}
	return problems
}

func haveReadyIndex(in Instructions, index int) CookStepItem {
	for _, g := range in.Checklist.ByStep {
		if g.Index != 0 {
			continue
		}
		for _, it := range g.Items {
			if it.IngredientIndex == index {
				return it
			}
		}
	}
	return CookStepItem{}
}

// The tacos from Hark's review: halved, then one half squeezed over them.
func TestHalvedLimeTellsOneStory(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{
		instructionLine("c", "Cilantro", "1/4", "oz"),
		instructionLine("l", "Lime", "1", "count"),
		instructionLine("b", "Ground Beef", "10", "oz"),
		instructionLine("t", "Flour Tortillas", "6", "count"),
		instructionLine("o", "Crispy Fried Onions", "1", "count"),
	}, "Roughly chop ¼ oz cilantro. Halve 1 lime.",
		"Cook 10 oz ground beef in a large pan until browned, 5 minutes.",
		"Warm 6 tortillas. Fill with beef, then top with cilantro, crispy fried onions, and a squeeze of lime.")
	for _, tc := range []struct {
		servings          int
		step3, ready, use string
	}{
		{2, "and a squeeze of ½ lime.", "for steps 1 and 3, ½ left over", "Use ½ Lime. "},
		{4, "and a squeeze of 1 lime.", "for steps 1 and 3, 1 left over", "Use 1 Lime. "},
	} {
		in := Annotate(r, tc.servings, nil, false)
		if got := joined(in.Steps[2]); !strings.HasSuffix(got, tc.step3) {
			t.Errorf("%d servings: step 3 = %q, want it to end %q", tc.servings, got, tc.step3)
		}
		if row := haveReady(in, "Lime"); row.Prep != tc.ready {
			t.Errorf("%d servings: Have Ready lime = %q, want %q", tc.servings, row.Prep, tc.ready)
		}
		notes := saveForLaterResponses([]SaveForLater{{
			Keys: []string{in.Ingredients[1].IngredientKey}, Name: "Lime", Use: "1 Lime",
			Save: "Save 1 Lime for Sesame-Soy Pork Bowls on Tuesday.",
			Text: "Use 1 Lime. Save 1 Lime for Sesame-Soy Pork Bowls on Tuesday.",
		}}, in)
		if want := tc.use + "Save 1 Lime for Sesame-Soy Pork Bowls on Tuesday."; len(notes) != 1 || notes[0].Text != want {
			t.Errorf("%d servings: Save card = %+v, want %q", tc.servings, notes, want)
		}
		// Step 3's half comes from the lime halved in step 1.
		for _, g := range in.Checklist.ByStep {
			for _, it := range g.Items {
				if g.Index == 3 && it.Name == "Lime" && it.Prep != "from step 1" {
					t.Errorf("%d servings: step 3 lime = %+v, want from step 1", tc.servings, it)
				}
			}
		}
		for _, p := range leftOverStory(in) {
			t.Errorf("%d servings: %s", tc.servings, p)
		}
	}
}

// A quartered lime still reads in wedges: one squeezed, three left over, and
// the card says the same.
func TestQuarteredLimeTellsOneStory(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{instructionLine("l", "Lime", "1", "count")},
		"Quarter lime.", "Top with a squeeze of lime.")
	in := Annotate(r, 2, nil, false)
	if got := joined(in.Steps[1]); got != "Top with a squeeze of 1 lime wedge." {
		t.Errorf("step 2 = %q", got)
	}
	if got := usedText(in.Ingredients[0]); got != "1 Lime wedge" {
		t.Errorf("used = %q, want 1 Lime wedge", got)
	}
	for _, p := range leftOverStory(in) {
		t.Error(p)
	}
}

// A card with nothing left over keeps the planner's own "Use".
func TestSaveCardKeepsItsUseWhenNothingIsLeftOver(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{instructionLine("l", "Lime", "1", "count")},
		"Quarter lime.", "Serve with lime wedges.")
	in := Annotate(r, 2, nil, false)
	notes := saveForLaterResponses([]SaveForLater{{
		Keys: []string{in.Ingredients[0].IngredientKey}, Name: "Lime", Use: "1 Lime",
		Save: "Save 1 Lime for Bowls.", Text: "Use 1 Lime. Save 1 Lime for Bowls.",
	}}, in)
	if len(notes) != 1 || notes[0].Text != "Use 1 Lime. Save 1 Lime for Bowls." {
		t.Errorf("notes = %+v", notes)
	}
}

// Every corpus recipe at every size tells one story.
func TestLeftOverStoryCorpus(t *testing.T) {
	for _, c := range loadCorpus(t) {
		for _, n := range c.Servings {
			for _, p := range leftOverStory(Annotate(c.recipe(), n, nil, true)) {
				t.Errorf("%s for %d: %s", c.Name, n, p)
			}
		}
	}
}

// Every seeded meal-kit recipe at every size tells one story. The seed file
// is local (importers/hellofresh/data is not committed); SEED_RECIPES names
// another copy.
func TestLeftOverStorySeededRecipes(t *testing.T) {
	path := cmpOr(os.Getenv("SEED_RECIPES"), "../../../importers/hellofresh/data/import/recipes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no seeded recipes at %s", path)
	}
	var f ImportFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, ir := range f.Recipes {
		r := buildRecipe(cleanImportRecipe(ir), func(ing ImportIngredient) string { return ing.SourceIngredientID })
		for _, n := range r.Servings {
			in := Annotate(r, n, nil, true)
			for _, st := range in.Ingredients {
				if st.LeftOver != nil {
					checked++
				}
			}
			for _, p := range leftOverStory(in) {
				t.Errorf("%s for %d: %s", r.Name, n, p)
			}
		}
	}
	t.Logf("%d recipes, %d ingredients with some left over", len(f.Recipes), checked)
}

// "half the tomatoes (all for 4 servings)" of a counted can: the bigger box
// uses the whole can, so nothing is left over there.
func TestHalfOfACountedLineIsAllForTheBiggerBox(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{
		{IngredientID: "t", Name: "Crushed Tomatoes", Amounts: []Amount{
			{Servings: 2, Quantity: "1/2", Unit: "count"}, {Servings: 4, Quantity: "1", Unit: "count"},
		}},
		instructionLine("o", "Onion", "1", "count"),
	}, "Halve, peel, and finely dice half the onion (all for 4).",
		"Add half the tomatoes (all for 4 servings) to the pan with the onion.")
	for _, tc := range []struct {
		servings       int
		onion          string
		tomatoLeftOver bool
	}{
		{2, "½", true},
		{4, "2", false},
	} {
		in := Annotate(r, tc.servings, nil, false)
		if lo := in.Ingredients[0].LeftOver; (lo != nil) != tc.tomatoLeftOver {
			t.Errorf("%d servings: tomatoes left over = %v, want %v", tc.servings, lo, tc.tomatoLeftOver)
		}
		for _, g := range in.Checklist.ByStep {
			for _, it := range g.Items {
				if g.Index == 1 && it.Name == "Onion" && it.AmountText != tc.onion {
					t.Errorf("%d servings: step 1 onion = %q, want %q", tc.servings, it.AmountText, tc.onion)
				}
			}
		}
		for _, p := range leftOverStory(in) {
			t.Errorf("%d servings: %s", tc.servings, p)
		}
	}
}

// "(use all the cannellini beans … for 4 servings)" later in the step: half
// the can for 2, all of it for 4.
func TestUseAllNoteLaterInTheStep(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{
		{IngredientID: "c", Name: "Cannellini Beans", Amounts: []Amount{
			{Servings: 2, Quantity: "1", Unit: "count"}, {Servings: 4, Quantity: "1", Unit: "count"},
		}},
		instructionLine("t", "Dried Thyme", "1/2", "tsp"),
	}, "Stir in half the cannellini beans, half the thyme, and 2 cups water "+
		"(use all the cannellini beans, all the thyme, and 4 cups water for 4 servings).")
	in := Annotate(r, 2, nil, false)
	if lo := in.Ingredients[0].LeftOver; lo == nil || lo.Text() != "½" {
		t.Errorf("2 servings: beans left over = %v, want ½", lo)
	}
	in = Annotate(r, 4, nil, false)
	if lo := in.Ingredients[0].LeftOver; lo != nil {
		t.Errorf("4 servings: beans left over = %q, want none", lo.Text())
	}
	if got := joined(in.Steps[0]); !strings.Contains(got, "1 tsp thyme") {
		t.Errorf("4 servings: step = %q, want all the thyme (1 tsp)", got)
	}
	if n := useAllFor("(use all the cannellini beans for 4 servings)", "Kidney Beans"); n != 0 {
		t.Errorf("kidney beans read the cannellini's note: %d", n)
	}
}

// Butter cut up ahead for a step that melts only half of it says what's left.
func TestAheadButterSaysWhatsLeftOver(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{
		instructionLine("b", "Bulgur", "1/2", "cup"), instructionLine("u", "Butter", "1", "tbsp"),
	}, "Cook bulgur.", "Drain bulgur.", "Fluff bulgur.", "Stir ½ TBSP butter (1 TBSP for 4 servings) into bulgur until melted.")
	in := Annotate(r, 2, nil, false)
	if row := haveReady(in, "Butter"); row.Prep != "cut into pieces, for step 4, 1 ½ tsp left over" {
		t.Errorf("Have Ready butter = %q", row.Prep)
	}
	for _, p := range leftOverStory(in) {
		t.Error(p)
	}
}
