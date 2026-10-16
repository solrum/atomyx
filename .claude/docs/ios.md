# iOS driver internals

## Purpose

For contributors or AI agents editing the iOS code path: the Go host
adapters under `core/internal/iosdriver` and `core/internal/iossimdriver`,
or the Swift XCUITest runner under `platforms/ios-agent`.

Read this before editing any of those trees, debugging an iOS
failure, or adding a capability that must work on iOS.

This doc does NOT cover: the cross-platform `Driver` contract itself
(see `architecture.md`), or the cross-platform tool surface (see
`tools.md`). Known traps are in `pitfalls.md`.

## Architecture

iOS support is split across two codebases that talk over TCP, line-
delimited JSON, on `127.0.0.1:22087` by default (see Runner identity and
port):

```
Go core
  IosDriver (core/internal/iosdriver) ── implements driver.Driver
      │
      ▼
  Conn (core/internal/transport.TCPClient)
      │ simulator: direct loopback
      │ device: iproxy USB tunnel
      ▼
Simulator or device (Swift XCUITest process, bundle id
dev.atomyx.driver.host)
  CommandServer ── CommandRegistry ── one CommandHandler per
                                        wire command
      │
      ▼
  XCUIBridge (DefaultXCUIBridge, wraps XCUIApplication) plus
  EventSynthesizer (gesture dispatch)
```

