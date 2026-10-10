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

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

func (f *fixture) check(name, product string, status ProductStatus, decision CheckDecision) ProductCheckReport {
	return ProductCheckReport{IngredientKey: f.keys[name], ProductID: product, Status: status, CheckedAt: f.clock.Add(-time.Minute), Decision: decision}
}

func (f *fixture) preference(t *testing.T, name string) Preference {
	t.Helper()
	p, err := f.svc.GetPreference(f.ctx, testHousehold, "walmart", f.keys[name])
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The member's rule: a linked product that isn't on Walmart must never be
// sent as if it were. The server enforces it on every link it builds, from
// what the phone reported, and never fetches Walmart itself.
func TestIntegrationHandoffEnforcesProductChecks(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	f.saveMeasured(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	f.save(t, "Yellow Onion", "https://www.walmart.com/ip/100000002", &PackageSize{Quantity: "3", Unit: "count"})
	f.save(t, "Kidney Beans", "https://www.walmart.com/ip/Test-Beans/100000005", &PackageSize{Quantity: "1", Unit: "can"})

	// Undecided: beef is gone and beans couldn't be checked. Only the gone
	// beef holds the send (an unchecked product the household picked goes in,
	// decision 639), and the results are recorded anyway.
	in := MatchInput{
		// The client lists the gone beef with a count: it must still never
		// be sent.
		Lines: []LineSelection{{IngredientKey: f.keys["Ground Beef"], Packages: 3}, {IngredientKey: f.keys["Yellow Onion"]}, {IngredientKey: f.keys["Kidney Beans"]}},
		Checks: []ProductCheckReport{
			f.check("Ground Beef", "100000001", ProductGone, ""),
			f.check("Kidney Beans", "100000005", ProductUnknown, ""),
			func() ProductCheckReport {
				r := f.check("Yellow Onion", "100000002", ProductFound, "")
				r.Name, r.PriceCents = "Yellow Onions, 3 lb Bag", cents(348)
				return r
			}(),
		},
	}
	_, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", in)
	var decision *DecisionNeededError
	if !errors.As(err, &decision) || len(decision.Lines) != 1 || decision.Names() != "Ground Beef" {
		t.Fatalf("undecided send = %v", err)
	}
	if list, _ := f.svc.ListHandoffs(ctx, testHousehold, HandoffFilter{}); len(list) != 0 {
		t.Fatalf("an undecided send stored a handoff: %+v", list)
	}
	beef := f.preference(t, "Ground Beef")
	if !beef.NeedsRechoosing() || beef.Check.CheckedAt.IsZero() {
		t.Errorf("beef check = %+v", beef.Check)
	}
	if beans := f.preference(t, "Kidney Beans"); beans.Check == nil || beans.Check.Status != ProductUnknown {
		t.Errorf("beans check = %+v", beans.Check)
	}
	onion := f.preference(t, "Yellow Onion")
	if onion.Check.Name != "Yellow Onions, 3 lb Bag" || onion.PriceCents == nil || *onion.PriceCents != 348 || onion.PriceSource != PriceFromProvider {
		t.Errorf("onion after its first check = %+v, price %v", onion.Check, onion.PriceCents)
	}

	// Saved Products and the match show "needs re-choosing" from the saved
	// check, with no report and no fetch.
	m, err := f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := excludedFor(m, f.keys["Ground Beef"]); e.Reason != ExcludedProductGone || !e.Check.Saved {
		t.Errorf("match beef = %+v", e)
	}
	if strings.Contains(linkItems(m.Links), "100000001") {
		t.Errorf("match links include the gone beef: %q", linkItems(m.Links))
	}

	// Decided: beef left out, beans sent anyway. The gone key is in no link,
	// and Not Included says why.
	in.Checks[0].Decision, in.Checks[1].Decision = DecisionLeaveOut, DecisionSendAnyway
	h, created, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", in)
	if err != nil || !created {
		t.Fatalf("decided send = %+v, %v", h, err)
	}
	if got := linkItems(h.Links); strings.Contains(got, "100000001") || got != "100000002,100000005_2" {
		t.Errorf("links = %q", got)
	}
	e, ok := excludedFor(h.Proposal, f.keys["Ground Beef"])
	if !ok || e.Reason != ExcludedLeftOutGone || e.ProductID != "100000001" || e.Check.Decision != DecisionLeaveOut {
		t.Errorf("beef excluded = %+v", e)
	}
	beans := lineFor(t, h.Proposal, "Kidney Beans")
	if beans.Check == nil || beans.Check.Status != ProductUnknown || beans.Check.Decision != DecisionSendAnyway {
		t.Errorf("beans line = %+v", beans.Check)
	}
	// Stored as sent: read back, the handoff says the same.
	stored, err := f.svc.GetHandoff(ctx, testHousehold, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := excludedFor(stored.Proposal, f.keys["Ground Beef"]); e.Reason != ExcludedLeftOutGone || e.ProductName != "Ground Beef (store)" || e.Check == nil {
		t.Errorf("stored beef = %+v", e)
	}
	if l := lineFor(t, stored.Proposal, "Kidney Beans"); l.Check == nil || l.Check.Decision != DecisionSendAnyway {
		t.Errorf("stored beans = %+v", l.Check)
	}

	// Re-chosen: the old check says nothing about the new product, and a
	// report for the old product counts as unchecked. The member just picked
	// it, so it's sent (decision 639).
	f.saveMeasured(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef-New/100000009", &PackageSize{Quantity: "16", Unit: "oz"})
	if p := f.preference(t, "Ground Beef"); p.NeedsRechoosing() || p.CurrentCheck() != nil {
		t.Errorf("re-chosen beef = %+v", p.Check)
	}
	h, _, err = f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{Checks: []ProductCheckReport{f.check("Ground Beef", "100000001", ProductGone, "")}})
	if err != nil || !strings.Contains(linkItems(h.Links), "100000009") {
		t.Errorf("stale report = %v, links %q", err, linkItems(h.Links))
	}
}

// A member's price is never replaced by the first check, only when Walmart's
// own price moves after that.
func TestIntegrationCheckPricePrecedence(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	price := int64(450)
	if _, _, err := f.svc.PutPreference(ctx, f.actor, "walmart", f.keys["Yellow Onion"], PreferenceInput{
		ProductURL: "100000002", DisplayName: "Onions", PackageSize: &PackageSize{Quantity: "3", Unit: "count"}, SetPrice: true, PriceCents: &price,
	}); err != nil {
		t.Fatal(err)
	}
	send := func(listed int64) Preference {
		t.Helper()
		r := f.check("Yellow Onion", "100000002", ProductFound, "")
		r.PriceCents = &listed
		if err := f.svc.recordReportedChecks(ctx, testHousehold, "walmart", []ProductCheckReport{r}); err != nil {
			t.Fatal(err)
		}
		return f.preference(t, "Yellow Onion")
	}
	if p := send(499); *p.PriceCents != 450 || p.PriceSource != PriceFromMember {
		t.Errorf("first check = %d %s", *p.PriceCents, p.PriceSource)
	}
	if p := send(499); *p.PriceCents != 450 {
		t.Errorf("same listed price = %d", *p.PriceCents)
	}
	if p := send(529); *p.PriceCents != 529 || p.PriceSource != PriceFromProvider {
		t.Errorf("moved listed price = %d %s", *p.PriceCents, p.PriceSource)
	}
	// Typing a price makes it the member's again.
	price = 500
	if _, _, err := f.svc.PutPreference(ctx, f.actor, "walmart", f.keys["Yellow Onion"], PreferenceInput{
		ProductURL: "100000002", DisplayName: "Onions", PackageSize: &PackageSize{Quantity: "3", Unit: "count"}, SetPrice: true, PriceCents: &price,
	}); err != nil {
		t.Fatal(err)
	}
	if p := send(529); *p.PriceCents != 500 || p.PriceSource != PriceFromMember {
		t.Errorf("after typing = %d %s", *p.PriceCents, p.PriceSource)
	}
}

func TestIntegrationProductChecksHTTP(t *testing.T) {
	f := newFixture(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember},
	}).Mount)
	do := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/households/"+testHousehold+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+testUser)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	f.saveMeasured(t, "Ground Beef", "100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	f.save(t, "Yellow Onion", "100000002", &PackageSize{Quantity: "3", Unit: "count"})
	beef, onion := f.keys["Ground Beef"], f.keys["Yellow Onion"]
	checks := func(beefDecision string) string {
		return `{"productChecks":[` +
			`{"ingredientKey":"` + beef + `","productId":"100000001","status":"gone","checkedAt":"2026-09-15T18:00:00Z"` + beefDecision + `},` +
			`{"ingredientKey":"` + onion + `","productId":"100000002","status":"found","checkedAt":"2026-09-15T18:00:00Z","name":"Onions","priceCents":348}]}`
	}
	const handoffs = "/plans/2026-W38/shopping/walmart/handoffs"

	code, body := do(http.MethodPost, handoffs, checks(""))
	if errBody, _ := body["error"].(map[string]any); code != http.StatusConflict || errBody["code"] != "products_need_decision" ||
		errBody["message"] != "Decide on these first: Ground Beef." {
		t.Fatalf("undecided = %d %v", code, body)
	}
	if code, body = do(http.MethodPost, handoffs, checks(`,"decision":"send_anyway"`)); code != http.StatusBadRequest {
		t.Errorf("send anyway on gone = %d %v", code, body)
	}

	code, body = do(http.MethodPost, "/plans/2026-W38/shopping/walmart/match", `{}`)
	if code != 200 || body["needsDecision"] != 1.0 {
		t.Fatalf("match = %d %v", code, body)
	}

	code, body = do(http.MethodPost, handoffs, checks(`,"decision":"leave_out"`))
	if code != http.StatusCreated || body["needsDecision"] != 0.0 {
		t.Fatalf("decided = %d %v", code, body)
	}
	var left map[string]any
	for _, e := range body["excluded"].([]any) {
		if e.(map[string]any)["ingredientKey"] == beef {
			left = e.(map[string]any)
		}
	}
	if left["reason"] != "left_out_gone" || left["text"] != "Left out. No longer on Walmart." || left["product"].(map[string]any)["productId"] != "100000001" ||
		left["check"].(map[string]any)["decision"] != "leave_out" {
		t.Errorf("not included = %v", left)
	}
	line := body["lines"].([]any)[0].(map[string]any)
	if line["check"].(map[string]any)["status"] != "found" {
		t.Errorf("line check = %v", line["check"])
	}

	code, body = do(http.MethodGet, "/shopping/walmart/preferences", "")
	if code != 200 {
		t.Fatalf("preferences = %d", code)
	}
	for _, it := range body["items"].([]any) {
		p := it.(map[string]any)
		switch p["ingredientKey"] {
		case beef:
			if p["needsRechoosing"] != true || p["check"].(map[string]any)["status"] != "gone" {
				t.Errorf("beef preference = %v", p)
			}
		case onion:
			if p["needsRechoosing"] != false || p["priceCents"] != 348.0 || p["priceSource"] != "provider" || p["check"].(map[string]any)["name"] != "Onions" {
				t.Errorf("onion preference = %v", p)
			}
		}
	}
}

// Leaving out the only line with a product leaves nothing for the cart. That
// is its own answer, so the app can say "nothing to add" instead of showing a
// generic failure, and no handoff is stored.
func TestIntegrationEverythingLeftOutIsNothingToSend(t *testing.T) {
	f := newFixture(t)
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	check := f.check("Ground Beef", "100000001", ProductGone, DecisionLeaveOut)
	_, _, err := f.svc.CreateHandoff(f.ctx, f.actor, testWeek, "walmart", MatchInput{Checks: []ProductCheckReport{check}})
	if !errors.Is(err, ErrEverythingLeftOut) {
		t.Fatalf("err = %v, want ErrEverythingLeftOut", err)
	}
	if list, _ := f.svc.ListHandoffs(f.ctx, testHousehold, HandoffFilter{}); len(list) != 0 {
		t.Fatalf("stored a handoff for an empty cart: %+v", list)
	}
}
