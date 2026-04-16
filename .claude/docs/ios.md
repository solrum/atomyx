# iOS support — implementation roadmap

> **Status**: 🟡 Approach chosen, not yet implemented. This document is the
> authoritative plan. Contributors working on iOS must read it fully before
> writing code. Do not skip phases. Do not start Phase N+1 until Phase N
> passes its exit criteria.

---

## TL;DR

- **Approach**: Custom Swift XCTest driver, Maestro-style. No Appium, no
  WebDriverAgent, no idb (after prototype).
- **Distribution**: Ship source via Swift Package Manager; user's Xcode
  builds the driver locally. No pre-built binaries. No Apple signing pain
  for the Atomyx project.
- **Support matrix**: Xcode latest-1 / latest / latest beta. iOS 16+.
  Simulator first, real device in Phase 5.
- **Timeline**: 8.5–9.5 weeks sequential, 6–7 weeks calendar with multiple
  contributors.
- **Non-goals**: Not chasing bleeding-edge XCUITest APIs. Not supporting
  iOS < 16. Not shipping pre-built binaries.

---

## Why this approach

The cross-platform by design principle says every capability must be
expressible through `DeviceController` without leaking platform details. We
evaluated four candidates against that bar:

| Option                  | Fits port cleanly | Latency  | Maintenance | Distribution |
| ----------------------- | ----------------- | -------- | ----------- | ------------ |
| Appium + xcuitest driver| No (WebDriver shape) | High  | Low (upstream) | Easy (npm) |
| Direct WebDriverAgent   | Partial           | Medium   | Medium       | Xcode project |
| idb (Meta)              | Partial           | Medium   | ⚠️ Meta drawdown | Brew |
| **Custom Swift driver** | **Yes**           | **Lowest** | **Higher** | **SPM + source** |

Appium and WDA leak WebDriver semantics into the adapter, forcing the tool
layer to absorb shape mismatches — the exact leak the cross-platform
principle forbids. idb is closer but Meta has de-prioritized the project.
Custom Swift is the only option where the wire protocol can mirror
Android's HTTP API one-to-one.

The pain point of custom is **Xcode/Swift version matrix coverage**. That
is mitigated by:

1. **Ship source, not binaries.** User's Xcode builds the driver. No
   pre-built matrix. No Apple signing infra for the Atomyx project.
2. **Pin minimum iOS 16 / Xcode 15.** Narrow the matrix to 2–3 active
   versions. XCUITest core API has been stable since iOS 9 — the matrix
   pain is overstated when you stay on the stable subset.
3. **Subset XCUITest API usage.** Core only (`XCUIApplication`,
   `XCUIElement`, `XCUIElementQuery`, `tap`, `typeText`, `swipe`,
   `screenshot`). No bleeding-edge accessibility audit APIs.
4. **Conditional compilation + runtime `@available` fallback.** Version
   drift isolated to a `XCUIBridge` strategy class.
5. **CI matrix on GitHub Actions macOS runners.** Weekly cron to catch
   Apple drift before users hit it.

Open-source contributor model absorbs the maintenance burden that would
be prohibitive for a single-maintainer project.

---

## Support policy

- **Supported**: Xcode latest-1, latest, latest beta (preview only).
  Currently means Xcode 15.4 / 16.x / 17 beta.
- **Best effort**: Xcode latest-2. PRs welcome, merge if not invasive.
- **Unsupported**: Xcode ≤ 14, iOS ≤ 15.
- **Platforms**: iOS simulator first (Phase 2–4), real iPhone/iPad in
  Phase 5. iPadOS treated as iOS.
- **No tvOS, watchOS, visionOS.** Different XCUITest flavors, out of
  scope.

Support policy is published in `ios/README.md` once the code lands.

---

## Architecture

```
ios/
├── driver/                          # Swift Package
│   ├── Package.swift
│   ├── Sources/
│   │   ├── AtomyxDriver/              # XCTest bundle target
│   │   │   ├── AtomyxDriver.swift        # XCTestCase entry point
│   │   │   ├── CommandServer.swift     # Unix socket / usbmux listener
│   │   │   ├── WireProtocol.swift      # JSON encode/decode matching RFC
│   │   │   └── Commands/               # One file per command
│   │   │       ├── LaunchCommand.swift
│   │   │       ├── TapCommand.swift
│   │   │       ├── DumpTreeCommand.swift
│   │   │       └── …
│   │   └── AtomyxBridge/              # XCUI version strategy
│   │       ├── XCUIBridge.swift           # protocol
│   │       ├── XCUIBridgeLegacy.swift     # iOS 15–16 path
│   │       ├── XCUIBridgeModern.swift     # iOS 17+ path
│   │       └── SelectorResolver.swift     # strategy chain, mirrors Android
│   └── Tests/                       # XCTest unit tests for command handlers
└── README.md                        # Build / signing / support matrix

src/adapters/ios-xctest.adapter.ts   # host-side HTTP/socket client
scripts/ios-setup.mjs                # `atomyx ios setup` — triggers xcodebuild
```

Wire protocol mirrors the Android HTTP routes one-to-one. Commands are
JSON over a local socket. Transport is Unix socket on simulator and
`usbmux`/`iproxy` on real device.

---

## Command surface (frozen after Phase 0)

Mirrors Android routes in `android/README.md`. 16 commands total; the
wire protocol names below are the **frozen** values a raw TCP client
must send. The TypeScript adapter's `DeviceController` method names
differ (they match the cross-platform port); only the Swift-side wire
protocol names are the source of truth for JSON payloads.

| Android route                       | iOS wire command  | Status | Notes |
|-------------------------------------|-------------------|--------|-------|
| `GET /tree?format=compact`          | `dumpTree`        | ✅ Week 2 | XCUIElement tree → CompactElement[] |
| `GET /keyboard`                     | `getKeyboard`     | ⏳ Batch 3 | `XCUIApplication.keyboards` |
| `GET /screenshot`                   | `screenshot`      | ✅ Batch 1 | `XCUIScreen.main.screenshot()` |
| `GET /current-activity`             | (host-side only)  | ✅ Week 2 | TS adapter returns tracked `lastLaunchedBundleId`; no wire command — XCUITest has no system-wide query |
| `GET /apps`                         | (host-side only)  | ✅ Batch 1 | TS adapter shells out to `xcrun simctl listapps <UDID>` + `plutil`; no wire command |
| `POST /resolve`                     | `resolveSelector` | ⏳ Batch 2 | Selector → element metadata |
| `POST /actions/tap`                 | `tap`             | ⏳ Batch 2 | Selector-based tap |
| `POST /actions/tap_coords`          | `tapAt`           | ✅ Week 2 | XCUICoordinate-based |
| `POST /actions/long_press`          | `longPressAt`     | ✅ Batch 1 | XCUICoordinate + duration |
| `POST /actions/clear_focused_input` | `clearFocusedInput` | ⏳ Batch 2 | `typeText(XCUIKeyboardKey.delete)` fallback |
| `POST /actions/swipe`               | `swipe`           | ✅ Batch 1 | XCUICoordinate from/to |
| `POST /actions/input`               | `inputText`       | ⏳ Batch 2 | Selector + text, uses `typeText` |
| `POST /actions/type_keyboard`       | `typeText`        | ✅ Batch 1 | Native `XCUIApplication.typeText(_:)` — handles system keyboard layout switching transparently. Custom in-app keyboards (Flutter / RN) deferred to Batch 3 fallback |
| `POST /actions/key`                 | `pressKey`        | ✅ Batch 1 | Returns `ActionResult` (port change — see decision log). `home` → `XCUIDevice.shared.press(.home)`, always `ok:true`. `enter` → `typeText("\n")`, always `ok:true`. `back` multi-strategy: (1) `navigationBars.buttons[0]` tap → `ok:true reason:"used: nav_bar_back"`, (2) edge-swipe fallback → `ok:false` with hint to fall back to `find_element` on Cancel/Done/Close/Back labels |
| `POST /actions/launch`              | `launchApp`       | ✅ Week 2 | `XCUIApplication.launch`; updates driver-tracked state |
| `POST /actions/force_stop`          | `forceStopApp`    | ✅ Batch 1 | `XCUIApplication.terminate`; clears driver-tracked state on match |
| `GET /health`                       | `ping`            | ✅ Week 2 | Returns `{pong, pid}` — liveness probe |

Selector mapping (mirrors TODO checklist at end of file):

