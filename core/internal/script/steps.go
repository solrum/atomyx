package script

// ScriptStep is the discriminated union of every canonical step kind
// the runner accepts. Adding a new step kind means adding a new struct
// implementing this
// interface plus one entry in the normalizer table in normalize.go.
// This interface itself never changes.
type ScriptStep interface {
	StepCommand() string
	isScriptStep()
	// authoredStep returns the step as the author wrote it when variable
	// interpolation changed it, and nil otherwise.
	authoredStep() ScriptStep
	setAuthored(ScriptStep)
	// writtenFormLost reports that the step's written form could not be
	// rebuilt, so nothing the step holds is safe to show.
	writtenFormLost() bool
	markWrittenFormLost()
}

// Step command names, one per ScriptStep implementation.
const (
	CommandLaunchApp        = "launchApp"
	CommandTap              = "tap"
	CommandType             = "type"
	CommandWaitFor          = "waitFor"
	CommandAssertVisible    = "assertVisible"
	CommandAssertNotVisible = "assertNotVisible"
	CommandAssertExists     = "assertExists"
	CommandScreenshot       = "screenshot"
	CommandSwipe            = "swipe"
	CommandPressKey         = "pressKey"
	CommandBack             = "back"
	CommandSleep            = "sleep"
	CommandCapture          = "capture"
	CommandAssertApi        = "assertApi"
	CommandExtract          = "extract"
	CommandRead             = "read"
	CommandAssertExpr       = "assertExpr"
	CommandHandle           = "handle"
	CommandBranch           = "branch"
	CommandRunFlow          = "runFlow"
	CommandPointer          = "pointer"
	CommandLongPress        = "longPress"
	CommandPinch            = "pinch"
	CommandRotate           = "rotate"
	CommandRepeat           = "repeat"
	CommandRetry            = "retry"
	CommandStopApp          = "stopApp"
	CommandKillApp          = "killApp"
	CommandClearState       = "clearState"

	CommandHideKeyboard          = "hideKeyboard"
	CommandEraseText             = "eraseText"
	CommandScrollUntilVisible    = "scrollUntilVisible"
	CommandWaitForAnimationToEnd = "waitForAnimationToEnd"
	CommandInputRandom           = "inputRandom"
)

// RepeatWhileIterationLimit is the most iterations a repeat step runs
// when it carries a `while` condition and no `times`. A loop whose
// condition still holds after that many iterations fails instead of
// running on, so a condition that never turns false cannot hang a run.
const RepeatWhileIterationLimit = 50

// LaunchAppStep launches (or relaunches) the app declared by the
// script's appId. ClearState true clears the app's data and then starts
// it fresh. StopApp true force-stops a running instance before the
// launch, and StopApp false reuses a running instance. A nil option
// leaves the run's configured launch policy in effect. Arguments are
// named values, rendered as text, that the app reads at startup.
type LaunchAppStep struct {
	stepSource
	Command    string            `json:"command"`
	ClearState *bool             `json:"clearState,omitempty"`
	StopApp    *bool             `json:"stopApp,omitempty"`
	Arguments  map[string]string `json:"arguments,omitempty"`
}

func (s *LaunchAppStep) StepCommand() string { return s.Command }
func (s *LaunchAppStep) isScriptStep()       {}

// TapStep taps the element resolved by Selector.
type TapStep struct {
	stepSource
	Command  string         `json:"command"`
	Selector ScriptSelector `json:"selector"`
}

func (s *TapStep) StepCommand() string { return s.Command }
func (s *TapStep) isScriptStep()       {}

// TypeStep types Text into the field resolved by Into, or the
// currently focused field when Into is nil.
type TypeStep struct {
	stepSource
	Command string          `json:"command"`
	Text    string          `json:"text"`
	Into    *ScriptSelector `json:"into,omitempty"`
}

func (s *TypeStep) StepCommand() string { return s.Command }
func (s *TypeStep) isScriptStep()       {}

// WaitForStep polls for Selector to become visible within TimeoutMs,
// failing the script's precondition (not the step) if it never does.
type WaitForStep struct {
	stepSource
	Command   string         `json:"command"`
	Selector  ScriptSelector `json:"selector"`
	TimeoutMs *int           `json:"timeoutMs,omitempty"`
}

func (s *WaitForStep) StepCommand() string { return s.Command }
func (s *WaitForStep) isScriptStep()       {}

