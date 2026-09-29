package orchestra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"unicode/utf16"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/wire"
)

// Default durations and fractions for actions whose parameters carry no
// zero-value default of their own. SwipeDirection applies
// DefaultSwipeDurationMs and DefaultSwipeDirectionFraction automatically
// when its options are left nil. LongPress and LongPressAt take an
// explicit duration, so DefaultLongPressDurationMs exists only for
// callers who want the package's standard timing without hardcoding it.
const (
	DefaultLongPressDurationMs    = 500
	DefaultSwipeDurationMs        = 200
	DefaultSwipeDirectionFraction = 0.6
)

const (
	keyboardDismissTimeoutMs = 1000
	keyboardDismissPollMs    = 50

	// inputFocusSettleTimeoutMs bounds how long a type action waits for
	// its target field to actually take input focus before it gives up
	// rather than dispatch text. It is generous enough to outlast a
	// soft-keyboard reflow moving focus onto the intended field, yet
	// short enough that a genuinely unfocusable target fails promptly
	// instead of stalling the flow. inputFocusSettlePollMs is the delay
	// between focus polls.
	inputFocusSettleTimeoutMs = 1500
	inputFocusSettlePollMs    = 50
)

// OrchestraDeps constructs an Orchestra. Driver and Clock are
// required; Finder and Scroll default to a Finder and
// ScrollController built over Driver and Clock when left nil. Logger
// defaults to logging.NoopLogger{} when left nil, so a caller that
// never wires one keeps building an Orchestra that logs nothing.
type OrchestraDeps struct {
	Driver driver.Driver
	Clock  Clock
	Finder *Finder
	Scroll *ScrollController
	Logger logging.Logger
}

// Orchestra is the high-level command layer of Atomyx. It is the ONE
// place that composes:
//
//	Driver (primitives)
//	  + Finder (tree query + wait)
//	    + ScrollController (scroll-into-view)
//	      + CompileSelector (priority broadening policy)
//	        + DetectObscurement (z-order safety)
//	          = Tap(selector) / InputText(selector) / ...
//
// Contract:
//
//   - Every action that takes a Selector runs it through
//     CompileSelector (priority broadening: id > label > text >
//     value > hint; role/enabled/clickable/focused AND-ed).
//
//   - Every action that takes a selector, except Tap when its caller
//     opts out via TapOptions.ScrollToFind, goes through
//     ScrollController.EnsureVisible first. This covers virtualized-
//     list recovery (scroll-search) and positional scroll-into-view
//     with safe-area insets.
//
//   - Every action then runs DetectObscurement on the resolved node
//     against the current hierarchy. An obscured element is NOT
//     tapped blindly: the action returns an ActionResult with Ok
//     false, a Reason, and an Obscurer, with enough information for
//     the caller to dismiss the obscurer or tap it directly.
//
//   - ScrollUnreachableError from the scroll controller is caught at
//     the Orchestra boundary and converted to an Ok-false
//     ActionResult. Any other error (driver transport failure,
//     context cancellation) propagates as a Go error: those are
//     infrastructure problems, not action outcomes.
//
//   - Coordinate-based actions (TapAt, SwipeAt, LongPressAt) bypass
//     the selector, scroll, and obscurement pipeline entirely and go
//     straight to the driver.
type Orchestra struct {
	driver driver.Driver
	clock  Clock
	finder *Finder
	scroll *ScrollController
	logger logging.Logger

	// maybeKeyboardOpen is a session-level hint: set true after
	// InputText completes because typing opens the keyboard on both
	// Android and iOS. Cleared on HideKeyboard, LaunchApp, and
	// StopApp. Tap uses it to decide whether to pay the cost of a
	// keyboard-state RPC before retrying past a keyboard obscurer.
	maybeKeyboardOpen bool

	// launchedAppsMu guards launchedApps. A sidecar host may dispatch
	// tool calls against one shared Orchestra from concurrent
	// goroutines, so the launched-app set needs its own lock rather
	// than relying on the caller to serialize LaunchApp calls.
	launchedAppsMu sync.Mutex
	// launchedApps is the set of app ids LaunchApp has already
	// launched in the current flow (a standalone script run, a
	// scenario, or a runFlow tree), keyed for O(1) membership. See
	// LaunchApp and ResetLaunchedApps.
	launchedApps map[string]struct{}
}

