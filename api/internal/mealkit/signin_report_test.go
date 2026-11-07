package mealkit

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

const reportPath = statusPath + "/sign-in-reports"

// reportRouter mounts the handler with a logger that writes JSON lines to buf.
func reportRouter(t *testing.T, buf *bytes.Buffer) chi.Router {
	t.Helper()
	f := newHandlerFixture(t, false)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.service, Authorizer: testAuthorizer, Tokens: fakeTokens{},
		Logger: slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}).Mount)
	return r
}

func postReport(r chi.Router, path, body, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token-"+userID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// lastReport is the last "meal-kit sign-in report" line in buf.
func lastReport(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var found map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) == nil && entry["msg"] == "meal-kit sign-in report" {
			found = entry
		}
	}
	if found == nil {
		t.Fatalf("no report logged in %s", buf.String())
	}
	return found
}

func TestSignInReportLogsOneLine(t *testing.T) {
	var buf bytes.Buffer
	r := reportRouter(t, &buf)
	body := `{"outcome":"failed","reason":"unreadable","stage":"deliveries","httpStatus":500,"elapsedMs":41234,
		"attempt":2,"retried":true,"pageCrashes":1,"navigationError":"NSURLErrorDomain -1009",
		"appVersion":"0.1.0 (86)","osVersion":"26.2","device":"iPhone17,1"}`
	// Works while imports are off, which is when a sign-in problem matters most to see.
	if rec := postReport(r, reportPath, body, userAda); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	got := lastReport(t, &buf)
	want := map[string]any{
		"level": "WARN", "source": "hellofresh", "householdId": hhAda, "userId": userAda, "outcome": "failed",
		"reason": "unreadable", "stage": "deliveries", "httpStatus": float64(500), "elapsedMs": float64(41234),
		"attempt": float64(2), "retried": true, "pageCrashes": float64(1), "navigationError": "NSURLErrorDomain -1009",
		"appVersion": "0.1.0 (86)", "osVersion": "26.2", "device": "iPhone17,1",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestSignInReportDropsAnythingNotOnTheList(t *testing.T) {
	var buf bytes.Buffer
	r := reportRouter(t, &buf)
	body := `{"outcome":"harvested","recipes":42,"weeks":30,"pages":8,"stopped":"cap","reason":"<script>",
		"stage":"https://evil.test/","navigationError":"see https://example.test/?token=abc for details",
		"device":"a very long device name that goes on and on and on and on and on","elapsedMs":99999999999}`
	if rec := postReport(r, reportPath, body, userAda); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	got := lastReport(t, &buf)
	if got["level"] != "INFO" || got["recipes"] != float64(42) || got["stopped"] != "cap" || got["elapsedMs"] != float64(3_600_000) {
		t.Errorf("report = %v", got)
	}
	for _, key := range []string{"reason", "stage", "navigationError", "device"} {
		if _, ok := got[key]; ok {
			t.Errorf("%s was logged: %v", key, got[key])
		}
	}
	if strings.Contains(buf.String(), "evil.test") || strings.Contains(buf.String(), "token=abc") {
		t.Error("untrusted text reached the log")
	}
}

func TestSignInReportNeedsAKnownOutcomeAndTheImportPermission(t *testing.T) {
	var buf bytes.Buffer
	r := reportRouter(t, &buf)
	if rec := postReport(r, reportPath, `{"outcome":"anything"}`, userAda); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown outcome = %d", rec.Code)
	}
	if rec := postReport(r, "/api/v1/households/"+hhAda+"/meal-kit/blueapron/sign-in-reports", `{"outcome":"failed"}`, userAda); rec.Code != http.StatusNotFound {
		t.Errorf("unknown source = %d", rec.Code)
	}
	bobPath := "/api/v1/households/" + hhBob + "/meal-kit/hellofresh/sign-in-reports"
	if rec := postReport(r, bobPath, `{"outcome":"failed"}`, userBob); rec.Code != http.StatusForbidden {
		t.Errorf("no import permission = %d", rec.Code)
	}
	if strings.Contains(buf.String(), `"msg":"meal-kit sign-in report"`) {
		t.Error("a refused report was logged")
	}
}
