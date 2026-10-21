package script

import (
	"strings"
	"testing"
)

func runVars(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	}
}

func parseOne(t *testing.T, yaml string, env map[string]string) ScriptStep {
	t.Helper()
	def, err := ParseScript(yaml, env)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return def.Steps[0]
}

func resolveOne(t *testing.T, step ScriptStep, values map[string]string) ScriptStep {
	t.Helper()
	resolved, changed, err := ResolveStep(step, runVars(values))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !changed {
		t.Fatal("step was not resolved")
	}
	return resolved
}

func TestResolveStepFillsEveryEligibleField(t *testing.T) {
	vars := map[string]string{"v": "Value", "n": "7"}
	cases := []struct {
		name  string
		yaml  string
		check func(t *testing.T, step ScriptStep)
	}{
		{"tap shorthand", "- tap: \"${v}\"\n", func(t *testing.T, s ScriptStep) {
			sel := s.(*TapStep).Selector
			if *sel.Text != "Value" || *sel.Label != "Value" {
				t.Fatalf("selector = %+v", sel)
			}
		}},
		{"selector fields", "- assertVisible: { id: \"i-${v}\", text: \"t-${v}\", label: \"l-${v}\", hint: \"h-${v}\", timeout: 500 }\n", func(t *testing.T, s ScriptStep) {
			sel := s.(*AssertVisibleStep).Selector
			if *sel.ID != "i-Value" || *sel.Text != "t-Value" || *sel.Label != "l-Value" || *sel.Hint != "h-Value" {
				t.Fatalf("selector = %+v", sel)
			}
			if *s.(*AssertVisibleStep).TimeoutMs != 500 {
				t.Fatal("timeout lost")
			}
		}},
		{"read selector", "- read: { selector: \"${v}\", as: out }\n", func(t *testing.T, s ScriptStep) {
			if *s.(*ReadStep).Selector.Text != "Value" {
				t.Fatalf("selector = %+v", s.(*ReadStep).Selector)
			}
		}},
		{"type text", "- type: \"code ${v}\"\n", func(t *testing.T, s ScriptStep) {
			if s.(*TypeStep).Text != "code Value" {
				t.Fatalf("text = %q", s.(*TypeStep).Text)
			}
		}},
		{"type object text and into", "- type: { text: \"${v}\", into: { id: \"${v}\" } }\n", func(t *testing.T, s ScriptStep) {
			ts := s.(*TypeStep)
			if ts.Text != "Value" || *ts.Into.ID != "Value" {
				t.Fatalf("step = %+v", ts)
			}
		}},
		{"openLink string", "- openLink: \"https://example.com/${v}\"\n", func(t *testing.T, s ScriptStep) {
			if s.(*OpenLinkStep).Link != "https://example.com/Value" {
				t.Fatalf("link = %q", s.(*OpenLinkStep).Link)
			}
		}},
		{"openLink object", "- openLink: { link: \"https://example.com/${v}\" }\n", func(t *testing.T, s ScriptStep) {
			if s.(*OpenLinkStep).Link != "https://example.com/Value" {
				t.Fatalf("link = %q", s.(*OpenLinkStep).Link)
			}
		}},
		{"httpRequest url headers body", "- httpRequest:\n    url: \"https://api.example.com/${v}?q=${v}\"\n    method: POST\n    headers: { X-Token: \"${v}\" }\n    body: { a: { b: \"${v}\" }, list: [\"${v}\", 3] }\n", func(t *testing.T, s ScriptStep) {
			h := s.(*HTTPRequestStep)
			if h.URL != "https://api.example.com/Value?q=Value" {
				t.Fatalf("url = %q", h.URL)
			}
			if h.Headers["X-Token"] != "Value" {
				t.Fatalf("headers = %v", h.Headers)
			}
			body := h.Body.(OrderedMap)
			a, _ := body.Get("a")
			inner := a.(OrderedMap)
			if b, _ := inner.Get("b"); b != "Value" {
				t.Fatalf("body.a.b = %v", b)
			}
			list, _ := body.Get("list")
			if items := list.([]any); items[0] != "Value" || items[1] == "Value" {
				t.Fatalf("body.list = %v", items)
			}
		}},
		{"httpRequest string body", "- httpRequest: { url: \"https://api.example.com/x\", method: POST, body: \"code=${v}\" }\n", func(t *testing.T, s ScriptStep) {
			if s.(*HTTPRequestStep).Body != "code=Value" {
				t.Fatalf("body = %v", s.(*HTTPRequestStep).Body)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			original := parseOne(t, c.yaml, nil)
			c.check(t, resolveOne(t, original, vars))
		})
	}
}

func TestResolveStepEscapesRunValuesInTheQueryPart(t *testing.T) {
	original := parseOne(t, "- httpRequest: { url: \"https://api.example.com/${v}?code=${v}\" }\n", nil)
	resolved := resolveOne(t, original, map[string]string{"v": "a&b=c"})
	if got := resolved.(*HTTPRequestStep).URL; got != "https://api.example.com/a&b=c?code=a%26b%3Dc" {
		t.Fatalf("url = %q", got)
	}
}

func TestResolveStepLeavesIneligibleFieldsAlone(t *testing.T) {
	vars := map[string]string{"v": "Value"}
	yaml := "- tap: { text: \"${v}\", id: { regex: \"^${v}$\" } }\n" +
		"- assertExpr: { expr: \"title == '${v}'\" }\n" +
		"- extract: { from: rsp, values: { code: { path: \"$.${v}\", regex: \"${v}\" } } }\n"
	def, err := ParseScript(yaml, nil)
	if err != nil {
		t.Fatal(err)
	}
	tap := resolveOne(t, def.Steps[0], vars).(*TapStep)
	if *tap.Selector.Text != "Value" {
		t.Fatalf("text = %q", *tap.Selector.Text)
	}
	if tap.Selector.IDRegex.Regex != "^${v}$" {
		t.Fatalf("selector regex = %q, want it untouched", tap.Selector.IDRegex.Regex)
	}
	for _, step := range def.Steps[1:] {
		if step.pendingSource() != nil {
			t.Fatalf("%s carries a pending resolution for fields that are never resolved", step.StepCommand())
		}
		if _, changed, _ := ResolveStep(step, runVars(vars)); changed {
			t.Fatalf("%s was resolved", step.StepCommand())
		}
	}
}

func TestTypedFieldsStillFailParsingWhenUnresolved(t *testing.T) {
	if _, err := ParseScript("- sleep: \"${ms}\"\n", nil); err == nil {
		t.Fatal("an unresolved typed field must fail at parse time")
	}
	if _, err := ParseScript("- swipe: \"${dir}\"\n", nil); err == nil {
		t.Fatal("an unresolved enum must fail at parse time")
	}
}

func TestResolveStepExpandsEachPlaceholderOnce(t *testing.T) {
	t.Run("run value holding a placeholder", func(t *testing.T) {
		original := parseOne(t, "- type: \"${a}-${b}\"\n", nil)
		resolved := resolveOne(t, original, map[string]string{"a": "${b}", "b": "B"})
		if got := resolved.(*TypeStep).Text; got != "${b}-B" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("env value holding a placeholder", func(t *testing.T) {
		original := parseOne(t, "- type: \"${env}-${late}\"\n", map[string]string{"env": "${late}"})
		resolved := resolveOne(t, original, map[string]string{"late": "L"})
		if got := resolved.(*TypeStep).Text; got != "${late}-L" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("env wins over a run variable", func(t *testing.T) {
		original := parseOne(t, "- type: \"${name}-${late}\"\n", map[string]string{"name": "fromEnv"})
		resolved := resolveOne(t, original, map[string]string{"name": "fromRun", "late": "L"})
		if got := resolved.(*TypeStep).Text; got != "fromEnv-L" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("a name env resolved is not pending", func(t *testing.T) {
		original := parseOne(t, "- type: \"${env}\"\n", map[string]string{"env": "${other}"})
		if original.pendingSource() != nil {
			t.Fatal("a step whose names are all resolved carries a pending resolution")
		}
		if got := original.(*TypeStep).Text; got != "${other}" {
			t.Fatalf("text = %q", got)
		}
	})
}

func TestResolveStepLeavesMissingNamesLiteral(t *testing.T) {
	original := parseOne(t, "- type: \"${a}-${b}\"\n", nil)
	resolved := resolveOne(t, original, map[string]string{"a": "A"})
	if got := resolved.(*TypeStep).Text; got != "A-${b}" {
		t.Fatalf("text = %q", got)
	}
	same, changed, err := ResolveStep(original, runVars(nil))
	if err != nil || changed || same != original {
		t.Fatalf("a step no variable supplies must come back unchanged, got changed=%v err=%v", changed, err)
	}
}

func TestResolveStepFastPathAllocatesNothing(t *testing.T) {
	plain := parseOne(t, "- tap: Login\n", nil)
	if plain.pendingSource() != nil {
		t.Fatal("a step with no placeholder carries a pending resolution")
	}
	pending := parseOne(t, "- tap: \"${v}\"\n", nil)
	lookup := runVars(nil)
	for name, step := range map[string]ScriptStep{"plain": plain, "no variable": pending} {
		allocs := testing.AllocsPerRun(100, func() {
			if _, changed, _ := ResolveStep(step, lookup); changed {
				t.Fatal("resolved")
			}
		})
		if allocs != 0 {
			t.Fatalf("%s: %v allocations, want 0", name, allocs)
		}
	}
}

func TestResolveStepKeepsTheAuthoredForm(t *testing.T) {
	original := parseOne(t, "- type: \"${otp}\"\n", map[string]string{"x": "y"})
	resolved := resolveOne(t, original, map[string]string{"otp": "123456"})
	if got := resolved.(*TypeStep).Text; got != "123456" {
		t.Fatalf("text = %q", got)
	}
	authored := AuthoredOf(resolved).(*TypeStep)
	if authored.Text != "${otp}" {
		t.Fatalf("authored text = %q, want the placeholder", authored.Text)
	}
	if resolved.pendingSource() != nil {
		t.Fatal("a resolved step must not be resolved again")
	}
}

func TestResolveStepCarriesALostWrittenForm(t *testing.T) {
	original := parseOne(t, "- tap: \"${v}\"\n", nil)
	original.markWrittenFormLost()
	resolved := resolveOne(t, original, map[string]string{"v": "x"})
	if !WrittenFormLost(resolved) {
		t.Fatal("the lost flag of the original did not carry over")
	}
}

func TestResolveStepErrorsNameNoValue(t *testing.T) {
	secret := "hunter2 \x7f"
	cases := map[string]string{
		"openLink": "- openLink: \"${v}\"\n",
		"http url": "- httpRequest: { url: \"https://h.example/${v}\" }\n",
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			original := parseOne(t, yaml, nil)
			_, _, err := ResolveStep(original, runVars(map[string]string{"v": secret}))
			if err == nil {
				t.Fatal("an invalid resolved step must fail")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error echoes the value: %v", err)
			}
			if !strings.Contains(err.Error(), "${v}") {
				t.Fatalf("error does not name the placeholder: %v", err)
			}
		})
	}
}

func TestPendingReachesChildrenOfEveryBlock(t *testing.T) {
	yaml := "- repeat:\n    times: 2\n    commands:\n      - type: \"${a}\"\n" +
		"- retry:\n    commands:\n      - type: \"${a}\"\n" +
		"- handle:\n    - when: { visible: x }\n      do:\n        - type: \"${a}\"\n    - otherwise: skip\n" +
		"- branch:\n    from: rsp\n    on:\n      - match: { status: 200 }\n        do:\n          - type: \"${a}\"\n    default:\n      - type: \"${a}\"\n"
	def, err := ParseScript(yaml, nil)
	if err != nil {
		t.Fatal(err)
	}
	var children []ScriptStep
	children = append(children, def.Steps[0].(*RepeatStep).Commands...)
	children = append(children, def.Steps[1].(*RetryStep).Commands...)
	children = append(children, def.Steps[2].(*HandleStep).Branches[0].Do.Steps...)
	branch := def.Steps[3].(*BranchStep)
	children = append(children, branch.On[0].Do.Steps...)
	children = append(children, branch.Default.Steps...)
	if len(children) != 5 {
		t.Fatalf("found %d children, want 5", len(children))
	}
	for i, child := range children {
		resolved := resolveOne(t, child, map[string]string{"a": "A"})
		if resolved.(*TypeStep).Text != "A" {
			t.Fatalf("child %d text = %q", i, resolved.(*TypeStep).Text)
		}
	}
	for _, block := range def.Steps {
		if block.pendingSource() != nil {
			t.Fatalf("block %s carries a pending resolution", block.StepCommand())
		}
	}
}

func TestWithoutRunTimeResolutionSealsEveryStep(t *testing.T) {
	def, err := ParseScript("- type: \"${v}\"\n", nil, WithoutRunTimeResolution())
	if err != nil {
		t.Fatal(err)
	}
	if def.Steps[0].pendingSource() != nil {
		t.Fatal("a sealed parse still carries a pending resolution")
	}
}
