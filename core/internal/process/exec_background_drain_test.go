package process

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"testing"
	"time"
)

func TestExecBackground_Wait_LeavesOutputWrittenBeforeExitReadable(t *testing.T) {
	proc, err := NewExecBackground().Start(context.Background(), "sh", "-c", "echo last; exit 0")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	out, err := io.ReadAll(proc.Stdout())

	if err != nil || string(out) != "last\n" {
		t.Fatalf("output after Wait = %q, %v, want the line written before exit", out, err)
	}
}

func TestExecBackground_Interrupt_LeavesTheLastLineWrittenOnSigintReadable(t *testing.T) {
	proc := startReady(t, "trap 'echo last; exit 0' INT; echo ready; while :; do sleep 0.05; done")

	if err := proc.Interrupt(10 * time.Second); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())

	if err != nil || string(out) != "last\n" {
		t.Fatalf("output after Interrupt = %q, %v, want the line written on SIGINT", out, err)
	}
}

func TestExecBackground_Wait_ClosesOutputHeldOpenByADescendantAfterTheDrainDelay(t *testing.T) {
	r := ExecBackground{waitDelay: 100 * time.Millisecond}
	proc, err := r.Start(context.Background(), "sh", "-c", "sleep 30 & echo x; exit 0")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = proc.Kill() }()
	if _, err := proc.Wait(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Wait() error = %v", err)
	}

	read := make(chan struct{})
	go func() { _, _ = io.ReadAll(proc.Stdout()); close(read) }()

	select {
	case <-read:
	case <-time.After(10 * time.Second):
		t.Fatal("reading stdout stayed blocked on a descendant holding it open")
	}
}
