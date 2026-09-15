package recommendations

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the Autopilot HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the Autopilot endpoints.
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

// Mount registers the routes on r, the /api/v1 router. Reading needs
// household.view; changing preferences and proposals needs plan.edit, like
// the planner.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		edit := households.RequirePermission(h.opts.Authorizer, households.PermPlanEdit, h.logger)
		const base = "/households/{householdId}/autopilot"
		r.With(view).Get(base+"/profile", h.getProfile)
		r.With(edit).Put(base+"/profile", h.replaceProfile)
		r.With(edit).Patch(base+"/profile", h.patchProfile)
		r.With(view).Get(base+"/profile/history", h.history)
		r.With(view).Get(base+"/vocabulary", h.vocabulary)
		r.With(view).Get(base+"/recipe-overrides", h.listOverrides)
		r.With(view).Get(base+"/recipes/{recipeId}/attributes", h.attributes)
		r.With(edit).Put(base+"/recipes/{recipeId}/override", h.setOverride)
		r.With(view).Get(base+"/weeks/{week}/context", h.getContext)
		r.With(edit).Put(base+"/weeks/{week}/context", h.putContext)
		r.With(edit).Delete(base+"/weeks/{week}/context", h.deleteContext)
		r.With(edit).Post(base+"/weeks/{week}/generate", h.generate)
		r.With(view).Get(base+"/weeks/{week}/proposal", h.getProposal)
		r.With(edit).Post(base+"/weeks/{week}/proposal/slots/{slotId}/swap", h.swap)
		r.With(edit).Post(base+"/weeks/{week}/proposal/accept", h.accept)
		r.With(edit).Post(base+"/weeks/{week}/proposal/reject", h.reject)
	})
}

// --- wire types -------------------------------------------------------------------

// ChoicesJSON is a set of cuisines, tags, and proteins.
type ChoicesJSON struct {
	Cuisines []string `json:"cuisines"`
	Tags     []string `json:"tags"`
	Proteins []string `json:"proteins"`
}

// TasteJSON is the taste section.
type TasteJSON struct {
	Likes    ChoicesJSON `json:"likes"`
	Dislikes ChoicesJSON `json:"dislikes"`
}

// RestrictionsJSON is the restrictions section.
type RestrictionsJSON struct {
	Diets               []string `json:"diets"`
	Allergens           []string `json:"allergens"`
	ExcludedIngredients []string `json:"excludedIngredients"`
	ExcludedCuisines    []string `json:"excludedCuisines"`
	ExcludedProteins    []string `json:"excludedProteins"`
	ExcludedTags        []string `json:"excludedTags"`
	NoSpicy             bool     `json:"noSpicy"`
}

// ScheduleJSON is the schedule section.
type ScheduleJSON struct {
	PlanDays     []string `json:"planDays"`
	Weeknights   []string `json:"weeknights"`
	MealsPerWeek int      `json:"mealsPerWeek"`
	// DefaultServings is null to use the household's default servings.
	DefaultServings     *int `json:"defaultServings"`
	WeeknightMaxMinutes *int `json:"weeknightMaxMinutes"`
}

// CookTimeJSON is the cookTime section.
type CookTimeJSON struct {
	QuickMaxMinutes      int  `json:"quickMaxMinutes"`
	MediumMaxMinutes     int  `json:"mediumMaxMinutes"`
	MaxLongPerWeek       int  `json:"maxLongPerWeek"`
	MinQuickPerWeek      int  `json:"minQuickPerWeek"`
	AvoidConsecutiveLong bool `json:"avoidConsecutiveLong"`
}

// WeekdayRuleJSON is a weekday rule.
type WeekdayRuleJSON struct {
	Day       string   `json:"day"`
	Label     string   `json:"label"`
	Cuisines  []string `json:"cuisines"`
	Tags      []string `json:"tags"`
	Proteins  []string `json:"proteins"`
	Methods   []string `json:"methods"`
	TimeBand  *string  `json:"timeBand"`
	Frequency string   `json:"frequency"`
}

