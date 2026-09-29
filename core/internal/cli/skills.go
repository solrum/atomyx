package cli

import (
	"context"
	"errors"
	"fmt"
)

// ExecuteSkills dispatches one skills-module command: init,
// update-skills, or help.
func ExecuteSkills(ctx context.Context, deps Deps, args []string) int {
	var command string
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case string(SkillsCommandInit):
		return dispatchSkillsCommand(deps, args[1:], SkillsCommandInit, RunInit)
	case string(SkillsCommandUpdateSkills):
		return dispatchSkillsCommand(deps, args[1:], SkillsCommandUpdateSkills, RunUpdateSkills)
	case "help", "":
		PrintSkillsModuleHelp(deps.stdout())
		return 0
	default:
		_, _ = fmt.Fprintf(deps.stderr(), "error: unknown skills command %q\n\n", command)
		PrintSkillsModuleHelp(deps.stderr())
		return 2
	}
}

// dispatchSkillsCommand parses rest against command's flag table and
// either prints that command's help, runs it, or reports a usage
// error. This is the shared shape of the init and update-skills branches in
// ExecuteSkills.
func dispatchSkillsCommand(deps Deps, rest []string, command SkillsCommand, run func(Deps, map[string]FlagValue) int) int {
	argv, err := ParseSkillsArgv(rest, command)
	if err != nil {
		var argvErr *ArgvError
		if !errors.As(err, &argvErr) {
			_, _ = fmt.Fprintf(deps.stderr(), "error: %s\n", err)
			return 2
		}
		_, _ = fmt.Fprintf(deps.stderr(), "error: %s\n\n", argvErr.Message)
		PrintSkillsCommandHelp(command, deps.stderr())
		return 2
	}
	if argv.Help {
		PrintSkillsCommandHelp(command, deps.stdout())
		return 0
	}
	return run(deps, argv.Flags)
}
