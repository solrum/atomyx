package iosdriver

import "testing"

func TestRunnerAppNameMatchesLauncherBundle(t *testing.T) {
	if RunnerAppName+".app" != xctestRunnerAppName {
		t.Fatalf("RunnerAppName %q and the launcher bundle name %q disagree", RunnerAppName, xctestRunnerAppName)
	}
}
