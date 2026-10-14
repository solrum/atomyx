package logging

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestMaskedLoggerMasksEveryTextFieldOfEveryLevelAndChild(t *testing.T) {
	var out bytes.Buffer
	mask := func(s string) string { return strings.ReplaceAll(s, "SECRET", "[MASK]") }
	logger := Masked(NewStructuredLogger(&out, LevelDebug), mask)
	child := logger.Child(LogContext{"inherited": "SECRET-a", "count": 3})

	logger.Debug("debug SECRET", LogContext{"k": "SECRET-b"})
	logger.Info("info SECRET", nil)
	logger.Warn("warn SECRET", LogContext{"k": "SECRET-c"})
	logger.Error("error SECRET", errors.New("cause SECRET"), LogContext{"k": "SECRET-d"})
	child.Info("child SECRET", LogContext{"k": "SECRET-e"})

	text := out.String()
	if strings.Contains(text, "SECRET") {
		t.Errorf("a log line carries the secret:\n%s", text)
	}
	if got := strings.Count(text, "[MASK]"); got != 11 {
		t.Errorf("masked %d fields, want 11:\n%s", got, text)
	}
	if !strings.Contains(text, `"count":3`) {
		t.Errorf("a non-text field changed:\n%s", text)
	}
}
