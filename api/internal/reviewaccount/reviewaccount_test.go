package reviewaccount

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/cuisine"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/photosearch"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

func TestSeedIsEightOfOurOwnRecipesWithPhotos(t *testing.T) {
	seed, err := LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	if seed.Household != "Our Kitchen" || len(seed.Recipes) != 8 {
		t.Fatalf("seed = %q with %d recipes; want Our Kitchen with 8", seed.Household, len(seed.Recipes))
	}
	if len(seed.Plan) != 7 {
		t.Errorf("plan = %v; want a dinner every night", seed.Plan)
	}
	photos := map[string]bool{}
	names := map[string]bool{}
	for _, r := range seed.Recipes {
		if names[r.Name] {
			t.Errorf("%s appears twice", r.Name)
		}
		names[r.Name] = true
		if r.Servings != 4 || r.PrepMinutes <= 0 || r.TotalMinutes < r.PrepMinutes {
			t.Errorf("%s: servings %d, prep %d, total %d", r.Name, r.Servings, r.PrepMinutes, r.TotalMinutes)
		}
		if len(r.Cuisines) != 1 || !cuisine.IsStandard(r.Cuisines[0]) {
			t.Errorf("%s: cuisines %v; want one from the standard list", r.Name, r.Cuisines)
		}
		if len(r.Steps) < 4 || len(r.Ingredients) < 5 {
			t.Errorf("%s: %d steps and %d ingredients; want a real recipe", r.Name, len(r.Steps), len(r.Ingredients))
		}
		for _, in := range r.Ingredients {
			if in.Unit != "" {
				if _, err := ingredients.LookupUnit(in.Unit); err != nil {
					t.Errorf("%s: %s has unit %q: %v", r.Name, in.Name, in.Unit, err)
				}
			}
		}
		text := strings.ToLower(r.Name + r.Headline + strings.Join(r.Steps, " "))
		for _, banned := range []string{"hellofresh", "hello fresh", "http", "—"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s mentions %q", r.Name, banned)
			}
		}
		// A free Unsplash photo of the dish, hotlinked, with its photographer.
		if !photosearch.IsUnsplashImageURL(r.Photo.ImageURL) || r.Photo.Photographer == "" ||
			!strings.HasPrefix(r.Photo.ProfileURL, "https://unsplash.com/@") ||
			!strings.HasPrefix(r.Photo.Page, "https://unsplash.com/photos/") || photos[r.Photo.ImageURL] {
			t.Errorf("%s: photo %+v; want its own credited Unsplash photo", r.Name, r.Photo)
		}
		photos[r.Photo.ImageURL] = true
		if c := r.Photo.Credit(); !strings.Contains(c.ProfileURL, "utm_source=dinneros") {
			t.Errorf("%s: credit %+v; want Unsplash's referral tags", r.Name, c)
		}
		if d := r.Draft(); !d.CuisinesChosen || len(d.Ingredients) != len(r.Ingredients) || d.SourceURL != "" || d.ImageURL != r.Photo.ImageURL {
			t.Errorf("%s: draft %+v; want a typed recipe with its cuisine chosen, its photo and no link", r.Name, d)
		}
	}
}

// --- Fakes ------------------------------------------------------------------------

type fakeHouseholds struct {
	byUser  map[string][]households.UserHousehold
	created int
	deleted []string
}

func (f *fakeHouseholds) ListForUser(_ context.Context, userID string) ([]households.UserHousehold, error) {
	return f.byUser[userID], nil
}

func (f *fakeHouseholds) Create(_ context.Context, userID string, in households.CreateInput) (households.Household, households.Membership, error) {
	f.created++
	h := households.Household{ID: fmt.Sprintf("%024d", f.created), Name: in.Name, CreatedBy: userID}
	m := households.Membership{HouseholdID: h.ID, UserID: userID, Role: households.RoleAdmin}
	f.byUser[userID] = append(f.byUser[userID], households.UserHousehold{Household: h, Membership: m})
	return h, m, nil
}

func (f *fakeHouseholds) DeleteEmptiedHousehold(_ context.Context, householdID, userID string) error {
	f.deleted = append(f.deleted, householdID)
	f.byUser[userID] = nil
	return nil
}

func (f *fakeHouseholds) GetHousehold(_ context.Context, id string) (households.Household, error) {
	for _, list := range f.byUser {
		for _, uh := range list {
			if uh.Household.ID == id {
				return uh.Household, nil
			}
		}
	}
	return households.Household{}, households.ErrNotFound
}

type fakeRecipes struct {
	saved   map[string][]string
	credits map[string]string
	deleted []string
	failAt  int
}

