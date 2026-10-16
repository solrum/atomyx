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
| `Connect()` | `GET /health` then `GET /ping` | `HttpControlServer` inline health check, then `PingRoute` | A `/ping` call that cannot reach the agent is swallowed, so `Connect` keeps conservative capability defaults. An agent whose `/ping` `protocolVersion` differs from the host's, whose `/ping` answers with an error status because it predates the route (version 0), or whose `/ping` lacks `capabilities.secureFields` is stale: see the notes below. A `/health` failure fails `Connect`, classified per the note below. |
| `Ping()` | `GET /health` | `HttpControlServer` inline health check | Bounded to a 1s probe timeout so a caller gets a fast answer. Classified per the note below. |
| `Hierarchy()` | `GET /tree` | `TreeRoute` then `UiTreeService.dumpTree` | Returns the hierarchical tree, then `NormalizeAndroidTree` maps it to the canonical shape. |
| `Tap()` | `POST /actions/tap_coords` | `TapCoordsRoute` then `GestureDispatcher.tapAt` then `GestureRunner.dispatch` | |
| `LongPress()` | `POST /actions/long_press` | `LongPressRoute` then `GestureDispatcher.longPressAt` then `GestureRunner.dispatch` | Duration coerced to `[300, 5000]` ms. |
| `Swipe()` | `POST /actions/swipe` | `SwipeRoute` then `GestureDispatcher.swipe` then `GestureRunner.dispatch` | |
| `DispatchGesture()` | `POST /actions/dispatch_gesture` | `DispatchGestureRoute` then `GestureRunner.dispatch` | Rejects a multi-pointer or pressure-carrying gesture host-side first, then the agent re-validates on arrival. A `capture` waypoint returns a base64 image per capture in the response. |
| `InputText()` | `POST /actions/type_keyboard` | `TypeKeyboardRoute` then `KeyboardTyper.typeViaKeyboard` | Always sent with `clearFirst: false`, honoring the append contract. A caller that wants to clear first calls `EraseText` as its own step. The set-text strategy re-reads the focused field fresh after writing and verifies it is the same field and holds the requested text appended to any prior content, or for a secure field a value of the requested length. On a mismatch the route replies `success: false` with `typed` set to the characters known to have landed and a short reason that never contains field contents. An empty field after the write falls through to the per-key strategies. |
| `EraseText()` | `POST /actions/clear_focused_input` | `ClearFocusedInputRoute` then `FocusedInputClearer.clearFocusedInput` | One call regardless of `count`, always clearing the field's entire value. On failure the route also replies with `strategiesTried`, `lastValue`, `focusedNodeDesc`, `screenWidth`, and `screenHeight` (success sends only `ok` and `reason`). The driver returns an `AGENT_REJECTED` error carrying the sanitized reason followed by the strategy names tried (at most six plain identifiers, never the remaining field value) and a remedy to focus an input field first, wrapping a `*ClearTextError` with the full diagnosis. |
| `PressKey()` | `POST /actions/key` | `KeyRoute` then `SystemActionDispatcher.pressKey` | Supports `back` and `home`. `enter` is rejected, because no accessibility or app-scope action synthesizes a hardware Enter on a non-rooted device. |
| `HideKeyboard()` | `POST /actions/hide_keyboard` | `HideKeyboardRoute` then `KeyboardHider.hideKeyboard` | |
| `LaunchApp()` | `POST /actions/launch` | `LaunchRoute` then `SystemActionDispatcher.launchApp` | `Context.startActivity` via the package's launch intent. The request's optional `arguments` object becomes string extras on that intent, and the driver rejects launch process arguments and environment variables with `INVALID_ARGUMENT` because an intent cannot carry them. After the intent is sent, the driver polls `CurrentForeground` until the package equals the launched one (the attach shortcut for an app already in front returns without waiting). |
| `StopApp()` / `KillApp()` | none (host `adb shell am force-stop <package>`) | the device shell | Runs through the host process runner because the shell user holds the force-stop permission that the agent process lacks, so reset works on non-rooted devices. The package name is validated against the Android package-name grammar before it reaches the shell. A non-zero exit returns a `*ForceStopError` carrying the shell's stderr, and a reset `LaunchApp` propagates it instead of relaunching over an unstopped process. The command exits 0 for a package that is installed but not running or not installed, so stopping an absent app is a no-op. The agent keeps `POST /actions/force_stop` (`ForceStopRoute`), but the driver does not call it. After the shell command exits 0, the driver polls `CurrentForeground` until the package differs from the stopped one. |
| `OpenLink()` | none (host `adb shell am start -a android.intent.action.VIEW -d '<link>'`) | the device shell | Runs through the host process runner. `adb shell` joins its words into one string for the device shell, so the link, and the package passed with `-p` when `OpenLinkOptions.AppID` is set, are each single-quoted and a quote inside them is escaped. The activity manager exits 0 when it cannot start an activity and prints a line starting with `Error`, so the driver reads that line and returns `LINK_NOT_OPENED`. The package name is validated against the Android package-name grammar first. The agent has no route for it. |
| `SetPermissions()` | none (host `adb shell pm grant` and `pm revoke`) | the device shell | Runs through the host process runner. The driver reads the API level with `getprop ro.build.version.sdk`, then maps each neutral name to its runtime permissions in `core/internal/androiddriver/permission_table.go`: camera, microphone, location (fine and coarse), contacts (read and write), calendar (read and write), photos (media images and video from API 33, read external storage before), and notifications (`POST_NOTIFICATIONS`, API 33 and later). A name with no permission at the device level, which is any name below API 23 and notifications below API 33, fails with `PERMISSIONS_UNSUPPORTED` before any change. The package and permission names are single-quoted like the `OpenLink` arguments. A group member the app does not declare is skipped, and the call fails with `PERMISSION_NOT_SET` only when the app declares none of them. Revoking a permission may stop the app, so a caller sets permissions before launching. |
| `SetOrientation()` | none (host `adb shell settings put system accelerometer_rotation 0`, then `user_rotation <value>`) | the device shell | Runs through the host process runner. Portrait is rotation 0, landscapeLeft is rotation 1 (the device turned a quarter counterclockwise) and landscapeRight is rotation 3 (a quarter clockwise). Upside-down portrait is not offered. Automatic rotation stays off and the rotation persists until changed, and nothing is restored when the session ends. The driver then reads `ScreenSize` every 250 ms for up to 10 seconds until it reflects the layout, and returns `ORIENTATION_NOT_SET` when it does not. `ScreenSize` comes from the live hierarchy, so there is no cached size to invalidate. The size cannot tell the two landscape orientations apart. |
| `SetLocation()` | none (host `adb -s <serial> emu geo fix <longitude> <latitude>`) | the emulator console | Runs through the host process runner and is offered only when the serial starts with `emulator-` (`CanSetLocation`). The console takes the longitude first, the reverse of the port order, and prints `KO` with exit status 0 when it rejects a command, so the driver reads the output and returns `LOCATION_NOT_SET`. A physical device has no console and needs a mock location provider installed on the device, so every call there fails with `LOCATION_UNSUPPORTED`. The position persists until changed. |
| `CurrentForeground()` | `GET /current-activity` | `CurrentActivityRoute` | The agent answers from a cache fed by window state change events, so the package trails the system by about 0.3 to 1 s after a stop, and the activity class can stay a placeholder such as `android.widget.FrameLayout` for up to about 3 s after a launch while the package is already right. Callers compare the package only. |
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

