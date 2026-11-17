package nextsteps

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// The rules over the real services on Mongo: a household with Plus on the
// house and no Autopilot has one step, and saving a profile clears it.
func TestIntegrationAutopilotStepClearsOnSave(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, plus.CompIndexes()...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	plusService := plus.NewService(plus.NewMongoStore(db), true).WithComps(plus.NewMongoComps(db))
	autopilotStore := recommendations.NewMongoStore(db)
	svc := NewService(Registry(Sources{
		Plus:      plusService,
		Autopilot: recommendations.NewService(recommendations.ServiceOptions{Store: autopilotStore}),
		Imports:   recipes.NewService(recipes.NewMongoStore(db)),
	}), nil)
	m := households.Membership{HouseholdID: household, Role: households.RoleAdmin}

	if got := ids(svc.Pending(ctx, m)); len(got) != 0 {
		t.Fatalf("without Plus = %v; want nothing", got)
	}
	comps := plus.NewMongoComps(db)
	if _, err := comps.Grant(ctx, plus.Comp{HouseholdID: household, Reason: plus.CompReasonBeta, GrantedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if got := ids(svc.Pending(ctx, m)); !reflect.DeepEqual(got, []string{StepAutopilotSetup}) {
		t.Fatalf("with Plus = %v; want Autopilot setup", got)
	}
	profile := recommendations.DefaultProfile(household)
	profile.CreatedBy, profile.UpdatedBy = "66e5a1f2c3b4a5d6e7f80b02", "66e5a1f2c3b4a5d6e7f80b02"
	if _, err := autopilotStore.SaveProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	if got := ids(svc.Pending(ctx, m)); len(got) != 0 {
		t.Errorf("after setup = %v; want nothing", got)
	}
}
