package menu

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// HandlerOptions configures the menu HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the menu endpoints.
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

// Mount registers the routes on r, the /api/v1 router. Every route needs
// household.view.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		const base = "/households/{householdId}"
		r.With(view).Get(base+"/menu", h.menu)
		r.With(view).Get(base+"/menu/recipes", h.recipes)
		r.With(view).Get(base+"/menu/filters", h.filters)
		r.With(view).Get(base+"/weeks", h.weeks)
	})
}

// --- wire types -------------------------------------------------------------------

// MenuResponse is returned by GET .../menu.
type MenuResponse struct {
	Week      string `json:"week"`
	WeekStart string `json:"weekStart"`
	WeekEnd   string `json:"weekEnd"`
	// CurrentWeek is the household's current week, which timing compares to.
	CurrentWeek string `json:"currentWeek"`
	Timing      Timing `json:"timing"`
	// Plan is null when nothing is stored for the week.
	Plan     *planning.PlanResponse   `json:"plan"`
	Proposal *ProposalSummaryResponse `json:"proposal"`
	Sections []SectionResponse        `json:"sections"`
}

// ProposalSummaryResponse summarizes the week's Autopilot proposal.
type ProposalSummaryResponse struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Version      int64  `json:"version"`
	PlannedMeals int    `json:"plannedMeals"`
}

// SectionResponse is a menu section.
type SectionResponse struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Title     string            `json:"title"`
	Subtitle  string            `json:"subtitle"`
	Items     []CardResponse    `json:"items"`
	MoreQuery map[string]string `json:"moreQuery"`
}

// CardResponse is a recipe card.
type CardResponse struct {
	Recipe       recipes.RecipeSummaryResponse `json:"recipe"`
	Badges       []BadgeResponse               `json:"badges"`
	Reason       *string                       `json:"reason"`
	InPlan       bool                          `json:"inPlan"`
	PlanEntryIDs []string                      `json:"planEntryIds"`
}

// BadgeResponse is a card badge.
type BadgeResponse struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// MenuRecipesResponse is returned by GET .../menu/recipes.
type MenuRecipesResponse struct {
	Items      []CardResponse `json:"items"`
	NextCursor *string        `json:"nextCursor"`
}

// FilterOptionResponse is a chip with a count.
type FilterOptionResponse struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// SortOptionResponse is a sort chip.
type SortOptionResponse struct {
	Value Sort   `json:"value"`
	Label string `json:"label"`
}

// FiltersResponse is returned by GET .../menu/filters.
type FiltersResponse struct {
	Proteins   []FilterOptionResponse `json:"proteins"`
	Cuisines   []FilterOptionResponse `json:"cuisines"`
	Tags       []FilterOptionResponse `json:"tags"`
	MaxMinutes []int                  `json:"maxMinutes"`
	Sorts      []SortOptionResponse   `json:"sorts"`
}

// WeekSummaryResponse is one week in the strip.
type WeekSummaryResponse struct {
	Week         string `json:"week"`
	WeekStart    string `json:"weekStart"`
	WeekEnd      string `json:"weekEnd"`
	Timing       Timing `json:"timing"`
	PlannedCount int    `json:"plannedCount"`
	AddOnCount   int    `json:"addOnCount"`
	CookedCount  int    `json:"cookedCount"`
	OrderedCount int    `json:"orderedCount"`
	Status       string `json:"status"`
}

// WeeksResponse is returned by GET .../weeks.
type WeeksResponse struct {
	Items        []WeekSummaryResponse `json:"items"`
	EarliestWeek *string               `json:"earliestWeek"`
}

func newCardResponses(cards []Card, bands autopilot.TimeBands) []CardResponse {
	out := make([]CardResponse, 0, len(cards))
	for _, c := range cards {
		resp := CardResponse{
			Recipe: recipes.NewRecipeSummaryResponse(recipes.SummaryOf(c.Recipe), c.Rating, bands),
			Badges: make([]BadgeResponse, 0, len(c.Badges)), InPlan: c.InPlan, PlanEntryIDs: c.PlanEntryIDs,
		}
		if resp.PlanEntryIDs == nil {
			resp.PlanEntryIDs = []string{}
		}
		for _, b := range c.Badges {
			resp.Badges = append(resp.Badges, BadgeResponse(b))
		}
		if c.Reason != "" {
			reason := c.Reason
			resp.Reason = &reason
		}
		out = append(out, resp)
	}
	return out
}

func filterOptions(options []FilterOption) []FilterOptionResponse {
	out := make([]FilterOptionResponse, 0, len(options))
	for _, o := range options {
		out = append(out, FilterOptionResponse(o))
	}
	return out
}

// --- handlers -------------------------------------------------------------------

