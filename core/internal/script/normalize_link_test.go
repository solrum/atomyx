package script

import (
	"strings"
	"testing"
)

func TestParseScript_OpenLinkForms(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		wantLink  string
		wantAppID *string
	}{
		{name: "bare link", yaml: "- openLink: atomyxdemo://open/focus\n", wantLink: "atomyxdemo://open/focus"},
		{name: "quoted link", yaml: "- openLink: \"https://example.com/a?x=1&y=2#top\"\n", wantLink: "https://example.com/a?x=1&y=2#top"},
		{name: "object", yaml: "- openLink:\n    link: myapp://home\n", wantLink: "myapp://home"},
		{name: "object with app", yaml: "- openLink:\n    link: myapp://home\n    appId: com.other\n", wantLink: "myapp://home", wantAppID: strPtr("com.other")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := mustParse(t, tc.yaml).Steps[0].(*OpenLinkStep)
			if step.Link != tc.wantLink {
				t.Errorf("link = %q, want %q", step.Link, tc.wantLink)
			}
			switch {
			case tc.wantAppID == nil && step.AppID != nil:
				t.Errorf("appId = %q, want none", *step.AppID)
			case tc.wantAppID != nil && (step.AppID == nil || *step.AppID != *tc.wantAppID):
				t.Errorf("appId = %v, want %q", step.AppID, *tc.wantAppID)
			}
		})
	}
}

func TestParseScript_OpenLinkExpandsEnvPlaceholders(t *testing.T) {
	def, err := ParseScript("appId: com.x\nname: n\nenv:\n  route: settings\n---\n- openLink: atomyxdemo://open/${route}\n", nil)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	if got := def.Steps[0].(*OpenLinkStep).Link; got != "atomyxdemo://open/settings" {
		t.Errorf("link = %q, want the expanded link", got)
	}
}

func TestParseScript_OpenLinkKeepsARuntimePlaceholderUnchecked(t *testing.T) {
	def, err := ParseScript("appId: com.x\nname: n\n---\n- openLink: ${target}\n", nil)
	if err != nil {
		t.Fatalf("ParseScript rejected a link that a run-time variable completes: %v", err)
	}
	if got := def.Steps[0].(*OpenLinkStep).Link; got != "${target}" {
		t.Errorf("link = %q, want the placeholder kept", got)
	}
}

func TestParseScript_OpenLinkRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{name: "bare step", yaml: "- openLink\n", want: "openLink"},
		{name: "null value", yaml: "- openLink:\n", want: "openLink: expected a link string"},
		{name: "number", yaml: "- openLink: 12\n", want: "openLink: expected a link string"},
		{name: "no scheme", yaml: "- openLink: example.com/path\n", want: "must start with a scheme"},
		{name: "empty", yaml: "- openLink: \"\"\n", want: "link: "},
		{name: "control character", yaml: "- openLink: \"myapp://a\\nb\"\n", want: "control characters"},
		{name: "object without link", yaml: "- openLink:\n    appId: com.x\n", want: "needs a `link`"},
		{name: "link not a string", yaml: "- openLink:\n    link: [a]\n", want: "openLink.link: must be a string"},
		{name: "app id not a string", yaml: "- openLink:\n    link: myapp://home\n    appId: [a]\n", want: "openLink.appId: must be a string"},
		{name: "empty app id", yaml: "- openLink:\n    link: myapp://home\n    appId: \"\"\n", want: "appId: must not be empty"},
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

func strPtr(v string) *string { return &v }
