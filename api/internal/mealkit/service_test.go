package mealkit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestService(t *testing.T, store *memoryStore, src *fakeSource) *Service {
	t.Helper()
	return NewService(ServiceOptions{
		Store:   store,
		Cipher:  testCipher(t),
		Sources: map[string]Source{SourceHelloFresh: src},
	})
}

func TestLinkStoresOnlyEncryptedTokensAndQueuesAnImport(t *testing.T) {
	store := &memoryStore{}
	src := &fakeSource{signIn: Tokens{AccessToken: "access-value", RefreshToken: "refresh-value", ExpiresAt: time.Now().Add(time.Hour)}}
	svc := newTestService(t, store, src)

	status, err := svc.Link(context.Background(), LinkRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh,
		Email: "cook@example.com", Password: "hunter2", StartImport: true,
	})
	if err != nil {
		t.Fatalf("Link() error = %v", err)
	}
	if status.Link == nil || status.Link.Status != LinkActive || status.Job == nil || status.Job.Status != JobQueued {
		t.Fatalf("status = %+v", status)
	}
	if status.Link.AccountLabel == "cook@example.com" {
		t.Error("the link carries the member's email address")
	}

	stored, err := store.GetLink(context.Background(), hhAda, SourceHelloFresh)
	if err != nil {
		t.Fatalf("GetLink() error = %v", err)
	}
	// The password is nowhere, and the tokens are only there encrypted.
	for _, secret := range []string{"hunter2", "access-value", "refresh-value"} {
		if containsBytes(stored.Secret.Ciphertext, secret) || containsBytes(stored.Secret.Key, secret) {
			t.Errorf("%q is readable in the stored link", secret)
		}
	}
	tokens, err := svc.Tokens(stored)
	if err != nil || tokens.AccessToken != "access-value" {
		t.Fatalf("Tokens() = %+v, %v", tokens, err)
	}
}

func containsBytes(b []byte, s string) bool {
	return len(s) > 0 && len(b) > 0 && string(b) != "" && indexOf(b, s) >= 0
}

func indexOf(haystack []byte, needle string) int {
	n := []byte(needle)
	for i := 0; i+len(n) <= len(haystack); i++ {
		if string(haystack[i:i+len(n)]) == needle {
			return i
		}
	}
	return -1
}

func TestLinkRejectsBadInputAndBadCredentials(t *testing.T) {
	store := &memoryStore{}
	src := &fakeSource{signIn: Tokens{AccessToken: "access"}}
	svc := newTestService(t, store, src)
	base := LinkRequest{HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Email: "cook@example.com", Password: "pw"}

	for name, mutate := range map[string]func(LinkRequest) LinkRequest{
		"no household": func(r LinkRequest) LinkRequest { r.HouseholdID = ""; return r },
		"bad source":   func(r LinkRequest) LinkRequest { r.Source = "blueapron"; return r },
		"bad email":    func(r LinkRequest) LinkRequest { r.Email = "not-an-email"; return r },
		"no password":  func(r LinkRequest) LinkRequest { r.Password = "  "; return r },
	} {
		if _, err := svc.Link(context.Background(), mutate(base)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	src.signInErr = ErrAuthExpired
	_, err := svc.Link(context.Background(), base)
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("rejected sign-in = %v, want a validation error", err)
	}
	if indexOf([]byte(validation.Message), "pw") >= 0 {
		t.Errorf("the message quotes the password: %q", validation.Message)
	}
}

func TestLinkIsDisabledWithoutAnEncryptionKey(t *testing.T) {
	svc := NewService(ServiceOptions{Store: &memoryStore{}, Sources: map[string]Source{SourceHelloFresh: &fakeSource{}}})
	if svc.Enabled() {
		t.Fatal("Enabled() is true without a cipher")
	}
	_, err := svc.Link(context.Background(), LinkRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Email: "a@b.com", Password: "pw",
	})
	if !errors.Is(err, ErrDisabled) {
		t.Errorf("Link() = %v, want ErrDisabled", err)
	}
	if _, err := svc.StartImport(context.Background(), hhAda, userAda, SourceHelloFresh); !errors.Is(err, ErrDisabled) {
		t.Errorf("StartImport() = %v, want ErrDisabled", err)
	}
}

