package session_test

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"dev.atomyx/internal/session"
)

func awaitState(t *testing.T, feed <-chan session.Observation, want session.HolderState) session.Observation {
	t.Helper()
	guard := time.After(remoteGuard)
	for {
		select {
		case o, open := <-feed:
			if !open {
				t.Fatalf("feed closed before state %s", want)
			}
			if o.Kind == session.ObservedState && o.State == want {
				return o
			}
		case <-guard:
			t.Fatalf("no state %s within %s", want, remoteGuard)
		}
	}
}

func TestRemoteDebug_AHandleOffersNoClientWhenTheHolderHasNoDebugger(t *testing.T) {
	_, _, path := socketHolder(t)
	if _, ok := dialRemote(t, path).Debug(); ok {
		t.Fatal("a remote handle offers a debug client for a holder without a debugger")
	}
}

func TestRemoteDebug_ARemoteClientDrivesAPausedRunEndToEnd(t *testing.T) {
	dbg := &scriptedDebugger{refuseUnforced: true}
	s, _, path := debuggableHolder(t, dbg)
	h := dialRemote(t, path)
	feed, err := h.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dc, _ := h.Debug()
	ctx := context.Background()

	if err := dc.SetBreakpoints(ctx, []string{"a.yml:3", "inline:2"}); err != nil {
		t.Fatal(err)
	}
	if err := dc.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dbg.breakpoints, []string{"a.yml:3", "inline:2"}) || dbg.pauses != 1 {
		t.Fatalf("debugger saw breakpoints %v and %d pauses", dbg.breakpoints, dbg.pauses)
	}

	s.EnterPause(testPause)
	paused := awaitState(t, feed, session.StatePaused)
	if paused.Pause == nil || *paused.Pause != testPause {
		t.Fatalf("paused observation location = %+v, want %+v", paused.Pause, testPause)
	}
	if info := h.Info(); info.Pause == nil || *info.Pause != testPause {
		t.Fatalf("remote Info().Pause = %+v, want %+v", info.Pause, testPause)
	}

	vars, err := dc.Variables(ctx)
	if err != nil || vars.Variables["user"] != "ann" || vars.Variables["token"] != "****" {
		t.Fatalf("Variables() = %+v, %v", vars, err)
	}
	act, err := dc.Act(ctx, "tap: {id: ok}")
	if err != nil || act.Command != "tap" || act.Outcome != "passed" || act.DurationMs != 12 || !slices.Equal(act.Artifacts, []string{"artifacts/a.png"}) {
		t.Fatalf("Act() = %+v, %v", act, err)
	}
	if !slices.Equal(dbg.acts, []string{"tap: {id: ok}"}) {
		t.Fatalf("debugger acts = %v", dbg.acts)
	}

	miss, err := dc.Resume(ctx, session.ResumeOptions{Step: true})
	if err != nil || miss.Resumed || miss.MissReason != "the target is not on screen" {
		t.Fatalf("unforced step = %+v, %v, want the advisory miss", miss, err)
	}
	if _, still := s.Paused(); !still {
		t.Fatal("an advisory miss left the run unpaused")
	}
	done, err := dc.Resume(ctx, session.ResumeOptions{Step: true, Force: true})
	if err != nil || !done.Resumed {
		t.Fatalf("forced step = %+v, %v", done, err)
	}
	want := []session.ResumeOptions{{Step: true}, {Step: true, Force: true}}
	if !reflect.DeepEqual(dbg.resumes, want) {
		t.Fatalf("debugger resumes = %+v, want %+v", dbg.resumes, want)
	}
	left := awaitState(t, feed, session.StateRunning)
	if left.Pause != nil {
		t.Fatalf("a state after the pause carries location %+v", left.Pause)
	}
}
