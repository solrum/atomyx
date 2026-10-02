// Package shutdown bounds how long a binary's exit path may run. A
// child process in its own process group no longer receives a
// terminal Ctrl-C, so every binary must stop its children itself, and
// a stuck child must not be able to hold the exit open forever.
package shutdown

import "time"

// GracePeriod is how long a binary waits for its cleanup to finish
// after a stop request before it gives up and exits anyway.
const GracePeriod = 10 * time.Second

// Bounded runs cleanup and reports whether it finished within grace.
// When it did not, cleanup keeps running on its own goroutine and the
// caller is expected to exit the process, which reclaims it.
func Bounded(grace time.Duration, cleanup func()) bool {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		cleanup()
	}()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-finished:
		return true
	case <-timer.C:
		return false
	}
}

// ForceExitAfter starts a watchdog that calls exit once grace has
// elapsed. It is meant to be started when a stop request arrives, so
// a cleanup that hangs still ends the process. The returned cancel
// disarms the watchdog and is safe to call more than once.
func ForceExitAfter(grace time.Duration, exit func()) (cancel func()) {
	timer := time.AfterFunc(grace, exit)
	return func() { timer.Stop() }
}
