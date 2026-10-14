package config

import (
	"fmt"
	"sort"
	"strings"
)

// SelectEnvironment picks one environment out of loaded, or handles
// the absence of one (loaded is nil when no config file was found).
// Every error message it returns is displayed to the user verbatim,
// so the exact wording here is part of the function's public
// contract.
//
// hasName distinguishes an omitted --env from an explicitly given
// name; name is only meaningful when hasName is true.
//
// When hasName is true, that environment is selected. It returns an
// error when: name contains "=", which signals the retired
// KEY=VALUE variable-injection syntax that --var now owns; no config
// was found at all; or the named environment is not declared in the
// config that was found.
//
// When hasName is false, a config with exactly one environment
// resolves to it. No config, or a config with zero or several
// environments, returns (nil, nil) instead of an error, so a run
// that never asked for an environment falls back to CLI flags.
func SelectEnvironment(loaded *LoadedConfig, name string, hasName bool) (*ResolvedEnvironment, error) {
	if hasName && strings.Contains(name, "=") {
		//nolint:staticcheck // text matched verbatim by the differential test comparing this message against the reference config oracle's output; punctuation and casing here are part of that parity contract, not a style choice
		return nil, fmt.Errorf(
			"--env now selects a named environment from the project config, "+
				"and no longer injects a variable. Use --var %s to inject a variable.",
			name,
		)
	}

	if loaded == nil {
		if hasName {
			//nolint:staticcheck // text matched verbatim by the differential test comparing this message against the reference config oracle's output; punctuation here is part of that parity contract, not a style choice
			return nil, fmt.Errorf(
				"--env %q was given, but no atomyx.config.yaml or atomyx.config.yml was found.",
				name,
			)
		}
		return nil, nil
	}

	var selected string
	switch {
	case hasName:
		if _, ok := loaded.Config.Environments[name]; !ok {
			//nolint:staticcheck // text matched verbatim by the differential test comparing this message against the reference config oracle's output; punctuation and casing here are part of that parity contract, not a style choice
			return nil, fmt.Errorf(
				"Environment %q not found in %s. Available environments: %s.",
				name, loaded.Path, availableEnvironments(loaded.Config),
			)
		}
		selected = name
	case len(loaded.Config.Environments) == 1:
		for only := range loaded.Config.Environments {
			selected = only
		}
	default:
		return nil, nil
	}

	environment := loaded.Config.Environments[selected]
	var app *AppEntry
	for i := range loaded.Config.Apps {
		if loaded.Config.Apps[i].ID == environment.App {
			app = &loaded.Config.Apps[i]
			break
		}
	}
	if app == nil {
		// Unreachable given a LoadedConfig built by ParseAndValidate:
		// its top-level superRefine equivalent already rejects any
		// document where an environment's app id is undeclared.
		return nil, fmt.Errorf("environments.%s.app references unknown app id %q", selected, environment.App)
	}

	var vars map[string]string
	if environment.Vars != nil {
		vars = *environment.Vars
	} else {
		vars = map[string]string{}
	}

	return &ResolvedEnvironment{
		Name:         selected,
		AppID:        app.AppID,
		Vars:         vars,
		EnvRefVars:   environment.EnvRefVars,
		EnvRefValues: environment.EnvRefValues,
		Platform:     app.Platform,
		DeviceID:     environment.Device,
		Proxy:        environment.Proxy,
		Artifact:     app.Artifact,
	}, nil
}

// availableEnvironments renders loaded.Config's environment names in
// their source declaration order. A plain Go map has no iteration
// order of its own, so EnvironmentOrder is what makes this list
// reproducible across runs instead of merely deterministic within
// one.
func availableEnvironments(cfg AtomyxConfig) string {
	names := cfg.EnvironmentOrder
	if len(names) != len(cfg.Environments) {
		// EnvironmentOrder is only populated by the YAML decode path;
		// a config built any other way (as every SelectEnvironment
		// unit test in this package does) falls back to a sorted
		// list so the message is still deterministic.
		names = make([]string, 0, len(cfg.Environments))
		for name := range cfg.Environments {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
