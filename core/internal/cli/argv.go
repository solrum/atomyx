package cli

import (
	"fmt"
	"strings"
)

// ArgvError reports a usage mistake in the argv the caller passed to
// ParseArgv: an unrecognized command, an unrecognized flag for the
// selected command, or a value flag given with no value.
type ArgvError struct {
	Message string
}

func (e *ArgvError) Error() string { return e.Message }

func newArgvError(format string, args ...any) *ArgvError {
	return &ArgvError{Message: fmt.Sprintf(format, args...)}
}

// Command identifies a driver-module command this build implements.
type Command string

const (
	CommandListDevices Command = "list-devices"
	CommandRun         Command = "run"
	CommandSuite       Command = "suite"
	CommandDoctor      Command = "doctor"
	CommandInspect     Command = "inspect"
	CommandVersion     Command = "version"
	CommandHelp        Command = "help"
)

// FlagValue is the parsed value of one flag: a bool for a "boolean"
// flag, a string for a "value" flag (including a boolean flag given
// in "--flag=value" form, which stores the raw string rather than a
// parsed bool), or a []string for a "repeatable" flag.
type FlagValue any

// commandsWithPositional lists the commands that accept one leading
// positional argument before their flags. "suite" is the only one:
// its positional argument names a suite declared in the project
// config.
var commandsWithPositional = map[Command]bool{CommandSuite: true}

// flagKind classifies how a flag consumes argv tokens: "boolean" is
// present or absent with no value, "value" consumes the next token
// (or the "--flag=value" suffix) and a repeated occurrence overwrites
// the previous one, and "repeatable" consumes tokens the same way but
// accumulates every occurrence into a []string instead of overwriting
// it. "--var" is the only repeatable flag, passed once per KEY=VALUE
// pair.
type flagKind string

const (
	flagBoolean    flagKind = "boolean"
	flagValue      flagKind = "value"
	flagRepeatable flagKind = "repeatable"
)

// flagDef is one entry in a command's flag table.
type flagDef struct {
	name string
	kind flagKind
}

// validCommands lists the commands this build recognizes. The order
// here is the order shown in the "Valid commands: ..." error message,
// so keep it stable rather than reordering for convenience.
var validCommands = []Command{CommandListDevices, CommandRun, CommandSuite, CommandDoctor, CommandInspect, CommandVersion, CommandHelp}

