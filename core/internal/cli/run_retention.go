package cli

import (
	"fmt"
	"time"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/storage"
)

// pruneToRetention applies the project's retention policy to the run
// folders under root, once, before a run or suite writes into it. It does
// nothing when the config has no retention block. Every problem is
// printed as one line on stderr and never changes the command's exit
// code, because a run must not fail on housekeeping.
//
// keepLast counts suite units, and the run about to start is not yet
// written, so it is extra: a pruned root holds at most keepLast units
// plus the new run.
func pruneToRetention(deps Deps, loaded *config.LoadedConfig, root string) {
	if loaded == nil || loaded.Config.Retention == nil {
		return
	}
	criteria := retentionCriteria(loaded.Config.Retention, time.UnixMilli(deps.clock().NowMs()))
	result, err := storage.PruneRoot(root, criteria)
	if err != nil {
		_, _ = fmt.Fprintf(deps.stderr(), "warning: retention prune skipped: %v\n", err)
		return
	}
	for _, warning := range result.Warnings() {
		_, _ = fmt.Fprintf(deps.stderr(), "warning: retention prune: %s\n", warning)
	}
}

func retentionCriteria(r *config.Retention, now time.Time) storage.PruneCriteria {
	keep, olderThanDays := r.Bounds()
	return storage.NewPruneCriteria(keep, olderThanDays, now)
}
