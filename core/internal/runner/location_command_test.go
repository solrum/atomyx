package runner

import (
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

func TestSetLocationCommand_MovesTheDevice(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- setLocation: {latitude: 37.7749, longitude: -122.4194}\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	calls := driverCalls(d, "SetLocation")
	if len(calls) != 1 || calls[0].Args[0] != (driver.Location{Latitude: 37.7749, Longitude: -122.4194}) {
		t.Fatalf("got SetLocation calls %v, want one at 37.7749, -122.4194", calls)
	}
	if got := detail(lastStep(t, result)); got != "Moved to 37.7749, -122.4194" {
		t.Errorf("detail = %q", got)
	}
}

func TestSetLocationCommand_FailsTheStepWhenTheDeviceCannotMove(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	d.SetCapabilities(driver.Capabilities{CanSetLocation: false})
	result := runConfigured(t, r, "appId: com.test\n---\n- setLocation: {latitude: 1, longitude: 2}\n")
	if result.Ok {
		t.Fatal("got Ok=true, want the step to fail")
	}
	if got := lastStep(t, result); got.Code != diag.CodeLocationUnsupported {
		t.Errorf("code = %q, want %q", got.Code, diag.CodeLocationUnsupported)
	}
	if calls := driverCalls(d, "SetLocation"); len(calls) != 0 {
		t.Errorf("got SetLocation calls %v, want none", calls)
	}
}

func TestTravelCommand_SetsEveryPointInOrder(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- travel:\n    speed: 100\n    points:\n      - {latitude: 1, longitude: 1}\n      - {latitude: 1, longitude: 1.001}\n      - {latitude: 1.001, longitude: 1.001}\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	calls := driverCalls(d, "SetLocation")
	if len(calls) != 3 || calls[0].Args[0] != (driver.Location{Latitude: 1, Longitude: 1}) || calls[2].Args[0] != (driver.Location{Latitude: 1.001, Longitude: 1.001}) {
		t.Fatalf("got SetLocation calls %v, want the three points in order", calls)
	}
	if got := detail(lastStep(t, result)); got != "Traveled through 3 points to 1.001, 1.001" {
		t.Errorf("detail = %q", got)
	}
}

func TestTravelCommand_FailsTheStepWhenTheDeviceCannotMove(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	d.SetCapabilities(driver.Capabilities{CanSetLocation: false})
	result := runConfigured(t, r, "appId: com.test\n---\n- travel:\n    points:\n      - {latitude: 1, longitude: 1}\n      - {latitude: 1, longitude: 1.0001}\n")
	if result.Ok {
		t.Fatal("got Ok=true, want the step to fail")
	}
	if got := lastStep(t, result); got.Code != diag.CodeLocationUnsupported {
		t.Errorf("code = %q, want %q", got.Code, diag.CodeLocationUnsupported)
	}
}

func TestSummarizeStep_Location(t *testing.T) {
	def := mustParse(t, "appId: com.test\n---\n- setLocation: {latitude: 1.5, longitude: -2}\n- travel:\n    points:\n      - {latitude: 1, longitude: 1}\n      - {latitude: 1, longitude: 1.001}\n    speed: 8\n")
	if got, want := SummarizeStep(def.Steps[0]).Text, `setLocation: 1.5, -2`; got != want {
		t.Errorf("setLocation summary = %q, want %q", got, want)
	}
	if got, want := SummarizeStep(def.Steps[1]).Text, `travel: 2 points at 8 m/s`; got != want {
		t.Errorf("travel summary = %q, want %q", got, want)
	}
}
