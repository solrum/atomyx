# iOS driver internals

## Purpose

For contributors or AI agents editing the iOS code path — host-side
TypeScript adapter under `packages/ios-driver/`, Swift XCUITest runner
under `platforms/ios-agent/`, or any tool/strategy that has to reason
about iOS-specific behavior.

Read before: editing any file in those two trees, debugging an iOS
smoke failure, or adding a capability that must work on iOS.

This doc does NOT cover: how to run the runner (see
`platforms/ios-agent/README.md`), the cross-platform tool contract
(see `tools.md`), or the architectural contract (see
`architecture.md`). Known traps are in `pitfalls.md`.

## Architecture

iOS support is split across two codebases that talk over TCP:

```
┌──────────────────────────────────────────────────────────────┐
│ Host (Node)                                                  │
│                                                              │
│  @atomyx/mcp tools ──► Orchestra (@atomyx/driver)            │
│                          │                                   │
│                          ▼                                   │
│                       IosDriver  ── tree-normalizer          │
│                          │                                   │
│              ┌───────────┼───────────┐                       │
│              ▼           ▼           ▼                       │
│         XctestLauncher  Iproxy    TcpClient                  │
│         (sim only)    (dev only)                             │
└─────────────────────────────────────┼────────────────────────┘
                                      │ 127.0.0.1:22087
                                      │ line-delimited JSON
                                      ▼
┌──────────────────────────────────────────────────────────────┐
│ Device / simulator (XCUITest process)                        │
│                                                              │
│   AtomyxDriverAgent ──► CommandServer ──► XCUIBridge         │
│                              │                 │             │
│                              ▼                 ▼             │
│                         Commands/*       XCUIApplication     │
└──────────────────────────────────────────────────────────────┘
```

Host-side files under `packages/ios-driver/src/`:

| File | Responsibility |
|---|---|
| `ios.driver.ts` | `IosDriver` class — implements the `Driver` port by mapping each method to a wire command |
| `tcp-client.ts` | JSON request/response framing over TCP |
| `iproxy.ts` | `iproxy` child-process lifecycle for physical devices |
| `xctest-launcher.ts` | Reuse-or-spawn `xcodebuild test-without-building` for simulator mode |
| `tree-normalizer.ts` | Swift `dumpRawTree` output → canonical `TreeNode` |

Runner-side files under `platforms/ios-agent/Tests/`:

| Path | Responsibility |
|---|---|
| `AtomyxDriverAgent.swift` | XCTestCase entry, registers commands, blocks on `RunLoop.main.run()`, picks the synthesizer via `EventSynthesizerFactory` |
| `Server/CommandServer.swift`, `Server/WireProtocol.swift` | TCP listener + JSON types |
| `Bridge/XCUIBridge.swift` | Protocol wrapping `XCUIApplication` for tree / app-lifecycle operations |
| `Bridge/ElementTypeName.swift` | `XCUIElement.ElementType` → human-readable string |
| `Bridge/EventSynthesizer.swift` | Protocol + data types (`PointerPath`, `Waypoint`, `EventCapabilities`, `SynthesizerError`) |
| `Bridge/PublicEventSynthesizer.swift` | `XCUICoordinate`-only impl; single-pointer tap / long-press / drag. `PointerPatternClassifier` is pure — unit-testable. |
| `Bridge/PrivateEventSynthesizer.swift` | **Only file that touches private XCTest symbols.** `XCSynthesizedEventRecord` / `XCPointerEventPath` / `XCTRunnerDaemonSession` via `NSClassFromString`. Dry-run probe at init. |
| `Bridge/EventSynthesizerFactory.swift` | Picks public or private based on `ATOMYX_IOS_SYNTHESIZER` env var |
| `Commands/*` | One file per wire command |
| `Commands/DispatchPointerCommand.swift` | Accepts raw waypoint list; routes through active synthesizer |
| `PressKey/*` | `pressKey` strategy chain (nav bar → edge swipe → keyboard fallback) |
| `CommandHandlerUnitTests.swift` | Runs without a simulator via `MockXCUIBridge` + classifier tests |

