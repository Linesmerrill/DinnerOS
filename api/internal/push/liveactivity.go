package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// LiveActivityEvent is what a Live Activity push does to the activity.
type LiveActivityEvent string

// Live Activity events. Starting an activity by push is not used: the app
// starts it locally when the member queues the work, so only these two exist.
const (
	LiveActivityUpdate LiveActivityEvent = "update"
	LiveActivityEnd    LiveActivityEvent = "end"
)

// LiveActivityTopicSuffix is appended to the app's bundle ID to form the
// apns-topic of a Live Activity push.
const LiveActivityTopicSuffix = ".push-type.liveactivity"

// LiveActivityMessage is one push to one Live Activity.
type LiveActivityMessage struct {
	// Token is the activity's own push token (Activity.pushTokenUpdates), not
	// the device token. Never log it.
	Token       string
	Environment Environment
	Event       LiveActivityEvent
	// ContentState must encode to exactly what the app's
	// ActivityAttributes.ContentState decodes, or the device drops the update.
	ContentState any
	// Timestamp orders updates: the device ignores one older than what it
	// already shows.
	Timestamp time.Time
	// StaleDate, when set, is when the system marks the content outdated.
	StaleDate time.Time
	// DismissalDate is when an ended activity leaves the Lock Screen. Only an
	// end event carries it; zero on an end means the system default.
	DismissalDate time.Time
	// LowPriority sends at apns-priority 5, which Apple does not count against
	// the activity's high-priority update budget. Progress updates use it; the
	// final state does not.
	LowPriority bool
}

type liveActivityAPS struct {
	Timestamp     int64             `json:"timestamp"`
	Event         LiveActivityEvent `json:"event"`
	ContentState  any               `json:"content-state"`
	StaleDate     int64             `json:"stale-date,omitempty"`
	DismissalDate int64             `json:"dismissal-date,omitempty"`
}

// LiveActivityPayload builds the JSON body APNs receives for m.
func LiveActivityPayload(m LiveActivityMessage) ([]byte, error) {
	if m.Event != LiveActivityUpdate && m.Event != LiveActivityEnd {
		return nil, fmt.Errorf("apns: unknown live activity event %q", m.Event)
	}
	if m.ContentState == nil {
		return nil, errors.New("apns: a live activity push needs a content state")
	}
	aps := liveActivityAPS{Timestamp: m.Timestamp.Unix(), Event: m.Event, ContentState: m.ContentState}
	if !m.StaleDate.IsZero() {
		aps.StaleDate = m.StaleDate.Unix()
	}
	if m.Event == LiveActivityEnd && !m.DismissalDate.IsZero() {
		aps.DismissalDate = m.DismissalDate.Unix()
	}
	return json.Marshal(map[string]any{"aps": aps})
}

// LiveActivityHeaders are the headers of a Live Activity push to the app with
// bundle ID topic.
func LiveActivityHeaders(topic string, m LiveActivityMessage) map[string]string {
	priority := 10
	if m.LowPriority {
		priority = 5
	}
	// An update nobody saw before it went stale is not worth delivering late;
	// an end is, until the activity would have been dismissed anyway.
	expires := m.StaleDate
	if m.Event == LiveActivityEnd {
		expires = m.DismissalDate
	}
	return map[string]string{
		"apns-topic":      topic + LiveActivityTopicSuffix,
		"apns-push-type":  "liveactivity",
		"apns-priority":   strconv.Itoa(priority),
		"apns-expiration": expirationHeader(expires),
	}
}

// LiveActivitySender delivers one Live Activity push. APNsClient is the real
// one.
type LiveActivitySender interface {
	SendLiveActivity(ctx context.Context, m LiveActivityMessage) error
}

var _ LiveActivitySender = (*APNsClient)(nil)

// SendLiveActivity implements LiveActivitySender through the same token-auth
// connection alert pushes use.
func (c *APNsClient) SendLiveActivity(ctx context.Context, m LiveActivityMessage) error {
	base := c.opts.ProductionURL
	switch m.Environment {
	case EnvironmentProduction:
	case EnvironmentSandbox:
		base = c.opts.SandboxURL
	default:
		return fmt.Errorf("apns: unknown environment %q", m.Environment)
	}
	payload, err := LiveActivityPayload(m)
	if err != nil {
		return err
	}
	return c.deliver(ctx, base, m.Token, payload, LiveActivityHeaders(c.opts.Topic, m))
}
