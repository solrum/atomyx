package cli

import (
	"context"
	"fmt"
	"io"

	"dev.atomyx/internal/shutdown"
	"dev.atomyx/internal/version"
)

// Run is the atomyx binary's top-level entry point: it handles
// top-level help and version, resolves a shortcut or a module name,
// and returns the process exit code for the caller to pass to
// os.Exit. It runs with context.Background(). A caller that needs to
// bound or cancel a run uses RunContext directly.
func Run(args []string, deps Deps) int {
	return RunContext(context.Background(), args, deps)
}

// RunContext is Run with an explicit context. When a stop signal
// canceled ctx, a nonzero result becomes the signal's conventional
// exit code, so a run that flushed a partial report still reports the
// interrupt. A command that finished successfully keeps exit code 0.
func RunContext(ctx context.Context, args []string, deps Deps) int {
	code := dispatch(ctx, args, deps)
	if code != 0 {
		if interrupted, ok := shutdown.InterruptExitCode(ctx); ok {
			return interrupted
		}
	}
	return code
}

func dispatch(ctx context.Context, args []string, deps Deps) int {
	stdout := deps.stdout()
	stderr := deps.stderr()

	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printTopLevelHelp(stdout)
		return 0
	}
	if args[0] == "version" || args[0] == "--version" || args[0] == "-v" {
		_, _ = fmt.Fprintf(stdout, "atomyx %s\n", version.Read())
		return 0
	}

	command := args[0]
	rest := args[1:]

	if shortcut, ok := Shortcuts[command]; ok {
		module, resolvedArgs := shortcut(rest)
		if exec, ok := Modules[module]; ok {
			return exec(ctx, deps, resolvedArgs)
		}
	}

	if exec, ok := Modules[command]; ok {
		return exec(ctx, deps, rest)
	}

	_, _ = fmt.Fprintf(stderr, "Unknown command: %q\n\n", command)
	printTopLevelHelp(stderr)
	return 2
}

func printTopLevelHelp(w io.Writer) {
	_, _ = io.WriteString(w, `atomyx: Agentic Test Orchestration Module

USAGE
  atomyx <module> <command> [flags]

MODULES
  driver          Device interaction: run scripts and suites, list
                  devices, check device health.
  skills          Install and update the bundled Claude skill and
                  agent files.
  runs            Inspect and prune recorded runs: list them, report
                  outcome statistics, and delete old ones.

SHORTCUTS
  atomyx run ...             is shorthand for atomyx driver run ...
  atomyx suite ...           is shorthand for atomyx driver suite ...
  atomyx devices             is shorthand for atomyx driver list-devices
  atomyx doctor ...          is shorthand for atomyx driver doctor ...
  atomyx setup ...           is shorthand for atomyx driver setup ...
  atomyx init ...            is shorthand for atomyx skills init ...
  atomyx update-skills ...   is shorthand for atomyx skills update-skills ...

DRIVER COMMANDS
  atomyx driver run --file <path> --platform <ios|android> --device <id>
  atomyx driver suite [<name>] [--tag <name>] [--glob <pattern>]
  atomyx driver list-devices [--json]
  atomyx driver doctor [--platform <ios|android>] [--device <id>]
  atomyx driver doctor --migrate-runs [--report-dir <path>] [--json]
  atomyx driver setup [--platform <ios|android>] [--device <id>] [--check] [--json]

MCP SERVER
  The MCP server ships as a separate binary, atomyx-mcp. It speaks
  the Model Context Protocol over stdio, so an AI client launches it
  directly instead of going through this binary.

EXAMPLES
  atomyx run --file test.yml --platform ios --device <UDID>
  atomyx devices --json
  atomyx driver run --file test.yml --proxy mitmproxy:/tmp/capture.jsonl

VERSION
  atomyx version

SEE ALSO
  https://atomyx.dev
`)
}