// New constructs an Orchestra from deps.
func New(deps OrchestraDeps) *Orchestra {
	finder := deps.Finder
	if finder == nil {
		finder = NewFinder(deps.Driver, deps.Clock)
	}
	scroll := deps.Scroll
	if scroll == nil {
		scroll = NewScrollController(deps.Driver, deps.Clock, DefaultScrollControllerOptions())
	}
	logger := deps.Logger
	if logger == nil {
		logger = logging.NoopLogger{}
	}
	return &Orchestra{
		driver:       deps.Driver,
		clock:        deps.Clock,
		finder:       finder,
		scroll:       scroll,
		logger:       logger,
		launchedApps: make(map[string]struct{}),
	}
}

// runLogger returns o.logger extended with ctx's run id under the
// "runId" field, when ctx carries one, so a call site can log a
// run-correlated entry without threading the run id through its own
// parameters.
func (o *Orchestra) runLogger(ctx context.Context) logging.Logger {
	return logging.ChildFromContext(o.logger, ctx)
}

// ── Tree inspection ──────────────────────────────────────────────

// Hierarchy returns the current UI hierarchy.
func (o *Orchestra) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	return o.driver.Hierarchy(ctx)
}

// Find runs a single-shot find. Returns all matches of the compiled
// selector.
func (o *Orchestra) Find(ctx context.Context, selector Selector) ([]*TreeCursor, error) {
	return o.finder.Find(ctx, CompileSelector(selector))
}

// FindOne runs a single-shot find, returning the first match or nil.
func (o *Orchestra) FindOne(ctx context.Context, selector Selector) (*TreeCursor, error) {
	return o.finder.FindOne(ctx, CompileSelector(selector))
}

// WaitFor polls for selector until it matches or opts.TimeoutMs
// elapses. Returns FindTimeoutError on deadline miss.
func (o *Orchestra) WaitFor(ctx context.Context, selector Selector, opts WaitOptions) ([]*TreeCursor, error) {
	return o.finder.WaitFor(ctx, CompileSelector(selector), opts)
}

// ── Selector actions ─────────────────────────────────────────────

// TapOptions configures Tap. ScrollToFind nil or true runs scroll-
// into-view before resolving the tap point (the default); false
// skips it, useful when the caller knows the element is already on
// screen and wants to avoid the scroll controller's hierarchy round-
// trip.
type TapOptions struct {
	ScrollToFind *bool
}

// Tap taps the element matching selector. Full pipeline: compile,
// scroll-into-view, obscurement check, coordinate tap. Returns an
// ActionResult; it returns a non-nil error only for infrastructure
// failures, never for an action-level rejection.
//
// When the pipeline rejects a tap because the resolved element is
// obscured by the on-screen keyboard, and InputText has run more
// recently than the last HideKeyboard, LaunchApp, or StopApp call,
// Tap dismisses the keyboard and retries once automatically.
func (o *Orchestra) Tap(ctx context.Context, selector Selector, opts TapOptions) (ActionResult, error) {
	scrollToFind := opts.ScrollToFind == nil || *opts.ScrollToFind
	prepared, err := o.prepareSelectorForAction(ctx, selector, scrollToFind)
	if err != nil {
		return ActionResult{}, err
	}
	if !prepared.ok {
		if o.maybeKeyboardOpen && prepared.result.Obscurer != nil && prepared.result.Obscurer.Role == RoleKeyboard {
			if _, err := o.driver.HideKeyboard(ctx); err != nil {
				return ActionResult{}, err
			}
			_, waitErr := waitForKeyboard(ctx, o.driver, o.clock, false, keyboardDismissTimeoutMs, keyboardDismissPollMs)
			if waitErr != nil {
				var timeoutErr *WaitTimeoutError
				if !errors.As(waitErr, &timeoutErr) {
					return ActionResult{}, waitErr
				}
			}
			o.maybeKeyboardOpen = false
			return o.Tap(ctx, selector, opts)
		}
		return prepared.result, nil
	}
	if err := o.driver.Tap(ctx, prepared.point); err != nil {
		return ActionResult{}, err
	}
	return ActionOk(prepared.resolvedBy, fmt.Sprintf("tapped at %s", pointStr(prepared.point))), nil
}

// LongPress long-presses the element matching selector. Same
// pipeline as Tap, but dispatches a long-press primitive instead of
// a tap. Unlike Tap, the caller cannot opt out of scroll-into-view.
func (o *Orchestra) LongPress(ctx context.Context, selector Selector, durationMs int) (ActionResult, error) {
	prepared, err := o.prepareSelectorForAction(ctx, selector, true)
	if err != nil {
		return ActionResult{}, err
	}
	if !prepared.ok {
		return prepared.result, nil
	}
	if err := o.driver.LongPress(ctx, prepared.point, durationMs); err != nil {
		return ActionResult{}, err
	}
	return ActionOk(prepared.resolvedBy, fmt.Sprintf("long-pressed at %s for %dms", pointStr(prepared.point), durationMs)), nil
}

