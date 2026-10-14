package main

import (
	"errors"
	"testing"
	"time"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/storage"
)

type warnRecorder struct {
	logging.Logger
	warnings []string
}

func (w *warnRecorder) Warn(msg string, _ logging.LogContext) { w.warnings = append(w.warnings, msg) }

func int64Ptr(n int64) *int64 { return &n }

func TestPruneRetentionDoesNothingWithoutAPolicy(t *testing.T) {
	called := false
	prune := func(string, storage.PruneCriteria) (storage.PruneResult, error) {
		called = true
		return storage.PruneResult{}, nil
	}
	now := time.Unix(1000, 0)

	pruneRetention(nil, "root", now, logging.NoopLogger{}, prune)
	pruneRetention(&config.AtomyxConfig{}, "root", now, logging.NoopLogger{}, prune)

	if called {
		t.Fatal("prune ran without a retention policy")
	}
}

func TestPruneRetentionPassesThePolicyAndTheRoot(t *testing.T) {
	var gotRoot string
	var got storage.PruneCriteria
	prune := func(root string, c storage.PruneCriteria) (storage.PruneResult, error) {
		gotRoot, got = root, c
		return storage.PruneResult{}, nil
	}
	now := time.Unix(1000, 0)
	cfg := &config.AtomyxConfig{Retention: &config.Retention{KeepLast: int64Ptr(5), OlderThanDays: int64Ptr(2)}}

	pruneRetention(cfg, "/runs", now, logging.NoopLogger{}, prune)

	if gotRoot != "/runs" || got.Keep != 5 || got.OlderThan != 48*time.Hour || !got.Now.Equal(now) {
		t.Fatalf("root = %q, criteria = %+v", gotRoot, got)
	}
}

func TestPruneRetentionTurnsFailuresIntoWarnings(t *testing.T) {
	cfg := &config.AtomyxConfig{Retention: &config.Retention{KeepLast: int64Ptr(1)}}
	logger := &warnRecorder{Logger: logging.NoopLogger{}}

	pruneRetention(cfg, "/runs", time.Now(), logger, func(string, storage.PruneCriteria) (storage.PruneResult, error) {
		return storage.PruneResult{}, errors.New("listing failed")
	})
	pruneRetention(cfg, "/runs", time.Now(), logger, func(string, storage.PruneCriteria) (storage.PruneResult, error) {
		return storage.PruneResult{Skipped: 1}, nil
	})

	if len(logger.warnings) != 2 {
		t.Fatalf("warnings = %v, want one per problem", logger.warnings)
	}
}
