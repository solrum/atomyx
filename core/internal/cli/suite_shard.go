package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/shard"
	"dev.atomyx/internal/shutdown"
)

// workerStopMargin is how much longer the parent waits for a worker to stop
// on its own than the worker spends on its own exit, so a worker that uses
// its whole budget is not ended while it is still finishing.
const workerStopMargin = 2 * time.Second

// workerStopGrace is how long the parent gives a worker to stop after an
// interrupt before it ends the worker.
const workerStopGrace = shutdown.GracePeriod + workerStopMargin

// shardedSuite is a suite that runs its entries over several devices.
type shardedSuite struct {
	flags       map[string]FlagValue
	positional  *string
	settings    suiteSettings
	request     shardRequest
	platform    string
	paths       []string
	jsonOutput  bool
	humanOut    io.Writer
	stdout      io.Writer
	stderr      io.Writer
	environment *string
}

// runShardedSuite runs the entries of a suite on several devices through
// one worker process per device and writes one merged result. The parent
// holds no lease of its own, every worker acquires its device the normal
// way. Its exit code is 0 when every entry ran and every entry that is not
// quarantined passed, 1 when an entry failed or was left unrun, 2 for
// devices that cannot be resolved, and the interrupt code after a stop
// signal.
func runShardedSuite(ctx context.Context, deps Deps, in shardedSuite) int {
	devices, err := resolveShardDevices(ctx, deps.Probe, in.request, in.platform)
	if err != nil {
		_, _ = fmt.Fprintf(in.stderr, "error: %s\n", err)
		return 2
	}
	if deps.Shards == nil {
		_, _ = fmt.Fprintln(in.stderr, "error: this build cannot start worker processes, so it cannot shard a suite")
		return 1
	}
	record, err := startRunRecord(in.settings.output, runIdentity{Kind: runfolder.KindSuite, Label: in.settings.label})
	if err != nil {
		return reportUncaught(ctx, in.stderr, deps.Debug, err)
	}
	defer record.end(ctx, in.stderr)

	startedAt := time.Now()
	outcome := shard.Run(ctx, shard.Options{
		Launcher:  deps.Shards,
		Devices:   devices,
		Paths:     in.paths,
		Args:      func(d shard.Device) []string { return workerArgs(in.positional, in.flags, d, record.ID()) },
		StopGrace: workerStopGrace,
		Warn:      func(message string) { _, _ = fmt.Fprintf(in.stderr, "warning: %s\n", message) },
	})
	finishedAt := time.Now()

	entries, index := shardedEntries(in.paths, outcome)
	result := scenario.Summarize(entries, in.positional, finishedAt.Sub(startedAt).Milliseconds())
	indexErr := record.writer.WriteIndex(index)
	counts := suiteMetaCounts(result)
	if outcome.Interrupted {
		record.setOutcome(runfolder.OutcomeError, counts)
		return reportUncaught(ctx, in.stderr, deps.Debug, context.Cause(ctx))
	}
	record.setOutcome(outcomeForResult(result.Ok), counts)
	if indexErr != nil {
		return reportUncaught(ctx, in.stderr, deps.Debug, indexErr)
	}
	return emitSuiteOutputs(ctx, deps, suiteOutputs{
		stdout:     in.stdout,
		humanOut:   in.humanOut,
		stderr:     in.stderr,
		jsonOutput: in.jsonOutput,
		record:     record,
		label:      in.settings.label,
		startedAt:  startedAt,
		finishedAt: finishedAt,
		platform:   joinedPlatforms(devices),
		deviceID:   joinedDeviceIDs(devices),
		envName:    in.environment,
	}, result, index)
}

func joinedDeviceIDs(devices []shard.Device) string {
	ids := make([]string, len(devices))
	for i, d := range devices {
		ids[i] = d.ID
	}
	return strings.Join(ids, ",")
}

func joinedPlatforms(devices []shard.Device) string {
	var platforms []string
	for _, d := range devices {
		if !slices.Contains(platforms, d.Platform) {
			platforms = append(platforms, d.Platform)
		}
	}
	return strings.Join(platforms, ",")
}
