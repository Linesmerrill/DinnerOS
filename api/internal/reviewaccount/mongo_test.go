package reviewaccount

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

const (
	mongoReviewEmail    = "review@example.test"
	mongoReviewPassword = "plain-test-password-0123"
)

// TestReviewSignInWithMongo signs the review account in through the real
// handler, services and stores: one household of our five recipes, no
// starter library, no Plus on the house, and nothing new on later sign-ins.
func TestReviewSignInWithMongo(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, slices.Concat(users.Indexes(), auth.Indexes(), households.Indexes(), recipes.Indexes(), plus.CompIndexes())...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	userService := users.NewService(users.NewMongoStore(db))
	tokens, err := auth.NewTokenService(auth.NewMongoSessionStore(db), auth.TokenOptions{SigningKey: []byte("0123456789abcdef0123456789abcdef-test-key")})
	if err != nil {
		t.Fatal(err)
	}
	recipeStore := recipes.NewMongoStore(db)
	recipeService := recipes.NewService(recipeStore)

	// A starter library every other new household gets.
	const starterSource = "6aa940907895ff325b652912"
	if err := recipeStore.SaveRecipes(ctx, starterSource, []recipes.Recipe{{Source: recipes.SourceHelloFresh, SourceRecipeID: "s1", Name: "Starter Dish"}}); err != nil {
		t.Fatal(err)
	}
	starter := recipes.NewStarter(recipes.StarterOptions{Service: recipeService, SourceHouseholdID: starterSource})
	householdService := households.NewService(households.ServiceOptions{
		Store: households.NewMongoStore(db), Users: userService, OnCreated: starter,
	})
	starter.SkipWhen(SkipStarter(householdService, userService))
	seeder, err := NewSeeder(householdService, recipeService, nil)
	if err != nil {
		t.Fatal(err)
	}
	review := auth.NewReviewSignIn(auth.ReviewOptions{Email: mongoReviewEmail, Password: mongoReviewPassword, Seeder: seeder})
	r := chi.NewRouter()
	r.Route("/api/v1", auth.NewHandler(auth.HandlerOptions{
		Service: auth.NewService(auth.ServiceOptions{Users: userService, Tokens: tokens}),
		Tokens:  tokens, Review: review,
	}).Mount)

	signIn := func(password string) (*httptest.ResponseRecorder, auth.SessionResponse) {
		t.Helper()
		body := fmt.Sprintf(`{"email":%q,"password":%q}`, mongoReviewEmail, password)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/review-sign-in", strings.NewReader(body)))
		var resp auth.SessionResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec, resp
	}

	if rec, _ := signIn("wrong-password-0123456"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status %d", rec.Code)
	}
	rec, first := signIn(mongoReviewPassword)
	if rec.Code != http.StatusOK || !first.IsNewUser {
		t.Fatalf("first sign-in: status %d, body %s", rec.Code, rec.Body.String())
	}
	for range 2 {
		if rec, again := signIn(mongoReviewPassword); rec.Code != http.StatusOK || again.User.ID != first.User.ID || again.IsNewUser {
			t.Fatalf("later sign-in: status %d, %+v", rec.Code, again)
		}
	}
	if err := starter.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	list, err := householdService.ListForUser(ctx, first.User.ID)
	if err != nil || len(list) != 1 || list[0].Household.Name != "Our Kitchen" || list[0].Membership.Role != households.RoleAdmin {
		t.Fatalf("households = %+v, %v; want one Our Kitchen the reviewer runs", list, err)
	}
	hid := list[0].Household.ID
	stored, err := recipeStore.ListRecipesAfter(ctx, hid, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, rc := range stored {
		names = append(names, rc.Name)
		if rc.Source != recipes.SourceManual {
			t.Errorf("%s has source %q; want a typed recipe", rc.Name, rc.Source)
		}
	}
	seed, _ := LoadSeed()
	if len(names) != len(seed.Recipes) || slices.Contains(names, "Starter Dish") {
		t.Errorf("recipes = %v; want the %d seeded recipes and no starter library", names, len(seed.Recipes))
	}

	// No Plus on the house: the reviewer sees the normal paywall.
	plusService := plus.NewService(plus.NewMongoStore(db), true).WithComps(plus.NewMongoComps(db))
	if st, err := plusService.Status(ctx, hid); err != nil || st.Active || st.Comp != nil {
		t.Errorf("Plus status = %+v, %v; want not active and no comp", st, err)
	}

	// Another new household still gets the starter library.
	other, _, err := userService.FindOrCreateByIdentity(ctx, users.VerifiedIdentity{Provider: users.ProviderApple, Subject: "friend"})
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := householdService.Create(ctx, other.ID, households.CreateInput{Name: "Friends", TimeZone: "America/Denver"})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := starter.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}
	if got, _ := recipeStore.ListRecipesAfter(ctx, h.ID, "", 10); len(got) != 1 {
		t.Errorf("another household got %d starter recipes, want 1", len(got))
	}
}
