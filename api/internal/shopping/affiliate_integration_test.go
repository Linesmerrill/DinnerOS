package shopping

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// With Impact IDs set, every Walmart cart link goes through Walmart's Impact
// tracking link, and the providers list, the match, and the handoff say so
// with the disclosure the app shows next to the button. Without them, links
// go straight to Walmart and nothing is disclosed.
func TestIntegrationWalmartAffiliateLinks(t *testing.T) {
	for _, tracked := range []bool{true, false} {
		f := newFixture(t)
		opts := providers.WalmartOptions{}
		if tracked {
			opts.Affiliate = &providers.ImpactAffiliate{PublisherID: "1234567", AdID: "565706", CampaignID: "9383"}
		}
		svc := NewService(ServiceOptions{
			Store: f.store, Providers: providers.NewRegistry(providers.NewWalmart(opts)),
			Grocery: f.plans, Catalog: f.recipes, Pantry: f.pantry, Plans: f.plans, Events: f.events,
		})
		svc.now = func() time.Time { return *f.clock }
		f.svc = svc
		if _, _, err := f.svc.PutPreference(f.ctx, f.actor, "walmart", f.keys["Ground Beef"], PreferenceInput{
			ProductURL: "123456789", DisplayName: "Ground Beef", PackageSize: &PackageSize{Quantity: "16", Unit: "oz"},
		}); err != nil {
			t.Fatal(err)
		}

		r := chi.NewRouter()
		r.Route("/api/v1", NewHandler(HandlerOptions{
			Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
			Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember},
		}).Mount)
		do := func(method, path string) map[string]any {
			t.Helper()
			req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer token-"+testUser)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code >= 300 {
				t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body.String())
			}
			var out map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			return out
		}
		want := ""
		if tracked {
			want = AffiliateDisclosureText
		}

		walmart := do(http.MethodGet, "/shopping/providers")["items"].([]any)[0].(map[string]any)
		disclosure, _ := walmart["affiliateDisclosure"].(string)
		if walmart["affiliateTracked"] != tracked || disclosure != want {
			t.Errorf("tracked=%v: provider = %v", tracked, walmart)
		}
		hh := "/households/" + testHousehold + "/plans/" + testWeek + "/shopping/walmart"
		match := do(http.MethodPost, hh+"/match")
		if d, _ := match["affiliateDisclosure"].(string); match["affiliateTracked"] != tracked || d != want {
			t.Errorf("tracked=%v: match disclosure = %v %q", tracked, match["affiliateTracked"], d)
		}
		handoff := do(http.MethodPost, hh+"/handoffs")
		links, _ := handoff["cartLinks"].([]any)
		if len(links) == 0 {
			t.Fatalf("tracked=%v: no cart links in %v", tracked, handoff)
		}
		for _, l := range links {
			u := l.(map[string]any)["url"].(string)
			wrapped := strings.HasPrefix(u, providers.WalmartImpactURL+"1234567/565706/9383?veh=aff&u=")
			if wrapped != tracked || !strings.Contains(u, "addToCart") {
				t.Errorf("tracked=%v: link %s", tracked, u)
			}
		}
	}
}
