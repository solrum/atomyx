// Package cli implements the atomyx command-line binary: argument
// parsing, command routing, the driver module's commands (run, suite,
// list-devices, version, help), and the skills module's commands
// (init, update-skills, help) over the Go core runtime.
//
// Writes of help text, version strings, and command output to stdout
// or stderr throughout this package are best-effort: a write failure
// on those streams means the process's own output is gone, so there
// is nothing a caller could do with the error that printing it
// somewhere else would fix. Every such write is deliberately
// unchecked rather than propagated.
package cli
