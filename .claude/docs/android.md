# Android driver internals

## Purpose

For contributors or AI agents editing the Android code path: the Go
host adapter under `core/internal/androiddriver`, the shared HTTP
transport under `core/internal/transport`, or the Kotlin agent under
`platforms/android-agent/app/src/main/java/dev/atomyx/agent`.

Read this before editing either tree, debugging an Android failure,
or adding a capability that must work on Android.

This doc does NOT cover: the cross-platform `Driver` contract itself
(see `architecture.md`), the cross-platform tool surface (see
`tools.md`), or how to check a device's proxy, certificate, or screen
lock state (see `android-device-setup.md`). Known traps are in
`pitfalls.md`.

## Architecture

The Android agent is a standalone APK that hosts an HTTP control
server on the device. The Go driver reaches it over `adb forward`:

```
Go core (core/internal/androiddriver)
  AndroidDriver ── implements driver.Driver
      │
      ▼
  Transport (core/internal/transport.HTTPClient)
      │ adb forward tcp:<hostPort> -> tcp:<devicePort>
      ▼
Device (Atomyx agent APK, dev.atomyx.agent)
  HttpControlServer ── Router ── Route implementations
      │
      ├── PingRoute, TreeRoute, ScreenshotRoute,
      │   CurrentActivityRoute, AppsRoute (reads)
      └── TapCoordsRoute, LongPressRoute, SwipeRoute,
          DispatchGestureRoute, TypeKeyboardRoute,
          ClearFocusedInputRoute, HideKeyboardRoute,
          KeyRoute, LaunchRoute, ForceStopRoute (actions)
             │
             ▼
  AccessibilityService.dispatchGesture / performGlobalAction /
  Context.startActivity
```

Two collaborators back the accessibility service:

| Collaborator | Role |
|---|---|
| `AtomyxAccessibilityService` | Provides `windows`, `dispatchGesture`, `performGlobalAction`. Required for every route except `/health`. |
| `AtomyxForegroundService` | Owns the HTTP server's lifecycle plus the persistent notification that keeps the process alive. |

`AndroidDriver` (`core/internal/androiddriver/driver.go`) implements
`driver.Driver` by mapping each port method onto one HTTP call
through the injected `Transport` interface
(`core/internal/androiddriver/transport.go`), backed in production by
`transport.HTTPClient`. App-lifecycle methods with no HTTP route
(install, uninstall, clear data, install check, and the adb-forward
tunnel `Connect`/`Disconnect` rely on) instead shell out through an
injected `process.Runner` that calls `adb` directly.

Everything above the `Driver` port, including selector resolution,
scroll-into-view, obscurement detection, and priority broadening,
lives in the composition layer that consumes `driver.Driver`, never
in `AndroidDriver` itself: the driver's only jobs are HTTP transport,
adb-forward lifecycle, and tree normalization
(`core/internal/androiddriver/normalize.go`) into the canonical
`wire.TreeNodeWire` shape.

## Command surface