- `resourceId` → `accessibilityIdentifier`
- `contentDesc` → `accessibilityLabel`
- `text` → `value` (StaticText) or `label` (Button)
- `textContains` → NSPredicate `label CONTAINS[cd] "X"`
- `hint` → fuzzy multi-field match
- `predicate` → raw NSPredicate (iOS-native, passes through)
- `classChain` → raw XCUITest class chain (iOS-native, passes through)

`pressKey("back")` is implemented as a swipe-from-left-edge gesture. iOS
has no hardware back button; raising an error is hostile to cross-platform
specs.

---

## Roadmap

### Phase 0 — RFC & preparation (3–5 days)

**Goal**: Lock scope before writing any Swift.

- [ ] Finalize this document as the RFC. Open a GitHub discussion linking
      to it.
- [ ] Maintainer approval required before Phase 1.
- [ ] Do not code during this phase. If discussion surfaces a blocker,
      update this doc and re-open.

**Exit criteria**: RFC approved in discussion, command surface frozen.

### Phase 1 — ~~idb prototype~~ SKIPPED

> **Status**: Removed 2026-04-15. See decision log at the end of this doc.
>
> Original plan was to use Meta's `idb` as a throwaway prototype to
> validate the port abstraction before committing to Swift. Investigation
> showed:
> - `idb_companion` binary is built Aug 2022 and hangs on `CoreSimulator:
>   Loading` with Xcode 16 / iOS 18.3 runtime.
> - Meta deprecated idb in 2023; no new releases.
> - Python client broken on Python 3.14 (`asyncio.get_event_loop()`
>   removed); workable only via pipx-pinned Python 3.12.
>
> Investing a week in a tool the upstream has abandoned is the wrong
> trade. Port abstraction validation moves into Phase 2 week 1 instead
> (see below).

### Phase 2 — Swift driver skeleton + port validation (2 weeks)

**Goal**: Swift XCTest bundle runs end-to-end for 3 commands. First week
doubles as port abstraction validation — the work that Phase 1 used to do.

**Week 1 — Exploration (port validation + wire protocol shakedown)**.
Code written this week is **not production-committed**. Goal is to
discover abstraction mismatches early, at the smallest possible scope.

- [ ] Create `native/ios-driver/` Swift Package
  - `Package.swift` with SPM config, iOS 16 minimum
  - XCTest bundle target + `XCUIApplication` bootstrap
- [ ] Minimal Swift prototype: 2 commands only — `dumpTree` and `tapAt`
  - Hardcode selector/coordinate paths, no strategy chain yet
  - No `XCUIBridge` version abstraction yet
  - No command registry — single switch statement is fine
- [ ] Smallest possible transport: Unix socket + JSON, no
      schema validation, no streaming
- [ ] Host-side throwaway `ios-xctest.adapter.ts` rewrite implementing
      `getUiSummary()` + `tapCoordinates()` only
- [ ] **Validate against real XCUITest semantics**:
  - Does `XCUIElementQuery` return elements in a shape that maps
    cleanly to `CompactElement`?
  - Does `accessibilityIdentifier` / `accessibilityLabel` / `value` /
    `label` actually populate the way the port assumes?
  - Does `XCUICoordinate.tap()` accept raw pixel coords or normalized?
  - Are there hidden session/state requirements (e.g. must app be
    launched through XCUITest, not `simctl launch`)?
- [ ] Document every mismatch in this doc; amend the command RFC if
      needed, keeping platform-neutral names

**Exit criteria for week 1**:
- ✅ Port interface does not require any `platform === 'ios'` branches
- ✅ Wire protocol shape frozen (JSON schema finalized)
- ✅ 2 commands work end-to-end: TS → Swift → simulator → response
- ❌ If abstraction mismatch found → amend port + RFC + repeat

**Week 1 status: PASS** (2026-04-15). Exploration surfaced one material
mismatch (no system-wide foreground query on iOS) which was resolved by
tracking state in the driver instead of changing the port. Wire protocol
for `launchApp` / `dumpTree` / `tapAt` frozen as Week 2 baseline. See
"Week 1 findings" below for the full list.

## Week 1 findings

Discovered while running the exploration driver against Settings on
iPhone 16 Pro Max (iOS 18.3, Xcode 16.2, build 16C5032a).

### Mismatches surfaced (action taken)

1. **No system-wide foreground query in XCUITest.** `XCUIApplication()`
   returns the test target's declared app, not "whatever is on screen".
   There is no equivalent of Android's `AccessibilityService.windows` /
   `rootInActiveWindow`. The simulator home screen is invisible to
   XCUITest unless an app has been explicitly launched.

   *Action*: Driver tracks `currentApp: XCUIApplication?` set on
   `launchApp`. `dumpTree` / `tapAt` require this to be non-nil. Host
   adapter caches `lastLaunchedBundleId` and returns it from
   `currentForeground()`. Port API unchanged — platform detail stays
   in the adapter. Cross-app navigation via OS (deep link, user swipe)
   will desync the tracked state; this is an accepted Week 1 limitation,
   to be documented in `pitfalls.md` once Week 2 lands.

   *Also added to scope*: `launchApp` became the 3rd Week 1 command. Not
   a scope creep — it's a prerequisite for `dumpTree`/`tapAt` to return
   anything meaningful.

### Characterization of the XCUITest tree (no action needed in Week 1,
feeds Week 2 adapter logic)

2. **High wrapper-to-signal ratio.** 166 total elements, only 47 carry
   any selector signal (identifier / label / value). The rest are
   `role=other` containers all reporting the same full-screen bounds.
   Week 2 dumpCompact must filter to elements with at least one stable
   signal, mirroring Android's `UiTreeService.dumpCompact` invariant.

3. **`accessibilityIdentifier` is excellent on Apple's own apps.**
   Settings rows carry ids like `com.apple.settings.general`,
   `com.apple.settings.siri`, `com.apple.settings.primaryAppleAccount`.
   These map 1:1 to Android's `resourceId` pattern. No adaptation
   needed — the existing `SelectorResolutionPipeline` priority order
   (resourceId → contentDesc → text → textContains → hint) works as-is.

4. **Button wrappers duplicate their children.** A single settings row
   is emitted as: a `button` with full label + bounds, plus child
   `staticText` (the row title) + `image` (the chevron), all with their
   own smaller bounds inside the button. `find_element(label="General")`
   would match two elements. Week 2 needs a merge strategy: drop child
   elements whose bounds are wholly contained by a parent button with
   the same label. This is analogous to Android's clickable-parent
   consolidation but triggered by different signals.

5. **Duplicate identifiers for SF Symbol reuse.** `chevron.forward`
   appears as the `identifier` of every row's disclosure indicator.
   This is SF Symbol name, not a unique resource id. The existing
   `AmbiguityDetector` will flag these as `(N×)` — no new logic needed,
   but Week 2 adapter must make sure the duplicate token detection
   path is exercised by the iOS data shape. Android already has the
   same issue with shared `contentDesc`, so the tests carry over.

6. **`accessibilityIdentifier == accessibilityLabel` overlap.** Many
   Apple buttons set both to the same string (e.g. "General" / "General").
   The Week 2 adapter's `toCompactElement` should dedupe the selector
   output: if `resourceId === contentDesc`, emit only `resourceId`.
   Cosmetic, but avoids noisy tree rendering.

7. **`XCUIElement.isHittable` ≠ Android's `clickable` semantically.**
   `isHittable` means "can receive touch events from the test runner's
   perspective" — it's true for most non-obscured elements, not only
   interactive ones. Week 2 `clickable` derivation should prefer
   `elementType == .button || .cell || .link` OR `isHittable &&
   identifier != ""`. Do not blindly map `isHittable → clickable`.

8. **Bounds are in points, as expected.** iPhone 16 Pro Max reported
   `right=440, bottom=956`. Logical spec is 430×932. Difference appears
   to come from the status bar / home indicator safe area being
   included in the root container frame. Week 2 verify via
   `UIScreen.main.bounds` and document the safe-area behavior — tools
   that compute "middle of screen" coordinates must not assume the
   reported bounds match the nominal device size.

9. **`XCUICoordinate` uses points and fires real UI events.** Tap at
   (220, 500) — computed from the `button` bounds in the dumped tree —
   successfully navigated from Settings root to "Apple Intelligence &
   Siri". No special hit-testing or coordinate conversion required.
   `tapAt(x, y)` with raw points matches Android's `tap_coordinates`
   semantics directly.

