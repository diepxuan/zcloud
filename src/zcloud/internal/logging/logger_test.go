package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerUsesKeyValues(t *testing.T) {
	var output bytes.Buffer
	logger := NewWithHandler(slog.NewTextHandler(&output, nil))

	logger.Info("listener_started", "account_id", "acc-1")

	line := output.String()
	for _, part := range []string{"level=INFO", "msg=listener_started", "account_id=acc-1"} {
		if !strings.Contains(line, part) {
			t.Fatalf("log %q missing %q", line, part)
		}
	}
}
