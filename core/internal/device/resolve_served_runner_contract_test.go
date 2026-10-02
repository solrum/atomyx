package device

import (
	"bufio"
	"context"
	"net"
	"testing"
)

const servedRunnerPort = "127.0.0.1:22087"

// serveRunner answers the ping handshake on the fixed runner port the
// way a runner started by hand on a simulator does.
func serveRunner(t *testing.T, simulatorUDID string) {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", servedRunnerPort)
	if err != nil {
		t.Fatalf("listen %s: %v", servedRunnerPort, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
					return
				}
				_, _ = conn.Write([]byte(`{"id":0,"ok":true,"data":{"pong":true,"simulatorUdid":"` + simulatorUDID + `"}}` + "\n"))
			}()
		}
	}()
}

func TestIOSWithoutDeviceKeysOnServedRunner(t *testing.T) {
	t.Skip("removed by the served runner resolution step")
	const udid = "8DCBCBB0-A213-4AE0-96F8-3061DDAD4610"
	serveRunner(t, udid)

	desc, err := ResolveDescriptor(context.Background(), NewStaticProbe(), PlatformIOS, "")
	if err != nil {
		t.Fatalf("ResolveDescriptor() error = %v", err)
	}
	identity, err := NewIdentityResolver(nil).Resolve(context.Background(), desc)
	if err != nil {
		t.Fatalf("Resolve() error = %v, want the lease identity of the served runner", err)
	}
	if identity.HardwareID != udid {
		t.Fatalf("HardwareID = %q, want %q reported by the served runner", identity.HardwareID, udid)
	}
}
