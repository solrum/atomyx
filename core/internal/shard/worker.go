package shard

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"time"

	"dev.atomyx/internal/process"
)

// Device names one device a worker drives. Platform is passed on to the
// worker so a mixed list of devices needs no guessing.
type Device struct {
	ID       string
	Platform string
}

// WorkerSpec says which worker to start: the device it drives and the
// command line arguments that make the executable run as that worker.
type WorkerSpec struct {
	Device Device
	Args   []string
}

// Exit is how a worker process ended.
type Exit struct {
	Code   int
	Stderr string
}

// Worker is one running worker as the coordinator sees it.
type Worker interface {
	// Lines yields the worker's decoded reports and is closed when the
	// worker's output ends. A line that cannot be decoded arrives as an
	// error line with code ProtocolErrorCode.
	Lines() <-chan Line
	// Send hands the worker its next entry path.
	Send(path string) error
	// CloseInput tells the worker no entry is left.
	CloseInput() error
	// Stop asks the worker to stop on its own, and ends it after grace.
	// It returns once the worker is gone.
	Stop(grace time.Duration)
	// Wait reports how the worker ended. It is called after Lines closed.
	Wait() Exit
}

// Launcher starts workers.
type Launcher interface {
	Launch(ctx context.Context, spec WorkerSpec) (Worker, error)
}

// ProtocolErrorCode is the code of the error line that stands in for a
// worker output line that is not a valid protocol line.
const ProtocolErrorCode = "WORKER_PROTOCOL"

// ProcessLauncher starts each worker as a process of one executable, in its
// own process group so a terminal interrupt reaches only the parent.
type ProcessLauncher struct {
	runner     process.InteractiveRunner
	executable string
}

// NewProcessLauncher returns a Launcher that runs executable through
// runner.
func NewProcessLauncher(runner process.InteractiveRunner, executable string) *ProcessLauncher {
	return &ProcessLauncher{runner: runner, executable: executable}
}

var _ Launcher = (*ProcessLauncher)(nil)

// Launch starts the worker. The process outlives a canceled ctx, because
// the coordinator stops its workers itself, first asking them to finish
// and then ending them.
func (l *ProcessLauncher) Launch(ctx context.Context, spec WorkerSpec) (Worker, error) {
	proc, err := l.runner.StartInteractive(context.WithoutCancel(ctx), l.executable, spec.Args...)
	if err != nil {
		return nil, err
	}
	lines := make(chan Line)
	go readLines(proc.Stdout(), lines)
	return &processWorker{proc: proc, lines: lines}, nil
}

type processWorker struct {
	proc  process.InteractiveProcess
	lines chan Line
}

func (w *processWorker) Lines() <-chan Line { return w.lines }

func (w *processWorker) Send(path string) error {
	_, err := io.WriteString(w.proc.Stdin(), path+"\n")
	return err
}

func (w *processWorker) CloseInput() error { return w.proc.Stdin().Close() }

func (w *processWorker) Stop(grace time.Duration) { _ = w.proc.Interrupt(grace) }

func (w *processWorker) Wait() Exit {
	result, _ := w.proc.Wait()
	return Exit{Code: result.ExitCode, Stderr: result.Stderr}
}

// readLines decodes each line of r into out and closes out at the end of
// the output.
func readLines(r io.Reader, out chan<- Line) {
	defer close(out)
	reader := bufio.NewReader(r)
	for {
		raw, err := reader.ReadBytes('\n')
		if len(raw) > 0 {
			line, decodeErr := DecodeLine(raw)
			if decodeErr != nil {
				line = Line{V: ProtocolVersion, Kind: LineError, Code: ProtocolErrorCode, Message: fmt.Sprint(decodeErr)}
			}
			out <- line
		}
		if err != nil {
			return
		}
	}
}
