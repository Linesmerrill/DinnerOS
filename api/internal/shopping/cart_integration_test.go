package shopping

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// linkItems returns the items= part of every link, joined by "|".
func linkItems(links []CartLink) string {
	var parts []string
	for _, l := range links {
		_, items, _ := strings.Cut(l.URL, "items=")
		items, _, _ = strings.Cut(items, "&")
		parts = append(parts, items)
	}
	return strings.Join(parts, "|")
}

func cartFor(t *testing.T, p Proposal, name string) LineCart {
	t.Helper()
	l := lineFor(t, p, name)
	if l.Cart == nil {
		t.Fatalf("%s has no cart state: %+v", name, l)
	}
	return *l.Cart
}

// allLines selects every line the fixture saved a product for, with counts.
func (f *fixture) allLines(counts map[string]int, names ...string) MatchInput {
	in := MatchInput{Lines: []LineSelection{}}
	for _, n := range names {
		in.Lines = append(in.Lines, LineSelection{IngredientKey: f.keys[n], Packages: counts[n]})
	}
	return in
}

// The owner's TestFlight bug: 13 items sent to Walmart, not checked out, and
// opening Walmart again to add more re-added all 13. A second send must add
// only what isn't in the cart yet, and every member must see the same state.
func TestIntegrationSendingAgainAddsOnlyWhatsNew(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	other := memberOf(otherMember)
	if _, err := f.svc.UpdateSettings(ctx, f.actor, "walmart", "5435"); err != nil {
		t.Fatal(err)
	}
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	f.save(t, "Yellow Onion", "https://www.walmart.com/ip/100000002", &PackageSize{Quantity: "3", Unit: "count"})
	f.save(t, "Kidney Beans", "https://www.walmart.com/ip/Test-Beans/100000005", &PackageSize{Quantity: "1", Unit: "can"})

	// Before anything is sent, a match carries no cart state.
	m, err := f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil || m.Cart != nil || lineFor(t, m, "Ground Beef").Cart != nil {
		t.Fatalf("first match = %+v, %v", m, err)
	}

	// First send: everything.
	h, isNew, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil || !isNew || len(h.Lines) != 3 || linkItems(h.Links) != "100000002,100000001_3,100000005_2" {
		t.Fatalf("first send = %+v, %v, %v", h, isNew, err)
	}

	// Another member on another device sees the same sent state.
	m, err = f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil || m.Cart == nil || m.Cart.HandoffID != h.ID || len(m.Links) != 0 || len(m.Cart.Other) != 0 {
		t.Fatalf("match after sending = %+v, %v", m, err)
	}
	if c := cartFor(t, m, "Ground Beef"); c != (LineCart{SentPackages: 3}) {
		t.Errorf("beef cart = %+v", c)
	}

	// Nothing new: no link, no write, the same handoff.
	again, isNew, err := f.svc.CreateHandoff(ctx, other, testWeek, "walmart", MatchInput{})
	if err != nil || isNew || again.ID != h.ID || len(again.Links) != 0 || again.Revision != h.Revision {
		t.Fatalf("send with nothing new = %+v, %v, %v", again, isNew, err)
	}

	// A new line, and beef up from 3 to 5: only garlic and 2 more beef.
	f.save(t, "Garlic", "100000003", &PackageSize{Quantity: "3", Unit: "count"})
	names := []string{"Ground Beef", "Yellow Onion", "Garlic", "Kidney Beans"}
	more, isNew, err := f.svc.CreateHandoff(ctx, other, testWeek, "walmart", f.allLines(map[string]int{"Ground Beef": 5}, names...))
	if err != nil || isNew || more.ID != h.ID {
		t.Fatalf("second send = %+v, %v, %v", more, isNew, err)
	}
	if got := linkItems(more.Links); got != "100000003,100000001_2" {
		t.Errorf("second send links = %q, want only the garlic and the extra beef", got)
	}
	beef, garlic := lineFor(t, more.Proposal, "Ground Beef"), lineFor(t, more.Proposal, "Garlic")
	if beef.ID != "l2" || beef.Packages != 5 || garlic.ID != "l4" || garlic.Packages != 1 || len(more.Lines) != 4 {
		t.Errorf("merged lines = %+v", more.Lines)
	}
	if types := f.events.types(); len(types) != 2 {
		t.Errorf("events = %v, want one per send that added something", types)
	} else if p, ok := f.events.list[1].Payload.(events.ShoppingHandoffCreated); !ok || p.Lines != 2 || p.Packages != 3 || p.HandoffID != h.ID {
		t.Errorf("second event = %+v", f.events.list[1].Payload)
	}

	// A decrease can't be applied by a link: it's reported, not sent.
	m, err = f.svc.Match(ctx, testHousehold, testWeek, "walmart", f.allLines(map[string]int{"Ground Beef": 4}, names...))
	if err != nil || len(m.Links) != 0 {
		t.Fatalf("decreased match = %+v, %v", m, err)
	}
	if c := cartFor(t, m, "Ground Beef"); c != (LineCart{SentPackages: 5, RemovePackages: 1}) {
		t.Errorf("decreased beef cart = %+v", c)
	}
	if same, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", f.allLines(map[string]int{"Ground Beef": 4}, names...)); err != nil || len(same.Links) != 0 {
		t.Errorf("sending a decrease = %+v, %v", same.Links, err)
	}

	// A replaced product is sent in full, and the old one is reported for
	// removal.
	f.save(t, "Yellow Onion", "https://www.walmart.com/ip/100000012", &PackageSize{Quantity: "3", Unit: "count"})
	m, err = f.svc.Match(ctx, testHousehold, testWeek, "walmart", f.allLines(map[string]int{"Ground Beef": 5}, names...))
	if err != nil || len(m.Cart.Other) != 1 {
		t.Fatalf("match after replacing onion = %+v, %v", m, err)
	}
	if o := m.Cart.Other[0]; o.ProductID != "100000002" || o.Reason != SentProductChanged || o.RemovePackages != 1 {
		t.Errorf("replaced onion = %+v", o)
	}
	if c := cartFor(t, m, "Yellow Onion"); c != (LineCart{AddPackages: 1}) {
		t.Errorf("new onion cart = %+v", c)
	}
	f.save(t, "Yellow Onion", "https://www.walmart.com/ip/100000002", &PackageSize{Quantity: "3", Unit: "count"})

	// Send again: a member removed the beans from the cart.
	if err := f.svc.SendAgain(ctx, viewer(), testWeek, "walmart", f.keys["Kidney Beans"]); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer send again = %v", err)
	}
	if err := f.svc.SendAgain(ctx, other, testWeek, "walmart", "not a key"); err == nil {
		t.Error("send again accepted a bad key")
	}
	if err := f.svc.SendAgain(ctx, other, testWeek, "walmart", f.keys["Kidney Beans"]); err != nil {
		t.Fatal(err)
	}
	m, _ = f.svc.Match(ctx, testHousehold, testWeek, "walmart", f.allLines(map[string]int{"Ground Beef": 5}, names...))
	if c := cartFor(t, m, "Kidney Beans"); c != (LineCart{AddPackages: 2}) || linkItems(m.Links) != "100000005_2" {
		t.Errorf("beans after send again = %+v, links %q", c, linkItems(m.Links))
	}
	resent, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", f.allLines(map[string]int{"Ground Beef": 5}, names...))
	if err != nil || linkItems(resent.Links) != "100000005_2" || lineFor(t, resent.Proposal, "Kidney Beans").ID != "l5" {
		t.Fatalf("resend = %+v, %v", resent, err)
	}

	// "Did you order these?" asks about what's in the cart: beef at 5. A
	// confirmed line still counts as sent, and answering some lines leaves
	// the handoff current.
	beef = lineFor(t, resent.Proposal, "Ground Beef")
	if beef.Packages != 5 || beef.Status != LinePending {
		t.Errorf("beef to confirm = %+v", beef)
	}
	confirmed, err := f.svc.Confirm(ctx, f.actor, h.ID, ConfirmInput{Lines: []ConfirmLine{{LineID: beef.ID}}})
	if err != nil || len(confirmed.Purchases) != 1 || confirmed.Purchases[0].Purchase.Quantity != "5" || !confirmed.Handoff.Active {
		t.Fatalf("confirm beef = %+v, %v", confirmed, err)
	}
	if m, _ = f.svc.Match(ctx, testHousehold, testWeek, "walmart", f.allLines(map[string]int{"Ground Beef": 5}, names...)); cartFor(t, m, "Ground Beef") != (LineCart{SentPackages: 5}) {
		t.Errorf("confirmed beef cart = %+v", cartFor(t, m, "Ground Beef"))
	}

	// Two members sending the same new line at once add it once.
	f.save(t, "Milk", "https://walmart.com/ip/Test-Milk/100000004", nil)
	withMilk := append(names, "Milk")
	var wg sync.WaitGroup
	results := make([]Handoff, 2)
	errs := make([]error, 2)
	for i, actor := range []households.Membership{f.actor, other} {
		wg.Go(func() {
			results[i], _, errs[i] = f.svc.CreateHandoff(ctx, actor, testWeek, "walmart", f.allLines(map[string]int{"Ground Beef": 5}, withMilk...))
		})
	}
	wg.Wait()
	milkLinks := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("concurrent send %d: %v", i, errs[i])
		}
		if strings.Contains(linkItems(results[i].Links), "100000004") {
			milkLinks++
		}
	}
	if milkLinks != 1 {
		t.Errorf("milk was added by %d concurrent sends, want 1", milkLinks)
	}

	// Start over: the cart was emptied, so the next send adds everything.
	if err := f.svc.StartOver(ctx, other, testWeek, "walmart"); err != nil {
		t.Fatal(err)
	}
	closed, _ := f.svc.GetHandoff(ctx, testHousehold, h.ID)
	if closed.Active || closed.ClosedReason != ClosedStartedOver || closed.Status() != HandoffDone {
		t.Errorf("started over = active %v reason %q status %s", closed.Active, closed.ClosedReason, closed.Status())
	}
	if m, _ = f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{}); m.Cart != nil {
		t.Errorf("match after starting over has cart %+v", m.Cart)
	}
	fresh, isNew, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil || !isNew || fresh.ID == h.ID || len(fresh.Links) != 1 || len(fresh.Lines) != 4 {
		t.Fatalf("send after starting over = %+v, %v, %v", fresh, isNew, err)
	}

	// Marking the week ordered closes it; taking that back reopens it.
	if _, err := f.svc.SetWeekOrdered(ctx, f.actor, testWeek, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.GetHandoff(ctx, testHousehold, fresh.ID); got.Active || got.ClosedReason != ClosedOrdered {
		t.Errorf("ordered handoff = %+v", got)
	}
	if m, _ = f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{}); m.Cart != nil {
		t.Errorf("match after ordering has cart %+v", m.Cart)
	}
	if _, err := f.svc.SetWeekOrdered(ctx, f.actor, testWeek, false); err != nil {
		t.Fatal(err)
	}
	if m, _ = f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{}); m.Cart == nil || m.Cart.HandoffID != fresh.ID {
		t.Errorf("match after unmarking = %+v", m.Cart)
	}
	if _, err := f.svc.SetWeekOrdered(ctx, f.actor, testWeek, true); err != nil {
		t.Fatal(err)
	}
	next, isNew, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil || !isNew || next.ID == fresh.ID || len(next.Links) != 1 {
		t.Errorf("send after ordering = %+v, %v, %v", next, isNew, err)
	}
	// Another week has its own handoff.
	if m, err = f.svc.Match(ctx, testHousehold, "2026-W39", "walmart", MatchInput{}); err != nil || m.Cart != nil {
		t.Errorf("next week's match = %+v, %v", m.Cart, err)
	}

	// Answering every line closes the handoff too.
	res, err := f.svc.Confirm(ctx, f.actor, next.ID, ConfirmInput{All: true})
	if err != nil || res.Handoff.Active || res.Handoff.ClosedReason != ClosedConfirmed {
		t.Fatalf("confirm = active %v reason %q, %v", res.Handoff.Active, res.Handoff.ClosedReason, err)
	}
	if m, _ = f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{}); m.Cart != nil {
		t.Errorf("match after confirming everything has cart %+v", m.Cart)
	}
}

