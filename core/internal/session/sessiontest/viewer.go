package sessiontest

import (
	"context"
	"sync"
	"testing"

	"dev.atomyx/internal/attach"
)

// StepLine is one step line a holder sends to a viewer.
type StepLine = attach.StepLine

// Viewer is a socket viewer of a holder, for a host's tests that read what
// a holder serves without importing the attach package. Requests return
// the error the holder answered, which carries its stable code.
type Viewer struct {
	client *attach.Client

	mu    sync.Mutex
	cond  *sync.Cond
	lines []StepLine
}

// DialViewer connects to the holder socket at path under the client name
// and returns once the holder has seen it attach, because a served request
// is answered only after the attach callback ran. The connection closes
// when the test ends.
func DialViewer(t testing.TB, path, name string) *Viewer {
	t.Helper()
	client, err := attach.Dial(context.Background(), attach.ClientConfig{Path: path, Client: name})
	if err != nil {
		t.Fatalf("dialing the holder at %s: %v", path, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	v := &Viewer{client: client}
	v.cond = sync.NewCond(&v.mu)
	if err := v.Status(); err != nil {
		t.Fatalf("holder status: %v", err)
	}
	return v
}

// Status asks the holder for its status.
func (v *Viewer) Status() error {
	return v.client.Call(context.Background(), attach.TypeStatus, nil, nil)
}

// TreeLatest reads the holder's latest tree.
func (v *Viewer) TreeLatest() error {
	return v.client.Call(context.Background(), attach.TypeTreeLatest, nil, nil)
}

// ScreenshotLatest reads the holder's latest screenshot.
func (v *Viewer) ScreenshotLatest() error {
	return v.client.Call(context.Background(), attach.TypeScreenshotLatest, nil, nil)
}

// TreeFresh asks the holder to capture a fresh tree and screenshot.
func (v *Viewer) TreeFresh() error {
	return v.client.Call(context.Background(), attach.TypeTreeFresh, nil, &attach.FreshResult{})
}

// FollowSteps subscribes from the oldest step line the holder holds and
// collects every line it sends.
func (v *Viewer) FollowSteps(t testing.TB) {
	t.Helper()
	if err := v.client.Call(context.Background(), attach.TypeSubscribe, attach.Subscribe{}, nil); err != nil {
		t.Fatalf("subscribing: %v", err)
	}
	go v.collect()
}

func (v *Viewer) collect() {
	for f := range v.client.Notices() {
		if f.Type != attach.NoticeStep {
			continue
		}
		var n attach.StepNotice
		if f.Decode(&n) != nil {
			continue
		}
		v.mu.Lock()
		v.lines = append(v.lines, n.Line)
		v.mu.Unlock()
		v.cond.Broadcast()
	}
}

// WaitForLines blocks until the viewer has collected n step lines.
func (v *Viewer) WaitForLines(n int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for len(v.lines) < n {
		v.cond.Wait()
	}
}

// Lines returns the step lines collected so far, oldest first.
func (v *Viewer) Lines() []StepLine {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]StepLine(nil), v.lines...)
}
