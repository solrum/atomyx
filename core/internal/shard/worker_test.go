package shard

import (
	"bufio"
	"context"
	"errors"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

func launchFake(t *testing.T) (Worker, *process.FakeBackgroundProcess, *process.FakeBackground) {
	t.Helper()
	background := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	background.Stub(proc)
	worker, err := NewProcessLauncher(background, "/bin/atomyx").Launch(context.Background(), WorkerSpec{Device: Device{ID: "d1"}, Args: []string{"suite", "--device", "d1", "--worker"}})
	if err != nil {
		t.Fatal(err)
	}
	return worker, proc, background
}

func TestProcessLauncherStartsTheExecutableWithTheSpecArguments(t *testing.T) {
	_, _, background := launchFake(t)

	calls := background.Invocations()
	if len(calls) != 1 || calls[0].Name != "/bin/atomyx" || len(calls[0].Args) != 4 || calls[0].Args[1] != "--device" {
		t.Errorf("invocations = %+v", calls)
	}
}

func TestProcessWorkerDecodesLinesAndTurnsAGarbledLineIntoAnErrorLine(t *testing.T) {
	worker, proc, _ := launchFake(t)
	_ = EncodeLine(proc, Line{Kind: LineReady, RunID: "r1"})
	_, _ = proc.Write([]byte("starting up\n"))
	proc.Exit(0)

	var got []Line
	for line := range worker.Lines() {
		got = append(got, line)
	}

	if len(got) != 2 || got[0].Kind != LineReady || got[0].RunID != "r1" {
		t.Fatalf("lines = %+v", got)
	}
	if got[1].Kind != LineError || got[1].Code != ProtocolErrorCode {
		t.Errorf("garbled line became %+v, want a protocol error line", got[1])
	}
}

func TestProcessWorkerSendWritesOnePathPerLineAndCloseInputEndsTheInput(t *testing.T) {
	worker, proc, _ := launchFake(t)

	if err := worker.Send("/flows/a.yml"); err != nil {
		t.Fatal(err)
	}
	if err := worker.Send("/flows/b.yml"); err != nil {
		t.Fatal(err)
	}
	_ = worker.CloseInput()

	scanner := bufio.NewScanner(proc.Input())
	var paths []string
	for scanner.Scan() {
		paths = append(paths, scanner.Text())
	}
	if len(paths) != 2 || paths[0] != "/flows/a.yml" || paths[1] != "/flows/b.yml" {
		t.Errorf("worker input = %v", paths)
	}
}

func TestProcessWorkerStopInterruptsTheProcessAndWaitReportsItsExit(t *testing.T) {
	worker, proc, _ := launchFake(t)

	worker.Stop(time.Second)

	if !proc.Interrupted() {
		t.Error("Stop did not interrupt the process")
	}
	for range worker.Lines() {
	}
	if exit := worker.Wait(); exit.Code != process.FakeInterruptExitCode {
		t.Errorf("exit = %+v, want the interrupt exit code", exit)
	}
}

func TestProcessLauncherReportsAProcessThatCannotStart(t *testing.T) {
	background := process.NewFakeBackground()
	background.DefaultErr = errors.New("exec format error")

	_, err := NewProcessLauncher(background, "/bin/atomyx").Launch(context.Background(), WorkerSpec{})

	if err == nil {
		t.Fatal("Launch succeeded for a process that cannot start")
	}
}

func TestProcessWorkerDeliversTheLastLineWrittenBeforeAnInterruptedExit(t *testing.T) {
	worker, proc, _ := launchFake(t)
	_ = EncodeLine(proc, Line{Kind: LineReady, RunID: "r1"})
	_ = EncodeLine(proc, Line{Kind: LineEntry, RunID: "r1", Entry: &Entry{Path: "/flows/a.yml", Status: "passed", Attempts: []Attempt{{Number: 1, Status: "passed"}}}})

	worker.Stop(time.Second)

	var kinds []LineKind
	for line := range worker.Lines() {
		kinds = append(kinds, line.Kind)
	}
	if len(kinds) != 2 || kinds[1] != LineEntry {
		t.Errorf("lines after Stop = %v, want the ready line and the entry line", kinds)
	}
}