// InputTextOptions configures InputText. ClearFirst nil or true
// erases existing content with Driver.EraseText(999) before typing
// (the default); false appends to the existing value.
type InputTextOptions struct {
	ClearFirst *bool
}

// InputText types text into the element matching selector. Pipeline:
// compile, scroll-into-view, obscurement, tap-to-focus, wait for the
// target to hold focus, InputText primitive. Unlike Tap, the caller
// cannot opt out of scroll-into-view.
//
// The tap-to-focus does not settle synchronously on every device: a
// soft-keyboard reflow or a secure field swapping in can leave focus on
// the previously focused field for a beat after the tap lands. Typing
// then races the text into that stale field. InputText therefore waits
// for the resolved target to actually hold input focus before erasing
// and typing, and returns an *InputFocusError without dispatching any
// text when focus never reaches the target, so a missed focus surfaces
// as a clear failure instead of text landing in the wrong field.
func (o *Orchestra) InputText(ctx context.Context, selector Selector, text string, opts InputTextOptions) (ActionResult, error) {
	prepared, err := o.prepareSelectorForAction(ctx, selector, true)
	if err != nil {
		return ActionResult{}, err
	}
	if !prepared.ok {
		return prepared.result, nil
	}
	if err := o.driver.Tap(ctx, prepared.point); err != nil {
		return ActionResult{}, err
	}
	if err := o.awaitTargetFocus(ctx, selector, prepared.resolvedBy); err != nil {
		return ActionResult{}, err
	}
	clearFirst := opts.ClearFirst == nil || *opts.ClearFirst
	if clearFirst && o.driver.Capabilities().CanEraseText {
		if err := o.driver.EraseText(ctx, 999); err != nil {
			return ActionResult{}, err
		}
	}
	if err := o.driver.InputText(ctx, text); err != nil {
		return ActionResult{}, err
	}
	o.maybeKeyboardOpen = true
	return ActionOk(prepared.resolvedBy, fmt.Sprintf("typed %d char(s) into field at %s", utf16Len(text), pointStr(prepared.point))), nil
}

// InputFocusError reports that a type action gave up because the field
// it needed to type into never became ready. InputText returns it when
// the tapped target never took input focus; TypeText returns it when no
// field held focus and no on-screen keyboard appeared. The action
// returns this instead of dispatching text, because typing while the
// intended field lacks focus races the text into whichever field does
// hold it, corrupting input silently rather than failing visibly.
type InputFocusError struct {
	// Target names how the intended field was resolved (for example
	// "id"), empty for TypeText where the caller focused the field
	// before calling.
	Target    string
	TimeoutMs int
	cause     error
}

func (e *InputFocusError) Error() string {
	if e.Target != "" {
		return fmt.Sprintf(
			"target field (resolved by %s) never took input focus within %dms, so the text was not typed to avoid entering it into the wrong field",
			e.Target, e.TimeoutMs,
		)
	}
	return fmt.Sprintf(
		"no field was ready for input within %dms (nothing held focus and no keyboard was shown), so the text was not typed",
		e.TimeoutMs,
	)
}

func (e *InputFocusError) Unwrap() error { return e.cause }

// Code implements diag.Coded.
func (e *InputFocusError) Code() string { return diag.CodeToolTimeout }

// Remedy implements diag.Coded.
func (e *InputFocusError) Remedy() string {
	return "Confirm the field can take focus (tap it and check it holds focus), then retry; if it never reports focus, target it by a stable id or tap its coordinates directly."
}

var _ diag.Coded = (*InputFocusError)(nil)

// awaitTargetFocus blocks until the element matching selector holds
// input focus, so a following EraseText and InputText land in the field
// the caller aimed at rather than whichever field held focus before the
// focus-settling tap. WaitForFocus polls the hierarchy and resolves only
// when a node reporting focus coincides with selector's own candidate,
// so a transient focus still on the previously focused field does not
// satisfy it. It returns on the first poll when the target already holds
// focus, so a settled field pays no extra latency. A focus that never
// reaches the target surfaces as an *InputFocusError; the caller must
// not type after receiving one. An infrastructure failure (driver
// transport, context cancellation) propagates unchanged.
func (o *Orchestra) awaitTargetFocus(ctx context.Context, selector Selector, resolvedBy string) error {
	_, err := WaitForFocus(ctx, WaitForFocusOptions{
		Driver:     o.driver,
		Selector:   selector,
		Clock:      o.clock,
		TimeoutMs:  inputFocusSettleTimeoutMs,
		IntervalMs: inputFocusSettlePollMs,
	})
	var timeout *WaitTimeoutError
	if errors.As(err, &timeout) {
		return &InputFocusError{Target: resolvedBy, TimeoutMs: inputFocusSettleTimeoutMs, cause: err}
	}
	return err
}

