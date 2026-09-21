package pantry

import (
	"net/http"
	"testing"
)

func TestFreezeEndpoint(t *testing.T) {
	s := newPantryTestServer(t)
	body := `{"name":"Pork Loin","quantity":"54","unit":"oz","portions":3,
		"source":{"provider":"walmart","handoffId":"h1","lineId":"l1"}}`
	rec := s.do(t, http.MethodPost, pantryPath(hhAda)+"/freezer", body, userAda)
	wantStatus(t, rec, http.StatusCreated)
	resp := decodeBody[FreezeResponse](t, rec)
	switch {
	case resp.AlreadyFrozen:
		t.Error("alreadyFrozen = true on the first freeze")
	case resp.Item.Storage != StorageFreezer:
		t.Errorf("storage = %q, want %q", resp.Item.Storage, StorageFreezer)
	case resp.Item.Frozen == nil:
		t.Fatal("frozen is null on a freezer item")
	case resp.Item.Frozen.Portions != 3:
		t.Errorf("portions = %d, want 3", resp.Item.Frozen.Portions)
	// 54 oz in three portions is 18 oz each: five hours a pound, rounded.
	case resp.Thaw.Hours != 6:
		t.Errorf("thaw hours = %d, want 6", resp.Thaw.Hours)
	case !resp.Thaw.Measured:
		t.Error("measured = false, want true: ounces are a weight")
	}

	// The same handoff line again changes nothing.
	again := s.do(t, http.MethodPost, pantryPath(hhAda)+"/freezer", body, userAda)
	wantStatus(t, again, http.StatusOK)
	if !decodeBody[FreezeResponse](t, again).AlreadyFrozen {
		t.Error("alreadyFrozen = false on a repeat, want true")
	}
}

func TestFreezeEndpointNeedsPantryEdit(t *testing.T) {
	s := newPantryTestServer(t)
	body := `{"name":"Pork Loin","quantity":"54","unit":"oz"}`
	wantError(t, s.do(t, http.MethodPost, pantryPath(hhAda)+"/freezer", body, userViewer), http.StatusForbidden, "forbidden")
	wantError(t, s.do(t, http.MethodPost, pantryPath(hhAda)+"/freezer", body, userBob), http.StatusNotFound, "not_found")
}

func TestPantryListFiltersByStorage(t *testing.T) {
	s := newPantryTestServer(t)
	rec := s.do(t, http.MethodPost, pantryPath(hhAda), `{"name":"Olive Oil","quantity":"16","unit":"oz"}`, userAda)
	wantStatus(t, rec, http.StatusCreated)
	rec = s.do(t, http.MethodPost, pantryPath(hhAda)+"/freezer", `{"name":"Pork Loin","quantity":"54","unit":"oz"}`, userAda)
	wantStatus(t, rec, http.StatusCreated)

	freezer := decodeBody[PantryListResponse](t, s.do(t, http.MethodGet, pantryPath(hhAda)+"?storage=freezer", "", userAda))
	if len(freezer.Items) != 1 || freezer.Items[0].DisplayName != "Pork Loin" {
		t.Fatalf("storage=freezer returned %+v, want only the pork loin", freezer.Items)
	}
	// "pantry" also answers for items stored before the freezer existed.
	shelf := decodeBody[PantryListResponse](t, s.do(t, http.MethodGet, pantryPath(hhAda)+"?storage=pantry", "", userAda))
	if len(shelf.Items) != 1 || shelf.Items[0].DisplayName != "Olive Oil" {
		t.Fatalf("storage=pantry returned %+v, want only the olive oil", shelf.Items)
	}
	if shelf.Items[0].Storage != StoragePantry {
		t.Errorf("storage = %q, want %q for an ordinary item", shelf.Items[0].Storage, StoragePantry)
	}
	if shelf.Items[0].Frozen != nil {
		t.Error("frozen is set on a shelf item, want null")
	}
	wantError(t, s.do(t, http.MethodGet, pantryPath(hhAda)+"?storage=garage", "", userAda),
		http.StatusBadRequest, "validation_failed")
}
