package planning

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the planning HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
	// Outcomes reads the cooked and skipped answers the plan carries; nil
	// leaves them out.
	Outcomes EventLister
}

// Handler serves the week plan endpoints.
type Handler struct {
	opts   HandlerOptions
	logger *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(opts HandlerOptions) *Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{opts: opts, logger: logger}
}

// Mount registers the routes on r, which is expected to be the /api/v1 router.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		edit := households.RequirePermission(h.opts.Authorizer, households.PermPlanEdit, h.logger)
		const plans = "/households/{householdId}/plans"
		r.With(view).Get(plans, h.list)
		r.With(view).Get(plans+"/{week}", h.get)
		r.With(view).Get(plans+"/{week}/grocery", h.groceryList)
		r.With(edit).Post(plans+"/{week}/entries", h.addEntry)
		r.With(edit).Patch(plans+"/{week}/entries/{entryId}", h.updateEntry)
		r.With(edit).Delete(plans+"/{week}/entries/{entryId}", h.deleteEntry)
		r.With(edit).Put(plans+"/{week}/status", h.setStatus)
		r.With(view).Get("/households/{householdId}/thaw", h.thawDue)
	})
}

// --- Wire types ---------------------------------------------------------------

// PlanResponse is a household's plan for one week.
type PlanResponse struct {
	HouseholdID string          `json:"householdId"`
	Week        string          `json:"week"`
	StartDate   string          `json:"startDate"`
	EndDate     string          `json:"endDate"`
	Status      Status          `json:"status"`
	Entries     []EntryResponse `json:"entries"`
	// CreatedAt and UpdatedAt are null for a week nobody has planned.
	CreatedAt *time.Time `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// EntryResponse is a planned recipe.
type EntryResponse struct {
	ID     string              `json:"id"`
	Recipe EntryRecipeResponse `json:"recipe"`
	// Day and Date are null for an unscheduled entry.
	Day      *Day      `json:"day"`
	Date     *string   `json:"date"`
	Servings int       `json:"servings"`
	Note     string    `json:"note"`
	AddedBy  string    `json:"addedBy"`
	AddedAt  time.Time `json:"addedAt"`
	// Origin is manual, or autopilot for entries added by accepting an
	// Autopilot proposal.
	Origin Origin `json:"origin"`
	// Customizations are the meal's protein choices; omitted when none.
	Customizations []EntryCustomizationResponse `json:"customizations,omitempty"`
	// Outcome is the household's "did you make it?" answer, whoever gave it;
	// omitted until someone answers.
	Outcome *EntryOutcomeResponse `json:"outcome,omitempty"`
}

// EntryCustomizationResponse is one customized ingredient line of an entry.
type EntryCustomizationResponse struct {
	IngredientKey string `json:"ingredientKey"`
	ChoiceID      string `json:"choiceId"`
	Label         string `json:"label"`
}

// EntryRecipeResponse is the recipe snapshot stored on an entry.
type EntryRecipeResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// IsAddon is true for a pairing's add-on, which is planned with a meal and
	// never counted as one. Always sent, so a client never has to work it out
	// from the catalog.
	IsAddon  bool   `json:"isAddon"`
	ImageURL string `json:"imageUrl,omitempty"`
}

// AddEntryResponse is returned by POST .../plans/{week}/entries.
type AddEntryResponse struct {
	Entry EntryResponse `json:"entry"`
	Plan  PlanResponse  `json:"plan"`
}

// PlanSummaryResponse describes one week in a range.
type PlanSummaryResponse struct {
	Week       string     `json:"week"`
	StartDate  string     `json:"startDate"`
	Status     Status     `json:"status"`
	EntryCount int        `json:"entryCount"`
	UpdatedAt  *time.Time `json:"updatedAt"`
}

// PlanListResponse is returned by GET .../plans.
type PlanListResponse struct {
	Items []PlanSummaryResponse `json:"items"`
}

// GroceryListResponse is returned by GET .../plans/{week}/grocery.
type GroceryListResponse struct {
	Week   string `json:"week"`
	Status Status `json:"status"`
	// PantryApplied is true when the household pantry decided statuses.
	PantryApplied bool `json:"pantryApplied"`
	// SpecialtiesApplied is true when specialty ingredient choices were
	// applied.
	SpecialtiesApplied bool                      `json:"specialtiesApplied"`
	Categories         []GroceryCategoryResponse `json:"categories"`
	// Batches are the house-made specialty ingredients the week uses.
	Batches []GroceryBatchResponse `json:"batches"`
	Skipped []SkippedEntryResponse `json:"skipped"`
	// SkippedItems are the ingredients the household chose not to buy. They
	// are not in Categories, so nothing asks anyone to buy them, but they are
	// reported here rather than dropped: the recipes still need them.
	//
	// Not to be confused with Skipped, which is about plan ENTRIES that could
	// not contribute at all.
	SkippedItems []GroceryItemResponse `json:"skippedItems"`
	// Meals are the week's planned recipes in plan order, each once. Every
	// item's shares name one of them, so a client can show the list meal by
	// meal without deriving anything.
	Meals []GroceryMealResponse `json:"meals"`
}

// GroceryMealResponse is one planned recipe the list can be grouped under.
type GroceryMealResponse struct {
	RecipeID   string  `json:"recipeId"`
	RecipeName string  `json:"recipeName"`
	ImageURL   *string `json:"imageUrl"`
	IsAddon    bool    `json:"isAddon"`
	// Day is the first day the recipe is planned on, null when unscheduled.
	Day *Day `json:"day"`
}

// GroceryShareResponse is the part of an item one meal needs.
type GroceryShareResponse struct {
	RecipeID   string                  `json:"recipeId"`
	RecipeName string                  `json:"recipeName"`
	Amounts    []GroceryAmountResponse `json:"amounts"`
	// QuantityText is this meal's amount for display, "" when it has none.
	QuantityText string `json:"quantityText"`
	Unquantified bool   `json:"unquantified"`
	// Combined is true when the amount was made for several meals together (a
	// house-made batch), so none is split out for this one.
	Combined bool `json:"combined"`
	// Extra is true for a line added for this meal directly (a pairing).
	Extra bool `json:"extra"`
	// Component is set when the share is part of a component of the meal: the
	// store ingredients a specialty ingredient (a sauce, crema, paste, or
	// blend) became. Leaving the component out of the meal leaves out every
	// share that names it.
	Component *GroceryComponentResponse `json:"component"`
}

// GroceryComponentResponse names a component of a meal.
type GroceryComponentResponse struct {
	SpecialtyID   string `json:"specialtyId"`
	SpecialtyKey  string `json:"specialtyKey"`
	SpecialtyName string `json:"specialtyName"`
	// IngredientKey is the recipe line the component replaced: the key to
	// skip to leave the whole component out. Empty for a batch.
	IngredientKey string `json:"ingredientKey"`
}

// GrocerySpecialtyResponse describes an item that is itself a specialty
// ingredient.
type GrocerySpecialtyResponse struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	// ChoiceType is null when the household hasn't chosen, as_is, or
	// house_made_batch when a batch in the pantry covers the week.
	ChoiceType *grocery.ChoiceType `json:"choiceType"`
	OptionID   *string             `json:"optionId"`
	HouseMade  bool                `json:"houseMade"`
	// SuggestedOptions are offered when the household hasn't chosen.
	SuggestedOptions []GroceryOptionRefResponse `json:"suggestedOptions"`
	Text             string                     `json:"text"`
}

// GroceryOptionRefResponse names an option to suggest.
type GroceryOptionRefResponse struct {
	ID        string             `json:"id"`
	Type      grocery.ChoiceType `json:"type"`
	Name      string             `json:"name"`
	IsDefault bool               `json:"isDefault"`
}

// GroceryViaResponse is an item's provenance when it stands in for a
// specialty ingredient.
type GroceryViaResponse struct {
	Kind          grocery.ViaKind `json:"kind"`
	SpecialtyID   string          `json:"specialtyId"`
	SpecialtyKey  string          `json:"specialtyKey"`
	SpecialtyName string          `json:"specialtyName"`
	OptionID      string          `json:"optionId"`
	OptionName    string          `json:"optionName"`
	// Strategy is the household strategy that picked the option ("similar" or
	// "closest"), empty when a member chose it explicitly.
	Strategy string `json:"strategy"`
	// Yield and Batches are set for house_made_batch.
	Yield   *GroceryAmountResponse  `json:"yield"`
	Batches *int                    `json:"batches"`
	Recipes []GroceryRecipeResponse `json:"recipes"`
	Text    string                  `json:"text"`
}

// GroceryBatchResponse is a house-made specialty ingredient the week uses.
type GroceryBatchResponse struct {
	SpecialtyID   string                `json:"specialtyId"`
	SpecialtyKey  string                `json:"specialtyKey"`
	SpecialtyName string                `json:"specialtyName"`
	OptionID      string                `json:"optionId"`
	OptionName    string                `json:"optionName"`
	Yield         GroceryAmountResponse `json:"yield"`
	// Status is inPantry or make; Reason explains it.
	Status       grocery.BatchStatus     `json:"status"`
	Reason       grocery.BatchReason     `json:"reason"`
	Batches      int                     `json:"batches"`
	PantryItemID *string                 `json:"pantryItemId"`
	Remaining    *GroceryAmountResponse  `json:"remaining"`
	Needed       *GroceryAmountResponse  `json:"needed"`
	Recipes      []GroceryRecipeResponse `json:"recipes"`
	Text         string                  `json:"text"`
}

// GroceryCategoryResponse is one aisle.
type GroceryCategoryResponse struct {
	Category string                `json:"category"`
	Items    []GroceryItemResponse `json:"items"`
}

// GroceryItemResponse is one aggregated ingredient.
type GroceryItemResponse struct {
	IngredientKey string                  `json:"ingredientKey"`
	Name          string                  `json:"name"`
	Amounts       []GroceryAmountResponse `json:"amounts"`
	// QuantityText joins the amounts for display ("1 onion + 8 oz").
	QuantityText string `json:"quantityText"`
	// OnHandText is set when the pantry has some but not enough, and the
	// amounts are the rest to buy: "12 oz at home".
	OnHandText   string                  `json:"onHandText,omitempty"`
	Unquantified bool                    `json:"unquantified"`
	Status       grocery.Status          `json:"status"`
	Recipes      []GroceryRecipeResponse `json:"recipes"`
	// Specialty is true when the item is a specialty ingredient by its own
	// name; SpecialtyDetail says more.
	Specialty       bool                      `json:"specialty"`
	SpecialtyDetail *GrocerySpecialtyResponse `json:"specialtyDetail"`
	// Via lists the specialty ingredients the item stands in for.
	Via []GroceryViaResponse `json:"via"`
	// Extras are items added to the week directly, such as an Autopilot
	// pairing ("Club crackers for Chicken Noodle Soup").
	Extras []GroceryExtraResponse `json:"extras"`
	// Shares split the item by meal, in the order of recipes: how much each
	// meal needs, and whether it is for a component of that meal.
	Shares []GroceryShareResponse `json:"shares"`
	// SkipScope is week, always, or recipe on an item in SkippedItems, and
	// absent on every item that is actually on the list. An item skipped with
	// recipe holds only the left-out meals' share; the same ingredient can
	// also be on the list for other meals.
	SkipScope grocery.SkipScope `json:"skipScope,omitempty"`
	// SkipText says what the skip does, in the words the app shows. Absent
	// unless the item is skipped.
	SkipText string `json:"skipText,omitempty"`
}

// GroceryExtraResponse is an item added to the week directly.
type GroceryExtraResponse struct {
	ID string `json:"id"`
	// Origin is pairing: an accepted Autopilot pairing.
	Origin string `json:"origin"`
	Text   string `json:"text"`
}

// GroceryAmountResponse is a combined amount in one unit.
type GroceryAmountResponse struct {
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
	Unit          string  `json:"unit"`
	Text          string  `json:"text"`
}

// GroceryRecipeResponse is a recipe that needs the item.
type GroceryRecipeResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SkippedEntryResponse is an entry left out of the grocery list.
type SkippedEntryResponse struct {
	EntryID    string     `json:"entryId"`
	RecipeID   string     `json:"recipeId"`
	RecipeName string     `json:"recipeName"`
	Reason     SkipReason `json:"reason"`
}

type addEntryRequest struct {
	RecipeID string  `json:"recipeId"`
	Day      *string `json:"day"`
	Servings int     `json:"servings"`
	Note     string  `json:"note"`
}

type updateEntryRequest struct {
	// Day is raw so an explicit null (unschedule) differs from an absent field.
	Day      json.RawMessage `json:"day"`
	Servings *int            `json:"servings"`
	Note     *string         `json:"note"`
}

type setStatusRequest struct {
	Status string `json:"status"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// NewPlanResponse returns the wire form of a plan, for other modules that
// return plans (accepting an Autopilot proposal).
func NewPlanResponse(p Plan) PlanResponse { return newPlanResponse(p) }

// NewEntryResponse returns the wire form of an entry of plan p, whose week and
// first day decide the entry's date.
func NewEntryResponse(p Plan, e Entry) EntryResponse { return newEntryResponse(p, e) }

func newPlanResponse(p Plan) PlanResponse {
	resp := PlanResponse{
		HouseholdID: p.HouseholdID, Week: p.Week.String(),
		StartDate: p.Week.StartDateOn(p.First()), EndDate: p.Week.EndDateOn(p.First()),
		Status: p.Status, Entries: make([]EntryResponse, 0, len(p.Entries)),
		CreatedAt: timePtr(p.CreatedAt), UpdatedAt: timePtr(p.UpdatedAt),
	}
	for _, e := range p.Entries {
		resp.Entries = append(resp.Entries, newEntryResponse(p, e))
	}
	return resp
}

func newEntryResponse(p Plan, e Entry) EntryResponse {
	resp := EntryResponse{
		ID: e.ID,
		Recipe: EntryRecipeResponse{
			ID: e.RecipeID, Name: e.RecipeName, IsAddon: e.RecipeIsAddon, ImageURL: e.RecipeImageURL,
		},
		Servings: e.Servings, Note: e.Note, AddedBy: e.AddedBy, AddedAt: e.AddedAt.UTC(), Origin: e.Origin,
	}
	if resp.Origin == "" {
		resp.Origin = OriginManual
	}
	for _, c := range e.Customizations {
		resp.Customizations = append(resp.Customizations, EntryCustomizationResponse(c))
	}
	if e.Day != "" {
		day, date := e.Day, p.DateOf(e.Day)
		resp.Day, resp.Date = &day, &date
	}
	return resp
}

func newGroceryListResponse(g GroceryList) GroceryListResponse {
	resp := GroceryListResponse{
		Week: g.Week.String(), Status: g.Status, PantryApplied: g.PantryApplied, SpecialtiesApplied: g.SpecialtiesApplied,
		Categories:   make([]GroceryCategoryResponse, 0, len(g.Categories)),
		Batches:      make([]GroceryBatchResponse, 0, len(g.Batches)),
		Skipped:      make([]SkippedEntryResponse, 0, len(g.Skipped)),
		SkippedItems: make([]GroceryItemResponse, 0, len(g.SkippedItems)),
		Meals:        GroceryMeals(g.Meals),
	}
	for _, b := range g.Batches {
		resp.Batches = append(resp.Batches, newGroceryBatchResponse(b))
	}
	for _, c := range g.Categories {
		cr := GroceryCategoryResponse{Category: c.Category, Items: make([]GroceryItemResponse, 0, len(c.Items))}
		for _, item := range c.Items {
			cr.Items = append(cr.Items, newGroceryItemResponse(item))
		}
		resp.Categories = append(resp.Categories, cr)
	}
	for _, item := range g.SkippedItems {
		resp.SkippedItems = append(resp.SkippedItems, newGroceryItemResponse(item))
	}
	for _, s := range g.Skipped {
		resp.Skipped = append(resp.Skipped, SkippedEntryResponse(s))
	}
	return resp
}

// newGroceryItemResponse renders one aggregated item. Skipped items go through
// it too, so the review screen sees the same amounts, recipes, and provenance
// as the list would have shown.
func newGroceryItemResponse(item grocery.Item) GroceryItemResponse {
	ir := GroceryItemResponse{
		IngredientKey: item.IngredientKey, Name: item.Name, Unquantified: item.Unquantified, Status: item.Status,
		Amounts: make([]GroceryAmountResponse, 0, len(item.Amounts)),
		Recipes: make([]GroceryRecipeResponse, 0, len(item.Sources)),
	}
	if item.OnHand != nil {
		ir.OnHandText = amountText(*item.OnHand) + " at home"
	}
	texts := make([]string, 0, len(item.Amounts))
	for _, a := range item.Amounts {
		text := amountText(a)
		texts = append(texts, text)
		ir.Amounts = append(ir.Amounts, GroceryAmountResponse{
			Quantity: a.Quantity.String(), QuantityValue: a.Quantity.Float64(), Unit: a.Unit.Code, Text: text,
		})
	}
	ir.QuantityText = strings.Join(texts, " + ")
	for _, src := range item.Sources {
		ir.Recipes = append(ir.Recipes, GroceryRecipeResponse{ID: src.RecipeID, Name: src.RecipeName})
	}
	ir.Shares = GroceryShares(item.Shares)
	ir.Via = make([]GroceryViaResponse, 0, len(item.Via))
	for _, v := range item.Via {
		ir.Via = append(ir.Via, newGroceryViaResponse(v))
	}
	ir.Extras = make([]GroceryExtraResponse, 0, len(item.Extras))
	for _, e := range item.Extras {
		ir.Extras = append(ir.Extras, GroceryExtraResponse(e))
	}
	if s := item.Specialty; s != nil {
		ir.Specialty, ir.SpecialtyDetail = true, newGrocerySpecialtyResponse(*s)
	}
	if item.SkipScope != "" {
		ir.SkipScope, ir.SkipText = item.SkipScope, skipText(item)
	}
	return ir
}

// GroceryMeals renders the week's meals, for the Shop tab's proposal too.
func GroceryMeals(meals []GroceryMeal) []GroceryMealResponse {
	out := make([]GroceryMealResponse, 0, len(meals))
	for _, m := range meals {
		mr := GroceryMealResponse{RecipeID: m.RecipeID, RecipeName: m.RecipeName, IsAddon: m.IsAddon}
		if m.ImageURL != "" {
			image := m.ImageURL
			mr.ImageURL = &image
		}
		if m.Day != "" {
			day := m.Day
			mr.Day = &day
		}
		out = append(out, mr)
	}
	return out
}

// GroceryShares renders an item's per-meal shares, for the Shop tab's lines
// too.
func GroceryShares(shares []grocery.Share) []GroceryShareResponse {
	out := make([]GroceryShareResponse, 0, len(shares))
	for _, sh := range shares {
		sr := GroceryShareResponse{
			RecipeID: sh.RecipeID, RecipeName: sh.RecipeName, Unquantified: sh.Unquantified,
			Combined: sh.Combined, Extra: sh.Extra, Amounts: make([]GroceryAmountResponse, 0, len(sh.Amounts)),
		}
		texts := make([]string, 0, len(sh.Amounts))
		for _, a := range sh.Amounts {
			text := amountText(a)
			texts = append(texts, text)
			sr.Amounts = append(sr.Amounts, GroceryAmountResponse{
				Quantity: a.Quantity.String(), QuantityValue: a.Quantity.Float64(), Unit: a.Unit.Code, Text: text,
			})
		}
		sr.QuantityText = strings.Join(texts, " + ")
		if c := sh.Component; c != nil {
			sr.Component = &GroceryComponentResponse{
				SpecialtyID: c.SpecialtyID, SpecialtyKey: c.SpecialtyKey, SpecialtyName: c.SpecialtyName, IngredientKey: c.LineKey,
			}
		}
		out = append(out, sr)
	}
	return out
}

// skipText is the app's wording for why an item is held back. The skips
// package says the same thing about its own records; this is the grocery
// list's copy, so planning doesn't depend on that package for a few strings.
func skipText(item grocery.Item) string {
	switch item.SkipScope {
	case grocery.SkipAlways:
		return "Never buying this"
	case grocery.SkipRecipe:
		names := make([]string, 0, len(item.Sources))
		for _, s := range item.Sources {
			names = append(names, s.RecipeName)
		}
		if len(names) == 0 {
			return "Left out of one recipe"
		}
		return "Left out of " + strings.Join(names, ", ")
	}
	return "Skipped this week"
}

// amountText renders "1 ½ cups", "2 cloves", or "3" for counts.
func amountText(a grocery.Amount) string {
	text := a.Quantity.Format()
	if label := a.Unit.Label(a.Quantity); label != "" {
		text += " " + label
	}
	return text
}

func measureResponse(m grocery.Measure) GroceryAmountResponse {
	unit, _ := ingredients.LookupUnit(m.Unit)
	return GroceryAmountResponse{
		Quantity: m.Quantity.String(), QuantityValue: m.Quantity.Float64(), Unit: m.Unit,
		Text: amountText(grocery.Amount{Quantity: m.Quantity, Unit: unit}),
	}
}

func optionalMeasure(m *grocery.Measure) *GroceryAmountResponse {
	if m == nil {
		return nil
	}
	r := measureResponse(*m)
	return &r
}

func recipeResponses(sources []grocery.Source) []GroceryRecipeResponse {
	out := make([]GroceryRecipeResponse, 0, len(sources))
	for _, s := range sources {
		out = append(out, GroceryRecipeResponse{ID: s.RecipeID, Name: s.RecipeName})
	}
	return out
}

// joinNames joins names as "A", "A and B", or "A, B, and C".
func joinNames(sources []grocery.Source) string {
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.RecipeName)
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// customizedViaText is "2x Ground Pork in A" for a doubled line, or
// "Ground Beef instead of Ground Pork in A" for a swap.
func customizedViaText(v grocery.ItemVia) string {
	text := v.OptionName
	if strings.HasPrefix(v.OptionID, "swap:") {
		text += " instead of " + v.SpecialtyName
	}
	if names := joinNames(v.Recipes); names != "" {
		text += " in " + names
	}
	return text
}

func newGroceryViaResponse(v grocery.ItemVia) GroceryViaResponse {
	resp := GroceryViaResponse{
		Kind: v.Kind, SpecialtyID: v.SpecialtyID, SpecialtyKey: v.SpecialtyKey, SpecialtyName: v.SpecialtyName,
		OptionID: v.OptionID, OptionName: v.OptionName, Strategy: v.Strategy,
		Yield: optionalMeasure(v.Yield), Recipes: recipeResponses(v.Recipes),
	}
	switch v.Kind {
	case grocery.ViaStoreAlternative:
		resp.Text = "for " + v.SpecialtyName
		if names := joinNames(v.Recipes); names != "" {
			resp.Text += " in " + names
		}
		if v.Strategy != "" {
			resp.Text = "Store alternative " + resp.Text + " (your default)"
		}
	case grocery.ViaCustomized:
		resp.Text = customizedViaText(v)
	case grocery.ViaHouseMadeBatch:
		batches := v.Batches
		resp.Batches = &batches
		yield := ""
		if resp.Yield != nil {
			yield = resp.Yield.Text
		}
		if batches > 1 {
			resp.Text = fmt.Sprintf("to make %d batches of %s (each makes about %s)", batches, v.SpecialtyName, yield)
		} else {
			resp.Text = fmt.Sprintf("to make %s (makes about %s)", v.SpecialtyName, yield)
		}
		if v.Strategy != "" {
			resp.Text = "House-made batch " + resp.Text + " (your default)"
		}
	}
	return resp
}

func newGrocerySpecialtyResponse(s grocery.LineSpecialty) *GrocerySpecialtyResponse {
	resp := &GrocerySpecialtyResponse{
		ID: s.ID, Key: s.Key, Name: s.Name, HouseMade: s.HouseMade,
		SuggestedOptions: make([]GroceryOptionRefResponse, 0, len(s.Suggestions)),
	}
	if s.ChoiceType != "" {
		choice := s.ChoiceType
		resp.ChoiceType = &choice
	}
	if s.OptionID != "" {
		id := s.OptionID
		resp.OptionID = &id
	}
	for _, o := range s.Suggestions {
		resp.SuggestedOptions = append(resp.SuggestedOptions, GroceryOptionRefResponse(o))
	}
	switch {
	case s.HouseMade:
		resp.Text = "In pantry (house-made)"
	case s.ChoiceType == grocery.ChoiceAsIs:
		resp.Text = "Specialty ingredient, bought as is"
	default:
		resp.Text = "Specialty ingredient: choose a store alternative or a house-made batch"
	}
	return resp
}

func newGroceryBatchResponse(b grocery.BatchPlan) GroceryBatchResponse {
	resp := GroceryBatchResponse{
		SpecialtyID: b.SpecialtyID, SpecialtyKey: b.SpecialtyKey, SpecialtyName: b.SpecialtyName,
		OptionID: b.OptionID, OptionName: b.OptionName, Yield: measureResponse(b.Yield),
		Status: b.Status, Reason: b.Reason, Batches: b.Batches,
		Remaining: optionalMeasure(b.Remaining), Needed: optionalMeasure(b.Needed), Recipes: recipeResponses(b.Recipes),
	}
	if b.ItemID != "" {
		id := b.ItemID
		resp.PantryItemID = &id
	}
	switch {
	case b.Status == grocery.BatchInPantry:
		resp.Text = "In pantry (house-made)"
	case b.Batches > 1:
		resp.Text = fmt.Sprintf("Make %d batches (each makes about %s)", b.Batches, resp.Yield.Text)
	default:
		resp.Text = fmt.Sprintf("Make a batch (makes about %s)", resp.Yield.Text)
	}
	return resp
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	q := r.URL.Query()
	items, err := h.opts.Service.List(r.Context(), actor.HouseholdID, q.Get("from"), q.Get("to"))
	if err != nil {
		h.writeError(w, r, "list plans failed", err)
		return
	}
	first, err := h.opts.Service.FirstDay(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "list plans failed", err)
		return
	}
	resp := PlanListResponse{Items: make([]PlanSummaryResponse, 0, len(items))}
	for _, s := range items {
		resp.Items = append(resp.Items, PlanSummaryResponse{
			Week: s.Week.String(), StartDate: s.Week.StartDateOn(first), Status: s.Status,
			EntryCount: s.EntryCount, UpdatedAt: timePtr(s.UpdatedAt),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	p, err := h.opts.Service.Get(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"))
	if err != nil {
		h.writeError(w, r, "get plan failed", err)
		return
	}
	resp := newPlanResponse(p)
	h.withOutcomes(r.Context(), actor.HouseholdID, &resp)
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) groceryList(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	g, err := h.opts.Service.GroceryList(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"))
	if err != nil {
		h.writeError(w, r, "build grocery list failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newGroceryListResponse(g))
}

func (h *Handler) addEntry(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req addEntryRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := NewEntry{RecipeID: req.RecipeID, Servings: req.Servings, Note: req.Note}
	if req.Day != nil {
		if *req.Day == "" {
			validationFailed(w, r, "day must be one of mon, tue, wed, thu, fri, sat, sun, or null")
			return
		}
		in.Day = *req.Day
	}
	p, e, err := h.opts.Service.AddEntry(r.Context(), actor.HouseholdID, actor.UserID, chi.URLParam(r, "week"), in)
	if err != nil {
		h.writeError(w, r, "add plan entry failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, AddEntryResponse{Entry: newEntryResponse(p, e), Plan: newPlanResponse(p)})
}

func (h *Handler) updateEntry(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req updateEntryRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	changes := EntryChanges{Servings: req.Servings, Note: req.Note}
	if len(req.Day) > 0 {
		var day *string
		if err := json.Unmarshal(req.Day, &day); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", `field "day" has the wrong type`)
			return
		}
		d := Day("")
		if day != nil {
			if *day == "" {
				validationFailed(w, r, "day must be one of mon, tue, wed, thu, fri, sat, sun, or null")
				return
			}
			d = Day(*day)
		}
		changes.Day = &d
	}
	p, err := h.opts.Service.UpdateEntry(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"), chi.URLParam(r, "entryId"), changes)
	if err != nil {
		h.writeError(w, r, "update plan entry failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newPlanResponse(p))
}

func (h *Handler) deleteEntry(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if _, err := h.opts.Service.DeleteEntry(r.Context(), actor.HouseholdID, actor.UserID, chi.URLParam(r, "week"), chi.URLParam(r, "entryId")); err != nil {
		h.writeError(w, r, "delete plan entry failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req setStatusRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	p, err := h.opts.Service.SetStatus(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"), req.Status)
	if err != nil {
		h.writeError(w, r, "set plan status failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newPlanResponse(p))
}

// writeError maps service errors to responses.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, ErrInvalidWeek), errors.Is(err, ErrInvalidRange), errors.Is(err, ErrInvalidEntry),
		errors.Is(err, ErrInvalidStatus), errors.Is(err, ErrRecipeNotFound):
		validationFailed(w, r, publicMessage(err))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "plan entry not found")
	case errors.Is(err, ErrFinalized):
		httpx.WriteError(w, r, http.StatusConflict, "plan_finalized", "the plan is finalized; set its status to draft to change entries")
	case errors.Is(err, ErrPlanFull):
		httpx.WriteError(w, r, http.StatusConflict, "plan_full", fmt.Sprintf("a week holds at most %d entries", MaxEntriesPerWeek))
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}

func validationFailed(w http.ResponseWriter, r *http.Request, message string) {
	httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", message)
}

// publicMessage strips package prefixes from sentinel-wrapped errors whose
// details are safe to show clients.
func publicMessage(err error) string {
	return strings.ReplaceAll(err.Error(), "planning: ", "")
}
