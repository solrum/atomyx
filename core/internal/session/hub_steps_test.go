package session_test

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session"
)

const runA = "run-a"

func stepLine(seq int64) runfolder.StepLine { return runStepLine(runA, seq) }

func runStepLine(runID string, seq int64) runfolder.StepLine {
	return runfolder.StepLine{SchemaVersion: 1, RunID: runID, Seq: seq, StepIndex: int(seq), Command: "tap", Phase: runfolder.StepPhaseStarted}
}

func publishSteps(hub *session.Hub, from, to int64) { publishRunSteps(hub, runA, from, to) }

func publishRunSteps(hub *session.Hub, runID string, from, to int64) {
	for seq := from; seq <= to; seq++ {
		hub.PublishStep(runStepLine(runID, seq))
	}
}

func nextNotice(t *testing.T, feed <-chan session.Observation) session.Observation {
	t.Helper()
	select {
	case o, ok := <-feed:
		if !ok {
			t.Fatal("feed closed, want a notice")
		}
		return o
	default:
		t.Fatal("no notice queued")
		return session.Observation{}
	}
}

func requireNoNotice(t *testing.T, feed <-chan session.Observation) {
	t.Helper()
	select {
	case o := <-feed:
		t.Fatalf("unexpected notice %+v", o)
	default:
	}
}

func TestHub_StepNoticesArriveInOrderWithTreeNotices(t *testing.T) {
	hub := session.NewHub()
	feed, err := hub.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}

	hub.PublishStep(stepLine(1))
	hub.ObserveTree(rootTree("a"), 1, true)
	hub.PublishStep(stepLine(2))

	want := []struct {
		kind session.ObservationKind
		rev  uint64
	}{{session.ObservedStep, 1}, {session.ObservedTree, 1}, {session.ObservedStep, 2}}
	for i, w := range want {
		got := nextNotice(t, feed)
		if got.Kind != w.kind || got.Revision != w.rev {
			t.Errorf("notice %d = kind %d revision %d, want kind %d revision %d", i, got.Kind, got.Revision, w.kind, w.rev)
		}
	}
	lines, gap, _ := hub.StepsSince("", 0)
	if gap || len(lines) != 2 || lines[0].Seq != 1 || lines[1].Seq != 2 {
		t.Fatalf("StepsSince(0) = %+v gap %v, want lines 1 and 2", lines, gap)
	}
}

func TestHub_PlainSubscribeNeverReceivesStepNotices(t *testing.T) {
	hub := session.NewHub()
	feed, err := hub.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	publishSteps(hub, 1, 40)

	requireNoNotice(t, feed)
}

func TestHub_ResumeAfterADropReceivesEveryMissedLineWithNoGap(t *testing.T) {
	hub := session.NewHub()
	feed, err := hub.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	publishSteps(hub, 1, 3)
	for i := 0; i < 3; i++ {
		nextNotice(t, feed)
	}

	publishSteps(hub, 4, 40)
	var last session.Observation
	for o := range feed {
		last = o
	}
	if last.Kind != session.ObservedDropped {
		t.Fatalf("last notice = kind %d, want ObservedDropped", last.Kind)
	}

	resumed, err := hub.SubscribeSince(context.Background(), runA, 3)
	if err != nil {
		t.Fatal(err)
	}
	first := nextNotice(t, resumed)
	if first.Kind != session.ObservedStep || first.Revision != 40 {
		t.Fatalf("first resume notice = kind %d revision %d, want ObservedStep covering 40", first.Kind, first.Revision)
	}
	requireNoNotice(t, resumed)
	lines, gap, _ := hub.StepsSince(runA, 3)
	if gap || len(lines) != 37 || lines[0].Seq != 4 || lines[36].Seq != 40 {
		t.Fatalf("StepsSince(3) = %d lines gap %v, want lines 4 to 40 with no gap", len(lines), gap)
	}
}

func TestHub_ResumePastTheRingReportsAGapThenTheHeldLines(t *testing.T) {
	hub := session.NewHub()
	const total = 400
	publishSteps(hub, 1, total)

	resumed, err := hub.SubscribeSince(context.Background(), runA, 5)
	if err != nil {
		t.Fatal(err)
	}

	gap := nextNotice(t, resumed)
	if gap.Kind != session.ObservedStepGap || gap.Revision <= 6 {
		t.Fatalf("first notice = kind %d revision %d, want ObservedStepGap with the oldest held seq above 6", gap.Kind, gap.Revision)
	}
	step := nextNotice(t, resumed)
	if step.Kind != session.ObservedStep || step.Revision != total {
		t.Fatalf("second notice = kind %d revision %d, want ObservedStep covering %d", step.Kind, step.Revision, total)
	}
	lines, lost, _ := hub.StepsSince(runA, 5)
	if !lost || lines[0].Seq != int64(gap.Revision) || lines[len(lines)-1].Seq != total {
		t.Fatalf("StepsSince(5) first %d gap %v, want the lines from %d on and a gap", lines[0].Seq, lost, gap.Revision)
	}
}

