# Atomyx: Known pitfalls

Traps the team has already hit once. Read before touching the
corresponding area. For the high-level map see
[`repo-map.md`](./repo-map.md).

## Android control plane

- **Never cache `AccessibilityNodeInfo` references across tool calls.**
  They go stale. `UiTreeService`
  (`platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/UiTreeService.kt`)
  is a metadata-only registry by design: it captures `bounds: Rect` and
  stable selectors at dump time, uses the cached bounds for gestures,
  and re-resolves a fresh node from the live tree for any action that
  needs one.
- **Never replace `serviceInfo` wholesale in `onServiceConnected`.**
  Augment it instead. Replacing it with a fresh
  `AccessibilityServiceInfo()` clobbers the capability and flag values
  the manifest's accessibility config declared and causes
  `service.windows` to return empty until the service is re-bound. See
  `platforms/android-agent/app/src/main/java/dev/atomyx/agent/service/AtomyxAccessibilityService.kt`.
- **The tap pipeline auto-dismisses the keyboard when it obscures the
  target.** Orchestra's `Tap(selector)` pipeline
  (`core/internal/orchestra/orchestra.go`) checks the session flag
  `maybeKeyboardOpen` together with the obscurement result's role.
  When both indicate a keyboard is blocking the target, it dismisses
  the keyboard before retrying instead of dispatching a tap that would
  land on the IME.
- **Never call `typeViaKeyboard` without accounting for IME layout
  switches.** When focus moves from a numeric field to a text field,
  the keyboard re-renders. `KeyboardTyper.typeViaKeyboard`
  (`platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/KeyboardTyper.kt`)
  polls internally before per-key dispatch and falls through to an
  on-screen-key scan for custom in-app keypads (banking apps, OTP
  entry widgets, most common on Flutter).
- **`UiTreeService.dumpCompact` keeps any element with a stable
  signal** (clickable flag, text, contentDesc, or resourceId), not
  only clickable-or-labeled elements. Flutter elements frequently
  carry only a resourceId. `dumpCompact` is an internal helper
  consumed by the keyboard and clear-text fallbacks
  (`platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/InputDispatcher.kt`,
  `platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/KeyboardTyper.kt`,
  and the strategies under
  `platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/clear/`),
  not a wire route, so tightening its filter breaks those fallbacks
  silently rather than an API contract.
- **`clickable` is a ranking signal, never a hard filter.** Flutter,
  Compose, and React Native dispatch gestures in-engine via a gesture
  detector without setting the accessibility clickable bit, so a
  selector match must not require it. `CompileSelector` in
  `core/internal/orchestra/priority_broadening.go` only applies
  `IsClickable()` when the caller explicitly asks for
  `Selector.Clickable == true`. Otherwise `rankCandidates` uses
  clickable (plus focused) only to break ties among multiple matches.
- **Never run gesture commands on the accessibility-service main
  thread.** `AccessibilityService.dispatchGesture(g, callback,
  handler=null)` delivers its `GestureResultCallback` on the service's
  main thread. Calling `GestureRunner.dispatch`
  (`platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/GestureRunner.kt`)
  from main would block main waiting on a latch that only the main
  thread can fire, indistinguishable from a 30s hang. NanoHTTPD's
  worker threads are the supported entry point. A new code path that
  dispatches gestures from a non-route context must route through a
  background executor first.
- **Two-tier capability gate for `DispatchGesture`.** The Go adapter
  (`core/internal/androiddriver/driver.go`) rejects a multi-pointer or
  pressure-carrying gesture before it ever leaves the host, and the
  agent's `GestureRunner.validatePointers` re-validates everything on
  arrival. Either tier alone is insufficient, because a bypassed host
  (direct HTTP poke) or a bypassed agent (older APK build) would leak
  an unsafe gesture. Removing a check from one side is a regression
  even when the other still catches it.
- **`GestureDescription.StrokeDescription` requires `duration > 0`.**
  `GestureRunner.validatePointers` enforces monotonic-non-decreasing
  waypoint offsets at parse time, and a zero-duration tap survives
  because `MIN_PRESS_MS` (50ms) floors every issued stroke. A new
  stroke-building path must apply the same floor, or the platform
  rejects the gesture with an uncaught `IllegalArgumentException`
  rather than a structured rejection.
