package runfolder

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const childRootEnv = "ATOMYX_RUNFOLDER_TEST_ROOT"

// TestMain re-executes the test binary as a second process that
// starts a run, prints its id, and then blocks until stdin closes. It
// never finalizes, so both a clean exit and a SIGKILL show the kernel
// dropping the lock of a run that never completed.
func TestMain(m *testing.M) {
	if root := os.Getenv(childRootEnv); root != "" {
		os.Exit(runWriterChild(root))
	}
	os.Exit(m.Run())
}

func runWriterChild(root string) int {
	w, err := NewStarter(root, time.Now, rand.Reader).Start("forked", Meta{Kind: KindScript})
	if err != nil {
		fmt.Println("error: " + err.Error())
		return 2
	}
	fmt.Println(w.ID())
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

func startWriterChild(t *testing.T, root string) (id string, cmd *exec.Cmd, stdin io.WriteCloser) {
	t.Helper()
	cmd = exec.CommandContext(context.Background(), os.Args[0])
	cmd.Env = append(os.Environ(), childRootEnv+"="+root)
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
	id = strings.TrimSpace(line)
	if err != nil || strings.HasPrefix(id, "error:") {
		t.Fatalf("child did not start a run: %q, %v", line, err)
	}
	return id, cmd, stdin
}

func TestForkedWriterIsLiveUntilItsProcessEnds(t *testing.T) {
	for name, end := range map[string]func(*exec.Cmd, io.WriteCloser){
		"clean exit": func(_ *exec.Cmd, stdin io.WriteCloser) { _ = stdin.Close() },
		"sigkill":    func(cmd *exec.Cmd, _ io.WriteCloser) { _ = cmd.Process.Kill() },
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			id, cmd, stdin := startWriterChild(t, root)
			live := NewLockLiveness(root)
			meta, ok, err := NewReader(root).Get(id)
			if err != nil || !ok {
				t.Fatalf("read child run: ok=%v err=%v", ok, err)
			}
			if !live.IsLive(id) || meta.Settle(live).Outcome != "" {
				t.Fatal("run of a live process is not running")
			}
			end(cmd, stdin)
			_ = cmd.Wait()
			if live.IsLive(id) {
				t.Fatal("run still live after its process ended")
			}
			if got := meta.Settle(live); got.Outcome != OutcomeInterrupted {
				t.Fatalf("settled as %+v", got)
			}
		})
	}
}
