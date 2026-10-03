package attach

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"dev.atomyx/internal/diag"
)

const (
	// DefaultMaxConnections is how many viewers a server serves at once.
	DefaultMaxConnections = 8
	// DefaultHelloTimeout is how long a connection may take to say hello.
	DefaultHelloTimeout = 5 * time.Second
	// DefaultWriteTimeout is how long one write may block before the
	// viewer is disconnected.
	DefaultWriteTimeout = 5 * time.Second
	// DefaultQueueSize is how many encoded frames wait for a viewer's
	// writer before the server disconnects it for reading too slowly.
	DefaultQueueSize = 64
	// DefaultQueueBytes is how many bytes of encoded frames may wait for a
	// viewer's writer before the server disconnects it. It holds two
	// maximum-size frames, so one response in flight always fits.
	DefaultQueueBytes = 2 * MaxFrameBytes

	socketMode = 0o600
)

// ServerConfig configures Listen. Zero values select the defaults.
type ServerConfig struct {
	// Path is the unix socket path, at most 104 bytes on macOS.
	Path     string
	Handlers Handlers
	// PeerUID reads the uid of a connecting peer. The platform lookup is
	// the default.
	PeerUID        PeerCredFunc
	MaxConnections int
	HelloTimeout   time.Duration
	WriteTimeout   time.Duration
	QueueSize      int
	QueueBytes     int64

	hooks testHooks
}

// testHooks let a test hold the writer or observe the read loop. They
// are nil outside tests.
type testHooks struct {
	// beforeWrite runs in the writer before each queued frame is written.
	beforeWrite func()
	// afterDispatch runs in the read loop after each request is answered.
	afterDispatch func()
}

func (c ServerConfig) withDefaults() ServerConfig {
	if c.MaxConnections <= 0 {
		c.MaxConnections = DefaultMaxConnections
	}
	if c.HelloTimeout <= 0 {
		c.HelloTimeout = DefaultHelloTimeout
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = DefaultWriteTimeout
	}
	if c.QueueSize <= 0 {
		c.QueueSize = DefaultQueueSize
	}
	if c.QueueBytes <= 0 {
		c.QueueBytes = DefaultQueueBytes
	}
	return c
}

// Server accepts viewers on a unix socket and serves them from the
// handlers its owner supplied.
type Server struct {
	cfg ServerConfig
	now func() time.Time
	ln  *net.UnixListener

	ctx    context.Context
	cancel context.CancelFunc

	nextConnID atomic.Uint64

	mu     sync.Mutex
	conns  map[*conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// Listen binds cfg.Path, restricts it to its owner, and starts accepting.
// The socket file is not removed by Close, so the owner that created the
// directory decides when it goes.
func Listen(cfg ServerConfig) (*Server, error) {
	return listen(cfg, time.Now)
}

func listen(cfg ServerConfig, now func() time.Time) (*Server, error) {
	cfg = cfg.withDefaults()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: cfg.Path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("attach: listening on %s: %w", cfg.Path, err)
	}
	ln.SetUnlinkOnClose(false)
	if err := os.Chmod(cfg.Path, socketMode); err != nil {
		_ = ln.Close()
		_ = os.Remove(cfg.Path)
		return nil, fmt.Errorf("attach: restricting %s: %w", cfg.Path, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cfg: cfg, now: now, ln: ln, ctx: ctx, cancel: cancel, conns: map[*conn]struct{}{}}
	s.wg.Add(1)
	go s.acceptLoop()
	return s, nil
}

// Path is the socket path the server listens on.
func (s *Server) Path() string { return s.cfg.Path }

// Close stops accepting, closes every connection, and returns once every
// goroutine of the server has ended. It is safe to call more than once.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	open := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		open = append(open, c)
	}
	s.mu.Unlock()

	err := s.ln.Close()
	s.cancel()
	for _, c := range open {
		c.close()
	}
	s.wg.Wait()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		nc, err := s.ln.AcceptUnix()
		if err != nil {
			return
		}
		s.admit(nc)
	}
}

func (s *Server) admit(nc *net.UnixConn) {
	if err := VerifyPeer(nc, s.cfg.PeerUID); err != nil {
		_ = nc.Close()
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = nc.Close()
		return
	}
	if len(s.conns) >= s.cfg.MaxConnections {
		s.mu.Unlock()
		s.refuse(nc)
		return
	}
	c := newConn(s, nc)
	s.conns[c] = struct{}{}
	s.wg.Add(2)
	s.mu.Unlock()
	go c.writeLoop()
	go c.run()
}

// refuse tells a viewer over the cap why it is turned away. The frame has
// no id because no request was read, so the client reads it as the end of
// the connection.
func (s *Server) refuse(nc *net.UnixConn) {
	defer func() { _ = nc.Close() }()
	frame := NewErrorResponse(0, TypeHello, diag.NewHolderBusy(
		fmt.Sprintf("%d viewers are already attached", s.cfg.MaxConnections)))
	_ = nc.SetWriteDeadline(s.now().Add(s.cfg.WriteTimeout))
	_ = WriteFrame(nc, frame)
}

func (s *Server) forget(c *conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}
