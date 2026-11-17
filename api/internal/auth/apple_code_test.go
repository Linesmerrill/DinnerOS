package auth

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

type savedCode struct{ userID, code string }

type fakeCodeSaver struct {
	saved chan savedCode
	block chan struct{}
}

func (f *fakeCodeSaver) SaveFromCode(ctx context.Context, userID, code string) {
	if f.block != nil {
		<-f.block
	}
	if _, ok := ctx.Deadline(); !ok {
		code = "no deadline"
	}
	f.saved <- savedCode{userID, code}
}

func TestAppleSignInPassesTheAuthorizationCodeOn(t *testing.T) {
	saver := &fakeCodeSaver{saved: make(chan savedCode, 1)}
	srv := newAuthTestServer(t, serverConfig{appleCodes: saver})

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/apple", `{"identityToken":"apple-token","nonce":"n-1","authorizationCode":"c-123"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	body := decodeBody[SessionResponse](t, rec)
	select {
	case got := <-saver.saved:
		if got.userID != body.User.ID || got.code != "c-123" {
			t.Errorf("saved %+v, want user %s code c-123", got, body.User.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("code was not passed on")
	}
	if strings.Contains(srv.logs.String(), "c-123") {
		t.Errorf("logs contain the code: %s", srv.logs.String())
	}
}

func TestAppleSignInDoesNotWaitForTheCode(t *testing.T) {
	saver := &fakeCodeSaver{saved: make(chan savedCode, 1), block: make(chan struct{})}
	srv := newAuthTestServer(t, serverConfig{appleCodes: saver})

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/apple", `{"identityToken":"apple-token","nonce":"n-1","authorizationCode":"c-123"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d while the exchange hangs", rec.Code)
	}
	close(saver.block)
	<-saver.saved
}

func TestAppleSignInSkipsMissingOrOversizedCodes(t *testing.T) {
	saver := &fakeCodeSaver{saved: make(chan savedCode, 4)}
	srv := newAuthTestServer(t, serverConfig{appleCodes: saver})

	for _, body := range []string{
		`{"identityToken":"apple-token","nonce":"n-1"}`,
		`{"identityToken":"apple-token","nonce":"n-1","authorizationCode":"` + strings.Repeat("c", maxAuthorizationCodeLength+1) + `"}`,
	} {
		if rec := srv.do(t, http.MethodPost, "/api/v1/auth/apple", body, ""); rec.Code != http.StatusOK {
			t.Errorf("status = %d, body %s", rec.Code, rec.Body)
		}
	}
	// A failed sign-in never reaches the saver either.
	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/apple", `{"identityToken":"wrong","nonce":"n-1","authorizationCode":"c"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token status = %d", rec.Code)
	}
	select {
	case got := <-saver.saved:
		t.Errorf("saved %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}
