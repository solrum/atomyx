package script

import (
	"strings"
	"testing"
)

func TestParseScript_LaunchAppOptions(t *testing.T) {
	cases := []struct {
		name         string
		yaml         string
		wantClear    *bool
		wantStopApp  *bool
		wantAnyValue bool
	}{
		{name: "bare", yaml: "- launchApp\n"},
		{name: "null value", yaml: "- launchApp:\n"},
		{name: "clearState true", yaml: "- launchApp:\n    clearState: true\n", wantClear: boolPtr(true)},
		{name: "stopApp false", yaml: "- launchApp:\n    stopApp: false\n", wantStopApp: boolPtr(false)},
		{name: "both", yaml: "- launchApp:\n    clearState: false\n    stopApp: true\n", wantClear: boolPtr(false), wantStopApp: boolPtr(true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := mustParse(t, tc.yaml).Steps[0].(*LaunchAppStep)
			if !boolPtrEqual(step.ClearState, tc.wantClear) {
				t.Errorf("clearState = %v, want %v", step.ClearState, tc.wantClear)
			}
			if !boolPtrEqual(step.StopApp, tc.wantStopApp) {
				t.Errorf("stopApp = %v, want %v", step.StopApp, tc.wantStopApp)
			}
		})
	}
}

func TestParseScript_LaunchAppOptionFromPlaceholder(t *testing.T) {
	def, err := ParseScript("appId: com.x\nname: n\nenv:\n  clean: \"true\"\n---\n- launchApp:\n    clearState: ${clean}\n", nil)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	if got := def.Steps[0].(*LaunchAppStep).ClearState; got == nil || !*got {
		t.Errorf("clearState = %v, want true", got)
	}
}

func TestParseScript_LaunchAppRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{name: "not an object", yaml: "- launchApp: now\n", want: "launchApp: expected no value or an object"},
		{name: "clearState not a boolean", yaml: "- launchApp:\n    clearState: maybe\n", want: "launchApp.clearState: must be true or false"},
		{name: "stopApp not a boolean", yaml: "- launchApp:\n    stopApp: 3\n", want: "launchApp.stopApp: must be true or false"},
		{name: "clearState with attach", yaml: "- launchApp:\n    clearState: true\n    stopApp: false\n", want: "clearState: true cannot be combined with stopApp: false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScript(tc.yaml, nil)
			if err == nil {
				t.Fatalf("ParseScript accepted %q", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseScript_AppLifecycleSteps(t *testing.T) {
	commands := map[string]func(ScriptStep) *string{
		CommandStopApp:    func(s ScriptStep) *string { return s.(*StopAppStep).AppID },
		CommandKillApp:    func(s ScriptStep) *string { return s.(*KillAppStep).AppID },
		CommandClearState: func(s ScriptStep) *string { return s.(*ClearStateStep).AppID },
	}
	for command, appIDOf := range commands {
		t.Run(command, func(t *testing.T) {
			forms := map[string]string{
				"bare":        "- " + command + "\n",
				"null":        "- " + command + ":\n",
				"empty map":   "- " + command + ": {}\n",
				"string":      "- " + command + ": com.other\n",
				"object":      "- " + command + ":\n    appId: com.other\n",
				"placeholder": "- " + command + ": ${other}\n",
			}
			want := map[string]string{"string": "com.other", "object": "com.other", "placeholder": "com.other"}
			for form, body := range forms {
				def, err := ParseScript("appId: com.x\nname: n\nenv:\n  other: com.other\n---\n"+body, nil)
				if err != nil {
					t.Fatalf("%s: ParseScript: %v", form, err)
				}
				step := def.Steps[0]
				if step.StepCommand() != command {
					t.Fatalf("%s: got command %q", form, step.StepCommand())
				}
				got := appIDOf(step)
				switch expected, set := want[form]; {
				case set && (got == nil || *got != expected):
					t.Errorf("%s: appId = %v, want %q", form, got, expected)
				case !set && got != nil:
					t.Errorf("%s: appId = %q, want none", form, *got)
				}
			}
		})
	}
}

func TestParseScript_AppLifecycleRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{name: "empty app id", yaml: "- stopApp: \"\"\n", want: "appId: must not be empty"},
		{name: "number app id", yaml: "- killApp: 12\n", want: "killApp: appId must be a string"},
		{name: "list app id", yaml: "- clearState:\n    appId: [a]\n", want: "clearState: appId must be a string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScript(tc.yaml, nil)
			if err == nil {
				t.Fatalf("ParseScript accepted %q", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func boolPtr(v bool) *bool { return &v }

func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestParseScript_LaunchAppArguments(t *testing.T) {
	def := mustParse(t, "- launchApp:\n    arguments:\n      route: /focus\n      retries: 3\n      ratio: 1.5\n      debug: true\n")
	want := map[string]string{"route": "/focus", "retries": "3", "ratio": "1.5", "debug": "true"}
	got := def.Steps[0].(*LaunchAppStep).Arguments
	if len(got) != len(want) {
		t.Fatalf("arguments = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("arguments[%s] = %q, want %q", key, got[key], value)
		}
	}
}

func TestParseScript_LaunchAppArgumentsRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{name: "not a mapping", yaml: "- launchApp:\n    arguments: route\n", want: "launchApp.arguments: must be a mapping"},
		{name: "list value", yaml: "- launchApp:\n    arguments:\n      route: [a]\n", want: "launchApp.arguments.route: must be a string, number or boolean"},
		{name: "null value", yaml: "- launchApp:\n    arguments:\n      route:\n", want: "launchApp.arguments.route: must be a string, number or boolean"},
		{name: "with attach", yaml: "- launchApp:\n    stopApp: false\n    arguments:\n      route: /x\n", want: "arguments cannot be combined with stopApp: false"},
		{name: "key with equals", yaml: "- launchApp:\n    arguments:\n      \"a=b\": x\n", want: "must not contain '='"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScript(tc.yaml, nil)
			if err == nil {
				t.Fatalf("ParseScript accepted %q", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
