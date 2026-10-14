package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
	"dev.atomyx/internal/storageroot"
)

// ExecuteRuns dispatches one runs-module command: list, or help.
func ExecuteRuns(ctx context.Context, deps Deps, args []string) int {
	var command string
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case string(RunsCommandList):
		return dispatchRunsCommand(ctx, deps, args[1:], RunsCommandList, runsList)
	case "help", "":
		PrintRunsModuleHelp(deps.stdout())
		return 0
	default:
		_, _ = fmt.Fprintf(deps.stderr(), "error: unknown runs command %q\n\n", command)
		PrintRunsModuleHelp(deps.stderr())
		return 2
	}
}

// dispatchRunsCommand parses rest against command's flag table and
// either prints that command's help, runs it, or reports a usage error.
func dispatchRunsCommand(ctx context.Context, deps Deps, rest []string, command RunsCommand, run func(context.Context, Deps, *storage.RunCatalog, string, map[string]FlagValue) int) int {
	argv, err := ParseRunsArgv(rest, command)
	if err != nil {
		var argvErr *ArgvError
		if !errors.As(err, &argvErr) {
			_, _ = fmt.Fprintf(deps.stderr(), "error: %s\n", err)
			return 2
		}
		_, _ = fmt.Fprintf(deps.stderr(), "error: %s\n\n", argvErr.Message)
		PrintRunsCommandHelp(command, deps.stderr())
		return 2
	}
	if argv.Help {
		PrintRunsCommandHelp(command, deps.stdout())
		return 0
	}
	root, err := storageroot.Resolve("")
	if err != nil {
		return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
	}
	store, err := storage.NewFileStorage(storage.FileStorageOptions{Root: root})
	if err != nil {
		return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
	}
	catalog := storage.NewRunCatalog(store, root, runfolder.NewLockLiveness(root))
	return run(ctx, deps, catalog, root, argv.Flags)
}

// runsMisuse reports a misuse of a runs command and returns the misuse
// exit code.
func runsMisuse(deps Deps, format string, args ...any) int {
	_, _ = fmt.Fprintf(deps.stderr(), "error: %s\n", fmt.Sprintf(format, args...))
	return 2
}

// parseSinceFlag reads the optional --since flag as epoch milliseconds,
// accepting an RFC 3339 timestamp or a YYYY-MM-DD date taken as
// midnight UTC. It reports zero when the flag is absent.
func parseSinceFlag(flags map[string]FlagValue) (int64, error) {
	raw, ok := flagString(flags, "--since")
	if !ok {
		return 0, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UnixMilli(), nil
	}
	if t, err := time.Parse(time.DateOnly, raw); err == nil {
		return t.UnixMilli(), nil
	}
	return 0, fmt.Errorf("--since %q is neither an RFC 3339 timestamp nor a YYYY-MM-DD date", raw)
}

// runKey is the test key a run is listed and filtered under: its stored
// test key, else its script path.
func runKey(m runfolder.Meta) string {
	if m.TestKey != "" {
		return m.TestKey
	}
	return m.ScriptPath
}

func formatTimeMs(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func formatDurationMs(ms int64) string {
	return (time.Duration(ms) * time.Millisecond).String()
}

// skippedFooter words the count of unreadable run records, or returns
// the empty string when there are none.
func skippedFooter(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "1 run record could not be read and was skipped."
	}
	return fmt.Sprintf("%d run records could not be read and were skipped.", n)
}

func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