### Items that are NOT mismatches (noted for clarity)

- `CompactElement.bounds` shape fits XCUITest output without adjustment.
- Wire protocol (line-delimited JSON over TCP localhost) transports
  fine inside the simulator; no timing or framing issues observed.
- Swift `DispatchQueue.main.sync` from the accept-loop thread into the
  XCUITest-owning main thread works because `RunLoop.current.run()`
  on the main thread continues processing GCD events.
- `XCUIApplication.state.rawValue` returned from `launchApp` is
  enough for a basic liveness check; no need to add a separate "is
  running" command.

### Wire protocol frozen for Week 2

```
→ { "id": N, "type": "ping", "args": {} }
← { "id": N, "ok": true, "data": { "pong": true, "pid": <int> } }

→ { "id": N, "type": "launchApp", "args": { "bundleId": "com.apple.Preferences" } }
← { "id": N, "ok": true, "data": { "bundleId": "...", "state": <int> } }

→ { "id": N, "type": "dumpTree", "args": { "bundleId"?: "...", "limit"?: 200 } }
← { "id": N, "ok": true, "data": { "total": N, "count": M, "truncated": bool, "elements": [...] } }
  element shape: { type, id, label, value?, enabled, hittable, x, y, w, h }
  coordinates/dimensions in POINTS; x/y are element midpoints.

→ { "id": N, "type": "tapAt", "args": { "x": 220, "y": 500 } }
← { "id": N, "ok": true, "data": { "x": 220, "y": 500 } }

Errors: { "id": N, "ok": false, "error": "..." }
```

Week 2 extends this with 13 more commands (see command surface table at
the top of this doc) WITHOUT breaking the above shape.

**Week 2 — Production skeleton**.

- [ ] Rewrite Swift driver with proper structure:
  - `CommandServer.swift` / `WireProtocol.swift` / `Commands/*`
  - `XCUIBridge` protocol + `XCUIBridgeLegacy` / `XCUIBridgeModern` stubs
  - Command registry for dispatch
- [ ] `usbmux` transport abstraction stub (wiring in Phase 5)
- [ ] Implement 3 commands end-to-end: `launch`, `dumpTree`, `tapAt`
- [ ] Host-side `src/adapters/ios-xctest.adapter.ts` rewrite using the
      frozen wire protocol
- [ ] `scripts/ios-setup.mjs` — wraps `xcodebuild build-for-testing`
- [ ] Swift unit tests for the 3 command handlers

**Exit criteria for week 2**: 3 commands work end-to-end with production
structure. Swift unit tests green. Week 1 exploration code deleted or
refactored into the production skeleton.

**Week 2 status: PASS** (2026-04-15).

- ✅ Swift driver split into 10 files (Server/WireProtocol, Server/CommandServer,
  Bridge/XCUIBridge, Commands/{CommandHandler, PingCommand, LaunchAppCommand,
  DumpTreeCommand, TapAtCommand}, AtomyxDriverUITests, CommandHandlerUnitTests).
- ✅ Command registry + handler protocol pattern (analog of Android Route/Router).
- ✅ `XCUIBridge` protocol with `DefaultXCUIBridge` stable-API implementation.
  Version-specific split deferred until a concrete need appears (premature
  abstraction avoided).
- ✅ 11 unit tests green for command handlers + wire protocol via
  `MockXCUIBridge`. Serving test (`testServeCommands`) kept out of unit-test
  runs via `xcodebuild -only-testing` class selection, not env-var gating
  (env vars do not pass through `xcodebuild test` to the test child process).
- ✅ Makefile with `setup` / `test` / `serve` / `smoke` / `clean` targets.
  Auto-detects booted simulator UDID; overridable via `UDID` / `DESTINATION`.
- ✅ Production TS adapter (`src/adapters/ios-xctest.adapter.ts`) rewrite:
  driver's server-side filter cuts total elements 166 → 60 (64% reduction);
  identifier==label dedupe works for exact-match cosmetic cleanup;
  `clickable` derivation via interactive type whitelist + hittable-with-id
  fallback; state tracking for `currentForeground()` via `lastLaunchedBundleId`.
- ✅ Smoke test against iPhone 16 Pro Max iOS 18.3: launchApp + dumpTree +
  tapAt + tree delta after tap (+31 added / -17 removed, confirming
  navigation from Settings root to Siri page).

See "Week 2 finding revisions" below for corrections to Week 1 findings
discovered during production mapping work.

## Week 2 finding revisions

Two Week 1 findings needed correction after production adapter mapping
surfaced the actual element shape.

**Finding #4 revision — button wrapper + duplicate children do NOT need
an adapter-level merge.** Week 1 speculated that a merge strategy would
be needed. In practice the duplication is:

```
button      "General" bounds=20,330..420,374 resourceId="com.apple.settings.general"
staticText  "General" bounds=40,338..143,367 contentDesc="General"
```

Both elements carry "General" as a potential selector match. This is
**ambiguity**, not duplication. The existing cross-platform
`AmbiguityDetector` in `src/tools/core/` already handles this pattern
for Android (where the same contentDesc appears on multiple sibling
elements) and emits `(N×)` markers in the rendered tree. The same
detector works on iOS data shape without modification.

Adapter-level merge would duplicate logic that already lives at the
tool layer and would violate the "strategy classes own business rules"
principle. No action taken in Week 2; no action needed.

**Finding #6 revision — `accessibilityIdentifier == accessibilityLabel`
is the MINORITY case, not the majority.** Week 1 claimed "many Apple
buttons set both to the same string (e.g. 'General' / 'General')".
Actual Week 2 data shows Apple's Settings app almost always uses
different strings:

- Settings rows: `id = "com.apple.settings.general"`, `label = "General"`
- Nav bar title: `id = "Settings"`, `label = "Settings"` ← exact match
- SF Symbol images: `id = "chevron.forward"`, `label = "chevron.forward"` ← exact match
- System controls: `id = "Dictate"`, `label = "Dictate"` ← exact match

The dedupe logic (`if (label && label !== id) selector.contentDesc = label`)
still fires for the minority case and remains useful as cosmetic cleanup.
But the majority of Settings rows keep both fields because they carry
distinct information: `resourceId` is the developer-set stable id,
`contentDesc` is the user-visible label. Agents benefit from both.

### Finding #11 — Flutter apps do NOT always use custom keypads (kabuappStation discovery)

Week 1 assumed Flutter banking apps would typically draw their own
numeric keypads via `GestureDetector` / `CustomPaint` without
`Semantics` annotations — matching the pattern on Android where this
forced `GestureDispatcher.typeViaOnScreenKeys` fallback with on-screen
key tap scanning.

Discovery during Batch 3 Phase 3 work against
`inc.guide.kabuappStation.dev` (Japanese stock/futures trading app):

- Account number + password fields are **standard Flutter TextField**
  widgets bound to iOS `UIKeyboard`
- Tapping the field (via `tapAt` on midpoint) triggers the system
  keyboard exactly as a UIKit app would
- Native `XCUIApplication.typeText("123")` types successfully into the
  focused field; subsequent `dumpTree` shows `value="123"` on the
  field element
- The keyboard IS visible in `app.snapshot()` as an
  `elementType == .keyboard` node with id `"UIKeyboardLayoutStar Preview"`
  and 11 child `.key` descendants (digits 0–9 + Delete)

**Implications**:

1. The "Flutter custom keypad fallback" planned for Batch 3 is NOT
   needed for this app. Deferred until a real custom-keypad fixture
   surfaces.
2. Not all Flutter apps are adversarial to XCUITest accessibility —
   well-written Flutter apps with `Semantics()` annotations produce
   queryable trees.
3. The kabuappStation Android version DID require custom keypad tap
   scanning — but that was for a DIFFERENT keypad (in-app PIN entry,
   not the login account number field). The iOS version of the same
   logical screen uses system keyboard; the Android + iOS codebases
   diverge in keyboard implementation.

**Downstream effect**: Finding #11 does not invalidate the planned
fallback path architecturally — just means we don't have a fixture to
validate it today. When a fixture app surfaces, the fallback should
use host-side composition (getKeyboard → if not visible → dumpTree
scan for key-like elements → tap each char), not a new Swift command.

### Finding #12 — `XCUIElement.ElementType` enum order (type20 = .key)

Batch 3 Phase 3 uncovered `elementTypeName` was missing most
`ElementType` enum cases — only ~25 were explicitly handled, the
rest fell through to `"type\(t.rawValue)"`. On iOS system keyboards,
keys are `elementType == .key` (raw value 20) which rendered as the
string `"type20"` in tree dumps — unreadable for agents doing role
filtering.

