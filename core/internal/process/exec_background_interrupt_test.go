package process

import (
	"bufio"
	"context"
	"testing"
	"time"
)

const interruptTestGrace = 1500 * time.Millisecond

func startReady(t *testing.T, script string) BackgroundProcess {
	t.Helper()
	proc, err := NewExecBackground().Start(context.Background(), "sh", "-c", script)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = proc.Kill() })
	line, err := bufio.NewReader(proc.Stdout()).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("handshake = %q, %v", line, err)
	}
	return proc
}

func TestExecBackground_Interrupt_ProcessExitingOnSigintIsNotKilled(t *testing.T) {
	proc := startReady(t, "trap 'exit 3' INT; echo ready; while :; do sleep 0.05; done")

	start := time.Now()
	if err := proc.Interrupt(10 * time.Second); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Interrupt() took %v, want a prompt return after the process exited", elapsed)
	}
	result, _ := proc.Wait()
	if result.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3 from the SIGINT handler", result.ExitCode)
	}
}

func TestExecBackground_Interrupt_ProcessIgnoringSigintIsKilledAfterGrace(t *testing.T) {
	proc := startReady(t, "trap '' INT; echo ready; while :; do sleep 0.05; done")

	start := time.Now()
	if err := proc.Interrupt(interruptTestGrace); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < interruptTestGrace {
		t.Fatalf("Interrupt() returned after %v, before the %v grace", elapsed, interruptTestGrace)
	}
	result, _ := proc.Wait()
	if result.ExitCode == 0 || result.ExitCode == 3 {
		t.Fatalf("exit code = %d, want a signal death", result.ExitCode)
	}
}

func TestExecBackground_Interrupt_AfterExitIsHarmless(t *testing.T) {
	proc, err := NewExecBackground().Start(context.Background(), "sh", "-c", "exit 0")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if err := proc.Interrupt(interruptTestGrace); err != nil {
		t.Fatalf("Interrupt() after exit error = %v", err)
	}
	if err := proc.Interrupt(interruptTestGrace); err != nil {
		t.Fatalf("second Interrupt() error = %v", err)
	}
}
