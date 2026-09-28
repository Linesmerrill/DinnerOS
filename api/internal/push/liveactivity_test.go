package push

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

type testState struct {
	Phase string `json:"phase"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

func TestSendLiveActivityUpdateHeadersAndPayload(t *testing.T) {
	client, fake, _ := newFakeAPNs(t, testKey(t), nil)
	at := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	err := client.SendLiveActivity(context.Background(), LiveActivityMessage{
		Token: testDeviceToken, Environment: EnvironmentSandbox, Event: LiveActivityUpdate,
		ContentState: testState{Phase: "importing", Done: 40, Total: 740},
		Timestamp:    at, StaleDate: at.Add(30 * time.Minute), LowPriority: true,
	})
	if err != nil {
		t.Fatalf("SendLiveActivity() error = %v", err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(fake.requests))
	}
	req := fake.requests[0]
	if req.gateway != "sandbox" || req.path != "/3/device/"+testDeviceToken {
		t.Errorf("sent to %s %s", req.gateway, req.path)
	}
	for header, want := range map[string]string{
		"apns-push-type":  "liveactivity",
		"apns-topic":      "com.linesmerrill.dinneros.push-type.liveactivity",
		"apns-priority":   "5",
		"apns-expiration": strconv.FormatInt(at.Add(30*time.Minute).Unix(), 10),
	} {
		if got := req.header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	aps, _ := req.body["aps"].(map[string]any)
	if aps["event"] != "update" || aps["timestamp"] != float64(at.Unix()) || aps["stale-date"] != float64(at.Add(30*time.Minute).Unix()) {
		t.Errorf("aps = %v", aps)
	}
	if _, ok := aps["dismissal-date"]; ok {
		t.Errorf("an update carries a dismissal-date: %v", aps)
	}
	if _, ok := aps["alert"]; ok {
		t.Errorf("a live activity update carries an alert: %v", aps)
	}
	state, _ := aps["content-state"].(map[string]any)
	if state["phase"] != "importing" || state["done"] != float64(40) || state["total"] != float64(740) {
		t.Errorf("content-state = %v", state)
	}
}

func TestLiveActivityEndCarriesDismissalDateAtHighPriority(t *testing.T) {
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	m := LiveActivityMessage{
		Token: testDeviceToken, Environment: EnvironmentProduction, Event: LiveActivityEnd,
		ContentState: testState{Phase: "done", Done: 740, Total: 740},
		Timestamp:    at, DismissalDate: at.Add(4 * time.Hour),
	}
	body, err := LiveActivityPayload(m)
	if err != nil {
		t.Fatalf("LiveActivityPayload() error = %v", err)
	}
	var decoded struct {
		APS map[string]json.RawMessage `json:"aps"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("payload is not valid JSON: %v\n%s", err, body)
	}
	dismiss := strconv.FormatInt(at.Add(4*time.Hour).Unix(), 10)
	if string(decoded.APS["event"]) != `"end"` || string(decoded.APS["dismissal-date"]) != dismiss {
		t.Errorf("aps = %s", body)
	}
	h := LiveActivityHeaders("com.linesmerrill.dinneros", m)
	if h["apns-priority"] != "10" || h["apns-expiration"] != dismiss || h["apns-push-type"] != "liveactivity" {
		t.Errorf("headers = %v", h)
	}
}

func TestLiveActivityPayloadRejectsUnknownEventAndMissingState(t *testing.T) {
	if _, err := LiveActivityPayload(LiveActivityMessage{Event: "start", ContentState: testState{}}); err == nil {
		t.Error("start event: want an error")
	}
	if _, err := LiveActivityPayload(LiveActivityMessage{Event: LiveActivityUpdate}); err == nil {
		t.Error("no content state: want an error")
	}
}

func TestSendLiveActivityReportsAGoneToken(t *testing.T) {
	client, _, _ := newFakeAPNs(t, testKey(t), func(*http.Request) (int, string) { return http.StatusGone, "Unregistered" })
	err := client.SendLiveActivity(context.Background(), LiveActivityMessage{
		Token: testDeviceToken, Environment: EnvironmentProduction, Event: LiveActivityUpdate,
		ContentState: testState{}, Timestamp: time.Now(),
	})
	if !errors.Is(err, ErrDeviceTokenInvalid) {
		t.Fatalf("error = %v, want ErrDeviceTokenInvalid", err)
	}
}
