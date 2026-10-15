package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"dev.atomyx/internal/history"
	"dev.atomyx/internal/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var getTestStatsInputSchema = jsObject(map[string]any{
	"test": jsStringDesc("Report only the test with exactly this test key."),
	"last": map[string]any{
		"type":        "number",
		"description": "Count only the most recent N runs of each test. A whole number of at least 1.",
	},
	"since": jsStringDesc("Count only runs started at or after this time. An RFC 3339 timestamp or a YYYY-MM-DD date taken as midnight UTC."),
})

var getTestStatsSchema = compileToolSchema("get_test_stats", getTestStatsInputSchema)

type getTestStatsArgs struct {
	Test  *string `json:"test,omitempty"`
	Last  *int    `json:"last,omitempty"`
	Since *string `json:"since,omitempty"`
}

type testStatsEntryWire struct {
	TestKey          string   `json:"testKey"`
	Runs             int      `json:"runs"`
	Passed           int      `json:"passed"`
	Failed           int      `json:"failed"`
	Flaky            int      `json:"flaky"`
	FlakyRate        float64  `json:"flakyRate"`
	InfraFailures    int      `json:"infraFailures"`
	TestFailures     int      `json:"testFailures"`
	InfraFailureRate float64  `json:"infraFailureRate"`
	AbortedRuns      int      `json:"abortedRuns"`
	Attempts         int      `json:"attempts"`
	LastOutcomes     []string `json:"lastOutcomes"`
	DurationP50Ms    int64    `json:"durationP50Ms"`
	DurationP95Ms    int64    `json:"durationP95Ms"`
}

type testStatsResult struct {
	Ok               bool                 `json:"ok"`
	Tests            []testStatsEntryWire `json:"tests"`
	Skipped          int                  `json:"skipped"`
	InfraFailures    int                  `json:"infraFailures"`
	TestFailures     int                  `json:"testFailures"`
	InfraFailureRate float64              `json:"infraFailureRate"`
	AbortedRuns      int                  `json:"abortedRuns"`
	Attempts         int                  `json:"attempts"`
}

// registerGetTestStatsTool adds the get_test_stats tool: outcome
// statistics per test key across persisted runs, flakiest first.
func registerGetTestStatsTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "get_test_stats",
		Description: "Report how each test has behaved across its persisted runs: " +
			"how often it passed, failed, or flipped between the two, and how long it takes. " +
			"Use it to decide whether a failure is a real regression or a flaky test, and " +
			"which tests to investigate first. It complements list_runs, which lists " +
			"individual runs, while this tool aggregates them per test. " +
			"Each entry has `testKey`, `runs`, `passed` (includes flaky runs), `failed`, " +
			"`flaky` (passed only after a retry), `flakyRate` (flaky divided by runs, 0 to 1), " +
			"`lastOutcomes` (up to 10 of passed, failed, or flaky, most recent first), " +
			"`infraFailures` and `testFailures` (failures blamed on the infrastructure, meaning the device, agent, timeouts, or host tooling, or on the test), " +
			"`abortedRuns` (runs that ended device lost or interrupted, which are not in `runs` or `failed` but count in `infraFailures`), " +
			"`attempts` (`runs` plus `abortedRuns`), `infraFailureRate` (infraFailures divided by attempts, 0 to 1), and " +
			"`durationP50Ms` and `durationP95Ms` (0 when no run has a duration). " +
			"Entries are sorted flakiest first, then by failures, then by key. " +
			"The top level repeats `infraFailures`, `testFailures`, and `infraFailureRate` summed over all entries, with `abortedRuns` and `attempts`. " +
			"`skipped` counts stored runs that could not be read and are not included. " +
			"A test key is the path of a script or scenario file relative to its project " +
			"root (the nearest folder holding an Atomyx config file, else the git root), " +
			"written with forward slashes, and a run started from this server through " +
			"run_script is keyed `mcp:` followed by the script name. Runs that are " +
			"unfinished or cancelled are not counted, and an interrupted or device-lost run counts only as an infrastructure failure. " +
			"`test` limits the report to one key, `last` keeps the most recent N runs of each " +
			"test, and `since` drops older runs, and when both are given `since` applies first.",
		InputSchema: getTestStatsInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(getTestStatsSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("get_test_stats", err), nil
		}
		var args getTestStatsArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("get_test_stats", err), nil
		}
		filter := history.Filter{}
		if args.Test != nil {
			filter.Key = *args.Test
		}
		if args.Last != nil {
			filter.Window.LastN = *args.Last
		}
		if args.Since != nil {
			ms, ok := history.ParseSince(*args.Since)
			if !ok {
				return invalidArgumentsResult("get_test_stats", fmt.Errorf(
					"since %q is neither an RFC 3339 timestamp nor a YYYY-MM-DD date", *args.Since)), nil
			}
			filter.Window.SinceMs = ms
		}

		report, err := history.Query(tc.History, filter)
		if err != nil {
			tc.Logger.Warn("run.stats_failed", logging.LogContext{"error": err.Error()})
			return errorResult(err), nil
		}
		return jsonToolResult(testStatsWireOf(report))
	})
}

func testStatsWireOf(report history.Result) testStatsResult {
	out := testStatsResult{Ok: true, Tests: make([]testStatsEntryWire, 0, len(report.Tests)), Skipped: report.Skipped,
		InfraFailures: report.Totals.InfraFailures, TestFailures: report.Totals.TestFailures,
		InfraFailureRate: report.Totals.InfraFailureRate,
		AbortedRuns:      report.Totals.AbortedRuns, Attempts: report.Totals.Attempts}
	for _, s := range report.Tests {
		last := make([]string, len(s.LastOutcomes))
		for i, o := range s.LastOutcomes {
			last[i] = string(o)
		}
		out.Tests = append(out.Tests, testStatsEntryWire{
			TestKey: s.Key, Runs: s.Runs, Passed: s.Passed, Failed: s.Failed, Flaky: s.Flaky,
			FlakyRate: s.FlakyRate, LastOutcomes: last,
			InfraFailures: s.InfraFailures, TestFailures: s.TestFailures, InfraFailureRate: s.InfraFailureRate, AbortedRuns: s.AbortedRuns, Attempts: s.Attempts,
			DurationP50Ms: s.DurationP50Ms, DurationP95Ms: s.DurationP95Ms,
		})
	}
	return out
}