// AssertVisibleStep asserts Selector is visible, failing the step
// itself (not a precondition) when it is absent.
type AssertVisibleStep struct {
	stepSource
	Command   string         `json:"command"`
	Selector  ScriptSelector `json:"selector"`
	TimeoutMs *int           `json:"timeoutMs,omitempty"`
}

func (s *AssertVisibleStep) StepCommand() string { return s.Command }
func (s *AssertVisibleStep) isScriptStep()       {}

// AssertNotVisibleStep asserts Selector is NOT visible.
type AssertNotVisibleStep struct {
	stepSource
	Command   string         `json:"command"`
	Selector  ScriptSelector `json:"selector"`
	TimeoutMs *int           `json:"timeoutMs,omitempty"`
}

func (s *AssertNotVisibleStep) StepCommand() string { return s.Command }
func (s *AssertNotVisibleStep) isScriptStep()       {}

// AssertExistsStep asserts a node matching Selector is present in the
// UI hierarchy, failing the step itself (not a precondition) when it
// is absent.
type AssertExistsStep struct {
	stepSource
	Command   string         `json:"command"`
	Selector  ScriptSelector `json:"selector"`
	TimeoutMs *int           `json:"timeoutMs,omitempty"`
}

func (s *AssertExistsStep) StepCommand() string { return s.Command }
func (s *AssertExistsStep) isScriptStep()       {}

// ScreenshotStep captures a screenshot, optionally labeled.
type ScreenshotStep struct {
	stepSource
	Command string  `json:"command"`
	Label   *string `json:"label,omitempty"`
}

func (s *ScreenshotStep) StepCommand() string { return s.Command }
func (s *ScreenshotStep) isScriptStep()       {}

// SwipeStep swipes the viewport's centre in Direction: one of "up",
// "down", "left", or "right".
type SwipeStep struct {
	stepSource
	Command   string `json:"command"`
	Direction string `json:"direction"`
}

func (s *SwipeStep) StepCommand() string { return s.Command }
func (s *SwipeStep) isScriptStep()       {}

// PressKeyStep presses the hardware or system key named by Key.
type PressKeyStep struct {
	stepSource
	Command string `json:"command"`
	Key     string `json:"key"`
}

func (s *PressKeyStep) StepCommand() string { return s.Command }
func (s *PressKeyStep) isScriptStep()       {}

// BackStep navigates back one step.
type BackStep struct {
	stepSource
	Command string `json:"command"`
}

func (s *BackStep) StepCommand() string { return s.Command }
func (s *BackStep) isScriptStep()       {}

// SleepStep unconditionally waits Ms milliseconds.
type SleepStep struct {
	stepSource
	Command string `json:"command"`
	Ms      int    `json:"ms"`
}

func (s *SleepStep) StepCommand() string { return s.Command }
func (s *SleepStep) isScriptStep()       {}

// CaptureStep arms the network-capture proxy for the next request
// matching Pattern ("METHOD /path", `*` wildcard supported) and binds
// it to the variable named As.
type CaptureStep struct {
	stepSource
	Command string `json:"command"`
	Pattern string `json:"pattern"`
	As      string `json:"as"`
}

func (s *CaptureStep) StepCommand() string { return s.Command }
func (s *CaptureStep) isScriptStep()       {}

// AssertApiStep asserts that the request captured as From matches
// Status and/or the dot-path Body assertions. Body preserves the
// declaration order of its keys, because a failure listing more than
// one mismatched key reports them in that order.
type AssertApiStep struct {
	stepSource
	Command string      `json:"command"`
	From    string      `json:"from"`
	Status  *int        `json:"status,omitempty"`
	Body    *OrderedMap `json:"body,omitempty"`
}

func (s *AssertApiStep) StepCommand() string { return s.Command }
func (s *AssertApiStep) isScriptStep()       {}

// ExtractStep extracts fields from the request captured as From into
// runtime variables: Values maps a target variable name to a dot-path
// into the captured response, preserving the declaration order of
// its keys.
type ExtractStep struct {
	stepSource
	Command string            `json:"command"`
	From    string            `json:"from"`
	Values  *OrderedStringMap `json:"values"`
}

func (s *ExtractStep) StepCommand() string { return s.Command }
func (s *ExtractStep) isScriptStep()       {}

