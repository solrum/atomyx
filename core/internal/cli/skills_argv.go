package cli

import "strings"

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
	defs := skillsCommandFlags[command]
	flags := make(map[string]FlagValue)

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--help" || arg == "-h" {
			return ParsedSkillsArgv{Command: command, Flags: flags, Help: true}, nil
		}

		key := arg
		var inlineValue *string
		if eqIdx := strings.IndexByte(arg, '='); eqIdx > 0 {
			key = arg[:eqIdx]
			v := arg[eqIdx+1:]
			inlineValue = &v
		}

		kind, known := flagKindFor(defs, key)
		if !known {
			return ParsedSkillsArgv{}, newArgvError(
				"Command %q does not accept %q. Valid flags: %s.",
				string(command), key, flagNameList(defs),
			)
		}

		if inlineValue != nil {
			if *inlineValue == "" {
				return ParsedSkillsArgv{}, newArgvError("Flag %q requires a non-empty value.", key)
			}
			if _, dup := flags[key]; dup {
				return ParsedSkillsArgv{}, newArgvError("Flag %q was specified more than once.", key)
			}
			flags[key] = *inlineValue
			continue
		}

		if kind == flagBoolean {
			if _, dup := flags[key]; dup {
				return ParsedSkillsArgv{}, newArgvError("Flag %q was specified more than once.", key)
			}
			flags[key] = true
			continue
		}

		next := i + 1
		if next >= len(args) || strings.HasPrefix(args[next], "-") {
			return ParsedSkillsArgv{}, newArgvError("Flag %q requires a value.", key)
		}
		if _, dup := flags[key]; dup {
			return ParsedSkillsArgv{}, newArgvError("Flag %q was specified more than once.", key)
		}
		flags[key] = args[next]
		i = next
	}

	return ParsedSkillsArgv{Command: command, Flags: flags, Help: false}, nil
}
