# Android driver internals

## Purpose

For contributors or AI agents editing the Android code path —
host-side TypeScript adapter under `packages/android-driver/`, Kotlin
APK agent under `platforms/android-agent/`, or any tool that has to
reason about Android-specific behavior.

Read before: editing any file in those two trees, debugging an
Android smoke failure, or adding a capability that must work on
Android.

This doc does NOT cover: how to install the APK (see
`platforms/android-agent/README.md`), the cross-platform tool
contract (see `tools.md`), or the architectural contract (see
`architecture.md`). Known traps are in `pitfalls.md`.

## Architecture

Android support is split across two codebases that talk over HTTP
via `adb forward`:

```
┌──────────────────────────────────────────────────────────────┐
│ Host (Node)                                                  │
│                                                              │
│  @atomyx/mcp tools ──► Orchestra (@atomyx/driver)            │
│                          │                                   │
│                          ▼                                   │
│                      AndroidDriver ── tree-normalizer        │
│                          │                                   │
│                          ▼                                   │
│                      HttpClient  (fetch-based, 10s default)  │
└─────────────────────────────────────┼────────────────────────┘
                                      │ adb forward
                                      │ tcp:$hostPort → tcp:8765
                                      ▼
┌──────────────────────────────────────────────────────────────┐
│ Device (Atomyx Agent APK, dev.atomyx.agent)                  │
│                                                              │
│  NanoHTTPD ──► Router ──► routes/CommonRoutes.kt             │
│                             │                                │
│       ┌──────────┬──────────┼──────────┬──────────┐          │
│       ▼          ▼          ▼          ▼          ▼          │
│  Gesture   Gesture    Input     System    UiTree             │
│  Dispatcher Runner    Dispatcher Action   Service            │
│  (tap/     (completion- (typing/ Dispatcher (tree/           │
│   swipe/    aware        clear/  (pressKey/ compact)         │
│   longPress) dispatch)   hide)    launch/                    │
│       │          │                stop)                      │
│       └──────────┤                 │                         │
│                  ▼                 ▼                         │
│         AccessibilityService.dispatchGesture /               │
│         performGlobalAction / Context.startActivity          │
└──────────────────────────────────────────────────────────────┘
```

Two collaborators live alongside the APK:

| Collaborator | Role |
|---|---|
| `AtomyxAccessibilityService` | Provides `rootInActiveWindow`, `dispatchGesture`, `performGlobalAction`. Required for every tool. |
| `AtomyxForegroundService` | Owns `HttpControlServer` lifecycle + persistent notification so the OS doesn't reclaim the process. |

`HttpClient` is stateless (native `fetch`), so "disconnect" is a
matter of releasing `adb forward`. No TCP session to reopen.

## Host-side scope

Same contract as the iOS adapter. `AndroidDriver` does NOT:

- Resolve selectors — `@atomyx/driver` runs all selector logic on
  the canonical `TreeNode` from `hierarchy()`.
- Scroll into view — core's `ScrollController` composes
  `hierarchy()` + `swipe()` host-side.
- Detect obscurement — core's `detectObscurement` runs host-side.
- Broaden selector priority — core's `compileSelector` handles it.

That leaves the driver with four jobs: HTTP transport, adb-forward
lifecycle, tree normalisation, and a 1:1 mapping from the `Driver`
interface to APK routes.

## Command surface

The APK exposes ~15 routes; the gesture-related contract is:

| Driver port method | Route | Device handler | Notes |
|---|---|---|---|
| `connect()` | `/health` → `/ping` | `HttpControlServer` + `PingRoute` | Ping populates capabilities + mechanism |
| `reconnect()` | `/health` → `/ping` | same | Picks up capability drift across APK upgrades |
| `hierarchy()` | `/tree` | `TreeRoute` → `UiTreeService.dumpTree` | Returns hierarchical `RawElementDto` |
| `tap()` | `/actions/tap_coords` | `TapCoordsRoute` → `GestureDispatcher.tapAt` → `GestureRunner` | Completion-aware; 50ms MIN_PRESS_MS floor |
| `swipe()` | `/actions/swipe` | `SwipeRoute` → `GestureDispatcher.swipe` → `GestureRunner` | Completion-aware; blocks for full duration |
| `longPress()` | `/actions/long_press` | `LongPressRoute` → `GestureDispatcher.longPressAt` → `GestureRunner` | Duration coerced to [300, 5000]ms |
| `dispatchGesture()` | `/actions/dispatch_gesture` | `DispatchGestureRoute` → `GestureRunner.dispatch` | Same pipeline as the primitives above; `CountDownLatch(1)` 30s |
| `inputText()` | `/actions/type_keyboard` | `TypeKeyboardRoute` → `InputDispatcher.typeViaKeyboard` | ACTION_SET_TEXT fast path + IME fallback |
| `eraseText()` | `/actions/clear_focused_input` | `ClearFocusedInputRoute` → `InputDispatcher.clearFocusedInput` | One RPC regardless of count |
| `hideKeyboard()` | `/actions/hide_keyboard` | `HideKeyboardRoute` → `InputDispatcher.hideKeyboard` | GLOBAL_ACTION_BACK + IME poll |
| `pressKey()` | `/actions/key` | `KeyRoute` → `SystemActionDispatcher.pressKey` | Supports back / home only (enter requires root) |
| `launchApp()` | `/actions/launch` | `LaunchRoute` → `SystemActionDispatcher.launchApp` | `Context.startActivity` via launch intent |
| `stopApp()` / `killApp()` | `/actions/force_stop` | `ForceStopRoute` → `SystemActionDispatcher.forceStopApp` | Reflective `forceStopPackage`, falls back to `killBackgroundProcesses` |
| `screenshot()` | `/screenshot` | `ScreenshotRoute` | JPEG, base64-encoded |
| `currentForeground()` | `/current-activity` | `CurrentActivityRoute` | UsageStats-backed |

