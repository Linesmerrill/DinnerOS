package users

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newTestMongoStore(t *testing.T) *MongoStore {
	t.Helper()
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
		t.Fatalf("EnsureIndexes() error = %v", err)
	}
	return NewMongoStore(client.Database())
}

func TestIntegrationMongoStoreUsersAndIdentities(t *testing.T) {
	store := newTestMongoStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	user, err := store.CreateUser(ctx, User{DisplayName: "Ada", PrimaryEmail: "ada@example.com", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	got, err := store.GetUser(ctx, user.ID)
	if err != nil || got != user {
		t.Fatalf("GetUser() = %+v, %v; want %+v", got, err, user)
	}

	identity, err := store.CreateIdentity(ctx, AuthIdentity{
		UserID: user.ID, Provider: ProviderApple, Subject: "001.abc", CreatedAt: now, LastUsedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateIdentity() error = %v", err)
	}
	_, err = store.CreateIdentity(ctx, AuthIdentity{UserID: user.ID, Provider: ProviderApple, Subject: "001.abc"})
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate CreateIdentity() error = %v, want ErrDuplicate", err)
	}
	// Same subject from a different provider is a different identity.
	if _, err := store.CreateIdentity(ctx, AuthIdentity{
		UserID: user.ID, Provider: ProviderGoogle, Subject: "001.abc", CreatedAt: now.Add(time.Second), LastUsedAt: now,
	}); err != nil {
		t.Errorf("CreateIdentity(google) error = %v", err)
	}

	later := now.Add(time.Hour)
	if err := store.TouchIdentity(ctx, identity.ID, "ada@privaterelay.appleid.com", later); err != nil {
		t.Fatalf("TouchIdentity() error = %v", err)
	}
	found, err := store.FindIdentity(ctx, ProviderApple, "001.abc")
	if err != nil {
		t.Fatalf("FindIdentity() error = %v", err)
	}
	if found.UserID != user.ID || !found.LastUsedAt.Equal(later) || found.Email != "ada@privaterelay.appleid.com" || !found.EmailVerified {
		t.Errorf("FindIdentity() = %+v", found)
	}

	list, err := store.ListIdentities(ctx, user.ID)
	if err != nil || len(list) != 2 || list[0].Provider != ProviderApple || list[1].Provider != ProviderGoogle {
		t.Errorf("ListIdentities() = %+v, %v", list, err)
	}

	if _, err := store.FindIdentity(ctx, ProviderGoogle, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindIdentity(missing) error = %v, want ErrNotFound", err)
	}
	if err := store.TouchIdentity(ctx, "000000000000000000000000", "", later); !errors.Is(err, ErrNotFound) {
		t.Errorf("TouchIdentity(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetUser(ctx, "not-an-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUser(invalid) error = %v, want ErrNotFound", err)
	}

	if err := store.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}
	if _, err := store.GetUser(ctx, user.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUser(deleted) error = %v, want ErrNotFound", err)
	}
}

func TestIntegrationServiceFindOrCreate(t *testing.T) {
	svc := NewService(newTestMongoStore(t))
	ctx := context.Background()
	v := VerifiedIdentity{Provider: ProviderDev, Subject: "dev-1", Email: "dev@example.com", EmailVerified: true, DisplayName: "Dev"}

	first, created, err := svc.FindOrCreateByIdentity(ctx, v)
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	second, created, err := svc.FindOrCreateByIdentity(ctx, v)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("second: id=%q created=%v err=%v; want id %q", second.ID, created, err, first.ID)
	}
}
