# @atomyx/ios-driver

iOS driver for Atomyx. Implements the `Driver` port from
`@atomyx/driver` by talking to the Swift XCUITest runner in
[`platforms/ios-agent/`](../../platforms/ios-agent/) over
line-delimited JSON on TCP `127.0.0.1:22087`. Simulator uses
direct loopback; physical devices use an `iproxy` (libimobiledevice)
usbmux tunnel.

Deliberately thin. All selector resolution, scroll-into-view,
obscurement detection, priority broadening, and observation-driven
wait primitives live in `@atomyx/driver`. This driver only exposes
transport + tree normalization + primitive dispatch.

## What's inside

- **`IosDriver`** (`src/ios.driver.ts`) — the `Driver`
  implementation. Constructs a `TcpClient`, optionally an
  `Iproxy` tunnel (for physical devices), and dispatches method
  calls as JSON commands to the Swift runner.
- **`TcpClient`** (`src/tcp-client.ts`) — line-delimited JSON TCP
  client with id correlation, pending waiter map, reconnect,
  typed `TcpClientError` (not-connected / driver-error /
  disconnected / parse-error / timeout).
- **`Iproxy`** (`src/iproxy.ts`) — libimobiledevice tunnel
  lifecycle. Probe-handshake reuse recognizes an existing Atomyx
  driver tunnel and avoids duplicate spawn; `ensurePortFree`
  detects bind conflicts; `waitForTunnelUp` polls for readiness
  (no timeout heuristic); typed `IproxyError`.
- **`normalizeIosTree`** (`src/tree-normalizer.ts`) — iOS raw
  snapshot → canonical `TreeNodeWire`. Maps `elementType` to
  canonical `Role`, mirrors `label` to `text` where appropriate,
  derives `clickable` from the interactive-type whitelist,
  plumbs `focused` from the snapshot's `hasFocus` signal.

## Usage

```ts
import { IosDriver } from "@atomyx/ios-driver";
import { Orchestra, SystemClock } from "@atomyx/driver";

// Simulator
const driver = new IosDriver({ kind: "simulator", udid: "..." });

// Physical device
// const driver = new IosDriver({ kind: "device", udid: "00008101-..." });

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
| `canEraseText` | `true` — Swift `clearFocusedInput` uses ⌘A + ⌫ with exact-length delete-loop fallback |
| `canHideKeyboard` | `true` — Swift `hideKeyboard` command |
| `canWaitForIdle` | `false` — host polls `waitForTreeStable` |
| `canSetLocation` | `false` |
| `canSetOrientation` | `false` |

`supportedKeyCodes`: `back`, `home`, `enter`. iOS `back` may
return `ok: false` since there is no system back primitive —
the host falls back to finding an on-screen Cancel / Close
affordance.

**Fast text clear**: `ClearFocusedInputCommand` issues ⌘A + ⌫
when the simulator has hardware-keyboard pairing on (O(1)
keystrokes). When the shortcut is ignored, the command reads the
focused field's `value.count` from `app.snapshot()` and dispatches
exactly that many delete keys.

## Prerequisites

- **Simulator**: Xcode 15+, one booted simulator, the Swift
  runner built + serving via `make serve` in
  `platforms/ios-agent/`. For the ⌘A clear fast path, enable
  hardware-keyboard pairing (`xcrun simctl keyboard <udid> enable
  hardware`, or Simulator I/O → Keyboard menu).
- **Physical device**: Xcode + Apple Developer team,
  libimobiledevice (`brew install libimobiledevice`), the Swift
  runner built via `make build-device` and serving via
  `make serve-device`.

See [`platforms/ios-agent/README.md`](../../platforms/ios-agent/README.md)
for the Swift-side setup.

## Dependencies

- `@atomyx/driver` — the `Driver` port + types.
- `@atomyx/driver-wire` — `TreeNodeWire` schema.

No external runtime deps. Uses Node's built-in `net` for TCP.

## See also

- [`platforms/ios-agent/`](../../platforms/ios-agent/) — Swift XCUITest runner.
- [`.claude/docs/ios.md`](../../.claude/docs/ios.md) — iOS bridge decision log.
- [`.claude/docs/pitfalls.md`](../../.claude/docs/pitfalls.md) — known iOS traps.
