package cli

// SkillsCommand identifies a skills-module command this build
// implements.
type SkillsCommand string

const (
	SkillsCommandInit         SkillsCommand = "init"
	SkillsCommandUpdateSkills SkillsCommand = "update-skills"
)

// skillsCommandFlags lists every flag each skills command accepts, in
// the order shown in the "Valid flags: ..." error message.
var skillsCommandFlags = map[SkillsCommand][]flagDef{
	SkillsCommandInit: {
		{"--target", flagValue},
		{"--force", flagBoolean},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
	SkillsCommandUpdateSkills: {
		{"--target", flagValue},
		{"--help", flagBoolean},
		{"-h", flagBoolean},
	},
}

// ParsedSkillsArgv is the result of parsing the argv passed to one
// skills-module command (the tokens after "init" or "update-skills").
type ParsedSkillsArgv struct {
	Command SkillsCommand
	Flags   map[string]FlagValue
	Help    bool
}

// ParseSkillsArgv parses the argv passed to a skills-module command
// against that command's flag table in skillsCommandFlags. It accepts
// both "--flag value" and "--flag=value" forms. A value flag treats
// the next token as missing when that token starts with a single
// dash, not "--" as the driver module's parser requires, since a
// skills command flag never takes a value that itself looks like
// another flag. It returns a non-nil *ArgvError for an unrecognized
// flag, a value flag given with no value, an empty "--flag=" value,
// or a flag given more than once.
func ParseSkillsArgv(args []string, command SkillsCommand) (ParsedSkillsArgv, error) {
	flags, help, err := parseModuleFlags(args, string(command), skillsCommandFlags[command])
	if err != nil {
		return ParsedSkillsArgv{}, err
	}
	return ParsedSkillsArgv{Command: command, Flags: flags, Help: help}, nil
}
