package process

import (
	"bufio"
	"context"
	"io"
	"testing"
)

func TestExecBackground_StartInteractive_ForwardsInputAndSeesEndOfFile(t *testing.T) {
	proc, err := NewExecBackground().StartInteractive(context.Background(), "sh", "-c", `while read line; do echo "got:$line"; done`)
	if err != nil {
		t.Fatalf("StartInteractive() error = %v", err)
	}
	t.Cleanup(func() { _ = proc.Kill() })
	out := bufio.NewReader(proc.Stdout())

	if _, err := io.WriteString(proc.Stdin(), "alpha\n"); err != nil {
		t.Fatal(err)
	}
	if line, _ := out.ReadString('\n'); line != "got:alpha\n" {
		t.Fatalf("first reply = %q", line)
	}
	_ = proc.Stdin().Close()
	result, err := proc.Wait()
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("Wait() = %+v, %v, want a clean exit after the input closed", result, err)
	}
}

func TestExecBackground_Start_LeavesStandardInputEmpty(t *testing.T) {
	proc, err := NewExecBackground().Start(context.Background(), "sh", "-c", "cat; echo done")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proc.Kill() })
	if _, ok := proc.(InteractiveProcess); ok && proc.(InteractiveProcess).Stdin() != nil {
		t.Fatal("a process started with Start exposes an input pipe")
	}
	line, _ := bufio.NewReader(proc.Stdout()).ReadString('\n')
	if line != "done\n" {
		t.Fatalf("output = %q, want the process to see an empty input", line)
	}
}

func TestFakeBackground_StartInteractive_ReaderSeesWritesThenEndOfFile(t *testing.T) {
	fake := NewFakeBackground()
	staged := NewFakeBackgroundProcess()
	fake.Stub(staged)

	proc, err := fake.StartInteractive(context.Background(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(proc.Stdin(), "one\ntwo\n")
	_ = proc.Stdin().Close()

	got, err := io.ReadAll(staged.Input())
	if err != nil || string(got) != "one\ntwo\n" {
		t.Fatalf("input = %q, %v", got, err)
	}
	if _, err := io.WriteString(proc.Stdin(), "late"); err == nil {
		t.Fatal("a write after Close succeeded")
	}
}

func TestFakeBackground_InputEndsWhenTheProcessExits(t *testing.T) {
	staged := NewFakeBackgroundProcess()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.ReadAll(staged.Input())
	}()
	staged.Exit(0)
	<-done
}
