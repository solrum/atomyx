package iosdriver

import "dev.atomyx/internal/driver"

// launchParams builds the runner's launchApp request for bundleID.
//
// Each launch argument is delivered twice, because apps read launch
// input in two ways. It becomes the launch environment entry NAME=value
// and the launch argument pair "-NAME" "value", which the app's standard
// user defaults read as a registered override. Explicit Environment
// entries win over an Arguments entry of the same name, and explicit
// Args follow the pairs derived from Arguments.
func launchParams(bundleID string, noReset bool, args driver.LaunchArgs) (map[string]any, error) {
	if err := driver.ValidateLaunchArguments(args.Arguments); err != nil {
		return nil, err
	}
	params := map[string]any{"bundleId": bundleID, "noReset": noReset}

	var launchArguments []string
	environment := make(map[string]string, len(args.Arguments)+len(args.Environment))
	for _, key := range driver.SortedLaunchArgumentKeys(args.Arguments) {
		value := args.Arguments[key]
		launchArguments = append(launchArguments, "-"+key, value)
		environment[key] = value
	}
	launchArguments = append(launchArguments, args.Args...)
	for key, value := range args.Environment {
		environment[key] = value
	}

	if len(launchArguments) > 0 {
		params["launchArguments"] = launchArguments
	}
	if len(environment) > 0 {
		params["launchEnvironment"] = environment
	}
	return params, nil
}