// ChangeJSON says who last changed something and when.
type ChangeJSON struct {
	UpdatedBy string    `json:"updatedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProfileResponse is the household's Autopilot profile.
type ProfileResponse struct {
	HouseholdID  string            `json:"householdId"`
	Configured   bool              `json:"configured"`
	Taste        TasteJSON         `json:"taste"`
	Restrictions RestrictionsJSON  `json:"restrictions"`
	Schedule     ScheduleJSON      `json:"schedule"`
	CookTime     CookTimeJSON      `json:"cookTime"`
	Novelty      string            `json:"novelty"`
	Equipment    []string          `json:"equipment"`
	WeekdayRules []WeekdayRuleJSON `json:"weekdayRules"`
	// Sections has every section; a section never set is null.
	Sections  map[string]*ChangeJSON `json:"sections"`
	Effective EffectiveJSON          `json:"effective"`
	CreatedBy *string                `json:"createdBy"`
	CreatedAt *time.Time             `json:"createdAt"`
	UpdatedBy *string                `json:"updatedBy"`
	UpdatedAt *time.Time             `json:"updatedAt"`
}

// EffectiveJSON holds values resolved from the household.
type EffectiveJSON struct {
	DefaultServings int `json:"defaultServings"`
}

type profileRequest struct {
	Taste        *TasteJSON         `json:"taste"`
	Restrictions *RestrictionsJSON  `json:"restrictions"`
	Schedule     *ScheduleJSON      `json:"schedule"`
	CookTime     *CookTimeJSON      `json:"cookTime"`
	Novelty      *string            `json:"novelty"`
	Equipment    *[]string          `json:"equipment"`
	WeekdayRules *[]WeekdayRuleJSON `json:"weekdayRules"`
}

// DayOverrideJSON is a day's context.
type DayOverrideJSON struct {
	Day        string `json:"day"`
	Skip       bool   `json:"skip"`
	MaxMinutes *int   `json:"maxMinutes"`
	Servings   *int   `json:"servings"`
}

// WeekContextResponse is a week's context.
type WeekContextResponse struct {
	HouseholdID  string            `json:"householdId"`
	Week         string            `json:"week"`
	StartDate    string            `json:"startDate"`
	EndDate      string            `json:"endDate"`
	Configured   bool              `json:"configured"`
	Skip         bool              `json:"skip"`
	Busy         bool              `json:"busy"`
	MealsPerWeek *int              `json:"mealsPerWeek"`
	MaxMinutes   *int              `json:"maxMinutes"`
	Servings     *int              `json:"servings"`
	Days         []DayOverrideJSON `json:"days"`
	Note         string            `json:"note"`
	UpdatedBy    *string           `json:"updatedBy"`
	UpdatedAt    *time.Time        `json:"updatedAt"`
}

type weekContextRequest struct {
	Skip         bool              `json:"skip"`
	Busy         bool              `json:"busy"`
	MealsPerWeek *int              `json:"mealsPerWeek"`
	MaxMinutes   *int              `json:"maxMinutes"`
	Servings     *int              `json:"servings"`
	Days         []DayOverrideJSON `json:"days"`
	Note         string            `json:"note"`
}

// TextJSON is a coded, human-readable text.
type TextJSON struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// ProposalRecipeJSON is the recipe snapshot on a slot.
type ProposalRecipeJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl,omitempty"`
}

// SlotJSON is a proposed meal.
type SlotJSON struct {
	ID          string             `json:"id"`
	Day         string             `json:"day"`
	Date        string             `json:"date"`
	Recipe      ProposalRecipeJSON `json:"recipe"`
	Servings    int                `json:"servings"`
	CookMinutes *int               `json:"cookMinutes"`
	TimeBand    string             `json:"timeBand"`
	Score       float64            `json:"score"`
	Signals     map[string]float64 `json:"signals"`
	Reasons     []TextJSON         `json:"reasons"`
	SwapCount   int                `json:"swapCount"`
}

// UnfilledJSON is a day the proposal couldn't fill.
type UnfilledJSON struct {
	Day  string `json:"day"`
	Date string `json:"date"`
	Code string `json:"code"`
	Text string `json:"text"`
}

// ObjectiveJSON is the week objective breakdown.
type ObjectiveJSON struct {
	Meals    float64 `json:"meals"`
	Variety  float64 `json:"variety"`
	CookTime float64 `json:"cookTime"`
	Rules    float64 `json:"rules"`
	Novelty  float64 `json:"novelty"`
	Total    float64 `json:"total"`
}

