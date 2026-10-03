package attach

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"dev.atomyx/internal/diag"
)

// zeroTime clears a connection deadline.
var zeroTime time.Time

// outbound is one encoded frame waiting for the writer. A nil line means
// close after the frames before it have been written. A pull frame
// releases the connection's pull slot once it is written.
type outbound struct {
	line []byte
	pull bool
}

// conn is one attached viewer. run reads and dispatches requests, and
// writeLoop is the only writer, fed by a bounded queue so answering a
// request never waits on a slow viewer.
type conn struct {
	srv    *Server
	nc     *net.UnixConn
	reader *FrameReader
	out    chan outbound

	// queuedBytes counts the bytes of frames that are queued or being
	// written, so a viewer that never reads cannot hold more than
	// ServerConfig.QueueBytes of the holder's memory.
	queuedBytes atomic.Int64
	// pullBusy is set while a pull response (a tree or screenshot read) is
	// queued or being written, so a viewer cannot stack large responses.
	pullBusy atomic.Bool

	ctx    context.Context
	cancel context.CancelFunc

	closeOnce  sync.Once
	subscribed bool
	// draining is set once the last frame is queued, so the writer closes
	// the socket after writing everything before it.
	draining atomic.Bool
}

func newConn(s *Server, nc *net.UnixConn) *conn {
	ctx, cancel := context.WithCancel(s.ctx)
	return &conn{
		srv:    s,
		nc:     nc,
		reader: NewFrameReader(nc),
		out:    make(chan outbound, s.cfg.QueueSize),
		ctx:    ctx,
		cancel: cancel,
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
	if !c.handshake() {
		return
	}
	c.serve()
}

// writeLoop sends queued frames. A nil entry means close after the frames
// before it have been written.
func (c *conn) writeLoop() {
	defer c.srv.wg.Done()
	defer c.srv.forget(c)
	defer c.close()
	for {
		select {
		case <-c.ctx.Done():
			return
		case item := <-c.out:
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
			if item.pull {
				c.pullBusy.Store(false)
			}
		}
	}
}

// enqueue hands a line to the writer without waiting. A full queue or an
// exhausted byte budget means the viewer stopped reading, so the
// connection closes.
func (c *conn) enqueue(line []byte, pull bool) bool {
	size := int64(len(line))
	if c.queuedBytes.Add(size) > c.srv.cfg.QueueBytes {
		c.queuedBytes.Add(-size)
		c.close()
		return false
	}
	select {
	case c.out <- outbound{line: line, pull: pull}:
		return true
	default:
		c.queuedBytes.Add(-size)
		c.close()
		return false
	}
}

func (c *conn) send(f Frame) bool {
	return c.sendFrame(f, false)
}

func (c *conn) sendFrame(f Frame, pull bool) bool {
	line, err := EncodeFrame(f)
	if errors.Is(err, ErrFrameTooLarge) && f.Kind() == KindResponse {
		line, err = EncodeFrame(NewErrorResponse(f.ID, f.Type, errResponseTooLarge))
	}
	if err != nil {
		c.close()
		return false
	}
	return c.enqueue(line, pull)
}

func (c *conn) respond(req Frame, data any) bool {
	f, err := NewResponse(req.ID, req.Type, data)
	if err != nil {
		return c.fail(req, diag.New(diag.CodeUnknown, "the response could not be encoded", "", err))
	}
	return c.send(f)
}

func (c *conn) fail(req Frame, cause error) bool {
	return c.send(NewErrorResponse(req.ID, req.Type, cause))
}

// drain queues the end of the stream, after which the writer closes the
// connection.
func (c *conn) drain() {
	c.draining.Store(true)
	c.enqueue(nil, false)
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
		version, err := NegotiateVersion(hello.Protocol, SupportedVersions)
		if err != nil {
			c.fail(f, err)
			continue
		}
		if !c.respond(f, HelloResult{Protocol: version, Features: c.srv.cfg.Handlers.features()}) {
			return false
		}
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
		ok = c.dispatch(f)
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
func (c *conn) dispatch(f Frame) bool {
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
			return c.observe(h, f)
		}
	}
	return c.fail(f, diag.NewUnsupported(f.Type))
}
