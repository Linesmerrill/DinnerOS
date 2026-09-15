package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newTestSessionStore(t *testing.T) (*MongoSessionStore, *mongodb.Client) {
	t.Helper()
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
		t.Fatalf("EnsureIndexes() error = %v", err)
	}
	return NewMongoSessionStore(client.Database()), client
}

func TestIntegrationMongoSessionStore(t *testing.T) {
	store, client := newTestSessionStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	userID := bson.NewObjectID().Hex()

	created, err := store.CreateSession(ctx, Session{
		UserID: userID, FamilyID: "fam-1", TokenHash: "hash-1",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastUsedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := store.CreateSession(ctx, Session{UserID: userID, FamilyID: "fam-1", TokenHash: "hash-1", ExpiresAt: now}); !errors.Is(err, mongodb.ErrDuplicate) {
		t.Errorf("duplicate token hash error = %v, want ErrDuplicate", err)
	}
	sibling, err := store.CreateSession(ctx, Session{UserID: userID, FamilyID: "fam-1", TokenHash: "hash-2", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateSession(ctx, Session{UserID: userID, FamilyID: "fam-2", TokenHash: "hash-3", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	found, err := store.FindSessionByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("FindSessionByTokenHash() error = %v", err)
	}
	if found.ID != created.ID || found.UserID != userID || found.FamilyID != "fam-1" || !found.ExpiresAt.Equal(now.Add(time.Hour)) ||
		found.RotatedAt != nil || found.RevokedAt != nil {
		t.Errorf("found = %+v", found)
	}
	if _, err := store.FindSessionByTokenHash(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("missing error = %v, want ErrSessionNotFound", err)
	}

	later := now.Add(time.Minute)
	if ok, err := store.MarkRotated(ctx, created.ID, later); err != nil || !ok {
		t.Fatalf("MarkRotated() = %v, %v; want true", ok, err)
	}
	if ok, err := store.MarkRotated(ctx, created.ID, later); err != nil || ok {
		t.Errorf("second MarkRotated() = %v, %v; want false", ok, err)
	}
	rotated, _ := store.FindSessionByTokenHash(ctx, "hash-1")
	if rotated.RotatedAt == nil || !rotated.RotatedAt.Equal(later) || !rotated.LastUsedAt.Equal(later) {
		t.Errorf("rotated = %+v", rotated)
	}

	if err := store.RevokeFamily(ctx, "fam-1", later); err != nil {
		t.Fatalf("RevokeFamily() error = %v", err)
	}
	for hash, wantRevoked := range map[string]bool{"hash-1": true, "hash-2": true, "hash-3": false} {
		s, _ := store.FindSessionByTokenHash(ctx, hash)
		if (s.RevokedAt != nil) != wantRevoked {
			t.Errorf("%s revoked = %v, want %v", hash, s.RevokedAt != nil, wantRevoked)
		}
	}
	if ok, _ := store.MarkRotated(ctx, sibling.ID, later); ok {
		t.Error("MarkRotated on revoked session = true, want false")
	}
	if ok, _ := store.MarkRotated(ctx, other.ID, later); !ok {
		t.Error("MarkRotated on other family = false, want true")
	}

	// The TTL index exists so expired sessions are purged.
	cur, err := client.Database().Collection(SessionsCollection).Indexes().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var specs []bson.M
	if err := cur.All(ctx, &specs); err != nil {
		t.Fatal(err)
	}
	var ttl, unique bool
	for _, spec := range specs {
		switch spec["name"] {
		case "expiresAt_ttl":
			ttl = spec["expireAfterSeconds"] != nil
		case "tokenHash_unique":
			unique = spec["unique"] == true
		}
	}
	if !ttl || !unique {
		t.Errorf("indexes = %v; want TTL on expiresAt and unique tokenHash", specs)
	}
}

func TestIntegrationTokenServiceWithMongo(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()
	svc, err := NewTokenService(store, TokenOptions{SigningKey: testSigningKey})
	if err != nil {
		t.Fatal(err)
	}
	userID := bson.NewObjectID().Hex()

	first, err := svc.IssueForNewSignIn(ctx, userID)
	if err != nil {
		t.Fatalf("IssueForNewSignIn() error = %v", err)
	}
	second, gotUser, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil || gotUser != userID {
		t.Fatalf("Refresh() = %q, %v", gotUser, err)
	}
	if _, _, err := svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("reuse error = %v, want ErrRefreshTokenReused", err)
	}
	if _, _, err := svc.Refresh(ctx, second.RefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("after reuse error = %v, want ErrRefreshTokenReused", err)
	}

	third, err := svc.IssueForNewSignIn(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, third.RefreshToken); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, _, err := svc.Refresh(ctx, third.RefreshToken); err == nil {
		t.Fatal("Refresh after Revoke succeeded")
	}
}
