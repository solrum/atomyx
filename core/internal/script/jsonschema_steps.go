package script

// stepCommandOrder lists every step definition name in the same order
// ScriptStep's TypeScript union declares its variants, so the
// generated schema's "oneOf" reads in the order a script author
// encounters the commands in the reference documentation.
var stepCommandOrder = []string{
	"LaunchAppStep",
	"TapStep",
	"TypeStep",
	"WaitForStep",
	"AssertVisibleStep",
	"AssertNotVisibleStep",
	"AssertExistsStep",
	"ScreenshotStep",
	"SwipeStep",
	"PressKeyStep",
	"BackStep",
	"SleepStep",
	"CaptureStep",
	"AssertApiStep",
	"ExtractStep",
	"ReadStep",
	"AssertExprStep",
	"HandleStep",
	"BranchStep",
	"RunFlowStep",
	"PointerStep",
	"LongPressStep",
	"PinchStep",
	"RotateStep",
}

// captureVarPattern is the shape a capture-bound variable name must
// match, shared by CaptureStep.As, AssertApiStep.From, ExtractStep.From,
// BranchStep.From, and ReadStep.As.
var captureVarPattern = captureVarRegex.String()

func stepJSONSchemaDefinitions() map[string]any {
	return map[string]any{
		"LaunchAppStep": objectSchema(
			map[string]any{"command": constString(CommandLaunchApp)},
			[]string{"command"},
			"Launch (or relaunch) the app declared at the top of the script via `appId`.",
		),
		"TapStep": objectSchema(
			map[string]any{
				"command":  constString(CommandTap),
				"selector": ref("ScriptSelector"),
			},
			[]string{"command", "selector"},
			"Tap the element resolved by `selector`.",
		),
		"TypeStep": objectSchema(
			map[string]any{
				"command": constString(CommandType),
				"text":    stringSchema("Text to type. `${var}` placeholders are resolved against the script env."),
				"into":    ref("ScriptSelector"),
			},
			[]string{"command", "text"},
			"Type `text` into the field resolved by `into`, or the currently focused field when `into` is omitted.",
		),
		"WaitForStep": objectSchema(
			map[string]any{
				"command":   constString(CommandWaitFor),
				"selector":  ref("ScriptSelector"),
				"timeoutMs": integerMin(0, "Polling timeout in milliseconds."),
			},
			[]string{"command", "selector"},
			"Wait for `selector` to become visible within `timeoutMs`.",
		),
		"AssertVisibleStep": objectSchema(
			map[string]any{
				"command":   constString(CommandAssertVisible),
				"selector":  ref("ScriptSelector"),
				"timeoutMs": integerMin(0, "Polling timeout in milliseconds."),
			},
			[]string{"command", "selector"},
			"Assert `selector` is currently visible.",
		),
		"AssertNotVisibleStep": objectSchema(
			map[string]any{
				"command":   constString(CommandAssertNotVisible),
				"selector":  ref("ScriptSelector"),
				"timeoutMs": integerMin(0, "How long to wait for the element to disappear, in milliseconds."),
			},
			[]string{"command", "selector"},
			"Assert `selector` is NOT visible.",
		),
		"AssertExistsStep": objectSchema(
			map[string]any{
				"command":   constString(CommandAssertExists),
				"selector":  ref("ScriptSelector"),
				"timeoutMs": integerMin(0, "Polling timeout in milliseconds."),
			},
			[]string{"command", "selector"},
			"Assert a node matching `selector` is present in the UI hierarchy.",
		),
		"ScreenshotStep": objectSchema(
			map[string]any{
				"command": constString(CommandScreenshot),
				"label":   stringSchema("Filename-safe label attached to the screenshot in the run artifacts."),
			},
			[]string{"command"},
			"Capture a screenshot and attach it to the run artifacts.",
		),
		"SwipeStep": objectSchema(
			map[string]any{
				"command":   constString(CommandSwipe),
				"direction": stringEnum([]string{"up", "down", "left", "right"}, "Swipe direction."),
			},
			[]string{"command", "direction"},
			"Swipe the centre of the visible viewport in a cardinal direction.",
		),
		"PressKeyStep": objectSchema(
			map[string]any{
				"command": constString(CommandPressKey),
				"key":     stringMinLen(1, "Key identifier, for example 'enter', 'back', or 'home'."),
			},
			[]string{"command", "key"},
			"Press a hardware or system key.",
		),
		"BackStep": objectSchema(
			map[string]any{"command": constString(CommandBack)},
			[]string{"command"},
			"Navigate back one step.",
		),
		"SleepStep": objectSchema(
			map[string]any{
				"command": constString(CommandSleep),
				"ms":      integerMin(0, "Sleep duration in milliseconds."),
			},
			[]string{"command", "ms"},
			"Unconditionally wait `ms` milliseconds.",
		),
		"CaptureStep": objectSchema(
			map[string]any{
				"command": constString(CommandCapture),
				"pattern": stringMinLen(1, "Method plus path pattern to match against captured traffic, for example 'POST /api/login'."),
				"as":      stringMinLenPattern(1, captureVarPattern, "Variable name to bind the captured request to."),
			},
			[]string{"command", "pattern", "as"},
			"Capture the next request matching `pattern` via the network proxy.",
		),
		"AssertApiStep": objectSchema(
			map[string]any{
				"command": constString(CommandAssertApi),
				"from":    stringMinLenPattern(1, captureVarPattern, "Variable name of a previously captured request."),
				"status":  integerMinMax(100, 599, "Expected HTTP status code."),
				"body":    anyMapSchema("Body assertions keyed by dot-path."),
			},
			[]string{"command", "from"},
			"Assert that a captured request's response matches the given status and/or body shape.",
		),
		"ExtractStep": objectSchema(
			map[string]any{
				"command": constString(CommandExtract),
				"from":    stringMinLenPattern(1, captureVarPattern, "Variable name of a previously captured request."),
				"values":  stringMapSchema("Map of target variable name to dot-path extracted from the captured response."),
			},
			[]string{"command", "from", "values"},
			"Extract fields from a captured API response into runtime variables.",
		),
		"ReadStep": objectSchema(
			map[string]any{
				"command":  constString(CommandRead),
				"selector": ref("ScriptSelector"),
				"field":    stringEnum(readFieldValues(), "Which field to read from the resolved element. Defaults to 'value'."),
				"as":       stringMinLenPattern(1, captureVarPattern, "Variable name to bind the read value to."),
			},
			[]string{"command", "selector", "as"},
			"Resolve an element and read one field into a runtime variable.",
		),
		"AssertExprStep": objectSchema(
			map[string]any{
				"command": constString(CommandAssertExpr),
				"expr":    stringMinLen(1, "Boolean expression evaluated against the script's runtime variables."),
			},
			[]string{"command", "expr"},
			"Assert a boolean expression over runtime variables.",
		),
		"HandleStep": objectSchema(
			map[string]any{
				"command":   constString(CommandHandle),
				"branches":  arraySchema(ref("HandleBranch"), 1, "Ordered list of branches. First matching `when` wins."),
				"otherwise": stringEnum([]string{"fail", "skip"}, "Action when no branch matches within `timeout`. Defaults to 'fail'."),
				"timeout":   integerMin(0, "Max time in milliseconds the runner waits for any branch's `when` condition to hold."),
			},
			[]string{"command", "branches"},
			"Probe the current screen state and run the first matching branch.",
		),
		"BranchStep": objectSchema(
			map[string]any{
				"command": constString(CommandBranch),
				"from":    stringMinLenPattern(1, captureVarPattern, "Variable name of a previously captured request to route on."),
				"on":      arraySchema(ref("BranchCase"), 1, "Ordered list of cases. First matching `match` wins."),
				"default": stepsOrFileSchema(),
			},
			[]string{"command", "from", "on"},
			"Route on the request captured as `from`.",
		),
		"RunFlowStep": objectSchema(
			map[string]any{
				"command": constString(CommandRunFlow),
				"file":    stringMinLen(1, "Path to the sub-flow YAML file, relative to the current working directory."),
				"env":     stringMapSchema("Extra variables to pass to the sub-flow, merged over the parent env."),
			},
			[]string{"command", "file"},
			"Execute another YAML script as a sub-flow.",
		),
		"PointerStep": objectSchema(
			map[string]any{
				"command":        constString(CommandPointer),
				"touches":        arraySchema(ref("PointerTouch"), 1, "One or more pointer paths. A single entry is a single-pointer gesture; more than one entry is a multi-pointer gesture whose paths run in parallel on a shared clock."),
				"moveDurationMs": numberMin(0, "Duration in milliseconds of each `move` action in this gesture. Defaults to 200 ms."),
			},
			[]string{"command", "touches"},
			"General-purpose pointer gesture expressing W3C Actions sequences: tap, long-press, drag, flick, pinch, rotate, multi-finger. Its `touches` list holds one path per pointer.",
		),
		"LongPressStep": objectSchema(
			map[string]any{
				"command":     constString(CommandLongPress),
				"at":          ref("PointerTarget"),
				"holdMs":      numberMin(0, "Hold duration in milliseconds before release. Defaults to 500 ms."),
				"thenSwipeTo": ref("PointerTarget"),
			},
			[]string{"command", "at"},
			"Press and hold at `at` for `holdMs`, then release. When `thenSwipeTo` is set, drag to it before releasing (a hold-then-drag).",
		),
		"PinchStep": objectSchema(
			map[string]any{
				"command":    constString(CommandPinch),
				"center":     ref("PointerTarget"),
				"startGap":   numberMin(0, "Distance in points between the two contact points at the start of the pinch."),
				"endGap":     numberMin(0, "Distance in points between the two contact points at the end of the pinch. Greater than `startGap` spreads the fingers (a zoom-in)."),
				"durationMs": numberMin(0, "Duration in milliseconds of the pinch motion. Defaults to 200 ms."),
				"axis":       stringEnum(PinchAxisValues, "Axis the two contact points spread along. Defaults to 'vertical'."),
			},
			[]string{"command", "center", "startGap", "endGap"},
			"Two-finger pinch centered on `center`, moving the contact points from `startGap` apart to `endGap` apart. A selector `center` is resolved to a coordinate at dispatch time.",
		),
		"RotateStep": objectSchema(
			map[string]any{
				"command":    constString(CommandRotate),
				"center":     ref("PointerTarget"),
				"degrees":    numberSchema("Signed rotation in degrees each contact point sweeps around the center."),
				"radius":     numberMin(0, "Distance in points from the center to each contact point. Defaults to 100 points."),
				"durationMs": numberMin(0, "Duration in milliseconds of the rotation motion. Defaults to 200 ms."),
			},
			[]string{"command", "center", "degrees"},
			"Two-finger rotation of `degrees` around `center`, with the contact points placed `radius` apart on opposite sides. A selector `center` is resolved to a coordinate at dispatch time.",
		),
	}
}

func readFieldValues() []string {
	values := make([]string, 0, len(ReadAttributeFields)+len(ReadStateFields))
	values = append(values, ReadAttributeFields...)
	values = append(values, ReadStateFields...)
	return values
}
