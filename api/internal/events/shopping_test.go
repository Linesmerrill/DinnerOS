package events

import (
	"context"
	"strings"
	"testing"
)

func TestShoppingStoreRequestedValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload ShoppingStoreRequested
		wantErr bool
	}{
		{"catalog entry", ShoppingStoreRequested{Key: "kroger", Catalog: true}, false},
		{"free text", ShoppingStoreRequested{Key: "some-local-market"}, false},
		{"no key", ShoppingStoreRequested{Catalog: true}, true},
		{"key too long", ShoppingStoreRequested{Key: strings.Repeat("a", MaxStoreKeyLength+1)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.payload.validate(); (err != nil) != tc.wantErr {
				t.Errorf("validate() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestShoppingStoreRequestedIsServerObserved keeps the new type out of the
// ingestion endpoint: a store request is recorded by the API, never sent by a
// client.
func TestShoppingStoreRequestedIsServerObserved(t *testing.T) {
	if typeSpecs[TypeShoppingStoreRequested].client {
		t.Error("shopping.store_requested must not be a client type")
	}
	if typeSpecs[TypeShoppingStoreRequested].recipe {
		t.Error("shopping.store_requested must not require a recipeId")
	}
	svc, _ := newTestService(&memoryStore{})
	err := svc.Record(context.Background(), Event{
		HouseholdID: hhA, UserID: userA, Type: TypeShoppingStoreRequested,
		Payload: ShoppingStoreRequested{Key: "kroger", Catalog: true},
	})
	if err != nil {
		t.Fatalf("Record() = %v", err)
	}
}
