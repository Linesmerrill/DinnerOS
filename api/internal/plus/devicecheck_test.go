package plus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// fakeApple is DeviceCheck's two endpoints, keeping bits by device token.
type fakeApple struct {
	mu    sync.Mutex
	key   *ecdsa.PrivateKey
	bits  map[string]bool
	paths []string
}

func (f *fakeApple) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.paths = append(f.paths, r.URL.Path)
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		tok, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return &f.key.PublicKey, nil }, jwt.WithValidMethods([]string{"ES256"}))
		if err != nil || tok.Header["kid"] != "KEY1234567" {
			http.Error(w, "Unable to verify authorization token", http.StatusUnauthorized)
			return
		}
		if iss, _ := tok.Claims.GetIssuer(); iss != "TEAM123456" {
			http.Error(w, "bad issuer", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		device, _ := body["device_token"].(string)
		if device == "" || body["transaction_id"] == "" || body["timestamp"] == nil {
			http.Error(w, "Missing or badly formatted device token payload", http.StatusBadRequest)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/query_two_bits"):
			if !f.bits[device] {
				_, _ = w.Write([]byte("Failed to find bit state"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"bit0": true, "bit1": false, "last_update_time": "2026-11"})
		case strings.HasSuffix(r.URL.Path, "/v1/update_two_bits"):
			f.bits[device] = body["bit0"] == true
		default:
			http.NotFound(w, r)
		}
	})
}

func TestDeviceCheckClient(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	apple := &fakeApple{key: key, bits: map[string]bool{}}
	prod := httptest.NewServer(apple.handler(t))
	defer prod.Close()
	devApple := &fakeApple{key: key, bits: map[string]bool{}}
	dev := httptest.NewServer(http.StripPrefix("/dev", devApple.handler(t)))
	defer dev.Close()
	c := NewDeviceCheckClient(DeviceCheckOptions{
		KeyID: "KEY1234567", TeamID: "TEAM123456", PrivateKey: key,
		ProductionURL: prod.URL, DevelopmentURL: dev.URL + "/dev", Now: func() time.Time { return time.Now() },
	})
	ctx := t.Context()
	p := households.DeviceProof{Token: "dGVzdA=="}
	if tied, err := c.Query(ctx, p); err != nil || tied {
		t.Fatalf("new device = %v %v", tied, err)
	}
	if err := c.Tie(ctx, p); err != nil {
		t.Fatal(err)
	}
	if tied, err := c.Query(ctx, p); err != nil || !tied {
		t.Fatalf("tied device = %v %v", tied, err)
	}
	// A development token goes to the development server.
	if _, err := c.Query(ctx, households.DeviceProof{Token: "ZGV2", Development: true}); err != nil {
		t.Fatal(err)
	}
	if len(devApple.paths) != 1 || devApple.paths[0] != "/v1/query_two_bits" || len(apple.paths) != 3 {
		t.Errorf("development = %v, production = %v", devApple.paths, apple.paths)
	}
	// Apple's refusals come back as status and short text.
	_, err := c.Query(ctx, households.DeviceProof{Token: ""})
	var dcErr *DeviceCheckError
	if !errors.As(err, &dcErr) || dcErr.Status != http.StatusBadRequest || !strings.Contains(dcErr.Text, "Missing or badly formatted") {
		t.Errorf("bad token = %v", err)
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	wrong := NewDeviceCheckClient(DeviceCheckOptions{KeyID: "KEY1234567", TeamID: "TEAM123456", PrivateKey: other, ProductionURL: prod.URL})
	if _, err := wrong.Query(ctx, p); !errors.As(err, &dcErr) || dcErr.Status != http.StatusUnauthorized {
		t.Errorf("wrong key = %v", err)
	}
}

func TestShortText(t *testing.T) {
	if got := ShortText([]byte("  Missing or badly formatted device token payload\nmore\n")); got != "Missing or badly formatted device token payload" {
		t.Errorf("short = %q", got)
	}
	if got := ShortText([]byte(strings.Repeat("x", 500))); len(got) != 200 {
		t.Errorf("long = %d", len(got))
	}
}
