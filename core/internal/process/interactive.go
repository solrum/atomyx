package process

import (
	"context"
	"io"
)

// InteractiveProcess is a BackgroundProcess whose standard input the
// caller writes to. A caller ends the input by closing the writer, which
// the process reads as end of file.
type InteractiveProcess interface {
	BackgroundProcess
	// Stdin returns the writer feeding the process's standard input.
	// Closing it more than once is safe.
	Stdin() io.WriteCloser
}

// InteractiveRunner starts a command like BackgroundRunner.Start and also
// connects a pipe to its standard input. A command started through
// BackgroundRunner.Start keeps an empty standard input, so a caller that
// needs to talk to the process asks for this runner instead.
type InteractiveRunner interface {
	StartInteractive(ctx context.Context, name string, args ...string) (InteractiveProcess, error)
}
