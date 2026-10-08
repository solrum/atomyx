package attach

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"dev.atomyx/internal/diag"
)

// zeroTime clears a connection deadline.
var zeroTime time.Time

// outbound is one encoded frame waiting for the writer. A nil line means
// close after the frames before it have been written.
type outbound struct {
	line []byte
	// notice marks a frame that holds one of the connection's notice
	// slots, which the writer releases once it takes the frame.
	notice bool
	// fireAfter asks the writer to release the connection's pending
	// after-reply callback once this frame has been written.
	fireAfter bool
}

// conn is one attached viewer. run reads and dispatches requests, and
// writeLoop is the only writer, fed by a bounded queue so answering a
// request never waits on a slow viewer.
type conn struct {
	srv    *Server
	nc     *net.UnixConn
	reader *FrameReader
	out    chan outbound
	// noticeSlots bounds how many pump frames may sit in out, so responses
	// and other frames always find room.
	noticeSlots chan struct{}
	id          uint64
	// client is the name from the accepted hello request.
	client string

	// queuedBytes counts the bytes of frames that are queued or being
	// written, so a viewer that never reads cannot hold more than
	// ServerConfig.QueueBytes of the holder's memory.
	queuedBytes atomic.Int64

	ctx    context.Context
	cancel context.CancelFunc

	closeOnce sync.Once
	// writerDone is closed when the writer has ended, so Server.Close can
	// tell that every frame queued before it was written.
	writerDone chan struct{}
	// announced is set once the viewer handler was told this connection
	// observes. Only the read goroutine touches it.
	announced bool
	// debugging is set once the viewer handler was told this connection
	// sends debug requests.
	debugging bool
	// sub is the connection's live notice subscription. Only the read
	// goroutine touches it.
	sub *liveSubscription
	// afterMu guards after, which the read goroutine sets and the writer
	// or the read goroutine's exit consumes.
	afterMu sync.Mutex
	after   []func()
	// actRunning is set while an act.step request runs on its own goroutine.
	actRunning atomic.Bool
	// draining is set once the last frame is queued, so the writer closes
	// the socket after writing everything before it.
	draining atomic.Bool
}

// liveSubscription is the notice flow one observe.subscribe started.
type liveSubscription struct {
	cancel context.CancelFunc
	// done is closed when the pump of the subscription has ended.
	done chan struct{}
}

func newLiveSubscription(parent context.Context) (context.Context, *liveSubscription) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, &liveSubscription{cancel: cancel, done: make(chan struct{})}
}

// responseReserve is how many queue positions the notice pump leaves
// free. A replay longer than the queue would otherwise keep the queue
// full, and the response to a viewer request sent meanwhile would find no
// room and close the connection. It is capped at half the queue so a small
// queue still carries notices.
const responseReserve = 16

func noticeSlotCount(queueSize int) int {
	return queueSize - min(responseReserve, queueSize/2)
}

