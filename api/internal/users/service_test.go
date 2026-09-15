package users

import (
	"context"
	"testing"
	"time"
)

func TestFindOrCreateByIdentity(t *testing.T) {
	ctx := context.Background()

	t.Run("creates user on first sign-in and reuses it afterwards", func(t *testing.T) {
		store := newMemoryStore()
		svc := NewService(store)

		first, created, err := svc.FindOrCreateByIdentity(ctx, VerifiedIdentity{
			Provider: ProviderApple, Subject: "001.abc", Email: "a@example.com", EmailVerified: true, DisplayName: " Ada ",
		})
		if err != nil || !created {
			t.Fatalf("first sign-in: created=%v err=%v", created, err)
		}
		if first.DisplayName != "Ada" || first.PrimaryEmail != "a@example.com" {
			t.Errorf("user = %+v", first)
		}

		second, created, err := svc.FindOrCreateByIdentity(ctx, VerifiedIdentity{Provider: ProviderApple, Subject: "001.abc"})
		if err != nil || created {
			t.Fatalf("second sign-in: created=%v err=%v", created, err)
		}
		if second.ID != first.ID {
			t.Errorf("second sign-in user %q, want %q", second.ID, first.ID)
		}
	})

	t.Run("never merges accounts by email", func(t *testing.T) {
		svc := NewService(newMemoryStore())
		apple, _, err := svc.FindOrCreateByIdentity(ctx, VerifiedIdentity{
			Provider: ProviderApple, Subject: "s1", Email: "same@example.com", EmailVerified: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		google, created, err := svc.FindOrCreateByIdentity(ctx, VerifiedIdentity{
			Provider: ProviderGoogle, Subject: "s1", Email: "same@example.com", EmailVerified: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !created || google.ID == apple.ID {
			t.Errorf("google sign-in created=%v id=%q apple id=%q; want a separate new user", created, google.ID, apple.ID)
		}
	})

	t.Run("unverified email is not stored", func(t *testing.T) {
		store := newMemoryStore()
		svc := NewService(store)
		user, _, err := svc.FindOrCreateByIdentity(ctx, VerifiedIdentity{
			Provider: ProviderGoogle, Subject: "g1", Email: "unverified@example.com",
		})
		if err != nil {
			t.Fatal(err)
		}
		if user.PrimaryEmail != "" {
			t.Errorf("PrimaryEmail = %q, want empty", user.PrimaryEmail)
		}
		ids, _ := svc.ListIdentities(ctx, user.ID)
		if len(ids) != 1 || ids[0].Email != "" || ids[0].EmailVerified {
			t.Errorf("identities = %+v", ids)
		}
	})

	t.Run("sign-in updates lastUsedAt and verified email", func(t *testing.T) {
		store := newMemoryStore()
		svc := NewService(store)
		clock := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		svc.now = func() time.Time { return clock }

		user, _, err := svc.FindOrCreateByIdentity(ctx, VerifiedIdentity{Provider: ProviderApple, Subject: "s"})
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Hour)
		if _, _, err := svc.FindOrCreateByIdentity(ctx, VerifiedIdentity{
			Provider: ProviderApple, Subject: "s", Email: "new@example.com", EmailVerified: true,
		}); err != nil {
			t.Fatal(err)
		}
		ids, _ := svc.ListIdentities(ctx, user.ID)
		if len(ids) != 1 || !ids[0].LastUsedAt.Equal(clock) || ids[0].Email != "new@example.com" || !ids[0].EmailVerified {
			t.Errorf("identity = %+v", ids)
		}
	})

	t.Run("lost sign-up race signs in to the winner and removes the orphan", func(t *testing.T) {
		store := newMemoryStore()
		svc := NewService(store)
		var winner User
		store.beforeCreateIdentity = func() {
			var err error
			winner, _, err = NewService(store).FindOrCreateByIdentity(ctx, VerifiedIdentity{Provider: ProviderApple, Subject: "race"})
			if err != nil {
				t.Errorf("winner sign-in: %v", err)
			}
		}

		got, created, err := svc.FindOrCreateByIdentity(ctx, VerifiedIdentity{Provider: ProviderApple, Subject: "race"})
		if err != nil {
			t.Fatal(err)
		}
		if created || got.ID != winner.ID {
			t.Errorf("got %q created=%v, want winner %q", got.ID, created, winner.ID)
		}
		if len(store.users) != 1 {
			t.Errorf("users = %d, want 1 (orphan removed)", len(store.users))
		}
	})

	t.Run("requires provider and subject", func(t *testing.T) {
		svc := NewService(newMemoryStore())
		for _, v := range []VerifiedIdentity{{Provider: ProviderApple}, {Subject: "x"}, {Provider: ProviderApple, Subject: "  "}} {
			if _, _, err := svc.FindOrCreateByIdentity(ctx, v); err == nil {
				t.Errorf("FindOrCreateByIdentity(%+v) error = nil", v)
			}
		}
	})
}
