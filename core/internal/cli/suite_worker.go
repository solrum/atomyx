package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/shard"
)

// runSuiteWorker is `atomyx suite --device <id> --worker`, the process a
// sharding parent starts once per device. It holds its device through the
// normal acquire path, reads entry paths from deps.Stdin one per line,
// runs each through the suite runner, and reports each finished entry on
// deps.Stdout as a line of the shard protocol. It writes its own suite run
// folder and ends when the input ends or the device is lost.
//
// Exit codes: 0 when the input ended normally, 1 when the worker could not
// start, lost its device, or was interrupted, 2 for usage errors.
func runSuiteWorker(ctx context.Context, deps Deps, flags map[string]FlagValue, positional *string) int {
	stdout, stderr := deps.stdout(), deps.stderr()
	deviceID, _ := flagString(flags, "--device")
	_, devices := flagString(flags, "--devices")
	_, mode := flagString(flags, "--shard")
	if devices || mode {
		_, _ = fmt.Fprintln(stderr, "error: --worker drives one device and cannot be combined with --devices or --shard")
		return 2
	}
	if deviceID == "" {
		_, _ = fmt.Fprintln(stderr, "error: --worker requires --device")
		return 2
	}
	cfg, err := resolveSuiteSettings(flags, positional)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		return reportWorkerFailure(ctx, deps, err)
	}
	cfg.cwd = cwd

	desc, err := resolveDescriptor(ctx, deps.Probe, deps.Runners, resolvePlatform(flags, cfg.environment), deviceID)
	if err != nil {
		return reportWorkerFailure(ctx, deps, err)
	}
	parentRunID, _ := flagString(flags, "--parent-run")
	host, err := startSuiteHost(ctx, deps, cfg, resolveProxyFlag(flags, cfg.environment), parentRunID, desc, videoEnabled(flags, cfg.loaded))
	if err != nil {
		return reportWorkerFailure(ctx, deps, err)
	}
	defer host.close(ctx, stderr)

	if err := shard.EncodeLine(stdout, shard.Line{Kind: shard.LineReady, RunID: host.record.ID()}); err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}

	startedAt := time.Now()
	finished, lost := serveWorkerEntries(host, positional, workerPaths(deps.Stdin), stdout, stderr)

	result := scenario.Summarize(finished, positional, time.Since(startedAt).Milliseconds())
	counts := suiteMetaCounts(result)
	switch {
	case lost:
		host.record.setOutcome(runfolder.OutcomeDeviceLost, counts)
	case host.runCtx.Err() != nil:
		host.record.setOutcome(runfolder.OutcomeError, counts)
	default:
		host.record.setOutcome(outcomeForResult(result.Ok), counts)
	}
	printSuiteResult(stderr, result)
	if lost || host.runCtx.Err() != nil {
		return 1
	}
	return 0
}

// serveWorkerEntries runs each path from paths as one suite entry and
// reports it, until the paths end, the device is lost, or the run context
// ends. It returns the entries it finished and whether the device was lost.
// After every entry it rewrites index.json, so a worker that is killed
// still leaves an index of what it finished.
func serveWorkerEntries(host *suiteHost, positional *string, paths <-chan string, stdout, stderr io.Writer) ([]scenario.SuiteEntryResult, bool) {
	var finished []scenario.SuiteEntryResult
	for {
		var path string
		select {
		case <-host.runCtx.Done():
			return finished, false
		case next, ok := <-paths:
			if !ok {
				return finished, false
			}
			path = next
		}
		entry, ran := runWorkerEntry(host, positional, path, len(finished))
		if !ran {
			return finished, false
		}
		finished = append(finished, entry)
		if err := host.record.writer.WriteIndex(suiteIndexEntries(scenario.SuiteResult{Entries: finished})); err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: could not write the suite index: %s\n", err)
		}
		lost := host.opened.LostErr() != nil || outcomeDeviceLost(entry.Result)
		wire := wireEntry(entry)
		if err := shard.EncodeLine(stdout, shard.Line{Kind: shard.LineEntry, RunID: host.record.ID(), Entry: &wire, DeviceLost: lost}); err != nil || lost {
			return finished, lost
		}
	}
}

// runWorkerEntry runs the entry at path as the position-th entry of the
// worker. A file that cannot be read is a failed entry. It reports false
// when the run was cancelled before the entry started.
func runWorkerEntry(host *suiteHost, positional *string, path string, position int) (scenario.SuiteEntryResult, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		message := err.Error()
		return scenario.SuiteEntryResult{
			Path:     path,
			Kind:     scenario.KindScript,
			Status:   scenario.EntryFailed,
			Error:    &message,
			Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryFailed, Error: &message}},
		}, true
	}
	result, _ := host.runner.Run(host.runCtx, []scenario.SuiteEntry{{Path: path, Content: string(content)}}, host.runOptions(positional, position)) //nolint:contextcheck // the host's run context is derived from the command context
	if len(result.Entries) == 0 {
		return scenario.SuiteEntryResult{}, false
	}
	return result.Entries[0], true
}

// workerPaths reads entry paths from in, one per line, and closes the
// channel when the input ends. Blank lines are skipped. A nil in is an
// input that has already ended.
func workerPaths(in io.Reader) <-chan string {
	paths := make(chan string)
	go func() {
		defer close(paths)
		if in == nil {
			return
		}
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			if line := scanner.Text(); line != "" {
				paths <- line
			}
		}
	}()
	return paths
}

// reportWorkerFailure tells the parent why the worker cannot start, with
// the stable error code on standard output, and tells the operator on
// standard error. It returns the worker's exit code.
func reportWorkerFailure(ctx context.Context, deps Deps, err error) int {
	code, message, _ := diag.Describe(err)
	_ = shard.EncodeLine(deps.stdout(), shard.Line{Kind: shard.LineError, Code: code, Message: message})
	return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
}
