# Atomyx Android agent (Kotlin APK)

Standalone Android app that hosts an HTTP control server on the device.
The host-side Go Android driver, `core/internal/androiddriver`,
connects over `adb forward` and drives the target through the
framework's `Driver` port.

This is the **device side** of Atomyx. It runs on an Android phone/emulator,
exposes a localhost HTTP server (port 8765) that an MCP client on a host
machine drives over `adb forward`. The server provides:

- Full hierarchical UI tree dumps (resourceId, contentDesc, text,
  hint, and state booleans per node). Selector resolution happens
  host-side against this tree
- Gesture dispatch (tap, long-press, tap-at-coords, swipe, arbitrary
  multi-pointer gestures, type-via-keyboard with on-screen-keys
  fallback for custom Flutter keypads, clear focused input via an
  ordered strategy chain)
- Soft keyboard detection + per-key tap typing
- Screenshot capture
- Foreground app tracking
- App lifecycle: launch, force-stop, list installed

## Architecture

```
app/src/main/java/dev/atomyx/agent/
├── service/
│   └── AtomyxAccessibilityService.kt   ← slim a11y service
├── ui/
│   └── MainActivity.kt               ← single-screen launcher
└── control/
    ├── HttpControlServer.kt          ← thin NanoHTTPD dispatcher
    ├── AtomyxForegroundService.kt      ← lifecycle + persistent notification
    ├── AtomyxServices.kt               ← DI container (per a11y instance)
    ├── UiTreeService.kt              : event-invalidated raw tree dump,
    │                                   the wire form GET /tree serves;
    │                                   dumpCompact stays internal, used
    │                                   only by the on-screen-key fallback
    ├── GestureRunner.kt              ← completion-aware dispatch pipeline
    │                                   (CountDownLatch 30s, validation,
    │                                   multi-stroke support)
    ├── GestureDispatcher.kt          : tap / long-press primitives,
    │                                   single-pointer, delegate to
    │                                   GestureRunner
    ├── KeyboardTyper.kt              : typeViaKeyboard, with an
    │                                   on-screen-keys fallback for
    │                                   custom Flutter keypads
    ├── KeyboardHider.kt              : dismiss the on-screen keyboard
    ├── FocusedInputClearer.kt        : clear the focused field via the
    │                                   clear/ strategy chain
    ├── SystemActionDispatcher.kt     ← pressKey / launchApp / forceStopApp
    ├── PermissionChecker.kt          ← permission state helpers
    ├── clear/                        : ordered clear-text strategy chain
    │   ├── ClearTextChain.kt
    │   ├── SetTextStrategy.kt
    │   ├── LongPressSelectAllStrategy.kt
    │   ├── SelectAllKeyEventStrategy.kt
    │   └── KeypadBackspaceStrategy.kt
    └── router/
        ├── Route.kt                  ← endpoint interface
        ├── Router.kt                 ← dispatcher
        └── CommonRoutes.kt           ← all endpoints as classes
```

## Build

```bash
cd platforms/android-agent
./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Min SDK 26, target 34. Tested on Pixel 6 (API 33).

## First-run setup

1. Open the **Atomyx** app on the device
2. Tap **Open Accessibility settings**, then toggle Atomyx ON
3. Tap **Open Usage Access settings**, then grant Atomyx
4. Return to Atomyx, tap **Enable Atomyx control server**
5. A persistent notification appears: "atomyx control server active"

You can also bootstrap from the host via adb:

```bash
PKG=dev.atomyx.agent
SVC=$PKG/$PKG.service.AtomyxAccessibilityService

# Bootstrap permissions
adb shell appops set $PKG android:get_usage_stats allow
adb shell pm grant $PKG android.permission.POST_NOTIFICATIONS

# Toggle accessibility ON. After fresh install or APK update, you MUST
# delete the setting first, because Android caches a stale binding to the
# previous APK signature and `service.windows` will return empty until
# re-bound.
adb shell 'settings delete secure enabled_accessibility_services'
sleep 1
adb shell settings put secure enabled_accessibility_services "$SVC"
sleep 1

