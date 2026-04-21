# Atomyx — Known pitfalls

Traps the team has already hit once. Read before touching the corresponding
area. For the high-level map see [`repo-map.md`](./repo-map.md).

## Android control plane

- **Never cache `AccessibilityNodeInfo` references across tool calls.** They
  go stale. Capture `bounds: Rect` at dump time and use cached bounds for
  gestures; re-resolve via `findAccessibilityNodeInfosByViewId` for
  node-action fallbacks.
- **Never replace `serviceInfo` wholesale in `onServiceConnected`.** Augment
  it. Replacing clobbers XML-declared capabilities and causes
  `service.windows` to return empty.
- **Never dispatch gestures at coordinates behind the IME.**
  Orchestra's `tap(selector)` pipeline auto-dismisses the keyboard when
  obscurement flags a keyboard-role node blocking the target AND the
  session flag `maybeKeyboardOpen` was set by a prior `inputText`.
  The agent-side `tap({x,y})` path also rejects coords inside the IME
  window via a geometric check.
- **Never call `typeViaKeyboard` without accounting for IME layout switches.**
  When focus moves from a numeric field to a text field, the keyboard
  re-renders; the handler polls internally before per-key dispatch and
  falls through to `typeViaOnScreenKeys` for custom in-app keypads
  (banking apps, OTP entry widgets).
- **`ResourceIdStrategy` has a walk fallback for Flutter / Compose / RN.**
  Android's `findAccessibilityNodeInfosByViewId` requires fully qualified
  `package:id/name` — Flutter exposes ids like `G01-05-01/2` without a
  prefix, so the strategy walks the tree and matches by suffix when the
  native lookup is empty.
- **`UiTreeService.dumpCompact` keeps elements with ANY stable signal**
  (resourceId / contentDesc / text / clickable). Do not revert this to
  "clickable or labeled only" — Flutter elements frequently have only a
  resourceId.
- **`clickable` flag is unreliable on Flutter / Compose / RN.** Those
  frameworks dispatch gestures in-engine via `GestureDetector` without
  setting the a11y clickable flag. The tool layer ignores the flag when
  deciding whether to tap; do not add a clickable filter.

- **Never run gesture commands on the accessibility-service main
  thread.** `AccessibilityService.dispatchGesture(g, callback,
  handler=null)` delivers its `GestureResultCallback` on the
  service's main thread. Calling `GestureRunner.dispatch` from
  main would block main waiting on a latch that only the main
  thread can fire — indistinguishable from a 30s hang. NanoHTTPD
  worker threads are off-main, which is the supported entry
  point. If a new code path dispatches gestures from a non-route
  context, route it through a background executor first. Same
  class of bug as iOS's main-thread completion-block deadlock.
- **Two-tier capability gate for `dispatchGesture`.** The host
  adapter validates `canMultiPointer` / `canPressure` /
  pressure-range / empty before leaving the driver; the APK's
  `GestureRunner.validatePointers` re-validates everything on
  arrival. Either tier alone is insufficient — a bypassed host
  (direct HTTP poke) or a bypassed device (older APK build)
  would leak unsafe gestures. Removing a rule from one side is
  a regression even when the other still catches it.
- **`GestureDescription.StrokeDescription` requires `duration > 0`
  and non-decreasing timestamps.** `GestureRunner.validatePointers`
  enforces monotonic-non-decreasing offsets at parse time; zero-
  duration taps survive because `buildStroke` floors each stroke
  at `MIN_PRESS_MS` (50ms). Any new stroke-building path must
  apply the same floor or the platform rejects the gesture with
  `IllegalArgumentException` — caught as an uncaught exception
  in the route handler rather than a structured rejection.
- **`canPressure` is permanently false on Android.** The
  accessibility gesture surface has no per-touch pressure API at
  any current Android level. Do not add a "maybe supported on
  device X" flip — the entire backend is pressure-less, and the
  host validator relies on this being a hard no. If Google ever
  ships a pressure-carrying `StrokeDescription` constructor,
  flip in one place: `GestureRunner.GestureCapabilities.DEFAULT`.
