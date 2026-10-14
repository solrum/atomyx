// Package history aggregates run outcomes per test key across
// recorded runs, so a caller can ask how often a test passed, failed,
// or flipped between the two over a window of past runs.
//
// The package is a leaf over the run folder contract. It reads run
// metadata and index entries through the runfolder types and reports
// failures through diag, and it must not depend on storage, the CLI,
// or any driver, so the same aggregation serves every consumer that
// can supply run records.
package history
