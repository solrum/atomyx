package session_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"dev.atomyx/internal/session"
)

var testPause = session.PauseLocation{Source: "/work/login.yml", Line: 7, Depth: 0, Command: "tap"}

func drainStates(ch <-chan session.Observation) []session.HolderState {
	var states []session.HolderState
	for {
		select {
		case o, ok := <-ch:
			if !ok {
				return states
			}
			if o.Kind == session.ObservedState {
				states = append(states, o.State)
			}
		default:
			return states
		}
	}
}

func TestPause_InfoAndViewersReportThePausedStateAndItsLocation(t *testing.T) {
	s, _ := openMock(t)
	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	events, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	s.EnterPause(testPause)
	info := s.Info()
	if info.State != session.StatePaused || info.Pause == nil || *info.Pause != testPause {
		t.Fatalf("paused info = %+v, want state paused at %+v", info, testPause)
	}
	if got := drainStates(events); !reflect.DeepEqual(got, []session.HolderState{session.StatePaused}) {
		t.Fatalf("viewers saw %v, want one paused notice", got)
	}

	s.LeavePause()
	info = s.Info()
	if info.State != session.StateIdle || info.Pause != nil {
		t.Fatalf("info after LeavePause = %+v, want idle with no pause", info)
	}
	if got := drainStates(events); !reflect.DeepEqual(got, []session.HolderState{session.StateIdle}) {
		t.Fatalf("viewers saw %v after LeavePause, want idle", got)
	}
}

func TestPause_SurvivesConcurrentActivityChangesAndNeverShowsAsRunningOrIdle(t *testing.T) {
	s, _ := openMock(t)
	events, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.EnterPause(testPause)
	drainStates(events)

	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			for range 5 {
				if i%2 == 0 {
					_ = s.SetRunning("run-1")
				} else {
					_ = s.SetIdle()
				}
			}
		})
	}
	wg.Wait()

	if info := s.Info(); info.State != session.StatePaused || info.Pause == nil {
		t.Fatalf("info after activity changes = %+v, want still paused", info)
	}
	for _, state := range drainStates(events) {
		if state != session.StatePaused {
			t.Fatalf("a viewer was told %q while the run was paused", state)
		}
	}
}

func TestPause_NeverReachesTheLeaseMetadata(t *testing.T) {
	s, _, _ := socketHolder(t)
	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	before := s.Info().Holder

	s.EnterPause(testPause)
	if got := s.Info().Holder; !reflect.DeepEqual(got, before) {
		t.Fatalf("lease holder changed on EnterPause: %+v, want %+v", got, before)
	}
	s.LeavePause()
	if got := s.Info().Holder; !reflect.DeepEqual(got, before) {
		t.Fatalf("lease holder changed on LeavePause: %+v, want %+v", got, before)
	}
}
