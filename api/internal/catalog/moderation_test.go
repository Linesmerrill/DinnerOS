package catalog

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

type memoryBlocks struct {
	mu   sync.Mutex
	list []Block
}

func (m *memoryBlocks) Add(_ context.Context, b Block) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, have := range m.list {
		if have.HouseholdID == b.HouseholdID && have.CatalogID == b.CatalogID {
			return nil
		}
	}
	m.list = append(m.list, b)
	return nil
}

func (m *memoryBlocks) Blocks(_ context.Context, householdID string) ([]Block, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Block
	for _, b := range m.list {
		if b.HouseholdID == householdID {
			out = append(out, b)
		}
	}
	return out, nil
}

type memoryReviews struct {
	mu        sync.Mutex
	decisions map[string]string
}

func (m *memoryReviews) Decide(_ context.Context, r Review) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.decisions == nil {
		m.decisions = map[string]string{}
	}
	m.decisions[r.CatalogID] = r.Decision
	return nil
}

func (m *memoryReviews) Decisions(context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.decisions {
		out[k] = v
	}
	return out, nil
}

const (
	hhCook  = "66e5a1f2c3b4a5d6e7f80b01"
	hhOther = "66e5a1f2c3b4a5d6e7f80b02"
	hhNew   = "66e5a1f2c3b4a5d6e7f80b03"
)

// publishTyped publishes a recipe typed by householdID's cook.
func publishTyped(t *testing.T, h *harness, householdID, sourceID, name string) string {
	t.Helper()
	return publishWith(t, h, sourceID, name, func(r *recipes.Recipe) {
		r.Source, r.HouseholdID = recipes.SourceManual, householdID
	})
}

func discoverNames(t *testing.T, h *harness, householdID string) []string {
	t.Helper()
	page, err := h.service.Discover(t.Context(), householdID, Query{})
	if err != nil {
		t.Fatal(err)
	}
	return resultNames(page)
}

func TestPublishedEntriesCarryAnOpaquePublisher(t *testing.T) {
	h := newHarness(t, nil)
	typed := publishTyped(t, h, hhCook, "m-1", "Cook Stew")
	kit := publish(t, h, "kit-1", "Kit Curry")
	e, err := h.store.Get(t.Context(), typed)
	if err != nil {
		t.Fatal(err)
	}
	if e.Publisher == "" || strings.Contains(e.Publisher, hhCook) {
		t.Errorf("typed entry publisher = %q; want an opaque hash", e.Publisher)
	}
	if k, _ := h.store.Get(t.Context(), kit); k.Publisher != "" {
		t.Errorf("an importer's entry has publisher %q", k.Publisher)
	}
}

// Hiding a cook hides every recipe that household shared, from the hiding
// household only; an importer's recipe hides just itself.
func TestHideCookHidesEveryRecipeFromThatCook(t *testing.T) {
	ctx := t.Context()
	h := newHarness(t, nil)
	h.service.WithModeration(&memoryBlocks{}, &memoryReviews{})
	stew := publishTyped(t, h, hhCook, "m-1", "Cook Stew")
	publishTyped(t, h, hhCook, "m-2", "Cook Pie")
	publishTyped(t, h, hhOther, "m-3", "Other Soup")
	kit := publish(t, h, "kit-1", "Kit Curry")

	e, err := h.service.Block(ctx, hhNew, "u1", stew)
	if err != nil || !HidesCook(e) {
		t.Fatalf("block = %+v, %v", e, err)
	}
	if got := discoverNames(t, h, hhNew); !slices.Equal(got, []string{"Kit Curry", "Other Soup"}) {
		t.Errorf("discover after hiding the cook = %v", got)
	}
	if page, _ := h.service.Search(ctx, hhNew, Query{Text: "pie"}); len(page.Items) != 0 {
		t.Errorf("search still shows the cook: %+v", page.Items)
	}
	if _, err := h.service.Get(ctx, hhNew, stew); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after hiding = %v", err)
	}
	if got := discoverNames(t, h, hhBob); len(got) != 4 {
		t.Errorf("another household sees %v; want all 4", got)
	}

	e, err = h.service.Block(ctx, hhNew, "u1", kit)
	if err != nil || HidesCook(e) {
		t.Fatalf("block kit = %+v, %v", e, err)
	}
	if got := discoverNames(t, h, hhNew); !slices.Equal(got, []string{"Other Soup"}) {
		t.Errorf("discover after hiding the kit recipe = %v", got)
	}
	if _, err := h.service.Block(ctx, hhNew, "u1", "66e5a1f2c3b4a5d6e7f8ffff"); !errors.Is(err, ErrNotFound) {
		t.Errorf("hiding an unknown recipe = %v", err)
	}
}

