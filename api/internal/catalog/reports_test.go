package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

// memoryReports is an in-memory ReportStore.
type memoryReports struct {
	mu   sync.Mutex
	list []Report
}

func newMemoryReports() *memoryReports { return &memoryReports{} }

func (m *memoryReports) Add(_ context.Context, r Report) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, have := range m.list {
		if have.CatalogID == r.CatalogID && have.HouseholdID == r.HouseholdID {
			return false, nil
		}
	}
	m.list = append(m.list, r)
	return true, nil
}

func (m *memoryReports) Reporters(context.Context) (map[string][]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]string{}
	for _, r := range m.list {
		if !slices.Contains(out[r.CatalogID], r.HouseholdID) {
			out[r.CatalogID] = append(out[r.CatalogID], r.HouseholdID)
		}
	}
	return out, nil
}

func (m *memoryReports) All(context.Context) ([]Report, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.list), nil
}

// A household that reports a recipe never sees it again; others still do
// until enough households report it.
func TestReportHidesTheRecipe(t *testing.T) {
	ctx := t.Context()
	h := newHarness(t, nil)
	h.service.WithReports(newMemoryReports())
	id := publish(t, h, "r-1", "Odd Stew")
	publish(t, h, "r-2", "Good Soup")

	created, err := h.service.Report(ctx, Report{CatalogID: id, HouseholdID: hhAda, UserID: "u1", Reason: " Spam "})
	if err != nil || !created {
		t.Fatalf("report = %v, %v", created, err)
	}
	if again, err := h.service.Report(ctx, Report{CatalogID: id, HouseholdID: hhAda, Reason: "other"}); err != nil || again {
		t.Errorf("second report = %v, %v; want kept once", again, err)
	}
	names := func(hh string) []string {
		page, err := h.service.Discover(ctx, hh, Query{})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range page.Items {
			out = append(out, it.Content.Name)
		}
		return out
	}
	if got := names(hhAda); len(got) != 1 || got[0] != "Good Soup" {
		t.Errorf("reporter's discover = %v", got)
	}
	if got := names(hhBob); len(got) != 2 {
		t.Errorf("another household's discover = %v; want both", got)
	}
	if page, _ := h.service.Search(ctx, hhAda, Query{Text: "stew"}); len(page.Items) != 0 || page.Total != 0 {
		t.Errorf("reporter's search = %+v", page)
	}
	if _, err := h.service.Get(ctx, hhAda, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("reporter's get = %v", err)
	}
	if _, _, err := h.service.Add(ctx, hhAda, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("reporter's add = %v", err)
	}

	// Two more households report it: now no one sees it.
	for _, hh := range []string{hhBob, "66e5a1f2c3b4a5d6e7f80a03"} {
		if _, err := h.service.Report(ctx, Report{CatalogID: id, HouseholdID: hh, Reason: "inappropriate"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := names("66e5a1f2c3b4a5d6e7f80a04"); len(got) != 1 || got[0] != "Good Soup" {
		t.Errorf("after %d reports a new household sees %v", PullAfter, got)
	}
}

func TestReportNeedsAReasonAndARecipe(t *testing.T) {
	h := newHarness(t, nil)
	h.service.WithReports(newMemoryReports())
	id := publish(t, h, "r-1", "Stew")
	if _, err := h.service.Report(t.Context(), Report{CatalogID: id, HouseholdID: hhAda, Reason: "meh"}); !errors.Is(err, ErrInvalidReport) {
		t.Errorf("bad reason = %v", err)
	}
	if _, err := h.service.Report(t.Context(), Report{CatalogID: "66e5a1f2c3b4a5d6e7f8ffff", HouseholdID: hhAda, Reason: "spam"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing recipe = %v", err)
	}
}

func TestReportEndpoint(t *testing.T) {
	s := newTestServer(t)
	s.harness.service.WithReports(newMemoryReports())
	id := publish(t, s.harness, "r-1", "Odd Stew")
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/households/"+hhAda+"/catalog/recipes/"+id+"/report", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userAda)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		return rec
	}
	if rec := post(`{"reason":"bogus"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Pick a reason") {
		t.Errorf("bad reason: %d %s", rec.Code, rec.Body.String())
	}
	rec := post(`{"reason":"copyright","note":"This is my cookbook's recipe."}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("report: %d %s", rec.Code, rec.Body.String())
	}
	if resp := decode[ReportResponse](t, rec); !resp.Hidden || resp.Message != ReportThanks {
		t.Errorf("response = %+v", resp)
	}
	if resp := decode[CatalogListResponse](t, s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/discover", userAda)); len(resp.Items) != 0 {
		t.Errorf("discover after report = %+v", resp.Items)
	}
}

func TestIntegrationMongoReports(t *testing.T) {
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(t.Context(), ReportIndexes()...); err != nil {
		t.Fatal(err)
	}
	store := NewMongoReports(client.Database())
	const recipe, other = "66e5a1f2c3b4a5d6e7f8aa01", "66e5a1f2c3b4a5d6e7f8aa02"
	households := []string{hhAda, hhBob, "66e5a1f2c3b4a5d6e7f80a03"}
	for _, hh := range households {
		if created, err := store.Add(t.Context(), Report{CatalogID: recipe, HouseholdID: hh, Reason: "spam"}); err != nil || !created {
			t.Fatalf("add = %v, %v", created, err)
		}
	}
	if created, err := store.Add(t.Context(), Report{CatalogID: recipe, HouseholdID: hhAda, Reason: "other"}); err != nil || created {
		t.Errorf("repeat add = %v, %v", created, err)
	}
	if _, err := store.Add(t.Context(), Report{CatalogID: other, HouseholdID: hhAda, Reason: "spam"}); err != nil {
		t.Fatal(err)
	}
	reporters, err := store.Reporters(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Sorted(slices.Values(households))
	if !slices.Equal(reporters[recipe], want) || !slices.Equal(reporters[other], []string{hhAda}) || len(reporters) != 2 {
		t.Errorf("reporters = %v", reporters)
	}
	if err := store.PurgeHousehold(t.Context(), hhAda); err != nil {
		t.Fatal(err)
	}
	reporters, _ = store.Reporters(t.Context())
	if len(reporters[recipe]) != 2 || len(reporters[other]) != 0 {
		t.Errorf("after purge reporters = %v", reporters)
	}
}
