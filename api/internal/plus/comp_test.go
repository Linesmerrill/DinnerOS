package plus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

type memoryComps struct {
	mu sync.Mutex
	m  map[string]Comp
}

func (s *memoryComps) Get(_ context.Context, id string) (Comp, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[id]
	return c, ok, nil
}

func (s *memoryComps) Grant(_ context.Context, c Comp) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[c.HouseholdID]; ok {
		return false, nil
	}
	s.m[c.HouseholdID] = c
	return true, nil
}

func (s *memoryComps) PurgeHousehold(context.Context, string) error { return nil }

// A comped household has Plus with Plus on for everyone, never sees the
// paywall, and Your Plus says it's on the house with the Founding badge.
func TestCompGivesPlus(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 23, 0, 0, 0, time.UTC)
	s, _, _, _ := detailsService(t, now)
	comps := &memoryComps{m: map[string]Comp{}}
	s.WithComps(comps)
	if ok, _ := s.HasPlus(ctx, hh); ok {
		t.Fatal("household has Plus before the comp")
	}
	if _, err := comps.Grant(ctx, Comp{HouseholdID: hh, Reason: CompReasonBeta, GrantedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Status(ctx, hh)
	if err != nil || !st.Enabled || !st.Active || st.Comp == nil {
		t.Fatalf("status = %+v, %v", st, err)
	}
	// No end date: still Plus years from now.
	s.now = func() time.Time { return now.AddDate(10, 0, 0) }
	if ok, _ := s.HasPlus(ctx, hh); !ok {
		t.Error("comp ended")
	}
	s.now = func() time.Time { return now }

	resp := statusResponse(st)
	if resp.Comp == nil || resp.Comp.Reason != CompReasonBeta || resp.Comp.ThanksTitle == "" || resp.Comp.ThanksText == "" ||
		resp.Comp.BenefitsTitle == "" || len(resp.Comp.Benefits) != len(DefaultOffer().Benefits) || len(resp.Comp.Benefits) == 0 {
		t.Errorf("status response comp = %+v", resp.Comp)
	}
	d, _ := s.Details(ctx, hh, admin("u1"))
	dr := DetailsFor(d, "", now)
	if dr.State != "comp" || dr.StateText != CompStateText || dr.Badge != FoundingBadge || dr.Plan != nil || dr.Date != nil ||
		dr.Since == nil || !dr.Since.Equal(now.Add(-time.Hour)) {
		t.Errorf("details = %+v", dr)
	}
	// A comp that ended gives nothing.
	comps.m[hh] = Comp{HouseholdID: hh, GrantedAt: now.Add(-48 * time.Hour), Until: now.Add(-time.Hour)}
	if ok, _ := s.HasPlus(ctx, hh); ok {
		t.Error("an ended comp still gives Plus")
	}
	// Another household still sees the paywall.
	r := chi.NewRouter()
	r.With(Require(s, nil)).Get("/households/{householdId}/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	comps.m[hh] = Comp{HouseholdID: hh, GrantedAt: now}
	for id, want := range map[string]int{hh: http.StatusNoContent, hhB: http.StatusPaymentRequired} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/households/"+id+"/x", nil))
		if rec.Code != want {
			t.Errorf("%s = %d, want %d", id, rec.Code, want)
		}
	}
	raw, _ := json.Marshal(statusResponse(st))
	if strings.Contains(string(raw), "—") {
		t.Errorf("status has an em dash: %s", raw)
	}
}

func TestIntegrationMongoComps(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	if err := client.EnsureIndexes(ctx, CompIndexes()...); err != nil {
		t.Fatal(err)
	}
	s := NewMongoComps(client.Database())
	at := time.Date(2026, 11, 13, 23, 0, 0, 0, time.UTC)
	if _, ok, err := s.Get(ctx, hh); ok || err != nil {
		t.Fatalf("empty = %v %v", ok, err)
	}
	if ok, err := s.Grant(ctx, Comp{HouseholdID: hh, Reason: CompReasonBeta, GrantedAt: at}); !ok || err != nil {
		t.Fatalf("grant = %v %v", ok, err)
	}
	if ok, err := s.Grant(ctx, Comp{HouseholdID: hh, Reason: "other", GrantedAt: at.Add(time.Hour)}); ok || err != nil {
		t.Errorf("second grant = %v %v", ok, err)
	}
	c, ok, err := s.Get(ctx, hh)
	if !ok || err != nil || c.Reason != CompReasonBeta || !c.GrantedAt.Equal(at) || !c.Until.IsZero() {
		t.Errorf("comp = %+v %v %v", c, ok, err)
	}
	if err := s.PurgeHousehold(ctx, hh); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, hh); ok {
		t.Error("purged comp remains")
	}
}