adb shell am start-foreground-service -n $PKG/.control.AtomyxForegroundService
```

> **Note**: This delete-then-put dance is an Android quirk for accessibility
> services after APK updates. End users installing once via the Play Store
> won't hit it. Developers iterating on the APK will, so running the snippet
> above is the workaround. The host-side `launch_app` tool preflights for
> this stale-binding state and returns an actionable rebind hint.

## Connect from host

```bash
adb forward tcp:8765 tcp:8765
curl http://127.0.0.1:8765/ping
# {"ok":true,"agent":"atomyx-android","sdkInt":34,"mechanism":"accessibility",
#  "capabilities":{"canMultiPointer":false,"canPressure":false}}
```

## HTTP API

| Method | Path | Body | Description |
|---|---|---|---|
| GET  | `/health`                        | None | Liveness check that works before accessibility connects |
| GET  | `/ping`                          | None | Handshake: liveness + gesture capabilities |
| GET  | `/tree`                          | None | Full hierarchical UI tree dump |
| GET  | `/keyboard`                      | None | IME state + key list with bounds |
| GET  | `/screenshot`                    | None | base64 JPEG |
| GET  | `/current-activity`              | None | Foreground package + activity |
| GET  | `/apps`                          | None | Installed apps |
| POST | `/actions/tap_coords`            | `{x, y}` | Tap raw coordinates |
| POST | `/actions/long_press`            | `{x, y, durationMs?}` | Long-press at coordinates |
| POST | `/actions/dispatch_gesture`      | `{pointers: [...]}` | Arbitrary multi-pointer gesture, one or more time-ordered waypoint lists |
| POST | `/actions/clear_focused_input`   | None | Clear the currently focused input via an ordered strategy chain |
| POST | `/actions/hide_keyboard`         | None | Dismiss the on-screen IME |
| POST | `/actions/swipe`                 | `{fromX, fromY, toX, toY, durationMs?}` | Swipe |
| POST | `/actions/type_keyboard`         | `{text, perKeyDelayMs?}` | Type via on-screen keyboard taps (with on-screen-keys fallback) |
| POST | `/actions/key`                   | `{key: "back"\|"home"\|"enter"}` | Press system key |
| POST | `/actions/launch`                | `{packageName, forceStop?}` | Launch app |
| POST | `/actions/force_stop`            | `{packageName}` | Force-stop app |

> The host side of Atomyx uses platform-neutral names (`appId`,
> `currentForeground()`). The `appId` (Android's `packageName`)
> translation happens in `core/internal/androiddriver/driver.go` at
> the HTTP boundary so the tool layer never sees Android-specific
> terminology.

### Selector resolution

The agent has no `/resolve` endpoint and no on-device selector
strategy chain. `GET /tree` returns the raw hierarchy (`resourceId`,
`contentDesc`, `text`, `hintText`, and the `clickable` / `enabled` /
`focused` state booleans per node, defined in
`core/internal/transport/android_types.go`), and every action route
takes coordinates, not a selector.

Selector resolution against that tree happens entirely host-side in
`core/internal/orchestra/priority_broadening.go`, using Atomyx's
cross-platform priority order: `id`, then `label`, then `text` (with
a fallback match against `label` when the caller gave `text` but no
`label`), then `value`, then `hint`. First match wins. The Android
driver's `core/internal/androiddriver` normalizes `resourceId` into
`id` and `contentDesc` into `label` before the tree reaches that
policy, so the same compiled selector logic drives every platform.

### Flutter / Compose / RN notes

- **Flutter exposes ids without the Android-native prefix.** Android's
  native `findAccessibilityNodeInfosByViewId` only accepts
  fully-qualified `package:id/name`, but Flutter Semantics ids arrive
  unprefixed (for example "G01-05-01/2"). `UiTreeService` reports the
  raw `resourceId` as-is over `GET /tree`, and the host-side `id`
  match in `core/internal/orchestra` is an exact string or regex
  match against exactly that value, with no prefix or suffix
  stripping. Match the unprefixed id directly, or use a regex
  selector when only a suffix is stable.
- **`UiTreeService.dumpCompact` keeps anything with a stable signal**
  (resourceId / contentDesc / text / clickable). Do not revert this to
  "clickable or labeled only", because Flutter elements frequently have only a
  resourceId.
- **The `clickable` flag is unreliable on Flutter / Compose / RN.** Those
  frameworks dispatch gestures in-engine via `GestureDetector` without
  setting the a11y clickable flag. Tools ignore the flag when deciding
  whether to tap. Do not add a clickable filter.
- **`typeViaKeyboard` has an on-screen-keys fallback.** When the standard
  IME path fails (custom Flutter keypads in banking apps), the dispatcher
  scans the compact tree for short labels and taps them directly.
- **`clearFocusedInput` has a backspace fallback.** `ACTION_SET_TEXT` is
  rejected on Flutter, so the fallback sends on-screen backspace taps via a
  structural backspace-key match.

## Extending

See [../../.claude/docs/development.md](../../.claude/docs/development.md) for the full extension
checklist. Quick summary:

### Add a new endpoint

1. Implement `Route` in `app/src/main/java/dev/atomyx/agent/control/router/`
   (new file, or add to
   `app/src/main/java/dev/atomyx/agent/control/router/CommonRoutes.kt`).
2. Register the class in `HttpControlServer.buildRoutes()`.
3. Add the request and response types to
   `core/internal/transport/android_types.go`, the client-side call
   to `core/internal/transport/http_client.go`, and the method to the
   `Transport` interface in `core/internal/androiddriver/transport.go`.
4. If the capability should reach the tool layer, add a method to
   the `Driver` port in `core/internal/driver/driver.go` and
   implement it in `core/internal/androiddriver`,
   `core/internal/iosdriver`, and `core/internal/iossimdriver`.

```kotlin
class MyRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/my-endpoint"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        return RouteResponse.ok("""{"ok":true}""")
    }
}
```

### Add a new selector type

Selector resolution is host-side only. The agent carries no
per-selector-type code. Adding a new selector field:

1. Add the field to `Selector` in `core/internal/orchestra/selector.go`.
2. Add its priority position to `CompileSelector` in
   `core/internal/orchestra/priority_broadening.go`.
3. If the field's value is not already in the tree the agent
   reports, add it to `UiTreeService`'s node dump on the device side
   and to `AndroidTreeNode` in
   `core/internal/transport/android_types.go`, then map it into the
   matching attribute in `NormalizeAndroidTree`
   (`core/internal/androiddriver/normalize.go`).
4. **Plan the iOS mapping** in `.claude/docs/ios.md`.

## License

MIT. See [../../LICENSE](../../LICENSE).