- **Long-press-then-drag must flow through the multi-phase
  dispatcher, not a single `GestureDescription`.**
  `GestureRunner` already routes single-pointer paths through
  `dispatchMultiPhase`: each adjacent waypoint pair becomes one
  `GestureDescription` with `willContinue=true` on its stroke,
  hold segments emit a minimal stroke and sleep the remainder
  at Kotlin level, move segments emit their full duration. The
  pointer stays DOWN between dispatches because
  `willContinue=true` preserves the touch across calls (the
  `AccessibilityService.dispatchGesture` contract).

  The reason a single-description approach is wrong: a
  `GestureDescription` stroke whose Path is zero-length still
  emits periodic same-coordinate `ACTION_MOVE` events (the
  platform interpolates at roughly 10ms inside every stroke's
  active window regardless of path length). That stream defeats
  any recogniser that uses a null-check on "have we received a
  move yet?" instead of a slop threshold — Flutter's
  `DelayedMultiDragGestureRecognizer`, which backs
  `ReorderableListView`, is one such recogniser. Slop-based
  long-press (`LongPressGestureRecognizer`) tolerates the
  zero-delta MOVEs, which is why `onLongPress` works with a
  single-description hold but reorder does not.

  Do NOT try to fix this by collapsing the hold stroke to
  `MIN_PRESS_MS` and pushing the next stroke's `startTime` to
  the real hold offset in hope of a silent gap within ONE
  description. `continueStroke` requires adjacent start times
  inside a single `GestureDescription`; gaps are silently
  compressed and the author's 1.4s timeline dispatches in tens
  of milliseconds. The silent hold only works as a Kotlin-level
  sleep BETWEEN dispatch calls.

## Tool layer (cross-platform)

- **Never import anything from `@synapse/*` or any parent-repo path.** Atomyx
  is standalone.
- **Never branch on `ctx.controller.platform` inside tools.** If behavior
  must differ, it belongs in the adapter.
- **Never assume a device-side HTTP server exists** — that's Android-specific.
  iOS uses a host-side bridge. Design tools against `DeviceController`, not
  against the Android HTTP API.
- **Never add a tool that duplicates an existing tool's intent.** The
  surface is deliberately small (27 tools) — overlapping tools cause
  measurable agent confusion. If your new tool overlaps `input_text` /
  `tap` / `find_element`, extend the
  existing tool with a new param instead.
- **Do not render resourceId as a path-like short form** (e.g.
  `#G01-05-01/2`). Agents misparse the `/` as a path separator. Use explicit
  `resourceId="G01-05-01/2"` quoted form in any text output.
- **`get_ui_tree` is cached (2s)** and the handler blocks duplicate calls on
  an unchanged screen. Tools that need fresh tree data should use
  `ctx.invalidateUiCache()` first (`server.ts` dispatches this automatically
  for mutating tools).
- **Do not inline business logic in a tool handler.** Tools under
  `packages/mcp/src/tools/` must delegate to `Orchestra`
  methods from `@atomyx/driver`. If you need new behavior, add
  it to `Orchestra` / `Finder` / `ScrollController` and call it from
  the tool — never paste logic into `execute()`. Filter composition
  lives in `packages/driver/src/filters/` and the
  `compileSelector` priority-broadening policy lives in
  `packages/driver/src/selectors/`.

## Observation-driven input + waits

- **Never introduce hardcoded sleeps in Orchestra action pipelines.**
  Fixed sleeps (`sleep(300)` after tap, `stepDelay: 500` between
  steps, `Thread.sleep(100)` verify windows) are fragile across
  Flutter / RN / iOS. Driver adapters self-synchronize on focus +
  keyboard; Orchestra should not add its own waits without a
  concrete observable justifying them.
- **The `maybeKeyboardOpen` session flag controls the tap keyboard-
  gate.** It is set after every `inputText`; if you invent a new
  Orchestra action that implicitly opens the keyboard, set the flag
  too or subsequent taps will not dismiss correctly on obscurement.
  Clear the flag in `launchApp` / `stopApp` / `hideKeyboard` to avoid
  stale state.
- **Selector ranking respects `nth` as an opt-out.** Without `nth`,
  `compileSelector` sorts candidates by `clickable + focused` score.
  With `nth: N` it preserves document order and picks the Nth match.
  When recording a script where positional selection matters, keep
  the `nth:` in place.
- **Waits live in `packages/driver/src/waits/`, not Orchestra.**
  `waitForFocus`, `waitForText`, `waitForInputReady`,
  `waitForInputCommitted`, `waitForKeyboard`, `waitForTreeStable`
  are free functions that take `Clock` for unit-test determinism.
  Add new waits here (following the same signature pattern), not
  inside Orchestra.
- **Script `handle` polls — do not insert `sleep:` before it.** The
  `handle` command polls the UI tree at 100 ms intervals up to its
  `timeout` (default 5 s) waiting for a branch `when` condition to
  hold. Scripts that padded a `sleep: 3000` before `handle` just
  waste wall-clock now; remove them.