// awaitInputReady blocks until some field is ready to receive text: a
// node reports input focus, or the on-screen keyboard is up. Unlike
// awaitTargetFocus it cannot check a specific target, because TypeText
// types into whatever field the caller already focused, so the empty
// selector matches any node and the wait confirms only that a
// focus-or-keyboard signal is present before dispatch. It returns on the
// first poll when a field is already ready, and surfaces a never-ready
// screen as an *InputFocusError instead of typing into a surface with no
// active input. An infrastructure failure propagates unchanged.
func (o *Orchestra) awaitInputReady(ctx context.Context) error {
	err := WaitForInputReady(ctx, WaitForInputReadyOptions{
		Driver:     o.driver,
		Selector:   Selector{},
		Clock:      o.clock,
		TimeoutMs:  inputFocusSettleTimeoutMs,
		IntervalMs: inputFocusSettlePollMs,
	})
	var timeout *WaitTimeoutError
	if errors.As(err, &timeout) {
		return &InputFocusError{TimeoutMs: inputFocusSettleTimeoutMs, cause: err}
	}
	return err
}

// ── Coordinate primitives (bypass selector pipeline) ─────────────

// TapAt dispatches a tap at point, bypassing selector resolution.
func (o *Orchestra) TapAt(ctx context.Context, point wire.Point) error {
	return o.driver.Tap(ctx, point)
}

// LongPressAt dispatches a long-press at point, bypassing selector
// resolution.
func (o *Orchestra) LongPressAt(ctx context.Context, point wire.Point, durationMs int) error {
	return o.driver.LongPress(ctx, point, durationMs)
}

// SwipeAt dispatches a linear drag from "from" to "to", bypassing
// selector resolution.
func (o *Orchestra) SwipeAt(ctx context.Context, from, to wire.Point, durationMs int) error {
	return o.driver.Swipe(ctx, from, to, durationMs)
}

// DispatchGesture dispatches an arbitrary W3C-Actions gesture. Thin
// passthrough to Driver.DispatchGesture. The caller is responsible
// for checking Capabilities().CanMultiPointer / CanPressure; the
// driver adapter also rejects mismatches defensively.
func (o *Orchestra) DispatchGesture(ctx context.Context, gesture driver.Gesture) (driver.GestureResult, error) {
	return o.driver.DispatchGesture(ctx, gesture)
}

// Capabilities returns the driver's capability flags.
func (o *Orchestra) Capabilities() driver.Capabilities {
	return o.driver.Capabilities()
}

// ResolvePoint resolves selector to the coordinate Tap's pipeline
// would tap, applying scroll-into-view, obscurement detection, and
// priority broadening, without dispatching a gesture. Returns an
// error on failure (element not found, obscured, scroll
// unreachable); a caller wanting to surface failure as an
// ActionResult should wrap the call.
func (o *Orchestra) ResolvePoint(ctx context.Context, selector Selector) (wire.Point, error) {
	prepared, err := o.prepareSelectorForAction(ctx, selector, true)
	if err != nil {
		return wire.Point{}, err
	}
	if !prepared.ok {
		return wire.Point{}, errors.New(resultReason(prepared.result))
	}
	return prepared.point, nil
}

// ResolveBounds resolves selector to its element's bounds, running
// the same pipeline ResolvePoint uses. Lets a caller compute a
// target other than the element's center without duplicating the
// resolution pipeline. Error behavior matches ResolvePoint.
func (o *Orchestra) ResolveBounds(ctx context.Context, selector Selector) (wire.Bounds, error) {
	prepared, err := o.prepareSelectorForAction(ctx, selector, true)
	if err != nil {
		return wire.Bounds{}, err
	}
	if !prepared.ok {
		return wire.Bounds{}, errors.New(resultReason(prepared.result))
	}
	return prepared.bounds, nil
}

// Direction is a screen-relative swipe direction for SwipeDirection.
type Direction string

const (
	Up    Direction = "up"
	Down  Direction = "down"
	Left  Direction = "left"
	Right Direction = "right"
)

