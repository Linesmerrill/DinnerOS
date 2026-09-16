package httpx

import (
	"encoding/json"
	"testing"
)

func TestOptionalTellsAbsentFromNull(t *testing.T) {
	type body struct {
		Price Optional[int64] `json:"price"`
	}
	for _, tc := range []struct {
		json    string
		set     bool
		value   *int64
		wantErr bool
	}{
		{json: `{}`},
		{json: `{"price": null}`, set: true},
		{json: `{"price": 499}`, set: true, value: ptr(int64(499))},
		{json: `{"price": "4.99"}`, wantErr: true},
	} {
		var b body
		err := json.Unmarshal([]byte(tc.json), &b)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: error = %v", tc.json, err)
		}
		if tc.wantErr {
			continue
		}
		if b.Price.Set != tc.set || (b.Price.Value == nil) != (tc.value == nil) || (tc.value != nil && *b.Price.Value != *tc.value) {
			t.Errorf("%s: got %+v", tc.json, b.Price)
		}
	}
}

func ptr[T any](v T) *T { return &v }
