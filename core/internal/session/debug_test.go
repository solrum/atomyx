package session_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

// fakeDebugger records the calls a session forwards. When actEntered is
// set, Act signals it and then blocks until actRelease closes.
type fakeDebugger struct {
	session.Debugger
	mu         sync.Mutex
	acts       int
	resumes    int
	actEntered chan struct{}
	actRelease chan struct{}
	resumeOut  session.ResumeResult
	resumeErr  error
	onResume   func()
}

func (f *fakeDebugger) Act(context.Context, string) (session.ActResult, error) {
	f.mu.Lock()
	f.acts++
	entered, release := f.actEntered, f.actRelease
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
		<-release
	}
	return session.ActResult{Command: "tap", Outcome: "passed"}, nil
}

func (f *fakeDebugger) Resume(context.Context, session.ResumeOptions) (session.ResumeResult, error) {
	f.mu.Lock()
	f.resumes++
	out, err, hook := f.resumeOut, f.resumeErr, f.onResume
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return out, err
}

func (f *fakeDebugger) counts() (acts, resumes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acts, f.resumes
}

// debuggableHolder opens a leased session with an attach socket and the
// given Debugger.
func debuggableHolder(t *testing.T, dbg session.Debugger) (*session.Session, *recordingDriver, string) {
	t.Helper()
	m := newSocketManager(t, clock.SystemClock{})
	factory := &recordingFactory{events: &eventLog{}}
	s, err := session.Open(context.Background(), socketDeps(t, factory, m), session.Request{Descriptor: androidDesc("A"), Debugger: dbg})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	drv := factory.drivers[0]
	drv.ScreenshotData = encodedPNG(t, 430, 932)
	for range 4 {
		drv.StageHierarchy(rootTree("r"))
	}
	return s, drv, socketPathFor(m)
}

func TestDebugLock_AFreshCaptureIsRefusedWhileAnActRuns(t *testing.T) {
	dbg := &fakeDebugger{actEntered: make(chan struct{}), actRelease: make(chan struct{})}
	s, drv, _ := debuggableHolder(t, dbg)
	s.EnterPause(testPause)

	done := make(chan error, 1)
	go func() { _, err := s.Act(context.Background(), "tap: {id: ok}"); done <- err }()
	<-dbg.actEntered

	captures := 0
	drv.onHierarchy = func(context.Context) { captures++ }
	if _, err := s.FreshTree(context.Background()); codeOf(err) != diag.CodeHolderBusy {
		t.Fatalf("fresh during an act: %v, want %s", err, diag.CodeHolderBusy)
	}
	if captures != 0 {
		t.Fatalf("a refused capture still made %d hierarchy calls", captures)
	}

	close(dbg.actRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := s.FreshTree(context.Background()); err != nil {
		t.Fatalf("fresh after the act: %v", err)
	}
}

func TestDebugLock_AnActWaitsForAFreshCaptureInFlight(t *testing.T) {
	dbg := &fakeDebugger{}
	s, drv, _ := debuggableHolder(t, dbg)
	s.EnterPause(testPause)
	var actErr error
	drv.onHierarchy = func(ctx context.Context) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		_, actErr = s.Act(ctx, "tap: {id: ok}")
	}

	if _, err := s.FreshTree(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(actErr, context.Canceled) {
		t.Fatalf("act during a capture: %v, want it to wait for the lock and give up", actErr)
	}
	if acts, _ := dbg.counts(); acts != 0 {
		t.Fatalf("the debugger ran %d acts during a capture, want none", acts)
	}
}

func TestDebug_ActNeedsAPausedRunAndADebugger(t *testing.T) {
	dbg := &fakeDebugger{resumeOut: session.ResumeResult{Resumed: true}}
	s, _, _ := debuggableHolder(t, dbg)
	if _, err := s.Act(context.Background(), "tap: {id: ok}"); codeOf(err) != diag.CodeHolderBusy {
		t.Fatalf("act while not paused: %v, want %s", err, diag.CodeHolderBusy)
	}
	if acts, _ := dbg.counts(); acts != 0 {
		t.Fatalf("the debugger ran %d acts for a run that is not paused", acts)
	}

	plain, _ := openMock(t)
	plain.EnterPause(testPause)
	if _, err := plain.Act(context.Background(), "tap: {id: ok}"); codeOf(err) != diag.CodeUnsupported {
		t.Fatalf("act without a debugger: %v, want %s", err, diag.CodeUnsupported)
	}
}

func TestAttachTreeFresh_IsAdmittedWhilePausedEvenWithACallInFlight(t *testing.T) {
	dbg := &fakeDebugger{actEntered: make(chan struct{}), actRelease: make(chan struct{})}
	s, _, path := debuggableHolder(t, dbg)
	c := dialHolder(t, path)
	ctx := context.Background()
	var fresh attach.FreshResult

	release, err := s.Gate().Enter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	s.EnterPause(testPause)
	if err := c.Call(ctx, attach.TypeTreeFresh, nil, &fresh); err != nil {
		t.Fatalf("fresh while paused: %v", err)
	}

	done := make(chan error, 1)
	go func() { _, err := s.Act(ctx, "tap: {id: ok}"); done <- err }()
	<-dbg.actEntered
	if err := c.Call(ctx, attach.TypeTreeFresh, nil, &fresh); codeOfRemote(err) != diag.CodeHolderBusy {
		t.Fatalf("fresh during an act: %v, want %s", err, diag.CodeHolderBusy)
	}
	close(dbg.actRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
