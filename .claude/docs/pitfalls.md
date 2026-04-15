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
  `GestureDispatcher.tap()` auto-dismisses the IME if the target intersects
  the keyboard bounds. Additionally, `tap_coordinates`-via-`tap({x,y})`
  rejects coords inside the IME window via the `coordInIme` geometric check.
- **Never call `typeViaKeyboard` without accounting for IME layout switches.**
  When focus moves from a numeric field to a text field, the keyboard
  re-renders; the handler polls via `waitForKeyboardReady` with a fast path.
  It also has a `typeViaOnScreenKeys` fallback for custom in-app keypads
  (Flutter banking apps).
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

## Tool layer (cross-platform)

- **Never import anything from `@synapse/*` or any parent-repo path.** Atomyx
  is standalone.
- **Never branch on `ctx.controller.platform` inside tools.** If behavior
  must differ, it belongs in the adapter.
- **Never assume a device-side HTTP server exists** — that's Android-specific.
  iOS uses a host-side bridge. Design tools against `DeviceController`, not
  against the Android HTTP API.
- **Never add a tool that duplicates an existing tool's intent.** The
  consolidation from 40 → 19 fixed a measurable agent-confusion problem. If
  your new tool overlaps `input_text` / `tap` / `find_element`, extend the
  existing tool with a new param instead.
- **Do not render resourceId as a path-like short form** (e.g.
  `#G01-05-01/2`). Agents misparse the `/` as a path separator. Use explicit
  `resourceId="G01-05-01/2"` quoted form in any text output.
- **`get_ui_tree` is cached (2s)** and the handler blocks duplicate calls on
  an unchanged screen. Tools that need fresh tree data should use
  `ctx.invalidateUiCache()` first (`server.ts` dispatches this automatically
  for mutating tools).
- **Do not inline business logic in a tool handler.** Tools under
  `packages/core-driver-mcp/src/tools/` must delegate to `Orchestra`
  methods from `@atomyx/core-driver`. If you need new behavior, add
  it to `Orchestra` / `Finder` / `ScrollController` and call it from
  the tool — never paste logic into `execute()`. The previous
  `src/tools/core/` strategy-class pattern is being phased out; new
  work uses the functional filter composition in
  `packages/core-driver/src/filters/` + the `compileSelector`
  priority broadening policy.

## iOS control plane

Traps surfaced while shipping Phase 3. Order = severity (most likely
to bite a new contributor first).

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
  even on short timeouts.** Verified against kabuappStation where
  `app.keyboards.element(boundBy: 0).waitForExistence(timeout: 0.2)`
  returned false despite the keyboard being present in the same
  tick's `app.snapshot()`. For anything requiring reliable detection,
  prefer snapshot walk over element-query polling.

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
  Xcode matrix (Phase 4), grep for `"type\d+"` in smoke outputs
  to catch new element types Apple added.

### Flutter / cross-platform assumptions

- **Flutter apps do NOT universally use custom keypads.** Week 1
  assumption (banking apps always draw their own `GestureDetector`
  keypads) was invalidated by `inc.guide.kabuappStation.dev` which
  uses the iOS system keyboard and native `typeText()` works fine.
  When designing a custom-keypad fallback, do NOT assume all
  Flutter apps need it — check `getKeyboard()` first, branch on
  visibility. The fallback path (host-side compose: dumpTree key
  scan → per-char tap) remains unvalidated until a real
  custom-keypad fixture surfaces.

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

- **Never commit to a new bridge approach without discussion.** The
  current Swift-driver path was chosen after evaluating Appium, WDA,
  idb, and Maestro (see `ios.md` decision log). Switching the
  bridge is a multi-week rewrite — propose in an issue first.

- **Always design features to reuse the `DeviceController`
  interface.** If you find yourself adding iOS-specific methods to
  the port, you're leaking platform details. The snapshot-based
  query strategy, selector priority chain, and iterative tree walk
  all sit in the adapter (Swift side or TS side), never in the port.

- **Sim driver and device driver cannot both serve port 22087
  simultaneously.** Simulator shares the host network namespace;
  its driver binds `127.0.0.1:22087` directly. A physical device
  needs `iproxy` to tunnel the same port via USB, but iproxy can't
  bind the host side if the sim is already on it. Early Phase 5
  had a race-condition bug where the adapter silently routed
  `select_device(<real-device-udid>)` to the sim driver when both
  were running — iproxy failed bind, the timeout-based "success"
  heuristic resolved anyway, and the subsequent TCP connect hit
  the sim's listener. Fix: adapter now explicitly probes the host
  port BEFORE spawning iproxy and polls for real tunnel
  connectivity AFTER, with hard deadlines and actionable errors.
  **Contributors must stop one driver before starting the other**:
  Ctrl+C the sim `make serve` terminal, or `pkill -f "xcodebuild.*AtomyxDriver"`,
  before `make serve-device`.

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
  `native/ios-driver/Tests/`, you MUST `make build-device` (or
  `make setup-device`) before `make serve-device`, otherwise the
  running process is the stale binary from the last build. Symptom:
  you write a Swift fix, restart the driver, and the exact same
  bug / error message comes back bit-for-bit. Rule of thumb: any
  Swift diff → rebuild cycle.

- **Xcode PCH cache breaks after directory rename / `git mv`.**
  Xcode's precompiled headers embed ABSOLUTE paths to the module
  cache location (`.../build/ModuleCache.noindex/...`). If the
  driver directory is renamed (e.g. `ios/driver/` →
  `native/ios-driver/` during the modules-layout migration), the
  next build fails with:

      error: PCH was compiled with module cache path '/...old path...'
             but the path is currently '/...new path...'
      error: missing required module 'SwiftShims'

  Fix: nuke the `build/` directory entirely and re-run `make setup`:

      cd native/ios-driver
      rm -rf build/
      make setup

  There is no incremental migration — the PCH embeds the old path
  in binary form and xcodebuild re-uses it. Document this in any
  commit that moves `native/ios-driver/` so contributors don't lose
  an afternoon debugging the obscure error message.

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
  root cause. `ensureVisible` has a Phase 0 `scrollSearchForSelector`
  that swipes the screen center column UP for N iterations (most
  anchor items live near list start), then DOWN for N iterations,
  probing `resolveSelector` between each swipe. Budget is
  intentionally small — 6 + 6 swipes — because this is a
  fallback, not the primary workflow, and runaway scrolling is
  worse than a clear "couldn't find element" error.

- **Search bars ARE editable text inputs.** `find-input.ts#isEditText`
  substring-matches `className` against a whitelist (`edittext`,
  `textfield`, `textinput`, `securetextfield`, `editable`). iOS
  `XCUIElementTypeSearchField` serializes as `className == "searchField"`
  which does NOT contain `"textfield"` — so without an explicit
  `searchfield` entry, `input_text(selector)` on iOS search UIs
  falls back to coord-only. Added to the whitelist; any new iOS
  editable-element class name should join the same list.
