package iosdriver

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

const superviseVerdictTimeout = 10 * time.Second

// openPipeProcess models a child that exited while a descendant still
// holds its stdout open: Wait reports the exit, but Stdout never
// reaches EOF.
type openPipeProcess struct {
	result process.BackgroundResult
	never  chan struct{}
}

func (p *openPipeProcess) Stdout() io.Reader { return blockingReader{p.never} }
func (p *openPipeProcess) Wait() (process.BackgroundResult, error) {
	return p.result, nil
}
func (p *openPipeProcess) Kill() error                   { return nil }
func (p *openPipeProcess) Interrupt(time.Duration) error { return nil }
func (p *openPipeProcess) Pid() int                      { return 0 }

type blockingReader struct{ never chan struct{} }

func (r blockingReader) Read([]byte) (int, error) {
	<-r.never
	return 0, io.EOF
}

func TestSuperviseBackgroundReportsExitWhileStdoutStaysOpen(t *testing.T) {
	proc := &openPipeProcess{
		result: process.BackgroundResult{ExitCode: 7, Stderr: "tail"},
		never:  make(chan struct{}),
	}
	defer close(proc.never)
	classified := errors.New("classified")
	exited, stop := superviseBackground(proc, func(r process.BackgroundResult, _ error) error {
		if r.ExitCode != 7 || r.Stderr != "tail" {
			t.Errorf("classify got %+v", r)
		}
		return classified
	})

	select {
	case err := <-exited:
		if !errors.Is(err, classified) {
			t.Fatalf("exit verdict = %v, want the classify result", err)
		}
	case <-time.After(superviseVerdictTimeout):
		t.Fatal("exit verdict was gated on stdout reaching EOF")
	}

	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(superviseVerdictTimeout):
		t.Fatal("stop blocked on a stdout reader that never ends")
	}
}

func TestNewPanicsWithoutABackgroundRunner(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New accepted a nil Options.Background")
		}
	}()
	New(nil, process.NewFake(), Options{UDID: "DEV-1"})
}

func TestNewXctestLauncherPanicsWithoutABackgroundRunner(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewXctestLauncher accepted a nil background runner")
		}
	}()
	NewXctestLauncher(process.NewFake(), nil, nil, LaunchConfig{})
}

func TestReportExitNotifiesAnExitNobodyAskedFor(t *testing.T) {
	exited := make(chan error, 1)
	cause := errors.New("runner exited")
	exited <- cause
	var stopping atomic.Bool
	var got error
	reportExit(exited, &stopping, func(verdict error) { got = verdict })
	if !errors.Is(got, cause) {
		t.Fatalf("notified verdict = %v, want %v", got, cause)
	}
}

func TestReportExitStaysSilentForADeliberateStop(t *testing.T) {
	exited := make(chan error, 1)
	exited <- errors.New("killed")
	var stopping atomic.Bool
	stopping.Store(true)
	reportExit(exited, &stopping, func(error) { t.Error("a deliberate stop was reported as a loss") })
}

func TestNotifyOnExitWithoutACallbackWatchesNothing(t *testing.T) {
	exited := make(chan error, 1)
	exited <- errors.New("exit")
	var stopping atomic.Bool
	notifyOnExit(exited, &stopping, nil)
	if len(exited) != 1 {
		t.Fatal("a nil callback consumed the exit verdict")
	}
}