// ReadStep resolves Selector and reads Field (defaulting to "value")
// into the variable named As.
type ReadStep struct {
	stepSource
	Command  string         `json:"command"`
	Selector ScriptSelector `json:"selector"`
	Field    *string        `json:"field,omitempty"`
	As       string         `json:"as"`
}

func (s *ReadStep) StepCommand() string { return s.Command }
func (s *ReadStep) isScriptStep()       {}

// AssertExprStep asserts that Expr, a boolean expression over runtime
// variables, evaluates to true. Expr is carried as an opaque string.
// this stage does not evaluate expressions.
type AssertExprStep struct {
	stepSource
	Command string `json:"command"`
	Expr    string `json:"expr"`
}

func (s *AssertExprStep) StepCommand() string { return s.Command }
func (s *AssertExprStep) isScriptStep()       {}

// ReadAttributeFields lists the attribute-bag fields ReadStep can
// fetch; each yields a string.
var ReadAttributeFields = []string{
	"id", "text", "label", "hint", "value", "role", "class", "package", "bounds",
}

// ReadStateFields lists the boolean state fields ReadStep can fetch.
var ReadStateFields = []string{
	"clickable", "enabled", "focused", "selected", "checked", "visible",
}

func isValidReadField(v string) bool {
	for _, f := range ReadAttributeFields {
		if f == v {
			return true
		}
	}
	for _, f := range ReadStateFields {
		if f == v {
			return true
		}
	}
	return false
}

// HandleCondition is the condition a handle branch matches on.
// Exactly one of Visible / NotVisible should be set (unlike
// PreconditionCheck, this is descriptive only, not enforced).
type HandleCondition struct {
	Visible    *StringOrSelector `json:"visible,omitempty"`
	NotVisible *StringOrSelector `json:"notVisible,omitempty"`
}

// HandleBranch is one branch of a handle step: a condition plus the
// steps to run when it matches.
type HandleBranch struct {
	When HandleCondition `json:"when"`
	Do   StepsOrFile     `json:"do"`
}

// HandleStep probes the current screen state and runs the first
// matching Branches entry; Otherwise ("fail" or "skip") fires when no
// branch matches within Timeout milliseconds.
type HandleStep struct {
	stepSource
	Command   string         `json:"command"`
	Branches  []HandleBranch `json:"branches"`
	Otherwise *string        `json:"otherwise,omitempty"`
	Timeout   *int           `json:"timeout,omitempty"`
}

func (s *HandleStep) StepCommand() string { return s.Command }
func (s *HandleStep) isScriptStep()       {}

// BranchMatchCondition is a branch case's match condition: Status
// and Body are ANDed together when both are set. Body preserves the
// declaration order of its keys, matching AssertApiStep.Body.
type BranchMatchCondition struct {
	Status *int        `json:"status,omitempty"`
	Body   *OrderedMap `json:"body,omitempty"`
}

// BranchCase is one case of a branch step: a match condition plus the
// steps to run when it matches.
type BranchCase struct {
	Match BranchMatchCondition `json:"match"`
	Do    StepsOrFile          `json:"do"`
}

// BranchStep routes on the request captured as From: the first
// matching On case runs, or Default when none match (a non-match is a
// step failure when Default is nil).
type BranchStep struct {
	stepSource
	Command string       `json:"command"`
	From    string       `json:"from"`
	On      []BranchCase `json:"on"`
	Default *StepsOrFile `json:"default,omitempty"`
}

func (s *BranchStep) StepCommand() string { return s.Command }
func (s *BranchStep) isScriptStep()       {}

// RunFlowStep executes the sub-flow at File in this script's context,
// merging Env over the parent's variables.
type RunFlowStep struct {
	stepSource
	Command string            `json:"command"`
	File    string            `json:"file"`
	Env     map[string]string `json:"env,omitempty"`
}

func (s *RunFlowStep) StepCommand() string { return s.Command }
func (s *RunFlowStep) isScriptStep()       {}

// PointerStep expresses a W3C Actions pointer gesture as a non-empty
// list of Touches. One touch is a single-pointer gesture; more than
// one touch is a multi-pointer gesture whose touches run in parallel.
// MoveDurationMs is the duration of each move action.
type PointerStep struct {
	stepSource
	Command        string         `json:"command"`
	Touches        []PointerTouch `json:"touches"`
	MoveDurationMs *float64       `json:"moveDurationMs,omitempty"`
}