**Fix**: expanded the switch to 70+ cases covering the full enum
(application, button, cell, group, alert, dialog, keyboard, key,
textField, tableRow, collectionView, pickerWheel, webView, menu,
datePicker, segmentedControl, etc.) with `@unknown default` for
forward compat when Apple adds new types in future Xcode releases.

**Implication**: if future iOS XCTest adds new element types, the
`@unknown default` path gracefully returns `"type\(rawValue)"`
instead of crashing, but those new types won't have human-readable
names until we explicitly add them. Worth a periodic check when
updating the supported Xcode matrix (Phase 4).

### Finding #10 — Interactive-type empty wrappers

New observation from Week 2 smoke data: roughly 13 of 60 post-filter
elements are interactive types (`button`, `cell`, `searchField`) with
**empty identifier / label / value**. The Swift-side filter keeps them
because their `elementType` is in the interactive whitelist. The host
adapter maps them to `CompactElement` with empty selector and empty
label.

These are UIKit scaffolding — e.g. containers that wrap an actual
interactive child but carry no metadata themselves. Agents can't
address them by selector, so they are noise at the tool-layer level.

**Action for Phase 3**: the tool-layer `filterStable` helper (already
used by Android via `filterStable()` in `tree-render.ts`) should receive
iOS data too and drop elements with empty selector AND empty label. No
changes to wire protocol or adapter needed — filtering happens one
layer up.

### Phase 3 — Full command surface (2 weeks, split into 3 batches)

**Goal**: All 16 commands work. Tool layer drives iOS unchanged.

Split into batches to keep each PR reviewable and avoid shipping
abstraction bugs at scale. Each batch has its own smoke pass before the
next one starts.

**Batch 1 — coord-based + host-side + typeText** ✅ PASS (2026-04-15). 7 commands + 1 port breaking change:

- [x] `screenshot` — `XCUIScreen.main.screenshot().pngRepresentation` → base64
- [x] `forceStopApp` — `XCUIApplication.terminate()` + clear tracked state
      if matching `currentBundleId`
- [x] `pressKey` — multi-strategy with honest return:
      - `home` / `enter` always `ok:true`
      - `back` tries (1) `navigationBars.buttons[0]` (verifiable, returns
        `ok:true reason:"used: nav_bar_back"`), (2) edge-swipe fallback
        (unverifiable, returns `ok:false`). Port signature changed from
        `Promise<void>` to `Promise<ActionResult>` — see decision log
- [x] `swipe` — `XCUICoordinate.press(forDuration:thenDragTo:)`. Duration
      param controls press time, not drag speed (XCUITest API limitation)
- [x] `longPressAt` — `XCUICoordinate.press(forDuration:)`
- [x] `listApps` — host-side `xcrun simctl listapps <UDID>` + `plutil
      -convert json`. No Swift command — simctl has direct access and
      the driver would just proxy
- [x] `typeText` — **promoted from Batch 3**. Native
      `XCUIApplication.typeText(_:)` is a 5-line Swift wrapper; no
      per-key tap orchestration needed for system keyboards. Custom
      in-app keyboards (Flutter / RN render their own key views)
      deferred to Batch 3 fallback path.
- [x] **Critical perf fix**: `DefaultXCUIBridge.dumpElements` rewritten
      to use `XCUIApplication.snapshot()` + local tree walk instead of
      `descendants(matching: .any)` + per-element property RPCs. End-to-end
      `getUiSummary` latency dropped from 10–20s → ~175ms (verified on
      iPhone 16 Pro Max iOS 18.3, 60-element tree). `XCUIElementSnapshot`
      does NOT expose `isHittable` — host adapter's `clickable`
      derivation now uses interactive-type whitelist only. Week 1
      finding #7 (hittable is a poor clickable proxy) effectively
      addressed as a side effect.
- [x] **Critical dead-path removal**: `DumpTreeCommand` no longer
      accepts a `bundleId` override arg — always uses
      `state.currentApp`. The override path silently returned 0
      elements because the fresh `XCUIApplication(bundleIdentifier:)`
      reference was unlaunched. Host adapter stops sending `bundleId`
      in dumpTree args and clears `lastLaunchedBundleId` cache on
      "no app launched" error (stale driver state recovery).
- [x] Swift unit tests: 11 existing + 13 new (forceStop 3, swipe 2,
      longPress 2, pressKey 3, screenshot 1, typeText 2). Total 24.
- [x] Smoke script covers a realistic flow: launch → tap search field
      → typeViaKeyboard → find result → tap result (verify nav) →
      pressKey back (verify return) → swipe → screenshot → cleanup.

Progress: **9 / 16 commands live** (4 from Week 2 + 7 from Batch 1).
`listApps` counts as a 6th adapter method even though it has no Swift
command.

**Batch 2 — selector resolution** ✅ PASS (2026-04-15). 4 commands:

- [x] `resolveSelector` — Snapshot-based priority chain
      (`resourceId` > `contentDesc` > `text` > `textContains` > `hint`)
      via iterative walk of `app.snapshot()`. iOS-native `predicate`
      escape hatch uses `XCUIElementQuery.matching(NSPredicate)`.
      `classChain` returns `.notFound` (Appium extension, not XCUITest
      native — may revisit if synapse requires).
- [x] `tap(selector)` — host-side composition:
      `resolveSelector` → `tapCoordinates` on midpoint. Returns
      `ActionResult` with `used: ${strategy}` reason. No Swift-side
      primitive needed; adapter composition lets the tool layer
      inspect resolved element before the gesture fires.
- [x] `inputText(selector, text)` — composition:
      `resolveSelector` → tap-to-focus → 250ms keyboard-animation
      wait → `typeText`.
- [x] `clearFocusedInput` — Swift `ClearFocusedInputCommand` types
      `XCUIKeyboardKey.delete.rawValue` N times (default 100, hard
      cap 500). Host adapter also wires this into `typeViaKeyboard`
      when `clearFirst: true`.
- [x] Extended `MockXCUIBridge` with `resolveBehavior` closure +
      `resolveCalls` recording. 6 new unit tests in
      `CommandHandlerUnitTests.swift`.
- [x] Port breaking change: `ResolvedElement.resolvedBy` enum extended
      with `"predicate"` and `"classChain"` for iOS escape hatches.

**Batch 3 — keyboard inspection** ✅ PASS (2026-04-15). 1 command:

- [x] `getKeyboard` — **snapshot-based walk**. Initial implementation
      used `app.keyboards.element(boundBy: 0).waitForExistence(timeout: 0.2)`
      which proved flaky against real apps — verified false negative
      against `inc.guide.kabuappStation.dev` where the keyboard WAS
      present in `app.snapshot()` but the XCUIElementQuery didn't
      match within the 200ms window. Rewritten as: one `app.snapshot()`
      RPC, walk iteratively for first `.keyboard` elementType, collect
      descendant `.key` / `.button` children. Reliable and matches
      the pattern of `dumpElements` / `resolveSelector`.
- [x] `detectKeyboardLayout` heuristic count-based (≥5 single-digit
      labels). Tolerates punctuation ("Delete", ".") in the key list
      which broke the earlier all-match rule.
- [x] `elementTypeName` switch expanded to 70+ `XCUIElement.ElementType`
      cases. Previously unknown values fell through to `"type20"` /
      `"type19"` strings which made tree dumps unreadable for iOS
      native keyboard elements.

**Custom-keyboard `typeText` fallback**: DEFERRED to Phase 6 hardening.
The original plan assumed Flutter banking apps would use custom
`GestureDetector` keypads drawn without `Semantics` annotations.
Against `inc.guide.kabuappStation.dev` (the original target fixture)
this assumption was FALSE — the app uses the iOS system keyboard and
native `XCUIApplication.typeText(_:)` works directly. Fallback
implementation requires a different fixture (a real custom-keypad
Flutter app) to validate. Tracked as Phase 6 work.

**Exit criteria for Phase 3**: All 16 commands pass end-to-end against
iOS simulator. Both smoke scripts (Settings-based and kabuappStation
probe) verify the wire protocol. Swift unit tests green (40+ tests).
TS type-check and `npm test` green (39/39).

### Phase 4 — Xcode matrix + CI ✅ SHIPPED (2026-04-15)

**Goal**: Automated coverage of the supported matrix.

