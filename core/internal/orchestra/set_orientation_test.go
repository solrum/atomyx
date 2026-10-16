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