func (f *fakeRecipes) KeepPhotoCredit(_ context.Context, imageURL string, c photosearch.Credit) error {
	if f.credits == nil {
		f.credits = map[string]string{}
	}
	f.credits[imageURL] = c.Name
	return nil
}

// Recipe IDs are "<household>/<name>".
func (f *fakeRecipes) List(_ context.Context, householdID string, _ recipes.ListQuery) (recipes.ListPage, error) {
	var page recipes.ListPage
	for _, name := range f.saved[householdID] {
		page.Items = append(page.Items, recipes.RecipeSummary{ID: householdID + "/" + name, Name: name})
	}
	return page, nil
}

func (f *fakeRecipes) Delete(_ context.Context, householdID, _ string, recipeID string) (recipes.DeleteResult, error) {
	name := strings.TrimPrefix(recipeID, householdID+"/")
	f.saved[householdID] = slices.DeleteFunc(f.saved[householdID], func(n string) bool { return n == name })
	f.deleted = append(f.deleted, name)
	return recipes.DeleteResult{}, nil
}

type fakePlanner struct {
	weeks map[string][]planning.NewEntry
}

func (f *fakePlanner) Get(_ context.Context, householdID, week string) (planning.Plan, error) {
	var p planning.Plan
	for range f.weeks[householdID+" "+week] {
		p.Entries = append(p.Entries, planning.Entry{})
	}
	return p, nil
}

func (f *fakePlanner) AddEntries(_ context.Context, householdID, _ string, week string, in []planning.NewEntry) (planning.Plan, []planning.Entry, error) {
	f.weeks[householdID+" "+week] = append(f.weeks[householdID+" "+week], in...)
	return planning.Plan{}, nil, nil
}

func (f *fakeRecipes) CreateFromDraft(_ context.Context, householdID string, d recipes.Draft, fromURL bool) (recipes.Recipe, error) {
	if fromURL {
		return recipes.Recipe{}, errors.New("seeded recipes are typed, not read from a page")
	}
	if f.failAt > 0 && len(f.saved[householdID])+1 == f.failAt {
		return recipes.Recipe{}, errors.New("write failed")
	}
	f.saved[householdID] = append(f.saved[householdID], d.Name)
	return recipes.Recipe{Name: d.Name, HouseholdID: householdID}, nil
}

type fakeIdentities map[string][]users.AuthIdentity

func (f fakeIdentities) ListIdentities(_ context.Context, userID string) ([]users.AuthIdentity, error) {
	return f[userID], nil
}