// SwipeDirectionOptions configures SwipeDirection. A nil field falls
// back to this package's default: DurationMs defaults to
// DefaultSwipeDurationMs, Fraction to DefaultSwipeDirectionFraction.
type SwipeDirectionOptions struct {
	DurationMs *int
	Fraction   *float64
}

// SwipeDirection dispatches a directional swipe centered on the
// screen. Convenience over SwipeAt: resolves screen dimensions from
// the driver and computes endpoints. Swipe distance defaults to 60%
// of the screen in the swipe axis, matching ScrollController's
// default MaxScrollFraction.
func (o *Orchestra) SwipeDirection(ctx context.Context, direction Direction, opts SwipeDirectionOptions) error {
	screen, err := o.driver.ScreenSize(ctx)
	if err != nil {
		return err
	}
	fraction := DefaultSwipeDirectionFraction
	if opts.Fraction != nil {
		fraction = *opts.Fraction
	}
	durationMs := DefaultSwipeDurationMs
	if opts.DurationMs != nil {
		durationMs = *opts.DurationMs
	}

	from, to := ComputeSwipeDirectionEndpoints(screen, direction, fraction)
	return o.driver.Swipe(ctx, from, to, durationMs)
}

// ComputeSwipeDirectionEndpoints is the pure endpoint computation
// behind SwipeDirection: a directional swipe centered on the screen,
// sized to fraction of the screen in the swipe axis. Split out from
// SwipeDirection itself so the math is unit- and conformance-
// testable independent of a live driver.
func ComputeSwipeDirectionEndpoints(screen wire.Size, direction Direction, fraction float64) (from, to wire.Point) {
	cx := screen.Width / 2
	cy := screen.Height / 2
	var dx, dy float64
	switch direction {
	case Left:
		dx = -1
	case Right:
		dx = 1
	}
	switch direction {
	case Up:
		dy = -1
	case Down:
		dy = 1
	}
	amountX := (screen.Width * fraction) / 2
	amountY := (screen.Height * fraction) / 2
	from = wire.Point{X: cx - (dx*amountX)/2, Y: cy - (dy*amountY)/2}
	to = wire.Point{X: cx + (dx*amountX)/2, Y: cy + (dy*amountY)/2}
	return from, to
}

// ── Keyboard + input primitives ──────────────────────────────────

// PressKey presses key on the driver.
func (o *Orchestra) PressKey(ctx context.Context, key driver.KeyCode) (ActionResult, error) {
	result, err := o.driver.PressKey(ctx, key)
	if err != nil {
		return ActionResult{}, err
	}
	if result.Ok {
		return ActionOk("", result.Reason), nil
	}
	reason := result.Reason
	if reason == "" {
		reason = fmt.Sprintf("pressKey(%s) returned ok:false", key)
	}
	return ActionFail(reason, nil), nil
}

// TypeText types text into whatever element currently has focus,
// bypassing selector resolution. It waits for an input-ready signal (a
// focused field or a visible keyboard) before dispatching, so a type
// that arrives before the author's focus has settled does not race the
// text onto an inactive surface. It cannot verify a specific target,
// because the caller chose the field; a screen that never becomes ready
// surfaces as an *InputFocusError rather than a silent no-op.
func (o *Orchestra) TypeText(ctx context.Context, text string) error {
	if err := o.awaitInputReady(ctx); err != nil {
		return err
	}
	return o.driver.InputText(ctx, text)
}

// EraseText erases count characters backwards from the focused
// field. Returns an error when the driver does not support it; the
// caller then falls back to PressKey(KeyDelete) count times.
func (o *Orchestra) EraseText(ctx context.Context, count int) error {
	if !o.driver.Capabilities().CanEraseText {
		return errors.New("driver does not support eraseText, so use pressKey('delete') N times")
	}
	return o.driver.EraseText(ctx, count)
}

// ── App lifecycle ─────────────────────────────────────────────────

