package runner

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/wire"
)

func TestHideKeyboardCommand(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- hideKeyboard\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if n := countCalls(tr, "HideKeyboard"); n != 1 {
		t.Errorf("got %d HideKeyboard calls, want 1", n)
	}
	if got := detail(lastStep(t, result)); got != "hideKeyboard: dispatched" {
		t.Errorf("detail = %q", got)
	}
}

func TestHideKeyboardCommand_UnsupportedDeviceFailsTheStep(t *testing.T) {
	tr := newTestRunner()
	caps := tr.driver.Capabilities()
	caps.CanHideKeyboard = false
	tr.driver.SetCapabilities(caps)
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- hideKeyboard\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want a failure")
	}
	if got := detail(lastStep(t, result)); !strings.Contains(got, "does not support hideKeyboard") {
		t.Errorf("detail = %q", got)
	}
}

func TestEraseTextCommand(t *testing.T) {
	cases := []struct {
		name      string
		step      string
		wantCount int
	}{
		{"default", "- eraseText\n", 50},
		{"count", "- eraseText: 7\n", 7},
		{"object", "- eraseText:\n    charactersToErase: 3\n", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTestRunner()
			tr.driver.StageHierarchy(rootTree())
			result := run(t, tr, "appId: com.test\n---\n"+tc.step)
			if !result.Ok {
				t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
			}
			var counts []int
			for _, c := range tr.driver.Calls {
				if c.Method == "EraseText" {
					counts = append(counts, c.Args[0].(int))
				}
			}
			if len(counts) != 1 || counts[0] != tc.wantCount {
				t.Errorf("EraseText counts = %v, want [%d]", counts, tc.wantCount)
			}
		})
	}
}

func TestEraseTextCommand_UnsupportedDeviceFailsTheStep(t *testing.T) {
	tr := newTestRunner()
	caps := tr.driver.Capabilities()
	caps.CanEraseText = false
	tr.driver.SetCapabilities(caps)
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- eraseText: 4\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want a failure")
	}
	if got := detail(lastStep(t, result)); !strings.Contains(got, "pressKey: delete") {
		t.Errorf("detail = %q, want a pointer to pressKey", got)
	}
	if n := countCalls(tr, "EraseText"); n != 0 {
		t.Errorf("got %d EraseText calls, want 0", n)
	}
}

func busyScreen(extra ...wire.TreeNodeWire) wire.TreeNodeWire {
	children := []wire.TreeNodeWire{
		button("a", "Row A", "0,100,430,140"),
		button("b", "Row B", "0,200,430,240"),
		button("c", "Row C", "0,300,430,340"),
		button("d", "Row D", "0,400,430,440"),
		button("e", "Row E", "0,500,430,540"),
	}
	return rootTree(append(children, extra...)...)
}

func TestScrollUntilVisibleCommand_ElementOnScreenNeedsNoScroll(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(busyScreen(button("footer", "Footer", "0,600,430,640")))

	result := run(t, tr, "appId: com.test\n---\n- scrollUntilVisible: Footer\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if n := countCalls(tr, "Swipe"); n != 0 {
		t.Errorf("got %d swipes, want 0", n)
	}
}

func TestScrollUntilVisibleCommand_DirectionDefaultsToDown(t *testing.T) {
	cases := []struct {
		name         string
		step         string
		wantDownward bool
	}{
		{"default", "- scrollUntilVisible: Footer\n", true},
		{"explicit up", "- scrollUntilVisible:\n    text: Footer\n    direction: up\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTestRunner()
			tr.driver.StageHierarchy(busyScreen())
			tr.driver.StageHierarchy(busyScreen())
			tr.driver.StageHierarchy(busyScreen(button("footer", "Footer", "0,600,430,640")))

			result := run(t, tr, "appId: com.test\n---\n"+tc.step)
			if !result.Ok {
				t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
			}
			for _, c := range tr.driver.Calls {
				if c.Method != "Swipe" {
					continue
				}
				from, to := c.Args[0].(wire.Point), c.Args[1].(wire.Point)
				if got := to.Y < from.Y; got != tc.wantDownward {
					t.Errorf("first swipe downward = %t, want %t", got, tc.wantDownward)
				}
				return
			}
			t.Errorf("no swipe was dispatched")
		})
	}
}

func TestScrollUntilVisibleCommand_MissingElementFailsWithNotFoundCode(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(busyScreen())

	result := run(t, tr, "appId: com.test\n---\n- scrollUntilVisible: Nowhere\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want a failure")
	}
	if got := lastStep(t, result).Code; got != diag.CodeElementNotFound {
		t.Errorf("code = %q, want %q", got, diag.CodeElementNotFound)
	}
}

func TestWaitForAnimationToEndCommand_PassesOnceTheScreenIsStable(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("box", "Box", "0,0,10,10")))
	tr.driver.StageHierarchy(rootTree(button("box", "Box", "0,50,10,60")))
	tr.driver.StageHierarchy(rootTree(button("box", "Box", "0,50,10,60")))

	result := run(t, tr, "appId: com.test\n---\n- waitForAnimationToEnd\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Screen settled after 400ms" {
		t.Errorf("detail = %q", got)
	}
}

