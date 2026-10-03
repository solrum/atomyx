package session_test

import (
	"context"
	"testing"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session"
)

func stepLine(seq int64) runfolder.StepLine {
	return runfolder.StepLine{SchemaVersion: 1, Seq: seq, StepIndex: int(seq), Command: "tap", Phase: runfolder.StepPhaseStarted}
}

func publishSteps(hub *session.Hub, from, to int64) {
	for seq := from; seq <= to; seq++ {
		hub.PublishStep(stepLine(seq))
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
	feed, err := hub.SubscribeSince(context.Background(), 0)
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
	lines, gap := hub.StepsSince(0)
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
	feed, err := hub.SubscribeSince(context.Background(), 0)
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

	resumed, err := hub.SubscribeSince(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	first := nextNotice(t, resumed)
	if first.Kind != session.ObservedStep || first.Revision != 40 {
		t.Fatalf("first resume notice = kind %d revision %d, want ObservedStep covering 40", first.Kind, first.Revision)
	}
	requireNoNotice(t, resumed)
	lines, gap := hub.StepsSince(3)
	if gap || len(lines) != 37 || lines[0].Seq != 4 || lines[36].Seq != 40 {
		t.Fatalf("StepsSince(3) = %d lines gap %v, want lines 4 to 40 with no gap", len(lines), gap)
	}
}

func TestHub_ResumePastTheRingReportsAGapThenTheHeldLines(t *testing.T) {
	hub := session.NewHub()
	const total = 400
	publishSteps(hub, 1, total)

	resumed, err := hub.SubscribeSince(context.Background(), 5)
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
	lines, lost := hub.StepsSince(5)
	if !lost || lines[0].Seq != int64(gap.Revision) || lines[len(lines)-1].Seq != total {
		t.Fatalf("StepsSince(5) first %d gap %v, want the lines from %d on and a gap", lines[0].Seq, lost, gap.Revision)
	}
}

func TestHub_ResumeAtTheNewestLineQueuesNothing(t *testing.T) {
	hub := session.NewHub()
	publishSteps(hub, 1, 3)

	resumed, err := hub.SubscribeSince(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}

	requireNoNotice(t, resumed)
}

func TestHub_ANewRunRestartingTheNumberingReportsAGapToAnOldResume(t *testing.T) {
	hub := session.NewHub()
	publishSteps(hub, 1, 50)
	publishSteps(hub, 1, 2)

	lines, gap := hub.StepsSince(50)

	if !gap || len(lines) != 2 || lines[0].Seq != 1 {
		t.Fatalf("StepsSince(50) = %+v gap %v, want the two lines of the new run and a gap", lines, gap)
	}
}

func TestHub_PublishingNeverBlocksOnAStuckSubscriber(t *testing.T) {
	hub := session.NewHub()
	stuck, err := hub.SubscribeSince(context.Background(), 0)
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
	if lines, _ := hub.StepsSince(990); len(lines) != 10 {
		t.Fatalf("hub kept %d lines after seq 990, want 10", len(lines))
	}
}
