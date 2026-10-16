package orchestra

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
)

func TestSetOrientation_PassesTheOrientationToTheDriver(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)

	if err := o.SetOrientation(context.Background(), driver.OrientationLandscapeRight); err != nil {
		t.Fatalf("SetOrientation: %v", err)
	}
	if len(d.Orientations) != 1 || d.Orientations[0] != driver.OrientationLandscapeRight {
		t.Fatalf("driver orientations = %v, want landscapeRight", d.Orientations)
	}
}

func TestSetOrientation_RejectsAnUnknownNameBeforeTheDriver(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)

	err := o.SetOrientation(context.Background(), "sideways")
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if len(d.Orientations) != 0 {
		t.Fatalf("driver orientations = %v, want none", d.Orientations)
	}
}

func TestSetOrientation_ADeviceWithoutTheCapabilityIsUnsupported(t *testing.T) {
	d := mock.New()
	d.SetCapabilities(driver.Capabilities{CanSetOrientation: false})
	o, _ := newTestOrchestra(d)

	err := o.SetOrientation(context.Background(), driver.OrientationPortrait)
	if code, _, _ := diag.Describe(err); code != diag.CodeOrientationUnsupported {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeOrientationUnsupported)
	}
	if len(d.Orientations) != 0 {
		t.Fatalf("driver orientations = %v, want none", d.Orientations)
	}
}

func TestSetOrientation_WaitsForTheScreenToSettleThroughTheDriverIdleWait(t *testing.T) {
	d := mock.New()
	d.SetCapabilities(driver.Capabilities{CanSetOrientation: true, CanWaitForIdle: true})
	o, _ := newTestOrchestra(d)

	if err := o.SetOrientation(context.Background(), driver.OrientationLandscapeLeft); err != nil {
		t.Fatalf("SetOrientation: %v", err)
	}
	var methods []string
	for _, c := range d.Calls {
		methods = append(methods, c.Method)
	}
	if len(methods) != 2 || methods[0] != "SetOrientation" || methods[1] != "WaitForIdle" {
		t.Fatalf("calls = %v, want SetOrientation then WaitForIdle", methods)
	}
	if got := d.Calls[1].Args[0]; got != OrientationIdleTimeoutMs {
		t.Fatalf("idle timeout = %v, want %d", got, OrientationIdleTimeoutMs)
	}
}

func TestSetOrientation_SettlesHostSideAndStaysBoundedOnAMovingScreen(t *testing.T) {
	d := mock.New()
	for i := 0; i < 100; i++ {
		d.StageHierarchy(node("moving", "Frame", "0,0,100,"+string(rune('0'+i%10))+"0"))
	}
	o, clock := newTestOrchestra(d)

	if err := o.SetOrientation(context.Background(), driver.OrientationLandscapeLeft); err != nil {
		t.Fatalf("SetOrientation must not fail on a screen that never settles: %v", err)
	}
	if got := clock.NowMs(); got > OrientationIdleTimeoutMs+1000 {
		t.Fatalf("waited %dms, want at most about %dms", got, OrientationIdleTimeoutMs)
	}
	reads := 0
	for _, c := range d.Calls {
		if c.Method == "Hierarchy" {
			reads++
		}
	}
	if reads < 2 {
		t.Fatalf("hierarchy reads = %d, want the host-side settle loop to poll", reads)
	}
}

func TestSetOrientation_ReturnsTheContextErrorFromTheSettleWait(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "Frame", "0,0,100,100"))
	o, _ := newTestOrchestra(d)
	ctx, cancel := context.WithCancel(context.Background())
	d.OnSwipe = nil
	cancel()

	if err := o.SetOrientation(ctx, driver.OrientationPortrait); err == nil {
		t.Fatal("SetOrientation with a canceled context returned nil")
	}
}
