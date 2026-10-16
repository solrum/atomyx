package androiddriver_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

func screenTree(width, height int) transport.AndroidTreeNode {
	return transport.AndroidTreeNode{
		ElementID: "root",
		Children: []transport.AndroidTreeNode{{
			ElementID: "window",
			ClassName: "android.widget.FrameLayout",
			Bounds:    &transport.AndroidBounds{Left: 0, Top: 0, Right: width, Bottom: height},
		}},
	}
}

// rotatingTransport answers each Tree call with the next staged tree and
// repeats the last one once the list runs out, like a screen that keeps
// its layout after it has rotated.
type rotatingTransport struct {
	*fakeTransport

	mu    sync.Mutex
	trees []transport.AndroidTreeNode
	reads int
}

func (r *rotatingTransport) Tree(context.Context) (transport.AndroidTreeNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tree := r.trees[len(r.trees)-1]
	if r.reads < len(r.trees) {
		tree = r.trees[r.reads]
	}
	r.reads++
	return tree, nil
}

func newOrientationDriver(runner *process.Fake, clk clock.Clock, trees ...transport.AndroidTreeNode) (*androiddriver.AndroidDriver, *rotatingTransport) {
	tr := &rotatingTransport{fakeTransport: newFakeTransport(), trees: trees}
	return androiddriver.New(tr, runner, androiddriver.Options{Serial: "S1", Clock: clk}), tr
}

func settingInvocation(name, value string) process.Invocation {
	return process.Invocation{Name: "adb", Args: []string{"-s", "S1", "shell", "settings", "put", "system", name, value}}
}

func TestAndroidDriver_SetOrientation_FixesAutoRotateThenStoresTheRotation(t *testing.T) {
	tests := []struct {
		orientation driver.Orientation
		rotation    string
		screen      transport.AndroidTreeNode
	}{
		{driver.OrientationPortrait, "0", screenTree(1080, 2400)},
		{driver.OrientationLandscapeLeft, "1", screenTree(2400, 1080)},
		{driver.OrientationLandscapeRight, "3", screenTree(2400, 1080)},
	}
	for _, tc := range tests {
		t.Run(string(tc.orientation), func(t *testing.T) {
			runner := process.NewFake()
			d, _ := newOrientationDriver(runner, clock.NewFakeClock(0), tc.screen)

			if err := d.SetOrientation(context.Background(), tc.orientation); err != nil {
				t.Fatalf("SetOrientation() error = %v", err)
			}
			want := []process.Invocation{
				settingInvocation("accelerometer_rotation", "0"),
				settingInvocation("user_rotation", tc.rotation),
			}
			if got := runner.Invocations(); !reflect.DeepEqual(got, want) {
				t.Fatalf("invocations = %+v, want %+v", got, want)
			}
		})
	}
}

func TestAndroidDriver_SetOrientation_WaitsUntilTheScreenSizeFlips(t *testing.T) {
	clk := clock.NewFakeClock(0)
	d, tr := newOrientationDriver(process.NewFake(), clk,
		screenTree(1080, 2400), screenTree(1080, 2400), screenTree(2400, 1080))

	if err := d.SetOrientation(context.Background(), driver.OrientationLandscapeLeft); err != nil {
		t.Fatalf("SetOrientation() error = %v", err)
	}
	if tr.reads != 3 {
		t.Fatalf("hierarchy reads = %d, want 3", tr.reads)
	}
	if want := int64(2 * driver.OrientationPollIntervalMs); clk.NowMs() != want {
		t.Fatalf("waited %dms, want %dms", clk.NowMs(), want)
	}
}

func TestAndroidDriver_SetOrientation_AScreenThatNeverRotatesIsOrientationNotSet(t *testing.T) {
	clk := clock.NewFakeClock(0)
	d, _ := newOrientationDriver(process.NewFake(), clk, screenTree(1080, 2400))

	err := d.SetOrientation(context.Background(), driver.OrientationLandscapeRight)
	if code, _, _ := diag.Describe(err); code != diag.CodeOrientationNotSet {
		t.Fatalf("SetOrientation() code = %q (err %v), want %q", code, err, diag.CodeOrientationNotSet)
	}
	if clk.NowMs() < driver.OrientationSettleTimeoutMs {
		t.Fatalf("gave up after %dms, before the %dms bound", clk.NowMs(), driver.OrientationSettleTimeoutMs)
	}
}

func TestAndroidDriver_SetOrientation_RejectsAnUnknownNameBeforeTheShell(t *testing.T) {
	runner := process.NewFake()
	d, _ := newOrientationDriver(runner, clock.NewFakeClock(0), screenTree(1080, 2400))

	err := d.SetOrientation(context.Background(), "sideways")
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("SetOrientation() code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if got := runner.Invocations(); len(got) != 0 {
		t.Fatalf("invocations = %+v, want none", got)
	}
}

func TestAndroidDriver_SetOrientation_ARefusedSettingIsOrientationNotSet(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stderr: "Error: Permission denied\n", ExitCode: 1}
	runner.DefaultErr = errors.New("exit status 1")
	d, _ := newOrientationDriver(runner, clock.NewFakeClock(0), screenTree(1080, 2400))

	err := d.SetOrientation(context.Background(), driver.OrientationLandscapeLeft)
	if code, _, _ := diag.Describe(err); code != diag.CodeOrientationNotSet {
		t.Fatalf("SetOrientation() code = %q (err %v), want %q", code, err, diag.CodeOrientationNotSet)
	}
	if got := runner.Invocations(); len(got) != 1 {
		t.Fatalf("invocations = %+v, want the first setting only", got)
	}
}

func TestAndroidDriver_SetOrientation_UnreachableDeviceIsReportedAsUnreachable(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stderr: "error: device offline\n", ExitCode: 1}
	runner.DefaultErr = errors.New("exit status 1")
	d, _ := newOrientationDriver(runner, clock.NewFakeClock(0), screenTree(1080, 2400))

	err := d.SetOrientation(context.Background(), driver.OrientationLandscapeLeft)
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
		t.Fatalf("SetOrientation() code = %q (err %v), want %q", code, err, diag.CodeAgentUnreachable)
	}
}

func TestAndroidDriver_SetOrientation_ACancelledContextStopsBeforeTheShell(t *testing.T) {
	runner := process.NewFake()
	d, _ := newOrientationDriver(runner, clock.NewFakeClock(0), screenTree(1080, 2400))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := d.SetOrientation(ctx, driver.OrientationLandscapeLeft); !errors.Is(err, context.Canceled) {
		t.Fatalf("SetOrientation() error = %v, want context.Canceled", err)
	}
	if got := runner.Invocations(); len(got) != 0 {
		t.Fatalf("invocations = %+v, want none", got)
	}
}
