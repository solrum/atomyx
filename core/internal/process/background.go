package process

import (
	"context"
	"io"
)

// BackgroundResult carries a BackgroundProcess's outcome once it has
// exited.
type BackgroundResult struct {
	ExitCode int
	// Stderr is the process's captured standard error, bounded to a
	// fixed byte budget so a verbose or endlessly chatty process
	// cannot grow memory without limit. When the output exceeds that
	// budget the end of it is kept and the earlier part is silently
	// dropped, because a failing tool prints its real error last.
	Stderr string
}

// BackgroundProcess is a process a BackgroundRunner has started but
// has not waited on. Unlike the Result a Runner.Run call returns only
// after the command completes, a BackgroundProcess's standard output
// is readable while the process keeps running, so a caller can act on
// an early line of output (a readiness handshake, for example)
// without waiting for the process to exit, because the process may
// run for the rest of the program's lifetime.
type BackgroundProcess interface {
	// Stdout returns a reader over the process's standard output. It
	// is valid until the process exits and its output is fully
	// drained. Reading past that point returns io.EOF.
	Stdout() io.Reader
	// Wait blocks until the process exits and reports how. A second
	// call to Wait returns the same result without waiting again.
	Wait() (BackgroundResult, error)
	// Kill terminates the process if it is still running. Killing an
	// already-exited process is a no-op.
	Kill() error
}

// BackgroundRunner starts an external command and returns a handle to
// it immediately, instead of blocking until it exits the way
// Runner.Run does. An adapter that needs to supervise a long-running
// helper process, one that keeps running after signaling it is ready
// instead of completing like a CLI invocation, takes a
// BackgroundRunner by constructor injection instead of calling
// os/exec directly, so a test can substitute a fake and assert
// against a scripted stdout stream without a real process.
type BackgroundRunner interface {
	Start(ctx context.Context, name string, args ...string) (BackgroundProcess, error)
}
