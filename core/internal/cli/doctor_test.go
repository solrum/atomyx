package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session/sessiontest"
)

// singleMockDriverFactory is a device.Factory that always connects
// and returns the same pre-staged mock.Driver, so a test can control
// what CheckHealth observes before Doctor runs.
type singleMockDriverFactory struct {
	driver *mockdriver.Driver
}

func (f singleMockDriverFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f singleMockDriverFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.driver.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.driver,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.driver, Clock: orchestra.SystemClock{}}),
	}, nil
}

func TestDoctorHealthyDeviceExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Factory: singleMockDriverFactory{driver: mockdriver.New()},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := Doctor(context.Background(), deps, map[string]FlagValue{})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "healthy:  true") {
		t.Errorf("stdout: got %q, want a healthy:true line", stdout.String())
	}
}

func TestDoctorUnreachableDeviceExitsOne(t *testing.T) {
	drv := mockdriver.New()
	drv.PingErr = diag.New(
		diag.CodeAgentUnreachable,
		"could not reach the agent on device emulator-5554",
		"Confirm the agent is running on the device, then try again.",
		nil,
	)
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Factory: singleMockDriverFactory{driver: drv},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := Doctor(context.Background(), deps, map[string]FlagValue{})
	if code != 1 {
		t.Fatalf("exit code: got %d, want 1; stdout: %s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "healthy:  false") {
		t.Errorf("stdout: got %q, want a healthy:false line", stdout.String())
	}
	if !strings.Contains(stdout.String(), diag.CodeAgentUnreachable) {
		t.Errorf("stdout: got %q, want the %s code", stdout.String(), diag.CodeAgentUnreachable)
	}
}

func TestDoctorJSONOutputShape(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Factory: singleMockDriverFactory{driver: mockdriver.New()},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := Doctor(context.Background(), deps, map[string]FlagValue{"--json": true})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	var report orchestra.HealthReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decoding doctor --json output: %v; stdout: %s", err, stdout.String())
	}
	if !report.Selected || !report.Healthy {
		t.Fatalf("decoded report: got %+v, want selected and healthy true", report)
	}
}

func TestDoctorInvalidPlatformExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := Doctor(context.Background(), deps, map[string]FlagValue{"--platform": "windows"})
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Unknown platform") {
		t.Errorf("stderr: got %q", stderr.String())
	}
}

func TestRunDoctorShortcutDispatchesToDriverDoctor(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := testDeps(&stdout, &stderr)
	code := Run([]string{"doctor"}, deps)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Atomyx: device health") {
		t.Errorf("stdout: got %q", stdout.String())
	}
}

func TestDoctorPrintsAgentVersionsWhenReported(t *testing.T) {
	drv := mockdriver.New()
	drv.Agent = driver.AgentInfo{ProtocolVersion: 2, AgentVersion: "0.1.0"}
	var stdout, stderr bytes.Buffer
	deps := Deps{Factory: singleMockDriverFactory{driver: drv}, Stdout: &stdout, Stderr: &stderr}

	if code := Doctor(context.Background(), deps, map[string]FlagValue{}); code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	for _, want := range []string{"agent:    0.1.0", "protocol: 2"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout: got %q, want a %q line", stdout.String(), want)
		}
	}
}

func TestDoctorOmitsAgentVersionsWhenUnreported(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := Deps{Factory: singleMockDriverFactory{driver: mockdriver.New()}, Stdout: &stdout, Stderr: &stderr}

	if code := Doctor(context.Background(), deps, map[string]FlagValue{}); code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "agent:") || strings.Contains(stdout.String(), "protocol:") {
		t.Errorf("stdout: got %q, want no agent or protocol line", stdout.String())
	}
}

func TestDoctorJSONCarriesAgentVersions(t *testing.T) {
	drv := mockdriver.New()
	drv.Agent = driver.AgentInfo{ProtocolVersion: 2, AgentVersion: "0.1.0"}
	var stdout, stderr bytes.Buffer
	deps := Deps{Factory: singleMockDriverFactory{driver: drv}, Stdout: &stdout, Stderr: &stderr}

	if code := Doctor(context.Background(), deps, map[string]FlagValue{"--json": true}); code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding doctor --json output: %v", err)
	}
	if decoded["agentVersion"] != "0.1.0" || decoded["protocolVersion"] != float64(2) {
		t.Fatalf("doctor --json = %v, want agentVersion 0.1.0 and protocolVersion 2", decoded)
	}
}