- [x] GitHub Actions workflow `.github/workflows/ios.yml`:
  ```yaml
  strategy:
    fail-fast: false
    matrix:
      include:
        - { runner: macos-14, xcode: '15.4', ios: '17.5' }
        - { runner: macos-15, xcode: '16.2', ios: '18.2' }
        - { runner: macos-15, xcode: '16.4', ios: '18.4' }
  ```
- [x] GitHub Actions workflow `.github/workflows/ts.yml` — Node 20
      + `tsc --noEmit` + `npm test` with `ATOMYX_TOOL_TIMING=0`.
- [x] Weekly cron: `0 8 * * 1` (Monday 08:00 UTC) catches Apple
      drift from new Xcode betas / iOS release behavior changes
      before users hit it.
- [x] `workflow_dispatch` for manual runs from the Actions tab.
- [x] Path-scoped triggers: `ios/driver/**` +
      `src/adapters/ios-xctest.adapter.ts` + workflow file itself.
      TS-only changes don't trigger the iOS matrix.
- [x] Simulator bootstrap step: jq-based device discovery
      (no device model hard-coding), boot + 60s poll for `Booted`
      state, pass UDID into Makefile via `make setup UDID=...`.
- [x] Failure artifact upload: `native/ios-driver/build/Logs/Test/` +
      intermediates, 7-day retention.

**Conditional compilation** (`#if compiler(>=5.9)` /
`if #available(iOS 17, *)`) — NOT needed today. The Swift driver
uses only the stable XCUITest API subset; no version-specific APIs
are referenced. The scaffolding (`@unknown default` in
`elementTypeName`, defensive `try?` on `app.snapshot()`) handles
API drift gracefully. Revisit if Batch 4+ adds features that
genuinely differ across iOS versions.

**Release tagging** (`ios-driver-v0.1.0`) — deferred. Atomyx is not
yet published as a standalone package; tagging is tied to the
repo-level release story which is synapse-integration dependent.

**Exit criteria**: workflows drafted, path-scoped, matrix defined.
**Verification pending first push to GitHub** — workflows don't
run locally, so "SHIPPED" here means "code complete"; actual CI
green is verified when the next push fires them.

### Phase 4 supported matrix (published in `ios/README.md`)

| Runner | Xcode | iOS Sim | Status |
|---|---|---|---|
| macos-14 | 15.4 | 17.5 | primary |
| macos-15 | 16.2 | 18.2 | primary |
| macos-15 | 16.4 | 18.4 | latest |

Fail-fast disabled — all matrix combos run even if one fails,
so a single Xcode/iOS drift doesn't block the others.

### Phase 5 — Real device support ✅ SHIPPED (2026-04-15)

**Goal**: Runs on real iPhone/iPad, not just simulator.

- [x] **`iproxy` (libimobiledevice) transport** — adapter
      auto-spawns `iproxy <port> <port> <UDID>` on connect when
      targeting a physical device. Simulator path unchanged (shares
      host network namespace, TCP localhost works directly).
      Lifecycle tracked: `iproxyProc` killed on `dispose()`. Failure
      modes surfaced with clear errors:
      - `ENOENT` → "install libimobiledevice: brew install libimobiledevice"
      - Immediate exit → "device not trusted / port in use"
      - Post-startup exit → stderr log (tunnel broken warning)

- [x] **`DeviceInfo.kind`** field added to the port. Optional
      `"sim" | "device"`. Undefined on Android (only physical devices
      via adb). Drives the iproxy branch in `IosXctestController.connect`.

- [x] **Device enumeration** via libimobiledevice. `device-router.ts`
      calls `idevice_id -l` for UDIDs and `ideviceinfo -u <UDID> -k
      DeviceName` for model names. Silently skips if libimobiledevice
      isn't installed — simulator enumeration still works.

