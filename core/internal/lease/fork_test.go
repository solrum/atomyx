package lease

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
)

const (
	childDirEnv  = "ATOMYX_LEASE_TEST_DIR"
	childKeyEnv  = "ATOMYX_LEASE_TEST_KEY"
	childReadyOK = "acquired"
)

// TestMain re-executes the test binary as the second lease-holding
// process: when the child environment is set, the binary acquires the
// lease, announces it on stdout, and then blocks until stdin closes.
// It never calls Release, so a clean exit and a SIGKILL both prove the
// kernel frees the flock when the process ends.
func TestMain(m *testing.M) {
	if dir := os.Getenv(childDirEnv); dir != "" {
		os.Exit(runLeaseChild(dir, Key(os.Getenv(childKeyEnv))))
	}
	os.Exit(m.Run())
}

func runLeaseChild(dir string, key Key) int {
	mgr := newManagerAt(dir, clock.SystemClock{})
	h := Holder{PID: os.Getpid(), Command: "forked child", Role: "cli", StartedAt: time.Now().UTC()}
	if _, err := mgr.Acquire(context.Background(), key, h); err != nil {
		fmt.Println("error: " + err.Error())
		return 2
	}
	fmt.Println(childReadyOK)
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

type forkedHolder struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

func startChild(t *testing.T, dir string, key Key) *forkedHolder {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), os.Args[0])
	cmd.Env = append(os.Environ(), childDirEnv+"="+dir, childKeyEnv+"="+string(key))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != childReadyOK {
		t.Fatalf("child did not acquire: %q, %v", line, err)
	}
	return &forkedHolder{cmd: cmd, stdin: stdin}
}

func assertHeldByChild(t *testing.T, m *Manager, key Key, child *forkedHolder) {
	t.Helper()
	_, err := m.Acquire(context.Background(), key, testHolder(os.Getpid(), "parent"))
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("second process got %v, want HeldError", err)
	}
	if !held.Known || held.Holder.PID != child.cmd.Process.Pid || held.Holder.Command != "forked child" {
		t.Fatalf("holder = %+v known=%v, want pid %d", held.Holder, held.Known, child.cmd.Process.Pid)
	}
}

func TestForked_SecondProcessIsHeldThenAcquiresAfterFirstExits(t *testing.T) {
	dir := t.TempDir()
	m := newManagerAt(dir, clock.NewFakeClock(0))
	key := KeyFor("android", "FORK")
	child := startChild(t, dir, key)

	assertHeldByChild(t, m, key, child)

	if err := child.stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.cmd.Wait(); err != nil {
		t.Fatalf("child exit: %v", err)
	}
	l, err := m.Acquire(context.Background(), key, testHolder(os.Getpid(), "parent"))
	if err != nil {
		t.Fatalf("acquire after the first process exited: %v", err)
	}
	_ = l.Release()
}

func TestForked_SecondProcessAcquiresAfterFirstIsKilled(t *testing.T) {
	dir := t.TempDir()
	m := newManagerAt(dir, clock.NewFakeClock(0))
	key := KeyFor("android", "KILL")
	child := startChild(t, dir, key)

	assertHeldByChild(t, m, key, child)

	if err := child.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.cmd.Wait()
	l, err := m.Acquire(context.Background(), key, testHolder(os.Getpid(), "parent"))
	if err != nil {
		t.Fatalf("acquire after SIGKILL: %v", err)
	}
	h, _ := readHolder(m.LockPath(key))
	if h.PID != os.Getpid() {
		t.Fatalf("stale child metadata not replaced: %+v", h)
	}
	_ = l.Release()
}
