package script

import (
	"strings"
	"testing"

	"dev.atomyx/internal/driver"
)

func TestParseScript_SetOrientationForms(t *testing.T) {
	for _, o := range driver.AllOrientations() {
		t.Run(string(o), func(t *testing.T) {
			step := mustParse(t, "- setOrientation: "+string(o)+"\n").Steps[0].(*SetOrientationStep)
			if step.Orientation != o {
				t.Errorf("orientation = %q, want %q", step.Orientation, o)
			}
		})
	}
}

func TestParseScript_SetOrientationExpandsEnvPlaceholders(t *testing.T) {
	def, err := ParseScript("appId: com.x\nname: n\nenv:\n  turn: landscapeLeft\n---\n- setOrientation: ${turn}\n", nil)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	if got := def.Steps[0].(*SetOrientationStep).Orientation; got != driver.OrientationLandscapeLeft {
		t.Errorf("orientation = %q, want landscapeLeft", got)
	}
}

func TestParseScript_SetOrientationRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{name: "bare step", yaml: "- setOrientation\n", want: "setOrientation"},
		{name: "null value", yaml: "- setOrientation:\n", want: "setOrientation: expected one of"},
		{name: "number", yaml: "- setOrientation: 90\n", want: "setOrientation: expected one of"},
		{name: "unknown name", yaml: "- setOrientation: sideways\n", want: "is not an orientation, use one of: portrait, landscapeLeft, landscapeRight"},
		{name: "upside down", yaml: "- setOrientation: portraitUpsideDown\n", want: "is not an orientation"},
		{name: "object", yaml: "- setOrientation:\n    orientation: portrait\n", want: "setOrientation: expected one of"},
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
