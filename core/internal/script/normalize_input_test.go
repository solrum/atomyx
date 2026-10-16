package script

import (
	"strings"
	"testing"
)

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func intPtr(v int) *int { return &v }

func TestParseScript_HideKeyboard(t *testing.T) {
	if _, ok := mustParse(t, "- hideKeyboard\n").Steps[0].(*HideKeyboardStep); !ok {
		t.Fatalf("hideKeyboard did not parse to *HideKeyboardStep")
	}
}

func TestParseScript_EraseTextForms(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want *int
	}{
		{"bare", "- eraseText\n", nil},
		{"count", "- eraseText: 5\n", intPtr(5)},
		{"object", "- eraseText:\n    charactersToErase: 12\n", intPtr(12)},
		{"empty object", "- eraseText: {}\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := mustParse(t, tc.yaml).Steps[0].(*EraseTextStep)
			if !intPtrEqual(step.Characters, tc.want) {
				t.Errorf("characters = %v, want %v", step.Characters, tc.want)
			}
		})
	}
}

func TestParseScript_WaitForAnimationToEndForms(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want *int
	}{
		{"bare", "- waitForAnimationToEnd\n", nil},
		{"number", "- waitForAnimationToEnd: 3000\n", intPtr(3000)},
		{"object", "- waitForAnimationToEnd:\n    timeout: 1500\n", intPtr(1500)},
		{"zero", "- waitForAnimationToEnd: 0\n", intPtr(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := mustParse(t, tc.yaml).Steps[0].(*WaitForAnimationToEndStep)
			if !intPtrEqual(step.TimeoutMs, tc.want) {
				t.Errorf("timeoutMs = %v, want %v", step.TimeoutMs, tc.want)
			}
		})
	}
}

func TestParseScript_ScrollUntilVisibleForms(t *testing.T) {
	cases := []struct {
		name          string
		yaml          string
		wantText      string
		wantID        string
		wantDirection string
	}{
		{"string", "- scrollUntilVisible: Footer\n", "Footer", "", ""},
		{"selector object", "- scrollUntilVisible:\n    id: footer\n", "", "footer", ""},
		{"direction", "- scrollUntilVisible:\n    text: Footer\n    direction: up\n", "Footer", "", "up"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := mustParse(t, tc.yaml).Steps[0].(*ScrollUntilVisibleStep)
			if tc.wantText != "" && (step.Selector.Text == nil || *step.Selector.Text != tc.wantText) {
				t.Errorf("text = %v, want %q", step.Selector.Text, tc.wantText)
			}
			if tc.wantID != "" && (step.Selector.ID == nil || *step.Selector.ID != tc.wantID) {
				t.Errorf("id = %v, want %q", step.Selector.ID, tc.wantID)
			}
			switch {
			case tc.wantDirection == "" && step.Direction != nil:
				t.Errorf("direction = %q, want none", *step.Direction)
			case tc.wantDirection != "" && (step.Direction == nil || *step.Direction != tc.wantDirection):
				t.Errorf("direction = %v, want %q", step.Direction, tc.wantDirection)
			}
		})
	}
}

func TestParseScript_InputRandomForms(t *testing.T) {
	cases := []struct {
		name       string
		yaml       string
		wantKind   string
		wantLength *int
	}{
		{"kind string", "- inputRandom: email\n", RandomKindEmail, nil},
		{"object", "- inputRandom:\n    kind: number\n    length: 6\n", RandomKindNumber, intPtr(6)},
		{"person name", "- inputRandom: personName\n", RandomKindPersonName, nil},
		{"text with length", "- inputRandom:\n    kind: text\n    length: 256\n", RandomKindText, intPtr(256)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := mustParse(t, tc.yaml).Steps[0].(*InputRandomStep)
			if step.Kind != tc.wantKind || !intPtrEqual(step.Length, tc.wantLength) {
				t.Errorf("got kind %q length %v, want kind %q length %v", step.Kind, step.Length, tc.wantKind, tc.wantLength)
			}
		})
	}
}

func TestParseScript_InputStepRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"erase zero", "- eraseText: 0\n", "charactersToErase: must be at least 1"},
		{"erase negative", "- eraseText:\n    charactersToErase: -3\n", "charactersToErase: must be at least 1"},
		{"erase fractional", "- eraseText: 1.5\n", "eraseText.charactersToErase: must be a whole number"},
		{"animation negative", "- waitForAnimationToEnd: -1\n", "timeoutMs: must be nonnegative"},
		{"animation text", "- waitForAnimationToEnd: soon\n", "waitForAnimationToEnd.timeout: must be a whole number"},
		{"scroll sideways", "- scrollUntilVisible:\n    text: Footer\n    direction: left\n", "direction: must be one of up/down"},
		{"scroll direction not a string", "- scrollUntilVisible:\n    text: Footer\n    direction: 3\n", "scrollUntilVisible.direction: expected a string"},
		{"random missing kind", "- inputRandom: {}\n", "kind: must be one of text/number/email/personName"},
		{"random unknown kind", "- inputRandom: color\n", "kind: must be one of text/number/email/personName"},
		{"random zero length", "- inputRandom:\n    kind: text\n    length: 0\n", "length: must be between 1 and 256"},
		{"random too long", "- inputRandom:\n    kind: text\n    length: 257\n", "length: must be between 1 and 256"},
		{"random person name length", "- inputRandom:\n    kind: personName\n    length: 5\n", "length: does not apply to personName"},
		{"random list", "- inputRandom: [text]\n", "inputRandom: expected a kind"},
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
