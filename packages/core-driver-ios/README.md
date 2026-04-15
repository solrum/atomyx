# @atomyx/core-driver-ios

iOS driver for Atomyx. Implements the `Driver` port from
`@atomyx/core-driver` by talking to the Swift XCUITest runner in
[`native/ios-driver/`](../../native/ios-driver/) over line-delimited
JSON on TCP `127.0.0.1:22087`. Simulator uses direct loopback;
physical devices use an `iproxy` (libimobiledevice) usbmux tunnel.

Deliberately thin. All selector resolution, scroll-into-view,
obscurement detection, and priority broadening live in
`@atomyx/core-driver`. This driver only exposes transport +
tree normalization + primitive dispatch.

## What's inside

- **`IosDriver`** (`src/ios.driver.ts`) — the `Driver` impl.
  Constructs a `TcpClient`, optionally an `Iproxy` tunnel (for
  physical devices), and dispatches method calls as JSON commands
  to the Swift runner.
- **`TcpClient`** (`src/tcp-client.ts`) — line-delimited JSON TCP
  client with id correlation, pending waiter map, reconnect,
  typed `TcpClientError` (not-connected / driver-error /
  disconnected / parse-error / timeout).
- **`Iproxy`** (`src/iproxy.ts`) — libimobiledevice tunnel
  lifecycle. Probe-handshake reuse (recognizes an existing Atomyx
  driver and avoids duplicate spawn), `ensurePortFree` bind-conflict
  detection, `waitForTunnelUp` poll-based readiness check (no
  timeout heuristic), typed `IproxyError`.
- **`normalizeIosTree`** (`src/tree-normalizer.ts`) — iOS raw
  snapshot → canonical `TreeNodeWire`. Maps `elementType` to
  canonical `Role`, mirrors `label` to `text` where appropriate,
  derives `clickable` from the interactive-type whitelist.

## Usage

```ts
import { IosDriver } from "@atomyx/core-driver-ios";
import { Orchestra, SystemClock } from "@atomyx/core-driver";

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
| `canEraseText` | `true` — Swift `clearFocusedInput` |
| `canWaitForIdle` | `false` — host polls tree-diff |
| `canSetLocation` | `false` |
| `canSetOrientation` | `false` |

`supportedKeyCodes`: `back`, `home`, `enter`. iOS `back` may
return `ok: false` since there is no system back primitive —
host-side policy falls back to finding an on-screen Cancel /
Close affordance.

## Prerequisites

- **Simulator**: Xcode 15+, one booted simulator, the Swift runner
  built + serving via `make serve` in `native/ios-driver/`.
- **Physical device**: Xcode + Apple Developer team, libimobiledevice
  (`brew install libimobiledevice`), the Swift runner built via
  `make build-device` and serving via `make serve-device`.

See [`native/ios-driver/README.md`](../../native/ios-driver/README.md)
for the Swift-side setup.

## Dependencies

- `@atomyx/core-driver` — the `Driver` port + types
- `@atomyx/core-driver-wire` — `TreeNodeWire` schema

No external runtime deps. Uses Node's built-in `net` for TCP.

## See also

- [`native/ios-driver/`](../../native/ios-driver/) — Swift XCUITest runner
- [`.claude/docs/ios.md`](../../.claude/docs/ios.md) — iOS bridge decision log
- [`.claude/docs/pitfalls.md`](../../.claude/docs/pitfalls.md) — known iOS traps