- **`canPressure` is permanently `false` on Android.** The
  accessibility gesture surface has no per-touch pressure API on any
  current Android level. Do not add a "maybe supported on device X"
  flip: the entire backend is pressure-less, and the host validator
  relies on this being a hard no. The single place to flip if the
  platform ever ships a pressure-carrying `StrokeDescription`
  constructor is `GestureRunner.GestureCapabilities.DEFAULT`.
- **Long-press-then-drag must flow through the multi-phase
  dispatcher, not a single `GestureDescription`.** A zero-length
  segment inside one `GestureDescription` still emits periodic
  same-coordinate `ACTION_MOVE` events (the platform interpolates at
  roughly 10ms inside every stroke's active window regardless of path
  length). A recognizer that rejects on the first move event, such as
  Flutter's `DelayedMultiDragGestureRecognizer` backing
  `ReorderableListView`, refuses that shape. `dispatchMultiPhase`
  instead issues a minimal `MIN_PRESS_MS` stroke for a hold segment,
  then sleeps the authored remainder at the Kotlin level between
  dispatch calls, producing no platform events during the hold.
- **Pointer coordinates are physical pixels, while Flutter lays out
  in logical pixels.** A Flutter app's own coordinate space is scaled
  by its `devicePixelRatio`. A script or tool computing a tap target
  from Flutter layout data must multiply by that ratio before
  dispatching through `Tap` or `DispatchGesture`.
- **A hold must exceed roughly 500ms to trigger a Flutter
  long-press.** Flutter's `LongPressGestureRecognizer` uses its own
  timer independent of the platform's, so an authored hold shorter
  than that risks resolving as a tap instead.
- **`adb forward` can drop between calls.** A USB disconnect, `adb
  kill-server`, or a device reboot drops every forward.
  `AgentLauncher.EnsureRunning`
  (`core/internal/androiddriver/agent_launcher.go`) re-issues the
  forward unconditionally rather than assuming a previously connected
  session is still reachable, and a caller building a new code path
  against the agent should do the same.
- **A host-side forward is a shared resource, not a per-driver one.**
  The local port `AndroidDriver.Connect`
  (`core/internal/androiddriver/driver.go`) forwards to the device is
  a real listening socket on the workstation. Never hard-code a host
  port for a new Android driver: set `Options.AllocateHostPort` so adb
  picks a free one per device, and supply `Options.NewTransport` so the
  client is built after the port is known. `Connect` reuses a forward
  that already maps this serial to the device port, and
  `Disconnect` removes only a forward this `Connect` created. With a
  fixed `HostPort`, a port already forwarded to a different device
  fails with `HOST_PORT_IN_USE`, not `DEVICE_BUSY`, which is reserved
  for a foreign automation session.

## Device lease

- **Acquire the lease before any agent connect.** Resolve the device's
  canonical identity first (`device.IdentityResolver.Resolve`), take the
  lease, and only then forward ports or launch a runner. Connecting
  first disturbs a live holder's forward or runner.
- **Key on the hardware identity, never on the transport string.** The
  Wi-Fi serial `<ip>:<port>` and the USB serial of the same phone must
  map to one lease, which is why identity reads `ro.serialno`.
- **Never unlink a lock file.** It is created once and reused. Replacing
  it is exactly what the descriptor-versus-path comparison detects, so
  removing and recreating it defeats that check. Only a socket may be
  unlinked, and only by the process holding its flock.
- **Never open a lease file with a raw `syscall.Open` or `unix.Open`.**
  Use `os.OpenFile` so close-on-exec applies, because a leaked lease
  descriptor in a spawned child keeps the device held after the owner
  exits. `core/scripts/check-no-raw-syscall-open.sh` fails the build on
  a raw open outside `core/internal/lease`.
- **Never conflate `DEVICE_HELD` with `DEVICE_BUSY`.** `DEVICE_HELD`
  means another Atomyx process holds the lease. `DEVICE_BUSY` is
  reserved for a foreign, non-Atomyx automation session found driving
  the device. The remedies differ, so a handler that returns the wrong
  one points the operator at the wrong fix.
