package runner

import (
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

func breakCode(err error) string {
	code, _, _ := diag.Describe(err)
	return code
}

func TestParseBreakLocation(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "work", "proj")
	abs := filepath.Join(string(filepath.Separator), "other", "x.yaml")
	cases := []struct {
		spec string
		want BreakLocation
	}{
		{"flows/a.yaml:12", BreakLocation{filepath.Join(base, "flows", "a.yaml"), 12}},
		{"./a.yaml:3", BreakLocation{filepath.Join(base, "a.yaml"), 3}},
		{"../b.yaml:4", BreakLocation{filepath.Join(filepath.Dir(base), "b.yaml"), 4}},
		{abs + ":7", BreakLocation{abs, 7}},
		{"inline:5", BreakLocation{"inline", 5}},
		{abs + "dir:v2/x.yaml:9", BreakLocation{abs + "dir:v2/x.yaml", 9}},
	}
	for _, c := range cases {
		got, err := ParseBreakLocation(c.spec, base)
		if err != nil || got != c.want {
			t.Errorf("%q: got %+v err=%v, want %+v", c.spec, got, err, c.want)
		}
	}
}

func TestParseBreakLocation_RejectsMalformed(t *testing.T) {
	for _, spec := range []string{"", "a.yaml", "a.yaml:", "a.yaml:x", "a.yaml:0", "a.yaml:-2", ":4", "inline:", "inline:0", "a.yaml:1.5"} {
		_, err := ParseBreakLocation(spec, "/work")
		if err == nil || breakCode(err) != diag.CodeInvalidArgument {
			t.Errorf("%q: got err=%v, want INVALID_ARGUMENT", spec, err)
		}
	}
}

const breakYAML = "appId: com.test\n---\n" +
	"- launchApp\n" + // line 3
	"- handle:\n" + // line 4
	"    - when:\n" +
	"        visible: Go\n" +
	"      do:\n" +
	"        - screenshot: a\n" +
	"- screenshot: last\n" // line 9

func TestValidateBreakLocations(t *testing.T) {
	def := mustParse(t, breakYAML)
	scripts := []BreakScript{{Path: "/s/flow.yaml", Definition: def}, {Path: "", Definition: def}}
	cases := []struct {
		name    string
		loc     BreakLocation
		wantErr string
	}{
		{"top-level step", BreakLocation{"/s/flow.yaml", 3}, ""},
		{"top-level after nesting", BreakLocation{"/s/flow.yaml", 9}, ""},
		{"inline script", BreakLocation{"inline", 4}, ""},
		{"unknown file", BreakLocation{"/s/other.yaml", 3}, "no script of this run"},
		{"line without a step", BreakLocation{"/s/flow.yaml", 1}, "no top-level step starts at line 1"},
		{"line past the end", BreakLocation{"/s/flow.yaml", 40}, "no top-level step starts at line 40"},
		{"nested only", BreakLocation{"/s/flow.yaml", 8}, "Nested steps are reached by stepping"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateBreakLocations([]BreakLocation{c.loc}, scripts)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("got %v", err)
				}
				return
			}
			if err == nil || breakCode(err) != diag.CodeInvalidArgument || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("got %v, want INVALID_ARGUMENT containing %q", err, c.wantErr)
			}
		})
	}
}

func TestValidateBreakLocations_UncleanScriptPathMatches(t *testing.T) {
	def := mustParse(t, breakYAML)
	err := ValidateBreakLocations([]BreakLocation{{"/s/flow.yaml", 3}}, []BreakScript{{Path: "/s/sub/../flow.yaml", Definition: def}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBreakLocationStringMatchesGateLocation(t *testing.T) {
	gate := &funcGate{}
	tr := gatedRunner(gate, "/s/flow.yaml")
	run(t, tr, breakYAML)
	want := BreakLocation{"/s/flow.yaml", 3}.String()
	if len(gate.seen) == 0 || gate.seen[0].Location != want {
		t.Fatalf("gate saw %+v, want first location %q", gate.seen, want)
	}
}
