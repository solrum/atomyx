# iOS driver internals

## Purpose

For contributors or AI agents editing the iOS code path: the Go host
adapters under `core/internal/iosdriver` and `core/internal/iossimdriver`,
or the Swift XCUITest runner under `platforms/ios-agent`.

Read this before editing any of those trees, debugging an iOS
failure, or adding a capability that must work on iOS.

This doc does NOT cover: the cross-platform `Driver` contract itself
(see `architecture.md`), or the cross-platform tool surface (see
`tools.md`). Known traps are in `pitfalls.md`.

## Architecture

iOS support is split across two codebases that talk over TCP, line-
delimited JSON, on `127.0.0.1:22087`:

```
Go core
  IosDriver (core/internal/iosdriver) ── implements driver.Driver
      │
      ▼
  Conn (core/internal/transport.TCPClient)
      │ simulator: direct loopback
      │ device: iproxy USB tunnel
      ▼
Simulator or device (Swift XCUITest process, bundle id
dev.atomyx.driver.host)
  CommandServer ── CommandRegistry ── one CommandHandler per
                                        wire command
      │
      ▼
  XCUIBridge (DefaultXCUIBridge, wraps XCUIApplication) plus
  EventSynthesizer (gesture dispatch)
```

`IosSimDriver` in `core/internal/iossimdriver` is a second, opt-in
adapter for simulator targets only. It routes `Tap`, `LongPress`,
`Swipe`, and the streaming touch phases through a direct-HID
WebSocket connection to an external `atomyx-sim-hid` helper process
(wrapping Apple's private `SimDeviceLegacyHIDClient` framework),
while every other `Driver` method, including `Hierarchy`,
`Screenshot`, app lifecycle, and `DispatchGesture` itself, delegates
to an embedded `iosdriver.IosDriver`. The device factory only builds
an `IosSimDriver` when the host opts in (`ATOMYX_SIM_HID=1`), the
host passes the capability floor (`IsSimDirectSupported` in
`core/internal/iossimdriver`: arm64 plus Xcode 16 or newer), and
starting the helper succeeds. Any other case falls back to the
XCUITest path in `IosDriver` alone. Multi-pointer gestures always go
through `IosDriver`, because the HID helper has no coordinated
multi-pointer sequencing.

Host-side files under `core/internal/iosdriver`:

| File | Responsibility |
|---|---|
| `core/internal/iosdriver/driver.go` | `IosDriver`, implementing `driver.Driver` by mapping each port method to a wire command |
| `core/internal/iosdriver/transport.go` | The `Conn` interface `IosDriver` depends on |
| `core/internal/iosdriver/iproxy.go` | `iproxy` USB tunnel lifecycle for physical devices |
| `core/internal/iosdriver/xctest_launcher.go` | Reuse-or-spawn `xcodebuild test-without-building` to bring the runner up |
| `core/internal/iosdriver/normalize.go` | Swift `dumpRawTree` output maps to the canonical `wire.TreeNodeWire` |
| `core/internal/iosdriver/apps.go` | App lifecycle (`ListApps`, `InstallApp`, `UninstallApp`, `ClearAppData`, `IsAppInstalled`), all host-only via `xcrun simctl` or `xcrun devicectl` |

Runner-side files under `platforms/ios-agent/Tests`:

| Path | Responsibility |
|---|---|
| `platforms/ios-agent/Tests/AtomyxDriverAgent.swift` | XCTestCase entry point. Registers every command, picks the gesture synthesizer, starts `CommandServer`, blocks on `RunLoop.main.run()` |
| `platforms/ios-agent/Tests/Server/CommandServer.swift`, `platforms/ios-agent/Tests/Server/WireProtocol.swift` | TCP listener plus the JSON request and response types |
| `platforms/ios-agent/Tests/Bridge/XCUIBridge.swift` | Protocol wrapping `XCUIApplication` for tree and app-lifecycle operations. `DefaultXCUIBridge` is the concrete implementation |
| `platforms/ios-agent/Tests/Bridge/EventSynthesizer.swift` | Protocol plus shared types (`PointerPath`, waypoint phases, `EventCapabilities`, `SynthesizerError`) |
| `platforms/ios-agent/Tests/Bridge/CoordinateSynthesizer.swift` | `XCUICoordinate`-only implementation: single-pointer tap, long-press, drag |
| `platforms/ios-agent/Tests/Bridge/EventRecordSynthesizer.swift` | **Only file that touches private XCTest symbols** (`XCSynthesizedEventRecord`, `XCPointerEventPath`, `XCTRunnerDaemonSession`, all via `NSClassFromString`). Full multi-pointer and pressure support. Probes itself at load time. |
| `platforms/ios-agent/Tests/Bridge/EventSynthesizerFactory.swift` | Picks `EventRecordSynthesizer` when its probe passes, `CoordinateSynthesizer` otherwise. Selection is fully internal, with no caller-facing knob. |
| `platforms/ios-agent/Tests/Commands` (directory) | One `CommandHandler` per wire command |
| `platforms/ios-agent/Tests/Commands/DispatchPointerCommand.swift` | Accepts a raw waypoint list, routes it through the active synthesizer |
| `platforms/ios-agent/Tests/PressKey` (directory) | `pressKey` strategy chain (nav bar back, then edge swipe, then a fallback strategy) |
| `platforms/ios-agent/Tests/Commands/Clear`, `platforms/ios-agent/Tests/Commands/Clear/Strategies` (directories) | `clearFocusedInput` strategy chain |
| `platforms/ios-agent/Tests/CommandHandlerUnitTests.swift` | Runs without a simulator via a mock bridge plus classifier unit tests |

## Host-side scope

Both Go adapters deliberately do not resolve selectors, scroll into
view, detect obscurement, or broaden selector priority. All of that
lives in the composition layer above `driver.Driver`, operating on
the canonical `wire.TreeNodeWire` `Hierarchy` produces. The Swift
runner never sees a selector: it only receives coordinates and, for
`DispatchGesture`, raw waypoints.

## Command surface

| Driver port method | Wire command | Swift handler | Notes |
|---|---|---|---|
| `Connect()` / `Ping()` | `ping` | `PingCommand` | Populates `CanMultiPointer` and `CanPressure` from the active synthesizer's capabilities. |
| `Hierarchy()` | `dumpRawTree` | `DumpRawTreeCommand` | Builds the tree from one `app.snapshot()` call, walked iteratively. |
| `Tap()` | `tapAt` | `TapAtCommand` | Always coordinates. Selector-based tap composes host-side. |
| `LongPress()` | `longPressAt` | `LongPressAtCommand` | |
| `Swipe()` | `swipe` | `SwipeCommand` | |
| `DispatchGesture()` | `dispatchPointer` | `DispatchPointerCommand` | Routes through the active `EventSynthesizer`. Rejects multi-pointer or pressure waypoints host-side first when the runner's capabilities don't support them. |
| `LaunchApp()` | `launchApp` | `LaunchAppCommand` | Updates `DriverState.currentApp` / `currentBundleId`. |
| `StopApp()` / `KillApp()` | `forceStopApp` | `ForceStopAppCommand` | Clears the tracked `currentApp` when it matches. |
| `InputText()` | `typeText` | `TypeTextCommand` | Native `XCUIApplication.typeText`. Requires a focused field. |
| `EraseText()` | `clearFocusedInput` | `ClearFocusedInputCommand` | 4-strategy chain, see quirks below. |
| `PressKey()` | `pressKey` | `PressKeyCommand` plus `PressKey/*` | Returns whether a verifiable affordance was used. |
| `HideKeyboard()` | `hideKeyboard` | `HideKeyboardCommand` | |
| `Screenshot()` | `screenshot` | `ScreenshotCommand` | PNG, base64-encoded. |
| `ScreenSize()` | `getScreenSize` | `GetScreenSizeCommand` | Points, not pixels. Falls back to the Springboard proxy's frame when no app is tracked. |
| `CurrentForeground()` | none | none | Always returns a typed unsupported error, see quirks below. |
| `ListApps()` / `InstallApp()` / `UninstallApp()` / `ClearAppData()` / `IsAppInstalled()` | none (host-only) | none | `xcrun simctl` (simulator) or `xcrun devicectl` (device), invoked directly by the Go adapter. |
| `WaitForIdle()` | none | none | Always reports `true` without waiting, so a caller must fall back to tree-diff polling. |

## Setup and run

**Simulator** (zero configuration):

```
cd platforms/ios-agent
make setup   # xcodegen + xcodebuild build-for-testing
make serve   # runs the driver, blocks the terminal
```

`make setup` auto-detects the first booted simulator's UDID.
`iosdriver.XctestLauncher` (`core/internal/iosdriver/xctest_launcher.go`)
is reuse-first: when a driver is already answering `ping` on the
target port, it skips `xcodebuild` entirely. Otherwise it spawns
`xcodebuild test-without-building` as a background child process
(`process.BackgroundRunner`), which stays up until the launcher's kill
function stops it.

**Physical device**:

```
cd platforms/ios-agent
cp device.env.example device.env   # one-time: copy, then edit
make setup-device
make serve-device
```

`device.env` carries a contributor's personal signing identity
(`DEVICE_UDID`, `DEV_TEAM`, `BUNDLE_ID`) and is loaded only for the
device-specific Make goals (`build-device`, `install-device`,
`test-device`, `serve-device`, `setup-device`, `check-device`). The
simulator goals never read it, so a simulator build never leaks a
contributor's signing identity. Prerequisites: `brew install
libimobiledevice` for `iproxy`, an Xcode account under Settings then
Accounts, and the device paired and trusted over USB.

On the Go side, `DriverProvisioner.EnsureIOSAgent` in
`core/internal/device` resolves the project directory from an
explicit option, then `ATOMYX_IOS_DRIVER_DIR`, then an upward search
for a `platforms/ios-agent` directory from the running binary and the
working directory. Device builds read their signing settings from
the environment, mirroring `device.env` for a manual `make`
invocation:

- `ATOMYX_DEV_TEAM` supplies the signing team and is required.
- `ATOMYX_CODE_SIGN_STYLE` is `Automatic` (the default when unset) or
  `Manual`.
- `ATOMYX_PROVISIONING_PROFILE_SPECIFIER` names the provisioning
  profile and is required when the style is `Manual`. A `Manual`
  style without it fails with a signing error before `xcodebuild`
  runs.

`IosDriver.Connect` (`core/internal/iosdriver/driver.go`) on a
physical device first launches an `iproxy` USB tunnel as a background child process
and waits for it to become reachable before dialing. `Disconnect` stops
the tunnel. On a simulator it dials
`127.0.0.1:22087` directly, because the simulator shares the host's
network namespace.

## Platform-specific quirks

**No system-wide foreground query.** `XCUIApplication()` with no
bundle id returns the test target, not whatever is on screen. The
Swift side tracks `DriverState.currentApp` (set by `launchApp`,
cleared by `forceStopApp`), and the Go side mirrors this in
`lastLaunchedBundleID`. `CurrentForeground` always returns a typed
`diag.CodeForegroundUnsupported` error rather than a possibly-stale
tracked value, because there is no way to truthfully answer it
without a private API. Cross-app navigation via deep link or user
gesture desyncs the tracked state. A following `Hierarchy` call on a
simulator can recover by probing candidate bundle ids currently in
`.runningForeground`, passed by the host as `bundleIdCandidates`.

**Gesture synthesizer selection is fully automatic, with no runtime
override.** `EventSynthesizerFactory.make()` picks
`EventRecordSynthesizer` (multi-pointer, pressure) when its dry-run
probe passes, and falls back to `CoordinateSynthesizer` (single-
pointer only) otherwise. There is no environment variable or wire
parameter to force one or the other. A caller only observes the
result through `Capabilities().CanMultiPointer` and `CanPressure`
after `Connect`.

**Private-symbol probe runs a real dry-run dispatch, not just a
selector check.** `EventRecordSynthesizer.isAvailable` verifies every
class and selector it depends on, then confirms
`XCTRunnerDaemonSession.sharedSession` is reachable. Apple's most
common drift pattern across Xcode majors is to keep a selector's name
stable while changing its behavior, which a plain
`respondsToSelector:` check would miss. The probe's log is exposed
through `PingCommand`'s response as `probeLog`, so a version-drift
failure is diagnosable from the host side without attaching a
debugger to the runner.

**Mid-gesture capture runs on a background queue independent of the
gesture's own completion.** A `capture` waypoint schedules a
screenshot on a dedicated `DispatchQueue`, timed against the same
`atOffsetSeconds` clock the gesture dispatch itself uses, so the
touch is still held when the screenshot fires. This is deliberate,
because `synthesizeEvent:completion:` posts its completion block to
the main queue, so any handler that calls it must run off main
(`CommandHandler.requiresMainThread = false`) to avoid deadlocking on
its own semaphore wait.

**The static tree snapshot folds nested accessibility-element
children.** `DumpRawTreeCommand` builds the tree from a single
`app.snapshot()` call rather than live element queries, which is
orders of magnitude faster but means some structurally nested
accessibility elements are folded into their parent's `children` the
same way XCUITest's own snapshot walk folds them. An id reachable
through a live query is not always reachable through the snapshot
tree. Prefer a stable identifier or a coordinate fallback over a deep
structural selector when a nested element proves unaddressable.

**`XCUIElementSnapshot` does not expose `isHittable`.** Hittability is
a live screen-state query, not part of a snapshot. The Go-side
`clickable` derivation in `NormalizeIosTree`
(`core/internal/iosdriver/normalize.go`) instead prioritizes
accessibility traits (`button`, `link`), falling back to an
interactive-elementType whitelist (`button`, `cell`, `textField`,
`searchField`, and so on) when no trait carries the signal.

**Bounds are in points and include safe-area insets.** `getScreenSize`
returns `app.frame` (or the Springboard proxy's frame when no app is
tracked), which includes the status bar and home indicator. A caller
computing "middle of screen" or checking whether a point sits inside
the visible viewport must not assume the reported bounds equal the
device's nominal logical size.

**`pressKey` is not a system primitive.** Android dispatches a BACK
intent regardless of app handling, but iOS only has per-screen
affordances. `BackKeyStrategy` tries the nav bar back button first
(verifiable, returns `affordanceFound: true`) and falls back to an
edge-swipe gesture (unverifiable, returns `affordanceFound: false`
with a hint to try a screen-specific Cancel, Done, or Close
affordance instead).

**`clearFocusedInput` runs a 4-strategy chain.** In order: a select-
all-plus-delete keyboard shortcut, a triple-tap select, a long-press
system edit menu, then an exact-length delete loop that reads the
field's current value length and deletes exactly that many
characters. The chain polls the focused field after each attempt to
confirm it is actually empty before advancing. A strategy that
reports success skips that verification because it already confirmed
the field is empty itself. The keyboard-shortcut fast path requires
simulator hardware-keyboard pairing (`xcrun simctl keyboard <udid>
enable hardware`, or Simulator, then I/O, then Keyboard). Without it,
the delete-loop fallback still covers the field.

**`searchField` is an editable element type.** A cross-platform "find
an input field" heuristic that substring-matches `textfield` alone
misses iOS search UIs. `interactiveIosRoles` in
`core/internal/iosdriver/normalize.go` includes `searchField`
alongside `textField` and `secureTextField`.

## Limitations

- **USB tether only** for physical devices. `iproxy` uses usbmux, and
  Wi-Fi debugging is not supported.
- **First-run trust prompt** on physical devices: iOS shows "Trust
  This Developer" the first time a manually signed test bundle
  launches. It must be tapped on the device and cannot be automated.
- **Cross-app navigation desync**: see the foreground-tracking quirk
  above.
- **`ScreenSize` is cached host-side** and is not invalidated on
  device rotation.
- **The direct-HID simulator path never handles multi-pointer
  gestures**, because the HID helper has no coordinated multi-pointer
  sequencing. `IosSimDriver.DispatchGesture` always delegates to the
  embedded XCUITest-backed driver.
- **No tvOS, watchOS, or visionOS.** Different XCUITest surfaces, out
  of scope.
- **A tap can be rejected as obscured by a dialog's own backdrop.**
  See [`../../docs/known-limitations.md`](../../docs/known-limitations.md#obscurement-check-can-flag-a-dialogs-backdrop-as-blocking-the-dialogs-own-button).
- **`press_key` types `tab`, `escape`, and `delete` as literal text**
  instead of pressing them. See
  [`../../docs/known-limitations.md`](../../docs/known-limitations.md#ios-press_key-types-an-unsupported-key-as-literal-text-instead-of-pressing-it).
- **Typing can fail on the Simulator when a field never reports
  focus or a keyboard.** See
  [`../../docs/known-limitations.md`](../../docs/known-limitations.md#typing-can-fail-on-the-ios-simulator-when-a-field-never-reports-focus-or-a-keyboard).
- **A focus-timeout error omits the field name for role-only
  selectors.** See
  [`../../docs/known-limitations.md`](../../docs/known-limitations.md#a-focus-timeout-error-omits-the-field-name-for-role-only-selectors).

## Extension points

**Adding a new Swift command:**

1. Create `platforms/ios-agent/Tests/Commands/<Name>Command.swift`
   implementing `CommandHandler`.
2. Register it in `platforms/ios-agent/Tests/AtomyxDriverAgent.swift`'s
   `testServeCommands`.
3. Add a matching call to `IosDriver` in
   `core/internal/iosdriver/driver.go`.
4. If it backs a new or changed `driver.Driver` port method, update
   the port in `core/internal/driver/driver.go` and implement the
   same shape in `core/internal/androiddriver/driver.go`.
5. Unit-test the handler against the mock bridge in
   `platforms/ios-agent/Tests/CommandHandlerUnitTests.swift`.

**Adding a new press-key strategy:** drop a file into
`platforms/ios-agent/Tests/PressKey/` implementing `PressKeyStrategy`
and register it with the `PressKeyRegistry` built in
`platforms/ios-agent/Tests/AtomyxDriverAgent.swift`. The chain
short-circuits on the first strategy that returns a verifiable hit.

**Adding a new clear-text strategy:** drop a file into
`platforms/ios-agent/Tests/Commands/Clear/Strategies/` and add it to
the ordered list `ClearFocusedInputCommand` builds its
`ClearTextChain` from.

**Adding a new gesture synthesizer backend:** implement
`EventSynthesizer` with its own `mechanismName` and an `isAvailable`
probe, then add one line to `EventSynthesizerFactory.make()` ahead of
the existing fallback. The factory does not branch on concrete
types, so a caller never learns a new backend exists.

## References

- `pitfalls.md`: iOS traps to avoid when editing the adapter.
- `tools.md`: cross-platform tool contract the driver plugs into.
- `android.md`: the Android-side equivalents. Both drivers implement
  the same `driver.Driver` port in `core/internal/driver/driver.go`,
  so their capability flags and gesture wire shapes line up one-to-one.
