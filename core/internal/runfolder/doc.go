// Package runfolder defines the on-disk contract of a run folder: the
// directory layout, the meta.json record, and the JSON Schema that
// external readers validate it against.
//
// A run folder is laid out as:
//
//	<root>/runs/v1/<runId>/
//	  meta.json
//	  steps.jsonl
//	  log.jsonl
//	  console.log
//	  artifacts/
//	  report/
//	  interventions.jsonl
//	  run.lock
//
// Invariants callers rely on:
//
//   - The layout names live in layout.go and nowhere else in production
//     Go code. The check-runfolder-layout gate rejects a string literal
//     that spells one of them outside this package.
//   - Times in meta.json are epoch milliseconds.
//   - Readers accept any schemaVersion up to SchemaVersion and ignore
//     unknown fields. Writers never reshape a field within a major
//     version.
//   - Run migration and every edit of a run record serialize on the
//     migrate lock under the runs directory. A migration takes it
//     exclusively and an editor takes it shared without waiting.
//   - The run id carries no format constraint, because a run migrated
//     from a flat record keeps its original id.
//   - A Writer holds an exclusive lock on run.lock while its run is
//     live. The kernel drops the lock when the process exits, so a run
//     with no completedAt and no lock was interrupted.
//   - meta.json is only ever replaced through a temporary file in the
//     same directory and a rename, so a reader never sees a partial
//     file.
//
// The package depends only on the standard library and the flock leaf
// package, so it can sit below every consumer of a run folder.
package runfolder