- **A probe never writes.** `atomyx devices` and `doctor --bundle` use
  `Manager.Probe`, which takes and releases the real flock but never
  creates the lock file or writes holder metadata. Only the process that
  keeps the flock for its session owns that write.
- **Lock file contents are display only.** Liveness comes from the
  flock, never from the pid or command in the file, because a dead
  holder's metadata can linger until the next acquirer overwrites it.

## Tool layer (cross-platform)

- **Never branch on the session's platform inside a tool handler.**
  Files under `core/internal/mcp` read `device.Platform` off the
  active session for bookkeeping
  (`core/internal/mcp/device_session.go`), but no tool handler
  switches on it to change behavior. If behavior must differ
  per platform, it belongs in the driver adapter
  (`core/internal/androiddriver` or `core/internal/iosdriver`), not in
  the tool layer.
- **Never assume a device-side HTTP server exists.** That is
  Android-specific, because iOS talks TCP to an XCUITest process.
  Design a new
  tool or strategy against the `driver.Driver` port
  (`core/internal/driver/driver.go`), never against a concrete
  transport.
- **Never add a tool that duplicates an existing tool's intent.** The
  surface is deliberately small on purpose, because overlapping tools
  cause measurable agent confusion (see `CLAUDE.md`'s non-negotiable
  rules). Before adding a new tool under `core/internal/mcp`, confirm
  no existing tool (`tap`, `input_text`, `find_element`, and so on)
  can absorb the intent through a new parameter instead.
- **Do not inline business logic in a tool handler.** Every tool under
  `core/internal/mcp` decodes its arguments, then delegates to an
  `orchestra.Orchestra` method
  (`core/internal/orchestra/orchestra.go`). New behavior belongs on
  `Orchestra`, `Finder`, or `ScrollController`
  (`core/internal/orchestra/scroll_controller.go`), called from the
  tool handler, not pasted into the handler itself. Selector
  compilation lives in `core/internal/orchestra/priority_broadening.go`
  and the raw filter primitives in `core/internal/orchestra/filter.go`.

## Observation-driven input and waits

- **Never introduce hardcoded sleeps in Orchestra action pipelines.**
  Fixed sleeps are fragile across Flutter, React Native, and iOS.
  Driver adapters and the wait helpers in
  `core/internal/orchestra/wait.go` self-synchronize on focus and
  keyboard state, so Orchestra should not add its own waits without a
  concrete observable justifying them.
- **`InputText` taps the selector, then waits for focus, before typing
  or erasing.** `Orchestra.InputText`
  (`core/internal/orchestra/orchestra.go`) dispatches a focus-settling
  tap, then calls `WaitForFocus` so a following erase and type land in
  the field the caller aimed at rather than whichever field held focus
  before the tap. A focus that never reaches the target returns an
  `*InputFocusError` instead of typing into the wrong field. A new
  input-shaped action should follow the same tap-then-wait sequence.
- **The `maybeKeyboardOpen` session flag controls the tap
  keyboard-dismiss gate.** It is set after every `InputText`
  (`core/internal/orchestra/orchestra.go`). A new Orchestra action that
  implicitly opens the keyboard must set the flag too, or a subsequent
  tap will not dismiss it correctly on obscurement. The flag is
  cleared on `LaunchApp`, `StopApp`, and `HideKeyboard` to avoid stale
  state.
- **Selector ranking respects `Nth` as an opt-out.** Without `Nth`,
  `CompileSelector`
  (`core/internal/orchestra/priority_broadening.go`) ranks candidates
  by a clickable-plus-focused score. With `Nth` set, ranking is
  skipped and the Nth match in document order is returned instead,
  because `Nth` is positional by definition. A script that records a
  step where positional selection matters must keep the `Nth` in
  place.
- **Waits live in `core/internal/orchestra/wait.go`, not inline in
  Orchestra.** `WaitForFocus`, `WaitForInputReady`, and
  `WaitForInputCommitted` take a `Clock` for deterministic tests. Add
  a new wait here, following the same signature pattern, rather than
  inline inside an Orchestra method.
