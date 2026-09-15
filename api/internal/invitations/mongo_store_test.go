package invitations

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newTestMongoClient(t *testing.T) *mongodb.Client {
	t.Helper()
	client := mongotest.Client(t)
	for range 2 { // applying indexes twice is a no-op
		if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	return client
}

func testInvitation(email, secret string, now time.Time) Invitation {
	return Invitation{
		HouseholdID: householdID,
		Email:       email,
		Role:        households.RoleMember,
		TokenHash:   hashSecret("token-" + secret),
		CodeHash:    hashSecret("code-" + secret),
		ExpiresAt:   now.Add(TTL),
		CreatedBy:   userAda,
		CreatedAt:   now,
	}
}

func TestIntegrationMongoStoreInvitations(t *testing.T) {
	store := NewMongoStore(newTestMongoClient(t).Database())
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	a, err := store.Create(ctx, testInvitation("cat@example.com", "a", now))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got, err := store.Get(ctx, householdID, a.ID); err != nil || got != a {
		t.Fatalf("Get() = %+v, %v; want %+v", got, err, a)
	}
	if _, err := store.Get(ctx, otherHousehold, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(other household) error = %v", err)
	}
	if got, err := store.FindByTokenHash(ctx, hashSecret("token-a")); err != nil || got.ID != a.ID {
		t.Errorf("FindByTokenHash() = %+v, %v", got, err)
	}
	if got, err := store.FindByCodeHash(ctx, hashSecret("code-a")); err != nil || got.ID != a.ID {
		t.Errorf("FindByCodeHash() = %+v, %v", got, err)
	}
	if _, err := store.FindByCodeHash(ctx, hashSecret("nope")); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindByCodeHash(unknown) error = %v", err)
	}

	// Uniqueness: one pending per household+email; token and code hashes unique.
	if _, err := store.Create(ctx, testInvitation("cat@example.com", "b", now)); !errors.Is(err, ErrDuplicate) {
		t.Errorf("second pending invitation error = %v, want ErrDuplicate", err)
	}
	other := testInvitation("cat@example.com", "c", now)
	other.HouseholdID = otherHousehold
	if _, err := store.Create(ctx, other); err != nil {
		t.Errorf("same email, other household error = %v", err)
	}
	dupToken := testInvitation("dan@example.com", "d", now)
	dupToken.TokenHash = a.TokenHash
	if _, err := store.Create(ctx, dupToken); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate token hash error = %v", err)
	}
	dupCode := testInvitation("dan@example.com", "e", now)
	dupCode.CodeHash = a.CodeHash
	if _, err := store.Create(ctx, dupCode); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate code hash error = %v", err)
	}

	expired := testInvitation("old@example.com", "f", now.Add(-TTL-time.Hour))
	if _, err := store.Create(ctx, expired); err != nil {
		t.Fatal(err)
	}
	newer, err := store.Create(ctx, testInvitation("dan@example.com", "g", now.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.ListPending(ctx, householdID, now)
	if err != nil || len(pending) != 2 || pending[0].ID != newer.ID || pending[1].ID != a.ID {
		t.Errorf("ListPending() = %+v, %v; want [newer, a]", pending, err)
	}

	// Acceptance is conditional.
	if err := store.MarkAccepted(ctx, a.ID, userCat, now.Add(TTL)); !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkAccepted(at expiry) error = %v, want ErrNotFound", err)
	}
	if err := store.MarkAccepted(ctx, a.ID, userCat, now); err != nil {
		t.Fatalf("MarkAccepted() error = %v", err)
	}
	if err := store.MarkAccepted(ctx, a.ID, userDan, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("second MarkAccepted() error = %v, want ErrNotFound", err)
	}
	accepted, _ := store.Get(ctx, householdID, a.ID)
	if accepted.AcceptedBy != userCat || !accepted.AcceptedAt.Equal(now) || accepted.Pending(now) {
		t.Errorf("accepted = %+v", accepted)
	}
	if err := store.UnmarkAccepted(ctx, a.ID, userDan); !errors.Is(err, ErrNotFound) {
		t.Errorf("UnmarkAccepted(wrong user) error = %v", err)
	}
	if err := store.UnmarkAccepted(ctx, a.ID, userCat); err != nil {
		t.Fatalf("UnmarkAccepted() error = %v", err)
	}
	if restored, _ := store.Get(ctx, householdID, a.ID); !restored.Pending(now) || restored.AcceptedBy != "" {
		t.Errorf("restored = %+v", restored)
	}

	// RevokePending clears the pending slot for the address, even when expired.
	if err := store.RevokePending(ctx, householdID, "cat@example.com", now); err != nil {
		t.Fatal(err)
	}
	if revoked, _ := store.Get(ctx, householdID, a.ID); revoked.RevokedAt.IsZero() {
		t.Errorf("RevokePending did not revoke: %+v", revoked)
	}
	if _, err := store.Create(ctx, testInvitation("cat@example.com", "h", now)); err != nil {
		t.Errorf("create after RevokePending error = %v", err)
	}
	if err := store.MarkAccepted(ctx, a.ID, userCat, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkAccepted(revoked) error = %v", err)
	}
	if err := store.RevokePending(ctx, householdID, "old@example.com", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, testInvitation("old@example.com", "i", now)); err != nil {
		t.Errorf("create after revoking expired invitation error = %v", err)
	}

	// Revoke is idempotent and scoped to the household.
	if err := store.Revoke(ctx, otherHousehold, newer.ID, now); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.Get(ctx, householdID, newer.ID); !n.RevokedAt.IsZero() {
		t.Error("Revoke with the wrong household revoked the invitation")
	}
	for range 2 {
		if err := store.Revoke(ctx, householdID, newer.ID, now); err != nil {
			t.Errorf("Revoke() error = %v", err)
		}
	}
	if n, _ := store.Get(ctx, householdID, newer.ID); n.RevokedAt.IsZero() {
		t.Error("Revoke did not revoke")
	}
}

func TestIntegrationConcurrentAccept(t *testing.T) {
	client := newTestMongoClient(t)
	if err := client.EnsureIndexes(context.Background(), households.Indexes()...); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	householdService := households.NewService(households.ServiceOptions{Store: households.NewMongoStore(client.Database())})
	h, admin, err := householdService.Create(ctx, userAda, households.CreateInput{Name: "Home", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	email := &fakeEmail{}
	svc := NewService(ServiceOptions{
		Store: NewMongoStore(client.Database()), Households: householdService, Email: email,
	})

	// Re-inviting through the service revokes the first invitation in MongoDB.
	first, err := svc.Create(ctx, admin, "cat@example.com", households.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Create(ctx, admin, "cat@example.com", households.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(ctx, userCat, "", first.Code); !errors.Is(err, ErrInvalid) {
		t.Errorf("accept superseded code error = %v, want ErrInvalid", err)
	}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = svc.Accept(ctx, fmt.Sprintf("%024x", 0x2000+i), "", res.Code)
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case !errors.Is(err, ErrInvalid):
			t.Errorf("unexpected error %v", err)
		}
	}
	_, members, err := householdService.Details(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || len(members) != 2 {
		t.Errorf("successes = %d, members = %d; want 1 and 2 (household %s)", succeeded, len(members), h.ID)
	}
}
