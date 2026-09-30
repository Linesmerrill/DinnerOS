package recipes

import (
	"math/big"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file renders a recipe's steps the way a member reads them while
// cooking: every ingredient the recipe lists is marked where the step names
// it, with the amount for the servings being cooked, and a specialty
// ingredient reads as whatever the household actually buys or makes.
//
// Nothing here is stored. Instructions are built on every read from the
// recipe as imported plus the household's current choices, so changing a
// choice changes the next read of every recipe that uses it, with no
// migration and nothing to keep in sync (decision 512).

// SegmentKind says what a piece of a rendered step is.
type SegmentKind string

// Segment kinds.
const (
	// SegmentText is plain instruction text.
	SegmentText SegmentKind = "text"
	// SegmentIngredient is an ingredient the recipe lists, with its amount.
	SegmentIngredient SegmentKind = "ingredient"
)

// Measure is an exact amount in a DinnerOS unit code.
type Measure struct {
	Quantity ingredients.Quantity
	Unit     string
	// Or is another way to measure it, one per unit ("bouillon cube"), shown
	// in parentheses: "2 tsp (or 2 bouillon cubes)". Empty for most.
	Or string
	// Exact keeps the unit as it is: a kitchen measure counted per packet
	// ("3 tsp" of bouillon base, one per packet) isn't tidied to "1 Tbsp".
	Exact bool
}

// Text renders the measure for people: "1 ½ cups", "2 cloves", "3". Big
// spoon counts read as a kitchen would measure them: "12 tsp" is "4 Tbsp",
// "12 Tbsp" is "¾ cup".
func (m Measure) Text() string {
	if m.Or == "" && !m.Exact {
		m = tidySpoons(m)
		// "10 tsp" measures as "3 Tbsp + 1 tsp".
		if mixed := spoonsMixed(m); mixed != "" {
			return mixed
		}
	}
	m = kitchenRound(m)
	text := m.Quantity.Format()
	if u, err := ingredients.LookupUnit(m.Unit); err == nil {
		if label := u.Label(m.Quantity); label != "" {
			text += " " + label
		}
	}
	if m.Or != "" {
		or := m.Or
		if m.Quantity.Cmp(ingredients.NewQuantity(1, 1)) > 0 {
			or += "s"
		}
		text += " (or " + m.Quantity.Format() + " " + or + ")"
	}
	return text
}

// kitchenRound rounds a spoon, cup, or small ounce amount with no kitchen
// fraction (⅛ oz scaled to 3 servings is 0.1875) to the nearest eighth, so it
// reads "¼ oz", not "0.19 oz". Whole and kitchen amounts are untouched.
func kitchenRound(m Measure) Measure {
	if m.Exact || m.Or != "" {
		return m
	}
	switch m.Unit {
	case "ml":
		// "12 ml" of vinegar is 2½ tsp to anyone holding measuring spoons.
		if m.Quantity.Cmp(ingredients.NewQuantity(90, 1)) > 0 {
			return m
		}
		m = tidySpoons(Measure{Quantity: m.Quantity.MulRat(big.NewRat(1, 5)), Unit: "tsp"})
		return kitchenRound(m)
	case "tsp", "tbsp", "cup":
	case "oz":
		if m.Quantity.Cmp(ingredients.NewQuantity(2, 1)) >= 0 {
			return m
		}
	default:
		return m
	}
	r := m.Quantity.Rat()
	eighths := new(big.Rat).Mul(r, big.NewRat(8, 1))
	if eighths.IsInt() || new(big.Rat).Mul(r, big.NewRat(3, 1)).IsInt() {
		return m
	}
	f, _ := eighths.Float64()
	n := int64(f + 0.5)
	if n < 1 {
		n = 1
	}
	m.Quantity = ingredients.NewQuantity(n, 8)
	return m
}

// spoonsMixed writes 6 tsp or more that didn't tidy to whole or half
// tablespoons as tablespoons plus teaspoons: "3 Tbsp + 1 tsp", not "10 tsp".
// It is "" for anything else.
func spoonsMixed(m Measure) string {
	if m.Unit != "tsp" || m.Quantity.Cmp(ingredients.NewQuantity(6, 1)) < 0 {
		return ""
	}
	r := m.Quantity.Rat()
	tbsp := new(big.Int).Quo(r.Num(), new(big.Int).Mul(r.Denom(), big.NewInt(3)))
	rest := new(big.Rat).Sub(r, new(big.Rat).SetInt(new(big.Int).Mul(tbsp, big.NewInt(3))))
	whole := Measure{Quantity: ingredients.NewQuantity(tbsp.Int64(), 1), Unit: "tbsp", Exact: true}
	if rest.Sign() == 0 {
		return whole.Text()
	}
	part := Measure{Quantity: ingredients.NewQuantity(1, 1).MulRat(rest), Unit: "tsp", Exact: true}
	return whole.Text() + " + " + part.Text()
}

// tidySpoons moves 3 tsp or more up to tablespoons, and 8 Tbsp or more up to
// cups, when the result is a half tablespoon or a quarter cup exactly.
func tidySpoons(m Measure) Measure {
	up := func(from, to string, per, min, step int64) bool {
		if m.Unit != from || m.Quantity.Cmp(ingredients.NewQuantity(min, 1)) < 0 {
			return false
		}
		q := m.Quantity.MulRat(big.NewRat(1, per))
		if !q.MulRat(big.NewRat(step, 1)).Rat().IsInt() {
			return false
		}
		m = Measure{Quantity: q, Unit: to}
		return true
	}
	up("tsp", "tbsp", 3, 3, 2)
	up("tbsp", "cup", 16, 8, 4)
	return m
}

// packetContents is what a packet of a specialty holds, from its unit sizes
// ("1 count = 2 tbsp"), or nil when the amount isn't counted in packets.
func packetContents(m *Measure, spec *grocery.Specialty) *Measure {
	if m == nil || (m.Unit != "count" && m.Unit != "package") {
		return nil
	}
	for _, size := range spec.UnitSizes {
		if size.Unit == m.Unit {
			return &Measure{Quantity: m.Quantity.Mul(size.Quantity), Unit: size.SizeUnit}
		}
	}
	return nil
}

// howToMake is the sentence that makes amount of a specialty from its house
// recipe: "To make 2 Tbsp Tex-Mex Paste, stir together 2 Tbsp Tomato Paste,
// …". Empty when there's no recipe or the amounts don't convert.
func howToMake(amount *Measure, spec *grocery.Specialty) string {
	how := spec.HowToMake
	if how == nil || how.Yield.Quantity.IsZero() {
		return ""
	}
	inYield, ok := grocery.ConvertMeasure(amount.Quantity.Rat(), amount.Unit, how.Yield.Unit, spec.UnitSizes)
	if !ok {
		return ""
	}
	ratio := inYield.Quo(inYield, how.Yield.Quantity.Rat())
	parts := make([]string, 0, len(how.Components))
	for _, c := range how.Components {
		scaled := scaledComponent(c, ratio)
		if scaled == nil {
			parts = append(parts, c.Name)
			continue
		}
		parts = append(parts, kitchenSpoon(*scaled).Text()+" "+strings.ToLower(c.Name))
	}
	return "To make " + amount.Text() + " " + spec.Name + ", stir together " + joinList(parts) + "."
}

// kitchenSpoon writes less than a tablespoon in teaspoons: "¾ tsp", not "¼ Tbsp".
func kitchenSpoon(m Measure) Measure {
	if m.Unit == "tbsp" && m.Quantity.Cmp(ingredients.NewQuantity(1, 1)) < 0 {
		return Measure{Quantity: m.Quantity.Mul(ingredients.NewQuantity(3, 1)), Unit: "tsp"}
	}
	return m
}

func unitOf(m *Measure) string {
	if m == nil {
		return ""
	}
	return m.Unit
}

// kitchenMeasure turns a packet count into what to scoop ("1 tomato paste" is
// 2 Tbsp), for an ingredient the household measures instead of opening a
// packet. Anything else comes back as it was.
func kitchenMeasure(name string, m *Measure) *Measure {
	if m == nil {
		return nil
	}
	km, ok := ingredients.KitchenMeasureFor(name, m.Unit)
	if !ok {
		return m
	}
	q := m.Quantity.MulRat(km.PerPacket)
	out := Measure{Quantity: q, Unit: km.Unit, Exact: true}
	if km.Or != "" {
		out.Or = km.Or
	}
	return &out
}

// Segment is one run of a rendered step. Joining every Text in order gives
// the step's text exactly, so a client that only wants a string needs no
// offsets and no encoding rules.
type Segment struct {
	Kind SegmentKind
	Text string
	// IngredientID, Name, Amount, and the flags are set on SegmentIngredient.
	// IngredientID is empty for an ingredient the catalog doesn't know.
	IngredientID string
	// Ingredient is the position of the recipe ingredient the segment
	// stands for, set on SegmentIngredient.
	Ingredient int
	// Name is what the segment stands for: the recipe's ingredient, or the
	// substitute when one replaced it.
	Name string
	// Amount is nil when the recipe gave no amount, when a later mention in
	// the same step already carried it, or when a substitution's amount
	// doesn't convert.
	Amount *Measure
	// Part is true when Amount is this step's share of the ingredient, as
	// the card wrote it, rather than the recipe's whole amount.
	Part bool
	// Spicy is the catalog's heat signal (ingredients.Spicy).
	Spicy bool
	// Substituted is true when the household's specialty choice changed this
	// mention's name or amount.
	Substituted bool
	// SpecialtyID is set when the mention is a specialty ingredient, whether
	// or not the household has chosen for it.
	SpecialtyID string
	// SpecialtyName is the specialty ingredient this mention stands in for,
	// set with SpecialtyID.
	SpecialtyName string
	// LeftOut is true when the household leaves this ingredient out of the
	// recipe. The mention stays in the step, without an amount, so the step
	// still reads as written and the client can strike it through.
	LeftOut bool
}

// StepNoteKind says why a step carries a note.
type StepNoteKind string

// Step note kinds.
const (
	// NoteSubstitution: the household's choice changes what goes in, or how
	// much, in a way a swapped word can't say on its own.
	NoteSubstitution StepNoteKind = "substitution"
	// NoteLeftOut: the step names an ingredient the household leaves out of
	// this recipe.
	NoteLeftOut StepNoteKind = "left_out"
)

// StepNote is a sentence shown under a step.
type StepNote struct {
	Kind        StepNoteKind
	SpecialtyID string
	Text        string
}

// InstructionStep is one step ready to read.
type InstructionStep struct {
	Index int
	// Text is the rendered step: the recipe's own text with substituted names
	// and amounts in place.
	Text string
	// Original is the recipe's own text, set only when Text differs from it.
	Original string
	ImageURL string
	Segments []Segment
	Notes    []StepNote
	// Timers are the step's cooking times, named (timers.go).
	Timers []StepTimer
	// LeftOut is true when every ingredient the step names is left out: the
	// step is there to make something the household doesn't make, and can be
	// skipped. A step that names nothing is never LeftOut.
	LeftOut bool
}

// IngredientState is one of the recipe's ingredients as the household cooks
// it, in the recipe's order, so the ingredient list can show what is left out
// and what a component is made of without matching names on the client.
type IngredientState struct {
	// Index is the ingredient's position in the recipe's ingredient list.
	Index int
	// IngredientKey is the key a skip for this ingredient is made with: the
	// catalog ID, or "name:" and the normalized name.
	IngredientKey string
	Name          string
	// SwapName is the protein cooked instead, when the meal swaps it ("Ground
	// Beef" for "Ground Pork"); empty otherwise.
	SwapName string
	// Amount is the ingredient's amount for the servings, as the cooking
	// screens show it: a packet reads as a kitchen measure ("2 Tbsp").
	Amount *Measure
	// LeftOut is set when the household leaves it out of this recipe.
	LeftOut *grocery.LeftOut
	// Component is set when the ingredient is a specialty ingredient the
	// household makes from store ingredients: a component of the meal.
	Component *Component
}

// Component is a specialty ingredient (a sauce, crema, paste, or blend) as the
// household makes it: which option, and from what.
type Component struct {
	SpecialtyID   string
	SpecialtyName string
	OptionName    string
	Type          grocery.ChoiceType
	// Parts are the store ingredients, with amounts for the servings being
	// cooked when a store alternative's amount converts ("2 tsp Sour Cream").
	Parts []string
}

// SpecialtyRef names a specialty ingredient a recipe uses.
type SpecialtyRef struct {
	ID   string
	Key  string
	Name string
}

// Substitution is one specialty ingredient the instructions read differently
// because of the household's choice.
type Substitution struct {
	SpecialtyRef
	OptionID   string
	OptionName string
	// Type is grocery.ChoiceStoreAlternative or grocery.ChoiceHouseMadeBatch.
	Type grocery.ChoiceType
	// Source is "household" when a member chose the option and "strategy"
	// when the household's standing strategy picked it
	// (docs/specialty-ingredients.md).
	Source string
	// Text says what replaced what: "Tex-Mex Paste → tomato paste, chili
	// powder, cumin, oil".
	Text string
}

// Choice sources.
const (
	ChoiceSourceHousehold = "household"
	ChoiceSourceStrategy  = "strategy"
)

// Instructions is a recipe's steps rendered for one serving size.
type Instructions struct {
	RecipeID   string
	RecipeName string
	// Servings is the serving size the amounts are for; ServingOptions are the
	// sizes the recipe was authored at.
	Servings       int
	ServingOptions []int
	// SpecialtiesApplied is true when the household's specialty choices were
	// consulted. It is false when no source was configured, so a client can
	// tell "no substitutions" from "substitutions weren't looked up".
	SpecialtiesApplied bool
	Steps              []InstructionStep
	// Substitutions are the specialty ingredients that read differently, and
	// Unchosen the ones the household hasn't decided about yet — those read as
	// the card wrote them.
	Substitutions []Substitution
	Unchosen      []SpecialtyRef
	// LeftOutApplied is true when the household's left-out ingredients were
	// consulted; Ingredients are the recipe's ingredients in order.
	LeftOutApplied bool
	Ingredients    []IngredientState
	// Checklist is the cooking screen's ingredient checklist, built from the
	// steps above (checklist.go).
	Checklist Checklist
}

// GroceryLines returns the recipe's ingredient lines for servings in the form
// the grocery engine and the specialty resolver expect. Lines with no
// authored amount at that size still appear, without a quantity.
func GroceryLines(r Recipe, servings int) []grocery.Line {
	lines := make([]grocery.Line, 0, len(r.Ingredients))
	for _, ing := range r.Ingredients {
		line := grocery.Line{
			IngredientKey: ingredientKey(ing), Name: ing.Name,
			Category: ing.Category, PantryStaple: ing.PantryStaple,
		}
		if m, ok := amountAt(ing, servings); ok {
			q := m.Quantity
			line.Quantity, line.UnitCode = &q, m.Unit
		}
		lines = append(lines, line)
	}
	return lines
}

// ingredientKey is the key a line is registered under: the catalog ID, or a
// stable key from the normalized name when the catalog doesn't know it. It
// matches what planning builds, so grocery.Specialties resolves the same way
// for a recipe read as for a week's list.
func ingredientKey(ing RecipeIngredient) string {
	if ing.IngredientID != "" {
		return ing.IngredientID
	}
	return "name:" + ingredients.NormalizeName(ing.Name)
}

// amountAt returns the line's authored amount for servings. Amounts are never
// scaled from another serving size (the grocery engine's rule), so a size the
// recipe wasn't authored at has no amount.
func amountAt(ing RecipeIngredient, servings int) (Measure, bool) {
	for _, a := range ing.Amounts {
		if a.Servings != servings {
			continue
		}
		q, ok := a.ExactQuantity()
		if !ok || a.Unit == "" && q.IsZero() {
			return Measure{}, false
		}
		return Measure{Quantity: q, Unit: a.Unit}, true
	}
	return Measure{}, false
}

// mention is one ingredient of the recipe, ready to be found in step text.
type mention struct {
	ingredientID string
	// name is the recipe's own name; display is what the step should read
	// (the substitute's name when one replaced it).
	name    string
	display string
	amount  *Measure
	// baseAmount is the amount for the card's smallest serving size, the
	// size its step numbers are written for.
	baseAmount *Measure
	spicy      bool

	substituted   bool
	specialtyID   string
	specialtyName string
	// note is the sentence a step gets the first time it mentions this
	// ingredient, empty when the swap speaks for itself.
	note string
	// leftOut is set when the household leaves the ingredient out.
	leftOut bool
	// swapped is set when the meal cooks another protein in its place; display
	// is that protein.
	swapped bool
	// wedges is set for a lime or lemon the recipe cuts into wedges, so a
	// squeeze or half of it reads in wedges.
	wedges bool
	// packaged is set for a plural name counted in ones ("1 Black Beans"): a
	// can or a package, so a step doesn't count it.
	packaged bool
	// forms are the spellings to look for, longest first.
	forms []string
}

// Swap is a protein the meal cooks in place of the recipe's: its name, and how
// much of it per amount of the original (2 for a double portion).
type Swap struct {
	Name   string
	Factor *big.Rat
}

// Swaps are a meal's protein swaps, by ingredient key.
type Swaps map[string]Swap

// Annotate renders r's steps for servings, with the household's specialty
// choices in specs applied. specs may be nil, in which case the steps read as
// the recipe wrote them; applied says whether they were looked up at all.
func Annotate(r Recipe, servings int, specs grocery.Specialties, applied bool) Instructions {
	return AnnotateWith(r, servings, specs, applied, nil, false)
}

// AnnotateWith is Annotate with what the household leaves out of the recipe
// (leftOut, looked up when leftOutApplied). A left-out ingredient is still
// named in the steps, marked and without an amount, with a note — never
// deleted — so a step reads sensibly and the member knows the recipe calls for
// it. A left-out specialty ingredient isn't substituted: nothing is made for
// it.
func AnnotateWith(r Recipe, servings int, specs grocery.Specialties, applied bool, leftOut grocery.LeftOutSet, leftOutApplied bool) Instructions {
	return AnnotateMeal(r, servings, specs, applied, leftOut, leftOutApplied, nil)
}

// AnnotateMeal is AnnotateWith for a planned meal that swaps or doubles its
// protein: the steps name the protein being cooked, at its amount, so nobody
// reads "pork*" and has to remember they bought beef.
func AnnotateMeal(r Recipe, servings int, specs grocery.Specialties, applied bool, leftOut grocery.LeftOutSet, leftOutApplied bool, swaps Swaps) Instructions {
	out := Instructions{
		RecipeID: r.ID, RecipeName: r.Name, Servings: servings,
		ServingOptions: append([]int(nil), r.Servings...), SpecialtiesApplied: applied,
		Steps: make([]InstructionStep, 0, len(r.Steps)), LeftOutApplied: leftOutApplied,
		Ingredients: make([]IngredientState, 0, len(r.Ingredients)),
	}
	mentions := make([]mention, 0, len(r.Ingredients))
	seenSpecialty := map[string]bool{}
	for index, ing := range r.Ingredients {
		key := ingredientKey(ing)
		state := IngredientState{Index: index, IngredientKey: key, Name: ing.Name}
		m := mention{ingredientID: ing.IngredientID, name: ing.Name, display: ing.Name, spicy: ingredients.Spicy(ing.Name)}
		if a, ok := cookAmountAt(r, ing, servings); ok {
			m.amount = &a
		}
		if a, ok := amountAt(ing, smallest(r.Servings)); ok {
			m.baseAmount = &a
		}
		if sw, ok := swaps[key]; ok {
			if sw.Factor != nil {
				m.amount, m.baseAmount = scaled(m.amount, sw.Factor), scaled(m.baseAmount, sw.Factor)
			}
			if sw.Name != "" && !strings.EqualFold(sw.Name, ing.Name) {
				m.display, m.swapped = sw.Name, true
				m.spicy = ingredients.Spicy(sw.Name)
				state.SwapName = sw.Name
			}
		}
		m.wedges = wedgeCitrus(ing.Name) != "" && cutIntoWedges(r.Steps, ing.Name)
		m.packaged = m.baseAmount != nil && m.baseAmount.Unit == "count" &&
			(m.baseAmount.Quantity.Cmp(ingredients.NewQuantity(1, 1)) <= 0 && singularize(ing.Name) != ing.Name ||
				// "1 Marinara Cup" is the container: "add 1 marinara" would be wrong.
				packagingName(ing.Name))
		// The packet count, before it reads as a kitchen measure: a substitute's
		// amount is worked out from packets.
		packets, basePackets := m.amount, m.baseAmount
		state.Amount = kitchenMeasure(ing.Name, m.amount)
		if lo, ok := leftOut.Lookup(key, "name:"+ingredients.NormalizeName(ing.Name)); ok {
			m.leftOut = true
			state.LeftOut = &lo
		}
		spec := specs[key]
		if spec != nil {
			state.Component = componentOf(spec, m.amount)
		}
		if spec == nil || spec.Choice == nil || spec.Choice.Type == grocery.ChoiceAsIs || m.leftOut {
			m.amount, m.baseAmount = kitchenMeasure(ing.Name, packets), kitchenMeasure(ing.Name, basePackets)
		}
		out.Ingredients = append(out.Ingredients, state)
		if spec != nil && m.leftOut {
			// Nothing is made or bought for a left-out specialty ingredient,
			// so it reads by its own name and isn't listed as substituted.
			m.specialtyID, m.specialtyName = spec.ID, spec.Name
			spec = nil
		}
		if spec != nil {
			m.specialtyID, m.specialtyName = spec.ID, spec.Name
			ref := SpecialtyRef{ID: spec.ID, Key: spec.Key, Name: spec.Name}
			switch {
			case spec.Choice == nil:
				if !seenSpecialty[spec.ID] {
					out.Unchosen = append(out.Unchosen, ref)
				}
				// Nothing chosen yet: a packet reads as what it holds ("2 Tbsp"),
				// and the step says how to make that much from scratch.
				if held := packetContents(m.amount, spec); held != nil {
					m.amount = held
					m.baseAmount = packetContents(m.baseAmount, spec)
					state.Amount = held
					out.Ingredients[len(out.Ingredients)-1].Amount = held
					m.note = howToMake(held, spec)
				}
			case spec.Choice.Type == grocery.ChoiceAsIs:
				// The household buys it under its own name: nothing changes.
			default:
				sub := applySubstitute(&m, spec)
				// A packet named by its own name reads as what it holds in the list too.
				if m.display == m.name {
					if held := packetContents(packets, spec); held != nil {
						out.Ingredients[len(out.Ingredients)-1].Amount = held
					}
				}
				// The household's own measure for a packet wins over the option's:
				// a stock concentrate packet is 1 tsp of whatever base they use.
				if km, ok := ingredients.KitchenMeasureFor(ing.Name, unitOf(packets)); ok && m.display != m.name {
					m.amount = &Measure{Quantity: packets.Quantity.MulRat(km.PerPacket), Unit: km.Unit, Exact: true}
					if basePackets != nil {
						m.baseAmount = &Measure{Quantity: basePackets.Quantity.MulRat(km.PerPacket), Unit: km.Unit, Exact: true}
					}
					state.Amount = m.amount
					out.Ingredients[len(out.Ingredients)-1].Amount = m.amount
				}
				if sub != nil && !seenSpecialty[spec.ID] {
					sub.SpecialtyRef = ref
					out.Substitutions = append(out.Substitutions, *sub)
				}
			}
			seenSpecialty[spec.ID] = true
		}
		m.forms = nameForms(m.name, specs[key])
		mentions = append(mentions, m)
	}
	dedupeForms(mentions)
	amounts := stepAmounts{
		servings: servings, base: smallest(r.Servings), stated: statedMentions(r.Steps, mentions),
		notedBefore: map[string]bool{}, seen: map[int]bool{},
	}
	for _, step := range r.Steps {
		st := renderStep(step, mentions, amounts)
		var names []string
		for _, seg := range st.Segments {
			if seg.Kind == SegmentIngredient {
				names = append(names, seg.Name)
			}
		}
		text := st.Text
		if st.Original != "" && text == "" {
			text = st.Original
		}
		st.Timers = stepTimers(text, names)
		out.Steps = append(out.Steps, st)
	}
	out.Checklist = buildChecklist(r, out)
	sort.SliceStable(out.Substitutions, func(i, j int) bool { return out.Substitutions[i].Name < out.Substitutions[j].Name })
	sort.SliceStable(out.Unchosen, func(i, j int) bool { return out.Unchosen[i].Name < out.Unchosen[j].Name })
	return out
}

// cookAmountAt is the line's amount for servings as the cooking screens show
// it. Meal-kit cards often leave their bigger boxes blank ("unit Red Onion"
// for 4), so when the recipe offers servings but this line has no amount
// there, it's scaled from the closest size that has one — a size that divides
// servings evenly first. The grocery list never scales (amountAt); this is
// only what to scoop.
func cookAmountAt(r Recipe, ing RecipeIngredient, servings int) (Measure, bool) {
	if m, ok := amountAt(ing, servings); ok {
		return m, true
	}
	if servings <= 0 || !slices.Contains(r.Servings, servings) {
		return Measure{}, false
	}
	from, fromMeasure := 0, Measure{}
	better := func(size int) bool {
		if from == 0 {
			return true
		}
		even, fromEven := servings%size == 0, servings%from == 0
		if even != fromEven {
			return even
		}
		return absInt(servings-size) < absInt(servings-from)
	}
	for _, a := range ing.Amounts {
		if a.Servings <= 0 || a.Servings == servings {
			continue
		}
		m, ok := amountAt(ing, a.Servings)
		if !ok || m.Quantity.IsZero() || !better(a.Servings) {
			continue
		}
		from, fromMeasure = a.Servings, m
	}
	if from == 0 {
		return Measure{}, false
	}
	fromMeasure.Quantity = fromMeasure.Quantity.MulRat(big.NewRat(int64(servings), int64(from)))
	return fromMeasure, true
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// scaled is m times factor, or nil.
func scaled(m *Measure, factor *big.Rat) *Measure {
	if m == nil {
		return nil
	}
	out := *m
	out.Quantity = m.Quantity.MulRat(factor)
	return &out
}

// cutIntoWedges reports whether a step quarters the citrus or cuts it into
// wedges ("Quarter lime", "Cut lemon into wedges").
func cutIntoWedges(steps []Step, name string) bool {
	citrus := wedgeCitrus(name)
	for _, st := range steps {
		lower := strings.ToLower(st.Text)
		if strings.Contains(lower, "quarter "+citrus) || strings.Contains(lower, citrus+" into wedges") ||
			strings.Contains(lower, citrus+"s into wedges") || strings.Contains(lower, citrus+" wedges") {
			return true
		}
	}
	return false
}

// smallest is the smallest serving size, the one a meal-kit card's own
// numbers are written for, or 0.
func smallest(sizes []int) int {
	out := 0
	for _, s := range sizes {
		if s > 0 && (out == 0 || s < out) {
			out = s
		}
	}
	return out
}

// statedMentions are the ingredients some step writes its own amount for.
func statedMentions(steps []Step, mentions []mention) map[int]bool {
	found := map[int]bool{}
	for _, step := range steps {
		renderStep(step, mentions, stepAmounts{stated: map[int]bool{}, found: found, notedBefore: map[string]bool{}})
	}
	return found
}

// componentOf describes a specialty ingredient the household makes from store
// ingredients, or nil when it buys it as is or hasn't chosen.
func componentOf(spec *grocery.Specialty, amount *Measure) *Component {
	choice := spec.Choice
	if choice == nil || (choice.Type != grocery.ChoiceStoreAlternative && choice.Type != grocery.ChoiceHouseMadeBatch) {
		return nil
	}
	// A store alternative that's just the bottle itself ("Toasted Sesame
	// Dressing" for Sesame Dressing) isn't made from anything: "Made with
	// Toasted Sesame Dressing" would only repeat the name.
	if choice.Type == grocery.ChoiceStoreAlternative && len(choice.Components) == 1 &&
		sameProduct(spec.Name, choice.Components[0].Name) {
		return nil
	}
	c := &Component{SpecialtyID: spec.ID, SpecialtyName: spec.Name, OptionName: choice.OptionName, Type: choice.Type}
	var ratio *big.Rat
	if choice.Type == grocery.ChoiceStoreAlternative {
		ratio = storeRatio(amount, spec, choice)
	}
	c.Parts = componentTexts(choice.Components, ratio)
	return c
}

// sameProduct reports whether product is the specialty under a longer name:
// every word of name appears in it ("Sesame Dressing" in "Toasted Sesame
// Dressing").
func sameProduct(name, product string) bool {
	have := strings.Fields(strings.ToLower(product))
	for _, w := range strings.Fields(strings.ToLower(name)) {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

// applySubstitute rewrites m to read as the household's chosen option, and
// returns what changed for the recipe-level summary.
func applySubstitute(m *mention, spec *grocery.Specialty) *Substitution {
	choice := spec.Choice
	source := ChoiceSourceHousehold
	if choice.Strategy != "" {
		source = ChoiceSourceStrategy
	}
	sub := &Substitution{OptionID: choice.OptionID, OptionName: choice.OptionName, Type: choice.Type, Source: source}
	switch choice.Type {
	case grocery.ChoiceStoreAlternative:
		ratio := storeRatio(m.amount, spec, choice)
		parts := componentTexts(choice.Components, ratio)
		sub.Text = spec.Name + " → " + joinList(parts)
		m.substituted = true
		if len(choice.Components) == 1 && ratio != nil {
			// One ingredient at a known amount replaces the packet outright,
			// so the step can simply name it.
			c := choice.Components[0]
			m.display = c.Name
			m.spicy = ingredients.Spicy(c.Name)
			m.amount = scaledComponent(c, ratio)
			return sub
		}
		// More than one ingredient, or an amount that doesn't convert: the
		// method changes, so the step keeps the original name and says what
		// to use instead rather than swapping a word silently. A packet reads
		// as what it holds ("2 Tbsp"), so the step has a real amount.
		if held := packetContents(m.amount, spec); held != nil {
			m.amount = held
		}
		m.note = "Instead of " + amountAndName(m.amount, spec.Name) + ", mix " + joinList(parts) + "."
		m.spicy = m.spicy || anySpicy(choice.Components)
	case grocery.ChoiceHouseMadeBatch:
		sub.Text = spec.Name + " → your house-made batch (" + choice.OptionName + ")"
		m.substituted = true
		converted, changed := batchAmount(m.amount, spec, choice)
		if changed {
			m.note = amountAndName(m.amount, spec.Name) + " is " + converted.Text() + " of your house-made " + spec.Name + "."
			m.amount = converted
		}
	default:
		return nil
	}
	return sub
}

// storeRatio is how many times the option's "per" amount this line needs, or
// nil when the line has no amount or the units don't convert exactly.
func storeRatio(amount *Measure, spec *grocery.Specialty, choice *grocery.Choice) *big.Rat {
	if amount == nil || amount.Quantity.IsZero() || choice.Per.Quantity.IsZero() {
		return nil
	}
	inPer, ok := grocery.ConvertMeasure(amount.Quantity.Rat(), amount.Unit, choice.Per.Unit, spec.UnitSizes)
	if !ok {
		return nil
	}
	return inPer.Quo(inPer, choice.Per.Quantity.Rat())
}

// batchAmount converts the line's amount into the batch's yield unit. changed
// is false when there is nothing to convert or no exact conversion, in which
// case the step keeps the recipe's own amount.
func batchAmount(amount *Measure, spec *grocery.Specialty, choice *grocery.Choice) (*Measure, bool) {
	if amount == nil || amount.Quantity.IsZero() || choice.Yield.Unit == "" || choice.Yield.Unit == amount.Unit {
		return amount, false
	}
	inYield, ok := grocery.ConvertMeasure(amount.Quantity.Rat(), amount.Unit, choice.Yield.Unit, spec.UnitSizes)
	if !ok {
		return amount, false
	}
	return &Measure{Quantity: quantityOf(inYield), Unit: choice.Yield.Unit}, true
}

func scaledComponent(c grocery.Component, ratio *big.Rat) *Measure {
	if c.Quantity == nil || c.Quantity.IsZero() || c.Unit == "" {
		return nil
	}
	q := c.Quantity.MulRat(ratio)
	// A bouillon base is counted a teaspoon per packet (decision 580), so
	// "3 tsp" stays three packets' worth rather than reading "1 Tbsp".
	return &Measure{Quantity: q, Unit: c.Unit, Exact: strings.Contains(strings.ToLower(c.Name), "bouillon")}
}

func quantityOf(r *big.Rat) ingredients.Quantity {
	return ingredients.NewQuantity(1, 1).MulRat(r)
}

// componentTexts renders an option's ingredients with their amounts, scaled by
// ratio when it is known.
func componentTexts(components []grocery.Component, ratio *big.Rat) []string {
	out := make([]string, 0, len(components))
	for _, c := range components {
		if ratio != nil {
			if m := scaledComponent(c, ratio); m != nil {
				out = append(out, m.Text()+" "+c.Name)
				continue
			}
		}
		out = append(out, c.Name)
	}
	return out
}

func anySpicy(components []grocery.Component) bool {
	for _, c := range components {
		if ingredients.Spicy(c.Name) {
			return true
		}
	}
	return false
}

// amountAndName is "1 tbsp Tex-Mex Paste", or just the name when there is no
// amount.
func amountAndName(m *Measure, name string) string {
	if m == nil {
		return name
	}
	return m.Text() + " " + name
}

// joinList joins parts as "A", "A and B", or "A, B, and C".
func joinList(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// nameForms returns the spellings of an ingredient to look for in step text,
// longest first: the name as written, without a parenthesis or a trailing
// clause, in singular and plural, plus the specialty ingredient's aliases.
// Nothing is guessed from the step text itself — a name the recipe doesn't
// list is never matched.
func nameForms(name string, spec *grocery.Specialty) []string {
	seeds := []string{name}
	if spec != nil {
		seeds = append(seeds, spec.Name)
		seeds = append(seeds, spec.Aliases...)
	}
	seen := map[string]bool{}
	var forms []string
	add := func(s string) {
		s = strings.Join(strings.Fields(s), " ")
		if len(s) < 3 || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		forms = append(forms, s)
	}
	// Cards call "Beef Stock Concentrate" just "stock concentrate" in the steps.
	if lower := strings.ToLower(name); strings.HasSuffix(lower, " stock concentrate") {
		seeds = append(seeds, "stock concentrate")
	}
	for _, seed := range seeds {
		for _, base := range append([]string{seed, trimQualifiers(seed)}, headForms(trimQualifiers(seed))...) {
			add(base)
			add(pluralize(base))
			add(singularize(base))
		}
	}
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
}

// descriptors are words a card drops when a step names the ingredient: "Red
// Onion" is "onion", "Ground Pork" is "pork", "Cooking Oil" is "oil", "Black
// Pepper" is "pepper". Colors that name a different vegetable ("green
// pepper", "green onion") aren't here.
var descriptors = map[string]bool{
	"red": true, "yellow": true, "white": true, "black": true, "long": true, "ground": true, "cooking": true,
	"fresh": true, "flour": true, "corn": true, "baby": true, "boneless": true, "skinless": true, "large": true,
	"jasmine": true, "basmati": true, "arborio": true, "roma": true, "persian": true, "english": true,
	"yukon": true, "gold": true, "vegetable": true, "canola": true,
	"small": true, "medium": true, "whole": true, "dried": true, "shredded": true, "grated": true, "minced": true,
	"sliced": true, "chopped": true, "diced": true, "crushed": true, "plain": true, "unsalted": true,
	"salted": true, "extra": true, "virgin": true, "light": true, "neutral": true,
}

// headForms are the shorter names a step uses for name: leading descriptors
// dropped one at a time ("Long Green Pepper" → "Green Pepper"), a trailing
// "Blend" ("Southwest Spice Blend" → "Southwest Spice"), and a protein's cut
// ("Chicken Breasts" → "Chicken").
func headForms(name string) []string {
	words := strings.Fields(name)
	var out []string
	if n := len(words); n > 1 && strings.EqualFold(words[n-1], "blend") {
		words = words[:n-1]
		out = append(out, strings.Join(words, " "))
	}
	for len(words) > 1 && descriptors[strings.ToLower(words[0])] {
		words = words[1:]
		out = append(out, strings.Join(words, " "))
	}
	// "Thinly slice chili" for Chili Pepper, "mince jalapeño" for Jalapeño
	// Pepper: a named chili drops the "pepper".
	if n := len(words); n > 1 && chiliNames[strings.ToLower(words[n-2])] &&
		(strings.EqualFold(words[n-1], "pepper") || strings.EqualFold(words[n-1], "peppers")) {
		out = append(out, strings.Join(words[:n-1], " "))
	}
	// "Pat chicken dry" for Chicken Breasts, "sear pork" for Pork Chops,
	// "add chicken" for Chicken Breast Strips (both cut words go).
	for n := len(words); n > 1 && cuts[strings.ToLower(words[n-1])]; n = len(words) {
		words = words[:n-1]
		out = append(out, strings.Join(words, " "))
	}
	// "chili sauce" for Sweet Thai Chili Sauce: a condiment keeps its last
	// two words.
	if n := len(words); n > 2 && condiments[strings.ToLower(words[n-1])] {
		out = append(out, strings.Join(words[n-2:], " "))
	}
	// "Parmesan" for Parmesan Cheese, "panko" for Panko Breadcrumbs,
	// "cavatappi" for Cavatappi Pasta: a step drops the kind of thing it is.
	if n := len(words); n > 1 && droppedTails[strings.ToLower(words[n-1])] {
		words = words[:n-1]
		out = append(out, strings.Join(words, " "))
	}
	// "bell pepper" for Green Bell Pepper: a color goes when two words stay.
	if len(words) > 2 && colors[strings.ToLower(words[0])] {
		words = words[1:]
		out = append(out, strings.Join(words, " "))
	}
	// "ponzu" for Ponzu Sauce, "hoisin" for Hoisin Sauce.
	if len(words) == 2 && strings.EqualFold(words[1], "sauce") && !sauceWordsInUse[strings.ToLower(words[0])] {
		out = append(out, words[0])
	}
	// "jam", "noodles", "vinegar", "steak": the kind of food is itself how a
	// step names it.
	if n := len(words); n > 1 && selfTails[strings.ToLower(words[n-1])] {
		out = append(out, words[n-1])
	}
	for _, tail := range []string{"noodles", "pasta", "jam"} {
		if lower := strings.ToLower(name); strings.HasSuffix(lower, " "+tail) && !slices.Contains(out, tail) {
			out = append(out, tail)
		}
	}
	// "baguette" for Demi-Baguette.
	if n := len(words); n == 1 && strings.Contains(words[0], "-") {
		parts := strings.Split(words[0], "-")
		out = append(out, parts[len(parts)-1])
	}
	// Names a card uses for the same thing: HelloFresh sends a long green
	// pepper for a poblano and calls it poblano in the steps.
	out = append(out, otherNames[strings.ToLower(name)]...)
	// "mushrooms" for Button Mushrooms, "couscous" for Israeli Couscous: the
	// last word is the food, unless it's a word every recipe uses for
	// something ("sauce", "oil", "pepper").
	if n := len(words); n > 1 {
		if last := words[n-1]; !genericTails[strings.ToLower(last)] {
			out = append(out, last)
		}
	}
	return out
}

// packagingName reports whether a name ends in the container it comes in.
func packagingName(name string) bool {
	words := strings.Fields(strings.ToLower(name))
	if len(words) < 2 {
		return false
	}
	switch words[len(words)-1] {
	case "cup", "can", "packet", "pack", "container", "jar", "tub",
		// Counted in packets: "1 Apricot Jam" is a packet, not "1 jam".
		"jam", "dressing", "crema", "glaze", "mayonnaise", "mustard", "honey", "syrup", "spread", "vinaigrette":
		return true
	}
	return false
}

// droppedTails are last words a step leaves off ("Parmesan", "panko").
var droppedTails = map[string]bool{
	"cheese": true, "pasta": true, "noodles": true, "breadcrumbs": true, "mix": true, "jam": true, "crumbles": true,
	"mustard": true,
	// Packaging: "Marinara Cup" is marinara.
	"cup": true, "cups": true, "can": true, "packet": true, "pack": true, "container": true, "jar": true, "tub": true,
}

// sauceWordsInUse are first words of a two-word sauce that mean something
// else on their own: "BBQ glaze", "a hot pan", "fish fillets".
var sauceWordsInUse = map[string]bool{
	"bbq": true, "barbecue": true, "hot": true, "fish": true, "chili": true, "cream": true, "pizza": true,
	"pasta": true, "steak": true, "garlic": true, "cheese": true, "tomato": true, "sweet": true, "spicy": true,
	"soy": false, "burger": true, "taco": true, "curry": true, "stir-fry": true, "teriyaki": false,
}

// selfTails are last words that name the ingredient on their own.
var selfTails = map[string]bool{"vinegar": true, "steak": true, "baguette": true, "rice": true, "tortillas": true}

// otherNames are names a card's steps use for an ingredient listed under
// another.
var otherNames = map[string][]string{"long green pepper": {"poblano", "poblanos", "poblano pepper"}}

// colors are first words a step drops when two words stay.
var colors = map[string]bool{"green": true, "red": true, "yellow": true, "orange": true}

// genericTails are last words too common to stand for one ingredient: "sauce"
// is also the dish's own sauce, "pepper" the black pepper.
var genericTails = map[string]bool{
	"sauce": true, "oil": true, "vinegar": true, "cheese": true, "powder": true, "seasoning": true, "spice": true,
	"blend": true, "paste": true, "mix": true, "concentrate": true, "stock": true, "juice": true, "water": true,
	"pepper": true, "peppers": true, "base": true, "dressing": true, "sugar": true, "salt": true, "flakes": true,
	"leaves": true, "crema": true, "glaze": true, "cream": true, "butter": true, "milk": true, "seeds": true,
	"broth": true, "wine": true, "rub": true, "herbs": true,
	// Parts, not foods: "Lobster Tails" isn't "tails", "Green Beans" isn't
	// "beans" (a stray "bean" elsewhere would take its amount).
	"tails": true, "tail": true, "fraîche": true, "fraiche": true, "florets": true, "halves": true,
	"wedges": true, "strips": true, "rings": true, "cubes": true, "ends": true, "tips": true, "sprigs": true,
	"stems": true, "beans": true, "mustard": true, "tops": true, "hearts": true, "chunks": true,
	// Units and packaging never stand for an ingredient.
	"cup": true, "cups": true, "can": true, "cans": true, "packet": true, "packets": true, "pack": true,
	"container": true, "jar": true, "bag": true, "bunch": true, "package": true, "tub": true, "oz": true,
	"slices": true, "slice": true, "pieces": true, "piece": true, "fillets": true, "fillet": true,
}

// condiments are last words of a name a step shortens to its last two
// ("chili sauce", "curry paste").
var condiments = map[string]bool{"sauce": true, "paste": true, "powder": true, "flakes": true, "vinegar": true}

// chiliNames are the words before "Pepper" that name a chili.
var chiliNames = map[string]bool{
	"chili": true, "chile": true, "jalapeño": true, "jalapeno": true, "serrano": true, "habanero": true,
	"poblano": true, "fresno": true, "thai": true, "cayenne": true,
}

// cuts are the last words of a protein a step drops.
var cuts = map[string]bool{
	"breast": true, "breasts": true, "thigh": true, "thighs": true, "cutlet": true, "cutlets": true,
	"chop": true, "chops": true, "tenderloin": true, "tenderloins": true, "fillet": true, "fillets": true,
	"filet": true, "filets": true, "loin": true, "tenders": true, "strips": true,
}

// dedupeForms gives a spelling two ingredients share to one of them: the one
// whose own name it is, else the one with the shorter name. With Black Pepper
// and Long Green Pepper, "pepper" in "salt and pepper" is the black pepper.
func dedupeForms(mentions []mention) {
	own := func(m mention, form string) bool {
		for _, n := range []string{m.name, trimQualifiers(m.name)} {
			for _, f := range []string{n, pluralize(n), singularize(n)} {
				if strings.EqualFold(f, form) {
					return true
				}
			}
		}
		return false
	}
	owner := map[string]int{}
	for i, m := range mentions {
		for _, f := range m.forms {
			k := strings.ToLower(f)
			j, seen := owner[k]
			if !seen {
				owner[k] = i
				continue
			}
			oi, oj := own(m, f), own(mentions[j], f)
			if oi && !oj || oi == oj && len(strings.Fields(m.name)) < len(strings.Fields(mentions[j].name)) {
				owner[k] = i
			}
		}
	}
	for i := range mentions {
		kept := mentions[i].forms[:0:0]
		for _, f := range mentions[i].forms {
			if owner[strings.ToLower(f)] == i {
				kept = append(kept, f)
			}
		}
		mentions[i].forms = kept
	}
}

// trimQualifiers drops a parenthesis or a trailing clause: "Chili Flakes
// (optional)" and "Scallions, thinly sliced" both become their head.
func trimQualifiers(name string) string {
	if i := strings.IndexByte(name, '('); i > 0 {
		name = name[:i]
	}
	if i := strings.IndexByte(name, ','); i > 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// pluralize and singularize change the last word only, with the rules that
// hold for ingredient names ("Scallion" ⇄ "Scallions", "Chili" ⇄ "Chilies",
// "Squash" ⇄ "Squashes").
func pluralize(name string) string {
	head, last := splitLastWord(name)
	lower := strings.ToLower(last)
	switch {
	case lower == "":
		return name
	case strings.HasSuffix(lower, "s"), strings.HasSuffix(lower, "x"), strings.HasSuffix(lower, "z"),
		strings.HasSuffix(lower, "ch"), strings.HasSuffix(lower, "sh"):
		return head + last + "es"
	case strings.HasSuffix(lower, "y") && len(lower) > 1 && !isVowel(rune(lower[len(lower)-2])):
		return head + last[:len(last)-1] + "ies"
	case strings.HasSuffix(lower, "o") && len(lower) > 1 && !isVowel(rune(lower[len(lower)-2])):
		return head + last + "es"
	}
	return head + last + "s"
}

func singularize(name string) string {
	head, last := splitLastWord(name)
	lower := strings.ToLower(last)
	switch {
	case strings.HasSuffix(lower, "ies") && len(lower) > 4:
		return head + last[:len(last)-3] + "y"
	case strings.HasSuffix(lower, "oes") && len(lower) > 4:
		return head + last[:len(last)-2]
	case strings.HasSuffix(lower, "ses"), strings.HasSuffix(lower, "xes"), strings.HasSuffix(lower, "zes"),
		strings.HasSuffix(lower, "ches"), strings.HasSuffix(lower, "shes"):
		return head + last[:len(last)-2]
	case strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") && len(lower) > 3:
		return head + last[:len(last)-1]
	}
	return name
}

func splitLastWord(name string) (head, last string) {
	if i := strings.LastIndexByte(name, ' '); i >= 0 {
		return name[:i+1], name[i+1:]
	}
	return "", name
}

func isVowel(r rune) bool { return strings.ContainsRune("aeiou", unicode.ToLower(r)) }
