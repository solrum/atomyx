package attach

import "sync"

// noticeInbox holds the notices a client has read and not yet delivered.
// It keeps arrival order, keeps only the newest tree notice and the newest
// screenshot notice, and bounds the step notices.
type noticeInbox struct {
	maxSteps int
	// wake carries one pending signal that the inbox changed.
	wake chan struct{}

	mu        sync.Mutex
	queue     []Frame
	steps     int
	gapQueued bool
	ended     bool
}

func newNoticeInbox(maxSteps int) *noticeInbox {
	return &noticeInbox{maxSteps: maxSteps, wake: make(chan struct{}, 1)}
}

// push adds a notice. A step notice past the bound is dropped, and one
// NoticeStepsDropped marker takes its place in the order until the
// consumer has received it.
func (b *noticeInbox) push(f Frame) {
	b.mu.Lock()
	switch f.Type {
	case NoticeTree, NoticeScreenshot:
		b.removeType(f.Type)
		b.queue = append(b.queue, f)
	case NoticeStep:
		if b.steps >= b.maxSteps {
			if !b.gapQueued {
				b.gapQueued = true
				b.queue = append(b.queue, Frame{V: ProtocolMajor, Type: NoticeStepsDropped})
			}
			break
		}
		b.steps++
		b.queue = append(b.queue, f)
	default:
		b.queue = append(b.queue, f)
	}
	b.mu.Unlock()
	b.signal()
}

func (b *noticeInbox) removeType(messageType string) {
	for i, held := range b.queue {
		if held.Type == messageType {
			b.queue = append(b.queue[:i], b.queue[i+1:]...)
			return
		}
	}
}

// next removes the oldest notice. ended reports that the connection is
// over, which with an empty inbox means nothing more will come.
func (b *noticeInbox) next() (f Frame, ok, ended bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.queue) == 0 {
		return Frame{}, false, b.ended
	}
	f = b.queue[0]
	b.queue = b.queue[1:]
	switch f.Type {
	case NoticeStep:
		b.steps--
	case NoticeStepsDropped:
		b.gapQueued = false
	}
	return f, true, false
}

func (b *noticeInbox) finish() {
	b.mu.Lock()
	b.ended = true
	b.mu.Unlock()
	b.signal()
}

func (b *noticeInbox) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