| Driver port method | Wire route | Agent handler | Notes |
|---|---|---|---|
| `Connect()` | `GET /health` then `GET /ping` | `HttpControlServer` inline health check, then `PingRoute` | A `/ping` failure is swallowed, so `Connect` keeps conservative capability defaults and an agent build predating the handshake still works at single-pointer, no-pressure. A `/health` failure fails `Connect`, classified per the note below. |
| `Ping()` | `GET /health` | `HttpControlServer` inline health check | Bounded to a 1s probe timeout so a caller gets a fast answer. Classified per the note below. |
| `Hierarchy()` | `GET /tree` | `TreeRoute` then `UiTreeService.dumpTree` | Returns the hierarchical tree, then `NormalizeAndroidTree` maps it to the canonical shape. |
| `Tap()` | `POST /actions/tap_coords` | `TapCoordsRoute` then `GestureDispatcher.tapAt` then `GestureRunner.dispatch` | |
| `LongPress()` | `POST /actions/long_press` | `LongPressRoute` then `GestureDispatcher.longPressAt` then `GestureRunner.dispatch` | Duration coerced to `[300, 5000]` ms. |
| `Swipe()` | `POST /actions/swipe` | `SwipeRoute` then `GestureDispatcher.swipe` then `GestureRunner.dispatch` | |
| `DispatchGesture()` | `POST /actions/dispatch_gesture` | `DispatchGestureRoute` then `GestureRunner.dispatch` | Rejects a multi-pointer or pressure-carrying gesture host-side first, then the agent re-validates on arrival. A `capture` waypoint returns a base64 image per capture in the response. |
| `InputText()` | `POST /actions/type_keyboard` | `TypeKeyboardRoute` then `KeyboardTyper.typeViaKeyboard` | Always sent with `clearFirst: false`, honoring the append contract. A caller that wants to clear first calls `EraseText` as its own step. |
| `EraseText()` | `POST /actions/clear_focused_input` | `ClearFocusedInputRoute` then `FocusedInputClearer.clearFocusedInput` | One call regardless of `count`, always clearing the field's entire value. Failure returns a `*ClearTextError` carrying the agent's full diagnosis. |
| `PressKey()` | `POST /actions/key` | `KeyRoute` then `SystemActionDispatcher.pressKey` | Supports `back` and `home`. `enter` is rejected, because no accessibility or app-scope action synthesizes a hardware Enter on a non-rooted device. |
| `HideKeyboard()` | `POST /actions/hide_keyboard` | `HideKeyboardRoute` then `KeyboardHider.hideKeyboard` | |
| `LaunchApp()` | `POST /actions/launch` | `LaunchRoute` then `SystemActionDispatcher.launchApp` | `Context.startActivity` via the package's launch intent. |
| `StopApp()` / `KillApp()` | `POST /actions/force_stop` | `ForceStopRoute` then `SystemActionDispatcher.forceStopApp` | Tries a reflective `forceStopPackage`, falls back to `killBackgroundProcesses`. Reports `ok:false` with a reason when the platform refuses both, and `AndroidDriver.StopApp` turns that into a `*ForceStopError`, so a reset `LaunchApp` propagates it instead of relaunching over an unstopped process. |
| `CurrentForeground()` | `GET /current-activity` | `CurrentActivityRoute` | |
| `ListApps()` | `GET /apps` | `AppsRoute` | Reads `PackageManager.getInstalledApplications`. |
| `Screenshot()` | `GET /screenshot` | `ScreenshotRoute` | JPEG, base64-encoded. |
| `InstallApp()` / `UninstallApp()` / `ClearAppData()` / `IsAppInstalled()` | none (host-only) | `adb install` / `adb uninstall` / `adb shell pm clear` / `adb shell pm list packages` | No HTTP route: the driver shells out directly. |
| `DeviceInfo()` | none | none | The agent exposes no device-info route. Only `UDID` (the serial) and `Kind` (derived from an `emulator-` serial prefix) carry real information. |
| `ScreenSize()` | derived from `GET /tree` | `TreeRoute` | No dedicated route. `ScreenSize` derives width and height from the largest-area child of the tree's synthetic root (`androiddriver.findScreenBounds`). |
| `WaitForIdle()` | none | none | Always reports `true` without waiting, because `Capabilities().CanWaitForIdle` is `false`, so a caller must fall back to tree-diff polling. |

`/health` answers `200 {"ok":true,"accessibilityConnected":true}` when
the accessibility service is bound and can read the screen, and `503
{"ok":false,"accessibilityConnected":<bool>,"reason":"<fixed text>"}`
otherwise. `Connect` and `Ping` classify a failed `/health` call apart
from an unreachable agent: a connection that never went through
reports `AGENT_UNREACHABLE` with a remedy to confirm the agent is
running, while a `503` reports `AGENT_NOT_READY` with the response's
own reason as the message and a remedy to re-enable the accessibility
service, since the agent process answered and named what is wrong.

## Setup and run

Default: emulator or physical device on the same workstation. Both
paths go through `adb forward`. `AgentLauncher` in
`core/internal/androiddriver` automates the full bring-up sequence:

1. `adb forward tcp:<hostPort> tcp:<devicePort>` (idempotent).
2. Probe `GET /health`. If it already reports
   `{"ok":true,"accessibilityConnected":true}`, return immediately.