// commandFlags lists every flag each command accepts, in the order
// shown in the "Valid flags: ..." error message, and how each flag
// consumes argv tokens. "version" and "help" accept none, matching
// the empty flag table a command with no entry here falls back to.
var commandFlags = map[Command][]flagDef{
	CommandListDevices: {
		{"--json", flagBoolean},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
	CommandRun: {
		{"--file", flagValue},
		{"--platform", flagValue},
		{"--device", flagValue},
		{"--proxy", flagValue},
		{"--json", flagBoolean},
		{"--var", flagRepeatable},
		{"--var-file", flagValue},
		{"--config", flagValue},
		{"--env", flagValue},
		{"--preconditions", flagValue},
		{"--skip-preconditions", flagBoolean},
		{"--report", flagValue},
		{"--report-dir", flagValue},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
	CommandSuite: {
		{"--tag", flagRepeatable},
		{"--glob", flagRepeatable},
		{"--config", flagValue},
		{"--env", flagValue},
		{"--var", flagRepeatable},
		{"--var-file", flagValue},
		{"--preconditions", flagValue},
		{"--skip-preconditions", flagBoolean},
		{"--report", flagValue},
		{"--report-dir", flagValue},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
	CommandDoctor: {
		{"--platform", flagValue},
		{"--device", flagValue},
		{"--json", flagBoolean},
		{"--migrate-runs", flagBoolean},
		{"--bundle", flagValue},
		{"--report-dir", flagValue},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
	CommandInspect: {
		{"--platform", flagValue},
		{"--device", flagValue},
		{"--no-open", flagBoolean},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
}

// ParsedArgv is the result of parsing the argv passed to the driver
// module.
type ParsedArgv struct {
	Command Command
	Flags   map[string]FlagValue
	// Positional is the leading positional argument, for a command in
	// commandsWithPositional. nil when the command accepts none, or
	// accepts one but none was given.
	Positional *string
	// Help is true when --help or -h was passed after the command
	// name.
	Help bool
}

// ParseArgv parses the argv passed to the driver module (the tokens
// after "driver" on the command line) against validCommands and
// commandFlags. It accepts both "--flag value" and "--flag=value"
// forms. A boolean flag given in "--flag=value" form stores the raw
// string rather than a parsed bool, so a caller must go through
// isTruthy (or an equivalent check) rather than a direct type
// assertion to bool. It returns a non-nil *ArgvError for an
// unrecognized command, an unrecognized flag for the selected
// command, or a value flag given with no value.
func ParseArgv(args []string) (ParsedArgv, error) {
	if len(args) == 0 {
		return ParsedArgv{Command: CommandHelp, Flags: map[string]FlagValue{}, Help: true}, nil
	}

	first := args[0]
	if first == "--help" || first == "-h" {
		return ParsedArgv{Command: CommandHelp, Flags: map[string]FlagValue{}, Help: true}, nil
	}
	if first == "--version" || first == "-v" {
		return ParsedArgv{Command: CommandVersion, Flags: map[string]FlagValue{}, Help: false}, nil
	}

	command, ok := commandNamed(first)
	if !ok {
		names := make([]string, len(validCommands))
		for i, c := range validCommands {
			names[i] = string(c)
		}
		return ParsedArgv{}, newArgvError(
			"Unknown command %q. Valid commands: %s. "+
				"The MCP server is a separate command named atomyx-mcp.",
			first, strings.Join(names, ", "),
		)
	}

	defs := commandFlags[command]
	flags := make(map[string]FlagValue)

	argStart := 1
	var positional *string
	if commandsWithPositional[command] {
		// "-h" and "--help" must reach the help check below rather than
		// bind as the command's positional argument, matching how
		// ParseSkillsArgv treats them.
		if len(args) > 1 && !strings.HasPrefix(args[1], "--") && args[1] != "-h" {
			p := args[1]
			positional = &p
			argStart = 2
		}
	}

	for i := argStart; i < len(args); i++ {
		arg := args[i]

		key := arg
		var inlineValue *string
		if eqIdx := strings.IndexByte(arg, '='); eqIdx > 0 {
			key = arg[:eqIdx]
			v := arg[eqIdx+1:]
			inlineValue = &v
		}

		kind, known := flagKindFor(defs, key)
		if !known {
			return ParsedArgv{}, newArgvError(
				"Command %q does not accept %q. Valid flags: %s.",
				string(command), key, flagNameList(defs),
			)
		}

		if inlineValue != nil {
			setFlag(flags, key, kind, *inlineValue)
			continue
		}

		if kind == flagBoolean {
			flags[key] = true
			continue
		}

		next := i + 1
		if next >= len(args) || strings.HasPrefix(args[next], "--") {
			return ParsedArgv{}, newArgvError("Flag %q requires a value.", key)
		}
		setFlag(flags, key, kind, args[next])
		i = next
	}

	return ParsedArgv{
		Command:    command,
		Flags:      flags,
		Positional: positional,
		Help:       isTruthy(flags["--help"]) || isTruthy(flags["-h"]),
	}, nil
}

func commandNamed(name string) (Command, bool) {
	for _, c := range validCommands {
		if string(c) == name {
			return c, true
		}
	}
	return "", false
}

func flagKindFor(defs []flagDef, name string) (flagKind, bool) {
	for _, d := range defs {
		if d.name == name {
			return d.kind, true
		}
	}
	return "", false
}

func flagNameList(defs []flagDef) string {
	if len(defs) == 0 {
		return "(none)"
	}
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.name
	}
	return strings.Join(names, ", ")
}

func setFlag(flags map[string]FlagValue, key string, kind flagKind, value string) {
	if kind != flagRepeatable {
		flags[key] = value
		return
	}
	existing, _ := flags[key].([]string)
	flags[key] = append(existing, value)
}

// isTruthy reports whether a parsed flag value should be treated as
// set: a bool carries its own value, a non-empty string counts as
// true regardless of its content (so "--flag=false" still counts as
// set, since the flag was given a value at all), and an absent or
// unrecognized value counts as false.
func isTruthy(v FlagValue) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != ""
	default:
		return false
	}
}
