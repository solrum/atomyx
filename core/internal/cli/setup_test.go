package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"syscall"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/setup"
	"dev.atomyx/internal/shutdown"
)

type recordingStep struct {
	id     string
	check  setup.Result
	fix    setup.Result
	calls  *[]string
	onCall func()
}

func (s *recordingStep) ID() string { return s.id }

func (s *recordingStep) Check(context.Context) setup.Result {
	*s.calls = append(*s.calls, "check:"+s.id)
	if s.onCall != nil {
		s.onCall()
	}
	return s.check
}

func (s *recordingStep) Fix(context.Context) setup.Result {
	*s.calls = append(*s.calls, "fix:"+s.id)
	return s.fix
}

func recordingSteps(steps ...*recordingStep) map[string][]setupStepFactory {
	var factories []setupStepFactory
	for i, step := range steps {
		step := step
		entry := setup.Entry{Step: step}
		if i > 0 {
			entry.Requires = []string{steps[i-1].id}
		}
		factories = append(factories, func(setupTarget) setup.Entry { return entry })
	}
	return map[string][]setupStepFactory{"android": factories, "ios": factories}
}

func setupDeps(t *testing.T, stdout, stderr *bytes.Buffer) Deps {
	t.Helper()
	t.Setenv("ATOMYX_STORAGE_DIR", t.TempDir())
	deps := leaseDeps(t, newLeaseManager(t), stdout, stderr)
	deps.Probe = device.NewStaticProbe(heldAndroid[:1])
	return deps
}

var okFlags = map[string]FlagValue{"--platform": "android", "--device": "emulator-5554"}

func TestSetupWithNoRegisteredStepsIsOkAndEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := setupDeps(t, &stdout, &stderr)

	code := runSetup(context.Background(), deps, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--json": true}, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if got, want := stdout.String(), `{"ok":true,"steps":[]}`+"\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestSetupRejectsAnUnknownPlatformWithExitTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := setupDeps(t, &stdout, &stderr)

	code := Setup(context.Background(), deps, map[string]FlagValue{"--platform": "symbian"})

	if code != 2 || !strings.Contains(stderr.String(), "Unknown platform") {
		t.Errorf("code = %d, stderr = %q, want 2 and an unknown platform error", code, stderr.String())
	}
}

func TestSetupExitsOneWhenAStepIsManualAndOneWhenFailed(t *testing.T) {
	for _, status := range []setup.Status{setup.StatusManual, setup.StatusFailed} {
		var calls []string
		var stdout, stderr bytes.Buffer
		deps := setupDeps(t, &stdout, &stderr)
		steps := recordingSteps(&recordingStep{id: "a", check: setup.Result{Status: status, Message: "bad", Remedy: "fix it"}, fix: setup.Result{Status: status, Message: "bad", Remedy: "fix it"}, calls: &calls})

		code := runSetup(context.Background(), deps, okFlags, steps)

		if code != 1 {
			t.Errorf("%s: exit code = %d, want 1", status, code)
		}
		if !strings.Contains(stdout.String(), "remedy: fix it") {
			t.Errorf("%s: stdout = %q, want the remedy", status, stdout.String())
		}
	}
}

