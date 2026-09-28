package shopping

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// scriptedChecker answers product checks from a script. An item it has no
// answer for is a network error: unknown, with no stop.
type scriptedChecker struct {
	mu      sync.Mutex
	results map[string]providers.ProductCheck
	stops   map[string]error
	calls   []string
}

func newScriptedChecker() *scriptedChecker {
	return &scriptedChecker{results: map[string]providers.ProductCheck{}, stops: map[string]error{}}
}

func (c *scriptedChecker) CheckProduct(_ context.Context, _ providers.Key, id string) (providers.ProductCheck, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, id)
	r, ok := c.results[id]
	if !ok {
		r = providers.ProductCheck{Status: providers.ProductUnknown, Detail: providers.DetailNetwork}
	}
	return r, c.stops[id]
}

func (c *scriptedChecker) set(id string, r providers.ProductCheck) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results[id] = r
}

func (c *scriptedChecker) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func foundItem(name string, price int64) providers.ProductCheck {
	return providers.ProductCheck{Status: providers.ProductFound, Name: name, PriceCents: &price, Pickup: "IN_STOCK", Delivery: "IN_STOCK"}
}

func excludedFor(t *testing.T, p Proposal, key string) Excluded {
	t.Helper()
	for _, e := range p.Excluded {
		if e.IngredientKey == key {
			return e
		}
	}
	t.Fatalf("%s isn't excluded: %+v", key, p.Excluded)
	return Excluded{}
}

func linkText(p Proposal) string {
	var urls []string
	for _, l := range p.Links {
		urls = append(urls, l.URL)
	}
	return strings.Join(urls, " ")
}

func TestIntegrationHandoffNeverSendsAGoneProduct(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	checker := newScriptedChecker()
	f.svc.checker = checker
	checker.set("100000001", foundItem("Test Ground Beef 16 oz", 499))
	checker.set("100000002", providers.ProductCheck{Status: providers.ProductGone})
	onion := f.keys["Yellow Onion"]

	f.saveMeasured(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	// Saving checks the product at once: this one is already gone.
	saved := f.save(t, "Yellow Onion", "https://www.walmart.com/ip/Test-Onion/100000002", &PackageSize{Quantity: "3", Unit: "count"})
	if saved.Health(f.svc.now()) != HealthGone || saved.CurrentCheck() == nil || saved.CurrentCheck().Status != providers.ProductGone {
		t.Fatalf("saved onion = %+v, want checked and gone", saved)
	}

	match, err := f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	if e := excludedFor(t, match, onion); e.Reason != ExcludedProductGone || e.ProductID != "100000002" || !e.Reason.NeedsDecision() {
		t.Errorf("onion = %+v, want product_gone needing a decision", e)
	}
	if strings.Contains(linkText(match), "100000002") || !strings.Contains(linkText(match), "100000001") {
		t.Errorf("match links = %s: a gone item must never be in one", linkText(match))
	}
	if beef := lineFor(t, match, "Ground Beef"); beef.Health != HealthOK || beef.Check == nil || beef.Check.Name != "Test Ground Beef 16 oz" {
		t.Errorf("beef line = %+v", beef)
	}

	// Sending without deciding is refused, and nothing is stored.
	_, _, err = f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	var decision *DecisionNeededError
	if !errors.As(err, &decision) || len(decision.Lines) != 1 || decision.Lines[0].IngredientKey != onion {
		t.Fatalf("CreateHandoff() error = %v, want a decision on the onion", err)
	}
	if list, _ := f.svc.ListHandoffs(ctx, testHousehold, HandoffFilter{}); len(list) != 0 {
		t.Fatalf("handoffs stored = %d", len(list))
	}

	// Selecting only the ready lines (as the app does once a count changed)
	// doesn't let the gone line drop out unnoticed either.
	_, _, err = f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{Lines: []LineSelection{{IngredientKey: f.keys["Ground Beef"], Packages: 3}}})
	if !errors.As(err, &decision) || len(decision.Lines) != 1 {
		t.Fatalf("CreateHandoff(selected lines) error = %v, want a decision on the onion", err)
	}

	// Knowingly left out: the hand-off goes, without the item, and says why.
	h, created, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{ExcludeKeys: []string{onion}})
	if err != nil || !created {
		t.Fatalf("CreateHandoff(leave out) = %v, %v", created, err)
	}
	if strings.Contains(linkText(h.Proposal), "100000002") {
		t.Errorf("handoff links = %s", linkText(h.Proposal))
	}
	stored, err := f.svc.GetHandoff(ctx, testHousehold, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e := excludedFor(t, stored.Proposal, onion); e.Reason != ExcludedLeftOutGone || e.ProductID != "100000002" || e.ProductName == "" {
		t.Errorf("stored onion exclusion = %+v, want left_out_gone with its product", e)
	}

	// Re-choosing replaces the gone product; the new one is checked and sent.
	checker.set("100000012", foundItem("Test Yellow Onions 3 lb", 329))
	rechosen := f.save(t, "Yellow Onion", "https://www.walmart.com/ip/Test-Onions/100000012", &PackageSize{Quantity: "3", Unit: "count"})
	if rechosen.Health(f.svc.now()) != HealthOK || rechosen.CurrentCheck().ProductID != "100000012" || rechosen.PriceCents == nil || *rechosen.PriceCents != 329 {
		t.Fatalf("re-chosen onion = %+v", rechosen)
	}
	h, _, err = f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil || !strings.Contains(linkText(h.Proposal), "100000012") {
		t.Errorf("after re-choosing = %s, %v", linkText(h.Proposal), err)
	}
}

