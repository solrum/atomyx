package script

import (
	"encoding/json"
	"strings"
	"testing"
)

func parseTapSelector(t *testing.T, yamlText string) ScriptSelector {
	t.Helper()
	def, err := ParseScript(yamlText, nil)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	tap, ok := def.Steps[len(def.Steps)-1].(*TapStep)
	if !ok {
		t.Fatalf("last step is %T, want *TapStep", def.Steps[len(def.Steps)-1])
	}
	return tap.Selector
}

func TestSelectorRegexAnchoredParsesAndMirrorsToLabel(t *testing.T) {
	sel := parseTapSelector(t, "- tap:\n    text: { regex: \"^Item [0-9]+$\" }\n")
	if sel.TextRegex == nil || sel.TextRegex.Regex != "^Item [0-9]+$" {
		t.Fatalf("TextRegex = %+v, want the anchored source", sel.TextRegex)
	}
	if sel.Text != nil {
		t.Errorf("Text = %q, want unset for a regex field", *sel.Text)
	}
	if sel.LabelRegex == nil {
		t.Errorf("LabelRegex unset, want text mirrored onto label")
	}
	p, err := sel.TextRegex.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !p.Matches("Item 42") || p.Matches("Item 42 extra") {
		t.Errorf("anchored pattern matched the wrong values")
	}
}

func TestSelectorRegexCaseInsensitiveFlag(t *testing.T) {
	sel := parseTapSelector(t, "- tap:\n    id: { regex: \"^submit$\", flags: \"i\" }\n")
	if sel.IDRegex == nil || sel.IDRegex.Flags != "i" {
		t.Fatalf("IDRegex = %+v, want flags i", sel.IDRegex)
	}
	p, err := sel.IDRegex.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !p.Matches("SUBMIT") {
		t.Errorf("case-insensitive pattern did not match SUBMIT")
	}
}

func TestSelectorRegexRejectedAtParseTime(t *testing.T) {
	cases := map[string]struct {
		yaml string
		want string
	}{
		"invalid pattern": {"- tap:\n    text: { regex: \"([a-z\" }\n", "invalid regex"},
		"unknown flag":    {"- tap:\n    text: { regex: \"abc\", flags: \"z\" }\n", "unsupported regex flag"},
		"lookahead":       {"- tap:\n    text: { regex: \"a(?=b)\" }\n", "lookahead"},
		"missing regex":   {"- tap:\n    text: { flags: \"i\" }\n", "requires a regex field"},
		"unknown key":     {"- tap:\n    text: { regex: \"a\", extra: 1 }\n", "unknown field"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseScript(tc.yaml, nil)
			if err == nil {
				t.Fatal("ParseScript succeeded, want a parse error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestSelectorPlainStringWithRegexCharactersStaysLiteral(t *testing.T) {
	sel := parseTapSelector(t, "- tap: \"Price (USD) [1.5]+\"\n")
	if sel.Text == nil || *sel.Text != "Price (USD) [1.5]+" {
		t.Fatalf("Text = %v, want the exact string", sel.Text)
	}
	if sel.TextRegex != nil || sel.LabelRegex != nil {
		t.Errorf("a plain string produced a regex field")
	}
}

func TestValidateReportsRegexSetAsLiteralAndRegex(t *testing.T) {
	text := "x"
	sel := &ScriptSelector{Text: &text, TextRegex: &RegexPattern{Regex: "("}}
	var got []string
	validateSelector(sel, "sel", func(format string, args ...any) {
		got = append(got, format)
	})
	if len(got) != 2 {
		t.Errorf("reports = %v, want one for both forms and one for the bad regex", got)
	}
}

func TestSelectorJSONRoundTripKeepsRegexShape(t *testing.T) {
	sel := parseTapSelector(t, "- tap:\n    text: { regex: \"^A\", flags: \"i\" }\n    id: \"plain\"\n")
	raw, err := json.Marshal(sel)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"text":{"regex":"^A","flags":"i"}`) || !strings.Contains(string(raw), `"id":"plain"`) {
		t.Fatalf("json = %s, want object text and string id", raw)
	}
	var back ScriptSelector
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.TextRegex == nil || *back.TextRegex != *sel.TextRegex || back.ID == nil || *back.ID != "plain" {
		t.Errorf("round trip lost data: %+v", back)
	}
}

func TestRenderStepKeepsRegexSelector(t *testing.T) {
	sel := parseTapSelector(t, "- tap:\n    text: { regex: \"^A\", flags: \"i\" }\n")
	node, err := selectorMapping(sel)
	if err != nil {
		t.Fatalf("selectorMapping: %v", err)
	}
	if len(node.Content) != 2 || node.Content[0].Value != "text" || len(node.Content[1].Content) != 4 {
		t.Errorf("rendered mapping = %+v, want text as a regex mapping and no separate label", node.Content)
	}
}
