package attach

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"dev.atomyx/internal/diag"
)

// DefaultNoticeBuffer is how many step notices a client holds for a
// consumer before it drops the newest and reports a gap.
const DefaultNoticeBuffer = 64

const (
	// DefaultDialTimeout bounds the connect of a Dial. A holder whose
	// accept queue is full never completes it.
	DefaultDialTimeout = 5 * time.Second
	// DefaultClientHelloTimeout bounds the hello exchange of a Dial. A process
	// that accepts the connection and never answers would otherwise hold
	// the caller forever.
	DefaultClientHelloTimeout = 5 * time.Second
)

// NoticeStepsDropped is a notice the client itself produces, never one a
// holder sends. It marks the place in the notice stream where step
// notices were dropped because the consumer fell behind, so the consumer
// resubscribes with the run id and the sequence number of the last step
// line it read.
const NoticeStepsDropped = "observe.client.stepsDropped"

// NoticeSubscribed is a notice the client itself produces, never one a
// holder sends. It is queued in order each time the holder accepts an
// observe.subscribe request, so every notice before it belongs to the
// subscription the request replaced and every notice after it to the new
// one. A consumer that resubscribes on a live connection ignores the
// notices that precede the marker.
const NoticeSubscribed = "observe.client.subscribed"

// ErrClientClosed is the cause of a ConnectionError after the caller
// closed the client itself.
var ErrClientClosed = errors.New("attach: client closed")

// RemoteError is a failure response from the holder. It carries the
// stable code, so diag.Describe reports it like any local failure.
type RemoteError struct {
	Type, ErrCode, Message, RemedyText string
}

func (e *RemoteError) Error() string  { return e.Message }
func (e *RemoteError) Code() string   { return e.ErrCode }
func (e *RemoteError) Remedy() string { return e.RemedyText }

var _ diag.Coded = (*RemoteError)(nil)

// TimeoutError reports a Dial stage that did not finish in time. It
// unwraps to context.DeadlineExceeded.
type TimeoutError struct {
	// Stage names what ran out: "dial", "hello", or "prime" when a caller
	// above Dial reports its own stage.
	Stage string
	Path  string
	After time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("attach: %s of %s did not finish within %s", e.Stage, e.Path, e.After)
}

func (e *TimeoutError) Unwrap() error { return context.DeadlineExceeded }

// ConnectionError reports that the connection ended. Cause is a
// *RemoteError when the holder turned the viewer away, ErrClientClosed
// after Close, and otherwise the read error that ended the stream, such
// as io.EOF when the holder exited.
type ConnectionError struct{ Cause error }

func (e *ConnectionError) Error() string { return "attach: connection ended: " + e.Cause.Error() }
func (e *ConnectionError) Unwrap() error { return e.Cause }

// ClientConfig configures Dial.
type ClientConfig struct {
	// Path is the holder's socket path.
	Path string
	// Client names the viewer in the hello request.
	Client string
	// Features lists the feature names the viewer wants.
	Features []string
	// PeerUID reads the uid of the holder. The platform lookup is the
	// default.
	PeerUID PeerCredFunc
	// NoticeBuffer bounds the step notices held for the consumer. Tree
	// and screenshot notices never queue, because only the newest of each
	// matters.
	NoticeBuffer int
	// DialTimeout bounds the connect. Zero selects DefaultDialTimeout.
	DialTimeout time.Duration
	// HelloTimeout bounds the hello exchange. Zero selects
	// DefaultClientHelloTimeout.
	HelloTimeout time.Duration
}

// Client is one attached viewer's connection to a holder. Its methods are
// safe for concurrent use.
type Client struct {
	nc      net.Conn
	hello   HelloResult
	inbox   *noticeInbox
	notices chan Frame
	done    chan struct{}
	// delivered is closed when the delivery goroutine has ended.
	delivered chan struct{}
	// stop is closed by Close so the delivery goroutine blocked on a
	// consumer that has stopped reading can leave.
	stop     chan struct{}
	stopOnce sync.Once

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan Frame
	err     error
	closing bool
}