// LaunchApp launches appID. It first confirms appID is installed on
// the current device, because on at least one platform launching an
// app that is not installed blocks indefinitely instead of failing.
// The confirmation reuses the same installed-app list ListApps
// reports, so it carries no platform knowledge of its own.
//
// LaunchApp dedups by appID for the lifetime of a flow (a standalone
// script run, a scenario, or a runFlow tree): once appID has been
// launched, a later default (args.NoReset nil) or attach
// (args.NoReset true) call for the same appID is a successful no-op,
// because the flow already has that app running and a crash mid-flow
// must surface as the next step failing, not as a silent relaunch.
// Only an explicit fresh launch (args.NoReset false, which force-stops
// then relaunches) bypasses the dedup, since the caller is asking for
// a real relaunch regardless of flow history. See ResetLaunchedApps
// for how this tracking resets between a suite's independent entries.
func (o *Orchestra) LaunchApp(ctx context.Context, appID string, args driver.LaunchArgs) error {
	if err := validateAppID(appID); err != nil {
		return err
	}

	dedup := args.NoReset == nil || *args.NoReset
	if dedup && o.claimLaunch(appID) {
		return nil
	}

	apps, err := o.driver.ListApps(ctx)
	if err != nil {
		if dedup {
			o.releaseLaunchClaim(appID)
		}
		return err
	}
	installed := false
	for _, app := range apps {
		if app.BundleID == appID {
			installed = true
			break
		}
	}
	if !installed {
		if dedup {
			o.releaseLaunchClaim(appID)
		}
		return diag.New(
			diag.CodeAppNotInstalled,
			fmt.Sprintf("app %q is not installed on the target device", appID),
			"install the app on the target device before launching it",
			nil,
		)
	}
	if err := o.driver.LaunchApp(ctx, appID, args); err != nil {
		if dedup {
			o.releaseLaunchClaim(appID)
		}
		return err
	}
	o.maybeKeyboardOpen = false
	if !dedup {
		o.recordLaunch(appID)
	}
	return nil
}

// claimLaunch atomically checks whether appID is already recorded as
// launched in the current flow and, when it is not, records the claim
// immediately, before the driver call itself runs. Claiming before the
// driver call completes is what keeps two concurrent LaunchApp calls
// for the same appID from both falling through to the driver.
func (o *Orchestra) claimLaunch(appID string) (alreadyLaunched bool) {
	o.launchedAppsMu.Lock()
	defer o.launchedAppsMu.Unlock()
	if _, ok := o.launchedApps[appID]; ok {
		return true
	}
	o.launchedApps[appID] = struct{}{}
	return false
}

// releaseLaunchClaim undoes a claimLaunch reservation after the
// launch it was reserved for fails (not installed, or a driver
// error), so a later step in the same flow gets a real launch attempt
// instead of a silent no-op against an app that was never actually
// launched.
func (o *Orchestra) releaseLaunchClaim(appID string) {
	o.launchedAppsMu.Lock()
	defer o.launchedAppsMu.Unlock()
	delete(o.launchedApps, appID)
}

// recordLaunch marks appID launched in the current flow. Used by the
// fresh (force-stop-then-relaunch) path, which bypasses claimLaunch's
// dedup check on the way in but must still leave appID recorded, so a
// later default or attach launch of the same app in the same flow
// treats it as already running.
func (o *Orchestra) recordLaunch(appID string) {
	o.launchedAppsMu.Lock()
	defer o.launchedAppsMu.Unlock()
	o.launchedApps[appID] = struct{}{}
}

// IsLaunched reports whether appID is already recorded as launched in
// the current flow, the same tracking LaunchApp's dedup consults. A
// caller uses it to decide, ahead of time, whether an upcoming
// LaunchApp call for appID would be a guaranteed no-op.
func (o *Orchestra) IsLaunched(appID string) bool {
	o.launchedAppsMu.Lock()
	defer o.launchedAppsMu.Unlock()
	_, ok := o.launchedApps[appID]
	return ok
}

// ResetLaunchedApps clears the flow-scoped launched-app tracking
// LaunchApp uses to dedup. A suite runner calls this at the start of
// every independent entry, because each entry is its own test case
// and must launch its own app from a clean slate; a scenario or a
// runFlow tree never calls this, because each one is a single flow
// across its own scripts.
func (o *Orchestra) ResetLaunchedApps() {
	o.launchedAppsMu.Lock()
	defer o.launchedAppsMu.Unlock()
	o.launchedApps = make(map[string]struct{})
}

// StopApp stops appID.
func (o *Orchestra) StopApp(ctx context.Context, appID string) error {
	if err := validateAppID(appID); err != nil {
		return err
	}
	if err := o.driver.StopApp(ctx, appID); err != nil {
		return err
	}
	o.maybeKeyboardOpen = false
	return nil
}

// KillApp forcibly terminates appID.
func (o *Orchestra) KillApp(ctx context.Context, appID string) error {
	if err := validateAppID(appID); err != nil {
		return err
	}
	if err := o.driver.KillApp(ctx, appID); err != nil {
		return err
	}
	o.maybeKeyboardOpen = false
	return nil
}

// ListApps enumerates installed apps on the current device.
func (o *Orchestra) ListApps(ctx context.Context) ([]driver.InstalledApp, error) {
	return o.driver.ListApps(ctx)
}

