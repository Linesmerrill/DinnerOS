package shopping

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// prepFixture is the story's setup: the store's only ground beef is an
// eight-pound pack, the week's two beef meals are on Tuesday and Thursday,
// and nothing has been prepped yet.
func prepFixture(t *testing.T) (*fixture, Handoff) {
	t.Helper()
	f := newFixture(t)
	f.svc.frozen, f.svc.freezer = f.pantry, f.pantry
	if _, err := f.svc.UpdateSettings(f.ctx, f.actor, "walmart", "5435"); err != nil {
		t.Fatal(err)
	}
	plan, err := f.plans.Get(f.ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	days := map[string]planning.Day{"Beef Tacos": "tue", "Beef Chili": "thu"}
	for _, e := range plan.Entries {
		day, ok := days[e.RecipeName]
		if !ok {
			continue
		}
		if _, err := f.plans.UpdateEntry(f.ctx, testHousehold, testWeek, e.ID, planning.EntryChanges{Day: &day}); err != nil {
			t.Fatal(err)
		}
	}
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "128", Unit: "oz"})
	h, _, err := f.svc.CreateHandoff(f.ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}
	return f, h
}

func prepCard(t *testing.T, s PrepSession, name string) PrepCard {
	t.Helper()
	for _, c := range s.Cards {
		if c.Pack.Name == name {
			return c
		}
	}
	t.Fatalf("no prep card for %s in %+v", name, s.Cards)
	return PrepCard{}
}

// TestIntegrationPrepSession follows the Sunday: the box arrives, the app
// says how many pieces to cut the beef into and what to keep out for
// Tuesday and Thursday, and finishing the card puts the rest away.
func TestIntegrationPrepSession(t *testing.T) {
	f, h := prepFixture(t)
	ctx := f.ctx

	session, err := f.svc.PrepSession(ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatalf("PrepSession: %v", err)
	}
	if session.State != PrepReady || session.Pending != 1 || len(session.Cards) != 1 {
		t.Fatalf("session = %s, %d pending, %+v", session.State, session.Pending, session.Cards)
	}
	card := prepCard(t, session, "Ground Beef")
	switch {
	case card.ID != h.ID+":"+card.LineID:
		t.Errorf("card ID = %q, want the handoff and line", card.ID)
	case card.Status != PrepPending:
		t.Errorf("status = %s, want pending", card.Status)
	case card.Portions.Reserved != "36" || card.Portions.Surplus != "92":
		t.Errorf("reserved %q / surplus %q, want 36 / 92", card.Portions.Reserved, card.Portions.Surplus)
	case card.Portions.Meals != 2 || card.Portions.TypicalMeal != "18" || card.Portions.Basis != BasisMeal:
		t.Errorf("meals = %d, typical = %q (%s); want 2 meals of 18 oz",
			card.Portions.Meals, card.Portions.TypicalMeal, card.Portions.Basis)
	case card.Portions.Portions != 5 || card.Portions.PortionSize != "18" || card.Portions.Leftover != "2":
		t.Errorf("portions = %d × %q, leftover %q; want 5 × 18 oz dinners and 2 oz over",
			card.Portions.Portions, card.Portions.PortionSize, card.Portions.Leftover)
	case card.Portions.Thaw.Hours != 6:
		t.Errorf("thaw = %d hours, want 6 for one 18 oz portion", card.Portions.Thaw.Hours)
	}
	// The card names the meals the reserve is for, by day, and promises only
	// the reminder that actually exists.
	if len(card.Meals) != 2 || card.Meals[0].Day != "tue" || card.Meals[1].Day != "thu" {
		t.Fatalf("meals = %+v, want Tuesday's tacos then Thursday's chili", card.Meals)
	}
	if !strings.Contains(card.Instruction, "Tuesday's Beef Tacos and Thursday's Beef Chili") {
		t.Errorf("instruction = %q", card.Instruction)
	}
	if card.Reminder != PrepReminderThaw || !strings.Contains(card.ReminderText, "Tuesday") {
		t.Errorf("reminder = %s: %q", card.Reminder, card.ReminderText)
	}

	// The member cuts it into four instead of five.
	session, done, err := f.svc.CompletePrepCard(ctx, f.actor, testWeek, card.ID, PrepInput{Portions: 4})
	if err != nil {
		t.Fatalf("CompletePrepCard: %v", err)
	}
	if done.Status != PrepDone || done.FrozenPortions != 4 || done.FrozenItemID == "" {
		t.Fatalf("finished card = %+v", done)
	}
	if done.Portions.Portions != 4 || done.Portions.PortionSize != "18" || done.Portions.Thaw.Hours != 6 {
		t.Errorf("four portions = %d × %q, %d hours; want 4 × 18 oz, 6 hours",
			done.Portions.Portions, done.Portions.PortionSize, done.Portions.Thaw.Hours)
	}
	if session.State != PrepFinished || session.Pending != 0 || session.Done != 1 {
		t.Errorf("session after = %s, %d pending, %d done", session.State, session.Pending, session.Done)
	}

	// The 36 oz the week needs stayed out of the freezer, and so did what is
	// left after four whole dinners: only 4 × 18 oz was sealed.
	stock, err := f.pantry.FrozenStock(ctx, testHousehold)
	if err != nil {
		t.Fatal(err)
	}
	if len(stock) != 1 || stock[0].Item.Quantity != "72" || stock[0].Item.Portions != 4 {
		t.Fatalf("freezer = %+v, want one 72 oz item in four portions", stock)
	}

	// Redoing the card seals nothing twice and does not rewrite the count:
	// the bags in the drawer say four.
	_, again, err := f.svc.CompletePrepCard(ctx, f.actor, testWeek, card.ID, PrepInput{Portions: 2})
	if err != nil {
		t.Fatalf("second CompletePrepCard: %v", err)
	}
	if again.FrozenPortions != 4 {
		t.Errorf("redone card = %d portions, want the 4 already in the freezer", again.FrozenPortions)
	}
	stock, err = f.pantry.FrozenStock(ctx, testHousehold)
	if err != nil {
		t.Fatal(err)
	}
	if len(stock) != 1 || stock[0].Item.Quantity != "72" || stock[0].Item.Portions != 4 {
		t.Fatalf("freezer after redoing = %+v, want the same one item", stock)
	}
}

