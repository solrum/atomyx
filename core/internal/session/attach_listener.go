package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/logging"
)

// attachListener is a bound attach server together with the identity of
// the socket file it created.
type attachListener struct {
	srv  *attach.Server
	path string
	file os.FileInfo
}

// listenAttach binds an attach server at path. The caller holds the
// device lease, so any file already at path belongs to a holder that is
// gone and is removed first.
func listenAttach(path string, handlers attach.Handlers) (*attachListener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("remove a stale socket: %w", err)
	}
	srv, err := attach.Listen(attach.ServerConfig{Path: path, Handlers: handlers})
	if err != nil {
		return nil, err
	}
	file, err := os.Lstat(path)
	if err != nil {
		_ = srv.Close()
		return nil, fmt.Errorf("read the socket file: %w", err)
	}
	return &attachListener{srv: srv, path: path, file: file}, nil
}

// stop closes the server and every connection, then removes the socket
// file only while the path still names the file this listener created.
// After a lost lease another holder may have bound the same path.
func (l *attachListener) stop() {
	_ = l.srv.Close()
	current, err := os.Lstat(l.path)
	if err == nil && os.SameFile(current, l.file) {
		_ = os.Remove(l.path)
	}
}

// bindAttach starts serving viewers at path and records the socket in the
// lease metadata. A host that cannot bind keeps driving the device
// without a socket, so viewers cannot attach to it.
func (s *Session) bindAttach(path string) {
	l, err := listenAttach(path, attach.Handlers{})
	if err != nil {
		s.deps.Logger.Warn("attach.socket_unavailable", logging.LogContext{"id": s.desc.ID, "error": err.Error()})
		return
	}
	s.socket = path
	s.mu.Lock()
	done := s.attachDone
	if !done {
		s.attach = l
	}
	s.mu.Unlock()
	if done {
		l.stop()
	}
}

// stopAttach ends the attach server and removes its socket. It is safe to
// call from several goroutines, and every call returns after the first
// one has finished stopping.
func (s *Session) stopAttach() {
	s.attachOnce.Do(func() {
		s.mu.Lock()
		s.attachDone = true
		l := s.attach
		s.attach = nil
		s.mu.Unlock()
		if l != nil {
			l.stop()
		}
	})
}