## iOS clear-input

- **`ClearFocusedInputCommand` uses ⌘A + ⌫ first, then an exact-
  length delete-loop fallback.** The fast path requires simulator
  hardware-keyboard pairing (`xcrun simctl keyboard <udid> enable
  hardware`, or Simulator I/O → Keyboard menu); without it ⌘A types
  a literal "a" then ⌫ deletes it, and the fallback loop reads the
  focused value length from `app.snapshot()` to delete exactly that
  many characters. When the field is already empty the command
  short-circuits with `strategy: "already-empty"`.

## iOS control plane

Order = severity (most likely to bite a new contributor first).

### Process / state

- **iOS has no system-wide foreground query.** Unlike Android's
  `AccessibilityService.rootInActiveWindow`, XCUITest can only drive an
  app whose `XCUIApplication` reference the driver is holding. The
  driver tracks this in `DriverState.currentApp` / `state.currentBundleId`
  and the host adapter caches `lastLaunchedBundleId`. **Never** expose
  a tool-layer flow that assumes `currentForeground()` reflects what a
  user manually opened on the sim — if the agent didn't go through
  `launchApp`, the tracked state is empty.

- **`launchApp` resets app state.** `XCUIApplication.launch()` on an
  already-running app terminates and relaunches — any manually-focused
  fields, open sheets, or mid-flow state are lost. Agents calling
  `launchApp` expecting "attach to current state" will be surprised.
  Document this in tool descriptions and prefer `activate()` only when
  you specifically need fresh state.

- **Stale driver state after simulator crash.** If the XCUITest process
  dies mid-session (simulator reboot, Xcode toolchain crash), the host
  adapter's `lastLaunchedBundleId` becomes a lie. The adapter's
  `getUiSummary()` catches "no app launched" errors and clears the
  cache, but this is recovery, not prevention. For long-running
  sessions, consider periodic health-check via `ping`.

### Queries and timing

- **Never use `XCUIElementQuery` + per-element property access for
  bulk operations.** Each property (`identifier`, `label`, `frame`,
  `isHittable`) is a separate RPC to the XCUITest daemon. On a
  ~200-element tree, `dumpElements` via query iteration takes
  **10–20 seconds** end-to-end. Always use `app.snapshot()` + local
  tree walk — one RPC, O(1) property access afterwards. See
  `DefaultXCUIBridge.dumpElements` for the pattern.

- **`XCUIElementSnapshot` does NOT expose `isHittable`.** Hittability
  is a live screen-state query, not cached in snapshot. Any
  code relying on `isHittable` to derive `clickable` semantics must
  use a different heuristic (element type whitelist is the adopted
  one — see `IosXctestController.toCompactElement`).

- **`XCUIElementQuery.waitForExistence(timeout:)` can false-negative
  even on short timeouts.** On app screens with a custom keyboard
  mounted, `app.keyboards.element(boundBy: 0).waitForExistence(
  timeout: 0.2)` returns false despite the keyboard being present in
  the same tick's `app.snapshot()`. For anything requiring reliable
  detection, prefer snapshot walk over element-query polling.

- **Unbounded recursion on deeply-nested trees.** Flutter / React
  Native apps routinely nest 30+ `other` wrapper layers. Any tree
  walker must be iterative (explicit stack) rather than recursive
  to avoid Swift call-stack overflow on pathological targets. See
  `dumpElements` and `resolveSelector` for the pattern.

### Gestures and tapping

- **iOS "back" is NOT a system primitive.** Android dispatches a
  BACK intent regardless of app handling; iOS has only per-screen
  affordances (nav bar back button, modal Cancel/Done, edge swipe,
  app-custom close). `pressKey("back")` tries `navigationBars.buttons[0]`
  first (verifiable) then edge-swipe fallback (unverifiable, returns
  `ok: false`). Agents must check the `ActionResult.ok` field and
  fall back to `find_element` on Cancel/Done/Close/Back labels when
  `ok: false`. **Never treat `pressKey("back")` as guaranteed** the
  way Android allows.

- **Edge-swipe back gesture needs precise params.** `XCUICoordinate`
  normalized offset `dx=0` is essential (not `x=2` absolute), and
  `press(forDuration: 0.0, thenDragTo:, withVelocity: .fast,
  thenHoldForDuration: 0.0)` is the reliable invocation. Slow press
  or non-zero hold gets interpreted as context-menu trigger, not
  navigation gesture.

