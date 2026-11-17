package nextsteps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

const household = "66e5a1f2c3b4a5d6e7f80a01"

type fakePlus struct {
	has      bool
	progress *plus.Progress
	err      error
}

func (f fakePlus) HasPlus(context.Context, string) (bool, error) { return f.has, f.err }
func (f fakePlus) ReferralProgress(context.Context, string) (*plus.Progress, error) {
	return f.progress, f.err
}

type fakeAutopilot struct {
	version int64
	err     error
}

func (f fakeAutopilot) Profile(_ context.Context, id string) (recommendations.Profile, error) {
	p := recommendations.DefaultProfile(id)
	p.Version = f.version
	return p, f.err
}

type fakeImports struct {
	open int
	err  error
}

func (f fakeImports) ImportReviews(_ context.Context, _, status string, limit int) ([]recipes.ReviewRecord, error) {
	if status != recipes.ReviewStatusOpen {
		return nil, errors.New("only open items count")
	}
	return make([]recipes.ReviewRecord, min(f.open, limit)), f.err
}

func pending(p *plus.Progress) *plus.Progress { p.State = string(plus.StatePending); return p }

func steps(done ...bool) []plus.ProgressStep {
	out := make([]plus.ProgressStep, len(done))
	for i, d := range done {
		out[i] = plus.ProgressStep{ID: "s", Done: d}
	}
	return out
}

func ids(steps []Step) []string {
	out := []string{}
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}

