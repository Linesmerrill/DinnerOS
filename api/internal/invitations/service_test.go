package invitations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

var testNow = time.Date(2026, 9, 14, 18, 30, 0, 0, time.UTC)

const (
	householdID    = "111111111111111111111111"
	otherHousehold = "222222222222222222222222"
	userAda        = "aaaaaaaaaaaaaaaaaaaaaaaa" // admin
	userBob        = "bbbbbbbbbbbbbbbbbbbbbbbb" // member
	userCat        = "cccccccccccccccccccccccc" // outsider
	userDan        = "dddddddddddddddddddddddd" // outsider
)

type testEnv struct {
	svc        *Service
	store      *memoryStore
	households *fakeHouseholds
	email      *fakeEmail
	clock      *fakeClock
	logs       *bytes.Buffer
	ada, bob   households.Membership
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{
		store:      &memoryStore{},
		households: newFakeHouseholds(),
		email:      &fakeEmail{},
		clock:      &fakeClock{t: testNow},
		logs:       &bytes.Buffer{},
	}
	env.households.addHousehold(householdID, "The Lines")
	env.households.addHousehold(otherHousehold, "Cabin")
	env.ada = env.households.setMember(householdID, userAda, households.RoleAdmin)
	env.bob = env.households.setMember(householdID, userBob, households.RoleMember)
	env.svc = NewService(ServiceOptions{
		Store:      env.store,
		Households: env.households,
		Users:      fakeUsers{userAda: "Ada", userBob: "Bob"},
		Email:      env.email,
		Logger:     slog.New(slog.NewJSONHandler(env.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:        env.clock.Now,
	})
	return env
}

// invite creates an invitation as Ada and returns the result and raw token.
func (e *testEnv) invite(t *testing.T, email string, role households.Role) (CreateResult, string) {
	t.Helper()
	res, err := e.svc.Create(context.Background(), e.ada, email, role)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	token, ok := strings.CutPrefix(e.email.last().AcceptURL, DefaultAcceptURLBase)
	if !ok && res.EmailDelivered {
		t.Fatalf("accept URL %q lacks base", e.email.last().AcceptURL)
	}
	return res, token
}

func (e *testEnv) assertNoSecretsLogged(t *testing.T, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if s != "" && strings.Contains(e.logs.String(), s) {
			t.Errorf("logs contain secret %q", s)
		}
	}
}

func TestCreateInvitation(t *testing.T) {
	env := newTestEnv(t)
	res, token := env.invite(t, "  Cat@Example.COM ", households.RoleMember)

	inv := res.Invitation
	if inv.ID == "" || inv.HouseholdID != householdID || inv.Email != "cat@example.com" || inv.Role != households.RoleMember ||
		inv.CreatedBy != userAda || !inv.CreatedAt.Equal(testNow) || !inv.ExpiresAt.Equal(testNow.Add(7*24*time.Hour)) {
		t.Errorf("invitation = %+v", inv)
	}
	if !codePattern.MatchString(strings.ReplaceAll(res.Code, "-", "")) || len(res.Code) != 11 || res.Code[5] != '-' {
		t.Errorf("code = %q, want XXXXX-XXXXX", res.Code)
	}
	if !res.EmailDelivered {
		t.Error("EmailDelivered = false")
	}

	sent := env.email.last()
	want := HouseholdInvitationEmail{
		To: "cat@example.com", HouseholdName: "The Lines", InviterName: "Ada", Role: households.RoleMember,
		Code: res.Code, AcceptURL: DefaultAcceptURLBase + token, ExpiresAt: inv.ExpiresAt,
	}
	if sent != want {
		t.Errorf("email = %+v\nwant    %+v", sent, want)
	}
	if len(token) != TokenLength {
		t.Errorf("token length = %d", len(token))
	}

	// Only hashes are stored.
	code, _ := NormalizeCode(res.Code)
	stored := env.store.snapshot()
	if len(stored) != 1 || stored[0].TokenHash != hashSecret(token) || stored[0].CodeHash != hashSecret(code) {
		t.Fatalf("stored = %+v", stored)
	}
	dump := fmt.Sprintf("%+v", stored)
	for _, secret := range []string{token, code, res.Code} {
		if strings.Contains(dump, secret) {
			t.Errorf("store contains raw secret %q", secret)
		}
	}
	env.assertNoSecretsLogged(t, token, code, res.Code)
}

func TestCreateInvitationValidationAndPermissions(t *testing.T) {
	tests := []struct {
		name    string
		actor   func(*testEnv) households.Membership
		email   string
		role    households.Role
		wantErr error
	}{
		{"empty email", func(e *testEnv) households.Membership { return e.ada }, " ", households.RoleMember, &ValidationError{}},
		{"not an email", func(e *testEnv) households.Membership { return e.ada }, "cat", households.RoleMember, &ValidationError{}},
		{"display name form", func(e *testEnv) households.Membership { return e.ada }, "Cat <cat@example.com>", households.RoleMember, &ValidationError{}},
		{"no domain dot", func(e *testEnv) households.Membership { return e.ada }, "cat@localhost", households.RoleMember, &ValidationError{}},
		{"too long", func(e *testEnv) households.Membership { return e.ada }, strings.Repeat("a", 250) + "@example.com", households.RoleMember, &ValidationError{}},
		{"invalid role", func(e *testEnv) households.Membership { return e.ada }, "cat@example.com", "owner", &ValidationError{}},
		{"member cannot invite", func(e *testEnv) households.Membership { return e.bob }, "cat@example.com", households.RoleMember, households.ErrForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			_, err := env.svc.Create(context.Background(), tt.actor(env), tt.email, tt.role)
			var ve *ValidationError
			if _, wantValidation := tt.wantErr.(*ValidationError); wantValidation {
				if !errors.As(err, &ve) {
					t.Fatalf("error = %v, want ValidationError", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if len(env.store.snapshot()) != 0 || len(env.email.sent) != 0 {
				t.Error("invitation stored or email sent despite error")
			}
		})
	}
}

func TestAcceptByToken(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	res, token := env.invite(t, "cat@example.com", households.RoleMember)

	got, err := env.svc.Accept(ctx, userCat, token, "")
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if !got.Joined || got.Household.ID != householdID || got.Household.Name != "The Lines" ||
		got.Membership.UserID != userCat || got.Membership.Role != households.RoleMember {
		t.Errorf("Accept() = %+v", got)
	}
	stored, _ := env.store.Get(ctx, householdID, res.Invitation.ID)
	if stored.AcceptedBy != userCat || !stored.AcceptedAt.Equal(testNow) {
		t.Errorf("stored = %+v, want accepted by cat", stored)
	}

	// Someone else cannot reuse it.
	if _, err := env.svc.Accept(ctx, userDan, token, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("reuse by another user error = %v, want ErrInvalid", err)
	}
	// The same user retrying (e.g. after a lost response) gets the same result.
	retry, err := env.svc.Accept(ctx, userCat, "", res.Code)
	if err != nil || retry.Joined || retry.Membership.UserID != userCat || retry.Household.ID != householdID {
		t.Errorf("retry = %+v, %v", retry, err)
	}
	// But not after leaving: a used invitation never lets someone back in.
	env.households.removeMember(householdID, userCat)
	if _, err := env.svc.Accept(ctx, userCat, token, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("retry after leaving error = %v, want ErrInvalid", err)
	}
	if !strings.Contains(env.logs.String(), "invitation rejected") {
		t.Error("rejection reason not logged")
	}
	env.assertNoSecretsLogged(t, token, res.Code)
}

func TestAcceptByCodeWithMessyInput(t *testing.T) {
	env := newTestEnv(t)
	res, _ := env.invite(t, "cat@example.com", households.RoleAdmin)

	messy := " " + strings.ToLower(res.Code[:3]) + " " + strings.ToLower(res.Code[3:8]) + " -" + res.Code[8:] + "\t"
	got, err := env.svc.Accept(context.Background(), userCat, "", messy)
	if err != nil {
		t.Fatalf("Accept(%q) error = %v", messy, err)
	}
	if got.Membership.Role != households.RoleAdmin || !got.Joined {
		t.Errorf("Accept() = %+v", got)
	}
}

func TestAcceptRejectsInvalidInput(t *testing.T) {
	env := newTestEnv(t)
	res, token := env.invite(t, "cat@example.com", households.RoleMember)
	ctx := context.Background()

	var ve *ValidationError
	if _, err := env.svc.Accept(ctx, userCat, token, res.Code); !errors.As(err, &ve) {
		t.Errorf("both token and code error = %v, want ValidationError", err)
	}
	if _, err := env.svc.Accept(ctx, userCat, " ", ""); !errors.As(err, &ve) {
		t.Errorf("neither error = %v, want ValidationError", err)
	}
	for name, try := range map[string][2]string{
		"unknown token":   {strings.Repeat("A", TokenLength), ""},
		"short token":     {"abc", ""},
		"unknown code":    {"", "00000-00000"},
		"malformed code":  {"", "ABCDE-FGHJU"},
		"wrong-size code": {"", "ABCDE"},
	} {
		if _, err := env.svc.Accept(ctx, userCat, try[0], try[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: error = %v, want ErrInvalid", name, err)
		}
	}
	if env.households.memberCount(householdID) != 2 {
		t.Error("membership created by invalid accept")
	}
}

func TestAcceptExpired(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	_, early := env.invite(t, "cat@example.com", households.RoleMember)
	_, late := env.invite(t, "dan@example.com", households.RoleMember)

	env.clock.Advance(TTL - time.Second)
	if _, err := env.svc.Accept(ctx, userCat, early, ""); err != nil {
		t.Errorf("accept one second before expiry error = %v", err)
	}
	env.clock.Advance(time.Second)
	if _, err := env.svc.Accept(ctx, userDan, late, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("accept at expiry error = %v, want ErrInvalid", err)
	}
	if !strings.Contains(env.logs.String(), `"reason":"expired"`) {
		t.Errorf("expiry reason not logged: %s", env.logs.String())
	}
	if pending, _ := env.svc.ListPending(ctx, env.ada); len(pending) != 0 {
		t.Errorf("ListPending() after expiry = %+v", pending)
	}
}

func TestRevokeInvitation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	res, token := env.invite(t, "cat@example.com", households.RoleMember)

	if pending, err := env.svc.ListPending(ctx, env.ada); err != nil || len(pending) != 1 {
		t.Fatalf("ListPending() = %+v, %v", pending, err)
	}
	if _, err := env.svc.ListPending(ctx, env.bob); !errors.Is(err, households.ErrForbidden) {
		t.Errorf("member ListPending() error = %v", err)
	}
	if err := env.svc.Revoke(ctx, env.bob, res.Invitation.ID); !errors.Is(err, households.ErrForbidden) {
		t.Errorf("member Revoke() error = %v", err)
	}
	if err := env.svc.Revoke(ctx, env.ada, res.Invitation.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if err := env.svc.Revoke(ctx, env.ada, res.Invitation.ID); err != nil {
		t.Errorf("second Revoke() error = %v, want idempotent success", err)
	}
	if _, err := env.svc.Accept(ctx, userCat, token, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("accept revoked error = %v, want ErrInvalid", err)
	}
	if pending, _ := env.svc.ListPending(ctx, env.ada); len(pending) != 0 {
		t.Errorf("ListPending() after revoke = %+v", pending)
	}

	if err := env.svc.Revoke(ctx, env.ada, "999999999999999999999999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Revoke(unknown) error = %v, want ErrNotFound", err)
	}
	otherAdmin := households.Membership{HouseholdID: otherHousehold, UserID: userDan, Role: households.RoleAdmin}
	if err := env.svc.Revoke(ctx, otherAdmin, res.Invitation.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Revoke(other household's invitation) error = %v, want ErrNotFound", err)
	}
}

func TestReinviteRevokesPrevious(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	first, firstToken := env.invite(t, "cat@example.com", households.RoleMember)
	second, secondToken := env.invite(t, "CAT@example.com", households.RoleAdmin)

	pending, err := env.svc.ListPending(ctx, env.ada)
	if err != nil || len(pending) != 1 || pending[0].ID != second.Invitation.ID {
		t.Fatalf("ListPending() = %+v, %v; want only the new invitation", pending, err)
	}
	if _, err := env.svc.Accept(ctx, userCat, firstToken, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("accept old token error = %v, want ErrInvalid", err)
	}
	if _, err := env.svc.Accept(ctx, userCat, "", first.Code); !errors.Is(err, ErrInvalid) {
		t.Errorf("accept old code error = %v, want ErrInvalid", err)
	}
	got, err := env.svc.Accept(ctx, userCat, secondToken, "")
	if err != nil || got.Membership.Role != households.RoleAdmin {
		t.Errorf("accept new token = %+v, %v", got, err)
	}

	// Inviting the same address to another household is independent.
	otherAdmin := env.households.setMember(otherHousehold, userAda, households.RoleAdmin)
	if _, err := env.svc.Create(ctx, otherAdmin, "dan@example.com", households.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Create(ctx, env.ada, "dan@example.com", households.RoleMember); err != nil {
		t.Fatal(err)
	}
	if p, _ := env.svc.ListPending(ctx, otherAdmin); len(p) != 1 {
		t.Errorf("other household pending = %+v", p)
	}
}

func TestDoubleAcceptRace(t *testing.T) {
	t.Run("conditional update rejects the loser", func(t *testing.T) {
		env := newTestEnv(t)
		ctx := context.Background()
		res, token := env.invite(t, "cat@example.com", households.RoleMember)
		env.store.beforeMarkAccepted = func() {
			// Dan wins between Cat's read and Cat's conditional update.
			if err := env.store.MarkAccepted(ctx, res.Invitation.ID, userDan, testNow); err != nil {
				t.Errorf("concurrent MarkAccepted() error = %v", err)
			}
		}
		if _, err := env.svc.Accept(ctx, userCat, token, ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Accept() error = %v, want ErrInvalid", err)
		}
		if _, err := env.households.GetMembership(ctx, householdID, userCat); !errors.Is(err, households.ErrNotFound) {
			t.Error("losing acceptance created a membership")
		}
	})

	t.Run("concurrent accepts", func(t *testing.T) {
		env := newTestEnv(t)
		ctx := context.Background()
		_, token := env.invite(t, "cat@example.com", households.RoleMember)

		const n = 20
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = env.svc.Accept(ctx, fmt.Sprintf("%024x", 0x1000+i), token, "")
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
		if succeeded != 1 || env.households.memberCount(householdID) != 3 {
			t.Errorf("successes = %d, members = %d; want 1 and 3", succeeded, env.households.memberCount(householdID))
		}
	})
}

func TestAcceptWhenAlreadyMember(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	res, token := env.invite(t, "bob@example.com", households.RoleAdmin)

	got, err := env.svc.Accept(ctx, userBob, token, "")
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if got.Joined || got.Membership.Role != households.RoleMember || got.Household.ID != householdID {
		t.Errorf("Accept() = %+v; want existing member role unchanged", got)
	}
	stored, _ := env.store.Get(ctx, householdID, res.Invitation.ID)
	if stored.AcceptedBy != userBob {
		t.Errorf("invitation not consumed: %+v", stored)
	}
	if _, err := env.svc.Accept(ctx, userCat, "", res.Code); !errors.Is(err, ErrInvalid) {
		t.Errorf("consumed invitation reused: %v", err)
	}
}

func TestEmailFailureStillReturnsCode(t *testing.T) {
	env := newTestEnv(t)
	env.email.err = errEmailDown
	ctx := context.Background()

	res, err := env.svc.Create(ctx, env.ada, "cat@example.com", households.RoleMember)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if res.EmailDelivered || res.Code == "" || res.Invitation.ID == "" {
		t.Fatalf("Create() = %+v; want stored invitation with code and EmailDelivered=false", res)
	}
	if !strings.Contains(env.logs.String(), "invitation email not delivered") {
		t.Errorf("email failure not logged: %s", env.logs.String())
	}
	if _, err := env.svc.Accept(ctx, userCat, "", res.Code); err != nil {
		t.Errorf("accept by code after email failure error = %v", err)
	}
	env.assertNoSecretsLogged(t, res.Code)
}

func TestAcceptJoinFailureRestoresInvitation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	_, token := env.invite(t, "cat@example.com", households.RoleMember)

	env.households.failAdd = errors.New("mongo unavailable")
	if _, err := env.svc.Accept(ctx, userCat, token, ""); err == nil || errors.Is(err, ErrInvalid) {
		t.Fatalf("Accept() error = %v, want internal error", err)
	}
	env.households.failAdd = nil
	if _, err := env.svc.Accept(ctx, userDan, token, ""); err != nil {
		t.Errorf("accept after restored invitation error = %v", err)
	}
}

func TestCustomAcceptURLBase(t *testing.T) {
	env := newTestEnv(t)
	env.svc.acceptURLBase = "https://example.com/invite?token="
	if _, err := env.svc.Create(context.Background(), env.ada, "cat@example.com", households.RoleMember); err != nil {
		t.Fatal(err)
	}
	if url := env.email.last().AcceptURL; !strings.HasPrefix(url, "https://example.com/invite?token=") || len(url) != len("https://example.com/invite?token=")+TokenLength {
		t.Errorf("AcceptURL = %q", url)
	}
}