- **`tap(selector)` via adapter composition is cheaper than
  `XCUIElement.tap()`.** The native `XCUIElement.tap()` path does
  extra pre-flight checks (wait for hittable, wait for stable) that
  fail silently on Flutter elements whose semantic nodes don't have
  proper hit-test metadata. The adapter composes `resolveSelector` +
  `tapCoordinates(midpoint)` instead, which goes through the
  coordinate dispatch path that's Flutter-compatible.

### `XCUIElement.ElementType` enum

- **`elementType == .key` is raw value 20.** If your
  `elementTypeName` switch is incomplete, keyboard keys render as
  `"type20"` in tree dumps — unreadable for agents doing role
  filtering. `DefaultXCUIBridge.elementTypeName` covers 70+ cases
  with `@unknown default` for forward compat. When bumping the
  Xcode support matrix, grep for `"type\d+"` in smoke outputs to
  catch new element types Apple added.

### Flutter / cross-platform assumptions

- **Flutter apps do NOT universally use custom keypads.** Some
  banking apps use the iOS
  system keyboard and native `typeText()` works directly. When
  designing a custom-keypad fallback, do NOT assume every Flutter
  app needs it — check `getKeyboard()` first, branch on
  visibility. The host-side compose fallback (dumpTree key scan →
  per-char tap) is only exercised when a real custom-keypad
  surface is present.

- **Interactive-type empty wrappers are noise.** Some `button` /
  `cell` elements in Flutter trees have empty
  identifier/label/value — the Swift-side stable-signal filter
  keeps them because the type is in the interactive whitelist, but
  they're not actionable from the agent's perspective. Tool-layer
  `filterStable` should drop these. Tracked for future tool-layer
  work.

### Architecture constraints

- **Never try to run an HTTP server inside an iOS app.** The sandbox
  suspends the process within seconds of backgrounding. All our
  transport is TCP from the XCUITest process (host-side).

- **Never change the iOS bridge strategy without discussion.** The
  Swift XCUITest driver is a multi-week commitment; switching would
  be a full rewrite of the iOS transport layer. Propose in an issue
  first so the operational impact is scoped before any code moves.

- **Always design features to reuse the `DeviceController`
  interface.** If you find yourself adding iOS-specific methods to
  the port, you're leaking platform details. The snapshot-based
  query strategy, selector priority chain, and iterative tree walk
  all sit in the adapter (Swift side or TS side), never in the port.

- **Sim driver and device driver cannot both serve port 22087
  simultaneously.** Simulator shares the host network namespace;
  its driver binds `127.0.0.1:22087` directly. A physical device
  needs `iproxy` to tunnel the same port via USB, but iproxy can't
  bind the host side if the sim is already on it. The adapter
  probes the host port with a `ping` handshake BEFORE spawning
  iproxy and polls for real tunnel connectivity AFTER, with hard
  deadlines and actionable errors. If the existing listener is
  another Atomyx driver responding to `ping`, the adapter reuses
  it rather than refusing. Contributors switching between sim and
  device must still stop one driver before starting the other:
  Ctrl+C the sim `make serve` terminal, or `pkill -f
  "xcodebuild.*AtomyxDriver"`, before `make serve-device`.

- **A device UDID selected via MCP does NOT automatically start the
  driver on that device.** `select_device(<device-udid>)` only
  spawns the host-side iproxy tunnel. The Swift driver must already
  be running on the device (via `make serve-device` in another
  terminal) otherwise the tunnel has nothing to forward to and
  the adapter fails with "tunnel did not become reachable — did
  you run `make serve-device`?". This is unlike Android where
  `adb forward` tunnels to an always-running device HTTP server;
  iOS XCTest has no such always-on service, so the driver
  lifecycle is explicit.

- **`make serve-device` uses `test-without-building` and does NOT
  rebuild the Swift driver.** After changing any Swift source under
  `platforms/ios-agent/Tests/`, you MUST `make build-device` (or
  `make setup-device`) before `make serve-device`, otherwise the
  running process is the stale binary from the last build. Symptom:
  you write a Swift fix, restart the driver, and the exact same
  bug / error message comes back bit-for-bit. Rule of thumb: any
  Swift diff → rebuild cycle.