## Host-side scope

The driver deliberately does NOT do selector resolution, scroll-into-
view, obscurement detection, priority broadening, or ambiguity
reporting. All of those live in `@atomyx/driver` and operate on the
canonical `TreeNode` produced by `tree-normalizer.ts`. The Swift
runner never sees a `Selector`; it only receives coordinates.

That leaves the iOS driver with four concerns: transport, `iproxy`
lifecycle, tree normalization, and a 1:1 mapping from `Driver`
interface methods to Swift commands.

## Command contract

Each wire command maps to exactly one `Driver` method (or is internal
infrastructure). The full list is in `platforms/ios-agent/README.md`.
Selected mappings below for quick reference:

| `Driver` method | Wire command | Swift handler | Notes |
|---|---|---|---|
| `connect()` / `reconnect()` | `ping` | `PingCommand.swift` | Liveness handshake before every `reconnect` |
| `hierarchy()` | `dumpRawTree` | `DumpRawTreeCommand.swift` | Nested snapshot walk, maxDepth 100 |
| `tap()` | `tapAt` | `TapAtCommand.swift` | Always coordinates; selector tap composes host-side |
| `longPress()` | `longPressAt` | `LongPressAtCommand.swift` | |
| `swipe()` | `swipe` | `SwipeCommand.swift` | `press(forDuration:thenDragTo:)` |
| `launchApp()` / `stopApp()` | `launchApp` / `forceStopApp` | `LaunchAppCommand.swift`, `ForceStopAppCommand.swift` | Updates tracked `currentApp` |
| `currentForeground()` | (host-only) | — | Cached `lastLaunchedBundleId`; see quirks |
| `inputText()` | `typeText` | `TypeTextCommand.swift` | Native `XCUIApplication.typeText`; requires focused field |
| `eraseText()` / clear | `clearFocusedInput` | `ClearFocusedInputCommand.swift` | ⌘A + ⌫ fast path, delete-loop fallback |
| `pressKey()` | `pressKey` | `PressKeyCommand.swift` + `PressKey/*` | Returns `KeyResult`; may be `ok:false` on iOS |
| `hideKeyboard()` | `hideKeyboard` | `HideKeyboardCommand.swift` | `KeyResult`; backed by the `canHideKeyboard` capability |
| `screenshot()` | `screenshot` | `ScreenshotCommand.swift` | PNG base64 |
| `screenSize()` | `getScreenSize` | `GetScreenSizeCommand.swift` | `app.frame` in points; host-cached per session |
| `listApps()` | (host-only) | — | `xcrun simctl listapps` or `xcrun devicectl`, branches on `kind` |

## Platform-specific quirks

Behaviors that affect contributors working on iOS. These are
present-tense traits of the platform, not changelog entries.

**No system-wide foreground query.** `XCUIApplication()` without a
bundle id returns the test target, not whatever is on screen. The
driver tracks `currentApp` internally (set by `launchApp`, cleared by
`forceStopApp` / `reconnect` / `handleDisconnect`). Cross-app
navigation via deep link or user gesture desyncs this state — the
next `getUiSummary` / `currentForeground` returns whatever the driver
last launched, not what the user sees.

**`pressKey` is not a system primitive.** Android dispatches a BACK
intent regardless of app handling; iOS only has per-screen
affordances. The strategy chain under `PressKey/` tries verifiable
paths first (`navigationBars.buttons[0]`) and returns `ok:true` with
a `used:` reason. When nothing verifiable is available it falls back
to edge swipe / keyboard and returns `ok:false` with a hint to try
`find_element` on Cancel / Done / Close / Back labels. Android always
returns `ok:true`.

**`XCUIElementSnapshot` does not expose `isHittable`.**
`DumpRawTreeCommand` builds the tree from a single `app.snapshot()`
rather than lazy queries, which is much faster but means the live-
only `isHittable` property is unavailable. The host-side `clickable`
derivation in `tree-normalizer.ts` uses an interactive-type whitelist
(`button`, `cell`, `searchField`, `textField`, etc.) instead.

