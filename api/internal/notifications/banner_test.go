package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func referralBanner(key string) New {
	return New{
		HouseholdID: hhA, Type: TypePlusReferral, Title: "Your free month of Plus is on",
		Body:    "Your free month of Plus is on. Thanks for joining from Lines's invite.",
		Subject: Subject{Kind: SubjectPlus}, DedupeKey: key, Banner: true,
	}
}

// runBannerContract checks a store keeps Banner, lists banners alone, and
// drops one once the member read it.
func runBannerContract(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	svc := NewService(ServiceOptions{Store: store, Now: func() time.Time { return testNow }})
	if _, _, err := svc.Create(ctx, lowNotification("plain")); err != nil {
		t.Fatal(err)
	}
	banner, created, err := svc.Create(ctx, referralBanner("plus.referral:x:referred"))
	if err != nil || !created || !banner.Banner {
		t.Fatalf("create banner = %+v %v %v", banner, created, err)
	}
	// Creating it again is the same banner: shown once.
	if again, created, _ := svc.Create(ctx, referralBanner("plus.referral:x:referred")); created || again.ID != banner.ID {
		t.Errorf("second create = %+v %v", again, created)
	}
	list, err := svc.Banners(ctx, memberOf(hhA, userA))
	if err != nil || len(list) != 1 || list[0].ID != banner.ID || !list[0].Banner || list[0].Type != TypePlusReferral {
		t.Fatalf("banners = %+v %v", list, err)
	}
	if _, err := svc.MarkRead(ctx, memberOf(hhA, userA), []string{banner.ID}, false); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.Banners(ctx, memberOf(hhA, userA)); len(list) != 0 {
		t.Errorf("seen banner still listed: %+v", list)
	}
	// Each member sees it until they have.
	if list, _ := svc.Banners(ctx, memberOf(hhA, userB)); len(list) != 1 {
		t.Errorf("other member banners = %+v", list)
	}
}

func TestMemoryStoreBanners(t *testing.T) { runBannerContract(t, &memoryStore{}) }

func TestIntegrationMongoStoreBanners(t *testing.T) {
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
		t.Fatal(err)
	}
	runBannerContract(t, NewMongoStore(client.Database()))
}

func TestBannerRoutes(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	banner, _, _ := svc.Create(ctx, referralBanner("k"))
	_, _, _ = svc.Create(ctx, lowNotification("plain"))
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: fakeAuthorizer{}, Tokens: fakeTokens{}}).Mount)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/households/"+hhA+"/notifications"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userA)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	rec := do(http.MethodGet, "/banners", "")
	var resp BannersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusOK || len(resp.Items) != 1 ||
		resp.Items[0].ID != banner.ID || !resp.Items[0].Banner || resp.Items[0].Subject.Kind != SubjectPlus {
		t.Fatalf("banners = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/read", `{"ids":["`+banner.ID+`"]}`); rec.Code != http.StatusOK {
		t.Fatalf("mark seen = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/banners", "")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("after seen = %s", rec.Body.String())
	}
	// The list shows banner false for ordinary notifications.
	rec = do(http.MethodGet, "", "")
	if !strings.Contains(rec.Body.String(), `"banner":false`) || !strings.Contains(rec.Body.String(), `"banner":true`) {
		t.Errorf("list = %s", rec.Body.String())
	}
}
