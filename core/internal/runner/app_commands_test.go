package runner

import (
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
)

// newConfiguredRunner builds a runner over a mock driver that has
// com.test and com.other installed, with the given run-level launch
// policy.
func newConfiguredRunner(noReset *bool, clearData bool, artifact *string) (*ScriptRunner, *mock.Driver) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}, {BundleID: "com.other"}}
	d.StageHierarchy(rootTree())
	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	r := NewScriptRunner(ScriptRunnerDeps{
		Orchestra:        orch,
		Clock:            clock,
		Logger:           noopLogger{},
		Commands:         DefaultCommands(),
		LaunchNoReset:    noReset,
		ClearDataOnFresh: clearData,
		AppArtifact:      artifact,
	})
	return r, d
}

func runConfigured(t *testing.T, r *ScriptRunner, yaml string) ScriptResult {
	t.Helper()
	result, err := r.Run(context.Background(), mustParse(t, yaml))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

func driverCalls(d *mock.Driver, method string) []mock.Call {
	var out []mock.Call
	for _, c := range d.Calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func TestAppLifecycleCommands_ActOnTheScriptApp(t *testing.T) {
	cases := []struct {
		step       string
		method     string
		wantDetail string
	}{
		{"stopApp", "StopApp", "Stopped com.test"},
		{"killApp", "KillApp", "Killed com.test"},
		{"clearState", "ClearAppData", "Cleared state of com.test"},
	}
	for _, tc := range cases {
		t.Run(tc.step, func(t *testing.T) {
			r, d := newConfiguredRunner(nil, false, nil)
			result := runConfigured(t, r, "appId: com.test\n---\n- "+tc.step+"\n")
			if !result.Ok {
				t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
			}
			if got := detail(lastStep(t, result)); got != tc.wantDetail {
				t.Errorf("detail = %q, want %q", got, tc.wantDetail)
			}
			calls := driverCalls(d, tc.method)
			if len(calls) != 1 || calls[0].Args[0] != "com.test" {
				t.Errorf("got %s calls %v, want one for com.test", tc.method, calls)
			}
		})
	}
}

func TestAppLifecycleCommands_ExplicitAppIDOverridesTheScript(t *testing.T) {
	forms := []string{
		"- stopApp: com.other\n",
		"- stopApp:\n    appId: com.other\n",
	}
	for _, form := range forms {
		r, d := newConfiguredRunner(nil, false, nil)
		result := runConfigured(t, r, "appId: com.test\n---\n"+form)
		if !result.Ok {
			t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
		}
		calls := driverCalls(d, "StopApp")
		if len(calls) != 1 || calls[0].Args[0] != "com.other" {
			t.Errorf("form %q: got StopApp calls %v, want one for com.other", form, calls)
		}
	}
}

func TestAppLifecycleCommands_NoAppAnywhereFails(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "name: no app\n---\n- stopApp\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want a failure")
	}
	if got := detail(lastStep(t, result)); got != noAppIDDetail {
		t.Errorf("detail = %q, want %q", got, noAppIDDetail)
	}
	if n := len(driverCalls(d, "StopApp")); n != 0 {
		t.Errorf("got %d StopApp calls, want 0", n)
	}
}

func TestAppLifecycleCommands_MalformedAppIDFailsTheStep(t *testing.T) {
	r, _ := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- killApp: \"not valid; rm -rf\"\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want a failure")
	}
	if got := lastStep(t, result); got.Code == "" {
		t.Errorf("failure carries no diag code, detail %q", detail(got))
	}
}

func TestAppLifecycleCommands_DetailKeepsThePlaceholder(t *testing.T) {
	r, _ := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\nenv:\n  target: com.other\n---\n- stopApp: ${target}\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Stopped ${target}" {
		t.Errorf("detail = %q, want the placeholder as written", got)
	}
}