// A member who skips a card leaves it in the list, and the pantry untouched.
func TestIntegrationPrepSkipAndResume(t *testing.T) {
	f, _ := prepFixture(t)
	ctx := f.ctx
	session, err := f.svc.PrepSession(ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	card := prepCard(t, session, "Ground Beef")
	session, skipped, err := f.svc.SkipPrepCard(ctx, f.actor, testWeek, card.ID)
	if err != nil {
		t.Fatalf("SkipPrepCard: %v", err)
	}
	if skipped.Status != PrepSkipped || session.Skipped != 1 || session.State != PrepFinished {
		t.Fatalf("skipped = %s, session = %s (%d skipped)", skipped.Status, session.State, session.Skipped)
	}
	if stock, err := f.pantry.FrozenStock(ctx, testHousehold); err != nil || len(stock) != 0 {
		t.Fatalf("freezer = %+v, %v; skipping must record nothing", stock, err)
	}

	// Thursday morning. The household comes back, and the card is where they
	// left it — but Tuesday has gone, so the copy names Thursday instead.
	f.advance(36 * time.Hour)
	session, err = f.svc.PrepSession(ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	card = prepCard(t, session, "Ground Beef")
	if card.Status != PrepSkipped {
		t.Errorf("status on the way back = %s, want the skip remembered", card.Status)
	}
	if len(card.Meals) != 2 || !card.Meals[0].Past || card.Meals[1].Past {
		t.Errorf("meals mid-week = %+v, want Tuesday past and Thursday still ahead", card.Meals)
	}
	if card.Reminder != PrepReminderThaw || !strings.Contains(card.ReminderText, "Thursday") {
		t.Errorf("reminder mid-week = %s: %q, want Thursday named", card.Reminder, card.ReminderText)
	}
	// Skipping is not final: the same card can still be done.
	if _, done, err := f.svc.CompletePrepCard(ctx, f.actor, testWeek, card.ID, PrepInput{}); err != nil ||
		done.Status != PrepDone {
		t.Errorf("finishing a skipped card = %+v, %v", done, err)
	}
}

// A remainder sealed some other way — the old bulk-pack sheet, another
// member's phone — is already done. A checklist must not ask for it again.
func TestIntegrationPrepCountsAnAlreadyFrozenRemainder(t *testing.T) {
	f, h := prepFixture(t)
	ctx := f.ctx
	beef := lineFor(t, h.Proposal, "Ground Beef")
	if _, err := f.pantry.Freeze(ctx, f.actor, pantry.FreezeInput{
		Name: "Ground Beef", Quantity: "92", Unit: "oz", Portions: 4,
		Source: &pantry.FreezeSource{Provider: "walmart", HandoffID: h.ID, LineID: beef.ID},
	}); err != nil {
		t.Fatal(err)
	}
	session, err := f.svc.PrepSession(ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	card := prepCard(t, session, "Ground Beef")
	if card.Status != PrepDone || !card.Pack.Frozen || session.Pending != 0 {
		t.Errorf("card = %s (frozen %v), %d pending", card.Status, card.Pack.Frozen, session.Pending)
	}
}

// A week that bought nothing oversized has a session, and it says so.
func TestIntegrationPrepNothingToDo(t *testing.T) {
	f := newFixture(t)
	f.svc.frozen, f.svc.freezer = f.pantry, f.pantry
	if _, err := f.svc.UpdateSettings(f.ctx, f.actor, "walmart", "5435"); err != nil {
		t.Fatal(err)
	}
	// A 36 oz pack for a 36 oz week leaves nothing to put away.
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "36", Unit: "oz"})
	if _, _, err := f.svc.CreateHandoff(f.ctx, f.actor, testWeek, "walmart", MatchInput{}); err != nil {
		t.Fatal(err)
	}
	session, err := f.svc.PrepSession(f.ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if session.State != PrepNothingToDo || len(session.Cards) != 0 || !strings.Contains(session.Headline, "Nothing to prep") {
		t.Errorf("session = %s, %d cards, %q", session.State, len(session.Cards), session.Headline)
	}
	// A week nobody shopped for is the same answer, not an error.
	if s, err := f.svc.PrepSession(f.ctx, testHousehold, "2026-W39"); err != nil || s.State != PrepNothingToDo {
		t.Errorf("unshopped week = %+v, %v", s, err)
	}
	if _, err := f.svc.PrepSession(f.ctx, testHousehold, "2026-W99"); !errors.Is(err, planning.ErrInvalidWeek) {
		t.Errorf("bad week error = %v", err)
	}
}

// Permissions and the wire shape, over HTTP.
func TestIntegrationPrepHTTP(t *testing.T) {
	f, _ := prepFixture(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember, testHousehold + "/" + viewerUser: "viewer"},
	}).Mount)
	do := func(method, path, body, userID string) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}
	base := "/households/" + testHousehold + "/prep/weeks/" + testWeek

	rec, body := do(http.MethodGet, base, "", testUser)
	if rec.Code != http.StatusOK || body["state"] != "ready" || body["pending"] != float64(1) {
		t.Fatalf("GET prep = %d %v", rec.Code, body)
	}
	cards, _ := body["cards"].([]any)
	if len(cards) != 1 {
		t.Fatalf("cards = %v", body["cards"])
	}
	card, _ := cards[0].(map[string]any)
	portions, _ := card["portions"].(map[string]any)
	thaw, _ := portions["thaw"].(map[string]any)
	options, _ := portions["options"].([]any)
	switch {
	case card["status"] != "pending" || card["kind"] != "bulk_pack" || card["freezable"] != true:
		t.Errorf("card = %v", card)
	case portions["portions"] != float64(5) || portions["portionSizeText"] != "18 oz" || portions["basis"] != "meal":
		t.Errorf("portions = %v", portions)
	case thaw["hours"] != float64(6) || thaw["measured"] != true:
		t.Errorf("thaw = %v", thaw)
	case len(options) != 5:
		t.Errorf("%d options, want 5: 92 oz holds five whole 18 oz dinners", len(options))
	case card["reminder"] != "thaw":
		t.Errorf("reminder = %v", card["reminder"])
	case card["imageUrl"] != "":
		// Present and empty, never missing: the fixture's beef has no photo.
		t.Errorf("imageUrl = %#v, want an empty string", card["imageUrl"])
	case card["boughtText"] != "8 lb" || card["neededText"] != "36 oz":
		t.Errorf("bought %v / needed %v, want 8 lb / 36 oz", card["boughtText"], card["neededText"])
	case portions["leftoverText"] != "2 oz":
		t.Errorf("leftoverText = %v, want 2 oz after five 18 oz bags", portions["leftoverText"])
	}
	cardID, _ := card["id"].(string)

	for _, tc := range []struct {
		method, path, body, user string
		status                   int
		code                     string
	}{
		{http.MethodGet, "/households/" + testHousehold + "/prep/weeks/2026-W99", "", testUser, 400, "validation_failed"},
		{http.MethodPost, base + "/cards/" + cardID + "/done", `{}`, viewerUser, 403, "forbidden"},
		{http.MethodPost, base + "/cards/nope/done", `{}`, testUser, 400, "validation_failed"},
		{http.MethodPost, base + "/cards/aaa:bbb/done", `{}`, testUser, 404, "not_found"},
		{http.MethodPost, base + "/cards/" + cardID + "/done", `{"portions":999}`, testUser, 400, "validation_failed"},
		{http.MethodPost, base + "/cards/" + cardID + "/skip", `{}`, viewerUser, 403, "forbidden"},
	} {
		rec, body := do(tc.method, tc.path, tc.body, tc.user)
		errBody, _ := body["error"].(map[string]any)
		if rec.Code != tc.status || errBody["code"] != tc.code {
			t.Errorf("%s %s as %s = %d %v, want %d %s", tc.method, tc.path, tc.user, rec.Code, body, tc.status, tc.code)
		}
	}

	rec, body = do(http.MethodPost, base+"/cards/"+cardID+"/done", `{"portions":4}`, testUser)
	doneCard, _ := body["card"].(map[string]any)
	session, _ := body["session"].(map[string]any)
	if rec.Code != http.StatusOK || doneCard["status"] != "done" || doneCard["frozenPortions"] != float64(4) ||
		session["state"] != "finished" {
		t.Fatalf("done = %d %v", rec.Code, body)
	}
	if rec, body = do(http.MethodGet, base, "", testUser); rec.Code != 200 || body["done"] != float64(1) {
		t.Errorf("GET prep after = %d %v", rec.Code, body)
	}
}

