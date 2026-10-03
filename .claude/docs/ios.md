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
| `platforms/ios-agent/Tests/PressKey` (directory) | `pressKey` strategy chain (nav bar back, then edge swipe, then a fallback strategy) |
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
| `Connect()` / `Ping()` | `ping` | `PingCommand` | Populates `CanMultiPointer` and `CanPressure` from the active synthesizer's capabilities. Also reports the runner identity (`runnerId`, `targetUdid`, `simulatorUdid`). |
| `Hierarchy()` | `dumpRawTree` | `DumpRawTreeCommand` | Builds the tree from one `app.snapshot()` call, walked iteratively. |
| `Tap()` | `tapAt` | `TapAtCommand` | Always coordinates. Selector-based tap composes host-side. |
| `LongPress()` | `longPressAt` | `LongPressAtCommand` | |
| `Swipe()` | `swipe` | `SwipeCommand` | |
| `DispatchGesture()` | `dispatchPointer` | `DispatchPointerCommand` | Routes through the active `EventSynthesizer`. Rejects multi-pointer or pressure waypoints host-side first when the runner's capabilities don't support them. |
| `LaunchApp()` | `launchApp` | `LaunchAppCommand` | Updates `DriverState.currentApp` / `currentBundleId`. |
| `StopApp()` / `KillApp()` | `forceStopApp` | `ForceStopAppCommand` | Clears the tracked `currentApp` when it matches. |
| `InputText()` | `typeText` | `TypeTextCommand` | Native `XCUIApplication.typeText`. Requires a focused field. |
| `EraseText()` | `clearFocusedInput` | `ClearFocusedInputCommand` | 4-strategy chain, see quirks below. |
| `PressKey()` | `pressKey` | `PressKeyCommand` plus `PressKey/*` | Returns whether a verifiable affordance was used. |
| `HideKeyboard()` | `hideKeyboard` | `HideKeyboardCommand` | |
| `Screenshot()` | `screenshot` | `ScreenshotCommand` | PNG, base64-encoded. |
| `ScreenSize()` | `getScreenSize` | `GetScreenSizeCommand` | Points, not pixels. Falls back to the Springboard proxy's frame when no app is tracked. |
| `CurrentForeground()` | none | none | Always returns a typed unsupported error, see quirks below. |
| `ListApps()` / `InstallApp()` / `UninstallApp()` / `ClearAppData()` / `IsAppInstalled()` | none (host-only) | none | `xcrun simctl` (simulator) or `xcrun devicectl` (device), invoked directly by the Go adapter. |
| `WaitForIdle()` | none | none | Always reports `true` without waiting, so a caller must fall back to tree-diff polling. |

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
builds the runner when it is missing or stale, then spawns
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
because no Atomyx record names it. No answering runner, or a runner that
names no device while the number of connected iPhones is not exactly
one, fails with `DEVICE_NOT_FOUND` and a remedy. Pass `--device` to have
the CLI launch its own runner.

`IosDriver.Connect` (`core/internal/iosdriver/driver.go`) on a
physical device first launches an `iproxy` USB tunnel as a background child process
and waits for it to become reachable before dialing. `Disconnect` stops
the tunnel. A driver built with `Options.ExternalTunnel` skips that
launch, because the provisioner that started the runner already owns the
tunnel. On a simulator the driver dials the host port directly, because
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

A runner lives as long as the process that launched it, not as long as
the session that first used it. The process that launches a runner writes
itself into the record as owner.

- Closing a session, whether a normal close or an idle release, ends the
  driver's connection and leaves the runner running.
- `device.RunnerKeeper` (`core/internal/device/runner_keeper.go`) holds
  every runner its process launched. `atomyx` and `atomyx-mcp` build one
  in `core/cmd/internal/runnerprovision` and run its `Cleanup` on both
  exit paths: after the command or the server returns, and in the stop
  watchdog that SIGINT and SIGTERM arm. Both are bounded by
  `shutdown.GracePeriod`.
- `RunnerKeeper.StopAll` stops each kept runner, its tunnel and its record
  through the provisioner's single kill path, and skips a runner whose
  record names another process as owner. It runs once, and a later call
  waits for the first.
- A Connect failure stops the runner at once, because no session ever
  used it. `device.Built.Release` stops it for a caller that owns the
  driver outright, and the Studio sidecar calls it after it disposes a
  device.
- The provisioner's loss hook leaves a runner alone when the record names
  another owner for the same runner id. It stops no tunnel, removes no
  record, and reports no loss.

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

`kill` removes the file only while it still names its own `runnerId` and
owner. A record that survives while its owner is alive belongs to a
runner that outlived its session. A record whose owner is gone means the
owner ended without its exit path. The same `pid` after a reboot or after the process ended can
name an unrelated process, so a reader trusts it only when
`processStartTime` and `hostBootTime` still match.

#### Orphan reaping

`session.Open` reads the record after it has acquired the device lease,
because a held lease proves no other session drives the device. The
reaper reads no owner field, so it ends a recorded runner even when its
owner is alive and idle.
`device.RunnerReaper` then ends the orphan. It reads the process through
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
with the recorded `runnerId` is ended with `xcrun simctl terminate`. A
runner that survives that is reported in the log and the record stays for
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

A refused runner surfaces as `IproxyErrBindConflict` from the tunnel
launch and as `XctestErrRunnerMismatch` from the launcher. The direct
HID path (`DriverFactory.createDriver`) runs the same check against the
runner it is about to use, so a runner left behind by a different
simulator is never adopted.

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
process as owner is not reported, and its tunnel and record stay. This
costs no polling, because it reads
the exit verdict `process.BackgroundProcess.Wait` already returns. A
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
tracked), which includes the status bar and home indicator. A caller
computing "middle of screen" or checking whether a point sits inside
the visible viewport must not assume the reported bounds equal the
device's nominal logical size.

**`pressKey` is not a system primitive.** Android dispatches a BACK
intent regardless of app handling, but iOS only has per-screen
affordances. `BackKeyStrategy` tries the nav bar back button first
(verifiable, returns `affordanceFound: true`) and falls back to an
edge-swipe gesture (unverifiable, returns `affordanceFound: false`
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