`/ping` carries `protocolVersion` (the wire contract number declared in
`AgentProtocol.kt`) and `agentVersion` (the build's version name), and
`Connect` stores both for `AgentInfo()`. The host accepts only an agent
whose `protocolVersion` equals `version.AgentProtocolVersion`. An agent
that answers `/ping` with an error status predates the route and counts
as version 0, while a `/ping` that cannot be reached at all is not a
mismatch and keeps the conservative defaults.

`/ping` also carries `capabilities.secureFields: true` in every agent
that reports `isPassword` on tree nodes. The capability list only grows,
so a host ignores capabilities it does not know and a new agent keeps
working with an older host. `Connect` treats an agent without the
capability as stale even when the versions agree, because it would
leave a password unmasked. When the driver was built with a reinstall
hook, which the device factory supplies from its provisioner, `Connect`
runs the install and accessibility rebind path once, ignoring the
readiness probe, and pings again. It fails with a remedy to build or
point `ATOMYX_ANDROID_APK` at a current agent when no APK can be found,
when no hook exists, or when the agent is still stale afterwards. The
error code is `AGENT_VERSION_MISMATCH` when the protocol versions
differ and `AGENT_NOT_READY` when only the secret field marker is
missing.

A mutating request waits for the router lock for at most 5 seconds. A
request still queued after that is refused with `429`
`{"error":"<message>","retryable":true}` and never runs, so a gesture
the host already gave up on cannot fire later. The driver reports it as
`AGENT_NOT_READY` with a remedy to wait for the running gesture and
repeat the call, and the remedy says the refused call did not run.

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
the whole workstation, `Connect` first checks `adb forward --list`.
`device.DriverFactory` builds every Android driver with
`Options.AllocateHostPort`, which works as follows:

- A forward that already maps this serial to the device port is
  reused. `Disconnect` leaves it in place, unless the factory was built
  with `DriverFactoryDeps.OwnAndroidForwards` (set by `atomyx` and
  `atomyx-mcp`, whose sessions hold the device lease) and the agent
  answered /health through it, in which case `Disconnect` removes it.
  The Studio sidecar never sets it.
- Otherwise `Connect` runs `adb -s <serial> forward tcp:0
  tcp:<devicePort>`, takes the host port adb prints, and removes that
  forward on `Disconnect`. Empty or non-numeric output from adb is an
  `AGENT_UNREACHABLE` error.
- A reused forward whose agent does not answer is treated as stale
  (`adb kill-server` and device reboots leave these behind). `Connect`
  removes it and allocates a fresh one once. An agent that answers
  with `AGENT_NOT_READY` is not stale and is never reallocated.
- The transport is built after allocation through
  `Options.NewTransport`, which is required when `AllocateHostPort`
  is set.

Without `AllocateHostPort`, `Options.HostPort` (default 8765) is the
fixed host port. In that mode `Connect` reuses a forward already
pointing at this serial and device port, creates one when none
exists, and fails with `HOST_PORT_IN_USE` when the local port is
already forwarded to a different device or device port.
`DEVICE_BUSY` stays reserved for a foreign automation session.

Reallocation happens at `Connect` time only. A forward dropped in the
middle of a run surfaces as an ordinary call failure for that run.

Multiple concurrent devices: each driver gets its own host port from
adb, so no per-driver port configuration is needed. The sidecar's
`DriverProvisioner.EnsureAndroidAgent` follows the same rule through
`androiddriver.ForwardHostPort`: it reuses the forward adb already holds
for the serial, or lets adb allocate one, and builds its health client
and its launcher against that port, so two serials never contend for the
fixed 8765.

Proxy, certificate, and screen-lock setup for a physical device are
covered in `android-device-setup.md`, not here.

## Platform-specific quirks

**The foreground report trails the system.** `LaunchApp` and `StopApp`
succeed once the intent or the shell command is accepted, but the
agent reports the foreground from window state change events. The
driver therefore polls `CurrentForeground` after a launch until the
package equals the launched one, and after a stop until it differs
from the stopped one. It compares the package only, polls every
100 ms for at most 5 s on `Options.Clock`, and on timeout or a failed
foreground read emits one warning through `Options.Warn` and returns
nil, because an app may hand off to another package. The attach
shortcut for an app already in front returns without waiting.

A launch that lands on a keyguard or on a permission dialog owned by
another package never reports the launched package in front, so the
wait runs the full 5 s bound and emits the timeout warning even though
the launch succeeded. The wait never outlasts the caller's deadline:
a deadline that ends it counts as a timeout and returns nil, while an
explicit cancellation returns the cancellation error.

**A lost Android agent has no exit signal.** The host supervises no
Android process. The control plane is an `AccessibilityService` inside
the device's app process, reached through an `adb forward` that the
adb server owns, so the host observes neither the agent exiting nor the
forward breaking without probing the device. `device.Built.Lost` stays
nil for Android. A lost agent or device surfaces as a failed step that
the liveness probe classifies as `RunAbortDeviceLost`, which the CLI and
the MCP server both record as outcome `deviceLost`. A lost lease still
moves the session to lost.

**No pressure injection.** The `AccessibilityService` gesture surface
has no per-touch pressure API at any current Android level.
`GestureRunner.GestureCapabilities.DEFAULT.canPressure` is
permanently `false`. `/ping` reports it, and the driver rejects
pressure waypoints before dispatch.

**A password input carries the `secure` attribute, not a distinct
role.** The agent reports `AccessibilityNodeInfo.isPassword` as
`isPassword` on a tree node, and only when it is true. The driver
normalizer turns it into `attributes["secure"] = "true"` and keeps the
role `text-field`. `orchestra.IsSecure` is the only reader of the
attribute.

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

**Mutating routes run one at a time.** The HTTP server handles each
request on its own thread. `Route.mutates` marks a route that changes
device state, and `Router` runs every such route behind one lock, so a
multi-step action such as `TypeKeyboardRoute` never interleaves with a
tap, swipe, key press, or launch from another request. The routes
outside the lock are `PingRoute`, `TreeRoute`, `KeyboardRoute`,
`ScreenshotRoute`, `CurrentActivityRoute`, and `AppsRoute`. They only
read state, and a gesture can run for tens of seconds, so a screenshot
or tree read during a held gesture must not wait for it. Tree reads
serialize against each other through `UiTreeService`'s own guard. The server registers its routes from `ShippedRoutes`, the one list the router tests also walk. A
`capture` waypoint inside `DispatchGestureRoute` calls the screenshot
surface directly on its own thread, so it never contends for the lock.

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

1. Add a `Route` implementation, declaring `mutates`, in
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