func TestWaitForAnimationToEndCommand_TimeoutDoesNotFailTheStep(t *testing.T) {
	tr := newTestRunner()
	for y := 0; y < 6; y++ {
		top := y * 10
		tr.driver.StageHierarchy(rootTree(button("box", "Box", boundsString(top))))
	}

	result := run(t, tr, "appId: com.test\n---\n- waitForAnimationToEnd: 600\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Screen still changing after 600ms, continuing" {
		t.Errorf("detail = %q", got)
	}
}

func boundsString(top int) string {
	return fmt.Sprintf("0,%d,10,%d", top, top+10)
}

func typedTexts(tr *testRunner) []string {
	var out []string
	for _, c := range tr.driver.Calls {
		if c.Method == "InputText" {
			out = append(out, c.Args[0].(string))
		}
	}
	return out
}

func runInputRandom(t *testing.T, seed uint64, step string) (string, ScriptResult) {
	t.Helper()
	tr := newTestRunner()
	tr.runner.deps.Random = NewSeededRandom(seed)
	tr.driver.StageHierarchy(rootTree(focusedButton("field", "", "0,200,100,250")))
	result := run(t, tr, "appId: com.test\n---\n"+step)
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	texts := typedTexts(tr)
	if len(texts) != 1 {
		t.Fatalf("got %d typed texts, want 1", len(texts))
	}
	return texts[0], result
}

func TestInputRandomCommand_ShapesByKind(t *testing.T) {
	cases := []struct {
		name    string
		step    string
		pattern string
	}{
		{"text default length", "- inputRandom: text\n", `^[a-z]{8}$`},
		{"text length", "- inputRandom:\n    kind: text\n    length: 20\n", `^[a-z]{20}$`},
		{"number default length", "- inputRandom: number\n", `^[0-9]{8}$`},
		{"number length", "- inputRandom:\n    kind: number\n    length: 3\n", `^[0-9]{3}$`},
		{"email", "- inputRandom: email\n", `^[a-z]{8}@example\.com$`},
		{"email length", "- inputRandom:\n    kind: email\n    length: 12\n", `^[a-z]{12}@example\.com$`},
		{"person name", "- inputRandom: personName\n", `^[A-Z][a-z]+ [A-Z][a-z]+$`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := runInputRandom(t, 7, tc.step)
			if !regexp.MustCompile(tc.pattern).MatchString(got) {
				t.Errorf("typed %q, want a match for %s", got, tc.pattern)
			}
		})
	}
}

func TestInputRandomCommand_SeedMakesTheValueRepeatable(t *testing.T) {
	first, _ := runInputRandom(t, 42, "- inputRandom: text\n")
	second, _ := runInputRandom(t, 42, "- inputRandom: text\n")
	other, _ := runInputRandom(t, 43, "- inputRandom: text\n")
	if first != second {
		t.Errorf("the same seed produced %q and %q", first, second)
	}
	if first == other {
		t.Errorf("different seeds produced the same value %q", first)
	}
}

func TestInputRandomCommand_ConsecutiveStepsDrawNewValues(t *testing.T) {
	tr := newTestRunner()
	tr.runner.deps.Random = NewSeededRandom(1)
	tr.driver.StageHierarchy(rootTree(focusedButton("field", "", "0,200,100,250")))

	result := run(t, tr, "appId: com.test\n---\n- inputRandom: text\n- inputRandom: text\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	texts := typedTexts(tr)
	if len(texts) != 2 || texts[0] == texts[1] {
		t.Errorf("typed %v, want two different values", texts)
	}
}

func TestInputRandomCommand_DetailNamesTheLengthNotTheValue(t *testing.T) {
	value, result := runInputRandom(t, 5, "- inputRandom:\n    kind: number\n    length: 6\n")
	got := detail(lastStep(t, result))
	if got != "Typed a random number of 6 characters into focused field" {
		t.Errorf("detail = %q", got)
	}
	if strings.Contains(got, value) {
		t.Errorf("detail %q repeats the typed value", got)
	}
}

func TestNewScriptRunner_DefaultsToAnEntropySeededSource(t *testing.T) {
	tr := newTestRunner()
	if tr.runner.deps.Random == nil {
		t.Fatalf("the runner has no random source by default")
	}
}

func TestSummarizeStep_InputSteps(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		{"- hideKeyboard\n", "hideKeyboard"},
		{"- eraseText\n", "eraseText"},
		{"- eraseText: 1\n", "eraseText: 1 character"},
		{"- eraseText: 12\n", "eraseText: 12 characters"},
		{"- scrollUntilVisible: Footer\n", `scrollUntilVisible: element.text = "Footer"`},
		{"- scrollUntilVisible:\n    text: Footer\n    direction: up\n", `scrollUntilVisible: element.text = "Footer" (direction="up")`},
		{"- waitForAnimationToEnd\n", "waitForAnimationToEnd"},
		{"- waitForAnimationToEnd: 2000\n", `waitForAnimationToEnd (timeout="2000ms")`},
		{"- inputRandom: email\n", "inputRandom: email"},
		{"- inputRandom:\n    kind: text\n    length: 4\n", "inputRandom: text (length=4)"},
	}
	for _, tc := range cases {
		def := mustParse(t, "appId: com.test\n---\n"+tc.yaml)
		if got := SummarizeStep(def.Steps[0]).Text; got != tc.want {
			t.Errorf("summary of %q = %q, want %q", tc.yaml, got, tc.want)
		}
	}
}
