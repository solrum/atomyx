package script

import "dev.atomyx/internal/driver"

// validateLaunchAppStep rejects the option combinations that
// contradict themselves. Clearing an app's data needs the app stopped,
// so it cannot be combined with reusing the running instance. Launch
// arguments only reach a new start of the app, so they cannot be
// combined with reusing the running instance either, and neither can
// permissions, because a running app may keep its earlier view of them.
// It also rejects an arguments key that cannot reach the app.
func validateLaunchAppStep(s *LaunchAppStep, path string, report reportFunc) {
	if s.ClearState != nil && *s.ClearState && s.StopApp != nil && !*s.StopApp {
		report("%s: clearState: true cannot be combined with stopApp: false, because clearing data stops the app", path)
	}
	if len(s.Arguments) > 0 && s.StopApp != nil && !*s.StopApp {
		report("%s: arguments cannot be combined with stopApp: false, because a reused instance never receives them", path)
	}
	if err := driver.ValidateLaunchArguments(s.Arguments); err != nil {
		report("%s.arguments: %s", path, err.Error())
	}
	if len(s.Permissions) > 0 {
		if s.StopApp != nil && !*s.StopApp {
			report("%s: permissions cannot be combined with stopApp: false, because a reused instance may keep its earlier view of them", path)
		}
		if err := driver.ValidatePermissions(s.Permissions); err != nil {
			report("%s.permissions: %s", path, err.Error())
		}
	}
}

// validateAppID rejects an explicitly empty app id. An absent id is
// valid and means the script's own appId.
func validateAppID(appID *string, path string, report reportFunc) {
	if appID != nil && len(*appID) == 0 {
		report("%s.appId: must not be empty", path)
	}
}