// A handoff stored before handoffs were kept per week still counts as what's
// in the cart, and becomes the week's current handoff on the next send.
func TestIntegrationLegacyHandoffIsAdopted(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	h, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	oid, _ := bson.ObjectIDFromHex(h.ID)
	if _, err := f.store.handoffs.UpdateOne(ctx, bson.D{{Key: "_id", Value: oid}},
		bson.D{{Key: "$unset", Value: bson.D{{Key: "active", Value: ""}, {Key: "revision", Value: ""}}}}); err != nil {
		t.Fatal(err)
	}
	f.save(t, "Yellow Onion", "https://www.walmart.com/ip/100000002", &PackageSize{Quantity: "3", Unit: "count"})
	f.advance(time.Minute)

	sent, isNew, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil || isNew || sent.ID != h.ID || !sent.Active || sent.Revision != 1 || linkItems(sent.Links) != "100000002" {
		t.Fatalf("send over a legacy handoff = %+v, %v, %v", sent, isNew, err)
	}
}

func TestIntegrationSendAgainHTTP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.UpdateSettings(ctx, f.actor, "walmart", "5435"); err != nil {
		t.Fatal(err)
	}
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember, testHousehold + "/" + viewerUser: "viewer"},
	}).Mount)
	do := func(method, path, body, userID string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/households/"+testHousehold+"/plans/2026-W38/shopping/walmart"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	if code, body := do(http.MethodPost, "/handoffs", `{}`, testUser); code != http.StatusCreated || !strings.Contains(body, `"active":true`) {
		t.Fatalf("first send = %d %s", code, body)
	}
	if code, body := do(http.MethodPost, "/handoffs", `{}`, testUser); code != http.StatusOK || !strings.Contains(body, `"cartLinks":[]`) {
		t.Errorf("second send = %d %s", code, body)
	}
	code, body := do(http.MethodPost, "/match", `{"lines":[{"ingredientKey":"`+f.keys["Ground Beef"]+`","packages":2}]}`, viewerUser)
	if code != http.StatusOK || !strings.Contains(body, `"cart":{"sentPackages":3,"addPackages":0,"removePackages":1}`) || !strings.Contains(body, `"other":[]`) {
		t.Errorf("match = %d %s", code, body)
	}
	for _, tc := range []struct {
		path, body, user string
		status           int
	}{
		{"/handoffs/send-again", `{"ingredientKey":"` + f.keys["Ground Beef"] + `"}`, viewerUser, http.StatusForbidden},
		{"/handoffs/send-again", `{"ingredientKey":""}`, testUser, http.StatusBadRequest},
		{"/handoffs/send-again", `{"ingredientKey":"` + f.keys["Ground Beef"] + `"}`, testUser, http.StatusNoContent},
		{"/handoffs/start-over", ``, viewerUser, http.StatusForbidden},
		{"/handoffs/start-over", ``, testUser, http.StatusNoContent},
		{"/handoffs/start-over", ``, testUser, http.StatusNoContent},
	} {
		if code, body := do(http.MethodPost, tc.path, tc.body, tc.user); code != tc.status {
			t.Errorf("POST %s as %s = %d %s, want %d", tc.path, tc.user, code, body, tc.status)
		}
	}
	if code, body := do(http.MethodPost, "/handoffs", `{}`, testUser); code != http.StatusCreated || !strings.Contains(body, "100000001_3") {
		t.Errorf("send after starting over = %d %s", code, body)
	}
}
