package script

// validateLaunchAppStep rejects the one option combination that
// contradicts itself: clearing an app's data needs the app stopped, so
// it cannot be combined with reusing the running instance.
func validateLaunchAppStep(s *LaunchAppStep, path string, report reportFunc) {
	if s.ClearState != nil && *s.ClearState && s.StopApp != nil && !*s.StopApp {
		report("%s: clearState: true cannot be combined with stopApp: false, because clearing data stops the app", path)
	}
}

// validateAppID rejects an explicitly empty app id. An absent id is
// valid and means the script's own appId.
func validateAppID(appID *string, path string, report reportFunc) {
	if appID != nil && len(*appID) == 0 {
		report("%s.appId: must not be empty", path)
	}
}