// Dial connects to the holder at cfg.Path, refuses a holder that runs as
// another user, and completes the hello exchange. The connect and the
// hello each have their own bound, and one that runs out returns a
// *TimeoutError, so a holder that never answers cannot hold the caller.
func Dial(ctx context.Context, cfg ClientConfig) (*Client, error) {
	dialTimeout := orDefault(cfg.DialTimeout, DefaultDialTimeout)
	helloTimeout := orDefault(cfg.HelloTimeout, DefaultClientHelloTimeout)
	dctx, cancelDial := context.WithTimeout(ctx, dialTimeout)
	defer cancelDial()
	nc, err := (&net.Dialer{}).DialContext(dctx, "unix", cfg.Path)
	if err != nil {
		if ctx.Err() == nil && dctx.Err() != nil {
			return nil, &TimeoutError{Stage: "dial", Path: cfg.Path, After: dialTimeout}
		}
		return nil, fmt.Errorf("attach: dialing %s: %w", cfg.Path, err)
	}
	uc, ok := nc.(*net.UnixConn)
	if !ok {
		_ = nc.Close()
		return nil, fmt.Errorf("attach: %s is not a unix connection", cfg.Path)
	}
	if err := VerifyPeer(uc, cfg.PeerUID); err != nil {
		_ = nc.Close()
		return nil, err
	}
	c := newClient(uc, cfg.NoticeBuffer)
	features := cfg.Features
	if features == nil {
		features = []string{}
	}
	hello := Hello{Protocol: SupportedVersions, Client: cfg.Client, Features: features}
	hctx, cancelHello := context.WithTimeout(ctx, helloTimeout)
	defer cancelHello()
	if err := c.Call(hctx, TypeHello, hello, &c.hello); err != nil {
		_ = c.Close()
		if ctx.Err() == nil && hctx.Err() != nil {
			return nil, &TimeoutError{Stage: "hello", Path: cfg.Path, After: helloTimeout}
		}
		return nil, err
	}
	return c, nil
}

func orDefault(d, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}
	return d
}

func newClient(nc net.Conn, noticeBuffer int) *Client {
	if noticeBuffer <= 0 {
		noticeBuffer = DefaultNoticeBuffer
	}
	c := &Client{
		nc:        nc,
		inbox:     newNoticeInbox(noticeBuffer),
		notices:   make(chan Frame),
		done:      make(chan struct{}),
		delivered: make(chan struct{}),
		stop:      make(chan struct{}),
		pending:   map[int64]chan Frame{},
	}
	go c.readLoop()
	go c.deliver()
	return c
}

// Protocol is the major version the holder chose.
func (c *Client) Protocol() int { return c.hello.Protocol }

// Features lists what the holder serves.
func (c *Client) Features() []string { return c.hello.Features }

// Notices delivers the notices a subscription starts, in the order they
// arrived. A consumer that falls behind sees only the newest tree notice,
// the newest screenshot notice and the newest state notice, and step notices past the buffer are
// replaced by one NoticeStepsDropped marker. The channel closes after the
// connection ends and the notices already held are delivered. Reading it
// is never required for calls to complete.
func (c *Client) Notices() <-chan Frame { return c.notices }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err is nil while the connection lives, then a *ConnectionError.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close ends the connection and waits for its goroutines to stop. It is
// safe to call more than once.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	c.stopOnce.Do(func() { close(c.stop) })
	_ = c.nc.Close()
	<-c.done
	<-c.delivered
	return nil
}

// beforeReplyWait is a seam set only by a test, which uses it to hold a
// call until its reply and the end of the connection are both ready, the
// one state in which the select below has two live cases.
var beforeReplyWait func(c *Client)