3. Install the agent APK (`adb install -r <apk>`). The APK path
   resolves from an explicit option, then the `ATOMYX_ANDROID_APK`
   environment variable, then an upward search from the running
   binary and the working directory for
   `platforms/android-agent/app/build/outputs/apk/debug/app-debug.apk`.
4. Rebind the accessibility service: `settings delete secure
   enabled_accessibility_services`, sleep, `settings put secure
   enabled_accessibility_services <service>`, sleep. Android caches
   the previous binding across an APK reinstall, so a fresh install
   without this rebind leaves the accessibility service unresponsive.
5. Start the foreground service:
   `am start-foreground-service -n dev.atomyx.agent/.control.AtomyxForegroundService`.
6. Poll `GET /health` until it reports ready or the launch timeout
   (30s default) elapses.

`AndroidDriver.Connect` runs a lighter version of this: it checks for
a foreign automation session already driving the device (a live
`dumpsys activity` instrumentation record, meaning some other tool
already has the device), then ensures the adb forward is in place,
verifies `/health`, then runs the `/ping` capability handshake.
Because a host-side local port is a real listening socket shared by
the whole workstation, `Connect` first checks `adb forward --list`: it
reuses a forward already pointing at this serial and device port
without recreating it, creates one when none exists, and fails with a
device-busy diagnostic when the local port is already forwarded
somewhere else. `AndroidDriver.Disconnect` removes the forward only
when this `Connect` call is the one that created it, so a forward
left behind by another run, an operator's manual setup, or another
driver instance sharing the same port survives a single driver's
disconnect.

Multiple concurrent devices: pass distinct `HostPort` values to each
`androiddriver.Options`. `adb forward` is scoped per `(serial,
hostPort)` pair.

Proxy, certificate, and screen-lock setup for a physical device are
covered in `android-device-setup.md`, not here.

## Platform-specific quirks

**No pressure injection.** The `AccessibilityService` gesture surface
has no per-touch pressure API at any current Android level.
`GestureRunner.GestureCapabilities.DEFAULT.canPressure` is
permanently `false`. `/ping` reports it, and the driver rejects
pressure waypoints before dispatch.

**Multi-pointer gestures dispatch one time segment per
`GestureDescription`, one continued stroke per pointer.**
`GestureRunner.dispatchMultiPointer` chains a sequence of
`GestureDescription` calls, one per waypoint-aligned time segment,
each holding exactly one stroke per pointer still active in that
segment (`willContinue=true` keeps a pointer's touch down across
calls). Packing every pointer's full path into a single description
instead loses angular fidelity across sub-stroke boundaries, which is
why that shape reads a rotate gesture as no rotation at all.
`GestureDescription.Builder` itself caps how many concurrent strokes
one description can hold, so this dispatch shape keeps every
description at exactly the pointer count, always well under that
platform cap.

**Hold segments emit a minimal stroke, then sleep silently between
dispatches.** A zero-length segment inside a single
`GestureDescription` still emits periodic same-coordinate
`ACTION_MOVE` events roughly every 10ms (the platform interpolates
regardless of path length). A recognizer that rejects a gesture on
any move event before its own long-press timer fires, such as
Flutter's `DelayedMultiDragGestureRecognizer` backing
`ReorderableListView`, refuses that shape. `GestureRunner` instead
issues a minimal `MIN_PRESS_MS` (50ms) stroke, then sleeps the
authored remainder at the Kotlin level between dispatch calls, which
produces no platform events at all during the hold.

**Pointer coordinates are physical pixels, while Flutter lays out in
logical pixels.** A Flutter app's own coordinate space is scaled by
its `devicePixelRatio`, so a script or tool computing a tap target
from Flutter layout data must multiply by that ratio before
dispatching through `Tap` or `DispatchGesture`, which both take
physical-pixel points.

**A hold must exceed roughly 500ms to trigger a Flutter long-press.**
Flutter's `LongPressGestureRecognizer` uses its own timer independent
of the platform's, so a `LongPress` or a `capture`-free hold segment
authored shorter than that risks resolving as a tap instead.