func newConn(s *Server, nc *net.UnixConn) *conn {
	ctx, cancel := context.WithCancel(s.ctx)
	return &conn{
		srv:         s,
		nc:          nc,
		id:          s.nextConnID.Add(1),
		reader:      NewFrameReader(nc),
		out:         make(chan outbound, s.cfg.QueueSize),
		noticeSlots: make(chan struct{}, noticeSlotCount(s.cfg.QueueSize)),
		writerDone:  make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (c *conn) close() {
	c.closeOnce.Do(func() {
		c.cancel()
		_ = c.nc.Close()
	})
}

func (c *conn) run() {
	defer c.srv.wg.Done()
	defer func() {
		if !c.draining.Load() {
			c.close()
		}
	}()
	defer c.fireAfter()
	defer c.endSubscription()
	if !c.handshake() {
		return
	}
	defer c.announceDetached()
	c.serve()
}

// writeLoop sends queued frames. A nil entry means close after the frames
// before it have been written.
func (c *conn) writeLoop() {
	defer c.srv.wg.Done()
	defer close(c.writerDone)
	defer c.srv.forget(c)
	defer c.close()
	defer c.fireAfter()
	for {
		select {
		case <-c.ctx.Done():
			return
		case item := <-c.out:
			if item.notice {
				<-c.noticeSlots
			}
			if item.line == nil {
				return
			}
			if hook := c.srv.cfg.hooks.beforeWrite; hook != nil {
				hook()
			}
			if err := c.nc.SetWriteDeadline(c.srv.now().Add(c.srv.cfg.WriteTimeout)); err != nil {
				return
			}
			if _, err := c.nc.Write(item.line); err != nil {
				return
			}
			c.queuedBytes.Add(-int64(len(item.line)))
			if item.fireAfter {
				c.fireAfter()
			}
		}
	}
}

// enqueue hands a line to the writer without waiting. A full queue or an
// exhausted byte budget means the viewer stopped reading, so the
// connection closes.
func (c *conn) enqueue(line []byte) bool {
	return c.enqueueItem(outbound{line: line})
}

func (c *conn) enqueueItem(item outbound) bool {
	size := int64(len(item.line))
	if c.queuedBytes.Add(size) > c.srv.cfg.QueueBytes {
		c.queuedBytes.Add(-size)
		c.close()
		return false
	}
	select {
	case c.out <- item:
		return true
	default:
		c.queuedBytes.Add(-size)
		c.close()
		return false
	}
}

// enqueueWaiting hands a line to the writer and waits for a notice slot,
// so a burst of notices larger than the queue reaches a viewer that is
// reading while the rest of the queue stays free for responses. It gives
// up when ctx or the connection ends. A viewer that stops reading is
// still disconnected, by the byte budget or by the write timeout.
func (c *conn) enqueueWaiting(ctx context.Context, line []byte) bool {
	select {
	case c.noticeSlots <- struct{}{}:
	case <-ctx.Done():
		return false
	case <-c.ctx.Done():
		return false
	}
	if c.enqueueItem(outbound{line: line, notice: true}) {
		return true
	}
	<-c.noticeSlots
	return false
}

func (c *conn) send(f Frame) bool {
	return c.sendItem(f, false)
}

func (c *conn) sendItem(f Frame, fireAfter bool) bool {
	line, err := EncodeFrame(f)
	if errors.Is(err, ErrFrameTooLarge) && f.Kind() == KindResponse {
		line, err = EncodeFrame(NewErrorResponse(f.ID, f.Type, errResponseTooLarge))
	}
	if err != nil {
		c.close()
		return false
	}
	return c.enqueueItem(outbound{line: line, fireAfter: fireAfter})
}

func (c *conn) respond(req Frame, data any) bool {
	f, err := NewResponse(req.ID, req.Type, data)
	if err != nil {
		return c.fail(req, diag.New(diag.CodeUnknown, "the response could not be encoded", "", err))
	}
	return c.send(f)
}

// fireAfter runs the pending after-reply callback once on its own
// goroutine, so the callback may close the server that waits for this
// connection's goroutines.
func (c *conn) fireAfter() {
	c.afterMu.Lock()
	pending := c.after
	c.after = nil
	c.afterMu.Unlock()
	for _, fn := range pending {
		go fn()
	}
}

func (c *conn) fail(req Frame, cause error) bool {
	return c.send(NewErrorResponse(req.ID, req.Type, cause))
}

// flush asks the writer to stop after the frames already queued. It
// reports false when the queue had no room for the end marker, in which
// case the connection is closed at once.
func (c *conn) flush() bool {
	return c.enqueue(nil)
}

// drain queues the end of the stream, after which the writer closes the
// connection.
func (c *conn) drain() {
	c.draining.Store(true)
	c.enqueue(nil)
}

// finish sends a last error that belongs to the connection, not to a
// request, then closes once it is written.
func (c *conn) finish(cause error) {
	if c.send(NewErrorResponse(0, "", cause)) {
		c.drain()
	}
}

// readFrame returns the next frame. It reports false when the connection
// is over, after answering a frame that cannot be parsed.
func (c *conn) readFrame() (Frame, bool) {
	f, err := c.reader.ReadFrame()
	switch {
	case err == nil:
		return f, true
	case errors.Is(err, ErrMalformedFrame), errors.Is(err, ErrFrameTooLarge):
		c.finish(invalidFrame(err.Error()))
	}
	return Frame{}, false
}

// handshake waits for a session.hello that names a shared version. A bad
// first request gets an error and the wait goes on until the deadline.
func (c *conn) handshake() bool {
	if err := c.nc.SetReadDeadline(c.srv.now().Add(c.srv.cfg.HelloTimeout)); err != nil {
		return false
	}
	for {
		f, ok := c.readFrame()
		if !ok {
			return false
		}
		if f.Kind() != KindRequest {
			c.finish(invalidFrame("a connection starts with a session.hello request"))
			return false
		}
		if f.Type != TypeHello {
			c.fail(f, invalidFrame("send session.hello first"))
			continue
		}
		var hello Hello
		if err := f.Decode(&hello); err != nil {
			c.fail(f, invalidFrame(err.Error()))
			continue
		}
		if len(hello.Client) > MaxClientNameBytes {
			c.fail(f, invalidFrame(fmt.Sprintf("the client name is longer than %d bytes", MaxClientNameBytes)))
			continue
		}
		version, err := NegotiateVersion(hello.Protocol, SupportedVersions)
		if err != nil {
			c.fail(f, err)
			continue
		}
		if !c.respond(f, HelloResult{Protocol: version, Features: c.srv.cfg.Handlers.features()}) {
			return false
		}
		c.client = hello.Client
		return c.nc.SetReadDeadline(zeroTime) == nil
	}
}

func (c *conn) serve() {
	for {
		f, ok := c.readFrame()
		if !ok {
			return
		}
		if f.Kind() != KindRequest {
			c.finish(invalidFrame("a viewer sends only requests"))
			return
		}
		if f.Type == TypeActStep {
			ok = c.serveAct(f)
		} else {
			ok = c.dispatch(c.ctx, f)
		}
		if hook := c.srv.cfg.hooks.afterDispatch; hook != nil {
			hook()
		}
		if !ok {
			return
		}
	}
}

// dispatch answers one request and reports whether the connection stays
// open.
func (c *conn) dispatch(ctx context.Context, f Frame) bool {
	switch f.Type {
	case TypeHello:
		return c.fail(f, invalidFrame("session.hello was already accepted"))
	case TypeBye:
		if c.respond(f, nil) {
			c.drain()
		}
		return false
	case TypeStatus:
		if h := c.srv.cfg.Handlers.Session; h != nil {
			return c.respond(f, h.Status())
		}
	case TypeSubscribe, TypeTreeLatest, TypeScreenshotLatest, TypeTreeFresh:
		if h := c.srv.cfg.Handlers.Observe; h != nil {
			c.announceAttached()
			return c.observe(ctx, h, f)
		}
	case TypeControlYield:
		if h := c.srv.cfg.Handlers.Control; h != nil {
			return c.yield(ctx, h, f)
		}
	case TypeDebugBreakpointsSet, TypeDebugPause, TypeDebugResume, TypeDebugStep, TypeDebugVariablesGet, TypeActStep:
		if h := c.srv.cfg.Handlers.Debug; h != nil {
			c.announceDebugging()
			return c.debug(ctx, h, f)
		}
	}
	return c.fail(f, diag.NewUnsupported(f.Type))
}

// announceAttached tells the viewer handler that this connection observes,
// once. A connection that only yields control or asks for status never
// watches the device, so it is not announced.
func (c *conn) announceAttached() {
	h := c.srv.cfg.Handlers.Viewers
	if h == nil || c.announced {
		return
	}
	c.announced = true
	h.ViewerAttached(c.viewerInfo())
}

// announceDebugging tells the viewer handler that this connection sent a
// debug request, once.
func (c *conn) announceDebugging() {
	h := c.srv.cfg.Handlers.Viewers
	if h == nil || c.debugging {
		return
	}
	c.debugging = true
	h.ViewerDebugging(c.viewerInfo())
}

// announceDetached pairs the attach and debugging calls, when they were
// made.
func (c *conn) announceDetached() {
	if c.debugging {
		c.srv.cfg.Handlers.Viewers.ViewerStoppedDebugging(c.viewerInfo())
	}
	if c.announced {
		c.srv.cfg.Handlers.Viewers.ViewerDetached(c.viewerInfo())
	}
}

func (c *conn) viewerInfo() ViewerInfo {
	return ViewerInfo{ID: c.id, Client: c.client}
}
