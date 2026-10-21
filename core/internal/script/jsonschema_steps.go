package script

import (
	"fmt"

	"dev.atomyx/internal/driver"
)

// stepCommandOrder lists every step definition name in the order the
// ScriptStep union declares its variants, so the
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
	"RepeatStep",
	"RetryStep",
	"StopAppStep",
	"KillAppStep",
	"ClearStateStep",
	"OpenLinkStep",
	"SetPermissionsStep",
	"SetOrientationStep",
	"SetLocationStep",
	"TravelStep",
	"HideKeyboardStep",
	"EraseTextStep",
	"ScrollUntilVisibleStep",
	"WaitForAnimationToEndStep",
	"InputRandomStep",
	"HTTPRequestStep",
}

// captureVarPattern is the shape a capture-bound variable name must
// match, shared by CaptureStep.As, AssertApiStep.From, ExtractStep.From,
// BranchStep.From, and ReadStep.As.
var captureVarPattern = captureVarRegex.String()

func stepJSONSchemaDefinitions() map[string]any {
	return map[string]any{
		"LaunchAppStep": objectSchema(
			map[string]any{
				"command":     constString(CommandLaunchApp),
				"clearState":  booleanSchema("When true, clear the app's data and start it fresh. Cannot be combined with `stopApp: false`."),
				"stopApp":     booleanSchema("When true, force-stop a running instance before launching. When false, reuse a running instance. When omitted, the run's launch policy applies."),
				"arguments":   scalarMapSchema("Named values handed to the app at launch. Each value is a string, number or boolean, delivered as text. They take effect only when the launch starts the app, so a launch that reuses a running instance ignores them."),
				"permissions": permissionsMapSchema("Permissions to grant (`allow`) or revoke (`deny`) before the app starts, keyed by permission name. A launch that reuses a running instance cannot set them, and a launch that sets them is never skipped as a repeat."),
			},
			[]string{"command"},
			"Launch (or relaunch) the app declared at the top of the script via `appId`.",
		),
		"StopAppStep":    appLifecycleStepSchema(CommandStopApp, "Stop an app. Behaves the same as `killApp` on every platform."),
		"KillAppStep":    appLifecycleStepSchema(CommandKillApp, "Terminate an app. Behaves the same as `stopApp` on every platform."),
		"ClearStateStep": appLifecycleStepSchema(CommandClearState, "Reset an app's data to a clean state."),
		"OpenLinkStep": objectSchema(
			map[string]any{
				"command": constString(CommandOpenLink),
				"link":    stringMinLen(1, "URL, deep link or universal link to open. It must start with a scheme such as `https:` or `myapp:`."),
				"appId":   stringMinLen(1, "App that must open the link. When omitted, the device chooses the handler."),
			},
			[]string{"command", "link"},
			"Open a URL, deep link or universal link in the device's handler for it.",
		),
		"SetPermissionsStep": objectSchema(
			map[string]any{
				"command":     constString(CommandSetPermissions),
				"appId":       stringMinLen(1, "App whose permissions change. Defaults to the script's `appId`."),
				"permissions": permissionsMapSchema("Permissions to grant (`allow`) or revoke (`deny`), keyed by permission name. At least one is required."),
			},
			[]string{"command", "permissions"},
			"Grant or revoke app permissions without a prompt.",
		),
		"SetOrientationStep": objectSchema(
			map[string]any{
				"command":     constString(CommandSetOrientation),
				"orientation": stringEnum(orientationNames(), "Orientation to rotate the device to. It persists until changed."),
			},
			[]string{"command", "orientation"},
			"Rotate the device to portrait or to one of the landscape orientations.",
		),
		"SetLocationStep": objectSchema(
			map[string]any{
				"command":   constString(CommandSetLocation),
				"latitude":  latitudeSchema(),
				"longitude": longitudeSchema(),
			},
			[]string{"command", "latitude", "longitude"},
			"Move the device to a latitude and longitude. The position persists until changed.",
		),
		"TravelStep": objectSchema(
			map[string]any{
				"command": constString(CommandTravel),
				"points": arraySchema(
					objectSchema(
						map[string]any{
							"latitude":  latitudeSchema(),
							"longitude": longitudeSchema(),
						},
						[]string{"latitude", "longitude"},
						"One position on the route.",
					),
					driver.MinTravelPoints,
					fmt.Sprintf("Positions to visit in order, from %d to %d.", driver.MinTravelPoints, driver.MaxTravelPoints),
				),
				"speed": withDescription(map[string]any{"type": "number", "exclusiveMinimum": 0},
					fmt.Sprintf("Speed in meters per second. Defaults to %v.", driver.DefaultTravelSpeedMetersPerSecond)),
			},
			[]string{"command", "points"},
			"Move the device through a list of positions, waiting between them for the time the distance takes at the given speed.",
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
				"values":  extractValuesSchema(),
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
		"RepeatStep": objectSchema(
			map[string]any{
				"command":  constString(CommandRepeat),
				"times":    integerMin(1, "Number of iterations to run. Without `while`, the block runs exactly this many times."),
				"while":    ref("RepeatCondition"),
				"commands": arraySchema(ref("ScriptStep"), 1, "Steps to run in every iteration."),
			},
			[]string{"command", "commands"},
			fmt.Sprintf("Run `commands` in a loop. At least one of `times` or `while` is required. With `while` and no `times`, the loop fails after %d iterations if the condition still holds.", RepeatWhileIterationLimit),
		),
		"RetryStep": objectSchema(
			map[string]any{
				"command":    constString(CommandRetry),
				"maxRetries": integerMinMax(0, RetryMaxRetriesLimit, fmt.Sprintf("Extra attempts after the first one fails. Defaults to %d.", DefaultRetryMaxRetries)),
				"commands":   arraySchema(ref("ScriptStep"), 1, "Steps to run in every attempt."),
			},
			[]string{"command", "commands"},
			"Run `commands`. When any step fails, run the whole list again from its first step, up to `maxRetries` extra attempts.",
		),
	}
}

func inputStepSchemaDefinitions() map[string]any {
	return map[string]any{
		"HideKeyboardStep": objectSchema(
			map[string]any{"command": constString(CommandHideKeyboard)},
			[]string{"command"},
			"Dismiss the on-screen keyboard. Succeeds when no keyboard is showing.",
		),
		"EraseTextStep": objectSchema(
			map[string]any{
				"command": constString(CommandEraseText),
				"charactersToErase": integerMin(1, fmt.Sprintf(
					"Number of characters to erase backwards from the focused field. Defaults to %d.", DefaultEraseTextCharacters)),
			},
			[]string{"command"},
			"Erase characters from the focused field.",
		),
		"ScrollUntilVisibleStep": objectSchema(
			map[string]any{
				"command":   constString(CommandScrollUntilVisible),
				"selector":  ref("ScriptSelector"),
				"direction": stringEnum([]string{"up", "down"}, "Scroll direction searched first. The other direction is searched afterwards. Defaults to down."),
			},
			[]string{"command", "selector"},
			"Scroll until the element resolved by `selector` sits in the viewport. Fails when the element is never found.",
		),
		"WaitForAnimationToEndStep": objectSchema(
			map[string]any{
				"command":   constString(CommandWaitForAnimationToEnd),
				"timeoutMs": integerMin(0, fmt.Sprintf("Longest wait in milliseconds. Defaults to %d.", DefaultAnimationTimeoutMs)),
			},
			[]string{"command"},
			"Wait until the screen stops changing. The step passes when the timeout elapses first.",
		),
		"HTTPRequestStep": objectSchema(
			map[string]any{
				"command": constString(CommandHTTPRequest),
				"method":  stringEnum(HTTPMethods, "HTTP method."),
				"url":     stringMinLen(1, "Absolute http or https URL. The host must be listed in the project's http.allowHosts."),
				"headers": stringMapSchema("Request headers."),
				"body": anyOfSchema("Request body: a string sent as is, or an object sent as JSON.",
					stringSchema(""), anyMapSchema("")),
				"as":           stringMinLenPattern(1, captureVarPattern, "Variable name to bind the response to, readable by extract and assertApi."),
				"expectStatus": integerMinMax(100, 599, "Status the response must have. Any 2xx passes when omitted."),
				"poll": objectSchema(
					map[string]any{
						"until":      stringMinLen(1, "JSON path into the response that must resolve to a non-empty value."),
						"intervalMs": integerMin(HTTPRequestMinPollIntervalMs, fmt.Sprintf("Pause between attempts in milliseconds. Defaults to %d.", HTTPRequestDefaultPollIntervalMs)),
					},
					[]string{"until"},
					"Repeat the request until the status passes and the path resolves to a non-empty value. Requires `as`.",
				),
				"timeoutMs": integerMinMax(1, HTTPRequestMaxTimeoutMs, fmt.Sprintf("Longest time the whole step may take in milliseconds. Defaults to %d.", HTTPRequestDefaultTimeoutMs)),
			},
			[]string{"command", "url"},
			"Send an HTTP request from the host running the script, for example to fetch a one-time code from a test mailbox.",
		),
		"InputRandomStep": objectSchema(
			map[string]any{
				"command": constString(CommandInputRandom),
				"kind":    stringEnum(RandomKinds, "Kind of value to type."),
				"length": integerMinMax(1, RandomLengthLimit, fmt.Sprintf(
					"Length of the generated text, digits or email name. Defaults to %d. Does not apply to personName.", DefaultRandomLength)),
			},
			[]string{"command", "kind"},
			"Type a generated value into the focused field.",
		),
	}
}

// extractValuesSchema is the `values` map of an extract step: each target
// variable maps to a dot-path, or to an object with a dot-path and a
// regular expression applied to the resolved value.
func extractValuesSchema() map[string]any {
	return withDescription(map[string]any{
		"type": "object",
		"additionalProperties": anyOfSchema("",
			stringSchema("Dot-path extracted from the captured response."),
			objectSchema(
				map[string]any{
					"path":  stringSchema("Dot-path resolved in the captured response."),
					"regex": stringSchema("Regular expression applied to the resolved value as text. The first capture group is stored, or the whole match when the pattern has no group."),
				},
				[]string{"path"},
				"A dot-path and a regular expression narrowing the resolved value.",
			),
		),
	}, "Map of target variable name to what is extracted from the captured response.")
}

// appLifecycleStepSchema is the schema of a step that acts on one app,
// identified by an optional `appId` that defaults to the script's own.
func appLifecycleStepSchema(command, description string) map[string]any {
	return objectSchema(
		map[string]any{
			"command": constString(command),
			"appId":   stringMinLen(1, "App to act on. Defaults to the script's `appId`."),
		},
		[]string{"command"},
		description,
	)
}

func readFieldValues() []string {
	values := make([]string, 0, len(ReadAttributeFields)+len(ReadStateFields))
	values = append(values, ReadAttributeFields...)
	values = append(values, ReadStateFields...)
	return values
}
