package households

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNormalizeZIP(t *testing.T) {
	for in, want := range map[string]string{"85004": "85004", " 85004 ": "85004", "": "", "  ": ""} {
		if got, err := NormalizeZIP(in); err != nil || got != want {
			t.Errorf("NormalizeZIP(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"8500", "850041", "8500a", "85-04", "８５００４"} {
		var v *ValidationError
		if _, err := NormalizeZIP(bad); !errors.As(err, &v) {
			t.Errorf("NormalizeZIP(%q) = %v, want a validation error", bad, err)
		}
	}
}

func TestZIPHandler(t *testing.T) {
	srv := newHouseholdTestServer(t)
	id := srv.createHousehold(t)
	path := "/api/v1/households/" + id + "/zip"

	if rec := srv.do(t, http.MethodGet, "/api/v1/households/"+id, "", userAda); !strings.Contains(rec.Body.String(), `"zip":null`) {
		t.Errorf("new household = %s, want a null zip", rec.Body.String())
	}
	// A member who sets up shopping saves it, not only an admin.
	rec := srv.do(t, http.MethodPut, path, `{"zip":"85004"}`, userBob)
	if rec.Code != http.StatusOK || decodeBody[HouseholdResponse](t, rec).ZIP == nil || *decodeBody[HouseholdResponse](t, rec).ZIP != "85004" {
		t.Fatalf("put = %d %s", rec.Code, rec.Body.String())
	}
	if rec := srv.do(t, http.MethodGet, "/api/v1/households/"+id, "", userAda); !strings.Contains(rec.Body.String(), `"zip":"85004"`) {
		t.Errorf("read back = %s", rec.Body.String())
	}
	// Renaming leaves it alone.
	if rec := srv.do(t, http.MethodPatch, "/api/v1/households/"+id, `{"name":"Casa"}`, userAda); !strings.Contains(rec.Body.String(), `"zip":"85004"`) {
		t.Errorf("after rename = %s", rec.Body.String())
	}
	wantError(t, srv.do(t, http.MethodPut, path, `{"zip":"123"}`, userBob), http.StatusBadRequest, "validation_failed")
	wantError(t, srv.do(t, http.MethodPut, path, `{"zip":"85004"}`, "stranger"), http.StatusNotFound, "not_found")
	if rec := srv.do(t, http.MethodPut, path, `{"zip":null}`, userBob); !strings.Contains(rec.Body.String(), `"zip":null`) {
		t.Errorf("cleared = %d %s", rec.Code, rec.Body.String())
	}
}

func TestSetZIPNeedsShoppingEdit(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.SetZIP(context.Background(), Membership{HouseholdID: "h", UserID: "u", Role: Role("viewer")}, "85004"); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v", err)
	}
}

func TestIntegrationMongoStoreZIP(t *testing.T) {
	store := newTestMongoStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	h, err := store.CreateHousehold(ctx, Household{Name: "Home", DefaultServings: 2, TimeZone: "America/Denver", CreatedBy: userAda, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.UpdateHousehold(ctx, h.ID, HouseholdPatch{ZIP: ptr("85004")}, now); err != nil || got.ZIP != "85004" {
		t.Fatalf("set = %+v, %v", got, err)
	}
	if got, _ := store.GetHousehold(ctx, h.ID); got.ZIP != "85004" {
		t.Errorf("get = %q", got.ZIP)
	}
	if got, err := store.UpdateHousehold(ctx, h.ID, HouseholdPatch{ZIP: ptr("")}, now); err != nil || got.ZIP != "" {
		t.Errorf("clear = %+v, %v", got, err)
	}
}
