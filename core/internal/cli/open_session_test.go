package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/session"
)

// leaseDeps returns Deps whose sessions arbitrate through a lease
// directory private to the test, so two Deps built from the same
// manager stand in for two Atomyx processes.
func leaseDeps(t *testing.T, manager *lease.Manager, stdout, stderr *bytes.Buffer) Deps {
	t.Helper()
	return Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: singleMockDriverFactory{driver: mockdriver.New()},
		Stdout:  stdout,
		Stderr:  stderr,
		Session: session.Deps{Leases: manager, Clock: clock.SystemClock{}},
	}
}

func newLeaseManager(t *testing.T) *lease.Manager {
	t.Helper()
	root := t.TempDir()
	manager, err := lease.NewManager(lease.Environment{
		GOOS:          "linux",
		Getenv:        func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": root}[k] },
		UserCacheDir:  func() (string, error) { return root, nil },
		Getuid:        os.Getuid,
		IsLocalFS:     func(string) (bool, error) { return true, nil },
		FallbackRoot:  root,
		MaxSocketPath: 4096,
	}, clock.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

var heldDevice = map[string]FlagValue{"--platform": "android", "--device": "emulator-5554"}

// holdDevice takes the device the way another Atomyx process would.
func holdDevice(t *testing.T, manager *lease.Manager) *deviceSession {
	t.Helper()
	var out, errOut bytes.Buffer
	deps := leaseDeps(t, manager, &out, &errOut)
	opened, err := openDevice(context.Background(), deps, "atomyx run", device.Descriptor{Platform: device.PlatformAndroid, ID: "emulator-5554"})
	if err != nil {
		t.Fatalf("the first process could not take the device: %v", err)
	}
	t.Cleanup(opened.Close)
	return opened
}

func TestDoctorOnAHeldDeviceReportsTheHolderAndExitsZero(t *testing.T) {
	manager := newLeaseManager(t)
	holdDevice(t, manager)
	var stdout, stderr bytes.Buffer

	code := Doctor(context.Background(), leaseDeps(t, manager, &stdout, &stderr), heldDevice)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a held device; stderr: %s", code, stderr.String())
	}
	for _, want := range []string{"held:     true", "atomyx run", "health was not checked"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout %q lacks %q", stdout.String(), want)
		}
	}
}

func TestDoctorJSONOnAHeldDeviceCarriesHeldHealthCheckedAndHolder(t *testing.T) {
	manager := newLeaseManager(t)
	holdDevice(t, manager)
	var stdout, stderr bytes.Buffer
	flags := map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--json": true}

	code := Doctor(context.Background(), leaseDeps(t, manager, &stdout, &stderr), flags)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var decoded struct {
		Held          bool `json:"held"`
		HealthChecked bool `json:"healthChecked"`
		Holder        struct {
			PID     int    `json:"pid"`
			Command string `json:"command"`
			State   string `json:"state"`
		} `json:"holder"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding %q: %v", stdout.String(), err)
	}
	if !decoded.Held || decoded.HealthChecked || decoded.Holder.PID != os.Getpid() || decoded.Holder.Command != "atomyx run" || decoded.Holder.State == "" {
		t.Fatalf("decoded = %+v, want held, not health checked, and this process as holder", decoded)
	}
}

func TestDoctorJSONOnAFreeDeviceReportsHealthCheckedAndNotHeld(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := leaseDeps(t, newLeaseManager(t), &stdout, &stderr)
	flags := map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--json": true}

	if code := Doctor(context.Background(), deps, flags); code != 0 {
		t.Fatalf("exit code = %d; stderr %s", code, stderr.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["held"] != false || decoded["healthChecked"] != true || decoded["healthy"] != true {
		t.Fatalf("decoded = %v, want held false, healthChecked true, healthy true", decoded)
	}
}

func TestRunOnAHeldDeviceFailsWithDeviceHeld(t *testing.T) {
	manager := newLeaseManager(t)
	holdDevice(t, manager)
	script := t.TempDir() + "/flow.yaml"
	if err := os.WriteFile(script, []byte("steps:\n  - sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	flags := map[string]FlagValue{"--file": script, "--platform": "android", "--device": "emulator-5554"}

	code := RunScript(context.Background(), leaseDeps(t, manager, &stdout, &stderr), flags)

	if code != 1 || !strings.Contains(stderr.String(), diag.CodeDeviceHeld) {
		t.Fatalf("exit code = %d stderr = %q, want 1 and DEVICE_HELD", code, stderr.String())
	}
}

func TestOpenDeviceReleasesTheLeaseOnClose(t *testing.T) {
	manager := newLeaseManager(t)
	first := holdDevice(t, manager)
	first.Close()

	var out, errOut bytes.Buffer
	_, err := openDevice(context.Background(), leaseDeps(t, manager, &out, &errOut), "atomyx run", device.Descriptor{Platform: device.PlatformAndroid, ID: "emulator-5554"})
	if err != nil {
		t.Fatalf("a second process could not take the released device: %v", err)
	}
}