// CurrentForeground reports the foreground app.
func (o *Orchestra) CurrentForeground(ctx context.Context) (driver.ForegroundInfo, error) {
	return o.driver.CurrentForeground(ctx)
}

// InstallApp installs the app artifact at source onto the current
// device.
func (o *Orchestra) InstallApp(ctx context.Context, source string) error {
	return o.driver.InstallApp(ctx, source)
}

// UninstallApp uninstalls appID from the current device.
func (o *Orchestra) UninstallApp(ctx context.Context, appID string) error {
	if err := validateAppID(appID); err != nil {
		return err
	}
	return o.driver.UninstallApp(ctx, appID)
}

// ClearAppData resets appID to a clean data state. See the platform
// notes on driver.Driver.ClearAppData.
func (o *Orchestra) ClearAppData(ctx context.Context, appID string, opts driver.ClearAppDataOptions) error {
	if err := validateAppID(appID); err != nil {
		return err
	}
	return o.driver.ClearAppData(ctx, appID, opts)
}

// IsAppInstalled reports whether appID is currently installed on the
// device.
func (o *Orchestra) IsAppInstalled(ctx context.Context, appID string) (bool, error) {
	if err := validateAppID(appID); err != nil {
		return false, err
	}
	return o.driver.IsAppInstalled(ctx, appID)
}

// DeviceInfo reports static information about the device.
func (o *Orchestra) DeviceInfo(ctx context.Context) (driver.DeviceInfo, error) {
	return o.driver.DeviceInfo(ctx)
}

// ── Media ─────────────────────────────────────────────────────────

// Screenshot captures a PNG-encoded screenshot of the current screen.
func (o *Orchestra) Screenshot(ctx context.Context) ([]byte, error) {
	return o.driver.Screenshot(ctx)
}

// ── Idle detection ───────────────────────────────────────────────

// WaitForIdle blocks until the UI reaches an idle state or the
// deadline expires. Uses the driver's native WaitForIdle when
// Capabilities().CanWaitForIdle is true; otherwise falls back to a
// host-side tree-diff polling loop driven by the Clock.
func (o *Orchestra) WaitForIdle(ctx context.Context, timeoutMs int) (bool, error) {
	if o.driver.Capabilities().CanWaitForIdle {
		return o.driver.WaitForIdle(ctx, timeoutMs)
	}
	return o.waitForIdleHostSide(ctx, timeoutMs)
}

func (o *Orchestra) waitForIdleHostSide(ctx context.Context, timeoutMs int) (bool, error) {
	const pollMs = 200
	deadline := o.clock.NowMs() + int64(timeoutMs)
	previous, err := o.driver.Hierarchy(ctx)
	if err != nil {
		return false, err
	}
	previousKey, err := treeSnapshotKey(previous)
	if err != nil {
		return false, err
	}
	for o.clock.NowMs() < deadline {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := o.clock.Sleep(ctx, pollMs); err != nil {
			return false, err
		}
		current, err := o.driver.Hierarchy(ctx)
		if err != nil {
			return false, err
		}
		currentKey, err := treeSnapshotKey(current)
		if err != nil {
			return false, err
		}
		if currentKey == previousKey {
			return true, nil
		}
		previousKey = currentKey
	}
	return false, nil
}

