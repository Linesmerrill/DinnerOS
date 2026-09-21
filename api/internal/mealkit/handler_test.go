package mealkit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type handlerFixture struct {
	router  chi.Router
	store   *memoryStore
	source  *fakeSource
	service *Service
}

func newHandlerFixture(t *testing.T, enabled bool) *handlerFixture {
	t.Helper()
	f := &handlerFixture{
		store:  &memoryStore{},
		source: &fakeSource{},
	}
	opts := ServiceOptions{Store: f.store, Sources: map[string]Source{SourceHelloFresh: f.source}}
	if enabled {
		opts.Cipher = testCipher(t)
	}
	f.service = NewService(opts)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.service, Authorizer: testAuthorizer, Tokens: fakeTokens{},
	}).Mount)
	f.router = r
	return f
}

func (f *handlerFixture) do(method, path, body, userID string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

const linkPath = "/api/v1/households/" + hhAda + "/meal-kit/hellofresh/link"
const statusPath = "/api/v1/households/" + hhAda + "/meal-kit/hellofresh"

func TestMealKitRoutesRequireTheImportPermission(t *testing.T) {
	f := newHandlerFixture(t, true)
	if rec := f.do(http.MethodGet, statusPath, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated = %d", rec.Code)
	}
	// Bob may view his own household but not import anywhere.
	if rec := f.do(http.MethodGet, statusPath, "", userBob); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Errorf("another household's member = %d %s", rec.Code, rec.Body)
	}
	bobPath := "/api/v1/households/" + hhBob + "/meal-kit/hellofresh"
	if rec := f.do(http.MethodGet, bobPath, "", userBob); rec.Code != http.StatusForbidden {
		t.Errorf("member without recipes.import = %d %s", rec.Code, rec.Body)
	}
}

func TestLinkStartsAnImportAndStatusReportsIt(t *testing.T) {
	f := newHandlerFixture(t, true)

	rec := f.do(http.MethodPut, linkPath, `{"accessToken":"session-value","refreshToken":"refresh-value"}`, userAda)
	var linked StatusResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &linked) != nil {
		t.Fatalf("link = %d %s", rec.Code, rec.Body)
	}
	if linked.Link == nil || linked.Link.Status != string(LinkActive) || linked.LatestJob == nil {
		t.Fatalf("link response = %+v", linked)
	}
	// Nothing about the credential comes back.
	if body := rec.Body.String(); strings.Contains(body, "hunter2") || strings.Contains(body, "cook@example.com") ||
		strings.Contains(body, "access") || strings.Contains(body, "refresh") {
		t.Errorf("the response echoes a credential: %s", body)
	}

	rec = f.do(http.MethodGet, statusPath, "", userAda)
	var status StatusResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if !status.Enabled || status.Link == nil || status.LatestJob == nil || status.LatestJob.Status != string(JobQueued) {
		t.Fatalf("status = %+v", status)
	}
	if status.LatestJob.Failures == nil {
		t.Error("failures should be an empty array, not null")
	}

	// Starting again returns the run that is already queued.
	rec = f.do(http.MethodPost, "/api/v1/households/"+hhAda+"/meal-kit/hellofresh/imports", "", userAda)
	var job JobResponse
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &job) != nil || job.ID != status.LatestJob.ID {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}

	rec = f.do(http.MethodGet, "/api/v1/households/"+hhAda+"/meal-kit/hellofresh/imports", "", userAda)
	var list JobListResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Items) != 1 {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
}

func TestUnlinkRemovesTheLinkAndIsRepeatable(t *testing.T) {
	f := newHandlerFixture(t, true)
	if rec := f.do(http.MethodPut, linkPath, `{"accessToken":"session-value"}`, userAda); rec.Code != http.StatusOK {
		t.Fatalf("link = %d %s", rec.Code, rec.Body)
	}
	for range 2 {
		if rec := f.do(http.MethodDelete, linkPath, "", userAda); rec.Code != http.StatusNoContent {
			t.Fatalf("unlink = %d %s", rec.Code, rec.Body)
		}
	}
	if _, err := f.store.GetLink(context.Background(), hhAda, SourceHelloFresh); err == nil {
		t.Error("the link survived the unlink")
	}
	if f.store.jobs[0].Status != JobCanceled {
		t.Errorf("job after unlink = %q", f.store.jobs[0].Status)
	}
}

func TestMealKitRoutesRefuseBadInputAndUnknownServices(t *testing.T) {
	f := newHandlerFixture(t, true)
	for name, body := range map[string]string{
		"no token":      `{"accessToken":"   "}`,
		"missing token": `{"refreshToken":"r"}`,
		"unknown field": `{"accessToken":"a","password":"pw"}`,
	} {
		if rec := f.do(http.MethodPut, linkPath, body, userAda); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d %s", name, rec.Code, rec.Body)
		}
	}
	unknown := "/api/v1/households/" + hhAda + "/meal-kit/blueapron"
	if rec := f.do(http.MethodGet, unknown, "", userAda); rec.Code != http.StatusNotFound {
		t.Errorf("unknown service = %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(http.MethodPost, "/api/v1/households/"+hhAda+"/meal-kit/hellofresh/imports", "", userAda); rec.Code != http.StatusConflict {
		t.Errorf("import without a link = %d %s", rec.Code, rec.Body)
	}
}

func TestMealKitRoutesSayTheFeatureIsOffWhenItIs(t *testing.T) {
	f := newHandlerFixture(t, false)
	rec := f.do(http.MethodGet, statusPath, "", userAda)
	var status StatusResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil || status.Enabled {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	rec = f.do(http.MethodPut, linkPath, `{"accessToken":"session-value"}`, userAda)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "import_disabled") {
		t.Errorf("link with the feature off = %d %s", rec.Code, rec.Body)
	}
}