// The card shows the ingredient's photo — the same one the recipe screen
// shows on that ingredient's row — and nothing when there isn't one.
func TestIntegrationPrepCardPhoto(t *testing.T) {
	f, _ := prepFixture(t)
	ctx := f.ctx

	session, err := f.svc.PrepSession(ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatalf("PrepSession: %v", err)
	}
	card := prepCard(t, session, "Ground Beef")
	if card.ImageURL != "" {
		t.Fatalf("imageUrl = %q before the ingredient has a photo, want empty", card.ImageURL)
	}
	if len(card.Pack.Recipes) == 0 {
		t.Fatalf("beef line records no recipes: %+v", card.Pack)
	}

	// HelloFresh's ingredient photo, as an import records it on the catalog.
	const photo = "https://img.example.com/ingredients/ground-beef.png"
	oid, err := bson.ObjectIDFromHex(card.Pack.IngredientID())
	if err != nil {
		t.Fatalf("beef ingredient ID %q: %v", card.Pack.IngredientID(), err)
	}
	catalog := f.store.prepCards.Database().Collection(recipes.IngredientsCollection)
	if _, err := catalog.UpdateOne(ctx, bson.D{{Key: "_id", Value: oid}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "imageUrl", Value: photo}}}}); err != nil {
		t.Fatal(err)
	}

	// The recipe screen's ingredient row shows it...
	recipe, err := f.recipes.Get(ctx, testHousehold, card.Pack.Recipes[0].ID)
	if err != nil {
		t.Fatalf("Get recipe: %v", err)
	}
	var onRecipe string
	for _, line := range recipe.Ingredients {
		if line.IngredientID == card.Pack.IngredientID() {
			onRecipe = line.ImageURL
		}
	}
	if onRecipe != photo {
		t.Fatalf("recipe ingredient image = %q, want %q", onRecipe, photo)
	}

	// ...and so does the prep card, as a service value and on the wire.
	session, err = f.svc.PrepSession(ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatalf("PrepSession after: %v", err)
	}
	card = prepCard(t, session, "Ground Beef")
	if card.ImageURL != photo {
		t.Errorf("card imageUrl = %q, want the recipe ingredient's %q", card.ImageURL, photo)
	}
	if got := prepCardResponse(card).ImageURL; got != photo {
		t.Errorf("response imageUrl = %q, want %q", got, photo)
	}
}

