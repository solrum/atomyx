// Package storage holds the persistence primitives the MCP tool
// layer builds on: a Storage port for arbitrary JSON and markdown
// records (bug reports, case studies, persisted run records) and a
// RunStore that tracks the single in-flight test run.
//
// Storage is deliberately generic. Callers namespace records by key
// prefix ("bugs/<id>", "case-studies/<month>/<slug>", "runs/<id>")
// rather than the package knowing about any specific record type.
// RunStore holds no persistence of its own: it tracks live run state
// in memory only, and a caller that wants a run to outlive the
// process persists the record it returns through a Storage instance,
// keyed by RunKey.
package storage