- **Xcode PCH cache breaks after directory rename / `git mv`.**
  Xcode's precompiled headers embed ABSOLUTE paths to the module
  cache location (`.../build/ModuleCache.noindex/...`). If the
  driver directory is renamed (e.g. `ios/driver/` →
  `platforms/ios-agent/` during the modules-layout migration), the
  next build fails with:

      error: PCH was compiled with module cache path '/...old path...'
             but the path is currently '/...new path...'
      error: missing required module 'SwiftShims'

  Fix: nuke the `build/` directory entirely and re-run `make setup`:

      cd platforms/ios-agent
      rm -rf build/
      make setup

  There is no incremental migration — the PCH embeds the old path
  in binary form and xcodebuild re-uses it. Document this in any
  commit that moves `platforms/ios-agent/` so contributors don't lose
  an afternoon debugging the obscure error message.

- **Private XCTest symbol drift falls back silently to the
  coordinate backend.** `EventRecordSynthesizer` looks up
  `XCSynthesizedEventRecord`, `XCPointerEventPath`, and
  `XCTRunnerDaemonSession` via `NSClassFromString`. When
  Apple changes a selector signature between Xcode major
  versions (observed 14→15 and 15→16), the probe at init
  catches it and the factory returns `CoordinateSynthesizer`
  instead. Multi-pointer gestures then start failing with
  `POINTER_MULTI_NOT_SUPPORTED` right after the Xcode
  upgrade — which reads like "my script broke" to a
  confused user. Mitigations: the weekly cron CI job runs
  the smoke against Xcode beta; the `ping` response carries
  `probeLog` so contributor support can see exactly which
  symbol drifted. When adding new private-symbol access,
  always: (a) keep it inside `EventRecordSynthesizer.swift`
  (the single choke point), (b) probe for it in the dry-
  run, (c) document the expected signature in a comment
  next to the call so version bumps can diff against the
  current expectation. Grep rule:

      grep -r 'NSClassFromString("XCSynth\|NSClassFromString("XCPointer\|NSClassFromString("XCTRunner' platforms/ios-agent/

  must return exactly the lines inside
  `EventRecordSynthesizer.swift`. Any other match is an
  isolation violation and a review blocker.