func TestClearStateCommand_PassesTheConfiguredArtifact(t *testing.T) {
	artifact := "/builds/app.ipa"
	r, d := newConfiguredRunner(nil, false, &artifact)
	result := runConfigured(t, r, "appId: com.test\n---\n- clearState\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	calls := driverCalls(d, "ClearAppData")
	if len(calls) != 1 || calls[0].Args[1] != artifact {
		t.Errorf("got ClearAppData calls %v, want the artifact %q", calls, artifact)
	}
}

func TestStopApp_LetsALaterLaunchRunAgain(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- launchApp\n- stopApp\n- launchApp\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if len(result.Steps) != 3 {
		t.Fatalf("got %d steps, want 3 because the relaunch after a stop must not be skipped: %s", len(result.Steps), debugSteps(result.Steps))
	}
	if n := len(driverCalls(d, "LaunchApp")); n != 2 {
		t.Errorf("got %d LaunchApp calls, want 2", n)
	}
}

func noResetOf(t *testing.T, d *mock.Driver) string {
	t.Helper()
	calls := driverCalls(d, "LaunchApp")
	if len(calls) != 1 {
		t.Fatalf("got %d LaunchApp calls, want 1", len(calls))
	}
	args := calls[0].Args[1].(driver.LaunchArgs)
	switch {
	case args.NoReset == nil:
		return "nil"
	case *args.NoReset:
		return "true"
	}
	return "false"
}

func TestLaunchAppCommand_StepOptionsOverrideTheRunPolicy(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name      string
		noReset   *bool
		clearData bool
		options   string
		wantMode  string
		wantClear int
	}{
		{"defaults are unchanged", nil, false, "", "nil", 0},
		{"configured fresh with clear keeps clearing", &no, true, "", "false", 1},
		{"stopApp false attaches", nil, false, "{stopApp: false}", "true", 0},
		{"stopApp false beats a configured fresh launch", &no, true, "{stopApp: false}", "true", 0},
		{"stopApp true is fresh without clearing", nil, false, "{stopApp: true}", "false", 0},
		{"clearState true is fresh and clears", nil, false, "{clearState: true}", "false", 1},
		{"clearState true beats configured attach", &yes, false, "{clearState: true}", "false", 1},
		{"clearState false suppresses the configured clear", &no, true, "{clearState: false}", "false", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, d := newConfiguredRunner(tc.noReset, tc.clearData, nil)
			step := "- launchApp\n"
			if tc.options != "" {
				step = "- launchApp: " + tc.options + "\n"
			}
			result := runConfigured(t, r, "appId: com.test\n---\n"+step)
			if !result.Ok {
				t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
			}
			if got := noResetOf(t, d); got != tc.wantMode {
				t.Errorf("NoReset = %s, want %s", got, tc.wantMode)
			}
			if got := len(driverCalls(d, "ClearAppData")); got != tc.wantClear {
				t.Errorf("got %d ClearAppData calls, want %d", got, tc.wantClear)
			}
		})
	}
}

func TestLaunchAppCommand_ClearStateRunsBeforeTheLaunch(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- launchApp:\n    clearState: true\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	var order []string
	for _, c := range d.Calls {
		if c.Method == "ClearAppData" || c.Method == "LaunchApp" {
			order = append(order, c.Method)
		}
	}
	if strings.Join(order, ",") != "ClearAppData,LaunchApp" {
		t.Errorf("call order = %v, want ClearAppData then LaunchApp", order)
	}
}

func TestLaunchAppCommand_StepOptionsDecideTheDedupSkip(t *testing.T) {
	cases := []struct {
		name      string
		options   string
		wantSteps int
	}{
		{"default relaunch is skipped", "", 1},
		{"attach relaunch is skipped", "{stopApp: false}", 1},
		{"stopApp true relaunch runs", "{stopApp: true}", 2},
		{"clearState relaunch runs", "{clearState: true}", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newConfiguredRunner(nil, false, nil)
			second := "- launchApp\n"
			if tc.options != "" {
				second = "- launchApp: " + tc.options + "\n"
			}
			result := runConfigured(t, r, "appId: com.test\n---\n- launchApp\n"+second)
			if !result.Ok {
				t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
			}
			if len(result.Steps) != tc.wantSteps {
				t.Errorf("got %d executed steps, want %d: %s", len(result.Steps), tc.wantSteps, debugSteps(result.Steps))
			}
		})
	}
}

func TestSummarizeStep_AppSteps(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		{"- launchApp\n", "launchApp"},
		{"- launchApp:\n    clearState: true\n    stopApp: true\n", "launchApp: clearState=true, stopApp=true"},
		{"- stopApp\n", "stopApp"},
		{"- killApp: com.other\n", `killApp: "com.other"`},
		{"- clearState:\n    appId: com.other\n", `clearState: "com.other"`},
	}
	for _, tc := range cases {
		def := mustParse(t, "appId: com.test\n---\n"+tc.yaml)
		if got := SummarizeStep(def.Steps[0]).Text; got != tc.want {
			t.Errorf("summary of %q = %q, want %q", tc.yaml, got, tc.want)
		}
	}
}