// HideKeyboard dismisses the on-screen keyboard, if any. Clears the
// maybeKeyboardOpen hint regardless of whether the driver reported a
// keyboard, matching the invariant that after HideKeyboard completes,
// subsequent taps don't need to worry about keyboard obscurement.
func (o *Orchestra) HideKeyboard(ctx context.Context) (ActionResult, error) {
	if !o.driver.Capabilities().CanHideKeyboard {
		return ActionFail("driver does not support hideKeyboard", nil), nil
	}
	result, err := o.driver.HideKeyboard(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	o.maybeKeyboardOpen = false
	reason := result.Reason
	if reason == "" {
		if result.Ok {
			reason = "dispatched"
		} else {
			reason = "no-op"
		}
	}
	return ActionOk("", fmt.Sprintf("hideKeyboard: %s", reason)), nil
}

// ── Internal: selector preparation pipeline ──────────────────────

// preparedAction is the internal result of prepareSelectorForAction:
// either a resolved coordinate and metadata (ok true) or a ready-to-
// return ActionResult (ok false).
type preparedAction struct {
	ok         bool
	point      wire.Point
	bounds     wire.Bounds
	cursor     *TreeCursor
	resolvedBy string
	result     ActionResult
}

// prepareSelectorForAction is the core pipeline every selector-based
// action shares: compile the selector, optionally scroll it into
// view, check obscurement against the same tree the resolved cursor
// points into, and compute the tap-center coordinate from its
// bounds.
func (o *Orchestra) prepareSelectorForAction(ctx context.Context, selector Selector, scrollToFind bool) (preparedAction, error) {
	filter := CompileSelector(selector)

	var cursor *TreeCursor
	if scrollToFind {
		c, err := o.scroll.EnsureVisible(ctx, filter)
		if err != nil {
			var unreachable *ScrollUnreachableError
			if errors.As(err, &unreachable) {
				return preparedAction{result: ActionFail(
					fmt.Sprintf("could not scroll element into view: %s", unreachable.Message), nil,
				)}, nil
			}
			return preparedAction{}, err
		}
		cursor = c
	} else {
		matches, err := o.finder.Find(ctx, filter)
		if err != nil {
			return preparedAction{}, err
		}
		if len(matches) == 0 {
			return preparedAction{result: ActionFail(
				"element not found (scroll disabled via scrollToFind=false)", nil,
			)}, nil
		}
		cursor = matches[0]
	}

	root := RootOf(cursor)
	ob := DetectObscurement(root, cursor)
	if ob.Obscured {
		reason := fmt.Sprintf(
			"element is visually obscured by [role=%s id=\"%s\" label=\"%s\"]. "+
				"Dismiss the obscuring element, or call tap on the obscurer directly.",
			ob.Obscurer.Role, ob.Obscurer.ID, ob.Obscurer.Label,
		)
		return preparedAction{result: ActionFail(reason, ob.Obscurer)}, nil
	}

	bounds, ok := ParseBounds(cursor.Node.Attributes[AttrBounds])
	if !ok {
		return preparedAction{result: ActionFail(
			"element resolved but has no bounds attribute; cannot compute tap coordinate", nil,
		)}, nil
	}
	point := BoundsCenter(bounds)
	resolvedBy := guessResolvedBy(selector, cursor.Node)

	return preparedAction{ok: true, point: point, bounds: bounds, cursor: cursor, resolvedBy: resolvedBy}, nil
}

// guessResolvedBy reverse-engineers which selector priority field
// actually resolved the match: priority broadening doesn't report
// this directly, so this matches the cursor's attributes against the
// selector's provided fields in the same order CompileSelector uses.
// Not perfect (a selector matching both id and text reports "id"
// even if broadening internally tried text), but matches the
// broadening policy.
func guessResolvedBy(selector Selector, node wire.TreeNodeWire) string {
	if selector.ID != nil {
		if v, ok := node.Attributes[AttrID]; ok && selector.ID.Matches(v) {
			return "id"
		}
	}
	if selector.Label != nil {
		if v, ok := node.Attributes[AttrLabel]; ok && selector.Label.Matches(v) {
			return "label"
		}
	}
	if selector.Text != nil {
		if v, ok := node.Attributes[AttrText]; ok && selector.Text.Matches(v) {
			return "text"
		}
	}
	if selector.Value != nil {
		if v, ok := node.Attributes[AttrValue]; ok && selector.Value.Matches(v) {
			return "value"
		}
	}
	if selector.Hint != nil {
		if v, ok := node.Attributes[AttrHint]; ok && selector.Hint.Matches(v) {
			return "hint"
		}
	}
	return ""
}

func resultReason(r ActionResult) string {
	if r.Reason == "" {
		return "selector resolution failed without a reason"
	}
	return r.Reason
}

func pointStr(p wire.Point) string {
	return fmt.Sprintf("(%d,%d)", int(math.Round(p.X)), int(math.Round(p.Y)))
}

// utf16Len counts s's length the way JavaScript's String.prototype.length
// does: in UTF-16 code units, not Unicode code points or UTF-8 bytes.
func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// treeSnapshotKey produces a full-fidelity comparison key for
// WaitForIdle's tree-diff polling: two snapshots of an unchanged
// hierarchy produce the same key, and any attribute, structural, or
// state-boolean change produces a different one. Mirrors the
// TypeScript implementation's JSON.stringify(tree) equality check;
// the exact byte encoding differs (encoding/json sorts map keys,
// JSON.stringify preserves insertion order) but that has no
// behavioral effect, since nothing compares this key across the
// language boundary. Only two Go-side snapshots are ever compared
// against each other.
func treeSnapshotKey(tree wire.TreeNodeWire) (string, error) {
	b, err := json.Marshal(tree)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
