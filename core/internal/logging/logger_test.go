package logging_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"dev.atomyx/internal/logging"
)

func TestFromEnv_ReturnsNonNoopLogger(t *testing.T) {
	logger := logging.FromEnv()
	if _, isNoop := any(logger).(logging.NoopLogger); isNoop {
		t.Fatalf("got a NoopLogger from FromEnv, want a concrete logger that actually writes entries")
	}
	if logger == nil {
		t.Fatal("got a nil logger from FromEnv")
	}
}

func TestFromEnv_DefaultsToInfoWhenUnset(t *testing.T) {
	original, wasSet := os.LookupEnv("ATOMYX_LOG_LEVEL")
	if err := os.Unsetenv("ATOMYX_LOG_LEVEL"); err != nil {
		t.Fatalf("Unsetenv: %v", err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv("ATOMYX_LOG_LEVEL", original)
		}
	})

	logger := logging.FromEnv()
	if logger.Level() != logging.LevelInfo {
		t.Fatalf("got level %v, want %v", logger.Level(), logging.LevelInfo)
	}
}

func TestFromEnv_FallsBackToInfoOnUnrecognizedValue(t *testing.T) {
	t.Setenv("ATOMYX_LOG_LEVEL", "not-a-real-level")
	logger := logging.FromEnv()
	if logger.Level() != logging.LevelInfo {
		t.Fatalf("got level %v, want %v", logger.Level(), logging.LevelInfo)
	}
}

func TestFromEnv_ReadsRecognizedLevel(t *testing.T) {
	t.Setenv("ATOMYX_LOG_LEVEL", "debug")
	logger := logging.FromEnv()
	if logger.Level() != logging.LevelDebug {
		t.Fatalf("got level %v, want %v", logger.Level(), logging.LevelDebug)
	}
}

func TestStructuredLogger_WritesStructuredLineWithChildFields(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewStructuredLogger(&buf, logging.LevelDebug)
	child := logger.Child(logging.LogContext{"runId": "run-123"})
	child.Info("script.step", logging.LogContext{"command": "launchApp"})

	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("output is not a single JSON object: %v, got %q", err, buf.String())
	}

	if decoded["level"] != "info" {
		t.Fatalf("got level=%v, want %q", decoded["level"], "info")
	}
	if decoded["msg"] != "script.step" {
		t.Fatalf("got msg=%v, want %q", decoded["msg"], "script.step")
	}
	fields, ok := decoded["fields"].(map[string]any)
	if !ok {
		t.Fatalf("got fields=%v, want an object carrying both the child's and the call's context", decoded["fields"])
	}
	if fields["runId"] != "run-123" {
		t.Fatalf("got fields.runId=%v, want %q (carried from Child)", fields["runId"], "run-123")
	}
	if fields["command"] != "launchApp" {
		t.Fatalf("got fields.command=%v, want %q", fields["command"], "launchApp")
	}
}

func TestStructuredLogger_FiltersEntriesBelowConfiguredLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewStructuredLogger(&buf, logging.LevelWarn)
	logger.Info("should not appear", nil)
	logger.Debug("should not appear either", nil)

	if buf.Len() != 0 {
		t.Fatalf("got output %q at level Warn for a Debug and an Info call, want nothing written", buf.String())
	}

	logger.Warn("should appear", nil)
	if buf.Len() == 0 {
		t.Fatal("got no output for a Warn call at level Warn, want one line written")
	}
}

func TestStructuredLogger_ErrorCarriesUnderlyingErrorMessage(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewStructuredLogger(&buf, logging.LevelDebug)
	logger.Error("command handler panicked", errBoom, logging.LogContext{"command": "tap"})

	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("output is not a single JSON object: %v, got %q", err, buf.String())
	}
	if decoded["level"] != "error" {
		t.Fatalf("got level=%v, want %q", decoded["level"], "error")
	}
	if decoded["error"] != errBoom.Error() {
		t.Fatalf("got error=%v, want %q", decoded["error"], errBoom.Error())
	}
}

func TestStructuredLogger_ChildDoesNotMutateParentContext(t *testing.T) {
	var buf bytes.Buffer
	parent := logging.NewStructuredLogger(&buf, logging.LevelDebug)
	child := parent.Child(logging.LogContext{"runId": "run-1"})
	_ = child

	buf.Reset()
	parent.Info("parent entry", nil)

	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("output is not a single JSON object: %v, got %q", err, buf.String())
	}
	if fields, ok := decoded["fields"].(map[string]any); ok {
		if _, present := fields["runId"]; present {
			t.Fatalf("parent entry carries runId=%v, want the parent unaffected by a child's Child call", fields["runId"])
		}
	}
}

var errBoom = boomError{}

type boomError struct{}

func (boomError) Error() string { return "boom" }
