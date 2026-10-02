package cli

import (
	"context"
	"fmt"
	"io"

	"dev.atomyx/internal/storage"
	"dev.atomyx/internal/storageroot"
)

// migrateRunsFlag selects the run migration mode of doctor.
const migrateRunsFlag = "--migrate-runs"

// deviceFlags are the doctor flags that only make sense for a device
// health check.
var deviceFlags = []string{"--platform", "--device"}

// migrationIssueJSON is one record a migration left in place.
type migrationIssueJSON struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// migrationJSON is the --json shape of a run migration. Every list is
// present and never null.
type migrationJSON struct {
	Root            string               `json:"root"`
	Migrated        []string             `json:"migrated"`
	AlreadyMigrated []string             `json:"alreadyMigrated"`
	Conflicts       []migrationIssueJSON `json:"conflicts"`
	Damaged         []migrationIssueJSON `json:"damaged"`
	ScratchRemoved  int                  `json:"scratchRemoved"`
}

// migrateRuns rewrites the flat run records under the run root as run
// folders and reports the outcome. It never touches a device. The root
// is --report-dir, then ATOMYX_STORAGE_DIR, then the default storage
// root, the same order the run command uses.
//
// Exit codes: 0 when every record is migrated or was already migrated,
// 1 when a record is damaged, a record conflicts with an existing
// folder, or storage failed, and 2 when the command is misused.
func migrateRuns(ctx context.Context, deps Deps, flags map[string]FlagValue) int {
	stdout, stderr := deps.stdout(), deps.stderr()
	for _, name := range deviceFlags {
		if _, set := flags[name]; set {
			_, _ = fmt.Fprintf(stderr, "error: %s checks run records only, so it cannot be combined with %s.\n", migrateRunsFlag, name)
			return 2
		}
	}
	explicit, _ := flagString(flags, "--report-dir")
	root, err := storageroot.Resolve(explicit)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	store, err := storage.NewFileStorage(storage.FileStorageOptions{Root: root})
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	report, err := storage.MigrateRuns(store, root)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: run migration stopped early: %v\n", err)
		_, _ = fmt.Fprintf(stderr, "Run the command again to finish. Records already moved stay moved.\n")
		return 1
	}
	if flagBool(flags, "--json") {
		if err := encodeJSON(stdout, toMigrationJSON(root, report)); err != nil {
			return reportUncaught(ctx, stderr, deps.Debug, err)
		}
	} else {
		printMigrationReport(stdout, root, report)
	}
	if !report.Clean() {
		return 1
	}
	return 0
}

func toMigrationJSON(root string, r storage.MigrationReport) migrationJSON {
	return migrationJSON{
		Root:            root,
		Migrated:        nonNil(r.Migrated),
		AlreadyMigrated: nonNil(r.AlreadyMigrated),
		Conflicts:       issuesJSON(r.Conflicts),
		Damaged:         issuesJSON(r.Damaged),
		ScratchRemoved:  r.ScratchRemoved,
	}
}

func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func issuesJSON(issues []storage.MigrationIssue) []migrationIssueJSON {
	out := make([]migrationIssueJSON, 0, len(issues))
	for _, i := range issues {
		out = append(out, migrationIssueJSON{ID: i.ID, Reason: i.Reason})
	}
	return out
}

func printMigrationReport(w io.Writer, root string, r storage.MigrationReport) {
	_, _ = fmt.Fprintf(w, "Atomyx: run migration\n")
	_, _ = fmt.Fprintf(w, "  root:             %s\n", root)
	_, _ = fmt.Fprintf(w, "  migrated:         %d\n", len(r.Migrated))
	_, _ = fmt.Fprintf(w, "  already migrated: %d\n", len(r.AlreadyMigrated))
	_, _ = fmt.Fprintf(w, "  conflicts:        %d\n", len(r.Conflicts))
	_, _ = fmt.Fprintf(w, "  damaged:          %d\n", len(r.Damaged))
	for _, i := range r.Conflicts {
		_, _ = fmt.Fprintf(w, "  conflict %s: %s\n", i.ID, i.Reason)
	}
	for _, i := range r.Damaged {
		_, _ = fmt.Fprintf(w, "  damaged %s: %s\n", i.ID, i.Reason)
	}
	if !r.Clean() {
		_, _ = fmt.Fprintf(w, "  Records listed above were left in place. Fix or remove them, then run the command again.\n")
	}
}
