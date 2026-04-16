# @atomyx/core-driver-android

Android driver for Atomyx. Implements the `Driver` port from
`@atomyx/core-driver` by talking to the Kotlin APK in
[`native/android-agent/`](../../native/android-agent/) over HTTP via
`adb forward`. The APK exposes a localhost control server on
port 8765; this package opens an `adb forward` tunnel and connects
through it.

Deliberately thin. All selector resolution, scroll-into-view,
obscurement detection, and priority broadening live in
`@atomyx/core-driver`. This driver only exposes transport + tree
normalization + primitive dispatch.

## What's inside

- **`AndroidDriver`** (`src/android.driver.ts`) — the `Driver`
  impl. Wraps `HttpClient` to dispatch method calls as HTTP
  POSTs, converts Android raw tree → canonical `TreeNode` via
  the normalizer.
- **`HttpClient`** (`src/http-client.ts`) — small `fetch` wrapper
  with timeout, typed `HttpClientError`, handles JSON framing.
- **`adb`** (`src/adb.ts`) — minimal wrapper around the `adb`
  CLI for `forward` lifecycle + device enumeration. Typed
  `AdbError` on missing binary or unreachable device.
- **`normalizeAndroidTree`** (`src/tree-normalizer.ts`) —
  legacy `RawElementDto` → canonical `TreeNodeWire`. Maps
  `resourceId` → `id`, `contentDesc` → `label`, `text` → `text`,
  `className` → `class` + derived `role`, `BoundsDto` → `bounds`
  string. `classNameToRole` whitelist covers Android widgets +
  Compose containers.

## Usage

```ts
import { AndroidDriver } from "@atomyx/core-driver-android";
import { Orchestra, SystemClock } from "@atomyx/core-driver";

const driver = new AndroidDriver({ serial: "emulator-5554" });
await driver.connect();

const orchestra = new Orchestra({
  driver,
  clock: new SystemClock(),
});
await orchestra.launchApp("com.example.app");
```

## Capabilities

| Flag | Value |
|---|---|
| `canScreenshot` | `true` |
| `canEraseText` | `false` — host fallback: press delete N times |
| `canWaitForIdle` | `false` — host polls tree-diff |
| `canSetLocation` | `false` |
| `canSetOrientation` | `false` |

`supportedKeyCodes`: `back`, `home`, `enter`. Android system key
dispatch always "fires" — the driver reports `ok: true`
unconditionally.

## Prerequisites

- `adb` on PATH (Android platform-tools, `brew install --cask android-platform-tools`)
- The Atomyx APK installed and running on the target device or
  emulator. See [`native/android-agent/README.md`](../../native/android-agent/README.md)
  for the install + accessibility permission flow.

## Legacy wire protocol note

The Kotlin APK currently exposes its original wire shape
(`resourceId`, `contentDesc`, `BoundsDto`) at legacy routes
(`/tree`, `/actions/tap_coords`, `/actions/swipe`). The
canonical `@atomyx/core-driver-wire` routes (`/hierarchy`,
`/gesture/tap`, ...) are NOT yet served by the APK. This driver
bridges the gap by calling the legacy routes and normalizing
responses host-side in `tree-normalizer.ts`.

When the APK migrates to emit canonical wire shapes directly,
this normalizer shrinks to a pass-through. Until then, the
translation layer is here in TypeScript.

## Dependencies

- `@atomyx/core-driver` — the `Driver` port + types
- `@atomyx/core-driver-wire` — `TreeNodeWire` schema

Uses Node's built-in `fetch` (Node 20+). No external runtime deps
beyond the workspace peers.

## See also

- [`native/android-agent/`](../../native/android-agent/) — Kotlin APK
- [`.claude/docs/development.md`](../../.claude/docs/development.md) — Android setup + rebinding after APK update
- [`.claude/docs/pitfalls.md`](../../.claude/docs/pitfalls.md) — Android traps
