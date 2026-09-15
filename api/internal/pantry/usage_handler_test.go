package pantry

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newUsageTestServer(t *testing.T) (*pantryTestServer, *usageFixture) {
	t.Helper()
	f := newUsageFixture(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: f.svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount)
	return &pantryTestServer{router: r, catalog: f.catalog}, f
}

func TestUsageHandlersFlow(t *testing.T) {
	srv, f := newUsageTestServer(t)
	base := pantryPath(hhAda)

	// Settings start at the default.
	rec := srv.do(t, http.MethodGet, base+"/settings", "", userViewer)
	wantStatus(t, rec, http.StatusOK)
	if got := strings.TrimSpace(rec.Body.String()); got != `{"lowThresholdPercent":80,"defaultLowThresholdPercent":80,"updatedBy":null,"updatedAt":null}` {
		t.Errorf("default settings = %s", got)
	}

	body := `{"name":"Butter","source":"grocery_list","quantity":"4","unit":"count","unitSize":{"quantity":"1/2","unit":"cup"},"week":"2026-W38","clientPurchaseId":"abc"}`
	rec = srv.do(t, http.MethodPost, base+"/purchases", body, userAda)
	wantStatus(t, rec, http.StatusCreated)
	res := decodeBody[RecordPurchaseResponse](t, rec)
	p, item := res.Purchase, res.Item
	if p.ItemID != item.ID || p.Source != PurchaseGroceryList || *p.Quantity != "4" || *p.Unit != "count" || *p.Week != "2026-W38" || *p.ClientPurchaseID != "abc" ||
		p.UnitSize == nil || p.UnitSize.Per != "count" || p.UnitSize.Unit != "cup" || p.UnitSize.QuantityValue != 0.5 || p.RecordedBy != userAda {
		t.Fatalf("purchase = %s", rec.Body.String())
	}
	e := item.Estimate
	if item.Status != StatusInStock || item.StatusSource != StatusSourcePerson || item.LowThresholdPercent != nil || item.UnitSize == nil || e == nil ||
		e.CycleID != p.ID || e.CycleSource != CycleGroceryList || e.Unit != "cup" || e.StartAmount.Quantity != "2" || e.Remaining.QuantityValue != 2 ||
		e.PercentRemaining != 100 || e.DailyRate != nil || e.AdjustedAt != nil || e.LowThresholdPercent != 80 || e.ThresholdSource != "household" ||
		e.Summary != "About 100% left." || e.BelowThreshold {
		t.Fatalf("item = %s", rec.Body.String())
	}
	rec = srv.do(t, http.MethodPost, base+"/purchases", body, userAda)
	wantStatus(t, rec, http.StatusOK)
	if again := decodeBody[RecordPurchaseResponse](t, rec); again.Purchase.ID != p.ID {
		t.Errorf("retry = %s", rec.Body.String())
	}

	// Cook, then read the explanation on the list.
	f.advance(3600e9)
	f.cook(t, "entry1", 4)
	rec = srv.do(t, http.MethodGet, base, "", userViewer)
	wantStatus(t, rec, http.StatusOK)
	list := decodeBody[PantryListResponse](t, rec)
	if len(list.Items) != 1 || list.Items[0].Estimate.RecipeUse.Count != 1 || list.Items[0].Estimate.RecipeUse.Quantity != "1/4" ||
		list.Items[0].Estimate.Summary != "About 88% left: 1 recipe used ¼ cup." {
		t.Fatalf("list = %s", rec.Body.String())
	}
	for _, field := range []string{`"otherUse":{"quantity":"0","quantityValue":0}`, `"dailyRate":null`, `"skippedRecipes":0`, `"statusSource":"person"`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("list missing %s: %s", field, rec.Body.String())
		}
	}

	// Item threshold override and clearing it.
	rec = srv.do(t, http.MethodPatch, base+"/"+item.ID, `{"lowThresholdPercent":95}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeBody[PantryItemResponse](t, rec); *got.LowThresholdPercent != 95 || got.Estimate.ThresholdSource != "item" {
		t.Errorf("override = %s", rec.Body.String())
	}
	rec = srv.do(t, http.MethodPatch, base+"/"+item.ID, `{"lowThresholdPercent":null}`, userAda)
	if !strings.Contains(rec.Body.String(), `"lowThresholdPercent":null`) {
		t.Errorf("cleared override = %s", rec.Body.String())
	}

	// Household threshold.
	rec = srv.do(t, http.MethodPut, base+"/settings", `{"lowThresholdPercent":10}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	settings := decodeBody[PantrySettingsResponse](t, rec)
	if settings.LowThresholdPercent != 10 || *settings.UpdatedBy != userAda || settings.UpdatedAt == nil {
		t.Errorf("settings = %s", rec.Body.String())
	}
	// 12% used now crosses 10%: the next read marks it low.
	rec = srv.do(t, http.MethodGet, base, "", userViewer)
	if got := decodeBody[PantryListResponse](t, rec).Items[0]; got.Status != StatusLow || got.StatusSource != StatusSourceEstimate || !got.Estimate.BelowThreshold {
		t.Errorf("after threshold = %s", rec.Body.String())
	}

	rec = srv.do(t, http.MethodGet, base+"/"+item.ID+"/purchases", "", userViewer)
	wantStatus(t, rec, http.StatusOK)
	if purchases := decodeBody[PurchaseListResponse](t, rec); len(purchases.Items) != 1 || purchases.Items[0].ID != p.ID {
		t.Errorf("purchases = %s", rec.Body.String())
	}

	for _, tc := range []struct {
		method, path, body, user string
		status                   int
		code                     string
	}{
		{http.MethodPost, base + "/purchases", `{"name":"Salt","source":"provider"}`, userAda, http.StatusBadRequest, "validation_failed"},
		{http.MethodPost, base + "/purchases", `{"name":"Salt","source":"manual","unitSize":{}}`, userAda, http.StatusBadRequest, "validation_failed"},
		{http.MethodPost, base + "/purchases", `{"name":"Salt","source":"manual","quantity":2}`, userAda, http.StatusBadRequest, "invalid_request"},
		{http.MethodPost, base + "/purchases", `{"itemId":"66e5a1f2c3b4a5d6e7f80d99","source":"manual"}`, userAda, http.StatusNotFound, "not_found"},
		{http.MethodPost, base + "/purchases", `{"name":"Salt","source":"manual"}`, userViewer, http.StatusForbidden, "forbidden"},
		{http.MethodGet, base + "/66e5a1f2c3b4a5d6e7f80d99/purchases", "", userAda, http.StatusNotFound, "not_found"},
		{http.MethodPut, base + "/settings", `{}`, userAda, http.StatusBadRequest, "validation_failed"},
		{http.MethodPut, base + "/settings", `{"lowThresholdPercent":101}`, userAda, http.StatusBadRequest, "validation_failed"},
		{http.MethodPut, base + "/settings", `{"lowThresholdPercent":50}`, userViewer, http.StatusForbidden, "forbidden"},
		{http.MethodPatch, base + "/" + item.ID, `{"lowThresholdPercent":0}`, userAda, http.StatusBadRequest, "validation_failed"},
		{http.MethodGet, pantryPath(hhBob) + "/settings", "", userAda, http.StatusNotFound, "not_found"},
	} {
		rec := srv.do(t, tc.method, tc.path, tc.body, tc.user)
		wantError(t, rec, tc.status, tc.code)
	}
}