// TestIntegrationPrepSmallMeatSurplus is the week that prompted the bag size:
// a 3 lb pack of beef for 36 oz of dinners leaves 12 oz, a quarter of the
// pack. It used to get no card at all. By default a bag is one 18 oz dinner,
// so the card says to throw the 12 oz in; with 6 oz bags it freezes two.
func TestIntegrationPrepSmallMeatSurplus(t *testing.T) {
	f := newFixture(t)
	f.svc.frozen, f.svc.freezer = f.pantry, f.pantry
	if _, err := f.svc.UpdateSettings(f.ctx, f.actor, "walmart", "5435"); err != nil {
		t.Fatal(err)
	}
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "48", Unit: "oz"})
	if _, _, err := f.svc.CreateHandoff(f.ctx, f.actor, testWeek, "walmart", MatchInput{}); err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}
	f.svc.households = orderHouseholds{households.Household{ID: testHousehold, TimeZone: "UTC"}}

	session, err := f.svc.PrepSession(f.ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatalf("PrepSession: %v", err)
	}
	card := prepCard(t, session, "Ground Beef")
	if card.Portions.Portions != 0 || card.Portions.Leftover != "12" {
		t.Fatalf("default = %d bags, %q left; want no bag and 12 oz left", card.Portions.Portions, card.Portions.Leftover)
	}
	if !strings.HasSuffix(card.Instruction, "12 oz left over. Throw it in for a little more protein, or toss it.") {
		t.Errorf("instruction = %q", card.Instruction)
	}

	six := 6
	f.svc.households = orderHouseholds{households.Household{ID: testHousehold, TimeZone: "UTC", FreezeMinOunces: &six}}
	session, err = f.svc.PrepSession(f.ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatalf("PrepSession: %v", err)
	}
	card = prepCard(t, session, "Ground Beef")
	if card.Portions.Portions != 2 || card.Portions.PortionSize != "6" || card.Portions.Leftover != "0" {
		t.Fatalf("6 oz bags = %d × %q, %q left; want 2 × 6 oz", card.Portions.Portions,
			card.Portions.PortionSize, card.Portions.Leftover)
	}
	// The label says what's in the bag and when it went in, so it can be told
	// apart from the other meat in the freezer.
	if len(card.BagLabel) != 3 || card.BagLabel[0] != "Ground Beef, 6 oz" ||
		!strings.HasPrefix(card.BagLabel[1], "Frozen ") || !strings.HasPrefix(card.BagLabel[2], "Best by ") {
		t.Errorf("bag label = %q", card.BagLabel)
	}
	_, done, err := f.svc.CompletePrepCard(f.ctx, f.actor, testWeek, card.ID, PrepInput{})
	if err != nil {
		t.Fatalf("CompletePrepCard: %v", err)
	}
	stock, err := f.pantry.FrozenStock(f.ctx, testHousehold)
	if err != nil {
		t.Fatal(err)
	}
	if len(done.BagLabel) != 0 {
		t.Errorf("a finished card still asks for a label: %q", done.BagLabel)
	}
	if done.FrozenPortions != 2 || len(stock) != 1 || stock[0].Item.Quantity != "12" || stock[0].Item.Portions != 2 {
		t.Fatalf("freezer = %+v, want one 12 oz item in two bags", stock)
	}
}
