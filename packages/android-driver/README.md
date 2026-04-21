# @atomyx/android-driver

Android driver for Atomyx. Implements the `Driver` port from
`@atomyx/driver` by talking to the Kotlin APK in
[`platforms/android-agent/`](../../platforms/android-agent/) over
HTTP via `adb forward`. The APK exposes a localhost control server
on port 8765; this package opens an `adb forward` tunnel and
connects through it.

Deliberately thin. All selector resolution, scroll-into-view,
obscurement detection, priority broadening, and observation-driven
wait primitives live in `@atomyx/driver`. This driver only exposes
transport + tree normalization + primitive dispatch.

## What's inside

- **`AndroidDriver`** (`src/android.driver.ts`) — the `Driver`
  implementation. Wraps `HttpClient` to dispatch method calls as
  HTTP POSTs and converts the APK's raw tree into the canonical
  `TreeNode` via the normalizer.
- **`HttpClient`** (`src/http-client.ts`) — small `fetch` wrapper
  with timeout, typed `HttpClientError`, handles JSON framing.
- **`adb`** (`src/adb.ts`) — minimal wrapper around the `adb`
  CLI for `forward` lifecycle + device enumeration. Typed
  `AdbError` on missing binary or unreachable device.
- **`normalizeAndroidTree`** (`src/tree-normalizer.ts`) —
  translates the APK's `RawElementDto` into the canonical
  `TreeNodeWire`. Maps `resourceId` → `id`, `contentDesc` →
  `label`, `text` → `text`, `className` → `class` + derived
  `role`, `BoundsDto` → `bounds` string. Plumbs `focused` and
  the `ext:isIme` IME-root marker so host-side observable-state
  helpers can find the focused element and the keyboard without
  extra RPCs.

## Usage

```ts
import { AndroidDriver } from "@atomyx/android-driver";
import { Orchestra, SystemClock } from "@atomyx/driver";

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
| `canEraseText` | `true` — backed by the APK's atomic clear route |
| `canHideKeyboard` | `true` — backed by the APK's hide-keyboard route |
| `canWaitForIdle` | `false` — host polls `waitForTreeStable` |
| `canSetLocation` | `false` |
| `canSetOrientation` | `false` |

`supportedKeyCodes`: `back`, `home`, `enter`. Android system key
dispatch always "fires" — the driver reports `ok: true`
unconditionally.

## Prerequisites

- `adb` on PATH (Android platform-tools).
- The Atomyx APK installed and running on the target device or
  emulator. See
  [`platforms/android-agent/README.md`](../../platforms/android-agent/README.md)
  for the install + accessibility permission flow.

## Wire protocol

The APK exposes `/tree`, `/keyboard`, `/actions/*`, `/health`,
etc. over HTTP. This driver reshapes responses into the canonical
`TreeNodeWire` shape host-side in `tree-normalizer.ts` so the core
framework operates on one uniform tree regardless of platform.
The translation layer is the single call site that knows any
Android-specific field name.

## Dependencies

- `@atomyx/driver` — the `Driver` port + types.
- `@atomyx/driver-wire` — `TreeNodeWire` schema.

Uses Node's built-in `fetch` (Node 20+). No external runtime deps
beyond the workspace peers.

## See also

- [`platforms/android-agent/`](../../platforms/android-agent/) — Kotlin APK.
- [`.claude/docs/development.md`](../../.claude/docs/development.md) — Android setup + rebinding after APK update.
- [`.claude/docs/pitfalls.md`](../../.claude/docs/pitfalls.md) — Android traps.