func (s *PointerStep) StepCommand() string { return s.Command }
func (s *PointerStep) isScriptStep()       {}

// LongPressStep holds At down, waits HoldMs, then releases; when
// ThenSwipeTo is set it drags to that target before releasing (a
// hold-then-drag). At and ThenSwipeTo carry the same location union a
// raw pointer action's target does: either an absolute point or a
// selector the driver resolves at dispatch time. HoldMs is optional;
// its zero-value default is applied by the runner.
type LongPressStep struct {
	stepSource
	Command     string         `json:"command"`
	At          *PointerTarget `json:"at"`
	HoldMs      *float64       `json:"holdMs,omitempty"`
	ThenSwipeTo *PointerTarget `json:"thenSwipeTo,omitempty"`
}

func (s *LongPressStep) StepCommand() string { return s.Command }
func (s *LongPressStep) isScriptStep()       {}

// PinchStep is a two-finger pinch centered on Center, moving the two
// contact points from StartGap apart to EndGap apart along Axis over
// DurationMs. EndGap greater than StartGap spreads the fingers (a
// zoom-in). Center carries the same location union a pointer action's
// target does; the runner resolves a selector center to a coordinate
// before it can place the two symmetric contacts. Axis and DurationMs
// are optional; their defaults are applied by the runner.
type PinchStep struct {
	stepSource
	Command    string         `json:"command"`
	Center     *PointerTarget `json:"center"`
	StartGap   float64        `json:"startGap"`
	EndGap     float64        `json:"endGap"`
	DurationMs *float64       `json:"durationMs,omitempty"`
	Axis       *string        `json:"axis,omitempty"`
}

func (s *PinchStep) StepCommand() string { return s.Command }
func (s *PinchStep) isScriptStep()       {}

// RotateStep is a two-finger rotation: two contact points on opposite
// sides of Center at Radius, each sweeping Degrees around the center
// over DurationMs. Center carries the same location union a pointer
// action's target does; the runner resolves a selector center to a
// coordinate before placing the contacts. Radius and DurationMs are
// optional; their defaults are applied by the runner.
type RotateStep struct {
	stepSource
	Command    string         `json:"command"`
	Center     *PointerTarget `json:"center"`
	Degrees    float64        `json:"degrees"`
	Radius     *float64       `json:"radius,omitempty"`
	DurationMs *float64       `json:"durationMs,omitempty"`
}

func (s *RotateStep) StepCommand() string { return s.Command }
func (s *RotateStep) isScriptStep()       {}

// RepeatCondition is the loop condition of a repeat step. Exactly one
// of Visible, NotVisible or Expr is set. Visible and NotVisible probe
// the current screen, and Expr is a boolean expression over the run's
// variables, evaluated by the same engine assertExpr uses.
type RepeatCondition struct {
	Visible    *StringOrSelector `json:"visible,omitempty"`
	NotVisible *StringOrSelector `json:"notVisible,omitempty"`
	Expr       *string           `json:"expr,omitempty"`
}

// RepeatStep runs Commands in a loop. With Times alone it runs exactly
// Times iterations. With While alone it runs while the condition holds,
// up to RepeatWhileIterationLimit iterations. With both it stops at
// whichever comes first. The condition is checked before every
// iteration.
type RepeatStep struct {
	stepSource
	Command  string           `json:"command"`
	Times    *int             `json:"times,omitempty"`
	While    *RepeatCondition `json:"while,omitempty"`
	Commands []ScriptStep     `json:"commands"`
}

func (s *RepeatStep) StepCommand() string { return s.Command }
func (s *RepeatStep) isScriptStep()       {}

// Bounds of a retry step's maxRetries.
const (
	// RetryMaxRetriesLimit is the largest maxRetries a retry step accepts.
	RetryMaxRetriesLimit = 3
	// DefaultRetryMaxRetries is the number of extra attempts a retry step
	// makes when it sets no maxRetries.
	DefaultRetryMaxRetries = 1
)

// RetryStep runs Commands and, when any of them fails, runs the whole
// list again from its first step, up to MaxRetries extra attempts. A nil
// MaxRetries means DefaultRetryMaxRetries.
type RetryStep struct {
	stepSource
	Command    string       `json:"command"`
	MaxRetries *int         `json:"maxRetries,omitempty"`
	Commands   []ScriptStep `json:"commands"`
}

