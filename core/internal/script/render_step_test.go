package script

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// ignoreStepSource keeps the comparison to a step's own fields, because
// the authored form every parsed step may carry is not part of what a
// round trip must preserve.
var ignoreStepSource = cmp.FilterPath(func(p cmp.Path) bool {
	return p.Last().Type() == reflect.TypeOf(stepSource{})
}, cmp.Ignore())

func strp(s string) *string { return &s }
func intp(n int) *int       { return &n }

func mirrored(text string) ScriptSelector { return ScriptSelector{Text: strp(text), Label: strp(text)} }

// roundTrip renders step, parses the output with the script parser,
// validates the result and requires the parsed step to equal step.
func roundTrip(t *testing.T, step ScriptStep) string {
	t.Helper()
	rendered, err := RenderStepYAML(step)
	if err != nil {
		t.Fatalf("RenderStepYAML(%+v): %v", step, err)
	}
	def, err := ParseScript(rendered, nil)
	if err != nil {
		t.Fatalf("rendered step does not parse:\n%s\nerror: %v", rendered, err)
	}
	if errs := Validate(def); len(errs) != 0 {
		t.Fatalf("rendered step does not validate:\n%s\nerrors: %v", rendered, errs)
	}
	if len(def.Steps) != 1 {
		t.Fatalf("rendered step parsed into %d steps:\n%s", len(def.Steps), rendered)
	}
	if diff := cmp.Diff(step, def.Steps[0], ignoreStepSource); diff != "" {
		t.Fatalf("step changed through the round trip (-want +got):\n%s\nyaml:\n%s", diff, rendered)
	}
	return rendered
}

func TestRenderStepYAML_RoundTripsEverySupportedStep(t *testing.T) {
	cases := map[string]ScriptStep{
		"tap bare text":         &TapStep{Command: CommandTap, Selector: mirrored("Sign in")},
		"tap id":                &TapStep{Command: CommandTap, Selector: ScriptSelector{ID: strp("login_button")}},
		"tap label only":        &TapStep{Command: CommandTap, Selector: ScriptSelector{Label: strp("Password")}},
		"tap hint":              &TapStep{Command: CommandTap, Selector: ScriptSelector{Hint: strp("Email address")}},
		"tap role and nth":      &TapStep{Command: CommandTap, Selector: ScriptSelector{Role: strp("text-field"), Nth: intp(1)}},
		"tap text and role":     &TapStep{Command: CommandTap, Selector: ScriptSelector{Text: strp("OK"), Label: strp("OK"), Role: strp("button")}},
		"tap text, other label": &TapStep{Command: CommandTap, Selector: ScriptSelector{Text: strp("OK"), Label: strp("Confirm")}},
		"tap every field": &TapStep{Command: CommandTap, Selector: ScriptSelector{
			ID: strp("a"), Text: strp("b"), Label: strp("c"), Hint: strp("d"), Role: strp("button"), Nth: intp(0),
		}},
		"waitFor bare":              &WaitForStep{Command: CommandWaitFor, Selector: mirrored("Dashboard")},
		"waitFor with timeout":      &WaitForStep{Command: CommandWaitFor, Selector: mirrored("Dashboard"), TimeoutMs: intp(5000)},
		"waitFor id with timeout":   &WaitForStep{Command: CommandWaitFor, Selector: ScriptSelector{ID: strp("spinner")}, TimeoutMs: intp(250)},
		"assertVisible bare":        &AssertVisibleStep{Command: CommandAssertVisible, Selector: mirrored("Welcome")},
		"assertVisible id":          &AssertVisibleStep{Command: CommandAssertVisible, Selector: ScriptSelector{ID: strp("title")}, TimeoutMs: intp(1000)},
		"assertNotVisible bare":     &AssertNotVisibleStep{Command: CommandAssertNotVisible, Selector: mirrored("Error")},
		"assertNotVisible role nth": &AssertNotVisibleStep{Command: CommandAssertNotVisible, Selector: ScriptSelector{Role: strp("dialog"), Nth: intp(2)}},
		"assertExists bare":         &AssertExistsStep{Command: CommandAssertExists, Selector: mirrored("Footer")},
		"assertExists id":           &AssertExistsStep{Command: CommandAssertExists, Selector: ScriptSelector{ID: strp("footer")}},
		"type into focused":         &TypeStep{Command: CommandType, Text: "hello"},
		"type empty text":           &TypeStep{Command: CommandType, Text: ""},
		"type into id":              &TypeStep{Command: CommandType, Text: "hello", Into: &ScriptSelector{ID: strp("email")}},
		"type into role and nth":    &TypeStep{Command: CommandType, Text: "hello", Into: &ScriptSelector{Role: strp("text-field"), Nth: intp(0)}},
		"type into bare text":       &TypeStep{Command: CommandType, Text: "x", Into: &ScriptSelector{Text: strp("Email"), Label: strp("Email")}},
	}
	for name, step := range cases {
		t.Run(name, func(t *testing.T) { roundTrip(t, step) })
	}
}

