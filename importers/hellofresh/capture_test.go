package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSaveAccountCaptures(t *testing.T) {
	dir := t.TempDir()
	id := "cccccccccccccccccccccccc"
	n, err := SaveAccountCaptures(dir, map[string]json.RawMessage{id: json.RawMessage(porkVariant(id))}, time.Now())
	if err != nil || n != 1 {
		t.Fatalf("SaveAccountCaptures() = %d, %v", n, err)
	}

	raws, err := LoadRawRecipes(dir)
	if err != nil {
		t.Fatalf("LoadRawRecipes() error = %v", err)
	}
	if len(raws) != 1 || raws[0].DeliveredID != id || raws[0].Origin != OriginAccount {
		t.Fatalf("loaded = %+v", raws)
	}
}

func TestSaveAccountCapturesRejectsMalformed(t *testing.T) {
	cases := map[string]map[string]json.RawMessage{
		"bad id":         {"not-an-id": json.RawMessage(porkVariant("cccccccccccccccccccccccc"))},
		"no ingredients": {"cccccccccccccccccccccccc": json.RawMessage(`{"name": "Empty", "ingredients": []}`)},
		"not json":       {"cccccccccccccccccccccccc": json.RawMessage(`[`)},
	}
	for name, captures := range cases {
		dir := t.TempDir()
		if _, err := SaveAccountCaptures(dir, captures, time.Now()); err == nil {
			t.Errorf("%s: want error", name)
		}
		if raws, _ := LoadRawRecipes(dir); len(raws) != 0 {
			t.Errorf("%s: wrote %d files, want none", name, len(raws))
		}
	}
}