**Bounds are in points and include safe-area insets.** A
`getUiSummary` on an iPhone 16 Pro Max reports `right=440, bottom=956`
while the logical spec is 430×932 — the status bar and home indicator
are included in the root frame. Any tool that computes "middle of
screen" must not assume the reported bounds equal the nominal device
size. The `ensureVisible` viewport check uses a 60 pt top / 50 pt
bottom inset for this reason.

**Anonymous containers cause false-positive obscurement.** Obscurement
detection runs host-side in `packages/driver/src/obscurement/
obscurement.ts` against the canonical tree emitted by the iOS
adapter. A naive "topmost element containing the target point" walk
returns the target's own ancestor (UICollectionView, generic `Other`
wrapper). Two guards in `detectObscurement`: (1) walk the subtree
from the candidate obscurer looking for the target by reference — if
found, the candidate is an ancestor and does not obscure; (2)
suppress `other` / `group` elements with empty identifier AND empty
label. Both are cross-platform but iOS is the primary trigger.

**⌘A + ⌫ on simulator requires hardware keyboard pairing.** The
clear fast path sends a keyboard shortcut; on a simulator without
hardware-keyboard pairing enabled the shortcut is silently ignored.
Enable via `xcrun simctl keyboard <udid> enable hardware` or through
Simulator I/O → Keyboard. The delete-loop fallback covers the miss.

**`XCUIElement.ElementType` enum needs explicit mapping.** Raw values
without a name render as `type20`, `type19`, etc. in tree dumps —
unreadable to agents doing role filtering. `ElementTypeName.swift`
maps 70+ documented cases and falls through to `type\(rawValue)` via
`@unknown default` for forward compatibility.

**`searchField` is an editable element type.** A substring match
against `"textfield"` alone misses iOS search UIs (Settings search,
App Store search). Any cross-platform "find an input field"
heuristic must include `searchfield` alongside `textfield`,
`securetextfield`, and Android equivalents.

## Lifecycle edge cases

**Reconnect verifies via ping before returning.** `IosDriver.reconnect`
reconnects the TCP client and then issues a `ping`. If the ping times
out or returns malformed data, `reconnect` throws with an actionable
message — a silently-stale binding is strictly worse than a hard
failure.

**Screen size is cached per session.** `ensureVisible` needs
`app.frame` for viewport checks; calling `getScreenSize` on every tap
doubles the RPC count. The cache is invalidated on `launchApp`,
matching `forceStopApp`, `reconnect`, and `handleDisconnect`. Device
rotation is NOT an invalidation trigger and is a known limitation.

**`Iproxy.start` pings before refusing an occupied port.** When a
contributor already has `make serve-device` running on 22087,
reselecting the device should reuse the existing tunnel. `iproxy.ts`
probes the occupied port with a `ping` handshake; if an Atomyx driver
responds it skips spawning a new `iproxy`. If the response is
unexpected it falls back to the "port in use — kill the other driver"
error path.

**`XctestLauncher` is reuse-first.** For `kind: "simulator"`, if the
port already has an Atomyx driver responding to `ping`, the launcher
skips `xcodebuild` entirely — covering the `make serve` workflow.
Otherwise it spawns `xcodebuild test-without-building` as a non-
detached child and kills it on Node exit.

## Limitations

- **USB tether only** for physical devices. `iproxy` uses usbmux; Wi-Fi
  debugging is not supported.
- **First-run trust prompt** on physical devices: iOS shows "Trust This
  Developer" the first time a manually-signed test bundle launches.
  Must be tapped on the device; cannot be automated.
- **Cross-app navigation desync** — see `currentApp` tracking quirk.
- **Device rotation does not invalidate the screen size cache.**
- **No tvOS / watchOS / visionOS.** Different XCUITest flavors; out
  of scope.

## Extension points

**Adding a new Swift command:**

1. Create `platforms/ios-agent/Tests/Commands/<Name>Command.swift`
   implementing `CommandHandler`.
2. Register it in `AtomyxDriverAgent.swift`'s command registry.
3. Add a matching `call(...)` in `packages/ios-driver/src/ios.driver.ts`.
4. If it affects a `Driver` port method, update the port in
   `packages/driver/src/driver/driver.port.ts` and implement the
   same shape in `packages/android-driver/src/android.driver.ts`.
