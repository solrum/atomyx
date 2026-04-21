# Atomyx iOS agent (Swift XCTest runner)

Swift XCUITest bundle that runs inside an iOS simulator or on a
physical device and exposes a line-delimited JSON command protocol
on `127.0.0.1:22087`. The host-side `@atomyx/ios-driver` TypeScript
package connects over TCP (loopback for simulator, `iproxy` usbmux
tunnel for device) and drives the target through the framework's
`Driver` port.

## Command surface

Covers the full `Driver` primitive set:

| Command | Purpose |
|---|---|
| `ping` | Liveness / handshake |
| `launchApp` | Bring an app to foreground |
| `forceStopApp` | Kill a running app |
| `dumpRawTree` | Full hierarchy for host-side normalization |
| `tapAt` | Coordinate tap |
| `longPressAt` | Coordinate long-press |
| `swipe` | Two-point swipe with duration |
| `pressKey` | back / home / enter / tab / escape |
| `screenshot` | PNG base64 |
| `typeText` | Native `typeText` into the focused field |
| `clearFocusedInput` | ⌘A + ⌫ fast path, exact-length delete-loop fallback |
| `hideKeyboard` | Dismiss the on-screen keyboard |
| `getKeyboard` | Inspect the visible keyboard |
| `getScreenSize` | `app.frame` in points |

Selector resolution, scroll-into-view, obscurement detection,
priority broadening, and observation-driven wait primitives live
**host-side** in `@atomyx/driver`. The Swift driver never receives
a `Selector` — it only speaks coordinates.

## Prerequisites

```bash
brew install xcodegen
xcode-select -p   # Xcode command-line tools present
```

- Xcode 15+
- iOS 16 SDK
- For **simulator**: one booted simulator
  (`xcrun simctl list devices booted`). Enable hardware-keyboard
  pairing (`xcrun simctl keyboard <udid> enable hardware`, or
  Simulator I/O → Keyboard menu) so the ⌘A clear fast path works.
- For **physical device**: Apple Developer team ID,
  `brew install libimobiledevice` for `iproxy`.

## Run (simulator)

```bash
cd platforms/ios-agent
make setup       # xcodegen + xcodebuild build-for-testing
make serve       # starts serving; blocks terminal
```

Expected log: `[atomyx] driver listening on 127.0.0.1:22087`

Then in another terminal:

```bash
atomyx mcp --platform ios --kind simulator
```

## Run (physical device)

One-time setup per contributor:

```bash
cp device.env.example device.env
$EDITOR device.env    # set DEVICE_UDID + DEV_TEAM (+ optional BUNDLE_ID)
```

```bash
make setup-device    # xcodegen + build with device.env code signing
make serve-device    # spawns iproxy tunnel + xcodebuild test-without-building
```

Then:

```bash
atomyx mcp --platform ios --kind device --device <udid>
```

## Layout

```
platforms/ios-agent/
├── project.yml          XcodeGen config
├── Makefile             setup / build / test / serve targets
├── device.env.example   per-contributor device config template
├── App/
│   ├── AtomyxHostApp.swift       Minimal SwiftUI host app
│   └── Generated-Info.plist
└── Tests/
    ├── AtomyxDriverAgent.swift         XCUITestCase entry — registers
    │                                       commands, starts the TCP
    │                                       server, blocks main thread
    ├── CommandHandlerUnitTests.swift   Unit tests using MockXCUIBridge
    ├── Server/
    │   ├── CommandServer.swift         TCP listener + JSON framing
    │   └── WireProtocol.swift          Request / Response types
    ├── Bridge/
    │   ├── XCUIBridge.swift            Protocol + DefaultXCUIBridge
    │   └── ElementTypeName.swift       XCUI elementType → wire string
    ├── PressKey/                       Press-key strategy chain
    └── Commands/                       Command implementations
```

## Makefile targets

| Target | What it does |
|---|---|
| `make setup` | xcodegen + build-for-testing (simulator) |
| `make build` | Just build-for-testing |
| `make test` | Run `CommandHandlerUnitTests` (fast, no simulator UI) |
| `make serve` | `test-without-building` → starts TCP server, blocks |
| `make smoke` | Host-side smoke test |
| `make clean` | Remove `build/` and regenerated `.xcodeproj` |
| `make setup-device` | xcodegen + build with device code signing |
| `make test-device` | Unit tests on device |
| `make serve-device` | iproxy + serve on device |

## Pitfalls

- **Stale build cache after directory moves.** Xcode's PCH embeds
  absolute paths; if you `git mv` or rename `platforms/ios-agent/`,
  nuke `build/` before the next `make setup` or you'll see
  `missing required module 'SwiftShims'` / `PCH was compiled with
  module cache path ...` errors.
- **`make serve-device` uses `test-without-building`** — it does
  NOT rebuild the Swift binary. After changing Swift source, run
  `make build-device` before `make serve-device`.
- **`serve` test is blocking.** `AtomyxDriverAgent.testServeCommands`
  calls `RunLoop.current.run()` and never returns. Unit tests are
  separated via
  `-only-testing:AtomyxDriverAgent/CommandHandlerUnitTests` so
  `make test` doesn't hang.
- **Bundle id `dev.atomyx.driver.host`.** Override via
  `BUNDLE_ID=...` in the Makefile or `device.env` if you need a
  distinct signing identity.

See [`.claude/docs/pitfalls.md`](../../.claude/docs/pitfalls.md)
for more. iOS decision log lives in
[`.claude/docs/ios.md`](../../.claude/docs/ios.md).
