package sessiontest

import (
	"context"
	"errors"
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
	ended bool
}

// DialViewer connects to the holder socket at path under the client name
// and returns once the holder has seen it attach, because the first
// observe request is answered only after the attach callback ran. The connection closes
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
	if err := v.TreeLatest(); err != nil {
		t.Fatalf("holder tree: %v", err)
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
	defer func() {
		v.mu.Lock()
		v.ended = true
		v.mu.Unlock()
		v.cond.Broadcast()
	}()
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

// ErrViewerEnded is returned by WaitForLines when the connection to the
// holder ended before enough step lines arrived.
var ErrViewerEnded = errors.New("the holder connection ended before the expected step lines arrived")

// WaitForLines blocks until the viewer has collected n step lines. It
// returns ErrViewerEnded when the connection ends first.
func (v *Viewer) WaitForLines(n int) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	for len(v.lines) < n {
		if v.ended {
			return ErrViewerEnded
		}
		v.cond.Wait()
	}
	return nil
}

// Lines returns the step lines collected so far, oldest first.
func (v *Viewer) Lines() []StepLine {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]StepLine(nil), v.lines...)
}

// RequestYield asks the holder at path to give its device to the process
// described by command, as a borrower does, and returns once the holder
// has ended the connection. It returns the error the holder answered, which
// carries its stable code, for example when it is busy.
func RequestYield(ctx context.Context, path, client, command string) error {
	_, err := attach.RequestYield(ctx, attach.ClientConfig{Path: path, Client: client}, command)
	return err
}