- [x] **Makefile device targets**: `setup-device`, `build-device`,
      `test-device`, `serve-device`. Require `DEVICE_UDID` +
      `DEV_TEAM` explicitly (no auto-detect — real-device UDIDs
      don't follow the sim UUID format). Pass code signing overrides
      to xcodebuild: `CODE_SIGN_STYLE=Automatic`,
      `CODE_SIGNING_REQUIRED=YES`, `DEVELOPMENT_TEAM=$(DEV_TEAM)`.
      `check-device` target fails fast with clear usage message if
      either var is missing.

- [x] **Docs**: `make help` lists device targets + prerequisites.
      `native/ios-driver/README.md` gets a new Phase 5 section (added
      alongside these code changes) with step-by-step real-device
      setup flow.

**NOT shipped** (scoped out — ship-as-you-go):
- `atomyx ios setup --device` CLI flag. The Makefile `setup-device`
  target covers the same ground; a dedicated CLI wrapper is polish,
  not functionality.
- CI real-device coverage — GitHub has no hosted iOS hardware
  runners. Phase 4 CI stays simulator-only. Manual maintainer
  verification when touching real-device code paths.

**Exit criteria**: contributor with Apple Developer account + a
paired iPhone/iPad can run Atomyx against a physical device following
only the documented setup flow.

### Phase 5 — known limitations on real device

- **First-run trust prompt**: iOS shows a "Trust This Developer"
  prompt the first time a manually-signed test bundle launches.
  User must tap it on the device. Tracked as a one-time-per-device
  manual step — cannot automate without jailbreak.
- **USB tether required**: iproxy uses usbmux which is USB-only.
  Wi-Fi debugging mode isn't supported yet. Phase 5+ could extend
  to `netmuxd` (Wi-Fi usbmux relay) but that's out of scope today.
- **First-run launch latency**: `xcodebuild test` on real device
  re-installs the test bundle each invocation (30–60s). Subsequent
  `test-without-building` calls against the same build are fast.
- **Simulator is still the development default.** Real device is
  for production validation / cross-platform confidence checks,
  not day-to-day iteration.

### Phase 6 — Hardening & polish (1–2 weeks)

**Goal**: Stability parity with Android.

- [ ] Crash recovery: driver auto-restart when XCTest bundle dies
- [ ] Stale binding detection analogous to Android accessibility stale
      binding; update `src/tools/preflight.ts` with iOS rebind hints
- [ ] Loading / transition classifier tuning for iOS animation patterns
      (springboard transitions differ from Android)
- [ ] `.claude/docs/pitfalls.md` — add iOS section with traps discovered during
      implementation
- [ ] Update playbook case studies for iOS-specific patterns

**Exit criteria**: iOS support merges into `main`. Announce in README and
GitHub release notes.

---

## Timeline

| Phase | Duration  | Cumulative |
|-------|-----------|------------|
| 0. RFC                            | 3–5 days  | ~1 week     |
| ~~1. idb prototype~~ (SKIPPED)    | —         | ~1 week     |
| 2. Swift skeleton + port validate | 2 weeks   | 3 weeks     |
| 3. Full commands                  | 2 weeks   | 5 weeks     |
| 4. CI matrix                      | 1 week    | 6 weeks     |
| 5. Real device                    | 1 week    | 7 weeks     |
| 6. Hardening                      | 1–2 weeks | **8–9 weeks** |

With multiple OSS contributors, Phases 3 and 4 can run in parallel,
compressing the calendar timeline to **5–6 weeks**.

---

## Non-skippable gates

1. **Phase 0 before Phase 2** — no Swift until RFC merges.
2. **Phase 2 week 1 before week 2** — no production structure until the
   port abstraction is validated against real XCUITest semantics via the
   minimal exploration prototype.
3. **Phase 3 before Phase 4** — no CI matrix until all commands work on
   one combo.
4. **Phase 4 before Phase 5** — no real device until CI is green on
   simulator.
5. **Phase 6 before merging to main** — no announcement until crash
   recovery works.

If a phase fails its exit criteria, **return to the previous phase**. Do
not skip forward. This is the rule that prevents a half-broken iOS path
from rotting in the codebase.

---

## Contributor roles

| Phase | Skills needed                              | Contributors |
|-------|--------------------------------------------|--------------|
| 0–1   | TypeScript + iOS semantics                 | 1 maintainer |
| 2–3   | Swift + XCTest + XCUITest experience       | 1–2 contributors |
| 4     | GitHub Actions + CI matrix                 | 1 contributor |
| 5–6   | iOS dev with Apple account + real device   | 1 contributor |

Recruit via GitHub discussion after Phase 0 RFC merges. Do not recruit
before — premature commitment makes the approach harder to adjust.

---

## What already exists in the repo

The TypeScript side is **platform-agnostic** and ready for the iOS
adapter:

- `src/adapters/device-controller.port.ts` — split interfaces (Inspector,
  Actor, AppManager, Lifecycle) with platform-neutral names (`appId`,
  `currentForeground()`, `InstalledApp`). iOS selector fields
  (`predicate`, `classChain`) already reserved.
- `src/adapters/device-router.ts` — routes `select_device` by platform;
  iOS branch is currently a stub.
- `src/adapters/ios-xctest.adapter.ts` — placeholder stub. Every method
  throws `not implemented`. Treat as the interface target, not as
  existing code to modify — Phase 2 will rewrite it from scratch.

Nothing in `src/runner/`, `src/explorer/`, `src/tools/`, or
`src/storage/` knows about platforms. They all talk to `DeviceController`.

---

## TODO markers in source

When iOS implementation begins, these markers show what needs to change.
Each is cross-referenced from the code:

- `src/adapters/ios-xctest.adapter.ts` — every `nope(...)` call. Phase 2.
- `src/adapters/device-controller.port.ts` — `TODO(ios)` above `Selector`
  interface (optional discriminated union by platform). Phase 3 optional.
- `src/adapters/device-controller.port.ts` — `TODO(ios)` next to
  `pressKey` (back button semantics). Phase 3 — implement as
  swipe-from-left-edge.
- `src/tools/preflight.ts` — `REBIND_HINTS.ios` points at this doc.
  Phase 6 — replace with real recovery commands.

---

## Prior art (read, don't copy)

- [Maestro iOS driver](https://github.com/mobile-dev-inc/Maestro) — the
  closest architectural precedent. Custom Swift XCTest runner, ships
  source, uses `XCTestBootstrap` for lifecycle.
- [WebDriverAgent](https://github.com/appium/WebDriverAgent) — reference
  for what NOT to do (we deliberately avoid its WebDriver semantics).
- [Facebook idb](https://github.com/facebook/idb) — for Phase 1
  prototyping and for `XCTestBootstrap` lifecycle patterns.
- Apple XCUITest documentation and WWDC sessions on UI testing.

Atomyx is Apache 2.0 — respect other projects' licenses when reading their
source. Do not copy code; read for design insight.

---

## Decision log

| Date       | Decision                                     | Rationale |
|------------|----------------------------------------------|-----------|
| 2026-04-15 | Custom Swift XCTest driver, Maestro-style    | Only option that matches port abstraction cleanly; OSS contributor model absorbs maintenance cost |
| 2026-04-15 | Ship source via SPM, not pre-built binaries  | Sidesteps Apple signing infra; user's Xcode handles version matrix |
| 2026-04-15 | iOS 16+ / Xcode 15+ minimum                  | Narrows support matrix to manageable size; XCUITest core API stable on this range |
| 2026-04-15 | Simulator first, real device Phase 5         | Unblocks Phases 2–4 without Apple Developer account requirement |
| 2026-04-15 | `pressKey("back")` = swipe-from-left-edge    | Cross-platform test specs should not break on iOS |
| 2026-04-15 | Drop Phase 1 (idb prototype); merge port validation into Phase 2 week 1 as Swift exploration | `idb_companion` binary (Aug 2022 build) hangs on CoreSimulator init with Xcode 16 / iOS 18.3; Meta deprecated idb in 2023; Python client requires pinned Python 3.12. Investing a week in an abandoned tool is the wrong trade. Swift exploration in Phase 2 week 1 achieves the same port validation without throwaway work in a dead codebase. |
| 2026-04-15 | Extend Week 1 scope from 2 to 3 commands (add `launchApp`) | XCUITest has no system-wide foreground query; `XCUIApplication()` without a bundleId is empty. `dumpTree` and `tapAt` cannot validate anything meaningful without a prior `launchApp`. Not scope creep — it's a prerequisite. |
| 2026-04-15 | iOS driver tracks `currentApp` state internally instead of requiring every inspect call to carry `appId` | Alternative was to force all `DeviceController` inspect methods to accept `appId`, which would downgrade Android's system-wide accessibility to match iOS's XCUITest limitation. Platform details stay in adapter; port stays neutral. Cross-app navigation (deep links, user swipes) will desync tracked state — documented as accepted iOS limitation for Phase 6 hardening. |
| 2026-04-15 | Wire protocol (`ping`, `launchApp`, `dumpTree`, `tapAt`) frozen as Week 2 baseline | All 4 commands exercised end-to-end against iPhone 16 Pro Max (iOS 18.3) + Xcode 16.2. Phase 2 Week 1 exit criteria all green. See "Week 1 findings" section for details. |
| 2026-04-15 | Evaluated reusing Maestro's iOS driver; rejected. Continue custom Swift driver. | Maestro's `maestro-driver-ios` overlaps ~80% with what Atomyx needs at the driver level and is battle-tested. However, Atomyx requires full control over wire protocol shape and command semantics for synapse integration — the downstream consumer has specific expectations Maestro's gRPC proto cannot satisfy without adapter-level translation that would leak into the tool layer. The 8-week custom path is justified by the integration requirement, not by technical superiority over Maestro at the primitive level. Revisit if synapse integration requirements change. |
| 2026-04-15 | Week 2 serving test selection via `xcodebuild -only-testing` instead of env-var gating | `SIMCTL_CHILD_ATOMYX_SERVE=1` does NOT pass through `xcodebuild test` to the test child process — `SIMCTL_CHILD_` prefix only works when `simctl` directly launches the process, which is not the case for xcodebuild's test runner. Initial Week 2 attempt with env-var gating silently skipped the serving test. Fix: remove the env guard and use `-only-testing:AtomyxDriverUITests/CommandHandlerUnitTests` for unit tests vs `-only-testing:AtomyxDriverUITests/AtomyxDriverUITests/testServeCommands` for serve mode. Class-level selection at the xcodebuild layer is cleaner than runtime gating anyway. |
| 2026-04-15 | Week 2 PASS. 3 commands (launchApp/dumpTree/tapAt + ping) + production adapter + unit tests + Makefile shipped. Phase 2 complete. | See Week 2 status section above for exit criteria details. Two Week 1 findings revised (#4 merge strategy not needed — `AmbiguityDetector` covers it; #6 id==label overlap is minority case not majority). New finding #10 about interactive-type empty wrappers added for Phase 3 tool-layer action. |
| 2026-04-15 | Phase 3 split into 3 batches instead of one big PR | Reduces review surface; each batch has its own smoke pass before the next starts. Batch 1 = coord-based + host-side (6 commands), Batch 2 = selector resolution (4 commands), Batch 3 = keyboard (2 commands). Matches the natural complexity gradient. |
| 2026-04-15 | `listApps` implemented host-side (not via driver) | `xcrun simctl listapps <UDID>` + `plutil -convert json` gives direct access from the host. Routing through the Swift driver would just proxy the same call. Keeps Swift driver focused on things that actually need XCUITest. |
| 2026-04-15 | Tool-layer execute time logging added at `ToolFactory.register()` | Single choke point for all tools (class-based and inline). Writes `[tool-timing] <name> <ms>ms <ok\|err>` to stderr. Opt out via `ATOMYX_TOOL_TIMING=0`. Goal: identify slow tools in production runs without per-tool instrumentation. |
| 2026-04-15 | Phase 3 Batch 1 PASS. 8/16 commands live. | screenshot, forceStopApp, swipe, longPressAt, pressKey, listApps — all end-to-end smoke green against iPhone 16 Pro Max iOS 18.3. Verified via smoke script: listApps returns 26 apps; launchApp tracks + forceStopApp clears state; screenshot writes 257KB PNG; tap navigates Settings→Siri; swipe/longPress/home dispatch cleanly. No regressions vs Week 2 baseline (60/47 elements). |
| 2026-04-15 | `DumpTreeCommand` dead `bundleId` override path removed; host adapter stops sending `bundleId` in dumpTree args | Post-Batch-1 review found the Swift code path for `XCUIApplication(bundleIdentifier:)` without `launch()` silently returns 0 elements — XCUITest queries against unlaunched refs are undefined. Adapter was forcing this path via its own `lastLaunchedBundleId` tracking. Fix: Swift always uses `state.currentApp`, adapter sends empty args, adapter clears stale `lastLaunchedBundleId` on "no app launched" error (recovery when driver process restarts). |
| 2026-04-15 | `dumpElements` rewritten to use `XCUIApplication.snapshot()` instead of `descendants(matching: .any)` + per-element property RPCs | User reported 10–20s latency for `getUiSummary` on Settings root (~60 elements). Root cause: XCUIElementQuery is lazy and every property access (`elementType`, `identifier`, `isHittable`, `frame`) triggers a separate RPC to the XCUITest daemon (remote process). Fix: `snapshot()` materializes the whole tree in one RPC; subsequent walk is local memory traversal. Measured: 10–20s → ~175ms (60× speedup). Trade-off: `XCUIElementSnapshot` does NOT expose `isHittable` (live-screen query). Host adapter's `clickable` derivation now uses interactive-type whitelist only — which effectively addresses Week 1 finding #7 (hittable was a poor proxy anyway). |
| 2026-04-15 | `typeText` command promoted from Batch 3 to Batch 1 | iOS native `XCUIApplication.typeText(_:)` handles system keyboard typing in one call — no per-key tap orchestration needed for `UIKeyboard`-backed inputs. 5-line Swift wrapper. Unblocks realistic smoke flow (tap search field → type → navigate). Custom in-app keyboards (Flutter, React Native) still need per-key tap fallback — deferred to Batch 3. Smoke test verified typing "Siri" into Settings search field produces filtered results. |
| 2026-04-15 | `DeviceActor.pressKey` return type changed from `Promise<void>` to `Promise<ActionResult>` (BREAKING) | Insight raised during Batch 1 smoke: iOS "back" is NOT a system primitive. Android OS dispatches a BACK intent regardless of app handling; iOS has only per-screen affordances (nav bar back button, modal Cancel/Done, swipe-from-edge, custom close buttons). The previous `void` return implicitly promised success, which was dishonest on iOS. New contract: iOS pressKey tries verifiable affordances first (`navigationBars.buttons[0]`), falls back to edge-swipe as best-effort, and reports `ok:false` with hint to fall back to `find_element` on Cancel/Done/Close/Back labels when nothing verifiable was used. Android adapter always returns `ok:true` (system intent guaranteed to fire). `PressKeyTool` result type updated. Port breaking change acceptable because synapse integration hasn't stabilized. Smoke verified: after Settings→Siri navigation, `pressKey("back")` returns `ok:true reason:"used: nav_bar_back"` and actually pops the stack. |
| 2026-04-15 | Phase 3 Batch 2 PASS. Selector resolution shipped via snapshot-based walk. | 4 commands: `resolveSelector`, `tap(selector)`, `inputText(selector, text)`, `clearFocusedInput`. Snapshot-based priority chain (`resourceId` > `contentDesc` > `text` > `textContains` > `hint`) via iterative walk — one `app.snapshot()` RPC, no per-element property round trips. iOS-native `predicate` escape hatch uses `XCUIElementQuery.matching(NSPredicate)` for cases the snapshot chain can't express. `classChain` returns notFound (Appium extension, not XCUITest native). `tap(selector)` and `inputText(selector)` implemented as host-side composition (resolve + coord tap) rather than Swift-side primitives — lets tool layer inspect resolved element before gesture fires. Port breaking change: `ResolvedElement.resolvedBy` enum extended with `"predicate"` and `"classChain"`. |
| 2026-04-15 | Phase 3 Batch 3 PASS. `getKeyboard` shipped. Custom-keypad fallback deferred. | Initial getKeyboard impl used `app.keyboards.element(boundBy: 0).waitForExistence(timeout: 0.2)` which proved flaky against kabuappStation (false negative despite keyboard being present in snapshot tree). Rewrote as snapshot walk: `app.snapshot()` → walk for first `.keyboard` elementType → collect `.key`/`.button` descendants. Same pattern as dumpElements/resolveSelector. `detectKeyboardLayout` heuristic rewritten count-based (≥5 single-digit labels) instead of all-match, which tolerates "Delete", ".", and other punctuation keys. |
| 2026-04-15 | `elementTypeName` switch expanded from ~25 to 70+ `XCUIElement.ElementType` cases | Discovery while debugging kabuappStation: iOS system keyboard keys are `elementType == .key` (raw value 20). The earlier incomplete switch fell through to `"type20"` in tree dumps, making keyboard elements unreadable to agents. Added all documented enum cases covering button/cell/group/alert/dialog/keyboard/key/textField/pickerWheel/webView/menu/datePicker/etc. `@unknown default` for forward compat when Apple adds new types in future Xcode releases. |
| 2026-04-15 | Custom-keypad `typeText` fallback DEFERRED (planned for Batch 3, dropped from Phase 3) | kabuappStation.dev discovery: this Flutter app uses iOS system keyboard, not a custom `GestureDetector` keypad. Native `typeText("123")` types successfully; the keyboard shows in `app.snapshot()` as `.keyboard` node. The planned fallback assumed Flutter banking apps would always use adversarial custom keypads — this was FALSE for the primary fixture. Fallback architecture still sound (host-side compose: `getKeyboard` check → `dumpTree` key scan → per-char tap) but can't be validated without a real custom-keypad app. Tracked as Phase 6 work when a fixture surfaces. |
| 2026-04-15 | Phase 3 COMPLETE. All 16 iOS commands live + 1 infrastructure (ping) + 2 host-side (listApps, currentForeground). | See Batch 1/2/3 status sections for per-batch details. Exit criteria met: full command surface shipped, both smoke scripts (Settings selector-flow + kabuappStation discovery probe) verified against iPhone 16 Pro Max iOS 18.3, Swift unit tests green (40+), TS tests green (39/39). Ready for Phase 4 (CI matrix) or Phase 6 (hardening) next. |
| 2026-04-15 | Phase 6 hardening partial: pitfalls.md iOS section + crash recovery + MCP integration smoke | 12 iOS traps documented in `.claude/docs/pitfalls.md` (process/state, queries/timing, gestures, enum, Flutter assumptions, architecture). Crash recovery: `connectionDead` flag, `handleDisconnect()` cleanup, `reconnect()` public method, actionable error in `call()` pre-check. Tool layer MCP integration verified via `scripts/ios-mcp-smoke.mjs` — end-to-end from MCP stdio → ToolFactory → Tool class → strategy (SelectorResolutionPipeline, AmbiguityDetector, UiTreeCache) → IosXctestController → Swift driver → iPhone sim. AmbiguityDetector + `(N×)` markers confirmed working cross-platform against iOS data shapes. Custom-keypad fallback deferred (still no valid fixture). |
| 2026-04-15 | Port `preflight()` helper branches on `ctl.platform === "ios"` — documented pragmatic exception | Android preflight heuristic (tree empty + foreground empty = stale binding) false-positives on every fresh iOS session because iOS has no system-wide foreground query — `getUiSummary` / `currentForeground` legitimately empty until `launchApp`. Alternative (per-adapter preflight method on the port) adds surface for a cross-cutting health check with a single use case. Pragmatic inline branch with rationale comment preferred. Platform branching stays inside this helper; tool layer strategies remain neutral. |
| 2026-04-15 | `getUiTree` (hierarchical) implemented via new `dumpRawTree` command | Discovered during MCP smoke: `StructuralInputFinder.collectAll` (Android-developed, cross-platform strategy) walks hierarchical `RawElement` trees. iOS adapter was throwing `nope("getUiTree")` because only flat `getUiSummary` was implemented. New Swift `DumpRawTreeCommand` returns nested dict via recursive `app.snapshot()` walk with `maxDepth=100` cap; TS adapter `getUiTree()` maps to `RawElement` shape. `elementType` string → `className` mapping critical so `find-input.ts#isEditText` substring match (`"textfield"` / `"securetextfield"`) works without iOS-specific branching. |
| 2026-04-15 | Phase 4 SHIPPED (code complete, CI verification pending first push) | `.github/workflows/ts.yml` (Node 20 + tsc + node:test) and `.github/workflows/ios.yml` (Xcode 15.4/16.2/16.4 × iOS 17.5/18.2/18.4 matrix, macos-14/15 runners, weekly cron, workflow_dispatch manual trigger, path-scoped triggers, jq-based sim discovery, failure artifact upload). Conditional compilation scaffolding not added — Swift driver uses only stable XCUITest API subset; no version-specific branching needed today. Revisit when features diverge across iOS versions. |
| 2026-04-15 | Phase 5 SHIPPED: real device support via iproxy tunneling | Adapter spawns `iproxy <port> <port> <UDID>` when `DeviceInfo.kind === "device"` (new optional field on port). Simulator path unchanged — sim shares host network namespace so TCP localhost works directly; physical device uses USB usbmux tunnel via libimobiledevice. `listAllDevices` enumerates via `idevice_id -l` + `ideviceinfo -k DeviceName` (silently skipped if libimobiledevice not installed, sim enumeration still works). Makefile `setup-device` / `build-device` / `test-device` / `serve-device` targets with `DEVICE_UDID` + `DEV_TEAM` required vars — no auto-detect for device UDID (format differs from sim UUID) or team id (security boundary). Code signing via `CODE_SIGN_STYLE=Automatic` + `DEVELOPMENT_TEAM` xcodebuild overrides. Host adapter `iproxy` lifecycle: spawn on connect, kill on dispose with SIGTERM, surface ENOENT → install hint + immediate-exit → trust/port diagnostic. |
| 2026-04-15 | `startIproxy` rewritten to eliminate port-conflict race | Field-found bug: when both simulator driver (via `make serve`) and device session were active, `select_device(<device-udid>)` silently routed MCP calls to the sim. Root cause: (1) iproxy failed to bind 127.0.0.1:22087 because sim driver was already there, (2) adapter's 300ms timeout-based "success" heuristic resolved BEFORE iproxy's `exit` event fired, (3) subsequent `waitForDriver` TCP connect hit the stale sim listener. Agent thought it was on device but launch_app etc. dispatched to sim. Fix: (a) `ensurePortFree()` probes 127.0.0.1:port via short TCP connect BEFORE spawning iproxy, throws actionable error with `pkill -f "xcodebuild.*AtomyxDriver"` hint if occupied; (b) `waitForTunnelUp()` polls for real tunnel connectivity with 5s deadline, not a timeout heuristic; (c) iproxy exit during startup becomes a rejected promise via `Promise.race` against the poll. Also surfaces a new class of error: "tunnel did not become reachable — did you run make serve-device?" when the device driver isn't actually running (common forgotten step; adapter can't start it remotely). Listed as pitfall in .claude/docs/pitfalls.md. |
| 2026-04-15 | `listApps` device path added via `xcrun devicectl device info apps --json-output -` | `simctl listapps` is sim-only. Device path uses Xcode 15+ `devicectl` JSON output, parses `result.apps[]` for `bundleIdentifier` + `name`. Adapter branches on `this.kind === "device"`. Fallback to `ideviceinstaller` not implemented — user is on Xcode 15+ per Phase 4 support matrix. |

| 2026-04-15 | `ensureVisible` scroll-into-view + obscurement detection added to host adapter | Real-device test on iOS 26 Settings surfaced two related gaps: (a) `tap(selector)` for items below the fold (`com.apple.settings.general` @214,896 on 932pt screen) resolved correctly but dispatched at AX coordinates that land in the home-indicator zone — silent miss. (b) `tap(selector)` for cells inside a UICollectionView sometimes dispatched on top of a transparent floating button that covered the target. Fix is composed: Swift `findObscurer` does a pre-order DFS z-order walk on the snapshot and returns the topmost containing node (with an ancestor-vs-obscurer disambiguation via reverse subtree walk, plus generic "Other"/"Group" with empty identifier/label suppressed). Host `ensureVisible` runs resolveSelector → isMidpointInViewport (with 60pt top / 50pt bottom safe-area insets) → swipe center-column in the computed direction (element below → finger up, above → finger down) → re-resolve. Hard budget: 8 iterations, ≤60% screen-height per swipe, progress check via bounds equality. `prepareSelectorForAction` threads both scroll and obscurement through `tap(selector)` and `inputText(selector)`. Obscurement propagates through the port as `ResolvedElement.obscuredBy: { role, identifier, label }` so cross-platform callers can surface the same structured error. |
| 2026-04-15 | Lazy per-session `screenSize` cache on `IosXctestController` | `ensureVisible` needs the app frame to run `isMidpointInViewport`. Calling the new `getScreenSize` command on every tap doubles the RPC count per gesture for no benefit — the frame only changes on app lifecycle transitions (rotation aside, which we document as a known limitation). Field-cached with invalidation on `launchApp`, `forceStopApp` (matching bundle), `reconnect`, and `handleDisconnect`. First `ensureVisible` after a lifecycle event pays one RPC; subsequent taps reuse. |
| 2026-04-15 | Obscurement "role=other identifier='' label=''" false positive fix shipped after real-device retest | First landing of `findObscurer` reported `[role=other identifier="" label=""]` for `com.apple.settings.general` on Settings root, blocking selector taps end-to-end. Root cause: pre-order DFS "last containing node" can legitimately return the target's own ancestor (UICollectionView / unstyled "Other" wrapper) because ancestors always contain any point their descendants contain. Fix composed of two guards: (1) after picking topmost, run a subtree walk from topmost looking for the target by reference equality — if reachable, topmost IS an ancestor, return nil (not obscured); (2) suppress when topmost is `other`/`group` with empty identifier AND empty label — generic anonymous containers are almost never real rendering blockers, and real modals/sheets/alerts always carry distinctive elementType OR non-empty identifier/label. Verified on iOS 26 real device: Settings General/Accessibility/WiFi selector taps now pass through without false-positive obscurement. |
| 2026-04-15 | Safe-area aware `isMidpointInViewport` (60pt top / 50pt bottom inset) | iPhone 12 Pro Max has `app.frame.height = 932pt`, so General row at AX midY=896 passed naive `cy < screen.height` check but landed inside the home-indicator zone — tap dispatched, no hit. Fix: inset the viewport rect before the midpoint check. Chose 60pt top (status bar 44pt + NavigationBar large-title buffer) and 50pt bottom (home indicator 34pt + TabBar buffer). Cost: at most one extra scroll iteration per legitimate edge case; ensureVisible re-resolves per iteration so the loop self-terminates when the element settles. Horizontal inset left at 0 — horizontal scroll is rare and over-inset would force unnecessary drags. |
| 2026-04-15 | `startIproxy` probes existing listener with a `ping` handshake before refusing | Second-session bug: a contributor already running `make serve-device` (iproxy alive on 22087) calls `select_device(<device-udid>)` again — adapter's `ensurePortFree` sees the occupied port and throws the "likely a simulator driver collision" error, steering the user toward `pkill -f xcodebuild` which kills their own device tunnel. Fix: when port is occupied, open a throwaway socket, write `{"id":0,"type":"ping","args":{}}\n`, and check for `{ok:true,data:{pong:true}}`. If match → existing listener IS an Atomyx driver, reuse it and skip iproxy spawn. If not → fall back to the original refusal path for genuine sim/stale collisions. Caveat documented: probe cannot verify the UDID the existing tunnel is bound to, so switching between two physical devices still requires explicit `dispose()` — the probe optimizes the single-device reselection path which was the reported friction. |
| 2026-04-15 | Scroll-search Phase 0 added to `ensureVisible` for virtualized lists | Second-round real-device test: after Back navigation left Settings root scrolled to the bottom, `tap(selector)` on `primaryAppleAccount` (logically at list start) failed because the cell was recycled off-screen and absent from `app.snapshot()` entirely — `resolveSelector` returned `found:false`, `ensureVisible` bailed immediately without attempting to scroll. Fix: when initial resolve returns not-found, run `scrollSearchForSelector` which swipes the screen center column UP for up to 6 iterations (most anchor items live near list start, covering the common "Back leaves list at bottom" case), then DOWN for up to 6 iterations (for lists starting at the top with far-down targets), re-resolving after each swipe. First hit hands off to the positional scroll loop for fine-grained centering. Budget intentionally small: this is a fallback, not the primary workflow, and the canonical orient-before-acting pattern (`get_ui_tree` → inspect → act) should remain the agent's default. |
| 2026-04-15 | `searchField` added to `isEditText` whitelist | `find-input.ts#isEditText` substring-matches `className` against `{edittext, textfield, textinput, securetextfield, editable}`. iOS `XCUIElementTypeSearchField` serializes (via adapter `elementType` → `className` mapping) as `"searchField"` — which does NOT contain `"textfield"` (`"searchfield".includes("textfield") === false`). Result: `find_element(inputField:true)` returned `found:false` and `input_text(selector)` failed to locate the editable on any iOS search UI (Settings search, App Store search, etc.). Added `searchfield` to the whitelist. Any new iOS editable elementType that surfaces in the field should join the same list — the whitelist is still the cheapest cross-platform strategy. |

Append new decisions here when the plan evolves.