## Setup & run

Default: emulator or physical device on the same workstation. Both
paths go through `adb forward`.

1. `make apk-install` (from `platforms/android-agent/`) or
   `./gradlew :app:installDebug`.
2. Open the Atomyx app on the device, tap "Enable Atomyx control
   server". This deep-links to Accessibility Settings and Usage
   Access — both must be granted or `AtomyxForegroundService`
   won't start.
3. Verify from the host: `adb forward tcp:8765 tcp:8765 && curl
   http://127.0.0.1:8765/health` should return
   `{"ok":true,"accessibilityConnected":true}`.

Multiple concurrent devices: pass distinct `hostPort` values to
each `AndroidDriver` constructor. `adb forward` is scoped per
`(serial, hostPort)` pair.

## Platform-specific quirks

**No pressure injection.** The `AccessibilityService` gesture
surface has no per-touch pressure API at any current Android
level. `GestureRunner.GestureCapabilities.DEFAULT.canPressure` is
permanently false; `/ping` reports it and the host-side validator
rejects pressure waypoints before dispatch.

**Multi-pointer runs through `GestureDescription.Builder`.** Each
pointer becomes one `StrokeDescription`; the builder accepts
concurrent strokes (non-overlapping in space, overlapping in time)
for pinch / rotate / two-finger scroll. `MAX_STROKE_COUNT` caps
each gesture at 10 strokes — enforced by the platform, not by
Atomyx. `canMultiPointer` is reported true on `SDK_INT >= N`
(minSdk is 26, so effectively always).

**Completion callback threads on main.** `dispatchGesture(g,
callback, handler=null)` delivers `GestureResultCallback` on the
service's main thread. Running a caller on main would deadlock;
`GestureRunner.dispatch` documents that contract. NanoHTTPD's
worker threads are off-main, which is the supported entry point.

**`press_key('enter')` unsupported on non-rooted devices.** There
is no accessibility or app-scope action to synthesise a hardware
Enter; `typeText` terminators must come through IME actions
(search/done key). `pressKey` throws explicitly rather than
silently dropping.

**Samsung-specific label noise.** `rootInActiveWindow` on Samsung
devices exposes Edge Panel and One UI overlays as clickable
elements; tree dumps carry them. Callers that expect "one app on
screen" must filter by package name or window kind host-side —
the APK returns the full layered tree.

**ADB forward is idempotent but not permanent.** A USB disconnect,
`adb kill-server`, or device reboot drops all forwards. Prefer
`reconnect()` over `disconnect()+connect()` — the former re-issues
the forward and re-runs the `/ping` handshake, surfacing capability
drift; the latter discards state the caller may still need.

## Gesture dispatch shape

`GestureRunner` applies two dispatch shapes depending on pointer
count:

- **Single-pointer**: multi-phase. Each adjacent waypoint pair
  becomes one `GestureDescription` dispatched sequentially,
  chained via `continueStroke(willContinue=true)` across dispatch
  calls. Hold segments (same-coordinate waypoints) emit a minimal
  `MIN_PRESS_MS` stroke then `Thread.sleep` the remainder — the
  sleep produces no platform events, so the pointer is truly
  stationary during the hold. This is the only shape that
  satisfies recognisers which reject on any MOVE event before a
  long-press timer fires (e.g. Flutter's
  `DelayedMultiDragGestureRecognizer`, which backs
  `ReorderableListView`).
- **Multi-pointer**: single dispatch, N parallel strokes. Each
  pointer becomes one stroke; the builder keeps their timing
  aligned so scale / rotate recognisers engage correctly.