// An operator's keep stops reports pulling a recipe for everyone; a remove
// pulls it at once.
func TestReviewKeepsOrRemovesAReportedRecipe(t *testing.T) {
	ctx := t.Context()
	h := newHarness(t, nil)
	h.service.WithReports(newMemoryReports()).WithModeration(&memoryBlocks{}, &memoryReviews{})
	odd := publish(t, h, "r-1", "Odd Stew")
	bad := publish(t, h, "r-2", "Bad Pie")
	publish(t, h, "r-3", "Good Soup")
	for _, hh := range []string{hhAda, hhBob, hhCook} {
		if _, err := h.service.Report(ctx, Report{CatalogID: odd, HouseholdID: hh, Reason: "wrong", Note: "Too salty."}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.service.Report(ctx, Report{CatalogID: bad, HouseholdID: hhAda, Reason: "inappropriate"}); err != nil {
		t.Fatal(err)
	}
	if got := discoverNames(t, h, hhNew); slices.Contains(got, "Odd Stew") {
		t.Fatalf("a recipe %d households reported is still shown: %v", PullAfter, got)
	}
	list, err := h.service.Reported(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].CatalogID != odd || list[0].Households != 3 || !list[0].HiddenForEveryone ||
		list[0].Name != "Odd Stew" || list[1].HiddenForEveryone {
		t.Fatalf("reported = %+v", list)
	}

	if err := h.service.Review(ctx, Review{CatalogID: odd, Decision: " Keep "}); err != nil {
		t.Fatal(err)
	}
	if err := h.service.Review(ctx, Review{CatalogID: bad, Decision: "remove"}); err != nil {
		t.Fatal(err)
	}
	if got := discoverNames(t, h, hhNew); !slices.Equal(got, []string{"Good Soup", "Odd Stew"}) {
		t.Errorf("after review a new household sees %v", got)
	}
	if got := discoverNames(t, h, hhAda); slices.Contains(got, "Odd Stew") {
		t.Errorf("a reporter sees the kept recipe again: %v", got)
	}
	if err := h.service.Review(ctx, Review{CatalogID: odd, Decision: "maybe"}); !errors.Is(err, ErrInvalidReview) {
		t.Errorf("bad decision = %v", err)
	}
	list, _ = h.service.Reported(ctx)
	if list[0].Decision == "" || list[1].Decision == "" {
		t.Errorf("reviewed list = %+v", list)
	}
}

func newModerationServer(t *testing.T) *testServer {
	t.Helper()
	h := newHarness(t, nil)
	h.service.WithReports(newMemoryReports()).WithModeration(&memoryBlocks{}, &memoryReviews{})
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: h.service, Authorizer: testAuthorizer, Tokens: fakeTokens{}, Operators: []string{userAda},
	}).Mount)
	return &testServer{router: r, harness: h}
}