- **The script `handle` command polls, so do not add a `sleep` before
  it.** `HandleCommand`
  (`core/internal/runner/handle_command.go`) polls the current screen
  state at a 100ms interval up to its timeout (5s default) waiting for
  a branch's `when` condition to hold. A script that pads a fixed
  sleep before `handle` just wastes wall-clock time, so remove it.

## iOS clear-input

- **`ClearFocusedInputCommand` runs a 4-strategy chain, in order: a
  select-all-plus-delete keyboard shortcut, a triple-tap select, a
  long-press system edit menu, then an exact-length delete loop.** See
  `platforms/ios-agent/Tests/Commands/ClearFocusedInputCommand.swift`
  and the strategies under
  `platforms/ios-agent/Tests/Commands/Clear/Strategies/`. The chain
  polls the focused field after each attempt to confirm it is actually
  empty before advancing. The keyboard-shortcut fast path requires
  simulator hardware-keyboard pairing (`xcrun simctl keyboard <udid>
  enable hardware`, or Simulator, then I/O, then Keyboard). Without
  it, the shortcut types a literal character instead of selecting, and
  the chain falls through to the remaining strategies.

## iOS control plane

Order roughly follows severity, most likely to bite a new contributor
first.

### Process and state

- **iOS has no system-wide foreground query.** Unlike Android's
  `AccessibilityService.rootInActiveWindow`, XCUITest can only drive
  an app whose `XCUIApplication` reference the driver already holds.
  The Swift side tracks this in `DriverState.currentApp` /
  `currentBundleId`
  (`platforms/ios-agent/Tests/Commands/CommandHandler.swift`), and the
  Go side mirrors it in `IosDriver.lastLaunchedBundleID`
  (`core/internal/iosdriver/driver.go`). Never build a flow that
  assumes `CurrentForeground` reflects what a user manually opened. If
  the agent never called `LaunchApp`, the tracked state is empty and
  `CurrentForeground` returns a typed unsupported error rather than a
  guess.
- **`LaunchApp` resets app state.** `XCUIApplication.launch()` on an
  already-running app terminates and relaunches it, so any manually
  focused field, open sheet, or mid-flow state is lost. A caller that
  wants to attach to the running instance instead of resetting it
  passes `LaunchArgs.NoReset = true`.
- **Stale driver state after a simulator crash.** If the XCUITest
  process dies mid-session (simulator reboot, Xcode toolchain crash),
  the host adapter's tracked bundle id becomes stale. `Hierarchy`
  recovers on its next call by probing candidate bundle ids, but this
  is recovery, not prevention. A long-running session should poll
  `Ping` periodically to catch the failure earlier.

### Queries and timing

- **Never use per-element property access for bulk tree operations.**
  Each property read (`identifier`, `label`, `frame`) is a separate RPC
  to the XCUITest daemon, so walking a tree that way costs seconds on
  a few hundred elements. `DumpRawTreeCommand` and `XCUIBridge.getKeyboard`
  (`platforms/ios-agent/Tests/Bridge/XCUIBridge.swift`) instead call
  `app.snapshot()` once and walk the resulting `XCUIElementSnapshot`
  tree locally, one RPC with local, constant-time property access
  afterward.
- **`XCUIElementSnapshot` does NOT expose `isHittable`.** Hittability
  is a live screen-state query, not part of a snapshot. Code that
  needs a `clickable` signal must use a different heuristic. The
  adopted one is the elementType and accessibility-trait whitelist in
  `NormalizeIosTree` (`core/internal/iosdriver/normalize.go`).
- **`XCUIElementQuery.waitForExistence(timeout:)` can false-negative
  even on a short timeout**, returning false against a Flutter host
  where the element in question is actually present in the same
  tick's `app.snapshot()`. Prefer a snapshot walk over element-query
  polling for anything requiring reliable detection.
- **Unbounded recursion on deeply nested trees.** Flutter and React
  Native apps routinely nest 30 or more wrapper layers. A tree walker
  must be iterative (an explicit stack), not recursive, to avoid a
  Swift call-stack overflow on a pathological target. See
  `XCUIBridge.buildRawTree`'s two-pass, stack-based walk for the
  pattern.

