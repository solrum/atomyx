package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
)

var envRefPattern = regexp.MustCompile(`\$\{ENV:([A-Za-z_][A-Za-z0-9_]*)\}`)

// ResolveEnvRefs substitutes every `${ENV:NAME}` occurrence inside
// each environment's vars values with os.Getenv(NAME), mirroring the
// TypeScript loader's resolveEnvRefs exactly, including its error
// message text: a config value that needs a secret references a
// process environment variable instead of holding it literally, and a
// reference to an unset variable is a hard failure rather than a
// silent empty substitution. Fields outside vars are not scanned:
// those are structural config, not user-authored strings, so silently
// rewriting them would hide misconfiguration instead of failing
// loudly.
func ResolveEnvRefs(cfg AtomyxConfig, path string) (AtomyxConfig, error) {
	if len(cfg.Environments) == 0 {
		return cfg, nil
	}

	environments := make(map[string]EnvironmentEntry, len(cfg.Environments))
	for name, environment := range cfg.Environments {
		if environment.Vars == nil {
			environments[name] = environment
			continue
		}
		resolved, err := resolveVars(*environment.Vars, name, path)
		if err != nil {
			return AtomyxConfig{}, err
		}
		environment.Vars = &resolved
		environments[name] = environment
	}

	resolvedConfig := cfg
	resolvedConfig.Environments = environments
	return resolvedConfig, nil
}

func resolveVars(vars map[string]string, environmentName, path string) (map[string]string, error) {
	resolved := make(map[string]string, len(vars))
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := vars[key]
		var substitutionErr error
		replaced := envRefPattern.ReplaceAllStringFunc(value, func(match string) string {
			if substitutionErr != nil {
				return match
			}
			varName := envRefPattern.FindStringSubmatch(match)[1]
			fromEnv, ok := os.LookupEnv(varName)
			if !ok {
				//nolint:staticcheck // text matched verbatim by the differential test comparing this message against the reference config oracle's output; punctuation here is part of that parity contract, not a style choice
				substitutionErr = fmt.Errorf(
					"%s references environment variable %q in environments.%s.vars.%s, but it is not set.",
					path, varName, environmentName, key,
				)
				return match
			}
			return fromEnv
		})
		if substitutionErr != nil {
			return nil, substitutionErr
		}
		resolved[key] = replaced
	}
	return resolved, nil
}
