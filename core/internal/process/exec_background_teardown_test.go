package process

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// teardownProbeSource is a standalone Go program compiled once per test
// binary run by buildTeardownProbe. It opens a TCP listener on an
// ephemeral port, forks a grandchild that only blocks forever, prints
// "<port> <grandchildPid>" as a single stdout line, then blocks itself.
// A kill that only signals the direct child, not its whole process
// group, would leave the grandchild running and the port held open, so
// this program gives TestExecBackground_Kill_ReapsGroupAndReleasesPort
// something a naive single-process kill cannot fully stop.
const teardownProbeSource = `package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sleep-child" {
		select {}
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	grandchild := exec.Command(os.Args[0], "sleep-child")
	if err := grandchild.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("%d %d\n", port, grandchild.Process.Pid)
	select {}
}
`

var (
	teardownProbeOnce sync.Once
	teardownProbePath string
	teardownProbeErr  error
)

// buildTeardownProbe compiles teardownProbeSource once per test binary
// process (including every repetition under go test -count) and
// returns the path to the resulting executable, so the compile cost is
// paid once rather than once per test iteration.
func buildTeardownProbe(t *testing.T) string {
	t.Helper()
	teardownProbeOnce.Do(func() {
		dir, err := os.MkdirTemp("", "atomyx-teardown-probe-*")
		if err != nil {
			teardownProbeErr = err
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(teardownProbeSource), 0o600); err != nil {
			teardownProbeErr = err
			return
		}
		binPath := filepath.Join(dir, "teardown-probe")
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binPath, "main.go")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			teardownProbeErr = fmt.Errorf("build teardown probe: %w\n%s", err, out)
			return
		}
		teardownProbePath = binPath
	})
	if teardownProbeErr != nil {
		t.Fatalf("buildTeardownProbe() error = %v", teardownProbeErr)
	}
	return teardownProbePath
}

// teardownPollInterval and teardownPollTimeout bound the polling loop
// TestExecBackground_Kill_ReapsGroupAndReleasesPort uses to observe
// process exit and port release after Kill, instead of sleeping a
// fixed duration long enough to cover the worst case every time.
const (
	teardownPollInterval = 20 * time.Millisecond
	teardownPollTimeout  = killGracePeriod + 3*time.Second
)

// waitUntil polls condition until it reports true, failing the test if
// teardownPollTimeout elapses first.
func waitUntil(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(teardownPollTimeout)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within %s", what, teardownPollTimeout)
		}
		time.Sleep(teardownPollInterval)
	}
}

// TestExecBackground_Kill_ReapsGroupAndReleasesPort proves that Kill
// tears down an entire spawned process group, not just the direct
// child: the probe process it starts opens a TCP listener and forks a
// grandchild that would survive a naive single-process kill. After
// Kill returns, both the grandchild and the listening port must be
// gone.
func TestExecBackground_Kill_ReapsGroupAndReleasesPort(t *testing.T) {
	probePath := buildTeardownProbe(t)

	r := NewExecBackground()
	proc, err := r.Start(context.Background(), probePath)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	line, err := bufio.NewReader(proc.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatalf("read probe output: %v", err)
	}
	fields := strings.Fields(line)
	if len(fields) != 2 {
		t.Fatalf("probe output = %q, want \"<port> <pid>\"", line)
	}
	port, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("parse port from %q: %v", line, err)
	}
	grandchildPID, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("parse grandchild pid from %q: %v", line, err)
	}

	address := fmt.Sprintf("127.0.0.1:%d", port)
	dialer := &net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(context.Background(), "tcp", address)
	if err != nil {
		t.Fatalf("dial probe listener before Kill: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close dial probe listener connection: %v", err)
	}

	if err := proc.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}

	waitUntil(t, "grandchild process exit", func() bool {
		return syscall.Kill(grandchildPID, 0) != nil
	})
	waitUntil(t, "listening port release", func() bool {
		probeDialer := &net.Dialer{Timeout: 100 * time.Millisecond}
		conn, err := probeDialer.DialContext(context.Background(), "tcp", address)
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	})
}
