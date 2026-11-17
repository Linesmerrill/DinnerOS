package reviewaccount

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/cuisine"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

func TestSeedIsFiveOfOurOwnRecipes(t *testing.T) {
	seed, err := LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	if seed.Household != "Our Kitchen" || len(seed.Recipes) != 5 {
		t.Fatalf("seed = %q with %d recipes; want Our Kitchen with 5", seed.Household, len(seed.Recipes))
	}
	names := map[string]bool{}
	for _, r := range seed.Recipes {
		if names[r.Name] {
			t.Errorf("%s appears twice", r.Name)
		}
		names[r.Name] = true
		if r.Servings != 2 || r.PrepMinutes <= 0 || r.TotalMinutes < r.PrepMinutes {
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
		if d := r.Draft(); !d.CuisinesChosen || len(d.Ingredients) != len(r.Ingredients) || d.SourceURL != "" || d.ImageURL != "" {
			t.Errorf("%s: draft %+v; want a typed recipe with its cuisine chosen and no link or photo", r.Name, d)
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
	saved  map[string][]string
	failAt int
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
	if got := rs.saved[hh.byUser["u1"][0].Household.ID]; len(got) != 5 {
		t.Errorf("saved %v; want the 5 recipes once", got)
	}

	// A household the reviewer already has (say, joined by invite) is left alone.
	hh.byUser["u2"] = []households.UserHousehold{{Household: households.Household{ID: "existing"}}}
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
	if len(hh.byUser["u1"]) != 1 || len(rs.saved[hh.byUser["u1"][0].Household.ID]) != 5 {
		t.Errorf("retry: households %v; want one with all 5 recipes", hh.byUser["u1"])
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