### Gestures and tapping

- **iOS "back" is NOT a system primitive.** Android dispatches a BACK
  intent regardless of app handling, but iOS only has per-screen
  affordances. `BackKeyStrategy`
  (`platforms/ios-agent/Tests/PressKey/BackKeyStrategy.swift`) tries
  the nav bar back button first (verifiable, reports
  `affordanceFound: true`) then an edge-swipe fallback (unverifiable,
  reports `affordanceFound: false`). A caller must check that field
  and fall back to a screen-specific Cancel, Done, or Close affordance
  when it is false, never treat `PressKey(back)` as guaranteed the way
  Android allows.
- **Edge-swipe back needs precise gesture parameters.** A normalized
  offset `dx = 0` (not a small positive value) and a zero-duration
  press with a fast drag and no final hold are what the pop gesture
  recognizer accepts. A slower press or a nonzero final hold reads as
  a context-menu trigger instead of a navigation gesture. See
  `BackKeyStrategy`'s edge-swipe branch.
- **Coordinate-based tap is more reliable than `XCUIElement.tap()`
  for cross-platform targets.** The native `tap()` path performs
  extra preflight checks (wait for hittable, wait for stable) that
  fail silently against a Flutter or React Native element whose
  semantic node lacks proper hit-test metadata. `TapAtCommand`
  dispatches through `EventSynthesizer` at a coordinate instead, the
  same path selector-based taps compose through host-side.

### `XCUIElement.ElementType` naming

- **An unmapped `elementType` renders as `typeNN`, unreadable for
  agent role filtering.** `elementTypeName`
  (`platforms/ios-agent/Tests/Bridge/ElementTypeName.swift`) covers
  the documented `XCUIElement.ElementType` cases explicitly and falls
  through to `type\(rawValue)` via `@unknown default` for forward
  compatibility. When bumping the supported Xcode range, grep a smoke
  run's tree dump for `"type\d+"` to catch any element type Apple
  added that the mapping doesn't yet cover.

### Flutter and cross-platform assumptions

- **Flutter apps do NOT universally use a custom keypad.** Some apps
  use the iOS system keyboard, and native `app.typeText()` works
  directly. A custom-keypad fallback must check `getKeyboard` first
  and branch on visibility rather than assuming every Flutter app
  needs the fallback path.
- **Interactive-type empty wrappers are noise.** Some `button` or
  `cell` elements in a Flutter tree carry an empty identifier, label,
  and value. The Swift-side stable-signal filter keeps them because
  the elementType is in the interactive whitelist, but they carry
  nothing an agent can act on. A consumer that filters for actionable
  elements should drop these rather than surface them.

### Architecture constraints

- **Never try to run an HTTP server inside an iOS app.** The sandbox
  suspends the process within seconds of backgrounding. All transport
  is TCP from the XCUITest process (host-side).
- **Never change the iOS bridge strategy without explicit direction.**
  The Swift XCUITest runner is the committed approach. An alternative
  bridge is a large rewrite of the iOS transport layer and stays out
  of scope until the user reopens it.
- **Design any new capability against `driver.Driver`
  (`core/internal/driver/driver.go`), never with iOS-specific methods
  on the port.** The snapshot-based query strategy, the coordinate-only
  gesture surface, and the iterative tree walk all sit inside the
  adapter, never on the shared interface.
- **A simulator-backed driver and a device-backed driver cannot both
  serve port 22087 at once.** A simulator shares the host's network
  namespace and binds `127.0.0.1:22087` directly. A physical device
  needs `iproxy` to tunnel the same port over USB, and `iproxy` cannot
  bind the host side while the simulator already holds it.
  `launchIproxy` (`core/internal/iosdriver/iproxy.go`) probes the host
  port with a ping handshake before spawning `iproxy`, reusing an
  existing Atomyx tunnel it finds rather than refusing outright.
  Switching between simulator and device targets still requires
  stopping one driver before starting the other.
