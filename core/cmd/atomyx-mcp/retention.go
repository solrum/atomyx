package main

import (
	"fmt"
	"time"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/storage"
)

// pruneFunc applies a prune policy to the run folders under a root.
type pruneFunc func(root string, criteria storage.PruneCriteria) (storage.PruneResult, error)

// pruneRetention applies the project's retention policy to the run
// folders under root, once. It does nothing when cfg has no retention
// block. It never returns an error and never writes to stdout, because
// stdout carries the protocol stream and a prune must not fail the host.
// Every problem is logged as a warning.
func pruneRetention(cfg *config.AtomyxConfig, root string, now time.Time, logger logging.Logger, prune pruneFunc) {
	if cfg == nil || cfg.Retention == nil {
		return
	}
	retention := cfg.Retention
	criteria := storage.NewPruneCriteria(int(derefInt64(retention.KeepLast)), int(derefInt64(retention.OlderThanDays)), now)
	result, err := prune(root, criteria)
	if err != nil {
		logger.Warn("retention prune skipped", logging.LogContext{"error": err.Error()})
		return
	}
	for _, warning := range result.Warnings() {
		logger.Warn(fmt.Sprintf("retention prune: %s", warning), nil)
	}
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