- **iOS private synthesize completion block — never declare
  it in Swift.** The completion block on
  `_XCT_synthesizeEvent:completion:` must be built in
  Objective-C, not Swift. Two failure modes if you skip
  the bridge:

  1. Swift `@convention(block) (NSError?) -> Void` produces
     a thunk that calls `objc_retain` on the argument before
     running the body. The XCTest daemon's XPC reply is
     observed (Xcode 16.2 / iOS 18.3) to invoke the block
     with a sentinel pointer (0x1) instead of nil or a real
     `NSError*`; the auto-retain crashes. Switching the
     argument to `(AnyObject?)` only changes the crash
     symbol from `objc_retain` to `swift_unknownObjectRetain`.
  2. Even with an Objective-C block, ARC will emit
     `objc_retain` on `id`-typed parameters at block entry.
     Declare the parameter as
     `__unsafe_unretained id _Nullable` — the daemon's
     sentinel pointer never gets retained.

  Capture rule: the block must NOT close over Swift values.
  XPC marshalling calls `_Block_copy` on the block, and a
  Swift-captured closure breaks that copy path with a
  segfault inside `_Block_object_assign`. Pass everything
  the callback needs as Objective-C parameters
  (`dispatch_semaphore_t` is fine — it's a true ObjC type).

  Reference impl in `platforms/ios-agent/Tests/Bridge/AtomyxBlockHelper.{h,m}`
  with the bridging header at
  `Tests/Bridge/AtomyxBridgingHeader.h` and the project.yml
  setting `SWIFT_OBJC_BRIDGING_HEADER` pointing at it. The
  Swift call site reads:

  ```swift
  let semaphore = DispatchSemaphore(value: 0)
  let block: Any = atomyxMakeSemaphoreSignalingBlock(semaphore)
  _ = session.perform(
      NSSelectorFromString("synthesizeEvent:completion:"),
      with: record, with: block,
  )
  _ = semaphore.wait(timeout: .now() + 30)
  ```

  Threading: the call MUST run on a background queue.
  `synthesizeEvent:completion:` posts the completion block
  to the main queue; if the dispatch is on main, main
  blocks on `wait` and the completion can never fire.
  `CommandServer` routes synthesizer-using commands
  (`tapAt`, `dispatchPointer`) to the background accept
  queue specifically for this reason — see the threading
  policy comment in `CommandServer.swift`.

- **Nested-Scrollable gesture arbitration is unreliable
  with synthesized swipes.** A vertical `ListView` sitting
  inside another vertical `ListView` (or any
  `Scrollable`-in-`Scrollable` arrangement) routes
  XCUITest-synthesized swipes intermittently to the outer
  scrollable, even when the swipe coordinates land inside
  the inner one. Manual MCP `swipe` commands work; rapid-
  succession `dispatchGesture` calls do not. Until a
  higher-level `scrollIntoView` drives nested scrollables
  explicitly, prefer flat scrollable hierarchies in test
  fixtures, or use MCP-level swipe (which routes through
  `SwipeCommand` and works consistently for single
  dispatches).

- **Multi-pointer dispatch fails mid-inertia.** iOS rejects
  `XCSynthesizedEventRecord` multi-pointer dispatches that
  arrive while a page-scroll inertia animation is still
  running. Symptom: pinch / rotate / two-finger gestures
  silently fail — no error, but Flutter `onScale*` /
  `onScaleStart` never fires. Fix: settle wait of ≥1 s
  between any page scroll and the next multi-pointer
  dispatch. Single-pointer dispatches do not require this
  wait.

- **Synthesizer-using commands MUST set
  `requiresMainThread = false`.** `EventRecordSynthesizer`
  guards against main-thread dispatch (the completion
  block lands on main, so waiting on it from main
  deadlocks). The default for `CommandHandler.requiresMainThread`
  is `true`, so any new gesture command that calls
  `synthesizer.dispatch` must explicitly opt out. Forgetting
  this returns a hard error from the synthesizer rather
  than a deadlock — the test suite catches it on the
  first dispatch — but it's the kind of bug a contributor
  trips over silently when adding a new gesture command.
  See `TapAtCommand`, `LongPressAtCommand`, `SwipeCommand`,
  and `DispatchPointerCommand` for the pattern.

### Agent ergonomics — selector actions

- **Obscurement detection must treat ancestor containers as
  non-obscurers.** Pre-order DFS "last node containing midpoint"
  returns the deepest rendered element at the point — which in
  iOS frequently IS the target's own ancestor (UICollectionView
  wrapping the cell wrapping the label). Without a descendant
  check, `ResolveSelectorCommand.findObscurer` would flag
  legitimately-visible cells as obscured by their own parents.
  Fix: after picking topmost, walk topmost's subtree looking for
  the target by reference equality — if reachable, topmost is an
  ancestor and NOT an obscurer. Anonymous "Other"/"Group"
  containers with empty identifier + label are also suppressed
  because they're almost always unstyled layout wrappers, not
  rendering blockers. Real obscurers (Sheet, Alert, floating
  Button, NavigationBar items) always carry a distinctive
  elementType OR a non-empty identifier/label.

- **Safe-area insets matter for "in viewport" checks.**
  `XCUIApplication.frame` returns the full screen rect (e.g.
  430×932 on iPhone 12 Pro Max) — status bar, notch, and home
  indicator included. An element whose midpoint sits in those
  edge zones is technically within the rect but is NOT reliably
  tappable (home indicator captures bottom swipes, status bar
  absorbs top taps). `ensureVisible` uses conservative insets
  (60pt top, 50pt bottom) before deciding an element is visible,
  so the scroll loop keeps running until the element is
  comfortably inside the interactive area.

- **`tap(selector)` on virtualized lists needs a scroll-SEARCH
  phase in addition to the positional scroll-into-view loop.**
  iOS UITableView/UICollectionView recycle off-screen cells, so a
  cell that exists logically may be absent from the current
  `app.snapshot()` tree entirely. `resolveSelector` correctly
  reports `found:false`, but the canonical agent workflow is
  "selector-first, fallback to coords" — the adapter should not
  force the agent to scroll manually when a recycled cell is the
  root cause. `ensureVisible` has a `scrollSearchForSelector` path
  that swipes the screen center column UP for N iterations (most
  anchor items live near list start), then DOWN for N iterations,
  probing `resolveSelector` between each swipe. Budget is
  intentionally small — 6 + 6 swipes — because this is a
  fallback, not the primary workflow, and runaway scrolling is
  worse than a clear "couldn't find element" error.

- **Search bars ARE editable text inputs.** Any editable-element
  whitelist that substring-matches class names (`edittext`,
  `textfield`, `textinput`, `securetextfield`, `editable`) must
  also include `searchfield` — iOS serializes
  `XCUIElementTypeSearchField` as `className == "searchField"`,
  which does NOT contain `"textfield"`. Missing it makes
  `input_text(selector)` on iOS search UIs fall back to coord-
  only. Any new iOS editable-element class name should join the
  same list.
