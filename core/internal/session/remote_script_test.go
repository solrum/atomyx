package session_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/session"
)

// scriptedHolder is a holder that speaks the attach protocol by hand, so
// a test decides the exact order of every frame on the wire.
type scriptedHolder struct {
	t    *testing.T
	nc   net.Conn
	read *attach.FrameReader
}

// serveScript listens on a unix socket and runs script on the first
// connection. It returns the socket path and a channel that closes when
// the script ends.
func serveScript(t *testing.T, script func(h *scriptedHolder)) (string, <-chan struct{}) {
	t.Helper()
	dir, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		t.Cleanup(func() { _ = nc.Close() })
		script(&scriptedHolder{t: t, nc: nc, read: attach.NewFrameReader(nc)})
	}()
	return path, ended
}

// expect reads the next request and fails the test unless it has type
// messageType.
func (h *scriptedHolder) expect(messageType string) attach.Frame {
	h.t.Helper()
	f, err := h.read.ReadFrame()
	if err != nil {
		h.t.Errorf("reading %s: %v", messageType, err)
		return attach.Frame{}
	}
	if f.Type != messageType {
		h.t.Errorf("request type = %s, want %s", f.Type, messageType)
	}
	return f
}

func (h *scriptedHolder) reply(request attach.Frame, data any) {
	h.t.Helper()
	f, err := attach.NewResponse(request.ID, request.Type, data)
	if err != nil {
		h.t.Errorf("building the response: %v", err)
		return
	}
	// A write fails only after the test closed the connection, and a frame
	// that never arrived fails the test's own assertions.
	_ = attach.WriteFrame(h.nc, f)
}

func (h *scriptedHolder) notice(messageType string, data any) {
	h.t.Helper()
	f, err := attach.NewNotice(messageType, data)
	if err != nil {
		h.t.Errorf("building the notice: %v", err)
		return
	}
	_ = attach.WriteFrame(h.nc, f)
}

func (h *scriptedHolder) step(seq int64) {
	h.t.Helper()
	h.notice(attach.NoticeStep, attach.StepNotice{Line: attach.StepLine{
		SchemaVersion: 1, RunID: runA, Seq: seq, StepIndex: int(seq), Command: "tap", Phase: "started", Artifacts: []string{},
	}})
}

// prime answers the requests DialRemote makes before it returns.
func (h *scriptedHolder) prime(pid int, startedAt int64) {
	h.t.Helper()
	h.reply(h.expect(attach.TypeHello), attach.HelloResult{Protocol: attach.ProtocolMajor, Features: []string{attach.FeatureObserve}})
	h.reply(h.expect(attach.TypeStatus), attach.Status{
		State:  "running",
		Device: attach.DeviceInfo{ID: "A", Platform: "android"},
		Holder: attach.HolderInfo{PID: pid, Command: "atomyx run", StartedAt: startedAt},
	})
	h.reply(h.expect(attach.TypeSubscribe), nil)
	h.reply(h.expect(attach.TypeTreeLatest), attach.TreeResult{})
	h.reply(h.expect(attach.TypeScreenshotLatest), attach.ScreenshotResult{})
}

// stepRevisionsUntil reads the feed until a step observation reaches
// last, and returns the revisions of every step observation it read.
func stepRevisionsUntil(t *testing.T, feed <-chan session.Observation, last uint64) []uint64 {
	t.Helper()
	var revisions []uint64
	for {
		o := awaitObservation(t, feed, session.ObservedStep, 0)
		revisions = append(revisions, o.Revision)
		if o.Revision >= last {
			return revisions
		}
	}
}

func assertStrictlyIncreasing(t *testing.T, revisions []uint64) {
	t.Helper()
	for i := 1; i < len(revisions); i++ {
		if revisions[i] <= revisions[i-1] {
			t.Fatalf("step revisions = %v, want strictly increasing with no repeat", revisions)
		}
	}
}

func TestRemoteHandle_ASameConnectionResubscribeKeepsEveryLineOnceAndInOrder(t *testing.T) {
	const noticeBuffer = 1
	path, _ := serveScript(t, func(h *scriptedHolder) {
		h.prime(7, 1000)
		h.notice(attach.NoticeTree, attach.TreeNotice{Revision: 1, Epoch: 1})
		pull := h.expect(attach.TypeTreeLatest)
		// The consumer is parked on the pull, so the client's notice buffer
		// fills and drops the later lines. Whether line 1 or line 2 is the
		// last one it stored depends on goroutine timing, so the script
		// resumes from whatever the viewer asks for.
		for seq := int64(1); seq <= 4; seq++ {
			h.step(seq)
		}
		h.reply(pull, attach.TreeResult{Revision: 1, Epoch: 1})

		// A burst can overflow the small buffer again, so the holder answers
		// every resubscribe until the viewer holds all five lines. Whether
		// the viewer stored line 1 or line 2 before the first drop depends
		// on goroutine timing, so each resume starts from what it asks for.
		staleSent := false
		for {
			f, err := h.read.ReadFrame()
			if err != nil {
				return
			}
			if f.Type != attach.TypeSubscribe {
				if f.Type == attach.TypeTreeLatest {
					h.reply(f, attach.TreeResult{Revision: 1, Epoch: 1})
				} else {
					h.reply(f, attach.ScreenshotResult{})
				}
				continue
			}
			var request attach.Subscribe
			if err := f.Decode(&request); err != nil || request.RunID != runA {
				t.Errorf("resubscribe = %+v, %v, want to resume in %s", request, err, runA)
				return
			}
			if !staleSent {
				// A notice of the replaced stream that was already on the
				// wire. The lines before it were dropped, so storing it
				// would skip them.
				staleSent = true
				h.step(5)
			}
			h.reply(f, nil)
			for seq := request.SinceSeq + 1; seq <= 5; seq++ {
				h.step(seq)
			}
		}
	})
	h := dialRemoteWith(t, session.RemoteConfig{Path: path, Client: "viewer", NoticeBuffer: noticeBuffer})
	feed, err := h.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}

	revisions := stepRevisionsUntil(t, feed, 5)

	assertStrictlyIncreasing(t, revisions)
	lines, gap, err := h.StepsSince("", 0)
	if err != nil || gap || len(lines) != 5 {
		t.Fatalf("StepsSince() = %d lines, gap %t, %v, want lines 1 to 5", len(lines), gap, err)
	}
	for i, line := range lines {
		if line.Seq != int64(i+1) {
			t.Fatalf("line %d has seq %d, want %d", i, line.Seq, i+1)
		}
	}
}

func TestRemoteHandle_DropsALineItHoldsAlready(t *testing.T) {
	released := make(chan struct{})
	path, ended := serveScript(t, func(h *scriptedHolder) {
		h.prime(7, 1000)
		<-released
		for _, seq := range []int64{1, 2, 2, 1, 3} {
			h.step(seq)
		}
	})
	h := dialRemote(t, path)
	feed, err := h.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	close(released)

	revisions := stepRevisionsUntil(t, feed, 3)
	<-ended

	if len(revisions) != 3 {
		t.Fatalf("step revisions = %v, want one observation for each of lines 1, 2 and 3", revisions)
	}
	assertStrictlyIncreasing(t, revisions)
	lines, _, err := h.StepsSince("", 0)
	if err != nil || len(lines) != 3 {
		t.Fatalf("StepsSince() = %d lines, %v, want 3", len(lines), err)
	}
}
