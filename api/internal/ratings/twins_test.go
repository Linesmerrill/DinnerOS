package ratings

import (
	"context"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

const hhC = "66e5a1f2c3b4a5d6e7f80c01"

type fakeMemberships map[string][]households.UserHousehold

func (f fakeMemberships) ListForUser(_ context.Context, userID string) ([]households.UserHousehold, error) {
	return f[userID], nil
}

// fakeMatcher knows tacos in hhA is the same dish as a copy in hhB.
type fakeMatcher map[string]string

func (f fakeMatcher) SameRecipeIn(_ context.Context, fromHouseholdID, recipeID, householdID string) (string, error) {
	return f[fromHouseholdID+"/"+recipeID+"@"+householdID], nil
}

func TestARatingFollowsTheDishIntoTheMembersOtherHouseholds(t *testing.T) {
	env := newTestEnv(t)
	const bTacos = "66e5a1f2c3b4a5d6e7f80b12"
	in := func(id string, role households.Role) households.UserHousehold {
		return households.UserHousehold{Household: households.Household{ID: id}, Membership: households.Membership{HouseholdID: id, UserID: userAda, Role: role}}
	}
	env.svc.memberships = fakeMemberships{userAda: {in(hhA, households.RoleMember), in(hhB, households.RoleMember), in(hhC, "guest")}}
	env.svc.matcher = fakeMatcher{
		hhA + "/" + tacos + "@" + hhB: bTacos,
		// A guest can't rate, so hhC gets nothing even though it has the dish.
		hhA + "/" + tacos + "@" + hhC: "66e5a1f2c3b4a5d6e7f80c12",
	}

	mustRate(t, env, member(hhA, userAda), tacos, RateInput{Score: 5})
	if i := env.store.find(hhB, bTacos, userAda); i < 0 || env.store.ratings[i].Score != 5 {
		t.Fatalf("hhB copy not rated: %+v", env.store.ratings)
	}
	if env.store.find(hhC, "66e5a1f2c3b4a5d6e7f80c12", userAda) >= 0 {
		t.Error("rated in a household where the member is a guest")
	}
	// One event, in the household where the member actually rated it.
	if got := env.recorder.recorded(); len(got) != 1 || got[0].HouseholdID != hhA {
		t.Errorf("events = %+v", got)
	}
	// A dish the other household doesn't have stays put.
	mustRate(t, env, member(hhA, userAda), curry, RateInput{Score: 3})
	if env.store.count() != 3 {
		t.Errorf("ratings = %d, want 3", env.store.count())
	}

	if err := env.svc.Remove(context.Background(), member(hhA, userAda), tacos); err != nil {
		t.Fatal(err)
	}
	if env.store.find(hhB, bTacos, userAda) >= 0 {
		t.Error("removing a rating left the copy in hhB")
	}
}
