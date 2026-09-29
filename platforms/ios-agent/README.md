# Atomyx iOS agent (Swift XCTest runner)

Swift XCUITest bundle that runs inside an iOS simulator or on a
physical device and exposes a line-delimited JSON command protocol
on `127.0.0.1:22087`. The host-side Go iOS driver,
`core/internal/iosdriver` (physical device) and
`core/internal/iossimdriver` (simulator), connects over TCP (loopback
for simulator, `iproxy` usbmux tunnel for device) and drives the
target through the framework's `Driver` port.

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
priority broadening, and driver-side wait primitives live
**host-side** in `core/internal/orchestra`. The Swift driver never
receives a `Selector`. Instead, it only speaks coordinates.

## Prerequisites

```bash
brew install xcodegen
xcode-select -p   # Xcode command-line tools present
```

- Xcode 15+
- iOS 16 SDK
- For **simulator**: one booted simulator
  (`xcrun simctl list devices booted`). Typing into fields needs the
  software keyboard to be visible, so the simulator's hardware
  keyboard pairing must stay off. On Xcode 16.2 with iOS 18.3,
  `xcrun simctl keyboard <udid> enable hardware` fails outright with
  a Mach error (NSMachErrorDomain code 5), so set it through the
  Simulator app instead: the I/O menu, then Keyboard, then toggle
  Connect Hardware Keyboard off.
- For **physical device**: Apple Developer team ID,
  `brew install libimobiledevice` for `iproxy`.

## Run (simulator)

```bash
cd platforms/ios-agent
make setup       # xcodegen + xcodebuild build-for-testing
make serve       # starts serving; blocks terminal
```

Expected log: `[atomyx] driver listening on 127.0.0.1:22087`

`make serve` does not return while it serves, so leave it running in
its own terminal. From another terminal, confirm it is reachable:

```bash
atomyx doctor --platform ios --device <UDID>
```

From there, either run a script directly:

```bash
atomyx run --file <script.yml> --platform ios --device <UDID>
```

or start the MCP server for an AI client to drive the same simulator:

```bash
atomyx-mcp
```

The connecting client selects the platform and device at runtime
through the `select_device` tool. `atomyx-mcp` itself takes no
platform or device flags.

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

Then, either run a script directly:

```bash
atomyx run --file <script.yml> --platform ios --device <udid>
```

or start `atomyx-mcp` and select the device through the
`select_device` tool, as in the simulator flow above.

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
    ├── AtomyxDriverAgent.swift         XCUITestCase entry that registers
    │                                       commands, starts the TCP
    │                                       server, blocks main thread
    ├── CommandHandlerUnitTests.swift   Unit tests using MockXCUIBridge
    ├── Server/
    │   ├── CommandServer.swift         TCP listener + JSON framing
    │   └── WireProtocol.swift          Request / Response types
    ├── Bridge/
    │   ├── XCUIBridge.swift            Protocol + DefaultXCUIBridge
    │   └── ElementTypeName.swift       XCUI elementType to wire string
    ├── PressKey/                       Press-key strategy chain
    └── Commands/                       Command implementations
```

## Makefile targets

| Target | What it does |
|---|---|
| `make setup` | xcodegen + build-for-testing (simulator) |
| `make build` | Just build-for-testing |
| `make test` | Run `CommandHandlerUnitTests` (fast, no simulator UI) |
| `make serve` | runs `test-without-building`, then starts TCP server, blocks |
| `make smoke` | Host-side smoke test |
| `make clean` | Remove `build/` and regenerated `.xcodeproj` |
| `make setup-device` | xcodegen + build with device code signing |
| `make test-device` | Unit tests on device |
| `make serve-device` | iproxy + serve on device |

## Pitfalls

- **Stale build cache after directory moves.** Xcode's PCH embeds
  absolute paths. If you `git mv` or rename `platforms/ios-agent/`,
  nuke `build/` before the next `make setup` or you'll see
  `missing required module 'SwiftShims'` / `PCH was compiled with
  module cache path ...` errors.
- **`make serve-device` uses `test-without-building`**, but it does
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
- **A first `make serve` run can crash** with `Test crashed with
  signal kill` and an xcresult `mkstemp` failure. Run `make serve`
  again; a plain restart succeeds.

See [`.claude/docs/pitfalls.md`](../../.claude/docs/pitfalls.md)
for more. iOS decision log lives in
[`.claude/docs/ios.md`](../../.claude/docs/ios.md).
