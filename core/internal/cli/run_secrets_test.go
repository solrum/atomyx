package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
)

func TestSuiteHostSeedsItsSecretSetWithExternalValuesOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := Deps{Probe: device.NewStaticProbe(nil), Factory: screenshotMockFactory{}, Stdout: &stdout, Stderr: &stderr}
	cfg := suiteSettings{
		output:       runOutput{Root: t.TempDir()},
		label:        "suite",
		externalEnv:  map[string]string{"TOKEN": "external-value", "appId": "com.example.app"},
		committedEnv: map[string]bool{"appId": true},
	}
	desc := device.Descriptor{ID: "emulator-5556", Platform: device.PlatformAndroid}

	host, err := startSuiteHost(context.Background(), deps, cfg, "", "", desc, false)
	if err != nil {
		t.Fatal(err)
	}
	defer host.close(context.Background(), &stderr)

	if got := host.record.secrets.String("external-value com.example.app"); got != orchestra.SecureMask+" com.example.app" {
		t.Fatalf("got %q", got)
	}
}

func TestEachRunRecordOwnsASecretSet(t *testing.T) {
	root := t.TempDir()
	identity := runIdentity{Label: "one"}
	first, err := startRunRecord(runOutput{Root: root}, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.writer.Close() }()
	second, err := startRunRecord(runOutput{Root: root}, runIdentity{Label: "two"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.writer.Close() }()

	if first.secrets == nil || first.secrets == second.secrets {
		t.Fatal("run records must each own a distinct secret set")
	}
}

func TestRunRecordMaskedLoggerUsesTheRunsSecretSet(t *testing.T) {
	record, err := startRunRecord(runOutput{Root: t.TempDir()}, runIdentity{Label: "log"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = record.writer.Close() }()
	record.secrets.Add("external-value")
	var out bytes.Buffer

	record.maskedLogger(logging.NewStructuredLogger(&out, logging.LevelDebug)).Info("saw external-value", nil)

	if strings.Contains(out.String(), "external-value") {
		t.Errorf("the log line carries the secret: %s", out.String())
	}
}