// ProposalResponse is an Autopilot week proposal.
type ProposalResponse struct {
	ID             string         `json:"id"`
	HouseholdID    string         `json:"householdId"`
	Week           string         `json:"week"`
	StartDate      string         `json:"startDate"`
	EndDate        string         `json:"endDate"`
	Status         ProposalStatus `json:"status"`
	Version        int64          `json:"version"`
	Attempt        int            `json:"attempt"`
	ModelVersion   string         `json:"modelVersion"`
	InputsHash     string         `json:"inputsHash"`
	RequestedMeals int            `json:"requestedMeals"`
	PlannedMeals   int            `json:"plannedMeals"`
	CandidateCount int            `json:"candidateCount"`
	ColdStart      bool           `json:"coldStart"`
	Slots          []SlotJSON     `json:"slots"`
	Unfilled       []UnfilledJSON `json:"unfilled"`
	Messages       []TextJSON     `json:"messages"`
	Objective      ObjectiveJSON  `json:"objective"`
	SwapCount      int            `json:"swapCount"`
	ExcludedSlots  []string       `json:"excludedSlotIds"`
	GeneratedBy    string         `json:"generatedBy"`
	GeneratedAt    time.Time      `json:"generatedAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	DecidedBy      *string        `json:"decidedBy"`
	DecidedAt      *time.Time     `json:"decidedAt"`
}

// SkippedSlotJSON is a proposed meal accepting didn't add.
type SkippedSlotJSON struct {
	SlotID string `json:"slotId"`
	Day    string `json:"day"`
	Reason string `json:"reason"`
}

// AcceptResponse is returned by accepting a proposal.
type AcceptResponse struct {
	Proposal ProposalResponse         `json:"proposal"`
	Plan     planning.PlanResponse    `json:"plan"`
	Added    []planning.EntryResponse `json:"added"`
	Skipped  []SkippedSlotJSON        `json:"skipped"`
}

type generateRequest struct {
	AvoidPrevious *bool `json:"avoidPrevious"`
}

type versionRequest struct {
	Version *int64 `json:"version"`
}

type acceptRequest struct {
	Version        *int64   `json:"version"`
	ExcludeSlotIDs []string `json:"excludeSlotIds"`
}

// MethodJSON says whether a recipe suits a cooking method.
type MethodJSON struct {
	Method         string `json:"method"`
	Label          string `json:"label"`
	Suits          bool   `json:"suits"`
	Source         string `json:"source"`
	HeuristicSuits bool   `json:"heuristicSuits"`
	Evidence       string `json:"evidence,omitempty"`
}

// OverrideJSON is a recipe's cooking-method override.
type OverrideJSON struct {
	RecipeID  string          `json:"recipeId"`
	Methods   map[string]bool `json:"methods"`
	UpdatedBy string          `json:"updatedBy"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// AttributesResponse is what Autopilot derives from a recipe.
type AttributesResponse struct {
	RecipeID    string   `json:"recipeId"`
	CookMinutes *int     `json:"cookMinutes"`
	TimeBand    string   `json:"timeBand"`
	Cuisines    []string `json:"cuisines"`
	// CuisineRegions are the broader regions Autopilot also matches.
	CuisineRegions []string      `json:"cuisineRegions"`
	Tags           []string      `json:"tags"`
	Proteins       []string      `json:"proteins"`
	Allergens      []string      `json:"allergens"`
	Diets          []string      `json:"diets"`
	Spicy          bool          `json:"spicy"`
	SpicyEvidence  string        `json:"spicyEvidence,omitempty"`
	Methods        []MethodJSON  `json:"methods"`
	Override       *OverrideJSON `json:"override"`
}

type overrideRequest struct {
	Methods map[string]*bool `json:"methods"`
}

// OverrideListResponse lists recipe overrides.
type OverrideListResponse struct {
	Items []OverrideJSON `json:"items"`
}

// HistoryItemJSON is one preference change.
type HistoryItemJSON struct {
	Type       string        `json:"type"`
	UserID     string        `json:"userId"`
	OccurredAt time.Time     `json:"occurredAt"`
	Week       string        `json:"week,omitempty"`
	RecipeID   string        `json:"recipeId,omitempty"`
	Sections   []string      `json:"sections,omitempty"`
	Changes    []ChangeEntry `json:"changes,omitempty"`
	Cleared    bool          `json:"cleared,omitempty"`
	Method     string        `json:"method,omitempty"`
	Value      string        `json:"value,omitempty"`
	Previous   string        `json:"previous,omitempty"`
}

// ChangeEntry is how one field changed.
type ChangeEntry struct {
	Field   string   `json:"field"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	From    string   `json:"from,omitempty"`
	To      string   `json:"to,omitempty"`
}

// HistoryResponse lists preference changes, newest first.
type HistoryResponse struct {
	Items []HistoryItemJSON `json:"items"`
}

// OptionJSON is a choice.
type OptionJSON struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	RecipeCount *int   `json:"recipeCount,omitempty"`
}

// VocabularyResponse lists the choices for preference screens.
type VocabularyResponse struct {
	Cuisines           []OptionJSON `json:"cuisines"`
	Tags               []OptionJSON `json:"tags"`
	Proteins           []OptionJSON `json:"proteins"`
	Diets              []OptionJSON `json:"diets"`
	Allergens          []OptionJSON `json:"allergens"`
	Equipment          []OptionJSON `json:"equipment"`
	Novelty            []OptionJSON `json:"novelty"`
	TimeBands          []OptionJSON `json:"timeBands"`
	Frequencies        []OptionJSON `json:"frequencies"`
	Days               []OptionJSON `json:"days"`
	CatalogRecipeCount int          `json:"catalogRecipeCount"`
	Limits             LimitsJSON   `json:"limits"`
}

// LimitsJSON are the validation bounds.
type LimitsJSON struct {
	MaxListValues          int `json:"maxListValues"`
	MaxExcludedIngredients int `json:"maxExcludedIngredients"`
	MaxValueLength         int `json:"maxValueLength"`
	MaxIngredientLength    int `json:"maxIngredientLength"`
	MaxRuleValues          int `json:"maxRuleValues"`
	MaxLabelLength         int `json:"maxLabelLength"`
	MaxNoteLength          int `json:"maxNoteLength"`
	MinCookMinutes         int `json:"minCookMinutes"`
	MaxCookMinutes         int `json:"maxCookMinutes"`
	MaxServings            int `json:"maxServings"`
}

// --- conversions ------------------------------------------------------------------

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func optionalInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// positive reads a nullable positive number: null means 0 (not set).
func positive(field string, v *int) (int, error) {
	if v == nil {
		return 0, nil
	}
	if *v <= 0 {
		return 0, invalidf("%s must be a positive number or null", field)
	}
	return *v, nil
}

func choicesJSON(c Choices) ChoicesJSON {
	return ChoicesJSON{orEmptyStrings(c.Cuisines), orEmptyStrings(c.Tags), orEmptyStrings(c.Proteins)}
}

func newProfileResponse(p Profile, servings int) ProfileResponse {
	r := p.Restrictions
	resp := ProfileResponse{
		HouseholdID: p.HouseholdID, Configured: p.Configured(),
		Taste: TasteJSON{Likes: choicesJSON(p.Taste.Likes), Dislikes: choicesJSON(p.Taste.Dislikes)},
		Restrictions: RestrictionsJSON{
			Diets: orEmptyStrings(r.Diets), Allergens: orEmptyStrings(r.Allergens), ExcludedIngredients: orEmptyStrings(r.ExcludedIngredients),
			ExcludedCuisines: orEmptyStrings(r.ExcludedCuisines), ExcludedProteins: orEmptyStrings(r.ExcludedProteins),
			ExcludedTags: orEmptyStrings(r.ExcludedTags), NoSpicy: r.NoSpicy,
		},
		Schedule: ScheduleJSON{
			PlanDays: orEmptyStrings(p.Schedule.PlanDays), Weeknights: orEmptyStrings(p.Schedule.Weeknights), MealsPerWeek: p.Schedule.MealsPerWeek,
			DefaultServings: optionalInt(p.Schedule.DefaultServings), WeeknightMaxMinutes: optionalInt(p.Schedule.WeeknightMaxMinutes),
		},
		CookTime:     CookTimeJSON(p.CookTime),
		Novelty:      p.Novelty,
		Equipment:    orEmptyStrings(p.Equipment),
		WeekdayRules: []WeekdayRuleJSON{},
		Sections:     map[string]*ChangeJSON{},
		Effective:    EffectiveJSON{DefaultServings: servings},
		CreatedBy:    optionalString(p.CreatedBy), CreatedAt: optionalTime(p.CreatedAt),
		UpdatedBy: optionalString(p.UpdatedBy), UpdatedAt: optionalTime(p.UpdatedAt),
	}
	for _, rule := range p.WeekdayRules {
		resp.WeekdayRules = append(resp.WeekdayRules, WeekdayRuleJSON{
			Day: rule.Day, Label: rule.Label, Cuisines: orEmptyStrings(rule.Cuisines), Tags: orEmptyStrings(rule.Tags),
			Proteins: orEmptyStrings(rule.Proteins), Methods: orEmptyStrings(rule.Methods), TimeBand: optionalString(rule.TimeBand),
			Frequency: rule.Frequency,
		})
	}
	for _, section := range Sections {
		resp.Sections[string(section)] = nil
		if change, ok := p.Sections[section]; ok {
			resp.Sections[string(section)] = &ChangeJSON{UpdatedBy: change.UpdatedBy, UpdatedAt: change.UpdatedAt}
		}
	}
	return resp
}

func (req profileRequest) update() (ProfileUpdate, error) {
	var u ProfileUpdate
	if t := req.Taste; t != nil {
		u.Taste = &Taste{Likes: Choices(t.Likes), Dislikes: Choices(t.Dislikes)}
	}
	if r := req.Restrictions; r != nil {
		u.Restrictions = &Restrictions{
			Diets: r.Diets, Allergens: r.Allergens, ExcludedIngredients: r.ExcludedIngredients, ExcludedCuisines: r.ExcludedCuisines,
			ExcludedProteins: r.ExcludedProteins, ExcludedTags: r.ExcludedTags, NoSpicy: r.NoSpicy,
		}
	}
	if s := req.Schedule; s != nil {
		servings, err := positive("schedule.defaultServings", s.DefaultServings)
		if err != nil {
			return ProfileUpdate{}, err
		}
		limit, err := positive("schedule.weeknightMaxMinutes", s.WeeknightMaxMinutes)
		if err != nil {
			return ProfileUpdate{}, err
		}
		u.Schedule = &Schedule{PlanDays: s.PlanDays, Weeknights: s.Weeknights, MealsPerWeek: s.MealsPerWeek, DefaultServings: servings, WeeknightMaxMinutes: limit}
	}
	if c := req.CookTime; c != nil {
		ct := CookTime(*c)
		u.CookTime = &ct
	}
	u.Novelty, u.Equipment = req.Novelty, req.Equipment
	if req.WeekdayRules != nil {
		rules := make([]WeekdayRule, 0, len(*req.WeekdayRules))
		for _, r := range *req.WeekdayRules {
			band := ""
			if r.TimeBand != nil {
				band = *r.TimeBand
			}
			rules = append(rules, WeekdayRule{
				Day: r.Day, Label: r.Label, Cuisines: r.Cuisines, Tags: r.Tags, Proteins: r.Proteins, Methods: r.Methods,
				TimeBand: band, Frequency: r.Frequency,
			})
		}
		u.WeekdayRules = &rules
	}
	return u, nil
}

func newWeekContextResponse(c WeekContext) WeekContextResponse {
	w, _ := planning.ParseWeek(c.Week)
	resp := WeekContextResponse{
		HouseholdID: c.HouseholdID, Week: c.Week, StartDate: w.Date(planning.Monday), EndDate: w.Date(planning.Sunday),
		Configured: c.Configured(), Skip: c.Skip, Busy: c.Busy, MealsPerWeek: optionalInt(c.MealsPerWeek),
		MaxMinutes: optionalInt(c.MaxMinutes), Servings: optionalInt(c.Servings), Days: []DayOverrideJSON{}, Note: c.Note,
		UpdatedBy: optionalString(c.UpdatedBy), UpdatedAt: optionalTime(c.UpdatedAt),
	}
	for _, d := range c.Days {
		resp.Days = append(resp.Days, DayOverrideJSON{Day: d.Day, Skip: d.Skip, MaxMinutes: optionalInt(d.MaxMinutes), Servings: optionalInt(d.Servings)})
	}
	return resp
}

func (req weekContextRequest) context() (WeekContext, error) {
	c := WeekContext{Skip: req.Skip, Busy: req.Busy, Note: req.Note}
	var err error
	if c.MealsPerWeek, err = positive("mealsPerWeek", req.MealsPerWeek); err != nil {
		return WeekContext{}, err
	}
	if c.MaxMinutes, err = positive("maxMinutes", req.MaxMinutes); err != nil {
		return WeekContext{}, err
	}
	if c.Servings, err = positive("servings", req.Servings); err != nil {
		return WeekContext{}, err
	}
	for _, d := range req.Days {
		o := DayOverride{Day: d.Day, Skip: d.Skip}
		if o.MaxMinutes, err = positive("days.maxMinutes", d.MaxMinutes); err != nil {
			return WeekContext{}, err
		}
		if o.Servings, err = positive("days.servings", d.Servings); err != nil {
			return WeekContext{}, err
		}
		c.Days = append(c.Days, o)
	}
	return c, nil
}

func newProposalResponse(p Proposal) ProposalResponse {
	w, _ := planning.ParseWeek(p.Week)
	resp := ProposalResponse{
		ID: p.ID, HouseholdID: p.HouseholdID, Week: p.Week, StartDate: w.Date(planning.Monday), EndDate: w.Date(planning.Sunday),
		Status: p.Status, Version: p.Version, Attempt: p.Attempt, ModelVersion: p.ModelVersion, InputsHash: p.InputsHash,
		RequestedMeals: p.Requested, PlannedMeals: p.Planned, CandidateCount: p.Candidates, ColdStart: p.ColdStart,
		Slots: []SlotJSON{}, Unfilled: []UnfilledJSON{}, Messages: []TextJSON{}, Objective: ObjectiveJSON(p.Objective),
		SwapCount: p.SwapCount, ExcludedSlots: orEmptyStrings(p.ExcludedSlots), GeneratedBy: p.GeneratedBy,
		GeneratedAt: p.GeneratedAt, UpdatedAt: p.UpdatedAt, DecidedBy: optionalString(p.DecidedBy), DecidedAt: optionalTime(p.DecidedAt),
	}
	for _, s := range p.Slots {
		sj := SlotJSON{
			ID: s.ID, Day: s.Day, Date: w.Date(planning.Day(s.Day)),
			Recipe:   ProposalRecipeJSON{ID: s.RecipeID, Name: s.RecipeName, ImageURL: s.RecipeImageURL},
			Servings: s.Servings, CookMinutes: optionalInt(s.CookMinutes), TimeBand: s.TimeBand, Score: s.Score,
			Signals: s.Signals, Reasons: []TextJSON{}, SwapCount: s.SwapCount,
		}
		if sj.Signals == nil {
			sj.Signals = map[string]float64{}
		}
		for _, r := range s.Reasons {
			sj.Reasons = append(sj.Reasons, TextJSON(r))
		}
		resp.Slots = append(resp.Slots, sj)
	}
	for _, u := range p.Unfilled {
		resp.Unfilled = append(resp.Unfilled, UnfilledJSON{Day: u.Day, Date: w.Date(planning.Day(u.Day)), Code: u.Code, Text: u.Text})
	}
	for _, m := range p.Messages {
		resp.Messages = append(resp.Messages, TextJSON(m))
	}
	return resp
}

func newOverrideJSON(o RecipeOverride) OverrideJSON {
	methods := o.Methods
	if methods == nil {
		methods = map[string]bool{}
	}
	return OverrideJSON{RecipeID: o.RecipeID, Methods: methods, UpdatedBy: o.UpdatedBy, UpdatedAt: o.UpdatedAt}
}

func newAttributesResponse(a RecipeAttributes) AttributesResponse {
	resp := AttributesResponse{
		RecipeID: a.RecipeID, CookMinutes: optionalInt(a.CookMinutes), TimeBand: a.TimeBand,
		Cuisines: orEmptyStrings(a.Cuisines), CuisineRegions: orEmptyStrings(a.CuisineRegions), Tags: orEmptyStrings(a.Tags), Proteins: orEmptyStrings(a.Proteins),
		Allergens: orEmptyStrings(a.Allergens), Diets: orEmptyStrings(a.Diets), Spicy: a.Spicy, SpicyEvidence: a.SpicyEvidence,
		Methods: []MethodJSON{},
	}
	for _, m := range a.Methods {
		resp.Methods = append(resp.Methods, MethodJSON{
			Method: m.Method, Label: optionLabel(EquipmentOptions, m.Method), Suits: m.Suits, Source: m.Source,
			HeuristicSuits: m.HeuristicSuits, Evidence: m.Evidence,
		})
	}
	if a.Override != nil && len(a.Override.Methods) > 0 {
		o := newOverrideJSON(*a.Override)
		resp.Override = &o
	}
	return resp
}

func optionsJSON(options []Option, counts bool) []OptionJSON {
	out := make([]OptionJSON, 0, len(options))
	for _, o := range options {
		oj := OptionJSON{Value: o.Value, Label: o.Label, Description: o.Description}
		if counts {
			n := o.RecipeCount
			oj.RecipeCount = &n
		}
		out = append(out, oj)
	}
	return out
}

// --- handlers ---------------------------------------------------------------------

func actor(r *http.Request) households.Membership {
	m, _ := households.MembershipFromContext(r.Context())
	return m
}

func (h *Handler) writeProfile(w http.ResponseWriter, r *http.Request, p Profile) {
	servings, err := h.opts.Service.DefaultServings(r.Context(), p)
	if err != nil {
		h.writeError(w, r, "load household servings failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newProfileResponse(p, servings))
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	p, err := h.opts.Service.Profile(r.Context(), actor(r).HouseholdID)
	if err != nil {
		h.writeError(w, r, "get autopilot profile failed", err)
		return
	}
	h.writeProfile(w, r, p)
}

func (h *Handler) replaceProfile(w http.ResponseWriter, r *http.Request) { h.updateProfile(w, r, true) }

func (h *Handler) patchProfile(w http.ResponseWriter, r *http.Request) { h.updateProfile(w, r, false) }

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request, replace bool) {
	var req profileRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	u, err := req.update()
	if err != nil {
		h.writeError(w, r, "update autopilot profile failed", err)
		return
	}
	a := actor(r)
	p, err := h.opts.Service.UpdateProfile(r.Context(), a.HouseholdID, a.UserID, u, replace)
	if err != nil {
		h.writeError(w, r, "update autopilot profile failed", err)
		return
	}
	h.writeProfile(w, r, p)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxHistoryItems {
			validationFailed(w, r, "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	items, err := h.opts.Service.History(r.Context(), actor(r).HouseholdID, limit)
	if err != nil {
		h.writeError(w, r, "list autopilot history failed", err)
		return
	}
	resp := HistoryResponse{Items: make([]HistoryItemJSON, 0, len(items))}
	for _, it := range items {
		hj := HistoryItemJSON{
			Type: string(it.Type), UserID: it.UserID, OccurredAt: it.OccurredAt, Week: it.Week, RecipeID: it.RecipeID,
			Sections: it.Sections, Cleared: it.Cleared, Method: it.Method, Value: it.Value, Previous: it.Previous,
		}
		for _, c := range it.Changes {
			hj.Changes = append(hj.Changes, ChangeEntry(c))
		}
		resp.Items = append(resp.Items, hj)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) vocabulary(w http.ResponseWriter, r *http.Request) {
	v, err := h.opts.Service.Vocabulary(r.Context(), actor(r).HouseholdID)
	if err != nil {
		h.writeError(w, r, "build autopilot vocabulary failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, VocabularyResponse{
		Cuisines: optionsJSON(v.Cuisines, true), Tags: optionsJSON(v.Tags, true), Proteins: optionsJSON(v.Proteins, true),
		Diets: optionsJSON(v.Diets, false), Allergens: optionsJSON(v.Allergens, false), Equipment: optionsJSON(v.Equipment, false),
		Novelty: optionsJSON(v.Novelty, false), TimeBands: optionsJSON(v.TimeBands, false), Frequencies: optionsJSON(v.Frequencies, false),
		Days: optionsJSON(v.Days, false), CatalogRecipeCount: v.CatalogRecipes,
		Limits: LimitsJSON{
			MaxListValues: MaxListValues, MaxExcludedIngredients: MaxExcludedIngredient, MaxValueLength: MaxValueLength,
			MaxIngredientLength: MaxIngredientLength, MaxRuleValues: MaxRuleValues, MaxLabelLength: MaxLabelLength,
			MaxNoteLength: MaxNoteLength, MinCookMinutes: MinCookMinutes, MaxCookMinutes: MaxCookMinutes, MaxServings: MaxServings,
		},
	})
}

func (h *Handler) listOverrides(w http.ResponseWriter, r *http.Request) {
	list, err := h.opts.Service.RecipeOverrides(r.Context(), actor(r).HouseholdID)
	if err != nil {
		h.writeError(w, r, "list recipe overrides failed", err)
		return
	}
	resp := OverrideListResponse{Items: make([]OverrideJSON, 0, len(list))}
	for _, o := range list {
		resp.Items = append(resp.Items, newOverrideJSON(o))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) attributes(w http.ResponseWriter, r *http.Request) {
	a, err := h.opts.Service.RecipeAttributes(r.Context(), actor(r).HouseholdID, chi.URLParam(r, "recipeId"))
	if err != nil {
		h.writeError(w, r, "get recipe attributes failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newAttributesResponse(a))
}

func (h *Handler) setOverride(w http.ResponseWriter, r *http.Request) {
	var req overrideRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	m := actor(r)
	a, err := h.opts.Service.SetRecipeOverride(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "recipeId"), req.Methods)
	if err != nil {
		h.writeError(w, r, "set recipe override failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newAttributesResponse(a))
}

func (h *Handler) getContext(w http.ResponseWriter, r *http.Request) {
	c, err := h.opts.Service.WeekContext(r.Context(), actor(r).HouseholdID, chi.URLParam(r, "week"))
	if err != nil {
		h.writeError(w, r, "get week context failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newWeekContextResponse(c))
}

func (h *Handler) putContext(w http.ResponseWriter, r *http.Request) {
	var req weekContextRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in, err := req.context()
	if err != nil {
		h.writeError(w, r, "save week context failed", err)
		return
	}
	m := actor(r)
	c, err := h.opts.Service.SaveWeekContext(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), in)
	if err != nil {
		h.writeError(w, r, "save week context failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newWeekContextResponse(c))
}

func (h *Handler) deleteContext(w http.ResponseWriter, r *http.Request) {
	m := actor(r)
	if err := h.opts.Service.ClearWeekContext(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week")); err != nil {
		h.writeError(w, r, "clear week context failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	var req generateRequest
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("request body must not exceed %d bytes", tooLarge.Limit))
			return
		}
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "request body could not be read")
		return
	}
	// The body is optional: an empty one, chunked or not, means no options.
	if len(bytes.TrimSpace(raw)) > 0 {
		r.Body = io.NopCloser(bytes.NewReader(raw))
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
	}
	m := actor(r)
	p, err := h.opts.Service.Generate(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), GenerateOptions(req))
	if err != nil {
		h.writeError(w, r, "generate autopilot week failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, newProposalResponse(p))
}

func (h *Handler) getProposal(w http.ResponseWriter, r *http.Request) {
	p, err := h.opts.Service.Proposal(r.Context(), actor(r).HouseholdID, chi.URLParam(r, "week"))
	if err != nil {
		h.writeError(w, r, "get autopilot proposal failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newProposalResponse(p))
}

func decodeVersion(w http.ResponseWriter, r *http.Request, v *int64) (int64, bool) {
	if v == nil {
		validationFailed(w, r, "version is required: send the proposal's current version")
		return 0, false
	}
	return *v, true
}

func (h *Handler) swap(w http.ResponseWriter, r *http.Request) {
	var req versionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	version, ok := decodeVersion(w, r, req.Version)
	if !ok {
		return
	}
	m := actor(r)
	p, err := h.opts.Service.Swap(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), chi.URLParam(r, "slotId"), version)
	if err != nil {
		h.writeError(w, r, "swap autopilot meal failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newProposalResponse(p))
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	var req acceptRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	version, ok := decodeVersion(w, r, req.Version)
	if !ok {
		return
	}
	m := actor(r)
	res, err := h.opts.Service.Accept(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), version, req.ExcludeSlotIDs)
	if err != nil {
		h.writeError(w, r, "accept autopilot week failed", err)
		return
	}
	resp := AcceptResponse{
		Proposal: newProposalResponse(res.Proposal), Plan: planning.NewPlanResponse(res.Plan),
		Added: make([]planning.EntryResponse, 0, len(res.Added)), Skipped: make([]SkippedSlotJSON, 0, len(res.Skipped)),
	}
	for _, e := range res.Added {
		resp.Added = append(resp.Added, planning.NewEntryResponse(res.Plan.Week, e))
	}
	for _, s := range res.Skipped {
		resp.Skipped = append(resp.Skipped, SkippedSlotJSON(s))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	var req versionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	version, ok := decodeVersion(w, r, req.Version)
	if !ok {
		return
	}
	m := actor(r)
	p, err := h.opts.Service.Reject(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), version)
	if err != nil {
		h.writeError(w, r, "reject autopilot week failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newProposalResponse(p))
}

// writeError maps service errors to responses.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, planning.ErrInvalidWeek):
		validationFailed(w, r, publicMessage(err))
	case errors.Is(err, ErrNotFound):
		text := publicMessage(err)
		if err.Error() == ErrNotFound.Error() {
			text = "no Autopilot proposal for this week"
		}
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", text)
	case errors.Is(err, ErrPlanFinalized):
		httpx.WriteError(w, r, http.StatusConflict, "plan_finalized", "the plan is finalized; set its status to draft to use Autopilot")
	case errors.Is(err, planning.ErrPlanFull):
		httpx.WriteError(w, r, http.StatusConflict, "plan_full", "the week has no room for these meals")
	case errors.Is(err, ErrProposalChanged):
		httpx.WriteError(w, r, http.StatusConflict, "proposal_changed", "the proposal changed; reload it and try again")
	case errors.Is(err, ErrProposalNotPending):
		httpx.WriteError(w, r, http.StatusConflict, "proposal_not_pending", "the proposal was already accepted or rejected")
	case errors.Is(err, ErrNoAlternative):
		httpx.WriteError(w, r, http.StatusConflict, "no_alternative", publicMessage(err))
	case errors.Is(err, ErrNothingToAccept):
		httpx.WriteError(w, r, http.StatusConflict, "nothing_to_accept", "every meal is excluded or its day is already planned")
	case errors.Is(err, ErrProposalStale):
		httpx.WriteError(w, r, http.StatusConflict, "proposal_stale", "a recipe in the proposal changed; generate the week again")
	case errors.Is(err, ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "the preferences kept changing; try again")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}

func validationFailed(w http.ResponseWriter, r *http.Request, message string) {
	httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", message)
}

// publicMessage strips sentinel prefixes from errors whose details are safe
// to show clients.
func publicMessage(err error) string {
	msg := err.Error()
	for _, prefix := range []string{ErrInvalid.Error() + ": ", ErrNotFound.Error() + ": ", ErrNoAlternative.Error() + ": ", "planning: "} {
		msg = strings.ReplaceAll(msg, prefix, "")
	}
	return msg
}
