package device

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/process"
)

func newTestRecorder() (ScreenRecorder, *process.Fake, *process.FakeBackground) {
	runner := process.NewFake()
	background := process.NewFakeBackground()
	return NewScreenRecorder(RecorderDeps{Runner: runner, Background: background}), runner, background
}

func TestScreenRecorder_PhysicalIOSDeviceIsUnsupported(t *testing.T) {
	recorder, runner, background := newTestRecorder()
	desc := Descriptor{ID: "00008110-0001", Platform: PlatformIOS, Kind: KindDevice}

	rec, err := recorder.Start(context.Background(), desc, t.TempDir(), "video-1")

	if rec != nil {
		t.Fatalf("recording = %v, want nil", rec)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeRecordingUnsupported {
		t.Fatalf("code = %q, want %q (err %v)", code, diag.CodeRecordingUnsupported, err)
	}
	if n := len(runner.Invocations()) + len(background.Invocations()); n != 0 {
		t.Fatalf("an unsupported device ran %d commands, want none", n)
	}
}

func TestScreenRecorder_UnknownPlatformIsUnsupported(t *testing.T) {
	recorder, _, _ := newTestRecorder()

	_, err := recorder.Start(context.Background(), Descriptor{ID: "x", Platform: "web", Kind: KindDevice}, t.TempDir(), "video-1")

	if code, _, _ := diag.Describe(err); code != diag.CodeRecordingUnsupported {
		t.Fatalf("code = %q, want %q", code, diag.CodeRecordingUnsupported)
	}
}
