package process

import (
	"errors"
	"testing"
	"time"
)

func TestFakeBackgroundProcess_Interrupt_ExitsWithoutKill(t *testing.T) {
	p := NewFakeBackgroundProcess()
	if err := p.Interrupt(time.Second); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if !p.Interrupted() || p.Killed() {
		t.Fatalf("Interrupted() = %v, Killed() = %v, want true and false", p.Interrupted(), p.Killed())
	}
	if result, _ := p.Wait(); result.ExitCode != FakeInterruptExitCode {
		t.Fatalf("exit code = %d, want %d", result.ExitCode, FakeInterruptExitCode)
	}
}

func TestFakeBackgroundProcess_Interrupt_IgnoredFallsBackToKill(t *testing.T) {
	p := NewFakeBackgroundProcess()
	p.IgnoreInterrupt()
	if err := p.Interrupt(time.Second); !errors.Is(err, ErrInterruptEscalated) {
		t.Fatalf("Interrupt() error = %v, want ErrInterruptEscalated", err)
	}
	if !p.Interrupted() || !p.Killed() {
		t.Fatalf("Interrupted() = %v, Killed() = %v, want both true", p.Interrupted(), p.Killed())
	}
}

func TestFakeBackgroundProcess_Interrupt_AfterExitChangesNothing(t *testing.T) {
	p := NewFakeBackgroundProcess()
	p.Exit(0)
	if err := p.Interrupt(time.Second); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if p.Killed() {
		t.Fatal("Interrupt() after exit killed the process")
	}
	if result, _ := p.Wait(); result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want the original 0", result.ExitCode)
	}
}
