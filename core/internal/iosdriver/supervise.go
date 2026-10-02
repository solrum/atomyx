package iosdriver

import (
	"io"
	"sync"

	"dev.atomyx/internal/process"
)

// superviseBackground watches a started long-lived process. It
// drains the process's standard output so a verbose child can never
// fill the pipe and stall, then waits for the process to exit and
// reports classify's verdict on the outcome through the returned
// exited channel, which receives exactly one value.
//
// stop kills the process group, waits until the watcher has observed
// the exit, and is safe to call any number of times, including after
// the process has already exited on its own.
//
// The child's standard error is retained by the BackgroundRunner up
// to a fixed byte budget and handed to classify through
// BackgroundResult, so a failure message can still quote the tail of
// the diagnostics.
func superviseBackground(
	proc process.BackgroundProcess,
	classify func(process.BackgroundResult, error) error,
) (exited <-chan error, stop func()) {
	out := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, proc.Stdout())
		result, err := proc.Wait()
		out <- classify(result, err)
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			_ = proc.Kill()
			<-done
		})
	}
	return out, stop
}