// Call sends one request and decodes the data of its success response
// into result, which may be nil. A failure response returns a
// *RemoteError, and a connection that ended returns the *ConnectionError.
// A request whose frame would pass MaxFrameBytes returns ErrFrameTooLarge
// and the connection stays usable.
func (c *Client) Call(ctx context.Context, messageType string, data, result any) error {
	c.mu.Lock()
	if c.err != nil {
		defer c.mu.Unlock()
		return c.err
	}
	c.nextID++
	id := c.nextID
	reply := make(chan Frame, 1)
	c.pending[id] = reply
	c.mu.Unlock()
	defer c.forget(id)

	request, err := NewRequest(id, messageType, data)
	if err != nil {
		return err
	}
	if err := c.write(ctx, request); err != nil {
		if errors.Is(err, ErrFrameTooLarge) {
			return err
		}
		if ctx.Err() != nil {
			// A write that was cut short may have left half a line on the
			// stream, so the connection cannot carry another request.
			_ = c.nc.Close()
			return ctx.Err()
		}
		// A holder that turned the viewer away has closed the socket, so
		// the read loop reports why instead of the failed write.
		select {
		case <-c.done:
			return c.Err()
		case <-ctx.Done():
			return err
		}
	}
	if beforeReplyWait != nil {
		beforeReplyWait(c)
	}
	select {
	case response := <-reply:
		return decodeResponse(response, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		// The read loop hands a response to its call before it reports the
		// end of the connection, so a holder that replies and then closes
		// must still deliver the reply, whichever case select picked.
		select {
		case response := <-reply:
			return decodeResponse(response, result)
		default:
			return c.Err()
		}
	}
}

func decodeResponse(response Frame, result any) error {
	if response.Ok == nil || !*response.Ok {
		return remoteError(response)
	}
	if result == nil {
		return nil
	}
	return response.Decode(result)
}

// write sends one frame. A context without a deadline still ends the
// write when it is cancelled, by moving the write deadline into the past.
func (c *Client) write(ctx context.Context, f Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	deadline, _ := ctx.Deadline()
	if err := c.nc.SetWriteDeadline(deadline); err != nil {
		return err
	}
	expired := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(expired)
		_ = c.nc.SetWriteDeadline(time.Unix(1, 0))
	})
	err := WriteFrame(c.nc, f)
	if !stop() {
		<-expired
	}
	return err
}

func (c *Client) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func remoteError(f Frame) *RemoteError {
	return &RemoteError{Type: f.Type, ErrCode: f.Code, Message: f.Message, RemedyText: f.Remedy}
}

// readLoop is the only reader. It never waits on the consumer, so a
// response is always read.
func (c *Client) readLoop() {
	reader := NewFrameReader(c.nc)
	var cause error
	defer func() { c.end(cause) }()
	for {
		f, err := reader.ReadFrame()
		if err != nil {
			cause = err
			return
		}
		switch f.Kind() {
		case KindNotice:
			c.inbox.push(f)
		case KindResponse:
			if f.ID == 0 {
				cause = remoteError(f)
				return
			}
			if f.Type == TypeSubscribe && f.Ok != nil && *f.Ok {
				c.inbox.push(Frame{V: ProtocolMajor, Type: NoticeSubscribed})
			}
			c.reply(f)
		}
	}
}

// reply hands a response to its call. A response with an unknown id, or a
// second response to the same id, is dropped.
func (c *Client) reply(f Frame) {
	c.mu.Lock()
	reply := c.pending[f.ID]
	c.mu.Unlock()
	if reply == nil {
		return
	}
	select {
	case reply <- f:
	default:
	}
}

// deliver moves notices from the inbox to the consumer, then closes the
// notices channel once the connection has ended and the inbox is empty.
func (c *Client) deliver() {
	defer close(c.delivered)
	defer close(c.notices)
	for {
		f, ok, ended := c.inbox.next()
		if ok {
			select {
			case c.notices <- f:
			case <-c.stop:
				return
			}
			continue
		}
		if ended {
			return
		}
		select {
		case <-c.inbox.wake:
		case <-c.stop:
			return
		}
	}
}

func (c *Client) end(cause error) {
	c.mu.Lock()
	if c.closing {
		cause = ErrClientClosed
	}
	c.err = &ConnectionError{Cause: cause}
	c.mu.Unlock()
	_ = c.nc.Close()
	close(c.done)
	c.inbox.finish()
}
