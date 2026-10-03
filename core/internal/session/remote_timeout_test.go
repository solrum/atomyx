package session_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/session"
)

// shortTimeout is a bound small enough to keep the test fast and large
// enough that a healthy local exchange never reaches it.
const shortTimeout = 50 * time.Millisecond

// mutePeer listens on a unix socket and accepts every connection. It
// answers the hello request when answerHello is set and nothing else, so
// a caller that waits for any other reply waits forever.
func mutePeer(t *testing.T, answerHello bool) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "m")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = nc.Close() })
			if answerHello {
				go answerHelloOnly(nc)
			}
		}
	}()
	return path
}

func answerHelloOnly(nc net.Conn) {
	f, err := attach.NewFrameReader(nc).ReadFrame()
	if err != nil {
		return
	}
	reply, err := attach.NewResponse(f.ID, attach.TypeHello, attach.HelloResult{Protocol: attach.ProtocolMajor, Features: []string{}})
	if err != nil {
		return
	}
	_ = attach.WriteFrame(nc, reply)
}

// within fails the test when fn does not return inside remoteGuard.
func within(t *testing.T, fn func()) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		fn()
	}()
	select {
	case <-finished:
	case <-time.After(remoteGuard):
		t.Fatalf("the call did not return within %s", remoteGuard)
	}
}

func TestDialRemote_FailsWithATimeoutWhenTheHolderNeverAnswersHello(t *testing.T) {
	path := mutePeer(t, false)
	var err error
	within(t, func() {
		_, err = session.DialRemote(context.Background(), session.RemoteConfig{Path: path, Client: "viewer", HelloTimeout: shortTimeout}, androidDesc("A"))
	})

	var timeout *attach.TimeoutError
	if !errors.As(err, &timeout) || timeout.Stage != "hello" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DialRemote() error = %v, want a hello timeout", err)
	}
}

func TestDialRemote_FailsWithATimeoutWhenTheHolderNeverAnswersTheFirstReads(t *testing.T) {
	path := mutePeer(t, true)
	var err error
	within(t, func() {
		_, err = session.DialRemote(context.Background(), session.RemoteConfig{Path: path, Client: "viewer", PrimeTimeout: shortTimeout}, androidDesc("A"))
	})

	var timeout *attach.TimeoutError
	if !errors.As(err, &timeout) || timeout.Stage != "prime" {
		t.Fatalf("DialRemote() error = %v, want a prime timeout", err)
	}
}

func TestAttach_ReportsTheHolderUnreachableWhenItAcceptsAndNeverAnswers(t *testing.T) {
	path := mutePeer(t, false)
	prober := &scriptedProber{script: []lease.Status{heldStatus(lease.Holder{PID: 7, Command: "atomyx run", Socket: path})}}
	dial := func(ctx context.Context, cfg session.RemoteConfig, desc device.Descriptor) (*session.RemoteHandle, error) {
		cfg.HelloTimeout = shortTimeout
		return session.DialRemote(ctx, cfg, desc)
	}
	var err error
	within(t, func() {
		_, err = session.Attach(context.Background(), session.AttachDeps{Leases: prober, Clock: &recordingClock{}, Dial: dial}, androidDesc("A"))
	})

	var unreachable *session.HolderUnreachableError
	var timeout *attach.TimeoutError
	if !errors.As(err, &unreachable) || !errors.As(err, &timeout) {
		t.Fatalf("Attach() error = %v, want an unreachable holder caused by a timeout", err)
	}
}