**`press_key('enter')` is unsupported on non-rooted devices.** There
is no accessibility or app-scope action that synthesizes a hardware
Enter, so a script's line terminators must go through IME actions
(search or done key) instead. `SystemActionDispatcher.pressKey`
throws explicitly rather than silently dropping the key.

**The accessibility tree exposes a filled text field's current value
under `text`, not a separate value attribute.** `UiTreeService`
reads a node's own `AccessibilityNodeInfo.text`, and
`NormalizeAndroidTree` maps it straight onto the canonical `text`
attribute. There is no separate raw-value field on the Android side.

**`adb forward` is idempotent but not permanent.** A USB disconnect,
`adb kill-server`, or a device reboot drops every forward. A caller
that suspects this should re-run the forward rather than assuming a
previously connected session is still reachable.
`AgentLauncher.EnsureRunning` re-issues its forward unconditionally on
every call for this reason. `AndroidDriver.Connect` instead checks
`adb forward --list` first and only issues a new forward when none
already points where this driver needs it, so it can tell a forward it
created apart from one it is merely reusing.

**Never cache `AccessibilityNodeInfo` across calls.** `UiTreeService`
is a metadata-only registry by design, because node references go
stale the moment the underlying view tree changes (keyboard appears,
focus shifts, a `RecyclerView` recycles a row). It captures bounds
and stable selectors at dump time and re-resolves a fresh node from
the live tree whenever an action needs one.

## Limitations

- `WaitForIdle` is not exposed, so a caller must fall back to
  tree-diff polling instead.
- `ScreenSize` has no dedicated route. It derives width and height
  from `/tree`'s largest-area child, so an unusual window layout with
  no window-rooted child at all reports an error.
- `DeviceInfo` reports `PlatformVersion` and `Model` as `"unknown"`,
  because the agent exposes no `getprop`-backed route.
- `CanPressure` is permanently `false`, because the accessibility
  gesture surface exposes no per-touch pressure injection on any
  current Android level.
- A tap can be rejected as obscured by a dialog's own backdrop. See
  [`../../docs/known-limitations.md`](../../docs/known-limitations.md#obscurement-check-can-flag-a-dialogs-backdrop-as-blocking-the-dialogs-own-button).
- A focus-timeout error omits the field name for role-only selectors.
  See [`../../docs/known-limitations.md`](../../docs/known-limitations.md#a-focus-timeout-error-omits-the-field-name-for-role-only-selectors).

## Extension points

**Add a new route** (host-agent wire method):

1. Add a `Route` implementation in
   `platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/router/CommonRoutes.kt`
   (or a new file in the same package).
2. Register it in `HttpControlServer.buildRoutes()`.
3. Add the corresponding call to the `Transport` interface in
   `core/internal/androiddriver/transport.go` and to
   `transport.HTTPClient` in `core/internal/transport`.
4. Wire it into `AndroidDriver` in
   `core/internal/androiddriver/driver.go`, matching the `driver.Driver`
   port method it backs.
5. Cover both sides with tests: Kotlin unit tests alongside the route,
   Go tests in `core/internal/androiddriver/driver_test.go`.

**Add a new gesture backend.** Today `GestureRunner` calls
`AccessibilityService.dispatchGesture` directly. A second backend
would extract a small interface carrying `dispatch` plus a
capabilities snapshot, select it at `GestureRunner` construction, and
surface its identity through `/ping`'s `mechanism` field so host-side
diagnostics can correlate. The current single-backend design is
intentional: Android has one stable public API for gesture dispatch,
so inventing the interface before a second concrete backend exists is
maintenance overhead for no real gain.

## References

- `platforms/android-agent/README.md`: APK build and permission
  granting flow.
- `android-device-setup.md`: proxy, certificate, and screen-lock setup
  for a connected device.
- `pitfalls.md`: Android traps to avoid when editing the adapter.
- `tools.md`: cross-platform tool contract the driver plugs into.
- `ios.md`: the iOS-side equivalents. Both drivers implement the same
  `driver.Driver` port in `core/internal/driver/driver.go`, so their
  capability flags and gesture wire shapes line up one-to-one.
