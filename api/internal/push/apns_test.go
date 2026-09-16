package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeAPNs is a local HTTP/2 TLS server standing in for both APNs gateways.
// Tests never reach Apple.
type fakeAPNs struct {
	mu       sync.Mutex
	requests []capturedRequest
	respond  func(r *http.Request) (int, string)
}

type capturedRequest struct {
	gateway string
	proto   int
	path    string
	header  http.Header
	body    map[string]any
}

func newFakeAPNs(t *testing.T, key *ecdsa.PrivateKey, respond func(r *http.Request) (int, string)) (*APNsClient, *fakeAPNs, func(time.Duration)) {
	t.Helper()
	fake := &fakeAPNs{respond: respond}
	handler := func(gateway string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			fake.mu.Lock()
			fake.requests = append(fake.requests, capturedRequest{gateway: gateway, proto: r.ProtoMajor, path: r.URL.Path, header: r.Header.Clone(), body: body})
			fake.mu.Unlock()
			status, reason := http.StatusOK, ""
			if fake.respond != nil {
				status, reason = fake.respond(r)
			}
			w.WriteHeader(status)
			if reason != "" {
				_, _ = io.WriteString(w, `{"reason":"`+reason+`"}`)
			}
		})
	}
	start := func(gateway string) *httptest.Server {
		s := httptest.NewUnstartedServer(handler(gateway))
		s.EnableHTTP2 = true
		s.StartTLS()
		t.Cleanup(s.Close)
		return s
	}
	prod, sandbox := start("production"), start("sandbox")
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	client, err := NewAPNsClient(APNsOptions{
		KeyID: "KEY1234567", TeamID: "6VTPDG2HNK", Topic: "com.linesmerrill.dinneros", PrivateKey: key,
		HTTPClient: prod.Client(), ProductionURL: prod.URL, SandboxURL: sandbox.URL,
		Now: func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	// Both servers share httptest's certificate, so one client trusts both.
	advance := func(d time.Duration) { clockMu.Lock(); now = now.Add(d); clockMu.Unlock() }
	return client, fake, advance
}

func testKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

const testDeviceToken = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"

func TestAPNsClientSendsOverHTTP2WithSignedToken(t *testing.T) {
	key := testKey(t)
	client, fake, _ := newFakeAPNs(t, key, nil)
	expiration := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	err := client.Send(context.Background(), Message{
		Token: testDeviceToken, Environment: EnvironmentSandbox, Title: "Time to order", Body: "Thursday is order day.",
		ThreadID: "shopping.order_due", CollapseID: "66e5a1f2c3b4a5d6e7f81001", Expiration: expiration,
		Data: map[string]any{"type": "shopping.order_due", "subject": map[string]string{"kind": "shopping_week", "id": "2026-W38"}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := client.Send(context.Background(), Message{Token: testDeviceToken, Environment: EnvironmentProduction, Title: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("requests = %d", len(fake.requests))
	}
	got := fake.requests[0]
	if got.gateway != "sandbox" || fake.requests[1].gateway != "production" {
		t.Errorf("gateways = %s, %s; want sandbox then production by token environment", got.gateway, fake.requests[1].gateway)
	}
	if got.proto != 2 {
		t.Errorf("proto = HTTP/%d, want HTTP/2", got.proto)
	}
	if got.path != "/3/device/"+testDeviceToken {
		t.Errorf("path = %s", got.path)
	}
	for header, want := range map[string]string{
		"apns-topic": "com.linesmerrill.dinneros", "apns-push-type": "alert", "apns-priority": "10",
		"apns-collapse-id": "66e5a1f2c3b4a5d6e7f81001", "apns-expiration": "1789603200",
	} {
		if v := got.header.Get(header); v != want {
			t.Errorf("%s = %q, want %q", header, v, want)
		}
	}
	aps, _ := got.body["aps"].(map[string]any)
	alert, _ := aps["alert"].(map[string]any)
	if alert["title"] != "Time to order" || alert["body"] != "Thursday is order day." || aps["thread-id"] != "shopping.order_due" || aps["sound"] != "default" {
		t.Errorf("aps = %v", aps)
	}
	if subject, _ := got.body["subject"].(map[string]any); subject["id"] != "2026-W38" || got.body["type"] != "shopping.order_due" {
		t.Errorf("custom data = %v", got.body)
	}

	// The provider token is an ES256 JWT with kid and iss, verifiable with the key.
	bearer, ok := strings.CutPrefix(got.header.Get("authorization"), "bearer ")
	if !ok {
		t.Fatalf("authorization = %q", got.header.Get("authorization"))
	}
	parsed, err := jwt.Parse(bearer, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		jwt.WithValidMethods([]string{"ES256"}), jwt.WithoutClaimsValidation())
	if err != nil {
		t.Fatalf("provider token: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if parsed.Header["kid"] != "KEY1234567" || claims["iss"] != "6VTPDG2HNK" || claims["iat"] == nil {
		t.Errorf("provider token header = %v claims = %v", parsed.Header, claims)
	}
}

func TestAPNsClientCachesAndRefreshesProviderToken(t *testing.T) {
	client, fake, advance := newFakeAPNs(t, testKey(t), nil)
	send := func() string {
		t.Helper()
		if err := client.Send(context.Background(), Message{Token: testDeviceToken, Environment: EnvironmentProduction, Title: "x"}); err != nil {
			t.Fatal(err)
		}
		return fake.requests[len(fake.requests)-1].header.Get("authorization")
	}
	first := send()
	advance(30 * time.Minute)
	if send() != first {
		t.Error("token re-signed after 30 minutes; APNs throttles refreshes under 20 minutes")
	}
	advance(20 * time.Minute) // 50 minutes old
	if send() == first {
		t.Error("token reused after 50 minutes; APNs rejects tokens older than an hour")
	}
}

func TestAPNsClientRetriesExpiredProviderToken(t *testing.T) {
	var calls int
	client, fake, _ := newFakeAPNs(t, testKey(t), func(*http.Request) (int, string) {
		calls++
		if calls == 1 {
			return http.StatusForbidden, "ExpiredProviderToken"
		}
		return http.StatusOK, ""
	})
	if err := client.Send(context.Background(), Message{Token: testDeviceToken, Environment: EnvironmentProduction, Title: "x"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(fake.requests) != 2 {
		t.Errorf("requests = %d, want a retry", len(fake.requests))
	}
}

func TestAPNsClientReportsInvalidTokens(t *testing.T) {
	for _, tc := range []struct {
		status      int
		reason      string
		wantInvalid bool
	}{
		{http.StatusBadRequest, "BadDeviceToken", true},
		{http.StatusGone, "Unregistered", true},
		{http.StatusBadRequest, "DeviceTokenNotForTopic", false},
		{http.StatusTooManyRequests, "TooManyRequests", false},
		{http.StatusInternalServerError, "InternalServerError", false},
	} {
		client, _, _ := newFakeAPNs(t, testKey(t), func(*http.Request) (int, string) { return tc.status, tc.reason })
		err := client.Send(context.Background(), Message{Token: testDeviceToken, Environment: EnvironmentProduction, Title: "x"})
		var apnsErr *APNsError
		if !errors.As(err, &apnsErr) || apnsErr.Reason != tc.reason || apnsErr.Status != tc.status {
			t.Errorf("%s: error = %v", tc.reason, err)
		}
		if errors.Is(err, ErrDeviceTokenInvalid) != tc.wantInvalid {
			t.Errorf("%s: invalid = %v, want %v", tc.reason, !tc.wantInvalid, tc.wantInvalid)
		}
		if err != nil && strings.Contains(err.Error(), testDeviceToken) {
			t.Errorf("%s: error leaks the device token", tc.reason)
		}
	}
}

func TestNewAPNsClientRequiresCredentials(t *testing.T) {
	if _, err := NewAPNsClient(APNsOptions{KeyID: "KEY1234567", TeamID: "6VTPDG2HNK", Topic: "x"}); err == nil {
		t.Error("NewAPNsClient without a key succeeded")
	}
}