func TestStartImportNeedsALinkAndNeverDuplicatesARun(t *testing.T) {
	store := &memoryStore{}
	src := &fakeSource{signIn: Tokens{AccessToken: "access"}}
	svc := newTestService(t, store, src)

	if _, err := svc.StartImport(context.Background(), hhAda, userAda, SourceHelloFresh); !errors.Is(err, ErrNoLink) {
		t.Fatalf("StartImport() without a link = %v, want ErrNoLink", err)
	}

	status, err := svc.Link(context.Background(), LinkRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh,
		Email: "cook@example.com", Password: "pw", StartImport: false,
	})
	if err != nil {
		t.Fatalf("Link() error = %v", err)
	}
	if status.Job != nil {
		t.Errorf("Link(StartImport: false) queued a job anyway")
	}

	first, err := svc.StartImport(context.Background(), hhAda, userAda, SourceHelloFresh)
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	second, err := svc.StartImport(context.Background(), hhAda, userAda, SourceHelloFresh)
	if err != nil || second.ID != first.ID {
		t.Fatalf("a second start made another job: %v %v", second.ID, err)
	}
}

func TestLinkAgainResumesAJobThatPausedForSignIn(t *testing.T) {
	store := &memoryStore{}
	src := &fakeSource{signIn: Tokens{AccessToken: "access"}}
	svc := newTestService(t, store, src)
	if _, err := svc.Link(context.Background(), LinkRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Email: "a@b.com", Password: "pw", StartImport: true,
	}); err != nil {
		t.Fatalf("Link() error = %v", err)
	}
	// The worker paused it waiting for a new sign-in.
	store.jobs[0].Status = JobPausedAuth

	status, err := svc.Link(context.Background(), LinkRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Email: "a@b.com", Password: "pw", StartImport: true,
	})
	if err != nil {
		t.Fatalf("re-link error = %v", err)
	}
	if len(store.jobs) != 1 {
		t.Fatalf("re-linking made a second job: %d jobs", len(store.jobs))
	}
	if store.jobs[0].Status != JobQueued || status.Job == nil || status.Job.Status != JobQueued {
		t.Errorf("job after re-link = %+v", store.jobs[0])
	}
}

func TestUnlinkDeletesTheTokensAndStopsEveryJob(t *testing.T) {
	store := &memoryStore{}
	src := &fakeSource{signIn: Tokens{AccessToken: "access"}}
	svc := newTestService(t, store, src)
	if _, err := svc.Link(context.Background(), LinkRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Email: "a@b.com", Password: "pw", StartImport: true,
	}); err != nil {
		t.Fatalf("Link() error = %v", err)
	}

	canceled, err := svc.Unlink(context.Background(), hhAda, SourceHelloFresh)
	if err != nil || canceled != 1 {
		t.Fatalf("Unlink() = %d, %v", canceled, err)
	}
	if _, err := store.GetLink(context.Background(), hhAda, SourceHelloFresh); !errors.Is(err, ErrNotFound) {
		t.Errorf("the link survived the unlink: %v", err)
	}
	if store.jobs[0].Status != JobCanceled {
		t.Errorf("job after unlink = %q", store.jobs[0].Status)
	}
	// Unlinking again is not an error.
	if _, err := svc.Unlink(context.Background(), hhAda, SourceHelloFresh); err != nil {
		t.Errorf("second Unlink() error = %v", err)
	}
}

func TestStatusReportsNothingForAHouseholdThatNeverLinked(t *testing.T) {
	svc := newTestService(t, &memoryStore{}, &fakeSource{})
	status, err := svc.Status(context.Background(), hhBob, SourceHelloFresh)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Link != nil || status.Job != nil {
		t.Errorf("status = %+v", status)
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	base, maxDelay := time.Minute, 10*time.Minute
	prev := time.Duration(0)
	for attempt := 1; attempt <= 3; attempt++ {
		d := Backoff(attempt, base, maxDelay, 0)
		if d <= prev {
			t.Errorf("Backoff(%d) = %v, not longer than %v", attempt, d, prev)
		}
		prev = d
	}
	if d := Backoff(50, base, maxDelay, 0); d != maxDelay {
		t.Errorf("Backoff(50) = %v, want the cap %v", d, maxDelay)
	}
	// Jitter only ever adds, and never more than a quarter.
	plain, jittered := Backoff(2, base, maxDelay, 0), Backoff(2, base, maxDelay, 1)
	if jittered <= plain || jittered > plain+plain/4 {
		t.Errorf("jittered = %v, plain = %v", jittered, plain)
	}
}
