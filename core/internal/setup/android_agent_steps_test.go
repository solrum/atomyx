package setup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

type fakeInstaller struct {
	installed  bool
	checkErr   error
	installErr error
	installs   int
}

func (f *fakeInstaller) AgentInstalled(context.Context, string) (bool, error) {
	return f.installed, f.checkErr
}

func (f *fakeInstaller) InstallAgent(context.Context, string) error {
	f.installs++
	return f.installErr
}

type fakeAccessibility struct {
	state    AccessibilityState
	readErr  error
	enableEr error
	enables  int
}

func (f *fakeAccessibility) AccessibilityServices(context.Context, string) (AccessibilityState, error) {
	return f.state, f.readErr
}

func (f *fakeAccessibility) EnableAgentAccessibility(context.Context, string) error {
	f.enables++
	return f.enableEr
}

func TestAndroidAgentInstalledCheck(t *testing.T) {
	ctx := context.Background()
	if got := NewAndroidAgentInstalledStep("s1", &fakeInstaller{installed: true}).Check(ctx); got.Status != StatusOK {
		t.Errorf("installed: %+v", got)
	}
	got := NewAndroidAgentInstalledStep("s1", &fakeInstaller{}).Check(ctx)
	if got.Status != StatusManual || !strings.Contains(got.Remedy, "without --check") {
		t.Errorf("missing: %+v", got)
	}
	got = NewAndroidAgentInstalledStep("s1", &fakeInstaller{checkErr: errors.New("adb died")}).Check(ctx)
	if got.Status != StatusFailed || !strings.Contains(got.Message, "adb died") || got.Remedy == "" {
		t.Errorf("read error: %+v", got)
	}
}

func TestAndroidAgentInstalledFix(t *testing.T) {
	ctx := context.Background()
	installer := &fakeInstaller{}
	if got := NewAndroidAgentInstalledStep("s1", installer).Fix(ctx); got.Status != StatusFixed || installer.installs != 1 {
		t.Errorf("fix: %+v installs=%d", got, installer.installs)
	}
	coded := diag.New("INSTALL", "install refused", "Free some space.", nil)
	got := NewAndroidAgentInstalledStep("s1", &fakeInstaller{installErr: coded}).Fix(ctx)
	if got.Status != StatusFailed || got.Remedy != "Free some space." || !strings.Contains(got.Message, "install refused") {
		t.Errorf("install error keeps its own remedy: %+v", got)
	}
	got = NewAndroidAgentInstalledStep("s1", &fakeInstaller{installErr: errors.New("boom")}).Fix(ctx)
	if got.Status != StatusFailed || got.Remedy == "" {
		t.Errorf("plain error gets the fallback remedy: %+v", got)
	}
}

func TestAndroidAccessibilityCheck(t *testing.T) {
	ctx := context.Background()
	on := &fakeAccessibility{state: AccessibilityState{AgentEnabled: true}}
	if got := NewAndroidAccessibilityStep("s1", on).Check(ctx); got.Status != StatusOK {
		t.Errorf("enabled: %+v", got)
	}
	off := &fakeAccessibility{state: AccessibilityState{Others: []string{"com.a/.One", "com.b/.Two"}}}
	got := NewAndroidAccessibilityStep("s1", off).Check(ctx)
	if got.Status != StatusManual || !strings.Contains(got.Message, "disables the other enabled accessibility services: com.a/.One, com.b/.Two") {
		t.Errorf("not enabled lists the others: %+v", got)
	}
	if off.enables != 0 {
		t.Error("Check must never enable the service")
	}
	none := NewAndroidAccessibilityStep("s1", &fakeAccessibility{}).Check(ctx)
	if !strings.Contains(none.Message, "disables any other enabled accessibility service") {
		t.Errorf("no others still warns: %+v", none)
	}
	failed := NewAndroidAccessibilityStep("s1", &fakeAccessibility{readErr: errors.New("no shell")}).Check(ctx)
	if failed.Status != StatusFailed {
		t.Errorf("read error: %+v", failed)
	}
}

func TestAndroidAccessibilityFix(t *testing.T) {
	ctx := context.Background()
	control := &fakeAccessibility{state: AccessibilityState{Others: []string{"com.a/.One"}}}
	got := NewAndroidAccessibilityStep("s1", control).Fix(ctx)
	if got.Status != StatusFixed || control.enables != 1 || !strings.Contains(got.Message, "now disabled: com.a/.One") {
		t.Errorf("fix: %+v enables=%d", got, control.enables)
	}
	quiet := NewAndroidAccessibilityStep("s1", &fakeAccessibility{}).Fix(ctx)
	if strings.Contains(quiet.Message, "disabled") {
		t.Errorf("no previous services means no disabled list: %+v", quiet)
	}
	failed := NewAndroidAccessibilityStep("s1", &fakeAccessibility{enableEr: errors.New("denied")}).Fix(ctx)
	if failed.Status != StatusFailed || failed.Remedy == "" {
		t.Errorf("enable error: %+v", failed)
	}
}