func TestHideCookEndpoint(t *testing.T) {
	s := newModerationServer(t)
	stew := publishTyped(t, s.harness, hhCook, "m-1", "Cook Stew")
	kit := publish(t, s.harness, "kit-1", "Kit Curry")
	base := "/api/v1/households/" + hhBob + "/catalog/recipes/"

	detail := decode[CatalogRecipeResponse](t, s.do(t, http.MethodGet, base+stew, userBob))
	if !detail.CanHideCook {
		t.Error("a typed recipe can't hide its cook")
	}
	if d := decode[CatalogRecipeResponse](t, s.do(t, http.MethodGet, base+kit, userBob)); d.CanHideCook {
		t.Error("an importer's recipe offers to hide a cook")
	}
	rec := s.send(t, http.MethodPost, base+stew+"/hide-cook", userBob, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("hide cook = %d (%s)", rec.Code, rec.Body.String())
	}
	if resp := decode[HideCookResponse](t, rec); !resp.Hidden || resp.Message != HideCookThanks {
		t.Errorf("response = %+v", resp)
	}
	if resp := decode[HideCookResponse](t, s.send(t, http.MethodPost, base+kit+"/hide-cook", userBob, "")); resp.Message != HideRecipeThanks {
		t.Errorf("kit response = %+v", resp)
	}
	if rec := s.do(t, http.MethodGet, base+stew, userBob); rec.Code != http.StatusNotFound {
		t.Errorf("hidden recipe get = %d", rec.Code)
	}
	if rec := s.send(t, http.MethodPost, "/api/v1/households/"+hhAda+"/catalog/recipes/"+stew+"/hide-cook", userBob, ""); rec.Code != http.StatusNotFound {
		t.Errorf("hiding for another household = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, hhCook) {
		t.Errorf("response names the cook's household: %s", body)
	}
}

func TestReportAdminRoutesAreForOperators(t *testing.T) {
	s := newModerationServer(t)
	odd := publish(t, s.harness, "r-1", "Odd Stew")
	if _, err := s.harness.service.Report(t.Context(), Report{CatalogID: odd, HouseholdID: hhBob, UserID: userBob, Reason: "spam", Note: "Ads."}); err != nil {
		t.Fatal(err)
	}
	const list = "/api/v1/admin/catalog/reports"
	review := list + "/" + odd + "/review"
	if rec := s.do(t, http.MethodGet, list, userBob); rec.Code != http.StatusForbidden {
		t.Errorf("non-operator list = %d", rec.Code)
	}
	if rec := s.send(t, http.MethodPost, review, userBob, `{"decision":"remove"}`); rec.Code != http.StatusForbidden {
		t.Errorf("non-operator review = %d", rec.Code)
	}
	rec := s.do(t, http.MethodGet, list, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d (%s)", rec.Code, rec.Body.String())
	}
	resp := decode[ReportListResponse](t, rec)
	if len(resp.Recipes) != 1 || resp.Recipes[0].Name != "Odd Stew" || resp.Recipes[0].Reports[0].Note != "Ads." {
		t.Fatalf("list = %+v", resp)
	}
	if body := rec.Body.String(); strings.Contains(body, hhBob) || strings.Contains(body, userBob) {
		t.Errorf("the list names who reported: %s", body)
	}
	if rec := s.send(t, http.MethodPost, review, userAda, `{"decision":"later"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad decision = %d", rec.Code)
	}
	if rec := s.send(t, http.MethodPost, review, userAda, `{"decision":"remove"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("review = %d (%s)", rec.Code, rec.Body.String())
	}
	if got := discoverNames(t, s.harness, hhNew); slices.Contains(got, "Odd Stew") {
		t.Errorf("a removed recipe is shown: %v", got)
	}
}

type sentAlert struct{ to, subject, text string }

type fakeAlerts chan sentAlert

func (f fakeAlerts) SendPlain(_ context.Context, to, subject, text string) error {
	f <- sentAlert{to, subject, text}
	return nil
}

// Each new report is emailed to the operators, once, naming no household.
func TestReportEmailsTheOperators(t *testing.T) {
	h := newHarness(t, nil)
	h.service.WithReports(newMemoryReports())
	alerts := make(fakeAlerts, 4)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: h.service, Authorizer: testAuthorizer, Tokens: fakeTokens{},
		ReportAlerts: alerts, ReportAlertTo: "ops@example.com",
	}).Mount)
	s := &testServer{router: r, harness: h}
	odd := publish(t, h, "r-1", "Odd Stew")
	path := "/api/v1/households/" + hhBob + "/catalog/recipes/" + odd + "/report"
	if rec := s.send(t, http.MethodPost, path, userBob, `{"reason":"inappropriate","note":"Rude words."}`); rec.Code != http.StatusOK {
		t.Fatalf("report = %d (%s)", rec.Code, rec.Body.String())
	}
	got := <-alerts
	if got.to != "ops@example.com" || got.subject != ReportAlertSubject {
		t.Errorf("alert = %+v", got)
	}
	for _, want := range []string{"Odd Stew", odd, "inappropriate", "Rude words.", "reported it: 1", "/review"} {
		if !strings.Contains(got.text, want) {
			t.Errorf("alert text has no %q:\n%s", want, got.text)
		}
	}
	if strings.Contains(got.text, hhBob) || strings.Contains(got.text, userBob) {
		t.Errorf("alert names who reported:\n%s", got.text)
	}
	// A repeat report from the same household is not emailed again.
	s.send(t, http.MethodPost, path, userBob, `{"reason":"spam"}`)
	select {
	case again := <-alerts:
		t.Errorf("a repeat report was emailed: %+v", again)
	default:
	}
}

