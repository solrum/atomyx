package runner

import (
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

func TestSetOrientationCommand_RotatesTheDevice(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- setOrientation: landscapeLeft\n- setOrientation: portrait\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	calls := driverCalls(d, "SetOrientation")
	if len(calls) != 2 || calls[0].Args[0] != driver.OrientationLandscapeLeft || calls[1].Args[0] != driver.OrientationPortrait {
		t.Fatalf("got SetOrientation calls %v, want landscapeLeft then portrait", calls)
	}
	if got := detail(lastStep(t, result)); got != "Rotated to portrait" {
		t.Errorf("detail = %q", got)
	}
}

func TestSetOrientationCommand_FailsTheStepWhenTheDeviceCannotRotate(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	d.SetCapabilities(driver.Capabilities{CanSetOrientation: false})
	result := runConfigured(t, r, "appId: com.test\n---\n- setOrientation: landscapeRight\n")
	if result.Ok {
		t.Fatal("got Ok=true, want the step to fail")
	}
	if got := lastStep(t, result); got.Code != diag.CodeOrientationUnsupported {
		t.Errorf("code = %q, want %q", got.Code, diag.CodeOrientationUnsupported)
	}
	if calls := driverCalls(d, "SetOrientation"); len(calls) != 0 {
		t.Errorf("got SetOrientation calls %v, want none", calls)
	}
}

func TestSummarizeStep_SetOrientation(t *testing.T) {
	def := mustParse(t, "appId: com.test\n---\n- setOrientation: landscapeLeft\n")
	if got, want := SummarizeStep(def.Steps[0]).Text, `setOrientation: "landscapeLeft"`; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}
