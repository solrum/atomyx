package androiddriver_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

const testPkg = "com.example.app"

// scriptedForeground serves a fixed sequence of foreground answers,
// repeating the last one once the sequence is exhausted.
type scriptedForeground struct {
	*fakeTransport
	answers []string
	errAt   int
	reads   int
	onRead  func(read int)
}

func (s *scriptedForeground) CurrentActivity(context.Context) (transport.CurrentActivityResponse, error) {
	s.reads++
	if s.onRead != nil {
		s.onRead(s.reads)
	}
	if s.errAt > 0 && s.reads == s.errAt {
		return transport.CurrentActivityResponse{}, errors.New("agent unreachable")
	}
	i := s.reads - 1
	if i >= len(s.answers) {
		i = len(s.answers) - 1
	}
	return transport.CurrentActivityResponse{PackageName: s.answers[i], Activity: "android.widget.FrameLayout"}, nil
}

func newWaitDriver(tr androiddriver.Transport) (*androiddriver.AndroidDriver, *clock.FakeClock, *[]string) {
	clk := clock.NewFakeClock(0)
	var warnings []string
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{
		Serial: "S1",
		Clock:  clk,
		Warn:   func(m string) { warnings = append(warnings, m) },
	})
	return d, clk, &warnings
}

func TestAndroidDriver_LaunchApp_WaitsUntilPackageMatches(t *testing.T) {
	tr := &scriptedForeground{fakeTransport: newFakeTransport(), answers: []string{"", "com.other", testPkg}}
	d, clk, warnings := newWaitDriver(tr)

	if err := d.LaunchApp(context.Background(), testPkg, driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v, want nil", err)
	}
	if tr.reads != 3 {
		t.Fatalf("foreground reads = %d, want 3", tr.reads)
	}
	if clk.NowMs() != 200 || len(*warnings) != 0 {
		t.Fatalf("elapsed = %d ms, warnings = %v, want 200 ms and none", clk.NowMs(), *warnings)
	}
}

func TestAndroidDriver_StopApp_WaitsUntilPackageDiffers(t *testing.T) {
	tr := &scriptedForeground{fakeTransport: newFakeTransport(), answers: []string{testPkg, testPkg, "com.launcher"}}
	d, _, warnings := newWaitDriver(tr)

	if err := d.StopApp(context.Background(), testPkg); err != nil {
		t.Fatalf("StopApp() error = %v, want nil", err)
	}
	if tr.reads != 3 || len(*warnings) != 0 {
		t.Fatalf("reads = %d, warnings = %v, want 3 reads and none", tr.reads, *warnings)
	}
}

func TestAndroidDriver_LaunchApp_Timeout_ReturnsNilAndWarnsOnce(t *testing.T) {
	tr := &scriptedForeground{fakeTransport: newFakeTransport(), answers: []string{"com.handoff"}}
	d, clk, warnings := newWaitDriver(tr)

	if err := d.LaunchApp(context.Background(), testPkg, driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v, want nil on timeout", err)
	}
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], testPkg) {
		t.Fatalf("warnings = %v, want exactly one naming the package", *warnings)
	}
	if clk.NowMs() < 5000 || clk.NowMs() > 5200 {
		t.Fatalf("elapsed = %d ms, want about 5000", clk.NowMs())
	}
}

func TestAndroidDriver_StopApp_Timeout_ReturnsNilAndWarnsOnce(t *testing.T) {
	tr := &scriptedForeground{fakeTransport: newFakeTransport(), answers: []string{testPkg}}
	d, _, warnings := newWaitDriver(tr)

	if err := d.StopApp(context.Background(), testPkg); err != nil {
		t.Fatalf("StopApp() error = %v, want nil on timeout", err)
	}
	if len(*warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", *warnings)
	}
}

func TestAndroidDriver_LaunchApp_ContextCanceledDuringWait_StopsWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tr := &scriptedForeground{fakeTransport: newFakeTransport(), answers: []string{"com.other"}}
	tr.onRead = func(read int) {
		if read == 2 {
			cancel()
		}
	}
	d, _, warnings := newWaitDriver(tr)

	err := d.LaunchApp(ctx, testPkg, driver.LaunchArgs{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LaunchApp() error = %v, want context.Canceled", err)
	}
	if tr.reads != 2 || len(*warnings) != 0 {
		t.Fatalf("reads = %d, warnings = %v, want 2 reads and no warning", tr.reads, *warnings)
	}
}

func TestAndroidDriver_LaunchApp_ForegroundErrorDuringWait_EndsWaitWithOneWarning(t *testing.T) {
	tr := &scriptedForeground{fakeTransport: newFakeTransport(), answers: []string{"com.other"}, errAt: 2}
	d, _, warnings := newWaitDriver(tr)

	if err := d.LaunchApp(context.Background(), testPkg, driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v, want nil", err)
	}
	if tr.reads != 2 || len(*warnings) != 1 {
		t.Fatalf("reads = %d, warnings = %v, want 2 reads and one warning", tr.reads, *warnings)
	}
}

func TestAndroidDriver_LaunchApp_AttachAlreadyForeground_SkipsWait(t *testing.T) {
	tr := &scriptedForeground{fakeTransport: newFakeTransport(), answers: []string{testPkg}}
	d, clk, warnings := newWaitDriver(tr)
	attach := true

	if err := d.LaunchApp(context.Background(), testPkg, driver.LaunchArgs{NoReset: &attach}); err != nil {
		t.Fatalf("LaunchApp() error = %v, want nil", err)
	}
	if tr.reads != 1 || len(tr.launchAppCalls) != 0 || clk.NowMs() != 0 || len(*warnings) != 0 {
		t.Fatalf("reads = %d, launches = %v, elapsed = %d, warnings = %v, want a single read and no launch or wait",
			tr.reads, tr.launchAppCalls, clk.NowMs(), *warnings)
	}
}
