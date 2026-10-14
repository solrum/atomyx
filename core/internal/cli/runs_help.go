package cli

import "io"

// PrintRunsCommandHelp writes the full flag reference for one
// runs-module command to w. This text is user-facing help output, so
// its wording and layout are a stable contract that must not change
// without deliberate intent.
func PrintRunsCommandHelp(command RunsCommand, w io.Writer) {
	switch command {
	case RunsCommandList:
		_, _ = io.WriteString(w, `atomyx runs list [--test <key>] [--since <date>] [--json]
  List recorded runs, newest first, with run id, start time, kind, test
  key, result, and duration. A run that is still being written shows
  the result "running". Runs whose records cannot be read are counted
  in a footer and never listed.

  --test <key>      Only runs of this test key (the script path for a
                    run recorded without a key)
  --since <date>    Only runs started at or after this time, as an
                    RFC 3339 timestamp or a YYYY-MM-DD date (midnight UTC)
  --json            Print one JSON document instead of a table
  --help, -h        Show this message
`)
	case RunsCommandStats:
		_, _ = io.WriteString(w, `atomyx runs stats [--test <key>] [--last <n>] [--since <date>] [--json]
  Report outcome statistics per test key: runs, passed, failed, flaky,
  flaky rate, p50 and p95 duration, and the latest outcomes. A suite
  run counts through its entries, and unfinished, cancelled, and
  interrupted runs are left out. Rows are sorted by flaky rate, then
  by failures, both highest first.

  --test <key>      Only this test key
  --last <n>        Count only the most recent n runs of each test
  --since <date>    Count only runs started at or after this time, as an
                    RFC 3339 timestamp or a YYYY-MM-DD date (midnight UTC)
  --json            Print one JSON document instead of a table
  --help, -h        Show this message

  LAST lists up to 10 outcomes, most recent first: P passed, F failed,
  K flaky (failed, then passed on a retry).
`)
	}
}

// PrintRunsModuleHelp writes the runs module's own top-level usage to
// w. This text is user-facing help output, so its wording and layout
// are a stable contract that must not change without deliberate intent.
func PrintRunsModuleHelp(w io.Writer) {
	_, _ = io.WriteString(w, `atomyx runs: inspect recorded runs

USAGE
  atomyx runs <command> [flags]

COMMANDS
  list             List recorded runs, newest first
  stats            Report pass, fail, and flaky statistics per test
  help             Print this usage and exit

FLAGS (list)
  --test <key>     Only runs of this test key
  --since <date>   Only runs started at or after this time
  --json           Print JSON instead of a table
  --help, -h       Show command help

FLAGS (stats)
  --test <key>     Only this test key
  --last <n>       Count only the most recent n runs of each test
  --since <date>   Count only runs started at or after this time
  --json           Print JSON instead of a table
  --help, -h       Show command help

The runs are read from the storage directory, which is
ATOMYX_STORAGE_DIR when set and ~/.atomyx otherwise.

SEE ALSO
  https://atomyx.dev
`)
}