func TestSetupJSONReportsEachStepAndSkipsDependents(t *testing.T) {
	var calls []string
	var stdout, stderr bytes.Buffer
	deps := setupDeps(t, &stdout, &stderr)
	steps := recordingSteps(
		&recordingStep{id: "first", check: setup.Result{Status: setup.StatusManual, Message: "missing", Remedy: "install"}, calls: &calls},
		&recordingStep{id: "second", check: setup.Result{Status: setup.StatusOK}, calls: &calls},
	)

	code := runSetup(context.Background(), deps, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--json": true, "--check": true}, steps)

	var got struct {
		OK    bool `json:"ok"`
		Steps []struct{ ID, Status, Message, Remedy string }
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if code != 1 || got.OK || len(got.Steps) != 2 {
		t.Fatalf("code = %d, report = %+v", code, got)
	}
	if got.Steps[0].ID != "first" || got.Steps[0].Status != "manual" || got.Steps[0].Remedy != "install" {
		t.Errorf("first = %+v", got.Steps[0])
	}
	if got.Steps[1].Status != "skipped" || strings.Join(calls, ",") != "check:first" {
		t.Errorf("second = %+v, calls = %v", got.Steps[1], calls)
	}
}

func TestSetupCheckNeverFixesAndFixModeDoes(t *testing.T) {
	for _, check := range []bool{true, false} {
		var calls []string
		var stdout, stderr bytes.Buffer
		deps := setupDeps(t, &stdout, &stderr)
		steps := recordingSteps(&recordingStep{id: "a", check: setup.Result{Status: setup.StatusManual, Remedy: "r"}, fix: setup.Result{Status: setup.StatusFixed, Message: "done"}, calls: &calls})
		flags := map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--check": check}

		code := runSetup(context.Background(), deps, flags, steps)

		wantCalls, wantCode := "check:a,fix:a", 0
		if check {
			wantCalls, wantCode = "check:a", 1
		}
		if strings.Join(calls, ",") != wantCalls || code != wantCode {
			t.Errorf("--check=%v: calls = %v, code = %d, want %s and %d", check, calls, code, wantCalls, wantCode)
		}
	}
}

func TestSetupOnAHeldDeviceReportsTheHolderChangesNothingAndExitsOne(t *testing.T) {
	var calls []string
	manager := newLeaseManager(t)
	holdDevice(t, manager)
	var stdout, stderr bytes.Buffer
	deps := leaseDeps(t, manager, &stdout, &stderr)
	deps.Probe = device.NewStaticProbe(heldAndroid[:1])
	steps := recordingSteps(&recordingStep{id: "a", check: setup.Result{Status: setup.StatusOK}, calls: &calls})

	code := runSetup(context.Background(), deps, okFlags, steps)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr.String())
	}
	if len(calls) != 0 {
		t.Errorf("steps ran on a held device: %v", calls)
	}
	for _, want := range []string{"held:     true", "holder:   pid ", "atomyx run", "nothing was changed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestSetupOnAHeldDeviceJSONNamesTheHolder(t *testing.T) {
	manager := newLeaseManager(t)
	holdDevice(t, manager)
	var stdout, stderr bytes.Buffer
	deps := leaseDeps(t, manager, &stdout, &stderr)
	deps.Probe = device.NewStaticProbe(heldAndroid[:1])

	code := runSetup(context.Background(), deps, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--json": true}, recordingSteps())

	var got struct {
		OK     bool                      `json:"ok"`
		Steps  []any                     `json:"steps"`
		Held   bool                      `json:"held"`
		Holder *struct{ Command string } `json:"holder"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if code != 1 || got.OK || !got.Held || got.Holder == nil || got.Holder.Command != "atomyx run" || got.Steps == nil {
		t.Errorf("code = %d, report = %+v", code, got)
	}
}

func TestSetupHoldsTheLeaseWhileStepsRunAndReleasesItAfterwards(t *testing.T) {
	manager := newLeaseManager(t)
	var stdout, stderr bytes.Buffer
	deps := leaseDeps(t, manager, &stdout, &stderr)
	deps.Probe = device.NewStaticProbe(heldAndroid[:1])
	var heldDuring bool
	var calls []string
	steps := recordingSteps(&recordingStep{id: "a", check: setup.Result{Status: setup.StatusOK}, calls: &calls, onCall: func() {
		heldDuring = leaseHeld(t, manager)
	}})

	code := runSetup(context.Background(), deps, okFlags, steps)

	if code != 0 {
		t.Fatalf("exit code = %d; stderr: %s", code, stderr.String())
	}
	if !heldDuring {
		t.Error("the device lease was not held while the step ran")
	}
	if leaseHeld(t, manager) {
		t.Error("the device lease was still held after setup returned")
	}
}

func TestSetupReleasesTheLeaseAndExitsWithTheSignalCodeOnCtrlC(t *testing.T) {
	manager := newLeaseManager(t)
	var stdout, stderr bytes.Buffer
	deps := leaseDeps(t, manager, &stdout, &stderr)
	deps.Probe = device.NewStaticProbe(heldAndroid[:1])
	ctx, cancel := context.WithCancelCause(context.Background())
	var calls []string
	steps := recordingSteps(&recordingStep{id: "a", check: setup.Result{Status: setup.StatusOK}, calls: &calls, onCall: func() {
		cancel(shutdown.InterruptCause(syscall.SIGINT))
	}})

	code := runSetup(ctx, deps, okFlags, steps)

	if code != 130 {
		t.Errorf("exit code = %d, want 130", code)
	}
	if leaseHeld(t, manager) {
		t.Error("the device lease was still held after an interrupt")
	}
}

func TestSetupWithoutAResolvableDeviceTakesNoLeaseAndStillRunsTheSteps(t *testing.T) {
	var calls []string
	var stdout, stderr bytes.Buffer
	deps := setupDeps(t, &stdout, &stderr)
	deps.Probe = device.NewStaticProbe(nil)
	steps := recordingSteps(&recordingStep{id: "a", check: setup.Result{Status: setup.StatusOK}, calls: &calls})

	code := runSetup(context.Background(), deps, map[string]FlagValue{"--platform": "ios"}, steps)

	if code != 0 || len(calls) != 1 {
		t.Errorf("code = %d, calls = %v, want 0 and one check", code, calls)
	}
	if !strings.Contains(stderr.String(), "no device was resolved") {
		t.Errorf("stderr = %q, want a note that no device was resolved", stderr.String())
	}
}

func TestSetupRegistrationErrorIsReported(t *testing.T) {
	var calls []string
	var stdout, stderr bytes.Buffer
	deps := setupDeps(t, &stdout, &stderr)
	dup := &recordingStep{id: "a", check: setup.Result{Status: setup.StatusOK}, calls: &calls}
	steps := map[string][]setupStepFactory{"android": {
		func(setupTarget) setup.Entry { return setup.Entry{Step: dup} },
		func(setupTarget) setup.Entry { return setup.Entry{Step: dup} },
	}}

	code := runSetup(context.Background(), deps, okFlags, steps)

	if code != 1 || !strings.Contains(stderr.String(), "registered twice") {
		t.Errorf("code = %d, stderr = %q", code, stderr.String())
	}
}

func TestSetupStepsByPlatformCoversBothPlatforms(t *testing.T) {
	for _, platform := range []string{"android", "ios"} {
		if _, ok := setupStepsByPlatform[platform]; !ok {
			t.Errorf("setupStepsByPlatform has no entry for %s", platform)
		}
	}
}

// leaseHeld reports whether a process holds the lease of the test
// device, probing the way atomyx devices does.
func leaseHeld(t *testing.T, manager leaseProber) bool {
	t.Helper()

	status, err := manager.Probe(context.Background(), lease.KeyFor("android", "emulator-5554"))
	if err != nil {
		t.Fatal(err)
	}
	return status.Held
}
