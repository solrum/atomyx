package iosdriver_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

const busyTestProbeTimeout = 80 * time.Millisecond

// startHeldListener starts a listener that keeps its first silentFirst
// connections open without replying, like a runner serving another
// client, and answers every later connection as an idle runner. A
// negative silentFirst never answers. reply, when set, replaces the
// pong with a raw line. It returns the port and the connection count, which includes the
// reachability dial that precedes the first probe.
func startHeldListener(t *testing.T, silentFirst int, reply string) (int, *atomic.Int32) {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var count atomic.Int32
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			n := int(count.Add(1))
			if silentFirst < 0 || n <= silentFirst {
				mu.Lock()
				held = append(held, c)
				mu.Unlock()
				continue
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				line, err := bufio.NewReader(c).ReadString('\n')
				if err != nil {
					return
				}
				var req struct {
					ID int64 `json:"id"`
				}
				_ = json.Unmarshal([]byte(line), &req)
				if reply != "" {
					_, _ = fmt.Fprint(c, reply)
					return
				}
				_, _ = fmt.Fprint(c, pongReply(req.ID, defaultIdentityFields))
			}(c)
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})
	return l.Addr().(*net.TCPAddr).Port, &count
}

func startTunnelOn(t *testing.T, port int) (*process.FakeBackground, error) {
	t.Helper()
	t.Cleanup(iosdriver.SetTunnelPingTimeout(busyTestProbeTimeout))
	bg := process.NewFakeBackground()
	_, _, err := iosdriver.StartTunnel(context.Background(), bg, &fakeClock{}, iosdriver.TunnelConfig{
		UDID: "DEV-1", HostPort: port, Reuse: func() (int, bool) { return 0, true },
	})
	return bg, err
}

func TestStartTunnel_IdleRunnerAnswersOnFirstProbe(t *testing.T) {
	port, count := startHeldListener(t, 0, "")

	bg, err := startTunnelOn(t, port)

	if err != nil {
		t.Fatalf("StartTunnel() error = %v, want the idle runner reused", err)
	}
	if got := count.Load() - 1; got != 1 {
		t.Fatalf("probes = %d, want 1", got)
	}
	if len(bg.Invocations()) != 0 {
		t.Fatalf("background starts = %+v, want none", bg.Invocations())
	}
}

func TestStartTunnel_BusyRunnerThatFreesWithinBudgetIsReused(t *testing.T) {
	port, count := startHeldListener(t, 2, "")

	_, err := startTunnelOn(t, port)

	if err != nil {
		t.Fatalf("StartTunnel() error = %v, want success once the runner frees", err)
	}
	if got := count.Load() - 1; got != 2 {
		t.Fatalf("probes = %d, want 2", got)
	}
}

func TestStartTunnel_RunnerBusyPastBudgetReportsBusy(t *testing.T) {
	port, count := startHeldListener(t, -1, "")

	bg, err := startTunnelOn(t, port)

	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) || iproxyErr.Code != iosdriver.IproxyErrRunnerBusy {
		t.Fatalf("StartTunnel() error = %v, want an IproxyErrRunnerBusy IproxyError", err)
	}
	if got := count.Load() - 1; got != 3 {
		t.Fatalf("probes = %d, want the full budget of 3", got)
	}
	if len(bg.Invocations()) != 0 {
		t.Fatalf("background starts = %+v, want no iproxy spawn next to a busy runner", bg.Invocations())
	}
}

func TestStartTunnel_ForeignListenerKeepsBindConflict(t *testing.T) {
	port, count := startHeldListener(t, 0, "HTTP/1.1 400 Bad Request\r\n\r\n")

	_, err := startTunnelOn(t, port)

	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) || iproxyErr.Code != iosdriver.IproxyErrBindConflict {
		t.Fatalf("StartTunnel() error = %v, want an IproxyErrBindConflict IproxyError", err)
	}
	if got := count.Load() - 1; got != 1 {
		t.Fatalf("probes = %d, want 1 because a foreign answer is not retried", got)
	}
}

func TestIosDriver_Connect_Device_BusyRunnerReportsNotReadyWithRemedy(t *testing.T) {
	port, _ := startHeldListener(t, -1, "")
	t.Cleanup(iosdriver.SetTunnelPingTimeout(busyTestProbeTimeout))
	d := iosdriver.New(newDefaultFakeConn(), process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Clock: &fakeClock{}, Background: process.NewFakeBackground(),
	})

	err := d.Connect(context.Background())

	code, message, remedy := diag.Describe(err)
	if code != diag.CodeAgentNotReady {
		t.Fatalf("code = %q, want %q", code, diag.CodeAgentNotReady)
	}
	if message != "the agent on device DEV-1 did not answer, it may be busy with another client or not be an Atomyx runner" {
		t.Fatalf("message = %q", message)
	}
	if remedy == "" {
		t.Fatal("remedy is empty")
	}
}

func TestStartTunnel_SilentListenerOnFreshPortReportsBindConflict(t *testing.T) {
	port, _ := startHeldListener(t, -1, "")
	t.Cleanup(iosdriver.SetTunnelPingTimeout(busyTestProbeTimeout))

	_, _, err := iosdriver.StartTunnel(context.Background(), process.NewFakeBackground(), &fakeClock{}, iosdriver.TunnelConfig{
		UDID: "DEV-1", HostPort: port, FreshPort: true,
	})

	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) || iproxyErr.Code != iosdriver.IproxyErrBindConflict {
		t.Fatalf("StartTunnel() error = %v, want an IproxyErrBindConflict IproxyError so the launch retries on a new port", err)
	}
}

func TestStartTunnel_BusyMessageDoesNotAssumeARunner(t *testing.T) {
	port, _ := startHeldListener(t, -1, "")

	_, err := startTunnelOn(t, port)

	want := fmt.Sprintf("the listener on 127.0.0.1:%d did not answer, it may be busy with another client or not be an Atomyx runner", port)
	if err == nil || err.Error() != want {
		t.Fatalf("StartTunnel() error = %v, want %q", err, want)
	}
}

// cancellingClock cancels the supplied context on its first Sleep.
type cancellingClock struct {
	cancel context.CancelFunc
}

func (c *cancellingClock) NowMs() int64 { return 0 }

func (c *cancellingClock) Sleep(ctx context.Context, _ int64) error {
	c.cancel()
	return ctx.Err()
}

func TestStartTunnel_CancelDuringBusyRetryReturnsContextError(t *testing.T) {
	port, _ := startHeldListener(t, -1, "")
	t.Cleanup(iosdriver.SetTunnelPingTimeout(busyTestProbeTimeout))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, _, err := iosdriver.StartTunnel(ctx, process.NewFakeBackground(), &cancellingClock{cancel: cancel}, iosdriver.TunnelConfig{
		UDID: "DEV-1", HostPort: port, Reuse: func() (int, bool) { return 0, true },
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StartTunnel() error = %v, want context.Canceled", err)
	}
}

func TestStartTunnel_ContextDeadlineDuringProbeReturnsContextError(t *testing.T) {
	port, _ := startHeldListener(t, -1, "")
	t.Cleanup(iosdriver.SetTunnelPingTimeout(busyTestProbeTimeout))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, _, err := iosdriver.StartTunnel(ctx, process.NewFakeBackground(), &fakeClock{}, iosdriver.TunnelConfig{
		UDID: "DEV-1", HostPort: port, Reuse: func() (int, bool) { return 0, true },
	})

	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("StartTunnel() error = %v, want the context error and not a busy or conflict diagnosis", err)
	}
}
