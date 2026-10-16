package driver

import (
	"fmt"
	"sort"
	"strings"

	"dev.atomyx/internal/diag"
)

// ValidateLaunchArguments rejects an arguments map whose keys or values
// cannot travel to an app on every platform. A key must be non-empty and
// free of "=" and NUL, because iOS carries each pair as an environment
// variable and a launch argument name. A value must be free of NUL. The
// error carries diag.CodeInvalidArgument and names the offending key.
func ValidateLaunchArguments(arguments map[string]string) error {
	for _, key := range SortedLaunchArgumentKeys(arguments) {
		switch {
		case key == "":
			return invalidLaunchArgument("launch argument keys must not be empty")
		case strings.ContainsAny(key, "=\x00"):
			return invalidLaunchArgument(fmt.Sprintf("launch argument key %q must not contain '=' or NUL", key))
		case strings.Contains(arguments[key], "\x00"):
			return invalidLaunchArgument(fmt.Sprintf("launch argument %q has a value containing NUL", key))
		}
	}
	return nil
}

// SortedLaunchArgumentKeys returns the keys of arguments in ascending
// order, so every adapter delivers a map in one deterministic sequence.
func SortedLaunchArgumentKeys(arguments map[string]string) []string {
	keys := make([]string, 0, len(arguments))
	for key := range arguments {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func invalidLaunchArgument(message string) error {
	return diag.New(diag.CodeInvalidArgument, message, diag.InvalidArgumentRemedy, nil)
}