func TestIntegrationStaleUnknownBlocksUntilCheckedOrLeftOut(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	checker := newScriptedChecker() // every check fails: unknown
	f.svc.checker = checker
	beef, garlic := f.keys["Ground Beef"], f.keys["Garlic"]
	f.saveMeasured(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	f.save(t, "Garlic", "https://www.walmart.com/ip/Test-Garlic/100000003", &PackageSize{Quantity: "3", Unit: "count"})

	// Just chosen, a failed check doesn't block: the member just saw it.
	if p, err := f.svc.Preflight(ctx, f.actor, testWeek, "walmart", MatchInput{}); err != nil || len(p.NeedsDecision()) != 0 {
		t.Fatalf("fresh preflight = %+v, %v", p.NeedsDecision(), err)
	}

	f.advance(ProductFreshness + time.Hour)
	before := checker.callCount()
	p, err := f.svc.Preflight(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	if checker.callCount()-before != 2 {
		t.Errorf("preflight made %d checks, want both stale products checked", checker.callCount()-before)
	}
	if len(p.NeedsDecision()) != 2 || excludedFor(t, p, beef).Reason != ExcludedProductUnverified {
		t.Fatalf("stale unknown = %+v, want both unverified", p.Excluded)
	}
	if strings.Contains(linkText(p), "100000001") || strings.Contains(linkText(p), "100000003") {
		t.Errorf("links = %s", linkText(p))
	}
	var decision *DecisionNeededError
	if _, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{}); !errors.As(err, &decision) || len(decision.Lines) != 2 {
		t.Fatalf("CreateHandoff() = %v, want a decision on both", err)
	}

	// Garlic answers now; beef is left out knowingly.
	checker.set("100000003", foundItem("Test Garlic 3 ct", 150))
	h, created, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{ExcludeKeys: []string{beef}})
	if err != nil || !created {
		t.Fatalf("CreateHandoff() = %v, %v", created, err)
	}
	if !strings.Contains(linkText(h.Proposal), "100000003") || strings.Contains(linkText(h.Proposal), "100000001") {
		t.Errorf("links = %s", linkText(h.Proposal))
	}
	if e := excludedFor(t, h.Proposal, beef); e.Reason != ExcludedLeftOutUnverified {
		t.Errorf("beef = %+v, want left_out_unverified", e)
	}
	if lineFor(t, h.Proposal, "Garlic").IngredientKey != garlic {
		t.Error("garlic isn't sent")
	}
}

