package session_test

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

// scriptedDebugger records what the holder forwards and refuses a resume
// that is not forced while refuseUnforced is set.
type scriptedDebugger struct {
	session.Debugger
	mu             sync.Mutex
	breakpoints    []string
	pauses         int
	resumes        []session.ResumeOptions
	acts           []string
	refuseUnforced bool
}

func (d *scriptedDebugger) SetBreakpoints(locations []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.breakpoints = slices.Clone(locations)
	return nil
}

func (d *scriptedDebugger) Pause() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pauses++
	return nil
}

func (d *scriptedDebugger) Resume(_ context.Context, opts session.ResumeOptions) (session.ResumeResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resumes = append(d.resumes, opts)
	if d.refuseUnforced && !opts.Force {
		return session.ResumeResult{MissReason: "the target is not on screen"}, nil
	}
	return session.ResumeResult{Resumed: true}, nil
}

func (d *scriptedDebugger) Act(_ context.Context, stepYAML string) (session.ActResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.acts = append(d.acts, stepYAML)
	return session.ActResult{Command: "tap", Outcome: "passed", DurationMs: 12, Artifacts: []string{"artifacts/a.png"}, Step: "- tap: ok"}, nil
}

func (d *scriptedDebugger) Variables() (session.VariablesView, error) {
	return session.VariablesView{Variables: map[string]string{"user": "ann", "token": "****"}}, nil
}

func TestAttachDebug_AHolderWithoutADebuggerAdvertisesNoDebugAndAnswersUnsupported(t *testing.T) {
	s, _, path := socketHolder(t)
	s.EnterPause(testPause)
	client := dialHolder(t, path)
	if slices.Contains(client.Features(), attach.FeatureDebug) {
		t.Fatal("a session without a debugger advertises debug")
	}
	requests := []string{attach.TypeDebugBreakpointsSet, attach.TypeDebugPause, attach.TypeDebugResume,
		attach.TypeDebugStep, attach.TypeDebugVariablesGet, attach.TypeActStep}
	for _, messageType := range requests {
		var data any
		switch messageType {
		case attach.TypeDebugBreakpointsSet:
			data = attach.DebugBreakpoints{Locations: []string{"a.yml:1"}}
		case attach.TypeActStep:
			data = attach.ActStep{Step: "tap: {id: ok}"}
		}
		if got := codeOfRemote(client.Call(context.Background(), messageType, data, nil)); got != diag.CodeUnsupported {
			t.Fatalf("%s: code = %q, want %s", messageType, got, diag.CodeUnsupported)
		}
	}
}

func TestAttachDebug_ARunThatIsNotPausedRefusesEverythingButPauseAndBreakpoints(t *testing.T) {
	dbg := &scriptedDebugger{}
	_, _, path := debuggableHolder(t, dbg)
	client := dialHolder(t, path)
	if !slices.Contains(client.Features(), attach.FeatureDebug) {
		t.Fatal("a session with a debugger lacks the debug feature")
	}
	ctx := context.Background()
	allowed := map[string]any{
		attach.TypeDebugBreakpointsSet: attach.DebugBreakpoints{Locations: []string{"a.yml:3"}},
		attach.TypeDebugPause:          nil,
	}
	for messageType, data := range allowed {
		if err := client.Call(ctx, messageType, data, nil); err != nil {
			t.Fatalf("%s while running: %v", messageType, err)
		}
	}
	refused := map[string]any{
		attach.TypeDebugVariablesGet: nil,
		attach.TypeDebugResume:       attach.DebugResume{},
		attach.TypeDebugStep:         attach.DebugResume{},
		attach.TypeActStep:           attach.ActStep{Step: "tap: {id: ok}"},
	}
	for messageType, data := range refused {
		if got := codeOfRemote(client.Call(ctx, messageType, data, nil)); got != diag.CodeHolderBusy {
			t.Fatalf("%s while running: code = %q, want %s", messageType, got, diag.CodeHolderBusy)
		}
	}
	if len(dbg.resumes) != 0 || len(dbg.acts) != 0 {
		t.Fatalf("the debugger saw %d resumes and %d acts of a run that was not paused", len(dbg.resumes), len(dbg.acts))
	}
}

func TestAttachDebug_TheStateNoticeCarriesALocationOnlyWhilePaused(t *testing.T) {
	s, _, path := debuggableHolder(t, &scriptedDebugger{})
	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	client := dialHolder(t, path)
	subscribe(t, client, "", 0)

	s.EnterPause(testPause)
	paused := stateNoticeJSON(t, client)
	var pausedNotice attach.StateNotice
	if err := json.Unmarshal(paused, &pausedNotice); err != nil {
		t.Fatal(err)
	}
	wantLoc := attach.PauseLocation{Source: testPause.Source, Line: testPause.Line, Depth: testPause.Depth, Command: testPause.Command}
	if pausedNotice.State != "paused" || pausedNotice.Pause == nil || *pausedNotice.Pause != wantLoc {
		t.Fatalf("paused notice = %s, want location %+v", paused, wantLoc)
	}

	s.LeavePause()
	idle := stateNoticeJSON(t, client)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(idle, &raw); err != nil {
		t.Fatal(err)
	}
	if _, has := raw["pause"]; has {
		t.Fatalf("idle notice = %s, want no pause field", idle)
	}
}

func stateNoticeJSON(t *testing.T, c *attach.Client) json.RawMessage {
	t.Helper()
	for {
		f := nextFrame(t, c)
		if f.Type == attach.NoticeState {
			return f.Data
		}
	}
}
