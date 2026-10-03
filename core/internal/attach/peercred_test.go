package attach

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
)

func connectedPair(t *testing.T) (server, client *net.UnixConn) {
	t.Helper()
	path := shortSocketPath(t)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	dialed, err := (&net.Dialer{}).DialContext(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dialed.Close() })
	accepted, err := ln.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accepted.Close() })
	return accepted, dialed.(*net.UnixConn)
}

func TestPlatformPeerUIDReportsTheCurrentUser(t *testing.T) {
	server, client := connectedPair(t)
	for name, c := range map[string]*net.UnixConn{"server side": server, "client side": client} {
		got, err := platformPeerUID(c)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := uint32(os.Getuid()); got != want {
			t.Fatalf("%s: uid = %d, want %d", name, got, want)
		}
	}
}

func TestVerifyPeerAcceptsTheCurrentUserByDefault(t *testing.T) {
	server, _ := connectedPair(t)
	if err := VerifyPeer(server, nil); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPeerRefusesAnotherUID(t *testing.T) {
	server, _ := connectedPair(t)
	other := uint32(os.Getuid()) + 1
	err := VerifyPeer(server, func(*net.UnixConn) (uint32, error) { return other, nil })
	var uidErr *PeerUIDError
	if !errors.As(err, &uidErr) || uidErr.Got != other || uidErr.Want != uint32(os.Getuid()) {
		t.Fatalf("error = %v, want a PeerUIDError for uid %d", err, other)
	}
}

func TestVerifyPeerReportsAFailedCredentialRead(t *testing.T) {
	server, _ := connectedPair(t)
	cause := errors.New("no credentials")
	err := VerifyPeer(server, func(*net.UnixConn) (uint32, error) { return 0, cause })
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want the credential error", err)
	}
}