func TestRules(t *testing.T) {
	planner := households.Membership{HouseholdID: household, Role: households.RoleMember}
	boom := errors.New("boom")
	for _, tc := range []struct {
		name    string
		sources Sources
		want    []string
	}{
		{"nothing to do", Sources{Plus: fakePlus{has: true}, Autopilot: fakeAutopilot{version: 1}, Imports: fakeImports{}}, []string{}},
		{"Plus and no Autopilot", Sources{Plus: fakePlus{has: true}, Autopilot: fakeAutopilot{}}, []string{StepAutopilotSetup}},
		{"no Plus, no Autopilot: nothing to set up", Sources{Plus: fakePlus{}, Autopilot: fakeAutopilot{}}, []string{}},
		{"Autopilot set up", Sources{Plus: fakePlus{has: true}, Autopilot: fakeAutopilot{version: 3}}, []string{}},
		{"Autopilot can't be read", Sources{Plus: fakePlus{has: true}, Autopilot: fakeAutopilot{err: boom}}, []string{}},
		{"import review items waiting", Sources{Imports: fakeImports{open: 4}}, []string{StepImportReview}},
		{"no import review items", Sources{Imports: fakeImports{}}, []string{}},
		{"import reviews can't be read", Sources{Imports: fakeImports{open: 1, err: boom}}, []string{}},
		{"no code entered", Sources{Plus: fakePlus{}}, []string{}},
		{"free month steps left", Sources{Plus: fakePlus{progress: pending(&plus.Progress{Steps: steps(true, false, false)})}}, []string{StepFreeMonth}},
		{"free month steps all done, waiting", Sources{Plus: fakePlus{progress: pending(&plus.Progress{Steps: steps(true, true, true)})}}, []string{}},
		{"free month earned", Sources{Plus: fakePlus{progress: &plus.Progress{State: string(plus.StateQualified), Steps: steps(true, true, true)}}}, []string{}},
		{"invite expired", Sources{Plus: fakePlus{progress: &plus.Progress{State: string(plus.StateExpired), Steps: steps(true, false)}}}, []string{}},
		{
			"every rule at once, in registry order",
			Sources{
				Plus:      fakePlus{has: true, progress: pending(&plus.Progress{Steps: steps(false)})},
				Autopilot: fakeAutopilot{}, Imports: fakeImports{open: 2},
			},
			[]string{StepAutopilotSetup, StepImportReview, StepFreeMonth},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(NewService(Registry(tc.sources), nil).Pending(context.Background(), planner))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Pending() = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestRulePaths(t *testing.T) {
	rules := Registry(Sources{Plus: fakePlus{}, Autopilot: fakeAutopilot{}, Imports: fakeImports{}})
	want := map[string][]string{
		StepAutopilotSetup: {RowHousehold, RowPlanning, RowAutopilotPreferences},
		StepImportReview:   {RowHousehold, RowPlanning, RowImportReview},
		StepFreeMonth:      {RowHousehold, RowGiveFreeMonth},
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if seen[r.ID] {
			t.Errorf("rule %s is registered twice", r.ID)
		}
		seen[r.ID] = true
		if !reflect.DeepEqual(r.Path, want[r.ID]) {
			t.Errorf("%s path = %v; want %v", r.ID, r.Path, want[r.ID])
		}
		if r.Permission == "" || !slices.Contains(households.AllPermissions(), r.Permission) {
			t.Errorf("%s permission %q isn't a household permission", r.ID, r.Permission)
		}
	}
	if len(seen) != len(want) {
		t.Errorf("registered %v; want %v", seen, want)
	}
}

// A member without a rule's permission never sees it: someone who can't
// import doesn't get Import Review's dot.
func TestPendingFollowsRole(t *testing.T) {
	rules := []Rule{
		{ID: "anyone", Path: []string{RowHousehold}, Pending: func(context.Context, string) (bool, error) { return true, nil }},
		{ID: "admins", Path: []string{RowHousehold}, Permission: households.PermHouseholdUpdate,
			Pending: func(context.Context, string) (bool, error) { return true, nil }},
	}
	svc := NewService(rules, nil)
	member := households.Membership{HouseholdID: household, Role: households.RoleMember}
	admin := households.Membership{HouseholdID: household, Role: households.RoleAdmin}
	if got := ids(svc.Pending(context.Background(), member)); !reflect.DeepEqual(got, []string{"anyone"}) {
		t.Errorf("member sees %v", got)
	}
	if got := ids(svc.Pending(context.Background(), admin)); !reflect.DeepEqual(got, []string{"anyone", "admins"}) {
		t.Errorf("admin sees %v", got)
	}
}

// --- HTTP ---------------------------------------------------------------------

type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

type fakeAuthorizer map[string]households.Role

func (f fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	role, ok := f[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	m := households.Membership{HouseholdID: householdID, UserID: userID, Role: role}
	if !role.Can(perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

func TestHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: NewService(Registry(Sources{
			Plus: fakePlus{has: true}, Autopilot: fakeAutopilot{}, Imports: fakeImports{open: 1},
		}), nil),
		Authorizer: fakeAuthorizer{household + "/planner": households.RoleMember},
		Tokens:     fakeTokens{},
	}).Mount)
	get := func(user, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/households/"+id+"/next-steps", nil)
		if user != "" {
			req.Header.Set("Authorization", "Bearer token-"+user)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	rec := get("planner", household)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Items []struct {
			ID   string   `json:"id"`
			Path []string `json:"path"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 || body.Items[0].ID != StepAutopilotSetup ||
		!reflect.DeepEqual(body.Items[0].Path, []string{"household", "planning", "autopilotPreferences"}) ||
		body.Items[1].ID != StepImportReview {
		t.Errorf("body = %s", rec.Body)
	}

	if rec := get("", household); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d", rec.Code)
	}
	if rec := get("stranger", household); rec.Code != http.StatusNotFound {
		t.Errorf("not a member: status = %d", rec.Code)
	}
}

// Nothing pending is an empty list, not null, so the app clears every dot.
func TestHandlerEmptyIsAList(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service:    NewService(nil, nil),
		Authorizer: fakeAuthorizer{household + "/planner": households.RoleMember},
		Tokens:     fakeTokens{},
	}).Mount)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/households/"+household+"/next-steps", nil)
	req.Header.Set("Authorization", "Bearer token-planner")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if got := strings.TrimSpace(rec.Body.String()); got != `{"items":[]}` {
		t.Errorf("body = %s", got)
	}
}