- **The XCUITest process and, for a physical device, the `iproxy`
  tunnel are two separate pieces of state, and exactly one owner starts
  each.** With a provisioner in `DriverFactoryDeps`,
  `DriverProvisioner.EnsureIOSAgent`
  (`core/internal/device/provisioner.go`) starts the runner on a port
  reserved for that target and, for a device, the tunnel as well, so the
  driver it feeds is built with `Options.ExternalTunnel` and starts no
  tunnel of its own. Without a provisioner (the sidecar), the runner is
  someone else's and `IosDriver.Connect`
  (`core/internal/iosdriver/iproxy.go`) starts the tunnel for a device
  target. Never start a tunnel from both sides.
- **Never share a derived data path between targets, and never build
  outside the build lock.** Every launch builds once into
  `platforms/ios-agent/build` under an exclusive lock on
  `build/.build.lock`, and takes the decision to build only after the
  lock is held. Each target then runs `test-without-building -xctestrun`
  from its own `build/run-<udid>` derived data path. A second build or a
  second runner writing into the same path corrupts the other's session.
- **Attach the runner's `kill` to the driver's teardown, never to a
  caller's own cleanup.** The provisioned driver stops its runner in
  `Disconnect` through `Options.OnDisconnect`, so every path that
  disconnects it also stops the runner. A new caller that builds a driver
  and forgets `Disconnect` leaks an `xcodebuild` process group.
- **Long-lived child processes go through `BackgroundRunner`, never
  `Runner.Run`.** `iproxy` and the XCUITest runner
  (`xcodebuild test-without-building`) are started with
  `process.BackgroundRunner` (`core/internal/process/background.go`),
  because `Runner.Run` applies a 10 minute fallback timeout to any
  deadline-less context and would kill them mid-session. The driver and
  the launcher drain the child's stdout, stop it by killing its process
  group through the `BackgroundProcess` handle, and receive the runner
  by constructor injection from the composition roots.
- **`make serve-device` uses `test-without-building`, and its build
  stamp depends on the Swift sources and the project file.** After a
  change under `platforms/ios-agent/Tests/` or `App/`, the next
  `make serve` or `make serve-device` rebuilds before it serves. Calling
  `xcodebuild test-without-building` by hand skips that check and runs
  the stale binary from the previous build.
- **A Derived-Data or module-cache path breaks after the driver
  directory is renamed or moved.** Xcode's precompiled headers embed
  an absolute path to the module cache location. If
  `platforms/ios-agent` (or its build output) moves, the next build
  fails with a PCH-path mismatch error referencing `SwiftShims`. Fix:
  remove `platforms/ios-agent/build` entirely and re-run `make setup`,
  since the PCH embeds the old path in binary form and `xcodebuild`
  reuses it rather than detecting the mismatch on its own.
- **Private XCTest symbol drift falls back silently to the coordinate
  backend.** `EventRecordSynthesizer`
  (`platforms/ios-agent/Tests/Bridge/EventRecordSynthesizer.swift`)
  looks up `XCSynthesizedEventRecord`, `XCPointerEventPath`, and
  `XCTRunnerDaemonSession` via `NSClassFromString`. When Apple changes
  a selector's behavior between Xcode majors, the dry-run probe at
  load time catches it and `EventSynthesizerFactory` falls back to
  `CoordinateSynthesizer` instead. A multi-pointer script then starts
  failing right after an Xcode upgrade, which reads as "my script
  broke" until someone checks `ping`'s `probeLog` field, which names
  exactly which class or selector drifted. When adding new
  private-symbol access, keep it inside
  `platforms/ios-agent/Tests/Bridge/EventRecordSynthesizer.swift` (the
  one file allowed to touch those symbols), probe for it in the dry
  run, and document the expected signature next to the call so a
  version bump can diff against the current expectation. A grep for
  `NSClassFromString("XCSynth`, `NSClassFromString("XCPointer`, or
  `NSClassFromString("XCTRunner` across `platforms/ios-agent/Tests`
  must return matches only inside that one file. Any other match is an
  isolation violation.