5. Unit-test the handler against `MockXCUIBridge` in
   `CommandHandlerUnitTests.swift`.

**Adding a new press-key strategy:** drop a file into
`platforms/ios-agent/Tests/PressKey/` implementing `PressKeyStrategy`
and register it in `PressKeyCommand`. The chain short-circuits on the
first strategy that returns a verifiable hit.

**Adding a new bridge implementation:** `XCUIBridge` is a protocol;
alternate implementations (e.g. for a future XCUI API divergence
across Xcode versions) go under `Bridge/` alongside
`DefaultXCUIBridge`. The command registry takes a bridge via
injection — no command code changes.

## Gesture dispatch architecture

Every gesture — tap, long-press, drag, press-and-drag, flick,
pinch, rotate, multi-finger — flows through `EventSynthesizer`.
Two concrete implementations back the protocol:

| Impl | Uses | Covers | Drift risk |
|---|---|---|---|
| `PublicEventSynthesizer` | `XCUICoordinate.tap()` / `press(forDuration:)` / `press(forDuration:thenDragTo:)` | Single-pointer tap, long-press, drag, press-and-drag | None — stable public subset |
| `PrivateEventSynthesizer` | `XCSynthesizedEventRecord` + `XCPointerEventPath` + `XCTRunnerDaemonSession` via `NSClassFromString` | Full W3C Actions (multi-pointer, pressure, multi-waypoint bezier) | Per-Xcode-major; caught by dry-run probe at init |

Selection is controlled at runtime by the
`ATOMYX_IOS_SYNTHESIZER` environment variable:

| Value | Behaviour |
|---|---|
| `auto` (default) | Probe then fall back. Private if probe passes, public otherwise. |
| `public` | Skip probe, force `PublicEventSynthesizer`. Incident downgrade for users; regression test path for contributors. |
| `private` | Probe then **throw** if unavailable. CI's private-path smoke uses this so a failing probe never hides behind a silent public downgrade. |

Invalid env values fall back to `auto` with a stderr warning.

**Probe contract** — `PrivateEventSynthesizer.isAvailable`
runs a full dry-run dispatch, not just class / selector
existence checks. Apple's most common drift pattern is to
keep names stable while changing behavior; existence checks
pass on that, dry-run does not. On failure the probe records
a line-by-line log in `lastProbeLog` so contributor support
and CI can pinpoint exactly which class or selector drifted.
The `ping` response surfaces `synthesizer` and `probeLog` so
host-side diagnostics inherit the log for free.

**Capability propagation** — the ping response carries
`capabilities: { canMultiPointer, canPressure, canBezierPath }`;
`IosDriver.connect()` reads these and publishes them on
`Driver.capabilities`. The YAML validator in `@atomyx/script`
consults them before dispatch so scripts that need multi-
pointer or pressure on a public-only driver fail with
`POINTER_MULTI_NOT_SUPPORTED` / `POINTER_PRESSURE_NOT_SUPPORTED`
at validation time — not after a half-executed gesture.

**Rollback on drift** — three tiers, lowest impact first:

1. User-side: set `ATOMYX_IOS_SYNTHESIZER=public` on the
   runner. Multi-pointer / pressure scripts fail fast with a
   clear capability error; single-pointer continues unchanged.
2. Patch release: hardcode `PrivateEventSynthesizer.probe()
   → false` for the affected Xcode version.
3. Revert: delete `PrivateEventSynthesizer.swift`. Single-
   pointer remains fully functional via `PublicEventSynthesizer`.

## References

- `platforms/ios-agent/README.md` — runner setup, Makefile targets,
  command list, contributor prerequisites.
- `pitfalls.md` — iOS traps to avoid when editing the adapter.
- `tools.md` — cross-platform tool contract the driver plugs into.
- `architecture.md` — why iOS uses an XCUITest runner rather than a
  device-side HTTP server (see "Non-negotiable rules" in `CLAUDE.md`).
