package androiddriver_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/process"
)

func TestAgentLauncher_EnableAccessibility_ReplacesTheServiceListWithoutInstalling(t *testing.T) {
	runner := process.NewFake()
	l := androiddriver.NewAgentLauncher(runner, newFakeClock(0), newFakeHealthProber(), androiddriver.AgentLauncherOptions{Serial: "S1"})

	if err := l.EnableAccessibility(context.Background()); err != nil {
		t.Fatalf("EnableAccessibility() error = %v", err)
	}

	wantArgs := [][]string{
		{"-s", "S1", "shell", "settings", "delete", "secure", "enabled_accessibility_services"},
		{"-s", "S1", "shell", "settings", "put", "secure", "enabled_accessibility_services", "dev.atomyx.agent/dev.atomyx.agent.service.AtomyxAccessibilityService"},
	}
	invocations := runner.Invocations()
	if len(invocations) != len(wantArgs) {
		t.Fatalf("adb invocations = %+v, want %d calls", invocations, len(wantArgs))
	}
	for i, want := range wantArgs {
		if !equalArgs(invocations[i].Args, want) {
			t.Fatalf("invocation[%d] args = %v, want %v", i, invocations[i].Args, want)
		}
	}
}

func TestAgentLauncher_EnableAccessibility_WriteFailureIsARebindError(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{}
	runner.DefaultErr = errors.New("settings refused")
	l := androiddriver.NewAgentLauncher(runner, newFakeClock(0), newFakeHealthProber(), androiddriver.AgentLauncherOptions{Serial: "S1"})

	var launcherErr *androiddriver.AgentLauncherError
	if err := l.EnableAccessibility(context.Background()); !errors.As(err, &launcherErr) || launcherErr.Code != androiddriver.ErrCodeRebindFailed {
		t.Fatalf("EnableAccessibility() error = %v, want a rebind-failed launcher error", err)
	}
}

func TestResolveAgentAPK(t *testing.T) {
	existing := newTestAPK(t)
	if path, found := androiddriver.ResolveAgentAPK(existing); path != existing || !found {
		t.Errorf("explicit existing file: %q, %v", path, found)
	}
	missing := filepath.Join(t.TempDir(), "missing.apk")
	if path, found := androiddriver.ResolveAgentAPK(missing); path != missing || found {
		t.Errorf("explicit missing file: %q, %v", path, found)
	}
	t.Setenv("ATOMYX_ANDROID_APK", existing)
	if path, found := androiddriver.ResolveAgentAPK(""); path != existing || !found {
		t.Errorf("environment fallback: %q, %v", path, found)
	}
}