func TestIntegrationMongoModeration(t *testing.T) {
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(t.Context(), ModerationIndexes()...); err != nil {
		t.Fatal(err)
	}
	blocks, reviews := NewMongoBlocks(client.Database()), NewMongoReviews(client.Database())
	const recipe, other = "66e5a1f2c3b4a5d6e7f8aa01", "66e5a1f2c3b4a5d6e7f8aa02"
	for range 2 {
		if err := blocks.Add(t.Context(), Block{HouseholdID: hhAda, CatalogID: recipe, Publisher: "p1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := blocks.Add(t.Context(), Block{HouseholdID: hhBob, CatalogID: other}); err != nil {
		t.Fatal(err)
	}
	got, err := blocks.Blocks(t.Context(), hhAda)
	if err != nil || len(got) != 1 || got[0].Publisher != "p1" || got[0].CatalogID != recipe {
		t.Fatalf("blocks = %+v, %v", got, err)
	}
	if err := blocks.PurgeHousehold(t.Context(), hhAda); err != nil {
		t.Fatal(err)
	}
	if got, _ := blocks.Blocks(t.Context(), hhAda); len(got) != 0 {
		t.Errorf("after purge blocks = %+v", got)
	}
	if got, _ := blocks.Blocks(t.Context(), hhBob); len(got) != 1 {
		t.Errorf("purge took another household's blocks: %+v", got)
	}

	if err := reviews.Decide(t.Context(), Review{CatalogID: recipe, Decision: DecisionKeep}); err != nil {
		t.Fatal(err)
	}
	if err := reviews.Decide(t.Context(), Review{CatalogID: recipe, Decision: DecisionRemove}); err != nil {
		t.Fatal(err)
	}
	decisions, err := reviews.Decisions(t.Context())
	if err != nil || len(decisions) != 1 || decisions[recipe] != DecisionRemove {
		t.Errorf("decisions = %v, %v", decisions, err)
	}
}

func TestIntegrationMongoIDsByPublisher(t *testing.T) {
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(t.Context(), Indexes()...); err != nil {
		t.Fatal(err)
	}
	store := NewMongoStore(client.Database())
	typed := libraryRecipe(recipes.SourceManual, "m-1", "Cook Stew", "olive oil")
	typed.HouseholdID = hhCook
	kit := libraryRecipe(recipes.SourceImport, "kit-1", "Kit Curry", "olive oil")
	entries := []Recipe{
		{CatalogKey: recipes.CatalogKey(typed), Origin: recipes.CatalogOrigin(typed), Publisher: recipes.CatalogPublisherKey(typed), Content: Content(typed)},
		{CatalogKey: recipes.CatalogKey(kit), Content: Content(kit)},
	}
	if _, err := store.Upsert(t.Context(), entries, testNow); err != nil {
		t.Fatal(err)
	}
	ids, err := store.IDsByPublisher(t.Context(), []string{recipes.CatalogPublisherKey(typed)})
	if err != nil || len(ids) != 1 {
		t.Fatalf("ids = %v, %v", ids, err)
	}
	e, err := store.Get(t.Context(), ids[0])
	if err != nil || e.Content.Name != "Cook Stew" || e.Publisher != recipes.CatalogPublisherKey(typed) {
		t.Errorf("entry = %+v, %v", e, err)
	}
	if ids, _ := store.IDsByPublisher(t.Context(), nil); len(ids) != 0 {
		t.Errorf("no publishers = %v", ids)
	}
}
