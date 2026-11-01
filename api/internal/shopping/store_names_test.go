package shopping

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

func TestChainName(t *testing.T) {
	for in, want := range map[string]string{
		"Fry's Food Store #622":          "Fry's Food Store",
		"Bashas' - Chandler (Ray Rd)":    "Bashas'",
		"Safeway Store 1234":             "Safeway",
		"Sprouts Farmers Market No. 412": "Sprouts Farmers Market",
		"Food City, Mesa":                "Food City",
		"Trader Joe's # 59":              "Trader Joe's",
		"99 Ranch Market":                "99 Ranch Market",
		"Corner Market | Downtown":       "Corner Market",
		"  Some Local Market  ":          "Some Local Market",
		"Store 7":                        "Store 7",
	} {
		if got := chainName(in); got != want {
			t.Errorf("chainName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveStoreName(t *testing.T) {
	for in, want := range map[string]string{
		"frys":                        "frys",
		"Fry's Food Store #622":       "frys",
		"Safeway Fuel Station":        "safeway",
		"Sprouts Farmers Market #412": "sprouts",
		"Giant Eagle Market District": "giant-eagle",
		"Giant Food":                  "giant",
		"Bashas' - Chandler":          "bashas",
		"Lunds & Byerlys Edina":       "lunds-byerlys",
		"H Mart":                      "h-mart",
		"Whole Foods Market":          "whole-foods",
	} {
		e, ok := resolveStoreName(in)
		if !ok || e.Key != want {
			t.Errorf("resolveStoreName(%q) = %q, %v; want %q", in, e.Key, ok, want)
		}
	}
	for _, in := range []string{"Fresh", "Some Local Market", "Corner Market #3", "Giantess Foods"} {
		if e, ok := resolveStoreName(in); ok {
			t.Errorf("resolveStoreName(%q) = %q, want no match", in, e.Key)
		}
	}
}

func TestNewStoreRequestSourcesAndPlaces(t *testing.T) {
	actor := memberOfHousehold(testHousehold, testUser)
	cases := []struct {
		in         StoreRequestInput
		key, name  string
		source     string
		custom     bool
		hasPlace   bool
		validation bool
	}{
		{in: StoreRequestInput{Key: "kroger"}, key: "kroger", name: "Kroger", source: SourceCatalog},
		{in: StoreRequestInput{Name: "frys"}, key: "frys", name: "Fry's", source: SourceTyped},
		{in: StoreRequestInput{Name: "Corner Market #3"}, key: "corner-market", name: "Corner Market", source: SourceTyped, custom: true},
		{
			in:  StoreRequestInput{Name: "Sprouts Farmers Market #412", Source: SourceNearby, Place: &StorePlace{Name: "Sprouts Farmers Market #412", Address: " 1 Test St, Phoenix, AZ 85004 "}},
			key: "sprouts", name: "Sprouts Farmers Market", source: SourceNearby, hasPlace: true,
		},
		{
			in:  StoreRequestInput{Name: "Corner Market - Downtown", Source: SourceNearby, Place: &StorePlace{Name: "Corner Market - Downtown", Address: "2 Test St"}},
			key: "corner-market", name: "Corner Market", source: SourceNearby, hasPlace: true,
		},
		{in: StoreRequestInput{Name: "x", Source: "guess"}, validation: true},
		{in: StoreRequestInput{Name: "Corner Market", Source: SourceNearby, Place: &StorePlace{Address: strings.Repeat("a", 201)}}, validation: true},
	}
	for _, c := range cases {
		req, err := newStoreRequest(actor, c.in, nil)
		var v *ValidationError
		if c.validation {
			if !errors.As(err, &v) {
				t.Errorf("%+v: err = %v, want validation", c.in, err)
			}
			continue
		}
		if err != nil || req.Key != c.key || req.Name != c.name || req.Source != c.source || req.Custom != c.custom || (req.Place != nil) != c.hasPlace {
			t.Errorf("%+v = %+v, %v", c.in, req, err)
		}
		if c.hasPlace && strings.HasPrefix(req.Place.Address, " ") {
			t.Errorf("place not trimmed: %+v", req.Place)
		}
	}
}

func TestIntegrationNearbyStoreRequestsRollUpAndKeepThePlace(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	first, _, err := f.svc.RequestStore(ctx, f.actor, StoreRequestInput{
		Name: "Sprouts Farmers Market #412", Source: SourceNearby,
		Place: &StorePlace{Name: "Sprouts Farmers Market #412", Address: "1 Test St, Phoenix, AZ 85004"},
	})
	if err != nil || first.Key != "sprouts" || first.Place == nil || first.Place.Address != "1 Test St, Phoenix, AZ 85004" || first.Custom {
		t.Fatalf("nearby = %+v, %v", first, err)
	}
	// Another household picks a different Sprouts: one chain, two requests.
	if _, _, err := f.svc.RequestStore(ctx, memberOfHousehold(secondHousehold, otherMember), StoreRequestInput{
		Name: "Sprouts Farmers Market", Source: SourceNearby, Place: &StorePlace{Name: "Sprouts Farmers Market", Address: "9 Test Ave"},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := f.svc.StoreCatalog(ctx, testHousehold, "sprouts")
	if err != nil || catalogItem(t, items, "sprouts").Requests != 2 {
		t.Errorf("sprouts demand = %+v, %v", items, err)
	}
	// Asking again without a place keeps the place; with one replaces it.
	again, _, err := f.svc.RequestStore(ctx, f.actor, StoreRequestInput{Key: "sprouts"})
	if err != nil || again.Place == nil || again.Place.Address != "1 Test St, Phoenix, AZ 85004" || again.Source != SourceCatalog {
		t.Errorf("again = %+v, %v", again, err)
	}
	moved, _, err := f.svc.RequestStore(ctx, f.actor, StoreRequestInput{
		Name: "Sprouts", Source: SourceNearby, Place: &StorePlace{Name: "Sprouts", Address: "5 Test Rd"},
	})
	if err != nil || moved.ID != first.ID || moved.Place.Address != "5 Test Rd" {
		t.Errorf("moved = %+v, %v", moved, err)
	}

	custom, _, err := f.svc.RequestStore(ctx, f.actor, StoreRequestInput{Name: "corner market #3"})
	if err != nil || !custom.Custom || custom.Key != "corner-market" || custom.Name != "Corner Market" {
		t.Errorf("custom = %+v, %v", custom, err)
	}
	list, _ := f.svc.ListStoreRequests(ctx, testHousehold)
	if len(list) != 2 {
		t.Errorf("list = %+v", list)
	}
}

func TestIntegrationStoreRequestPlaceOverHTTP(t *testing.T) {
	f := newFixture(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember},
		Households: fakeHouseholds{testUser: {testHousehold}},
	}).Mount)
	body := `{"name":"Fry's Food Store #622","source":"nearby","place":{"name":"Fry's Food Store #622","address":"1 Test St, Phoenix, AZ 85004"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/households/"+testHousehold+"/shopping/requests", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token-"+testUser)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req.WithContext(context.Background()))
	var out struct {
		Request StoreRequestResponse `json:"request"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusCreated || out.Request.Key != "frys" || out.Request.Place == nil || out.Request.Place.Address != "1 Test St, Phoenix, AZ 85004" || out.Request.Custom {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body.String())
	}
}