Why both shapes are needed, and why the hold-as-sleep trick has
to span dispatches rather than stay inside a single
`GestureDescription`: a zero-length segment inside one
`GestureDescription` still produces periodic same-coordinate
`ACTION_MOVE` events at roughly 10ms throughout the stroke's
active window (platform interpolation regardless of path
length). Recognisers that reject on any first-move event —
Flutter's `DelayedMultiDragGestureRecognizer` is one — refuse
the gesture. Expressing the hold as a Kotlin sleep BETWEEN
dispatches keeps the silent window truly silent.

## Dispatch latency

Measured on a Samsung Galaxy S10 Lite (API 33, Android 13), three
runs per pattern:

| Pattern | Wire duration | Round-trip avg | Overhead |
|---|---|---|---|
| Tap | 50ms stroke | ~77ms | ~27ms |
| Drag (5 waypoints) | 500ms | ~522ms | ~22ms |
| Long-press | 800ms | ~823ms | ~23ms |
| Pinch (2 pointers) | 500ms | ~525ms | ~25ms |
| Reorder (hold 800ms + 400ms drag) | 1200ms | ~1430ms | ~230ms |

The per-gesture constant overhead (JSON parse + validation +
`dispatchGesture` + completion callback + HTTP response) sits at
~20-30ms. Multi-phase dispatch (reorder, any hold-then-drag)
accumulates that overhead per phase — a 4-phase gesture pays
~100ms extra on top of the author-requested durations, which is
negligible compared with the real-world gesture duration.

## Lifecycle edge cases

**Accessibility service disconnect.** Toggling Atomyx accessibility
off in Settings invalidates `AccessibilityService.instance`.
`AtomyxServicesHolder` detects the instance switch on the next
request and rebuilds `AtomyxServices` from scratch. Existing tree
caches are discarded. Tools that were mid-dispatch return
`serviceUnavailable`; callers should `reconnect()` after re-enabling.

**APK upgrade during a live session.** Installing a new build
kills `AtomyxForegroundService` and drops the HTTP port. The host
sees a dropped connection on the next call. `reconnect()` re-runs
`/ping` to pick up any capability changes the new build ships.

**`/ping` 404 compatibility.** Older APK builds that pre-date the
handshake return 404 on `/ping`. `AndroidDriver.connect()` swallows
the 404 and keeps conservative defaults (`canMultiPointer=false`,
`canPressure=false`). `reconnect()` does NOT — a connected session
that suddenly loses `/ping` signals a stale-binding issue worth
surfacing.

## Limitations

- `waitForIdle` is not exposed; `capabilities.canWaitForIdle=false`
  forces core into tree-diff polling. Adding a native primitive
  would require the APK to listen for `TYPE_WINDOW_CONTENT_CHANGED`
  and expose an "idle" signal.
- `screenSize` is derived from `/tree`'s first window-rooted child's
  bounds — the APK does not expose a dedicated `/screen-size` route.
  Accurate for portrait; landscape / rotated states inherit from
  the window bounds which stay authoritative.
- `deviceInfo` returns stubs (`platformVersion: "unknown"`). The
  APK has no `getprop`-backed route yet; add one if a tool needs
  SDK version or model details.
- `canPressure` is permanently false — the accessibility gesture
  surface exposes no per-touch pressure injection on any current
  Android level. Scripts that carry pressure are rejected at
  validation before reaching the device.

## Extension points

**Add a new route** (host-device wire method):

1. Create a new `Route` class in
   `platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/router/CommonRoutes.kt`.
2. Register it in `HttpControlServer.buildRoutes()`.
3. Add a TS-side wrapper in
   `packages/android-driver/src/android.driver.ts` that calls
   `this.http.get|post(...)` with typed args.
4. Cover both sides with tests — Kotlin validation in
   `GestureRunnerValidateTest`-style pure tests, TS wiring in
   `android.driver.test.ts` with a fake server.

**Add a new gesture backend.** Today `GestureRunner` calls
`AccessibilityService.dispatchGesture` directly. A second backend
(e.g. UiAutomation-based for rooted devices) would:

1. Extract a `GestureBackend` interface with the `dispatch` +
   `capabilities` pair.
2. Factory-select at `GestureRunner` construction based on root /
   SDK.
3. Surface via `/ping`'s `mechanism` field so host diagnostics
   can correlate.

The current single-backend design is intentional — Android has
one stable public API for gesture dispatch, and inventing the
interface before a second concrete backend exists is
maintenance overhead for no real gain.

## References

- `platforms/android-agent/README.md` — APK build, Gradle targets,
  permission granting flow.
- `pitfalls.md` — Android traps to avoid when editing the adapter.
- `tools.md` — cross-platform tool contract the driver plugs into.
- `ios.md` — iOS-side equivalents; the two drivers share the
  cross-platform `Driver` port contract, so pointer wire shapes
  and capability flags line up one-to-one.
