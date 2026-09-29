// Package conformance drives the Go runtime's implementation of an
// operation and asserts its output matches a frozen golden fixture
// under goldens/, at the unit, stdio, and schema levels. Each golden
// was captured once from the reference TypeScript implementation;
// `go test ./internal/conformance/... -update` regenerates a
// suite's goldens from the Go implementation's own current output.
//
// The Go runtime is the strategic implementation. For a failed step's
// error detail, this package asserts error-code and structural parity
// (the stable code token, ok flags, step indices, command names) and
// treats the Go runtime as authoritative on the human-readable wording
// that follows the code: see DiffRunnerJSON in diff.go. Every other
// field, including a successful step's detail, is still compared
// exactly.
package conformance