`IosSimDriver` in `core/internal/iossimdriver` is a second, opt-in
adapter for simulator targets only. It routes `Tap`, `LongPress`,
`Swipe`, and the streaming touch phases through a direct-HID
WebSocket connection to an external `atomyx-sim-hid` helper process
(wrapping Apple's private `SimDeviceLegacyHIDClient` framework),
while every other `Driver` method, including `Hierarchy`,
`Screenshot`, app lifecycle, and `DispatchGesture` itself, delegates
to an embedded `iosdriver.IosDriver`. The device factory only builds
an `IosSimDriver` when the host opts in (`ATOMYX_SIM_HID=1`), the
host passes the capability floor (`IsSimDirectSupported` in
`core/internal/iossimdriver`: arm64 plus Xcode 16 or newer), and
starting the helper succeeds. Any other case falls back to the
XCUITest path in `IosDriver` alone. Multi-pointer gestures always go
through `IosDriver`, because the HID helper has no coordinated
multi-pointer sequencing.

Host-side files under `core/internal/iosdriver`:

| File | Responsibility |
|---|---|
| `core/internal/iosdriver/driver.go` | `IosDriver`, implementing `driver.Driver` by mapping each port method to a wire command |
| `core/internal/iosdriver/transport.go` | The `Conn` interface `IosDriver` depends on |
| `core/internal/iosdriver/iproxy.go` | `iproxy` USB tunnel lifecycle for physical devices |
| `core/internal/iosdriver/identity.go` | `ExpectedRunner`, the ping identity probe, and the identity check shared by the tunnel, the launcher, and the direct HID path |
| `core/internal/iosdriver/xctest_launcher.go` | Reuse-or-spawn `xcodebuild test-without-building` to bring the runner up |
| `core/internal/iosdriver/normalize.go` | Swift `dumpRawTree` output maps to the canonical `wire.TreeNodeWire` |
| `core/internal/iosdriver/apps.go` | App lifecycle (`ListApps`, `InstallApp`, `UninstallApp`, `ClearAppData`, `IsAppInstalled`), all host-only via `xcrun simctl` or `xcrun devicectl` |

Runner-side files under `platforms/ios-agent/Tests`:

| Path | Responsibility |
|---|---|
| `platforms/ios-agent/Tests/AtomyxDriverAgent.swift` | XCTestCase entry point. Registers every command, picks the gesture synthesizer, starts `CommandServer`, blocks on `RunLoop.main.run()` |
| `platforms/ios-agent/Tests/Server/RunnerEnvironment.swift` | Reads `ATOMYX_PORT`, `ATOMYX_RUNNER_ID`, `ATOMYX_TARGET_UDID`, and `SIMULATOR_UDID` |
| `platforms/ios-agent/Tests/Server/CommandServer.swift`, `platforms/ios-agent/Tests/Server/WireProtocol.swift` | TCP listener plus the JSON request and response types |
| `platforms/ios-agent/Tests/Bridge/XCUIBridge.swift` | Protocol wrapping `XCUIApplication` for tree and app-lifecycle operations. `DefaultXCUIBridge` is the concrete implementation |
| `platforms/ios-agent/Tests/Bridge/EventSynthesizer.swift` | Protocol plus shared types (`PointerPath`, waypoint phases, `EventCapabilities`, `SynthesizerError`) |
| `platforms/ios-agent/Tests/Bridge/CoordinateSynthesizer.swift` | `XCUICoordinate`-only implementation: single-pointer tap, long-press, drag |
| `platforms/ios-agent/Tests/Bridge/EventRecordSynthesizer.swift` | **Only file that touches private XCTest symbols** (`XCSynthesizedEventRecord`, `XCPointerEventPath`, `XCTRunnerDaemonSession`, all via `NSClassFromString`). Full multi-pointer and pressure support. Probes itself at load time. |
| `platforms/ios-agent/Tests/Bridge/EventSynthesizerFactory.swift` | Picks `EventRecordSynthesizer` when its probe passes, `CoordinateSynthesizer` otherwise. Selection is fully internal, with no caller-facing knob. |
| `platforms/ios-agent/Tests/Commands` (directory) | One `CommandHandler` per wire command |
| `platforms/ios-agent/Tests/Commands/DispatchPointerCommand.swift` | Accepts a raw waypoint list, routes it through the active synthesizer |
| `platforms/ios-agent/Tests/PressKey` (directory) | `pressKey` strategy chain (nav bar back, then top bar back, then edge swipe, then a fallback strategy) |
| `platforms/ios-agent/Tests/Commands/Clear`, `platforms/ios-agent/Tests/Commands/Clear/Strategies` (directories) | `clearFocusedInput` strategy chain |
| `platforms/ios-agent/Tests/CommandHandlerUnitTests.swift` | Runs without a simulator via a mock bridge plus classifier unit tests |

## Host-side scope

Both Go adapters deliberately do not resolve selectors, scroll into
view, detect obscurement, or broaden selector priority. All of that
lives in the composition layer above `driver.Driver`, operating on
the canonical `wire.TreeNodeWire` `Hierarchy` produces. The Swift
runner never sees a selector: it only receives coordinates and, for
`DispatchGesture`, raw waypoints.

## Command surface

| Driver port method | Wire command | Swift handler | Notes |
|---|---|---|---|
| `Connect()` / `Ping()` | `ping` | `PingCommand` | Populates `CanMultiPointer` and `CanPressure` from the active synthesizer's capabilities. Also reports the runner identity (`runnerId`, `targetUdid`, `simulatorUdid`) and the versions (`protocolVersion`, `agentVersion`). `Connect()` stores the versions for `AgentInfo()` and fails with `AGENT_VERSION_MISMATCH` when `protocolVersion` differs from the host's. |
| `Hierarchy()` | `dumpRawTree` | `DumpRawTreeCommand` | Builds the tree from one `app.snapshot()` call, walked iteratively. Dumps the Springboard when the tracked app is not running and no candidate is given. |
| `Tap()` | `tapAt` | `TapAtCommand` | Always coordinates. Selector-based tap composes host-side. |
| `LongPress()` | `longPressAt` | `LongPressAtCommand` | |
| `Swipe()` | `swipe` | `SwipeCommand` | |
| `DispatchGesture()` | `dispatchPointer` | `DispatchPointerCommand` | Routes through the active `EventSynthesizer`. Rejects multi-pointer or pressure waypoints host-side first when the runner's capabilities don't support them. |
| `LaunchApp()` | `launchApp` | `LaunchAppCommand` | Updates `DriverState.currentApp` / `currentBundleId`. Sets `launchArguments` and `launchEnvironment` on the application before it starts. The host sends each launch argument `name: value` as the environment entry `name=value` and as the argument pair `-name value`, which the app reads from its standard user defaults. Explicit environment entries win over an argument of the same name, and explicit arguments follow the derived pairs. |
| `StopApp()` / `KillApp()` | `forceStopApp` | `ForceStopAppCommand` | Clears the tracked `currentApp` when it matches. |
| `OpenLink()` | `openLink` | `OpenLinkCommand` | Sends the `url` and an optional `bundleId`. Without a bundle id the runner opens the link through `XCUIDevice.shared.system.open` and the tracked app does not change. With one it opens the link through `XCUIApplication.open`, which launches that app on the link, and tracks the app as the app under test. Both calls exist from iOS 16.4. On an older OS the runner replies `opened: false` with `reason: unsupported-os`, which the host returns as `LINK_UNSUPPORTED`. A simulator uses the same runner command as a device. |
| `SetPermissions()` | none (host `xcrun simctl privacy <udid> grant\|revoke <service> <bundle id>`) | the host | Runs through the host process runner and is offered only when the driver targets a simulator (`CanSetPermissions`). Services: location, microphone, photos, contacts, calendar. The simulator has no camera and no control over notification authorization, so those two names fail with `PERMISSIONS_UNSUPPORTED` before any change. A physical device offers no way to change an app's permissions from outside the app, so every call there fails with `PERMISSIONS_UNSUPPORTED`. The simulator adapter delegates to this path. |
| `SetOrientation()` | `setOrientation` | `SetOrientationCommand` | Sends the `orientation` name. The runner sets the system device orientation and records it in a cache that the event-record gesture synthesizer reads, so frame coordinates from the hierarchy are mapped into portrait device space and land correctly in landscape. The cache is seeded once on the main queue when serving begins, because `XCUIDevice.orientation` only allows main-thread access. A rotation made by hand is not observed until the next `setOrientation`. The host then reads `ScreenSize` until it reflects the layout and returns `ORIENTATION_NOT_SET` when the foreground app keeps another layout. The simulator wrapper drops its cached screen size after the call. The orchestration layer then waits for the screen to settle, bounded by `OrientationIdleTimeoutMs`, so the step returns with the layout at rest. |
| `SetLocation()` | `setLocation` on a physical device, none on a simulator (host `xcrun simctl location <udid> set <latitude>,<longitude>`) | `SetLocationCommand` on a device, the host on a simulator | Offered for both kinds (`CanSetLocation`). A simulator runs the host command, which works without the runner. A physical device sends `latitude` and `longitude` to the runner, which sets `XCUIDevice.shared.location` on the main thread, the default threading of a command. The API exists from iOS 16.4. On an older OS the runner replies `set: false` with `reason: unsupported-os`, which the host returns as `LOCATION_UNSUPPORTED`, and any other refusal returns `LOCATION_NOT_SET`. A failing simulator command returns `LOCATION_NOT_SET`. The position persists on the target until changed. The simulator wrapper delegates to this path. |
| `InputText()` | `typeText` | `TypeTextCommand` | Native `XCUIApplication.typeText`. Requires a focused field. With none, the runner types nothing and replies `success: false`, `reason: no_focused_field`, which the host returns as an `AGENT_REJECTED` error. |
| `EraseText()` | `clearFocusedInput` | `ClearFocusedInputCommand` | 4-strategy chain, see quirks below. |
| `PressKey()` | `pressKey` | `PressKeyCommand` plus `PressKey/*` | Returns whether a verifiable affordance was used. |
| `HideKeyboard()` | `hideKeyboard` | `HideKeyboardCommand` | |
| `Screenshot()` | `screenshot` | `ScreenshotCommand` | PNG, base64-encoded. |
| `ScreenSize()` | `getScreenSize` | `GetScreenSizeCommand` | Points, not pixels. Falls back to the Springboard proxy's frame when no app is tracked or the tracked app is not running. |
| `CurrentForeground()` | none | none | Always returns a typed unsupported error, see quirks below. |
| `ListApps()` / `InstallApp()` / `UninstallApp()` / `ClearAppData()` / `IsAppInstalled()` | none (host-only) | none | `xcrun simctl` (simulator) or `xcrun devicectl` (device), invoked directly by the Go adapter. |
| `WaitForIdle()` | none | none | Always reports `true` without waiting, so a caller must fall back to tree-diff polling. |

### Failed command contract

A command whose UI action fails returns an error reply and the runner keeps serving the next command. The runner captures any issue the test framework records while a command runs (a typing call with nothing focused, a tap that cannot resolve an element, and similar) instead of letting it end the serving test. The reply is `{"id": N, "ok": false, "error": "<short message>"}`, the same envelope as every other rejection. The message is one line of at most 160 characters with framework type names and object addresses removed. The host maps it to an `AGENT_REJECTED` error carrying that message, never to an unreachable or communication failure, and the same connection stays usable. Issues recorded outside command handling keep the framework's normal behavior.

## Setup and run

**Simulator** (zero configuration):

```
cd platforms/ios-agent
make setup   # xcodegen + xcodebuild build-for-testing
make serve   # runs the driver, blocks the terminal
```

`make setup` auto-detects the first booted simulator's UDID. The
`atomyx` CLI and `atomyx-mcp` do not need `make serve`: they launch their
own runner for the target, see Launch from the CLI and MCP. `make serve`
and `make serve-device` remain for contributors who drive the runner by
hand, attach a debugger, or read its output live.

`iosdriver.XctestLauncher` (`core/internal/iosdriver/xctest_launcher.go`)
builds the runner when it is missing or stale (see Rebuild rule), then spawns
`xcodebuild test-without-building` as a background child process
(`process.BackgroundRunner`), which stays up until the launcher's kill
function stops it. A launcher without a port allocator is reuse-first:
when the expected runner is already answering `ping` on the fixed port,
it skips `xcodebuild` entirely.

**Physical device**:

```
cd platforms/ios-agent
cp device.env.example device.env   # one-time: copy, then edit
make setup-device
make serve-device
```

`device.env` carries a contributor's personal signing identity
(`DEVICE_UDID`, `DEV_TEAM`, `BUNDLE_ID`) and is loaded only for the
device-specific Make goals (`build-device`, `install-device`,
`test-device`, `serve-device`, `setup-device`, `check-device`). The
simulator goals never read it, so a simulator build never leaks a
contributor's signing identity. Prerequisites: `brew install
libimobiledevice` for `iproxy`, an Xcode account under Settings then
Accounts, and the device paired and trusted over USB.

On the Go side, `DriverProvisioner.EnsureIOSAgent` in
`core/internal/device` resolves the project directory from an
explicit option, then `ATOMYX_IOS_DRIVER_DIR`, then an upward search
for a `platforms/ios-agent` directory from the running binary and the
working directory. Device builds read their signing settings from
the environment, mirroring `device.env` for a manual `make`
invocation. The `atomyx` binary and `atomyx-mcp` do not read
`platforms/ios-agent/device.env`, and the project config has no key for
the signing team, so driving a physical iPhone from the CLI needs these
variables exported in the shell, with `ATOMYX_IOS_DRIVER_DIR` set when
the binary does not sit inside the checkout:

- `ATOMYX_DEV_TEAM` supplies the signing team and is required.
- `ATOMYX_CODE_SIGN_STYLE` is `Automatic` (the default when unset) or
  `Manual`.
- `ATOMYX_PROVISIONING_PROFILE_SPECIFIER` names the provisioning
  profile and is required when the style is `Manual`. A `Manual`
  style without it fails with a signing error before `xcodebuild`
  runs.

The CLI commands `run`, `suite`, and `doctor` decide whether an iOS
`--device <udid>` is a simulator or a physical device by looking the
udid up in the device probe (`device.ResolveDescriptor` in
`core/internal/device/resolve.go`, scanning booted simulators through
`xcrun simctl` and paired devices through `idevice_id -l`). A udid the
scan does not find fails with `DEVICE_NOT_FOUND` and a remedy. Without
`--device`, the CLI does not provision a runner. It pings the runner served
on the default port, 22087, through the `device.RunnerIdentifier` port
(`device.PingRunnerIdentifier` in
`core/internal/device/runner_identifier.go`) and keys ownership on the
device the ping names. A runner that reports `simulatorUdid` names that
simulator, and one that reports `targetUdid` names that physical device.
A runner that reports neither, such as one started without the launch
environment, is taken to serve the single connected iPhone.
The resulting descriptor carries port 22087, so nothing is launched and
no runner record is written. A reaper therefore never ends that runner,
because no Atomyx record names it. A descriptor that carries its own
port, which is every descriptor resolved this way, does not reap a
recorded runner either. The next managed acquire, a descriptor with no
port, adopts or reaps the record. No answering runner, or a runner that
names no device while the number of connected iPhones is not exactly
one, fails with `DEVICE_NOT_FOUND` and a remedy. Pass `--device` to name
the device explicitly.

A named device reuses a runner already served on port 22087 when that
runner belongs to the device and speaks the host protocol. The match is on
`simulatorUdid` for a simulator and `targetUdid` for a physical device,
case-insensitive. The descriptor then carries port 22087, the CLI logs
that it uses the served runner, and Atomyx never stops it. A runner that
belongs to another device, reports no device, or does not answer is
ignored, and the CLI launches its own runner on an allocated port. A
runner that belongs to the device but speaks another protocol version
fails with `AGENT_VERSION_MISMATCH`, and the remedy is to stop the runner
served on port 22087 or rebuild it, so two runners never drive one device.
The MCP `select_device` tool resolves the same way when it omits `port`.
An explicit `port` replaces the resolved one, and the runner is then
re-verified only when that port is 22087.

The served runner accepts one client at a time. When another client holds
it, a named-device resolve waits for the probe timeout and then launches
its own runner.

`IosDriver.Connect` re-checks the identity of a runner whose identity
resolution proved (`Descriptor.RunnerVerified` becomes
`Options.ExternalRunner`), because the runner can be replaced between
resolution and connection. A runner that reports another device fails
with `RunnerIdentityError`. A runner that reports no device identity also
fails, with the rebuild remedy of a legacy runner.

`IosDriver.Connect` (`core/internal/iosdriver/driver.go`) on a
physical device first launches an `iproxy` USB tunnel as a background child process
and waits for it to become reachable before dialing. `Disconnect` stops
the tunnel, and so does a `Connect` that fails after starting it. A driver built with `Options.ExternalTunnel` skips that
launch, because the provisioner that started the runner already owns the
tunnel. A driver that starts its own tunnel keeps using a listener that
already answers as its runner and leaves it running. On a simulator the driver dials the host port directly, because
the simulator shares the host's network namespace.

### Launch from the CLI and MCP

`DriverFactory` (`core/internal/device/factory.go`) takes an optional
`AgentProvisioner` in `DriverFactoryDeps.Provisioner`. `atomyx` and
`atomyx-mcp` wire a `DriverProvisioner` there through
`core/cmd/internal/runnerprovision`. `atomyx-runtime` wires none and keeps
dialing the fixed port of the runner its own `ensureIosAgent` service
launched. That service builds its provisioner with `FixedIosPort`, so it
adopts a runner on `22087`, starts no tunnel, and writes no runner record.
`FixedIosPort` is a mode flag on the provisioner. A port policy injected
through the provisioner's dependencies is the cleaner shape if a third
mode appears.

For an iOS descriptor that has an id and no port of its own, `Build` calls
`EnsureIOSAgent(ctx, udid, kind)`, which returns `(port, kill, err)`, and
dials `port`. The returned `kill` is not attached to the driver, so
disconnecting the driver, and therefore closing a session, never stops
the runner. See "Runner ownership" below. A provisioning failure is reported with `AGENT_UNREACHABLE`, carrying the
launcher's own message, so a signing problem still names
`ATOMYX_DEV_TEAM`.

Each launch does the following:

- Reserves a free host port by binding `127.0.0.1:0`, and records it in
  the provisioner's in-process reserved set so two concurrent launches
  never receive the same port. A launch in another process can still take
  the port first, which the bounded retry covers.
- Assigns a fresh runner id and requires the runner to echo it.
- Passes the three `TEST_RUNNER_` variables through `process.WithEnv`,
  which carries extra environment on the context so `Runner.Run` and
  `BackgroundRunner.Start` keep their signatures.
- For a physical device, starts `iproxy <hostPort>:22087` from the
  launcher's `AfterSpawn` hook, before the ping wait, and stops it in
  `kill`.
- Writes the runner record described below as soon as the process exists.
- Hands the runner to the process's `device.RunnerKeeper` when the
  provisioner was built with one.

Build and run layout, both under `platforms/ios-agent/build`:

- One shared build. `xcodebuild build-for-testing` runs under an
  exclusive lock on `build/.build.lock`. The decision to build is taken
  after the lock is held, so launches that start together build once and
  the others find the product current. The `make` build and test targets
  in `platforms/ios-agent` run their `xcodebuild` step under the same
  lock through `perl`'s `flock`, so a manual build and a launcher never
  compile into the directory together. The build stamp on the product is
  the time the build started, so a source edited during a build still
  reads as newer than the product.
- Rebuild rule. The launcher records a digest next to each product, in
  `<product path>.digest`, so the simulator and the device product keep
  separate digests. The digest is a SHA-256 over the relative path and
  content of every regular file under `Tests`, `Sources` and `App`
  (Finder metadata excluded), `project.yml` and `project.pbxproj`, the
  destination kind, and the selected Xcode developer directory and
  version. A device digest also covers the signing team, the signing
  style and the provisioning profile. A product is rebuilt when it is
  missing or when its recorded digest differs from the digest of the
  current inputs, so an edit that keeps an old file time, a header or a
  plist change all trigger a rebuild, and a file touched without a
  content change does not. A product that never had a digest file, such
  as one built by an older host, is rebuilt only when it is older than the
  project file or a Swift source. The digest is taken before the build,
  and a pending marker replaces the previous digest file first, so a
  failed or interrupted build leaves a product that always reads as stale
  and never one judged by file times. The new digest is written
  atomically after a successful build, all under the build lock. The
  version of each selected Xcode is read once per process, while the
  developer directory is read on every check, so selecting another Xcode
  takes effect at once. When the selected Xcode cannot be read, the
  product counts as stale and an adoption check reports that reason.
- One run directory per target, `build/run-<udid>`. Each target runs
  `xcodebuild test-without-building -xctestrun <file from the shared
  build>` with that directory as its `-derivedDataPath` and a result
  bundle inside it, so concurrent targets never share mutable state.
- The build is bounded at 300 s, a fixed bound that is not
  configurable. A host runs it before it takes the device lease, through
  `session.Prepare`, which calls `DriverProvisioner.Prepare` and so
  `XctestLauncher.EnsureBuilt` under `session.Timeouts.RunnerBuildMs`. A
  build that outlasts the bound fails with `RUNNER_BUILD_TIMEOUT`. The
  launch wait for the runner to answer `ping` is 90 s and runs inside the
  acquisition budget. A first launch on a fresh machine compiles the
  Swift runner.
- The launcher reports `building runner` before a build and `starting
  runner` before each launch through the logger in
  `DriverProvisionerDeps`. The CLI and the MCP server write both lines to
  stderr as log records.

#### Runner ownership

A runner lives as long as the process that launched or adopted it, not as
long as the session that first used it. That process writes itself into
the record as owner.

- Closing a session, whether a normal close or an idle release, ends the
  driver's connection and leaves the runner running.
- `device.RunnerKeeper` (`core/internal/device/runner_keeper.go`) holds
  every runner its process launched or adopted. `atomyx` and `atomyx-mcp` build one
  in `core/cmd/internal/runnerprovision` and run its `Cleanup` on both
  exit paths: after the command or the server returns, and in the stop
  watchdog that SIGINT and SIGTERM arm. Both are bounded by
  `shutdown.GracePeriod`. `Cleanup` runs the process's own cleanup first,
  which closes its open sessions and releases their device leases, and
  then `StopAll`, so the keeper finds the leases free. When the watchdog
  forces the exit before that cleanup finishes, a runner may outlive the
  process, and the next acquirer adopts or reaps it.
- `RunnerKeeper.StopAll` stops each kept runner, its tunnel and its record
  through the provisioner's single kill path. For each runner it first
  takes that device's lease with one non-blocking attempt
  (`lease.Manager.TryAcquire`, supplied through `LeaseAttempt` by
  `core/cmd/internal/runnerprovision`) and releases the lease right after.
  It skips a runner whose lease is held, because another process is using
  the device. It reads the owner from the record again after it takes the
  lease and skips a runner whose record names another process, because
  that process adopted it. It runs once, and a later call waits for the
  first.
- A runner adopted from a lender goes back to it. When a process that
  lent the device on purpose is the source of an adoption, the adopter
  passes its identity in `ProvisionOptions.Lender` and the record keeps it
  as `lenderPid` and `lenderStartTime`. At exit, after the keeper takes
  the lease and finds itself still the owner, it checks the lender with
  its pid and start time. A live lender becomes the owner again, the
  lender fields are cleared, and the runner keeps running, so the lender
  adopts its own runner when it acquires the device again. A lender that
  is gone, or whose pid now belongs to another process, leaves the
  keeper to stop the runner as usual. An adoption with no lender records
  none and clears any lender the record held. A launched runner never
  records a lender.
- A stop acts on the state its process holds in memory, never on the
  record on disk. `RunnerReaper.ReapState` applies the reaper's guards to
  that state and touches no file, so a runner another process launched
  since, under another launch id and process, is never signaled. The
  record is removed only while it still names the stopped runner and its
  owner. A launched simulator runner's stop uses the same call after it
  kills the `xcodebuild` group, so the runner app that outlives the group
  is ended through `simctl`.
- A Connect failure stops the runner at once, because no session ever
  used it. `device.Built.Release` stops it for a caller that owns the
  driver outright, and the Studio sidecar calls it after it disposes a
  device.
- The provisioner's loss hook leaves a runner alone when the record names
  another owner for the same runner id. It stops no tunnel, removes no
  record, and reports no loss.

#### Runner adoption

A runner left running by an earlier holder, on a simulator or on a
physical device, is reused instead of relaunched. `device.DriverProvisioner.ProvisionIOSAgent` decides, under
the device lease and before it reaps. It adopts the
recorded runner only when all of these hold:

- The record names this device and a runner of the requested kind, and
  carries a launch id and a port.
- The reaper's checks pass: the host has not rebooted since the record
  was written, and the recorded pid is still the live `xcodebuild`
  process that was recorded.
- For a physical device, a tunnel reaches the recorded `hostPort`, as
  described below.
- The runner answers `ping` on the recorded `hostPort` with the recorded
  launch id (`iosdriver.CheckRunnerIdentity`).
- The record's build product identity matches the current build. The
  record carries `buildProductPath` and `buildProductModTime`, which
  `XctestLauncher.CurrentBuildProduct` reproduces from the compiled
  runner app. A product that is missing or older than a Swift source or
  the project file matches nothing.

The recorded owner is not a condition. Holding the device lease is what
entitles a process to drive the runner, so a runner whose owner is
another live process is adopted too. When any condition fails, the
provisioner logs the reason, ends the recorded runner through the reaper,
and launches a new one.

A physical device runner is adopted together with its tunnel.
`iosdriver.StartTunnel` runs against the recorded port and reuses a
listener that already answers as the recorded runner only when the
caller accounts for it (`TunnelConfig.Reuse`), so no tunnel is used that
nobody can stop.

- The recorded `iproxy` is adopted when `RunnerReaper.TunnelBlocker`
  passes: the recorded pid is alive, its start time matches
  `tunnelStartTime`, it belongs to this user, its command is `iproxy`, and
  it leads its own process group. It forwards the recorded port because
  the runner then answers on it. The record keeps `tunnelPid` and
  `tunnelStartTime`.
- When the recorded tunnel is gone, or its pid fails those checks and
  nothing listens on the port, a new `iproxy` starts for the recorded
  port and its pid and start time replace the recorded ones.
- A listener on the recorded port that fails the checks blocks the
  adoption, and the recorded runner is reaped and replaced.
- A tunnel started for an adoption that then fails is stopped before the
  replacement launches.

An adopting process:

- stops an adopted tunnel together with the runner through the reaper's
  guarded path when its keeper stops the runner, and stops a tunnel it
  started itself first
- writes itself into the record as owner, so the previous owner's keeper
  skips the runner, and registers the runner with its own `RunnerKeeper`,
  so that keeper stops it when the process exits
- passes the recorded port and launch id to the driver
- watches the runner's `xcodebuild` pid and start time every
  `adoptedRunnerWatchPeriod` (2 s), because an adopted runner has no
  child process whose exit could be awaited. A process that is gone, or
  whose pid now belongs to a different process, ends the runner through
  the reaper and reports a loss like a launched runner that exited.
  The same watcher reports a loss of the tunnel when an adopted tunnel's
  process ends. A record that names another owner is left alone.

`DriverFactory.Build` proves an adopted runner after `Connect` with one
`Hierarchy` call bounded by `adoptedRunnerProbeTimeout` (10 s). A
runner that fails to connect or to answer is ended, and the build
repeats once with `ProvisionOptions{Fresh: true}`, which skips adoption
and launches a new runner. A runner launched by the same call is not
probed.

#### Runner record

`<runtime dir>/<key>.ios-runner.json` is written at launch and removed by
`kill`, where `<key>` is the device's lease key. It holds:

| Field | Meaning |
|---|---|
| `udid` | Simulator or device the runner was launched for |
| `kind` | `simulator` or `device` |
| `runnerId` | Launch identifier the runner echoes in `ping` |
| `hostPort` | Host port the runner or its tunnel answers on |
| `pid` | Process id of the `xcodebuild` process |
| `pgid` | Process group id, equal to `pid` because the runner leads its own group |
| `processStartTime` | Start time of that process, read from the kernel, in UTC |
| `hostBootTime` | Host boot time, read from the kernel, in UTC |
| `startedAt` | Time the record was written, in UTC |
| `tunnelPid` | Process id of the `iproxy` tunnel of a physical device, absent for a simulator |
| `tunnelStartTime` | Start time of the tunnel process, read from the kernel, in UTC |
| `ownerPid` | Process id of the process that answers for the runner and stops it on exit |
| `ownerStartTime` | Start time of the owner process, read from the kernel, in UTC |
| `lenderPid` | Process id of the process that lent the device to the owner, absent when nobody lent it |
| `lenderStartTime` | Start time of the lender process, read from the kernel, in UTC |
| `buildProductPath` | Path of the compiled runner app the runner was started from |
| `buildProductModTime` | Modification time of that app when the runner started, in UTC |
| `buildProductDigest` | Digest of the inputs that app was built from, absent in a record written before digests were recorded |

Adoption requires the product on disk to still be the recorded build. The
digests are compared when both the record and the product have one, and
the path and modification time are compared otherwise, so a runner
recorded by an older host still adopts while its product is current.

`kill` removes the file only while it still names its own `runnerId` and
owner. A record that survives while its owner is alive belongs to a
runner that outlived its session. A record whose owner is gone means the
owner ended without its exit path. The same `pid` after a reboot or after the process ended can
name an unrelated process, so a reader trusts it only when
`processStartTime` and `hostBootTime` still match.

#### Orphan reaping

`device.DriverProvisioner` reads the record before it launches a runner,
and it runs only under the device lease that `session.Open` holds,
because a held lease proves no other session drives the device. It
first tries to adopt the recorded runner (see Runner adoption), and
reaps only when adoption fails. A provisioner built with `FixedIosPort`
writes no record and reaps nothing. The reaper reads no owner field, so
it ends a recorded runner even when its owner is alive and idle.
`device.RunnerReaper` ends the orphan. It reads the process through
`process.Inspector`, which asks the kernel for the pid's start time,
user, command name and process group, and it signals the recorded group
only when all of these hold:

- The record decodes, names the device being opened, and carries a
  `udid` and a `kind`. Any other record is deleted without signaling.
- The host boot time matches the recorded one within five seconds. The
  kernel boot time moves when the wall clock is stepped, so a larger
  difference means a reboot and the record is deleted.
- The pid is alive, its start time matches within two seconds, its user
  is the current user, and its command is `xcodebuild`.
- The process group is greater than 1, differs from the reaper's own, and
  is the group the live process belongs to.

The group receives `SIGTERM`, then `SIGKILL` after the shutdown grace
period. A simulator runner app runs under the simulator's own launchd, so
it sits outside the `xcodebuild` group. After the group is gone the
reaper pings the recorded `hostPort`, and a runner that still answers
with the recorded `runnerId` is ended with `xcrun simctl terminate`,
naming the bundle id read from the recorded runner product (see "Runner
bundle id" below). A runner that survives that is reported in the log and the record stays for
the next acquirer. A physical device runner has no such survivor. Its
`iproxy` tunnel is recorded as `tunnelPid` and `tunnelStartTime` once the
tunnel is up. The reaper ends it the same way under the same checks,
except that the command must be `iproxy` and the tunnel must lead its own
process group.

### Runner identity and port

The runner reads its launch parameters from its environment
(`platforms/ios-agent/Tests/Server/RunnerEnvironment.swift`). A launcher
passes each variable to `xcodebuild` with a `TEST_RUNNER_` prefix, which
`xcodebuild` strips before the test runner starts. Only variables in
`xcodebuild`'s own environment are forwarded, so a `NAME=value` operand
on its command line has no effect. `XctestLauncher` therefore starts the
runner with `process.WithEnv` on the start context, which adds the
variables to the child's environment.

| Variable | Meaning | Default |
|---|---|---|
| `ATOMYX_PORT` | Port the runner listens on. An unset, empty, non-numeric, or out-of-range value falls back to the default. | `22087` |
| `ATOMYX_RUNNER_ID` | Identifier the launcher assigned to this one launch | none |
| `ATOMYX_TARGET_UDID` | Physical device the launcher aimed the runner at | none |
| `SIMULATOR_UDID` | Simulator the runner executes in. Xcode sets it for every simulator destination, whoever launched the runner. | none |

The `ping` reply carries `runnerId`, `targetUdid`, and `simulatorUdid`
beside `pong`. A field is present only when its variable was set.

The reply also carries `protocolVersion`, the integer from
`platforms/ios-agent/Tests/AgentProtocol.swift`, and `agentVersion`, the
runner bundle's `CFBundleShortVersionString`. The version is omitted when
the bundle declares none. A missing `protocolVersion` counts as 0.

The host accepts a runner only when its reply proves it belongs to the
requested target (`iosdriver.ExpectedRunner` in
`core/internal/iosdriver/identity.go`):

- A launcher-assigned runner id must be echoed back exactly.
- Without a runner id, a simulator runner must report its own
  `simulatorUdid`. A runner started by hand through `make serve` is
  adopted this way, and the host logs a warning that it carries no
  launch id.
- Without a runner id, a device runner must report a matching
  `targetUdid`, because Xcode supplies no device identity of its own.
- A reported UDID that differs from the requested one is refused even
  when the runner id matches.
- A runner that reports no identity at all, such as one built before
  these fields existed, is refused with a message that names the remedy.
  Stop it, rebuild the runner with `make build`, and start it again.
  `make serve` and `make serve-device` rebuild automatically when a Swift
  source or the project file is newer than the last build.

Protocol version handshake:

- Provisioner adoption: a recorded runner whose `protocolVersion` differs
  from the host's is not adopted. The provisioner ends it under the
  device lease and launches a runner from the current build.
- Borrowed device: when a process borrows a device from an idle session
  and the runner on it reports another protocol version, the borrower
  replaces that runner. Borrowing across host builds with different
  protocol versions therefore relaunches the runner.
- Fixed-port path: a runner that already answers on the fixed port may
  belong to another process, so the host never stops it. It fails with
  `AGENT_VERSION_MISMATCH`, and the remedy is to stop that runner, rebuild
  it with `make build` in `platforms/ios-agent`, and start it again.
- Driver connect: `Connect()` only verifies. On a mismatch it fails with
  `AGENT_VERSION_MISMATCH` and neither stops nor relaunches the runner.

A refused runner surfaces as `IproxyErrBindConflict` from the tunnel
launch and as `XctestErrRunnerMismatch` from the launcher. The direct
HID path (`DriverFactory.createDriver`) runs the same check against the
runner it is about to use, so a runner left behind by a different
simulator is never adopted.

A runner that accepts the connection but does not answer the ping is
probed up to three times, 500 milliseconds apart. If it stays silent,
because the runner serves one client at a time and another client holds
it, the tunnel launch fails with `IproxyErrRunnerBusy` and `Connect`
returns `AGENT_NOT_READY` with a remedy to close the other client or
restart the runner. A listener that closes the connection or answers
with something else is still refused as `IproxyErrBindConflict`.

Ports differ by target:

- Simulator: the runner binds the host port itself, so `ATOMYX_PORT`
  is the host port.
- Device: the runner always listens on `22087` inside the device, and
  `iproxy <hostPort>:22087` maps a per-device host port to it.

`XctestLauncher` retries the whole allocate-and-launch sequence when
`LaunchConfig.AllocatePort` is set. A launch that loses the race for its
port, either by `xcodebuild` exiting with status 65 before the runner
answers or by another runner answering on the port, is retried on a
fresh port, up to `XctestMaxLaunchAttempts` attempts. Without an
allocator the port is fixed, so the first failure is returned.

For a hand-started runner, `make serve ATOMYX_PORT=<port>` binds it to a
port other than the default. `make serve-device` always passes
`DEVICE_UDID` as the target identity. Keep the default `ATOMYX_PORT` for
a device, because `iproxy` maps to the fixed device-side port. Both
targets run the products from the shared build with their own derived
data path, `RUN_DERIVED`, which defaults to `build/run-<udid>`.

## Platform-specific quirks

**Other UI automation on the device is reported as a warning, never a
failure.** At connect the driver lists the processes on the target once
and caches the foreign automation sessions it finds, which
`driver.ForeignAutomationReporter` exposes to `Orchestra.CheckHealth`.
On a simulator the listing is `xcrun simctl spawn <udid> launchctl
list`, and only rows with a numeric pid whose label contains
`.xctrunner` or `WebDriverAgent` count, so exited jobs are ignored. On a
physical device the listing is `xcrun devicectl device info processes`,
and an executable counts when it lives in a `*-Runner.app` bundle or is
a WebDriverAgent binary, because devicectl reports paths and not bundle
ids. Atomyx's own runner is excluded by the bundle id and app name the
device factory passes in `iosdriver.Options`. The bundle id comes from
the runner product (see "Runner bundle id" below). Each listing has its own
short timeout, and a failure reports nothing. The warning appears in
`atomyx doctor` and `check_health`.

**Runner bundle id.** The host reads `CFBundleIdentifier` from the
`Info.plist` of the compiled runner app it launched or adopted, through
`plutil -convert json`, which handles XML and binary plists. A runner
built with the default `BUNDLE_ID` reports
`dev.atomyx.driver.host.xctrunner`, and a custom `BUNDLE_ID` changes it
accordingly. A value without the `.xctrunner` suffix gets the suffix
appended. The id feeds the foreign automation exclusion and the bundle
the reaper terminates through `xcrun simctl terminate`. When the product
path is unknown or its plist cannot be read, the host uses the default
id. A runner served by hand on a fixed port has no known product, so a
custom bundle id on such a runner is not recognised and is reported as
foreign automation. `iosdriver.ResolveRunnerBundleID` owns the lookup.

**A secure text field carries the `secure` attribute and keeps the
role `secure-text-field`.** The runner reports the element type
`secureTextField`, and the normalizer sets `attributes["secure"] =
"true"` for it, so `iossimdriver` inherits it through `iosdriver`.
`orchestra.IsSecure` is the only reader of the attribute.

**A runner that exits on its own moves the session to lost.** The
provisioner supervises the `xcodebuild` process that hosts the runner
and, for a physical iPhone, the `iproxy` tunnel. When either exits
after the runner answered ready and before the session stopped it, the
provisioner ends the remaining process group through its single kill
path and reports a `device.ProcessExitError` naming which process
ended. The driver factory hands it to `session.Session` through
`device.Built.Lost`, and the session moves to lost with a cause that
wraps `session.ErrDeviceLost`. A runner whose record names another
process as owner is not reported, and its tunnel and record stay. For a
launched runner this costs no polling, because it reads the exit verdict
`process.BackgroundProcess.Wait` already returns. An adopted runner has
no child process, so its `xcodebuild` pid is polled instead. A
runner that dies without its process exiting, or a cable pull that
leaves both processes alive, is not signaled, and surfaces as a failed
step that the liveness probe classifies as `RunAbortDeviceLost`.

**No system-wide foreground query.** `XCUIApplication()` with no
bundle id returns the test target, not whatever is on screen. The
Swift side tracks `DriverState.currentApp` (set by `launchApp`,
cleared by `forceStopApp`), and the Go side mirrors this in
`lastLaunchedBundleID`. `CurrentForeground` always returns a typed
`diag.CodeForegroundUnsupported` error rather than a possibly-stale
tracked value, because there is no way to truthfully answer it
without a private API. Cross-app navigation via deep link or user
gesture desyncs the tracked state. A following `Hierarchy` call on a
simulator can recover by probing candidate bundle ids currently in
`.runningForeground`, passed by the host as `bundleIdCandidates`.

**Tracked app liveness.** Each command reads the tracked app's
process state once, through `XCUIBridge.appState`. The tracked
reference is never cleared by that check, so a crash, an uninstall, or
an external termination leaves it in place. The contract per command:

- `getScreenSize` and `dumpRawTree` use the tracked app only while it
  is `.runningForeground` or `.runningBackground`. Otherwise they use
  the Springboard proxy, and `dumpRawTree` tries `bundleIdCandidates`
  first. A `dumpRawTree` with no tracked app and no candidates still
  fails with "no app bound".
- `typeText`, `pressKey` (strategies that need an app), `hideKeyboard`,
  `getKeyboard`, and `clearFocusedInput` act on the app, so they reply
  "the app is not running, launch it first" when the tracked app has
  exited, and "no app launched, call launchApp first" when none was
  ever tracked.
- `launchApp` with `noReset` attaches only to a running tracked app.
  When the tracked app has exited, it launches a fresh instance and
  replaces the tracked reference. The attach path never starts the
  app, so it ignores launch arguments and environment.

**Gesture synthesizer selection is fully automatic, with no runtime
override.** `EventSynthesizerFactory.make()` picks
`EventRecordSynthesizer` (multi-pointer, pressure) when its dry-run
probe passes, and falls back to `CoordinateSynthesizer` (single-
pointer only) otherwise. There is no environment variable or wire
parameter to force one or the other. A caller only observes the
result through `Capabilities().CanMultiPointer` and `CanPressure`
after `Connect`.

**Private-symbol probe runs a real dry-run dispatch, not just a
selector check.** `EventRecordSynthesizer.isAvailable` verifies every
class and selector it depends on, then confirms
`XCTRunnerDaemonSession.sharedSession` is reachable. Apple's most
common drift pattern across Xcode majors is to keep a selector's name
stable while changing its behavior, which a plain
`respondsToSelector:` check would miss. The probe's log is exposed
through `PingCommand`'s response as `probeLog`, so a version-drift
failure is diagnosable from the host side without attaching a
debugger to the runner.

**Mid-gesture capture runs on a background queue independent of the
gesture's own completion.** A `capture` waypoint schedules a
screenshot on a dedicated `DispatchQueue`, timed against the same
`atOffsetSeconds` clock the gesture dispatch itself uses, so the
touch is still held when the screenshot fires. This is deliberate,
because `synthesizeEvent:completion:` posts its completion block to
the main queue, so any handler that calls it must run off main
(`CommandHandler.requiresMainThread = false`) to avoid deadlocking on
its own semaphore wait.

**The static tree snapshot folds nested accessibility-element
children.** `DumpRawTreeCommand` builds the tree from a single
`app.snapshot()` call rather than live element queries, which is
orders of magnitude faster but means some structurally nested
accessibility elements are folded into their parent's `children` the
same way XCUITest's own snapshot walk folds them. An id reachable
through a live query is not always reachable through the snapshot
tree. Prefer a stable identifier or a coordinate fallback over a deep
structural selector when a nested element proves unaddressable.

**`XCUIElementSnapshot` does not expose `isHittable`.** Hittability is
a live screen-state query, not part of a snapshot. The Go-side
`clickable` derivation in `NormalizeIosTree`
(`core/internal/iosdriver/normalize.go`) instead prioritizes
accessibility traits (`button`, `link`), falling back to an
interactive-elementType whitelist (`button`, `cell`, `textField`,
`searchField`, and so on) when no trait carries the signal.

**Bounds are in points and include safe-area insets.** `getScreenSize`
returns `app.frame` (or the Springboard proxy's frame when no app is
tracked or the tracked app is not running), which includes the status bar and home indicator. A caller
computing "middle of screen" or checking whether a point sits inside
the visible viewport must not assume the reported bounds equal the
device's nominal logical size.

**`pressKey` is not a system primitive.** Android dispatches a BACK
intent regardless of app handling, but iOS only has per-screen
affordances. `BackKeyStrategy` tries the nav bar back button first, then a
button labelled or identified "Back" inside the top 15 percent of the
window (strategy `top_bar_back`, for apps such as Flutter that draw
their own app bar). Both are verifiable and return
`affordanceFound: true`. It then falls back to an edge-swipe gesture (unverifiable, returns `affordanceFound: false`
with a hint to try a screen-specific Cancel, Done, or Close
affordance instead).

**`clearFocusedInput` runs a 4-strategy chain.** In order: a select-
all-plus-delete keyboard shortcut, a triple-tap select, a long-press
system edit menu, then an exact-length delete loop that reads the
field's current value length and deletes exactly that many
characters. The chain polls the focused field after each attempt to
confirm it is actually empty before advancing. A strategy that
reports success skips that verification because it already confirmed
the field is empty itself. The keyboard-shortcut fast path requires
simulator hardware-keyboard pairing (`xcrun simctl keyboard <udid>
enable hardware`, or Simulator, then I/O, then Keyboard). Without it,
the delete-loop fallback still covers the field.

**`searchField` is an editable element type.** A cross-platform "find
an input field" heuristic that substring-matches `textfield` alone
misses iOS search UIs. `interactiveIosRoles` in
`core/internal/iosdriver/normalize.go` includes `searchField`
alongside `textField` and `secureTextField`.

**A physical device app listing must include default apps.**
`devicectl device info apps` lists only developer-installed apps
unless `--include-default-apps` is passed. `fetchDeviceApps` in
`core/internal/iosdriver/apps.go` passes it, so `ListApps` and
`IsAppInstalled` see system apps such as `com.apple.Preferences`, and
`launchApp` does not report them as not installed.

## Limitations

- **USB tether only** for physical devices. `iproxy` uses usbmux, and
  Wi-Fi debugging is not supported.
- **First-run trust prompt** on physical devices: iOS shows "Trust
  This Developer" the first time a manually signed test bundle
  launches. It must be tapped on the device and cannot be automated.
- **Cross-app navigation desync**: see the foreground-tracking quirk
  above.
- **`ScreenSize` is cached host-side** and is not invalidated on
  device rotation.
- **The direct-HID simulator path never handles multi-pointer
  gestures**, because the HID helper has no coordinated multi-pointer
  sequencing. `IosSimDriver.DispatchGesture` always delegates to the
  embedded XCUITest-backed driver.
- **No tvOS, watchOS, or visionOS.** Different XCUITest surfaces, out
  of scope.
- **A tap can be rejected as obscured by a dialog's own backdrop.**
  See [`../../docs/known-limitations.md`](../../docs/known-limitations.md#obscurement-check-can-flag-a-dialogs-backdrop-as-blocking-the-dialogs-own-button).
- **`press_key` types `tab`, `escape`, and `delete` as literal text**
  instead of pressing them. See
  [`../../docs/known-limitations.md`](../../docs/known-limitations.md#ios-press_key-types-an-unsupported-key-as-literal-text-instead-of-pressing-it).
- **Typing can fail on the Simulator when a field never reports
  focus or a keyboard.** See
  [`../../docs/known-limitations.md`](../../docs/known-limitations.md#typing-can-fail-on-the-ios-simulator-when-a-field-never-reports-focus-or-a-keyboard).
- **A focus-timeout error omits the field name for role-only
  selectors.** See
  [`../../docs/known-limitations.md`](../../docs/known-limitations.md#a-focus-timeout-error-omits-the-field-name-for-role-only-selectors).

## Extension points

**Adding a new Swift command:**

1. Create `platforms/ios-agent/Tests/Commands/<Name>Command.swift`
   implementing `CommandHandler`.
2. Register it in `platforms/ios-agent/Tests/AtomyxDriverAgent.swift`'s
   `testServeCommands`.
3. Add a matching call to `IosDriver` in
   `core/internal/iosdriver/driver.go`.
4. If it backs a new or changed `driver.Driver` port method, update
   the port in `core/internal/driver/driver.go` and implement the
   same shape in `core/internal/androiddriver/driver.go`.
5. Unit-test the handler against the mock bridge in
   `platforms/ios-agent/Tests/CommandHandlerUnitTests.swift`.

**Adding a new press-key strategy:** drop a file into
`platforms/ios-agent/Tests/PressKey/` implementing `PressKeyStrategy`
and register it with the `PressKeyRegistry` built in
`platforms/ios-agent/Tests/AtomyxDriverAgent.swift`. The chain
short-circuits on the first strategy that returns a verifiable hit.

**Adding a new clear-text strategy:** drop a file into
`platforms/ios-agent/Tests/Commands/Clear/Strategies/` and add it to
the ordered list `ClearFocusedInputCommand` builds its
`ClearTextChain` from.

**Adding a new gesture synthesizer backend:** implement
`EventSynthesizer` with its own `mechanismName` and an `isAvailable`
probe, then add one line to `EventSynthesizerFactory.make()` ahead of
the existing fallback. The factory does not branch on concrete
types, so a caller never learns a new backend exists.

## References

- `pitfalls.md`: iOS traps to avoid when editing the adapter.
- `tools.md`: cross-platform tool contract the driver plugs into.
- `android.md`: the Android-side equivalents. Both drivers implement
  the same `driver.Driver` port in `core/internal/driver/driver.go`,
  so their capability flags and gesture wire shapes line up one-to-one.