func TestRenderStepYAML_PreservesAwkwardStrings(t *testing.T) {
	values := []string{
		"true", "false", "null", "42", "3.14", "~", "yes", "a: b", "- item", "#hash", " padded ", "trailing:",
		"multi\nline", "quote\"inside", "it's", "${password}", "Đăng nhập", "tab\tseparated", "{braces}", "[brackets]",
		"*star", "&anchor", "!tag", "%percent", "@at", "`tick`", "|", ">",
	}
	for _, v := range values {
		roundTrip(t, &TapStep{Command: CommandTap, Selector: mirrored(v)})
		roundTrip(t, &TapStep{Command: CommandTap, Selector: ScriptSelector{ID: strp(v)}})
		roundTrip(t, &TypeStep{Command: CommandType, Text: v, Into: &ScriptSelector{ID: strp("field")}})
	}
}

func TestRenderStepYAML_ATextSelectorWithoutLabelRendersAsTheParserReadsIt(t *testing.T) {
	rendered, err := RenderStepYAML(&TapStep{Command: CommandTap, Selector: ScriptSelector{Text: strp("Sign in")}})
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "- tap: Sign in\n" {
		t.Fatalf("rendered = %q, want the one-line form", rendered)
	}
	def, err := ParseScript(rendered, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := def.Steps[0].(*TapStep).Selector
	if got.Text == nil || got.Label == nil || *got.Label != "Sign in" {
		t.Fatalf("parsed selector = %+v, want the label mirrored from the text", got)
	}
}

func TestRenderStepYAML_ShapesAreTheDocumentedForms(t *testing.T) {
	cases := []struct {
		step ScriptStep
		want string
	}{
		{&TapStep{Command: CommandTap, Selector: mirrored("Sign in")}, "- tap: Sign in\n"},
		{&TapStep{Command: CommandTap, Selector: ScriptSelector{ID: strp("go")}}, "- tap:\n    id: go\n"},
		{&WaitForStep{Command: CommandWaitFor, Selector: mirrored("Home"), TimeoutMs: intp(3000)}, "- waitFor:\n    text: Home\n    timeout: 3000\n"},
		{&TypeStep{Command: CommandType, Text: "${password}", Into: &ScriptSelector{Role: strp("text-field"), Nth: intp(1)}},
			"- type:\n    into:\n      role: text-field\n      nth: 1\n    text: ${password}\n"},
		{&TypeStep{Command: CommandType, Text: "hi"}, "- type: hi\n"},
	}
	for _, tc := range cases {
		got, err := RenderStepYAML(tc.step)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("rendered %q, want %q", got, tc.want)
		}
	}
}

func TestRenderStepYAML_AnEnvPlaceholderResolvesThroughTheParser(t *testing.T) {
	rendered, err := RenderStepYAML(&TypeStep{Command: CommandType, Text: "${password}", Into: &ScriptSelector{ID: strp("pw")}})
	if err != nil {
		t.Fatal(err)
	}
	def, err := ParseScript(rendered, map[string]string{"password": "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	if got := def.Steps[0].(*TypeStep).Text; got != "hunter2" {
		t.Fatalf("resolved text = %q, want the env value", got)
	}
}

func TestRenderStepYAML_RejectsWhatItCannotRender(t *testing.T) {
	unsupported := []ScriptStep{
		&LaunchAppStep{Command: CommandLaunchApp},
		&SwipeStep{Command: CommandSwipe, Direction: "up"},
		&SleepStep{Command: CommandSleep, Ms: 10},
		nil,
	}
	for _, step := range unsupported {
		if _, err := RenderStepYAML(step); !errors.Is(err, ErrUnsupportedStep) {
			t.Errorf("RenderStepYAML(%T) error = %v, want ErrUnsupportedStep", step, err)
		}
	}
	empty := []ScriptStep{
		&TapStep{Command: CommandTap},
		&TapStep{Command: CommandTap, Selector: ScriptSelector{Nth: intp(0)}},
		&WaitForStep{Command: CommandWaitFor},
		&TypeStep{Command: CommandType, Text: "x", Into: &ScriptSelector{}},
	}
	for _, step := range empty {
		if _, err := RenderStepYAML(step); !errors.Is(err, ErrEmptySelector) {
			t.Errorf("RenderStepYAML(%+v) error = %v, want ErrEmptySelector", step, err)
		}
	}
}

func TestRenderStepYAML_RefusesAStepThatFailsValidation(t *testing.T) {
	invalid := []ScriptStep{
		&TapStep{Command: CommandTap, Selector: ScriptSelector{ID: strp("a"), Nth: intp(-1)}},
		&WaitForStep{Command: CommandWaitFor, Selector: mirrored("a"), TimeoutMs: intp(-5)},
	}
	for _, step := range invalid {
		_, err := RenderStepYAML(step)
		if err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Errorf("RenderStepYAML(%+v) error = %v, want an invalid step error", step, err)
		}
	}
}
