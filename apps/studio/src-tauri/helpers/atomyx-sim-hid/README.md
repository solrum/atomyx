# atomyx-sim-hid

Persistent helper binary that dispatches touch events into the iOS Simulator
via `SimDeviceLegacyHIDClient` — the same private SimulatorKit path
Simulator.app uses internally.

## What it does

The binary binds a localhost WebSocket server on an OS-assigned port and
emits one JSON handshake line to stdout when ready:

```
{"event":"listen","port":<port>,"transport":"ws-input"}
```

After that, stdout is silent. Gesture messages arrive as newline-delimited
JSON text frames from the client; each message receives a text-frame reply:

```json
{ "ok": true,  "id": 1 }
{ "ok": false, "id": 2, "error": "dispatch failed — check helper log for details" }
```

## Supported message types

```json
{ "type": "tap",        "x": 0.5, "y": 0.4, "holdMs": 50, "id": 1 }
{ "type": "swipe",      "x1": 0.5, "y1": 0.2, "x2": 0.5, "y2": 0.8,
                         "steps": 10, "stepMs": 16, "dwellMs": 0, "id": 2 }
{ "type": "touch-down", "x": 0.3, "y": 0.5, "id": 3 }
{ "type": "touch-move", "x": 0.35, "y": 0.5, "id": 3 }
{ "type": "touch-up",   "x": 0.4,  "y": 0.5, "id": 3 }
```

Coordinates are normalised [0..1] relative to the simulated screen.
`id` must be a unique integer per gesture sequence; multi-finger sessions
use distinct ids so iOS threads each touch through the HID stack correctly.

## HID dispatch recipe

Derived from baguette (Apache 2.0). Core path:

1. `IOHIDEventCreateDigitizerEvent` — build a parent digitizer event.
2. `IOHIDEventCreateDigitizerFingerEvent` — build a finger child, append via
   `IOHIDEventAppendEvent`.
3. `IndigoHIDMessageForTrackpadEventFromHIDEventRef` — wrap the IOHIDEvent
   pair into the Indigo message format SimulatorKit expects.
4. Patch two byte slots the wrapper leaves uninitialised:
   - offsets 0x6c and 0x10c → `UInt32(0x32)` (`IndigoHIDTouchTarget`)
   - offsets 0x3a/0x3b and 0xda/0xdb → edge bitmask (0x00 for interior touches)
5. `SimDeviceLegacyHIDClient.sendWithMessage:freeWhenDone:completionQueue:completion:`

## Xcode / iOS version behaviour

The private symbols (`SimDeviceLegacyHIDClient`, `IndigoHIDMessageForTrackpadEventFromHIDEventRef`,
`IOHIDEventCreateDigitizerEvent`, etc.) are verified against Xcode 26 / iOS 26.
On Xcode < 26 some or all of these symbols are absent from SimulatorKit;
`IndigoHIDClient.ensureWarm()` returns `false` and logs which symbols could
not be resolved. The WebSocket server still starts and every gesture receives
`{"ok":false}` so the orchestrator can detect the situation and fall back to
the XCUITest path without losing the connection.

## Usage

```
atomyx-sim-hid --udid <UDID> [--max-clients <N>]
```

`--udid` is required. `--max-clients` defaults to 4 (currently informational;
the server accepts all connections).

## Build

```
cd apps/studio/src-tauri/helpers/atomyx-sim-hid
./build.sh
```

Requires a Developer ID Application signing identity in the keychain. The
`ATOMYX_SIGN_IDENTITY` environment variable overrides the identity selection.
Output: `atomyx-sim-hid.app/` (signed bundle) and a `atomyx-sim-hid` symlink
pointing at the executable inside the bundle.

## Limitations

- macOS only; requires Xcode installed (for `xcode-select -p` and the
  SimulatorKit framework inside the Xcode bundle).
- Gesture dispatch requires the target simulator to be booted.
- Only interior touches are dispatched; edge-flagged gestures (home indicator
  swipe, control centre pull) are not wired in this binary — those remain
  on the XCUITest path for now.