func (s *RetryStep) StepCommand() string { return s.Command }
func (s *RetryStep) isScriptStep()       {}

// StopAppStep stops an app. A nil AppID means the script's own appId.
type StopAppStep struct {
	stepSource
	Command string  `json:"command"`
	AppID   *string `json:"appId,omitempty"`
}

func (s *StopAppStep) StepCommand() string { return s.Command }
func (s *StopAppStep) isScriptStep()       {}

// KillAppStep terminates an app. A nil AppID means the script's own
// appId. Every platform adapter implements it with the same device
// operation as StopApp, so the two steps behave identically.
type KillAppStep struct {
	stepSource
	Command string  `json:"command"`
	AppID   *string `json:"appId,omitempty"`
}

func (s *KillAppStep) StepCommand() string { return s.Command }
func (s *KillAppStep) isScriptStep()       {}

// ClearStateStep resets an app's data to a clean state. A nil AppID
// means the script's own appId.
type ClearStateStep struct {
	stepSource
	Command string  `json:"command"`
	AppID   *string `json:"appId,omitempty"`
}

func (s *ClearStateStep) StepCommand() string { return s.Command }
func (s *ClearStateStep) isScriptStep()       {}

// Kinds of value an inputRandom step types.
const (
	RandomKindText       = "text"
	RandomKindNumber     = "number"
	RandomKindEmail      = "email"
	RandomKindPersonName = "personName"
)

// RandomKinds lists every kind an inputRandom step accepts.
var RandomKinds = []string{RandomKindText, RandomKindNumber, RandomKindEmail, RandomKindPersonName}

// Bounds and defaults of the keyboard and wait steps.
const (
	// DefaultEraseTextCharacters is the number of characters an
	// eraseText step erases when it sets no charactersToErase.
	DefaultEraseTextCharacters = 50
	// DefaultRandomLength is the length of the generated part of an
	// inputRandom value when the step sets no length.
	DefaultRandomLength = 8
	// RandomLengthLimit is the largest length an inputRandom step
	// accepts.
	RandomLengthLimit = 256
	// DefaultAnimationTimeoutMs is how long a waitForAnimationToEnd
	// step waits for the screen to settle when it sets no timeout.
	DefaultAnimationTimeoutMs = 5000
)

// HideKeyboardStep dismisses the on-screen keyboard, if one is showing.
type HideKeyboardStep struct {
	stepSource
	Command string `json:"command"`
}

func (s *HideKeyboardStep) StepCommand() string { return s.Command }
func (s *HideKeyboardStep) isScriptStep()       {}

// EraseTextStep erases characters backwards from the focused field. A
// nil Characters means DefaultEraseTextCharacters.
type EraseTextStep struct {
	stepSource
	Command    string `json:"command"`
	Characters *int   `json:"charactersToErase,omitempty"`
}

func (s *EraseTextStep) StepCommand() string { return s.Command }
func (s *EraseTextStep) isScriptStep()       {}

// ScrollUntilVisibleStep scrolls until the element resolved by Selector
// sits in the viewport. Direction is "up" or "down", the scroll
// direction searched first, and nil means "down".
type ScrollUntilVisibleStep struct {
	stepSource
	Command   string         `json:"command"`
	Selector  ScriptSelector `json:"selector"`
	Direction *string        `json:"direction,omitempty"`
}

func (s *ScrollUntilVisibleStep) StepCommand() string { return s.Command }
func (s *ScrollUntilVisibleStep) isScriptStep()       {}

// WaitForAnimationToEndStep waits until the screen stops changing, or
// until TimeoutMs elapses. A nil TimeoutMs means
// DefaultAnimationTimeoutMs.
type WaitForAnimationToEndStep struct {
	stepSource
	Command   string `json:"command"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
}

func (s *WaitForAnimationToEndStep) StepCommand() string { return s.Command }
func (s *WaitForAnimationToEndStep) isScriptStep()       {}

// InputRandomStep types a generated value of Kind into the focused
// field. Length sizes the generated text or digits and applies to the
// text, number and email kinds. A nil Length means DefaultRandomLength.
type InputRandomStep struct {
	stepSource
	Command string `json:"command"`
	Kind    string `json:"kind"`
	Length  *int   `json:"length,omitempty"`
}

func (s *InputRandomStep) StepCommand() string { return s.Command }
func (s *InputRandomStep) isScriptStep()       {}
