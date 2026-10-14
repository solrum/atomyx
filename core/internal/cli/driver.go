package cli

import (
	"context"
	"errors"
	"fmt"

	"dev.atomyx/internal/version"
)

// ExecuteDriver dispatches one driver-module command: list-devices,
// run, suite, doctor, inspect, setup, version, or help.
func ExecuteDriver(ctx context.Context, deps Deps, args []string) int {
	argv, err := ParseArgv(args)
	if err != nil {
		var argvErr *ArgvError
		if !errors.As(err, &argvErr) {
			_, _ = fmt.Fprintf(deps.stderr(), "error: %s\n", err)
			return 2
		}
		_, _ = fmt.Fprintf(deps.stderr(), "error: %s\n\n", argvErr.Message)
		if len(args) > 0 {
			if cmd, ok := commandNamed(args[0]); ok {
				PrintCommandHelp(cmd, deps.stderr())
				return 2
			}
		}
		PrintModuleHelp(deps.stderr())
		return 2
	}

	switch argv.Command {
	case CommandListDevices:
		if argv.Help {
			PrintCommandHelp(CommandListDevices, deps.stdout())
			return 0
		}
		return ListDevices(ctx, deps, flagBool(argv.Flags, "--json"), deps.stdout())
	case CommandRun:
		if argv.Help {
			PrintCommandHelp(CommandRun, deps.stdout())
			return 0
		}
		return RunScript(ctx, deps, argv.Flags)
	case CommandSuite:
		if argv.Help {
			PrintCommandHelp(CommandSuite, deps.stdout())
			return 0
		}
		return RunSuite(ctx, deps, argv.Flags, argv.Positional)
	case CommandDoctor:
		if argv.Help {
			PrintCommandHelp(CommandDoctor, deps.stdout())
			return 0
		}
		return Doctor(ctx, deps, argv.Flags)
	case CommandInspect:
		if argv.Help {
			PrintCommandHelp(CommandInspect, deps.stdout())
			return 0
		}
		return Inspect(ctx, deps, argv.Flags)
	case CommandSetup:
		if argv.Help {
			PrintCommandHelp(CommandSetup, deps.stdout())
			return 0
		}
		return Setup(ctx, deps, argv.Flags)
	case CommandVersion:
		_, _ = fmt.Fprintf(deps.stdout(), "atomyx %s\n", version.Read())
		return 0
	case CommandHelp:
		PrintModuleHelp(deps.stdout())
		return 0
	default:
		// ParseArgv only ever returns a Command from validCommands, so
		// this is unreachable. Panic instead of silently rendering
		// help, so a Command added to validCommands without a case
		// here fails loudly instead of masquerading as --help.
		panic(fmt.Sprintf("cli: ParseArgv returned unregistered command %q", argv.Command))
	}
}
