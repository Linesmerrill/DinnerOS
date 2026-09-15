package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/requestid"
)

func TestContextRequestIDIsAttached(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, slog.LevelInfo, "json").With("component", "test")

	logger.InfoContext(requestid.With(context.Background(), "req-12345678"), "hello")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	if entry["requestId"] != "req-12345678" {
		t.Errorf("requestId = %v, want req-12345678", entry["requestId"])
	}
	if entry["component"] != "test" {
		t.Errorf("component = %v, want attrs from With preserved", entry["component"])
	}
}

func TestNoRequestIDWithoutContextValue(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, slog.LevelInfo, "text").Info("hello")

	if strings.Contains(buf.String(), "requestId") {
		t.Errorf("unexpected requestId in %q", buf.String())
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, slog.LevelWarn, "text").Info("hidden")

	if buf.Len() != 0 {
		t.Errorf("info logged at warn level: %q", buf.String())
	}
}