func TestHub_ResumeAtTheNewestLineQueuesNothing(t *testing.T) {
	hub := session.NewHub()
	publishSteps(hub, 1, 3)

	resumed, err := hub.SubscribeSince(context.Background(), runA, 3)
	if err != nil {
		t.Fatal(err)
	}

	requireNoNotice(t, resumed)
}

func TestHub_AResumeFromAnEarlierRunGetsEveryHeldLineOfTheCurrentRunWithNoGap(t *testing.T) {
	hub := session.NewHub()
	publishRunSteps(hub, "run-a", 1, 5)
	publishRunSteps(hub, "run-b", 1, 8)

	lines, gap, err := hub.StepsSince("run-a", 5)

	if err != nil || gap || len(lines) != 8 || lines[0].RunID != "run-b" || lines[0].Seq != 1 || lines[7].Seq != 8 {
		t.Fatalf("StepsSince(run-a, 5) = %d lines gap %v err %v, want all 8 lines of run-b and no gap", len(lines), gap, err)
	}
	resumed, err := hub.SubscribeSince(context.Background(), "run-a", 5)
	if err != nil {
		t.Fatal(err)
	}
	first := nextNotice(t, resumed)
	if first.Kind != session.ObservedStep || first.Revision != 8 {
		t.Fatalf("first resume notice = kind %d revision %d, want ObservedStep covering 8", first.Kind, first.Revision)
	}
	requireNoNotice(t, resumed)
}

func TestHub_AResumeWithNoRunGetsEveryHeldLineOfTheCurrentRun(t *testing.T) {
	hub := session.NewHub()
	publishRunSteps(hub, "run-a", 1, 3)

	lines, gap, err := hub.StepsSince("", 2)

	if err != nil || gap || len(lines) != 3 {
		t.Fatalf("StepsSince(\"\", 2) = %d lines gap %v err %v, want all 3 lines and no gap", len(lines), gap, err)
	}
}

func TestHub_AGapIsReportedOnlyWhenTheCurrentRunsEarliestLinesLeftTheRing(t *testing.T) {
	hub := session.NewHub()
	publishRunSteps(hub, "run-a", 1, 5)
	publishRunSteps(hub, "run-b", 1, 400)

	lines, gap, err := hub.StepsSince("run-a", 5)

	if err != nil || !gap || lines[0].Seq <= 1 || lines[len(lines)-1].Seq != 400 {
		t.Fatalf("StepsSince(run-a, 5) first %d gap %v err %v, want the held tail of run-b and a gap", lines[0].Seq, gap, err)
	}
}

func TestHub_ASinceSeqBeyondTheNewestLineOfTheSameRunIsAClientError(t *testing.T) {
	hub := session.NewHub()
	publishRunSteps(hub, "run-a", 1, 3)

	_, _, err := hub.StepsSince("run-a", 4)
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("StepsSince error = %v, want INVALID_ARGUMENT", err)
	}
	if _, err := hub.SubscribeSince(context.Background(), "run-a", 4); err == nil {
		t.Fatal("SubscribeSince accepted a sinceSeq beyond the newest line")
	}
	if _, err := hub.SubscribeSince(context.Background(), "run-a", 3); err != nil {
		t.Fatalf("SubscribeSince at the newest line = %v, want accepted", err)
	}
}

func TestHub_PublishingNeverBlocksOnAStuckSubscriber(t *testing.T) {
	hub := session.NewHub()
	stuck, err := hub.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		publishSteps(hub, 1, 1000)
	}()
	<-done

	var last session.Observation
	for o := range stuck {
		last = o
	}
	if last.Kind != session.ObservedDropped {
		t.Fatalf("stuck subscriber ended with kind %d, want ObservedDropped", last.Kind)
	}
	if lines, _, _ := hub.StepsSince(runA, 990); len(lines) != 10 {
		t.Fatalf("hub kept %d lines after seq 990, want 10", len(lines))
	}
}

func TestSession_StateChangesReachASubscriberInOrderWithStepNotices(t *testing.T) {
	s, _ := openMock(t)
	feed, err := s.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}

	s.PublishStep(stepLine(1))
	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	s.PublishStep(stepLine(2))
	if err := s.SetRunning(runA); err != nil {
		t.Fatal(err)
	}

	step := nextNotice(t, feed)
	idle := nextNotice(t, feed)
	step2 := nextNotice(t, feed)
	running := nextNotice(t, feed)
	if step.Kind != session.ObservedStep || step.Revision != 1 || step2.Kind != session.ObservedStep || step2.Revision != 2 {
		t.Fatalf("step notices = %+v and %+v, want lines 1 and 2", step, step2)
	}
	if idle.Kind != session.ObservedState || idle.State != session.StateIdle || idle.RunID != "" || idle.Command != s.Info().Holder.Command {
		t.Fatalf("second notice = %+v, want the idle state of %q", idle, s.Info().Holder.Command)
	}
	if running.Kind != session.ObservedState || running.State != session.StateRunning || running.RunID != runA {
		t.Fatalf("fourth notice = %+v, want the running state of run %s", running, runA)
	}
	requireNoNotice(t, feed)
}
