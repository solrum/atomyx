# adet — Android control plane

Standalone Android app that hosts the HTTP control server the [adet MCP
server](../README.md) connects to.

> This is the **Android-specific** control plane. iOS support is a **TODO** —
> the bridge approach has not been decided yet. See
> [../docs/ios.md](../docs/ios.md) for the open design questions.

This is the **device side** of adet. It runs on an Android phone/emulator,
exposes a localhost HTTP server (port 8765) that an MCP client on a host
machine drives over `adb forward`. The server provides:

- Live UI tree dumps (compact form with stable selectors + inline coords)
- Element resolution by selector (resourceId, contentDesc, text,
  textContains, hint)
- Gesture dispatch (tap, long-press, tap-at-coords, swipe, input text,
  type-via-keyboard with on-screen-keys fallback for custom Flutter
  keypads, clear focused input with backspace fallback)
- Soft keyboard detection + per-key tap typing
- Screenshot capture
- Foreground app tracking
- App lifecycle: launch, force-stop, list installed

## Architecture

```
android/app/src/main/java/dev/solrum/adet/agent/
├── service/
│   └── AdetAccessibilityService.kt   ← slim a11y service
├── ui/
│   └── MainActivity.kt               ← single-screen launcher
└── control/
    ├── HttpControlServer.kt          ← thin NanoHTTPD dispatcher
    ├── AdetForegroundService.kt      ← lifecycle + persistent notification
    ├── AdetServices.kt               ← DI container (per a11y instance)
    ├── UiTreeService.kt              ← event-invalidated tree dump cache;
    │                                   dumpCompact keeps anything with a
    │                                   stable signal (resourceId /
    │                                   contentDesc / text / clickable)
    ├── GestureDispatcher.kt          ← tap / tapAt / longPressAt / swipe /
    │                                   inputText / typeViaKeyboard
    │                                   (+ typeViaOnScreenKeys fallback for
    │                                   custom Flutter keypads) /
    │                                   clearFocusedInput (+ backspace
    │                                   fallback) / forceStopApp
    ├── SelectorResolver.kt           ← strategy-chain selector lookup
    ├── PermissionChecker.kt          ← permission state helpers
    ├── router/
    │   ├── Route.kt                  ← endpoint interface
    │   ├── Router.kt                 ← dispatcher
    │   └── CommonRoutes.kt           ← all endpoints as classes
    └── strategy/
        ├── ResolutionStrategy.kt     ← interface
        ├── ResourceIdStrategy.kt     ← native indexed lookup + WALK
        │                               FALLBACK for Flutter / Compose / RN
        │                               non-qualified ids (native lookup
        │                               requires `package:id/name`)
        ├── TextStrategy.kt           ← native text lookup (+ TextContains)
        ├── ContentDescStrategy.kt    ← tree walk
        └── HintStrategy.kt           ← fuzzy multi-source
```

## Build

```bash
cd android
./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Min SDK 26, target 34. Tested on Pixel 6 (API 33).

## First-run setup

1. Open the **adet** app on the device
2. Tap **Open Accessibility settings** → toggle adet ON
3. Tap **Open Usage Access settings** → grant adet
4. Return to adet, tap **Enable adet control server**
5. A persistent notification appears: "adet — control server active"

You can also bootstrap from the host via adb:

```bash
PKG=dev.solrum.adet.agent
SVC=$PKG/$PKG.service.AdetAccessibilityService

# Bootstrap permissions
adb shell appops set $PKG android:get_usage_stats allow
adb shell pm grant $PKG android.permission.POST_NOTIFICATIONS

# Toggle accessibility ON. After fresh install or APK update, you MUST
# delete the setting first — Android caches a stale binding to the previous
# APK signature and `service.windows` will return empty until re-bound.
adb shell 'settings delete secure enabled_accessibility_services'
sleep 1
adb shell settings put secure enabled_accessibility_services "$SVC"
sleep 1

