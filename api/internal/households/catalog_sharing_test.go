package households

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSharingListener struct{ published chan string }

func (f *fakeSharingListener) PublishHousehold(_ context.Context, householdID string) error {
	f.published <- householdID
	return nil
}

func TestCatalogSharingIsOffUntilChosenAndSharesWhatItHas(t *testing.T) {
	svc, _ := newTestService(t)
	listener := &fakeSharingListener{published: make(chan string, 2)}
	svc.SetSharingListener(listener)
	ctx := context.Background()
	h, admin := newHousehold(t, svc, nil)

	if h.Sharing() != CatalogSharingOff {
		t.Errorf("new household Sharing() = %q, want off", h.Sharing())
	}
	if mode, err := svc.CatalogSharing(ctx, h.ID); err != nil || mode != CatalogSharingOff {
		t.Errorf("CatalogSharing() = %q, %v", mode, err)
	}

	all := CatalogSharingAll
	updated, err := svc.Update(ctx, admin, UpdateInput{CatalogSharing: &all})
	if err != nil || updated.Sharing() != CatalogSharingAll {
		t.Fatalf("Update(all) = %+v, %v", updated, err)
	}
	// Turning it on shares what the household already has.
	select {
	case id := <-listener.published:
		if id != h.ID {
			t.Errorf("published %q, want %q", id, h.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turning sharing on published nothing")
	}

	// Turning it off publishes nothing more.
	off := CatalogSharingOff
	if _, err := svc.Update(ctx, admin, UpdateInput{CatalogSharing: &off}); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-listener.published:
		t.Errorf("turning sharing off published %q", id)
	case <-time.After(100 * time.Millisecond):
	}

	bad := "everyone"
	var ve *ValidationError
	if _, err := svc.Update(ctx, admin, UpdateInput{CatalogSharing: &bad}); !errors.As(err, &ve) {
		t.Errorf("everyone: err = %v, want invalid input", err)
	}
}
