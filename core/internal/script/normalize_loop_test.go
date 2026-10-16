package script

import (
	"strings"
	"testing"
)

func TestParseScript_RepeatTimes(t *testing.T) {
	def := mustParse(t, "- repeat:\n    times: 3\n    commands:\n      - back\n      - tap: Next\n")
	step, ok := def.Steps[0].(*RepeatStep)
	if !ok {
		t.Fatalf("step 0 is %T, want *RepeatStep", def.Steps[0])
	}
	if step.Times == nil || *step.Times != 3 {
		t.Errorf("times = %v, want 3", step.Times)
	}
	if step.While != nil {
		t.Errorf("while = %+v, want nil", step.While)
	}
	if len(step.Commands) != 2 {
		t.Errorf("commands = %d, want 2", len(step.Commands))
	}
}

func TestParseScript_RepeatWhileConditions(t *testing.T) {
	cases := map[string]string{
		"visible":    "visible: More",
		"notVisible": "notVisible: Done",
		"expr":       "expr: count < 3",
	}
	for name, cond := range cases {
		t.Run(name, func(t *testing.T) {
			def := mustParse(t, "- repeat:\n    while:\n      "+cond+"\n    commands:\n      - back\n")
			step := def.Steps[0].(*RepeatStep)
			if step.Times != nil || step.While == nil {
				t.Fatalf("got times=%v while=%v, want a while-only loop", step.Times, step.While)
			}
			set := step.While.Visible != nil || step.While.NotVisible != nil || step.While.Expr != nil
			if !set {
				t.Fatalf("while condition carries no %s", name)
			}
		})
	}
}

func TestParseScript_RepeatTimesAndWhile(t *testing.T) {
	def := mustParse(t, "- repeat:\n    times: 5\n    while:\n      visible: More\n    commands:\n      - back\n")
	step := def.Steps[0].(*RepeatStep)
	if step.Times == nil || *step.Times != 5 || step.While == nil || step.While.Visible == nil {
		t.Fatalf("got %+v, want both times and while", step)
	}
}

func TestParseScript_RepeatTimesFromPlaceholder(t *testing.T) {
	def, err := ParseScript("appId: com.x\nname: n\nenv:\n  n: \"4\"\n---\n- repeat:\n    times: ${n}\n    commands:\n      - back\n", nil)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	if got := *def.Steps[0].(*RepeatStep).Times; got != 4 {
		t.Errorf("times = %d, want 4", got)
	}
}

func TestParseScript_RepeatNestedBlock(t *testing.T) {
	def := mustParse(t, "- repeat:\n    times: 2\n    commands:\n      - repeat:\n          times: 3\n          commands:\n            - back\n")
	outer := def.Steps[0].(*RepeatStep)
	inner, ok := outer.Commands[0].(*RepeatStep)
	if !ok || *inner.Times != 3 {
		t.Fatalf("inner step = %+v, want a repeat of 3", outer.Commands[0])
	}
}

func TestParseScript_RepeatRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "no times and no while",
			yaml: "- repeat:\n    commands:\n      - back\n",
			want: "at least one of `times` or `while`",
		},
		{
			name: "times zero",
			yaml: "- repeat:\n    times: 0\n    commands:\n      - back\n",
			want: "times: must be at least 1",
		},
		{
			name: "times negative",
			yaml: "- repeat:\n    times: -2\n    commands:\n      - back\n",
			want: "times: must be at least 1",
		},
		{
			name: "times fractional",
			yaml: "- repeat:\n    times: 1.5\n    commands:\n      - back\n",
			want: "repeat.times: must be a whole number",
		},
		{
			name: "commands missing",
			yaml: "- repeat:\n    times: 2\n",
			want: "commands: must have at least one step",
		},
		{
			name: "commands empty",
			yaml: "- repeat:\n    times: 2\n    commands: []\n",
			want: "commands: must have at least one step",
		},
		{
			name: "commands not a list",
			yaml: "- repeat:\n    times: 2\n    commands: back\n",
			want: "repeat.commands: expected a step list",
		},
		{
			name: "while with no condition",
			yaml: "- repeat:\n    while: {}\n    commands:\n      - back\n",
			want: "exactly one of `visible`, `notVisible` or `expr`",
		},
		{
			name: "while with two conditions",
			yaml: "- repeat:\n    while:\n      visible: A\n      expr: x == 1\n    commands:\n      - back\n",
			want: "exactly one of `visible`, `notVisible` or `expr`",
		},
		{
			name: "while with empty expr",
			yaml: "- repeat:\n    while:\n      expr: \"\"\n    commands:\n      - back\n",
			want: "expr: must not be empty",
		},
		{
			name: "while not an object",
			yaml: "- repeat:\n    while: yes\n    commands:\n      - back\n",
			want: "repeat.while: expected an object",
		},
		{
			name: "repeat not an object",
			yaml: "- repeat: 3\n",
			want: "repeat: expected an object",
		},
		{
			name: "invalid nested step",
			yaml: "- repeat:\n    times: 2\n    commands:\n      - swipe: sideways\n",
			want: "commands[0].direction",
		},
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

func TestRepeatStep_AuthoredFormKeepsPlaceholders(t *testing.T) {
	def, err := ParseScript("appId: com.x\nname: n\nenv:\n  who: Ann\n---\n- repeat:\n    times: 2\n    commands:\n      - tap: ${who}\n", nil)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	outer := def.Steps[0].(*RepeatStep)
	nested := outer.Commands[0].(*TapStep)
	if got := *nested.Selector.Text; got != "Ann" {
		t.Fatalf("executed text = %q, want Ann", got)
	}
	written := AuthoredOf(nested).(*TapStep)
	if got := *written.Selector.Text; got != "${who}" {
		t.Errorf("authored text = %q, want ${who}", got)
	}
}