func (h *Handler) menu(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	m, err := h.opts.Service.Menu(r.Context(), actor.HouseholdID, actor.UserID, r.URL.Query().Get("week"))
	if err != nil {
		h.writeError(w, r, "build menu failed", err)
		return
	}
	resp := MenuResponse{
		Week: m.Week.String(), WeekStart: m.Week.StartDateOn(m.FirstDay), WeekEnd: m.Week.EndDateOn(m.FirstDay),
		CurrentWeek: m.CurrentWeek.String(), Timing: m.Timing, Sections: make([]SectionResponse, 0, len(m.Sections)),
	}
	if m.Plan != nil {
		plan := planning.NewPlanResponse(*m.Plan)
		resp.Plan = &plan
	}
	if p := m.Proposal; p != nil {
		resp.Proposal = &ProposalSummaryResponse{ID: p.ID, Status: string(p.Status), Version: p.Version, PlannedMeals: p.Planned}
	}
	for _, sec := range m.Sections {
		resp.Sections = append(resp.Sections, SectionResponse{
			ID: sec.ID, Kind: sec.Kind, Title: sec.Title, Subtitle: sec.Subtitle,
			Items: newCardResponses(sec.Cards, m.Bands), MoreQuery: sec.MoreQuery,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) recipes(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	q, err := parseListQuery(r.URL.Query())
	if err != nil {
		h.writeError(w, r, "", err)
		return
	}
	page, err := h.opts.Service.Recipes(r.Context(), actor.HouseholdID, actor.UserID, q)
	if err != nil {
		h.writeError(w, r, "list menu recipes failed", err)
		return
	}
	resp := MenuRecipesResponse{Items: newCardResponses(page.Cards, page.Bands)}
	if page.NextCursor != "" {
		resp.NextCursor = &page.NextCursor
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func parseListQuery(v url.Values) (ListQuery, error) {
	q := ListQuery{
		Search: v.Get("q"), Protein: v.Get("protein"), Cuisine: v.Get("cuisine"), Tag: v.Get("tag"),
		Sort: Sort(v.Get("sort")), Week: v.Get("week"), Cursor: v.Get("cursor"),
	}
	var err error
	if q.MaxMinutes, err = positiveParam(v, "maxMinutes"); err != nil {
		return ListQuery{}, err
	}
	if q.Limit, err = positiveParam(v, "limit"); err != nil {
		return ListQuery{}, err
	}
	if s := v.Get("addons"); s != "" {
		b, err := strconv.ParseBool(s)
		if err != nil {
			return ListQuery{}, invalidf("addons must be true or false")
		}
		q.Addons = b
	}
	return q, nil
}

// positiveParam parses an optional positive integer; absent is 0.
func positiveParam(v url.Values, name string) (int, error) {
	s := v.Get(name)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, invalidf("%s must be a positive integer", name)
	}
	return n, nil
}

func (h *Handler) filters(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	f, err := h.opts.Service.Filters(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "load menu filters failed", err)
		return
	}
	resp := FiltersResponse{
		Proteins: filterOptions(f.Proteins), Cuisines: filterOptions(f.Cuisines), Tags: filterOptions(f.Tags),
		MaxMinutes: f.MaxMinutes, Sorts: make([]SortOptionResponse, 0, len(f.Sorts)),
	}
	for _, s := range f.Sorts {
		resp.Sorts = append(resp.Sorts, SortOptionResponse(s))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) weeks(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	v := r.URL.Query()
	before, after := DefaultWeeksBefore, DefaultWeeksAfter
	for _, p := range []struct {
		name string
		dst  *int
	}{{"before", &before}, {"after", &after}} {
		if s := v.Get(p.name); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil {
				h.writeError(w, r, "", invalidf("%s must be an integer", p.name))
				return
			}
			*p.dst = n
		}
	}
	strip, err := h.opts.Service.Weeks(r.Context(), actor.HouseholdID, v.Get("around"), before, after)
	if err != nil {
		h.writeError(w, r, "load week strip failed", err)
		return
	}
	resp := WeeksResponse{Items: make([]WeekSummaryResponse, 0, len(strip.Weeks))}
	for _, ws := range strip.Weeks {
		resp.Items = append(resp.Items, WeekSummaryResponse{
			Week: ws.Week.String(), WeekStart: ws.Week.StartDateOn(strip.FirstDay), WeekEnd: ws.Week.EndDateOn(strip.FirstDay), Timing: ws.Timing,
			PlannedCount: ws.Planned, AddOnCount: ws.AddOns, CookedCount: ws.Cooked, OrderedCount: ws.Ordered, Status: ws.Status,
		})
	}
	if strip.Earliest != nil {
		earliest := strip.Earliest.String()
		resp.EarliestWeek = &earliest
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, ErrInvalid) {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", publicMessage(err))
		return
	}
	h.logger.ErrorContext(r.Context(), msg, "error", err)
	httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
}

// publicMessage strips the sentinel prefix from a validation error.
func publicMessage(err error) string {
	return strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": ")
}
