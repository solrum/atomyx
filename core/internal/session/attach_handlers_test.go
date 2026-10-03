package session_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

const typedPassword = "hunter2-correct-horse"

// socketHolder opens a leased session whose attach socket a test can dial,
// and returns it with the in-memory driver it drives.
func socketHolder(t *testing.T) (*session.Session, *mockdriver.Driver, string) {
	t.Helper()
	s, drv, path := socketHolderDriver(t)
	return s, drv.Driver, path
}

// socketHolderDriver is socketHolder that also returns the recording
// driver, whose onHierarchy hook lets a test overlap a mutating call with
// a capture.
func socketHolderDriver(t *testing.T) (*session.Session, *recordingDriver, string) {
	t.Helper()
	m := newSocketManager(t, clock.SystemClock{})
	factory := &recordingFactory{events: &eventLog{}}
	s, err := session.Open(context.Background(), socketDeps(t, factory, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, factory.drivers[0], socketPathFor(m)
}

func nextFrame(t *testing.T, c *attach.Client) attach.Frame {
	t.Helper()
	select {
	case f := <-c.Notices():
		return f
	case <-c.Done():
		t.Fatalf("connection ended while waiting for a notice: %v", c.Err())
		return attach.Frame{}
	}
}

func subscribe(t *testing.T, c *attach.Client, runID string, sinceSeq int64) {
	t.Helper()
	if err := c.Call(context.Background(), attach.TypeSubscribe, attach.Subscribe{RunID: runID, SinceSeq: sinceSeq}, nil); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
}

func codeOfRemote(err error) string {
	code, _, _ := diag.Describe(err)
	return code
}

func TestAttachStatus_ReportsTheHolderAndItsState(t *testing.T) {
	s, _, path := socketHolder(t)
	c := dialHolder(t, path)
	var status attach.Status
	if err := c.Call(context.Background(), attach.TypeStatus, nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "running" || status.Device.ID != "A" || status.Device.Platform != "android" {
		t.Fatalf("status = %+v, want a running holder of android device A", status)
	}
	if status.Holder.PID != os.Getpid() || status.Holder.Command != "atomyx run" || status.Holder.Role != "cli" {
		t.Fatalf("holder = %+v, want this process as atomyx run in role cli", status.Holder)
	}

	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), attach.TypeStatus, nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "idle" {
		t.Fatalf("state = %q, want idle after SetIdle", status.State)
	}
}

func TestAttachTreeFresh_IsRefusedUnlessTheHolderIsIdle(t *testing.T) {
	s, mock, path := socketHolder(t)
	c := dialHolder(t, path)
	ctx := context.Background()
	for range 2 {
		mock.StageHierarchy(secureTree(typedPassword))
	}
	mock.ScreenshotData = encodedPNG(t, 430, 932)
	var fresh attach.FreshResult

	err := c.Call(ctx, attach.TypeTreeFresh, nil, &fresh)
	if codeOfRemote(err) != diag.CodeHolderBusy {
		t.Fatalf("fresh while running: %v, want %s", err, diag.CodeHolderBusy)
	}

	if err := s.SetRunning("run-1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, attach.TypeTreeFresh, nil, &fresh); codeOfRemote(err) != diag.CodeHolderBusy {
		t.Fatalf("fresh with a run open: %v, want %s", err, diag.CodeHolderBusy)
	}

	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, attach.TypeTreeFresh, nil, &fresh); err != nil {
		t.Fatalf("fresh while idle: %v", err)
	}
	if fresh.Tree.Tree == nil || !fresh.Paired || len(fresh.Screenshot.Image) == 0 {
		t.Fatalf("fresh = %+v, want a paired tree and screenshot", fresh)
	}

	release, err := s.Gate().Enter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := c.Call(ctx, attach.TypeTreeFresh, nil, &fresh); codeOfRemote(err) != diag.CodeHolderBusy {
		t.Fatalf("fresh with a call in flight: %v, want %s", err, diag.CodeHolderBusy)
	}
}

func TestAttachTreeAndScreenshot_ServeTheLatestCaptureWithSecretsMasked(t *testing.T) {
	s, mock, path := socketHolder(t)
	c := dialHolder(t, path)
	ctx := context.Background()
	var tree attach.TreeResult
	if err := c.Call(ctx, attach.TypeTreeLatest, nil, &tree); err != nil || tree.Tree != nil {
		t.Fatalf("tree.latest before any capture = %+v, %v, want no tree", tree, err)
	}

	mock.StageHierarchy(secureTree(typedPassword))
	image := encodedPNG(t, 430, 932)
	mock.ScreenshotData = image
	if _, err := s.Orchestra().Hierarchy(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Orchestra().Screenshot(ctx); err != nil {
		t.Fatal(err)
	}

	var raw json.RawMessage
	if err := c.Call(ctx, attach.TypeTreeLatest, nil, &raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), typedPassword) {
		t.Fatalf("tree.latest carries the secret: %s", raw)
	}
	if err := json.Unmarshal(raw, &tree); err != nil || tree.Tree == nil || tree.Revision == 0 {
		t.Fatalf("tree.latest = %s, %v, want a stored tree with a revision", raw, err)
	}
	if got := tree.Tree.Children[1].Attributes["text"]; got == typedPassword || got == "" {
		t.Fatalf("password text = %q, want the mask", got)
	}

	var shot attach.ScreenshotResult
	if err := c.Call(ctx, attach.TypeScreenshotLatest, nil, &shot); err != nil {
		t.Fatal(err)
	}
	if shot.Format != "png" || string(shot.Image) != string(image) || shot.Revision == 0 {
		t.Fatalf("screenshot.latest = format %q, %d bytes, revision %d, want the stored png", shot.Format, len(shot.Image), shot.Revision)
	}
}

func TestAttachSubscribe_DeliversStepsAndCapturesInOrder(t *testing.T) {
	s, mock, path := socketHolder(t)
	c := dialHolder(t, path)
	subscribe(t, c, "", 0)

	var first, second attach.StepNotice
	var tree attach.TreeNotice

	s.PublishStep(stepLine(1))
	if f := nextFrame(t, c); f.Type != attach.NoticeStep || f.Decode(&first) != nil || first.Line.Seq != 1 || first.Line.RunID != runA {
		t.Fatalf("first notice = %+v %+v, want step 1 of %s", f, first, runA)
	}

	mock.StageHierarchy(rootTree("r"))
	if err := s.Built().Driver.Tap(context.Background(), wire.Point{X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Orchestra().Hierarchy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f := nextFrame(t, c); f.Type != attach.NoticeTree || f.Decode(&tree) != nil || tree.Revision != 1 || tree.Epoch == 0 {
		t.Fatalf("second notice = %+v %+v, want tree revision 1", f, tree)
	}

	s.PublishStep(stepLine(2))
	if f := nextFrame(t, c); f.Type != attach.NoticeStep || f.Decode(&second) != nil || second.Line.Seq != 2 || second.Epoch != tree.Epoch {
		t.Fatalf("third notice = %+v %+v, want step 2", f, second)
	}
}

func TestAttachSubscribe_ResumesAfterTheLastLineTheViewerRead(t *testing.T) {
	s, _, path := socketHolder(t)
	for seq := int64(1); seq <= 3; seq++ {
		s.PublishStep(stepLine(seq))
	}
	c := dialHolder(t, path)
	subscribe(t, c, runA, 1)

	for _, want := range []int64{2, 3} {
		var n attach.StepNotice
		if f := nextFrame(t, c); f.Type != attach.NoticeStep || f.Decode(&n) != nil || n.Line.Seq != want {
			t.Fatalf("notice = %+v %+v, want step %d", f, n, want)
		}
	}
}

func TestAttachSubscribe_AnnouncesLinesTheRingNoLongerHolds(t *testing.T) {
	s, _, path := socketHolder(t)
	const published = 300
	for seq := int64(1); seq <= published; seq++ {
		s.PublishStep(stepLine(seq))
	}
	c := dialHolder(t, path)
	subscribe(t, c, runA, 0)

	var gap attach.StepGapNotice
	f := nextFrame(t, c)
	if f.Type != attach.NoticeStepGap || f.Decode(&gap) != nil || gap.OldestSeq <= 1 {
		t.Fatalf("first notice = %+v %+v, want a gap above line 1", f, gap)
	}
	var first attach.StepNotice
	if f := nextFrame(t, c); f.Type != attach.NoticeStep || f.Decode(&first) != nil || first.Line.Seq != gap.OldestSeq {
		t.Fatalf("second notice = %+v %+v, want the oldest held line %d", f, first, gap.OldestSeq)
	}
}

// overlapTap makes every hierarchy capture of drv overlap a tap, which
// the observing driver reports as an unstable capture.
func overlapTap(s *session.Session, drv *recordingDriver) {
	drv.onHierarchy = func(ctx context.Context) {
		_ = s.Built().Driver.Tap(ctx, wire.Point{X: 1, Y: 1})
	}
}

func TestAttachCaptureState_NoticesAndResultsCarryStabilityAndPairing(t *testing.T) {
	s, drv, path := socketHolderDriver(t)
	c := dialHolder(t, path)
	ctx := context.Background()
	subscribe(t, c, "", 0)
	drv.Screen = wire.Size{Width: 430, Height: 932}
	drv.ScreenshotData = encodedPNG(t, 860, 1864)
	drv.StageHierarchy(rootTree("r"))
	drv.StageHierarchy(rootTree("r"))

	overlapTap(s, drv)
	if _, err := s.Orchestra().Hierarchy(ctx); err != nil {
		t.Fatal(err)
	}
	var tree attach.TreeNotice
	if f := nextFrame(t, c); f.Type != attach.NoticeTree || f.Decode(&tree) != nil || tree.Stable || tree.Paired {
		t.Fatalf("tree notice = %+v %+v, want an unstable, unpaired tree", f, tree)
	}
	var latest attach.TreeResult
	if err := c.Call(ctx, attach.TypeTreeLatest, nil, &latest); err != nil || latest.Stable {
		t.Fatalf("tree.latest = %+v, %v, want an unstable tree", latest, err)
	}

	drv.onHierarchy = nil
	if _, err := s.Orchestra().ScreenSize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Orchestra().Hierarchy(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Orchestra().Screenshot(ctx); err != nil {
		t.Fatal(err)
	}
	for f := nextFrame(t, c); f.Type != attach.NoticeScreenshot; f = nextFrame(t, c) {
		if f.Type != attach.NoticeTree {
			t.Fatalf("notice = %+v, want a tree or screenshot notice", f)
		}
	}
	want := s.Latest()
	if !want.Paired || !want.Highlightable || want.Scale != 2 || !want.TreeStable || !want.ScreenshotStable {
		t.Fatalf("holder snapshot = %+v, want a stable highlightable pair at scale 2", want)
	}
	var shotResult attach.ScreenshotResult
	if err := c.Call(ctx, attach.TypeScreenshotLatest, nil, &shotResult); err != nil {
		t.Fatal(err)
	}
	wantState := attach.CaptureState{Stable: true, Paired: true, Highlightable: true, Scale: 2}
	if shotResult.CaptureState != wantState {
		t.Fatalf("screenshot.latest state = %+v, want %+v", shotResult.CaptureState, wantState)
	}
}
