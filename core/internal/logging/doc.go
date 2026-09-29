// Package logging is the structured logging port every layer of the
// module writes through, so the host chooses the destination (stderr,
// a file, an editor panel, a remote log service) without runner,
// device, sidecar, or any other package depending on that choice. It
// is a leaf package (imports only the standard library) so any
// package in the module may depend on it without pulling in runner,
// orchestra, or any other business-logic package.
package logging
