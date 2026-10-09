package cli

import (
	"context"
	"io"
	"time"

	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/scenario"
)

// suiteOutputs is what a finished suite needs to tell the operator and the
// run folder about its result.
type suiteOutputs struct {
	stdout, humanOut, stderr io.Writer
	jsonOutput               bool
	record                   *runRecord
	label                    string
	startedAt, finishedAt    time.Time
	// platform and deviceID describe the devices the suite drove, for the
	// report header.
	platform, deviceID string
	envName            *string
}

// emitSuiteOutputs prints the suite result, writes the JSON object when
// asked, and renders the report into the run folder. It returns the
// command's exit code: 0 for a suite that passed, 1 for one that did not,
// and the uncaught-error code when an output could not be written.
func emitSuiteOutputs(ctx context.Context, deps Deps, o suiteOutputs, result scenario.SuiteResult, index []runfolder.IndexEntry) int {
	printSuiteResult(o.humanOut, result)
	if o.jsonOutput {
		payload := suiteJSONResult{Ok: result.Ok, RunFolder: o.record.Dir(), Entries: index}
		if err := encodeJSON(o.stdout, payload); err != nil {
			return reportUncaught(ctx, o.stderr, deps.Debug, err)
		}
		o.record.reportedInJSON = true
	}

	metadata := buildReportMetadata(o.record.ID(), o.label, o.startedAt, o.finishedAt, result.DurationMs, o.platform, o.deviceID, o.envName, nil)
	metadata.Command = report.ReportCommandSuite
	input := report.SuiteReportInput{
		Metadata:    metadata,
		Result:      result,
		Screenshots: suiteReportScreenshots(result),
		EntryVideos: indexEntryVideos(index),
	}
	bundle, genErr := report.NewFsReporter().Generate(input, o.record.reportOptions())
	if genErr != nil {
		return reportUncaught(ctx, o.stderr, deps.Debug, genErr)
	}
	printReportLocation(o.stderr, o.record, bundle)
	printVideoLocations(o.stderr, o.record.Dir(), input.EntryVideos)

	if result.Ok {
		return 0
	}
	return 1
}
