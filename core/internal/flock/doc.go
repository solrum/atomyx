// Package flock is the advisory file lock primitive shared by every
// package that holds a kernel-owned lock on an open file.
//
// The kernel owns the lock. It is attached to the open file
// description, so it disappears when the descriptor is closed or the
// process exits for any reason, including SIGKILL.
//
// Invariants callers rely on:
//
//   - Locking never switches the descriptor to blocking mode and
//     retries when a signal interrupts the call.
//   - A caller opens the file through os.OpenFile so close-on-exec
//     applies. A leaked descriptor in a child process would keep the
//     lock held after its owner exits.
//   - The package depends only on the standard library, so it sits
//     below every consumer.
package flock