- **The private synthesize completion block must be built in
  Objective-C, never in Swift.** A Swift `@convention(block) (NSError?)
  -> Void` (or any `AnyObject?`-typed variant) auto-retains its
  argument at block entry, and the XCTest daemon's XPC reply has been
  observed to invoke the block with a sentinel pointer instead of a
  real `NSError*` or nil, crashing on that auto-retain. The block must
  declare its parameter as `__unsafe_unretained id _Nullable` and must
  not close over any Swift value, because XPC's `_Block_copy` does not
  survive a Swift-captured closure. See
  `platforms/ios-agent/Tests/Bridge/AtomyxBlockHelper.h` and `.m`
  (wired through `SWIFT_OBJC_BRIDGING_HEADER` in
  `platforms/ios-agent/project.yml`) for the reference implementation,
  and dispatch the call from a background
  queue, because `synthesizeEvent:completion:` posts its completion
  block to main and a caller waiting on main would deadlock.
- **Gesture arbitration between nested scrollables is unreliable with
  a synthesized swipe.** A scrollable inside another scrollable can
  route an XCUITest-synthesized swipe intermittently to the outer
  scrollable even when the swipe coordinates land inside the inner
  one. A single MCP `swipe` call is reliable, while rapid consecutive
  `dispatchPointer` calls are not. Prefer a flat scrollable hierarchy
  in a test fixture where possible, or route through the `swipe` tool
  rather than a sequence of low-level dispatches.
- **Multi-pointer dispatch can fail mid-inertia.** The private
  synthesizer rejects a multi-pointer dispatch that arrives while a
  page-scroll inertia animation is still running: pinch, rotate, and
  two-finger gestures silently fail, with no error, but the app's own
  scale-gesture callback never fires. A caller should settle-wait at
  least a second between a page scroll and the next multi-pointer
  dispatch. A single-pointer dispatch does not need this wait.
- **Every synthesizer-using command must set `requiresMainThread =
  false`.** `EventRecordSynthesizer` guards against main-thread
  dispatch, because its completion block lands on main and waiting for
  it from main would deadlock.
  `CommandHandler.requiresMainThread` defaults to `true`, so a new
  gesture command that calls the synthesizer must explicitly opt out,
  matching `TapAtCommand`, `LongPressAtCommand`, `SwipeCommand`, and
  `DispatchPointerCommand`. Forgetting this returns a hard error from
  the synthesizer on the very first dispatch rather than hanging, but
  it is the kind of bug a contributor trips over silently when adding
  a new gesture command.

### Agent ergonomics for selector actions

- **Obscurement detection must treat an ancestor container as a
  non-obscurer.** A pre-order walk for "the topmost element containing
  this point" returns the deepest rendered element at that point,
  which on iOS is frequently the target's own ancestor (a collection
  view wrapping the cell wrapping the label). `DetectObscurement`
  (`core/internal/orchestra/obscurement.go`) checks whether the
  candidate obscurer's tree path is a prefix of the target's own path
  before flagging it, and separately suppresses a `container` or
  `other` role with no id and no label, because that combination is
  almost always an unstyled layout wrapper rather than a real
  obscurer.
- **Safe-area insets matter for "in viewport" checks.** A screen's
  reported bounds include the status bar, notch, and home indicator.
  An element whose midpoint sits in those edge zones is technically
  inside the rect but not reliably tappable. `ScrollController`
  (`core/internal/orchestra/scroll_controller.go`) uses conservative
  insets (60pt top, 50pt bottom by default) before deciding an element
  is visible, so the scroll loop keeps running until the element sits
  comfortably inside the interactive area.
- **A selector-based tap on a virtualized list needs a scroll-search
  phase, not only a positional scroll-into-view loop.** A list that
  recycles off-screen cells can report a logically present cell as
  absent from the current tree entirely. `ScrollController.scrollSearch`
  swipes the screen's center column in each direction for a small,
  fixed budget, probing the selector between each swipe, rather than
  forcing the caller to scroll manually. The budget stays small
  because this is a fallback, not the primary workflow, and unbounded
  scrolling is worse than a clear "could not find element" error.
- **Search bars ARE editable text inputs.** An editable-element
  whitelist that matches on class name must include `searchField`
  alongside `textField` and `secureTextField`, because iOS serializes
  a search field's class as `searchField`, which does not contain
  `textfield` as a substring. `interactiveIosRoles`
  (`core/internal/iosdriver/normalize.go`) includes it, and a new
  cross-platform "is this an input field" heuristic should too.
