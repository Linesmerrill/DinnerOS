package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type fakeRevoker struct {
	calls []string
	rec   *recorder
}

func (f *fakeRevoker) RevokeForDeletion(ctx context.Context, userID, code string) {
	if _, ok := ctx.Deadline(); !ok {
		code = "no deadline"
	}
	f.calls = append(f.calls, userID+":"+code)
	if f.rec != nil {
		f.rec.add("revoke apple")
	}
}

type orderedDeleter struct{ rec *recorder }

func (d orderedDeleter) Delete(context.Context, string) (Result, error) {
	d.rec.add("delete")
	return Result{}, nil
}

func deleteMe(t *testing.T, opts HandlerOptions, body string) *httptest.ResponseRecorder {
	t.Helper()
	opts.Tokens = fakeTokens{"good": userAda}
	r := chi.NewRouter()
	NewHandler(opts).Mount(r)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(http.MethodDelete, "/me", nil)
	} else {
		req = httptest.NewRequest(http.MethodDelete, "/me", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer good")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestHandlerRevokesAppleBeforeDeleting(t *testing.T) {
	rec := &recorder{}
	apple := &fakeRevoker{rec: rec}
	res := deleteMe(t, HandlerOptions{Service: orderedDeleter{rec}, Apple: apple}, `{"appleAuthorizationCode":"fresh-code"}`)
	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s)", res.Code, res.Body)
	}
	if strings.Join(rec.calls, ",") != "revoke apple,delete" {
		t.Errorf("calls = %v", rec.calls)
	}
	if len(apple.calls) != 1 || apple.calls[0] != userAda+":fresh-code" {
		t.Errorf("revoke calls = %v", apple.calls)
	}
}

func TestHandlerDeleteWithoutABody(t *testing.T) {
	// The app before build 116, and Google users, send no body.
	for _, body := range []string{"", "  ", "{}"} {
		apple := &fakeRevoker{}
		deleter := &fakeDeleter{}
		res := deleteMe(t, HandlerOptions{Service: deleter, Apple: apple}, body)
		if res.Code != http.StatusNoContent || deleter.userID != userAda {
			t.Errorf("body %q: status = %d, deleted %q", body, res.Code, deleter.userID)
		}
		if len(apple.calls) != 1 || apple.calls[0] != userAda+":" {
			t.Errorf("body %q: revoke calls = %v", body, apple.calls)
		}
	}
}

func TestHandlerDeleteIgnoresAnOversizedCode(t *testing.T) {
	apple := &fakeRevoker{}
	deleter := &fakeDeleter{}
	res := deleteMe(t, HandlerOptions{Service: deleter, Apple: apple},
		`{"appleAuthorizationCode":"`+strings.Repeat("c", maxAppleCodeLength+1)+`"}`)
	if res.Code != http.StatusNoContent || len(apple.calls) != 1 || apple.calls[0] != userAda+":" {
		t.Errorf("status = %d, revoke calls = %v", res.Code, apple.calls)
	}
}

func TestHandlerDeleteRejectsABadBody(t *testing.T) {
	for _, body := range []string{"not json", `{"other":1}`} {
		deleter := &fakeDeleter{}
		res := deleteMe(t, HandlerOptions{Service: deleter, Apple: &fakeRevoker{}}, body)
		if res.Code != http.StatusBadRequest || deleter.userID != "" {
			t.Errorf("body %q: status = %d, deleted %q", body, res.Code, deleter.userID)
		}
	}
}

func TestHandlerDeleteWithoutAppleRevoker(t *testing.T) {
	deleter := &fakeDeleter{}
	res := deleteMe(t, HandlerOptions{Service: deleter}, `{"appleAuthorizationCode":"code"}`)
	if res.Code != http.StatusNoContent || deleter.userID != userAda {
		t.Errorf("status = %d, deleted %q", res.Code, deleter.userID)
	}
}
