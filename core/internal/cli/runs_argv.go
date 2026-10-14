package cli

// RunsCommand identifies a runs-module command this build implements.
type RunsCommand string

const (
	RunsCommandList  RunsCommand = "list"
	RunsCommandStats RunsCommand = "stats"
	RunsCommandPrune RunsCommand = "prune"
)

// runsCommandFlags lists every flag each runs command accepts, in the
// order shown in the "Valid flags: ..." error message.
var runsCommandFlags = map[RunsCommand][]flagDef{
	RunsCommandList: {
		{"--test", flagValue},
		{"--since", flagValue},
		{"--json", flagBoolean},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
	RunsCommandStats: {
		{"--test", flagValue},
		{"--last", flagValue},
		{"--since", flagValue},
		{"--json", flagBoolean},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
	RunsCommandPrune: {
		{"--keep", flagValue},
		{"--older-than", flagValue},
		{"--dry-run", flagBoolean},
		{"--json", flagBoolean},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
}

// ParsedRunsArgv is the result of parsing the argv passed to one
// runs-module command (the tokens after "list", "stats", or "prune").
type ParsedRunsArgv struct {
	Command RunsCommand
	Flags   map[string]FlagValue
	Help    bool
}

// ParseRunsArgv parses the argv passed to a runs-module command against
// that command's flag table in runsCommandFlags, with the same forms and
// errors as ParseSkillsArgv.
func ParseRunsArgv(args []string, command RunsCommand) (ParsedRunsArgv, error) {
	flags, help, err := parseModuleFlags(args, string(command), runsCommandFlags[command])
	if err != nil {
		return ParsedRunsArgv{}, err
	}
	return ParsedRunsArgv{Command: command, Flags: flags, Help: help}, nil
}
