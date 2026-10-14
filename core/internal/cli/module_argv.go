package cli

import "strings"

// parseModuleFlags parses the argv of one command of a flag-only module
// (a module whose commands take no positional argument) against defs.
// It accepts both "--flag value" and "--flag=value" forms, and stops at
// "--help" or "-h" with help set. A value flag treats the next token as
// missing when that token starts with a dash, since no such flag takes
// a value that itself looks like another flag. It returns a non-nil
// *ArgvError for an unrecognized flag, a value flag given with no
// value, an empty "--flag=" value, or a flag given more than once.
func parseModuleFlags(args []string, command string, defs []flagDef) (flags map[string]FlagValue, help bool, err error) {
	flags = make(map[string]FlagValue)

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--help" || arg == "-h" {
			return flags, true, nil
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
			return nil, false, newArgvError(
				"Command %q does not accept %q. Valid flags: %s.",
				command, key, flagNameList(defs),
			)
		}

		if inlineValue != nil {
			if *inlineValue == "" {
				return nil, false, newArgvError("Flag %q requires a non-empty value.", key)
			}
			if _, dup := flags[key]; dup {
				return nil, false, newArgvError("Flag %q was specified more than once.", key)
			}
			flags[key] = *inlineValue
			continue
		}

		if kind == flagBoolean {
			if _, dup := flags[key]; dup {
				return nil, false, newArgvError("Flag %q was specified more than once.", key)
			}
			flags[key] = true
			continue
		}

		next := i + 1
		if next >= len(args) || strings.HasPrefix(args[next], "-") {
			return nil, false, newArgvError("Flag %q requires a value.", key)
		}
		if _, dup := flags[key]; dup {
			return nil, false, newArgvError("Flag %q was specified more than once.", key)
		}
		flags[key] = args[next]
		i = next
	}

	return flags, false, nil
}
