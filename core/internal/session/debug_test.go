package session_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestDebugLock_ResumeWaitsForAnInFlightAct(t *testing.T) {
	dbg := &fakeDebugger{actEntered: make(chan struct{}), actRelease: make(chan struct{}), resumeOut: session.ResumeResult{Resumed: true}}
	s, _, _ := debuggableHolder(t, dbg)
	s.EnterPause(testPause)
	done := make(chan error, 1)
	go func() { _, err := s.Act(context.Background(), "tap: {id: ok}"); done <- err }()
	<-dbg.actEntered

	resumed := make(chan session.ResumeResult, 1)
	go func() {
		res, _ := s.Resume(context.Background(), session.ResumeOptions{})
		resumed <- res
	}()
	awaitParkedIn(t, "lockDebug")
	if _, resumes := dbg.counts(); resumes != 0 {
		t.Fatalf("the runner was woken %d times while an act ran", resumes)
	}
	if _, paused := s.Paused(); !paused {
		t.Fatal("a resume that waits for the lock cleared the pause")
	}

	close(dbg.actRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if res := <-resumed; !res.Resumed {
		t.Fatalf("resume after the act = %+v", res)
	}
	if _, paused := s.Paused(); paused {
		t.Fatal("still paused after a successful resume")
	}
}

func TestResume_ARefusalOrAnErrorKeepsThePause(t *testing.T) {
	dbg := &fakeDebugger{resumeOut: session.ResumeResult{MissReason: "no match for id ok"}}
	s, _, _ := debuggableHolder(t, dbg)
	s.EnterPause(testPause)

	res, err := s.Resume(context.Background(), session.ResumeOptions{})
	if err != nil || res.Resumed || res.MissReason == "" {
		t.Fatalf("resume = %+v, %v, want an advisory refusal", res, err)
	}
	if loc, paused := s.Paused(); !paused || loc != testPause {
		t.Fatalf("pause after a refusal = %+v, %v, want it kept", loc, paused)
	}

	dbg.resumeErr = errors.New("boom")
	if _, err := s.Resume(context.Background(), session.ResumeOptions{}); err == nil {
		t.Fatal("resume error was swallowed")
	}
	if _, paused := s.Paused(); !paused {
		t.Fatal("pause cleared by a failed resume")
	}
}

func TestResume_ARunThatStopsAtItsNextStepStaysPaused(t *testing.T) {
	next := session.PauseLocation{Source: testPause.Source, Line: 8, Command: "type"}
	dbg := &fakeDebugger{resumeOut: session.ResumeResult{Resumed: true}}
	s, _, _ := debuggableHolder(t, dbg)
	dbg.onResume = func() { s.EnterPause(next) }
	s.EnterPause(testPause)

	if _, err := s.Resume(context.Background(), session.ResumeOptions{Step: true}); err != nil {
		t.Fatal(err)
	}
	if loc, paused := s.Paused(); !paused || loc != next {
		t.Fatalf("pause after a step = %+v, %v, want the new stop at %+v", loc, paused, next)
	}
}

func TestDebug_ActAndResumeNeedAPausedRunAndADebugger(t *testing.T) {
	dbg := &fakeDebugger{resumeOut: session.ResumeResult{Resumed: true}}
	s, _, _ := debuggableHolder(t, dbg)
	if _, err := s.Act(context.Background(), "tap: {id: ok}"); codeOf(err) != diag.CodeHolderBusy {
		t.Fatalf("act while not paused: %v, want %s", err, diag.CodeHolderBusy)
	}
	if _, err := s.Resume(context.Background(), session.ResumeOptions{}); codeOf(err) != diag.CodeHolderBusy {
		t.Fatalf("resume while not paused: %v, want %s", err, diag.CodeHolderBusy)
	}
	if acts, resumes := dbg.counts(); acts+resumes != 0 {
		t.Fatalf("the debugger was called %d times for a run that is not paused", acts+resumes)
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

func TestLeavePause_WaitsForAnActInFlight(t *testing.T) {
	dbg := &fakeDebugger{actEntered: make(chan struct{}), actRelease: make(chan struct{})}
	s, _, _ := debuggableHolder(t, dbg)
	s.EnterPause(testPause)
	actDone := make(chan error, 1)
	go func() { _, err := s.Act(context.Background(), "tap: {id: ok}"); actDone <- err }()
	<-dbg.actEntered

	left := make(chan struct{})
	go func() {
		s.LeavePause()
		close(left)
	}()
	awaitParkedIn(t, "lockDebugUninterruptibly")
	if _, paused := s.Paused(); !paused {
		t.Fatal("LeavePause cleared the pause while an act was still running")
	}

	close(dbg.actRelease)
	if err := <-actDone; err != nil {
		t.Fatal(err)
	}
	<-left
	if _, paused := s.Paused(); paused {
		t.Fatal("still paused after LeavePause returned")
	}
}

// awaitParkedIn returns once some goroutine is blocked inside a function
// whose name contains frame, so a test knows a call has reached its lock
// without sleeping.
func awaitParkedIn(t *testing.T, frame string) {
	t.Helper()
	buf := make([]byte, 1<<20)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(string(buf[:runtime.Stack(buf, true)]), "(*Session)."+frame) {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("no goroutine reached %s", frame)
}