adb shell am start-foreground-service -n $PKG/.control.AdetForegroundService
```

> **Note**: This delete-then-put dance is an Android quirk for accessibility
> services after APK updates. End users installing once via the Play Store
> won't hit it. Developers iterating on the APK will — running the snippet
> above is the workaround. The host-side `launch_app` tool preflights for
> this stale-binding state and returns an actionable rebind hint.

## Connect from host

```bash
adb forward tcp:8765 tcp:8765
curl http://127.0.0.1:8765/health
# {"ok":true,"accessibilityConnected":true}
```

## HTTP API

| Method | Path | Body | Description |
|---|---|---|---|
| GET  | `/health`                        | — | Liveness + accessibility state |
| GET  | `/tree?format=compact`           | — | UI tree dump (compact, with stable signals only) |
| GET  | `/keyboard`                      | — | IME state + key list with bounds |
| GET  | `/screenshot`                    | — | base64 PNG |
| GET  | `/current-activity`              | — | Foreground package + activity |
| GET  | `/apps`                          | — | Installed apps |
| POST | `/resolve`                       | `{selector}` | Resolve selector → element metadata |
| POST | `/actions/tap`                   | `{selector}` | Tap an element |
| POST | `/actions/tap_coords`            | `{x, y}` | Tap raw coordinates (rejected if inside IME bounds) |
| POST | `/actions/long_press`            | `{x, y, durationMs?}` | Long-press at coordinates |
| POST | `/actions/clear_focused_input`   | — | Clear currently focused EditText (with backspace fallback) |
| POST | `/actions/swipe`                 | `{fromX, fromY, toX, toY, durationMs?}` | Swipe |
| POST | `/actions/input`                 | `{selector, text}` | Set text on an EditText |
| POST | `/actions/type_keyboard`         | `{text, perKeyDelayMs?}` | Type via on-screen keyboard taps (with on-screen-keys fallback) |
| POST | `/actions/key`                   | `{key: "back"\|"home"\|"enter"}` | Press system key |
| POST | `/actions/launch`                | `{packageName, forceStop?}` | Launch app |
| POST | `/actions/force_stop`            | `{packageName}` | Force-stop app |

> The host side of adet uses platform-neutral names (`appId`,
> `currentForeground()`). The `appId` ↔ `packageName` translation happens
> in `src/adapters/agent-direct.adapter.ts` at the HTTP boundary so the
> tool layer never sees Android-specific terminology.

### Selector shape

```json
{
  "selector": {
    "resourceId": "com.example:id/btn_login",
    "contentDesc": "Login button",
    "text": "Sign in",
    "textContains": "ign",
    "hint": "login",
    "nth": 0
  }
}
```

Strategies are tried in this priority order (fastest + most specific first):
`resourceId` → `text` → `contentDesc` → `textContains` → `hint`. First match
wins.

> **Note on host-side broadening**: the device-side order above is the raw
> strategy chain. The host-side `SelectorResolutionPipeline` in
> `src/tools/core/` applies a different priority
> (`resourceId` → `contentDesc` → `text` → `textContains` → `hint`) when
> retrying a failed resolve, so the caller gets Android Material
> `contentDesc`-first broadening without knowing the platform convention.

### Flutter / Compose / RN notes

- **`ResourceIdStrategy` walks the tree as a fallback.** Android's native
  `findAccessibilityNodeInfosByViewId` only accepts fully-qualified
  `package:id/name`. Flutter exposes ids like `G01-05-01/2` without a
  prefix, so when the native lookup returns empty the strategy walks the
  tree and matches by `viewIdResourceName == rid` OR `endsWith("/$rid")` OR
  `endsWith(rid)`.
- **`UiTreeService.dumpCompact` keeps anything with a stable signal**
  (resourceId / contentDesc / text / clickable). Do not revert this to
  "clickable or labeled only" — Flutter elements frequently have only a
  resourceId.
- **The `clickable` flag is unreliable on Flutter / Compose / RN.** Those
  frameworks dispatch gestures in-engine via `GestureDetector` without
  setting the a11y clickable flag. Tools ignore the flag when deciding
  whether to tap; do not add a clickable filter.
- **`typeViaKeyboard` has an on-screen-keys fallback.** When the standard
  IME path fails (custom Flutter keypads in banking apps), the dispatcher
  scans the compact tree for short labels and taps them directly.
- **`clearFocusedInput` has a backspace fallback.** `ACTION_SET_TEXT` is
  rejected on Flutter; the fallback sends on-screen backspace taps via a
  structural backspace-key match.

## Extending

See [../docs/development.md](../docs/development.md) for the full extension
checklist. Quick summary:

### Add a new endpoint

1. Implement `Route` in `control/router/` (new file or add to `CommonRoutes.kt`)
2. Register the class in `HttpControlServer.buildRoutes()`
3. Add the client-side call in `src/adapters/agent-direct.adapter.ts`
4. If the capability should reach the tool layer, add a method to the
   relevant sub-interface in `src/adapters/device-controller.port.ts` and
   implement it in **both** the Android adapter and the iOS stub.

```kotlin
class MyRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/my-endpoint"
    override fun handle(request: RouteRequest, services: AdetServices): RouteResponse {
        return RouteResponse.ok("""{"ok":true}""")
    }
}
```

### Add a new selector type

1. Add field to `SelectorResolver.Selector`
2. Implement `ResolutionStrategy` in a new file under `control/strategy/`
3. Add it to `SelectorResolver.defaultStrategies()` in the desired priority
   position
4. Also add the field to the host-side `Selector` type in
   `src/adapters/device-controller.port.ts`
5. **Plan the iOS mapping** in `docs/ios.md`

```kotlin
class MyStrategy : ResolutionStrategy {
    override val name = "myType"
    override fun canResolve(selector: SelectorResolver.Selector): Boolean = ...
    override fun resolve(selector, service): List<AccessibilityNodeInfo> = ...
}
```

## License

Apache 2.0 — see [../LICENSE](../LICENSE).
