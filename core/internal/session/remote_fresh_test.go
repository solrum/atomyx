package session_test

import (
	"context"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/session"
)

func TestRemoteHandle_AFreshCaptureNotifiesEverySubscriberOnce(t *testing.T) {
	const (
		treeRevision       = 7
		screenshotRevision = 3
		nextTreeRevision   = 8
	)
	subscribed := make(chan struct{})
	path, _ := serveScript(t, func(h *scriptedHolder) {
		h.prime(7, 1000)
		<-subscribed
		fresh := h.expect(attach.TypeTreeFresh)
		h.reply(fresh, attach.FreshResult{
			Tree:       attach.TreeResult{Revision: treeRevision, Epoch: 1},
			Screenshot: attach.ScreenshotResult{Revision: screenshotRevision, Epoch: 1},
		})
		// The holder announces the capture it just returned, which the
		// handle already holds, and then a newer one.
		h.notice(attach.NoticeTree, attach.TreeNotice{Revision: treeRevision, Epoch: 1})
		h.reply(h.expect(attach.TypeTreeLatest), attach.TreeResult{Revision: treeRevision, Epoch: 1})
		h.notice(attach.NoticeTree, attach.TreeNotice{Revision: nextTreeRevision, Epoch: 1})
		h.reply(h.expect(attach.TypeTreeLatest), attach.TreeResult{Revision: nextTreeRevision, Epoch: 1})
	})
	h := dialRemote(t, path)
	other, err := h.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	close(subscribed)

	if _, err := h.FreshTree(context.Background()); err != nil {
		t.Fatalf("FreshTree() error = %v", err)
	}

	if o := awaitObservation(t, other, session.ObservedTree, 0); o.Revision != treeRevision {
		t.Fatalf("first tree observation = %+v, want revision %d", o, treeRevision)
	}
	if o := awaitObservation(t, other, session.ObservedScreenshot, 0); o.Revision != screenshotRevision {
		t.Fatalf("screenshot observation = %+v, want revision %d", o, screenshotRevision)
	}
	if o := awaitObservation(t, other, session.ObservedTree, 0); o.Revision != nextTreeRevision {
		t.Fatalf("second tree observation = %+v, want revision %d with no repeat of %d", o, nextTreeRevision, treeRevision)
	}
}