func TestSeederSeedsOnceAndOnlyWithoutAHousehold(t *testing.T) {
	hh := &fakeHouseholds{byUser: map[string][]households.UserHousehold{}}
	rs := &fakeRecipes{saved: map[string][]string{}}
	s, err := NewSeeder(hh, rs, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for range 3 {
		if err := s.ReviewAccountSignedIn(ctx, "u1"); err != nil {
			t.Fatal(err)
		}
	}
	if hh.created != 1 || len(hh.byUser["u1"]) != 1 || hh.byUser["u1"][0].Household.Name != "Our Kitchen" {
		t.Fatalf("households = %+v (created %d); want one Our Kitchen", hh.byUser["u1"], hh.created)
	}
	if got := rs.saved[hh.byUser["u1"][0].Household.ID]; len(got) != 8 || len(rs.credits) != 8 {
		t.Errorf("saved %v with %d credits; want the 8 recipes once, each photo credited", got, len(rs.credits))
	}

	// A household the reviewer already has (say, joined by invite) is left alone.
	hh.byUser["u2"] = []households.UserHousehold{{Household: households.Household{ID: "existing", CreatedBy: "someone"}}}
	if err := s.ReviewAccountSignedIn(ctx, "u2"); err != nil || hh.created != 1 || len(rs.saved["existing"]) != 0 {
		t.Errorf("u2: err %v, created %d, saved %v; want nothing new", err, hh.created, rs.saved["existing"])
	}
}

func TestSeederRemovesAHalfSeededHousehold(t *testing.T) {
	hh := &fakeHouseholds{byUser: map[string][]households.UserHousehold{}}
	rs := &fakeRecipes{saved: map[string][]string{}, failAt: 3}
	s, err := NewSeeder(hh, rs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewAccountSignedIn(context.Background(), "u1"); err == nil {
		t.Fatal("want an error when a recipe can't be saved")
	}
	if len(hh.deleted) != 1 || len(hh.byUser["u1"]) != 0 {
		t.Fatalf("deleted %v, left %v; want the new household removed", hh.deleted, hh.byUser["u1"])
	}
	rs.failAt = 0
	if err := s.ReviewAccountSignedIn(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if len(hh.byUser["u1"]) != 1 || len(rs.saved[hh.byUser["u1"][0].Household.ID]) != 8 {
		t.Errorf("retry: households %v; want one with all 8 recipes", hh.byUser["u1"])
	}
}

// The household seeded by build 116 gets the new recipes in place of the
// earlier five; what the reviewer added stays.
func TestSeederSwapsTheEarlierSeed(t *testing.T) {
	h := households.Household{ID: "h1", CreatedBy: "u1", TimeZone: "America/Los_Angeles"}
	hh := &fakeHouseholds{byUser: map[string][]households.UserHousehold{"u1": {{Household: h}}}}
	rs := &fakeRecipes{saved: map[string][]string{"h1": append(slices.Clone(earlierSeed), "Grandma's Soup")}}
	s, err := NewSeeder(hh, rs, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.ReviewAccountSignedIn(context.Background(), "u1"); err != nil {
			t.Fatal(err)
		}
	}
	got := rs.saved["h1"]
	if len(got) != 9 || !slices.Contains(got, "Grandma's Soup") || slices.Contains(got, earlierSeed[0]) {
		t.Errorf("recipes = %v; want the 8 new ones and the reviewer's own", got)
	}
	if len(rs.deleted) != len(earlierSeed) {
		t.Errorf("deleted %v; want the earlier seed's five", rs.deleted)
	}
	// A household without the earlier recipes is left as it is.
	rs.saved["h1"] = []string{"Grandma's Soup"}
	if err := s.ReviewAccountSignedIn(context.Background(), "u1"); err != nil || len(rs.saved["h1"]) != 1 {
		t.Errorf("recipes = %v, %v; want the reviewer's choice kept", rs.saved["h1"], err)
	}
}

func TestSeederPlansTheCurrentWeekWhenEmpty(t *testing.T) {
	hh := &fakeHouseholds{byUser: map[string][]households.UserHousehold{}}
	rs := &fakeRecipes{saved: map[string][]string{}}
	pl := &fakePlanner{weeks: map[string][]planning.NewEntry{}}
	s, err := NewSeeder(hh, rs, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.WithPlanner(pl)
	// Tuesday Nov 17, 2026 in Cupertino; the fake household starts its week on
	// Monday (no week start set), so the week is 2026-W47 from Monday the 16th.
	s.now = func() time.Time { return time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	for range 3 {
		if err := s.ReviewAccountSignedIn(ctx, "u1"); err != nil {
			t.Fatal(err)
		}
	}
	hid := hh.byUser["u1"][0].Household.ID
	got := pl.weeks[hid+" 2026-W47"]
	if len(got) != 7 || len(pl.weeks) != 1 {
		t.Fatalf("weeks = %v; want 7 dinners in 2026-W47 once", pl.weeks)
	}
	if got[0].Day != "mon" || got[6].Day != "sun" || got[0].RecipeID != hid+"/"+s.seed.Plan[0] || got[0].Servings != 4 {
		t.Errorf("first = %+v, last = %+v; want the seed's plan from Monday, for 4", got[0], got[6])
	}

	// A week later, the new week is planned too.
	s.now = func() time.Time { return time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC) }
	if err := s.ReviewAccountSignedIn(ctx, "u1"); err != nil || len(pl.weeks[hid+" 2026-W48"]) != 7 {
		t.Errorf("next week: %v, %v", pl.weeks[hid+" 2026-W48"], err)
	}
}

func TestWeekDaysStartOnTheFirstDay(t *testing.T) {
	if got := weekDays(planning.Sunday); got[0] != planning.Sunday || got[6] != planning.Saturday || len(got) != 7 {
		t.Errorf("weekDays(sun) = %v", got)
	}
}

func TestSkipStarterOnlyForTheReviewAccount(t *testing.T) {
	hh := &fakeHouseholds{byUser: map[string][]households.UserHousehold{
		"reviewer": {{Household: households.Household{ID: "r", CreatedBy: "reviewer"}}},
		"friend":   {{Household: households.Household{ID: "f", CreatedBy: "friend"}}},
	}}
	ids := fakeIdentities{
		"reviewer": {{Provider: users.ProviderReview}},
		"friend":   {{Provider: users.ProviderApple}},
	}
	skip := SkipStarter(hh, ids)
	if !skip(context.Background(), "r") {
		t.Error("the review account's household should get no starter library")
	}
	if skip(context.Background(), "f") || skip(context.Background(), "missing") {
		t.Error("other households should get the starter library")
	}
}