func TestIntegrationChecksPauseWhenRefused(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	checker := newScriptedChecker()
	f.svc.checker = checker
	f.save(t, "Garlic", "https://www.walmart.com/ip/Test-Garlic/100000003", nil)
	f.save(t, "Milk", "https://www.walmart.com/ip/Test-Milk/100000004", nil)
	f.advance(ProductFreshness + time.Hour)
	checker.stops["100000003"] = &CheckStop{Blocked: true}
	checker.stops["100000004"] = &CheckStop{Blocked: true}

	before := checker.callCount()
	p, err := f.svc.Preflight(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	if checker.callCount()-before != 1 {
		t.Errorf("checks after a refusal = %d, want the run to stop at the first", checker.callCount()-before)
	}
	pause, err := f.store.GetCheckPause(ctx, providers.KeyWalmart)
	if err != nil || pause.Reason != providers.DetailBlocked || !pause.Until.Equal(f.svc.timestamp().Add(pauseAfterBlocked)) {
		t.Fatalf("pause = %+v, %v", pause, err)
	}
	if !p.ChecksPaused || len(p.NeedsDecision()) != 2 {
		t.Errorf("paused preflight = paused %v, %d decisions", p.ChecksPaused, len(p.NeedsDecision()))
	}
	// While paused nothing is requested, and unverified products still block.
	before = checker.callCount()
	if _, err := f.svc.Preflight(ctx, f.actor, testWeek, "walmart", MatchInput{}); err != nil || checker.callCount() != before {
		t.Errorf("checks while paused = %d, %v", checker.callCount()-before, err)
	}
	f.advance(pauseAfterBlocked + time.Minute)
	delete(checker.stops, "100000003")
	delete(checker.stops, "100000004")
	if _, err := f.svc.Preflight(ctx, f.actor, testWeek, "walmart", MatchInput{}); err != nil || checker.callCount() == before {
		t.Errorf("checks after the pause = %d, %v", checker.callCount()-before, err)
	}
}

func TestIntegrationSweepOrderCapAndStop(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	notifier := &orderNotifier{}
	f.svc.notifier = notifier
	other := households.Membership{HouseholdID: "66e5a1f2c3b4a5d6e7f80a02", UserID: testUser, Role: households.RoleMember}
	save := func(actor households.Membership, key, product string) Preference {
		t.Helper()
		p, _, err := f.svc.PutPreference(ctx, actor, "walmart", key, PreferenceInput{ProductID: product, DisplayName: key})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Saved with no checker: nothing is checked until the sweep.
	a := save(f.actor, "name:alpha", "200000001")
	b := save(other, "name:bravo", "200000002")
	c := save(f.actor, "name:charlie", "200000003")
	d := save(f.actor, "name:delta", "200000004")
	e := save(other, "name:echo", "200000001") // the same item as alpha
	checkedAt := func(p Preference, age time.Duration) {
		t.Helper()
		next, _, _ := applyCheck(p, foundItem("x", 100), f.svc.timestamp().Add(-age))
		if ok, err := f.store.SaveProductCheck(ctx, next, false); err != nil || !ok {
			t.Fatalf("seed check = %v, %v", ok, err)
		}
	}
	checkedAt(b, 72*time.Hour)
	checkedAt(c, 48*time.Hour)
	checkedAt(d, time.Hour) // recent: not due

	checker := newScriptedChecker()
	f.svc.checker = checker
	for _, id := range []string{"200000001", "200000003", "200000004"} {
		checker.set(id, foundItem("Item "+id, 100))
	}
	checker.set("200000002", providers.ProductCheck{Status: providers.ProductGone})

	report, err := f.svc.SweepProducts(ctx, providers.KeyWalmart, SweepOptions{Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	// Never-checked first (alpha and echo share one request), then the oldest
	// check; the cap stops before charlie, and delta isn't due.
	if strings.Join(checker.calls, ",") != "200000001,200000002" || report.Candidates != 4 || report.Checked != 2 {
		t.Fatalf("sweep calls = %v, report %+v", checker.calls, report)
	}
	for _, p := range []Preference{a, e} {
		got, err := f.store.GetPreference(ctx, p.HouseholdID, providers.KeyWalmart, p.IngredientKey)
		if err != nil || got.CurrentCheck() == nil || got.CurrentCheck().Status != providers.ProductFound {
			t.Errorf("%s after sweep = %+v, %v", p.IngredientKey, got.Check, err)
		}
	}
	if report.NewlyGone != 1 || report.Notified != 1 || len(notifier.created) != 1 {
		t.Fatalf("notifications = %+v, %+v", report, notifier.created)
	}
	n := notifier.created[0]
	if n.HouseholdID != other.HouseholdID || n.Type != notifications.TypeShoppingProductGone ||
		n.Subject != (notifications.Subject{Kind: notifications.SubjectShoppingProducts, ID: "walmart"}) ||
		!strings.Contains(n.Body, "bravo") || n.Title != "A saved product is no longer on Walmart" {
		t.Errorf("notification = %+v", n)
	}
	if relevant, err := f.svc.StillRelevant(ctx, notifications.Notification{HouseholdID: other.HouseholdID, Type: n.Type, Subject: n.Subject}); err != nil || !relevant {
		t.Errorf("StillRelevant(gone) = %v, %v", relevant, err)
	}

	// Next run: only charlie is still due. Walmart refuses it: the run stops
	// and every check pauses.
	checker.stops["200000003"] = &CheckStop{Blocked: true}
	f.advance(time.Minute)
	report, err = f.svc.SweepProducts(ctx, providers.KeyWalmart, SweepOptions{Max: 2})
	if err != nil || !report.Stopped || report.Checked != 1 || checker.calls[len(checker.calls)-1] != "200000003" {
		t.Fatalf("refused sweep = %+v, %v, calls %v", report, err, checker.calls)
	}
	if report.Notified != 0 {
		t.Errorf("gone products were notified again: %+v", report)
	}
	calls := checker.callCount()
	f.advance(time.Hour)
	if report, err = f.svc.SweepProducts(ctx, providers.KeyWalmart, SweepOptions{}); err != nil || !report.Paused || checker.callCount() != calls {
		t.Errorf("paused sweep = %+v, %v, %d new calls", report, err, checker.callCount()-calls)
	}

	// Re-choosing bravo before the push goes out makes it irrelevant.
	save(other, "name:bravo", "200000022")
	if relevant, err := f.svc.StillRelevant(ctx, notifications.Notification{HouseholdID: other.HouseholdID, Type: n.Type, Subject: n.Subject}); err != nil || relevant {
		t.Errorf("StillRelevant(re-chosen) = %v, %v", relevant, err)
	}
}

func TestIntegrationProductChecksHTTP(t *testing.T) {
	f := newFixture(t)
	checker := newScriptedChecker()
	f.svc.checker = checker
	checker.set("100000002", providers.ProductCheck{Status: providers.ProductGone})
	checker.set("100000001", foundItem("Test Ground Beef", 499))
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember, testHousehold + "/" + viewerUser: "viewer"},
	}).Mount)
	do := func(method, path, body, userID string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/households/"+testHousehold+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	status, body := do(http.MethodPut, "/shopping/walmart/preferences/"+f.keys["Yellow Onion"], `{"productId":"100000002","displayName":"Onions"}`, testUser)
	if status != http.StatusCreated || body["health"] != "gone" || body["check"].(map[string]any)["status"] != "gone" || body["healthText"] == nil {
		t.Fatalf("put gone product = %d %v", status, body)
	}
	status, body = do(http.MethodPut, "/shopping/walmart/preferences/"+f.keys["Ground Beef"], `{"productId":"100000001","displayName":"Beef"}`, testUser)
	check, _ := body["check"].(map[string]any)
	if status != http.StatusCreated || body["health"] != "ok" || body["priceCents"] != float64(499) || body["priceSource"] != "provider" ||
		check["name"] != "Test Ground Beef" || check["storeApproximate"] != true {
		t.Fatalf("put found product = %d %v", status, body)
	}

	if status, _ := do(http.MethodPost, "/plans/2026-W38/shopping/walmart/preflight", `{}`, viewerUser); status != http.StatusForbidden {
		t.Errorf("viewer preflight = %d", status)
	}
	status, body = do(http.MethodPost, "/plans/2026-W38/shopping/walmart/preflight", `{}`, testUser)
	if status != http.StatusOK || body["needsDecision"] != float64(1) {
		t.Fatalf("preflight = %d %v", status, body)
	}
	var onion map[string]any
	for _, e := range body["excluded"].([]any) {
		if m := e.(map[string]any); m["reason"] == "product_gone" {
			onion = m
		}
	}
	if onion == nil || onion["needsDecision"] != true || onion["product"].(map[string]any)["productId"] != "100000002" ||
		onion["product"].(map[string]any)["health"] != "gone" || !strings.Contains(onion["text"].(string), "No longer on Walmart") {
		t.Fatalf("gone line = %v", onion)
	}
	for _, l := range body["cartLinks"].([]any) {
		if strings.Contains(l.(map[string]any)["url"].(string), "100000002") {
			t.Errorf("preflight link has the gone item: %v", l)
		}
	}
	status, body = do(http.MethodPost, "/plans/2026-W38/shopping/walmart/handoffs", `{}`, testUser)
	if errBody, _ := body["error"].(map[string]any); status != http.StatusConflict || errBody["code"] != "products_need_decision" ||
		!strings.Contains(errBody["message"].(string), "Yellow Onion") {
		t.Fatalf("handoff with a gone product = %d %v", status, body)
	}
	status, body = do(http.MethodPost, "/plans/2026-W38/shopping/walmart/handoffs", `{"excludeKeys":["`+f.keys["Yellow Onion"]+`"]}`, testUser)
	if status != http.StatusCreated {
		t.Fatalf("handoff leaving it out = %d %v", status, body)
	}
	reasons := []string{}
	for _, e := range body["excluded"].([]any) {
		reasons = append(reasons, e.(map[string]any)["reason"].(string))
	}
	if !slices.Contains(reasons, "left_out_gone") {
		t.Errorf("handoff exclusions = %v", reasons)
	}
}
