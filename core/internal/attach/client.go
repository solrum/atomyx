package attach

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"dev.atomyx/internal/diag"
)

// DefaultNoticeBuffer is how many notices a client holds for a consumer
// before it stops reading the socket.
const DefaultNoticeBuffer = 64

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
	PeerUID      PeerCredFunc
	NoticeBuffer int
}

// Client is one attached viewer's connection to a holder. Its methods are
// safe for concurrent use.
type Client struct {
	nc      *net.UnixConn
	hello   HelloResult
	notices chan Frame
	done    chan struct{}
	// stop is closed by Close so a read loop blocked on a consumer that has
	// stopped reading can leave.
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
// another user, and completes the hello exchange.
func Dial(ctx context.Context, cfg ClientConfig) (*Client, error) {
	nc, err := (&net.Dialer{}).DialContext(ctx, "unix", cfg.Path)
	if err != nil {
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
	buffer := cfg.NoticeBuffer
	if buffer <= 0 {
		buffer = DefaultNoticeBuffer
	}
	c := &Client{
		nc:      uc,
		notices: make(chan Frame, buffer),
		done:    make(chan struct{}),
		stop:    make(chan struct{}),
		pending: map[int64]chan Frame{},
	}
	go c.readLoop()
	features := cfg.Features
	if features == nil {
		features = []string{}
	}
	hello := Hello{Protocol: SupportedVersions, Client: cfg.Client, Features: features}
	if err := c.Call(ctx, TypeHello, hello, &c.hello); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// Protocol is the major version the holder chose.
func (c *Client) Protocol() int { return c.hello.Protocol }

// Features lists what the holder serves.
func (c *Client) Features() []string { return c.hello.Features }

// Notices delivers the notices a subscription starts. The channel closes
// when the connection ends, and the consumer must keep reading it, or the
// holder disconnects the viewer for reading too slowly.
func (c *Client) Notices() <-chan Frame { return c.notices }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err is nil while the connection lives, then a *ConnectionError.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close ends the connection and waits for the read loop to stop. It is
// safe to call more than once.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	c.stopOnce.Do(func() { close(c.stop) })
	_ = c.nc.Close()
	<-c.done
	return nil
}

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
		// A holder that turned the viewer away has closed the socket, so
		// the read loop reports why instead of the failed write.
		select {
		case <-c.done:
			return c.Err()
		case <-ctx.Done():
			return err
		}
	}
	select {
	case response := <-reply:
		if response.Ok == nil || !*response.Ok {
			return remoteError(response)
		}
		if result == nil {
			return nil
		}
		return response.Decode(result)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
}

func (c *Client) write(ctx context.Context, f Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	deadline, _ := ctx.Deadline()
	if err := c.nc.SetWriteDeadline(deadline); err != nil {
		return err
	}
	return WriteFrame(c.nc, f)
}

func (c *Client) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func remoteError(f Frame) *RemoteError {
	return &RemoteError{Type: f.Type, ErrCode: f.Code, Message: f.Message, RemedyText: f.Remedy}
}

// readLoop is the only reader and the only sender on notices.
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
			select {
			case c.notices <- f:
			case <-c.stop:
				return
			}
		case KindResponse:
			if f.ID == 0 {
				cause = remoteError(f)
				return
			}
			c.mu.Lock()
			reply := c.pending[f.ID]
			c.mu.Unlock()
			if reply != nil {
				reply <- f
			}
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
	close(c.notices)
	close(c.done)
}
