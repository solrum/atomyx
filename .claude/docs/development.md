# Atomyx: Development workflow

## Purpose

Who reads this: a contributor or AI agent about to build, test, or
extend the Go core, the Android agent, or the iOS agent.

When to read it: before running a build or test command, and before
adding a script step, an MCP tool, or a Driver capability.

What this does not cover: why the layers are shaped this way (see
[`architecture.md`](./architecture.md)), where a file lives (see
[`repo-map.md`](./repo-map.md)), or the MCP tool contract (see
[`tools.md`](./tools.md)). This is a development-only build: there is
no release pipeline, no version tag, and no distributed binary to
document here.

## TL;DR

```bash
cd core && make build          # builds all six binaries into core/bin/
cd core && go test ./...       # unit and conformance tests
```

`make check` (or `make -C core check` for only the Go gates) additionally
runs `gofmt`, `go vet`, `go-arch-lint`, `golangci-lint`, a comment-style
gate, and a schema-freshness gate. Run it before opening a pull request.

## Building the Go core

From `core/`:

```bash
make build          # go build into core/bin/: atomyx, atomyx-mcp,
                     # atomyx-runtime, gen-config-schema, gen-run-schema,
                     # gen-script-schema
make test           # go test ./...
```

`make build` is also reachable from the repository root as
`make build-core`, which delegates to `core/Makefile`.

## The local gates, and how to run each

`make -C core check`, or `make check` from the repository root for
the repo-wide lints alongside it, runs these steps in order:

| Gate | Local command (run from `core/`) |
|---|---|
| Format | `gofmt -l .` (fails if it prints any file) |
| Build | `go build ./...` |
| Vet | `go vet ./...` |
| Layer-import gate | `go-arch-lint check --project-path .` (install with `go install github.com/fe3dback/go-arch-lint@latest`) |
| Lint gate | `golangci-lint run ./...` (install `v2.13.2`, the version `.golangci.yml` is authored and verified against) |
| Comment gate | `bash scripts/check-comments.sh` |
| Raw syscall.Open gate | `bash scripts/check-no-raw-syscall-open.sh` |
| Test-support import gate | `bash scripts/check-test-support-imports.sh` |
| Run folder layout gate | `go run ./scripts/check-runfolder-layout .` |
| Loopback listener gate | `go run ./scripts/check-listen-loopback .` |
| Inspector boundary gate | `bash scripts/check-inspector-boundary.sh` |
| Schema freshness gate | `bash scripts/check-schema-fresh.sh` |
| Test | `go test -race ./...` |

The layer-import gate reads `core/.go-arch-lint.yml`. See
[`architecture.md`](./architecture.md) for what its rules encode.
The comment gate forbids two things in production Go source:
comments that anchor current behavior to a superseded implementation,
and prose that uses a banned punctuation mark as a stand-in for a
connecting word. The run folder layout gate rejects run folder layout names spelled outside
`core/internal/runfolder`, and [`pitfalls.md`](./pitfalls.md) lists them. The schema-freshness
gate confirms the committed JSON Schema files still match the Go structs that generate
them.

## The agent protocol version

The host accepts an agent only when it reports exactly the protocol
version the host declares. Raise the number whenever the wire contract
between the host and an agent changes incompatibly, for example a
removed or renamed field, a changed meaning, or a new required request.
Additive changes the older side ignores do not need a bump.

Three files carry the number and change together:

- `core/internal/version/protocol.go` (`AgentProtocolVersion`)
- `platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/AgentProtocol.kt` (`AGENT_PROTOCOL_VERSION`)
- `platforms/ios-agent/Tests/AgentProtocol.swift` (`AgentProtocol.version`)

`bash scripts/check-agent-protocol.sh`, run by the root `make check`,
fails unless each file declares the number exactly once and all three
are equal.

## Commit size hook

Install the hook once per clone with `make hooks`, which sets
`core.hooksPath` to `scripts/hooks`. The `commit-msg` hook counts the
lines added and removed in the staged change and rejects a commit
above 400 changed lines. Binary files and the four generated JSON
Schema files are not counted, and a merge commit is skipped.

A commit that cannot be split carries a trailer line in its message
stating why:

```
Large-Commit: <reason this change cannot be split>
```

An amend counts only the staged delta, not the lines already in the
commit. `git commit --no-verify` bypasses the hook.
`bash scripts/check-commit-size-hook.sh`, run by the root `make check`,
tests the hook against a temporary repository.

## Regenerating the JSON Schema files

Run after any change to a field or schema-generation type in
`core/internal/script`, `core/internal/config`, or
`core/internal/runfolder`:

```bash
cd core
make gen-schema      # writes internal/script/atomyx-script.schema.json
                      # internal/config/atomyx-config.schema.json,
                      # and internal/runfolder/atomyx-run.schema.json
make check-schema     # fails if the committed files no longer match
```

## Conformance corpus and goldens

`core/internal/conformance` drives the Go implementation of an
operation and compares its output against a frozen fixture under
`core/internal/conformance/goldens/`, organized by subsystem
(`config`, `driver`, `expression`, `mcp`, `normalizers`, `report`,
`runner`, `scenario_parse`, `scenario_run`, `script`, `sidecar`,
`suite_run`). Run the corpus like any other test:

```bash
cd core
go test ./internal/conformance/...
```

When a change intentionally alters an operation's output, regenerate
every golden fixture from the current implementation instead of
hand-editing the fixture files:

```bash
cd core
go test ./internal/conformance/... -update
```

Review the resulting diff carefully: it is the only signal that a
change altered a previously-frozen contract.

## Extension checklists

### Adding a new script step

1. Add the step's field shape to the schema types in
   `core/internal/script` and update the parser
   (`core/internal/script/parse.go`, `core/internal/script/normalize.go`
   or a sibling `normalize_*.go` file) to accept it.
2. Add the step's execution logic as a new command in
   `core/internal/runner` (see the existing `*_command.go` files for
   the `CommandDefinition` shape) and register it in
   `core/internal/runner/commands.go`.
3. Add a unit test in `core/internal/runner` and a parse test in
   `core/internal/script`.
4. Regenerate and check the script JSON Schema (see above).
5. Add a golden fixture under
   `core/internal/conformance/goldens/script/` or
   `core/internal/conformance/goldens/runner/` and confirm
   `go test ./internal/conformance/... -update` produces the
   expected diff.

### Adding a new MCP tool

1. Confirm no existing tool in [`tools.md`](./tools.md) can absorb
   the intent through a parameter, because the surface is
   deliberately small.
2. Create `core/internal/mcp/<name>_tool.go` following the shape of
   an existing tool file: an input JSON Schema built with the
   `js*` helpers in `core/internal/mcp/schema_helpers.go`, a
   `register<Name>Tool` function that calls `server.AddTool`, and a
   handler that validates arguments, resolves the active
   `Orchestra` through `orchestraOrFail`, and calls one or more
   `Orchestra` (or `RunStore`, `Storage`, `ScriptRunner`) methods.
3. Register the new tool in `core/internal/mcp/server.go`, inside
   `NewServer`, keeping the registration order's grouping by concern.
4. Add a test in `core/internal/mcp/<name>_tool_test.go` that
   dispatches through the server the same way an MCP client would.
5. Update [`tools.md`](./tools.md) with the new row.

### Adding a new Driver capability

Touches all four implementers and the observing decorator, so budget
for five adapter changes plus the port itself:

1. Add the method (or the new `Capabilities` flag) to
   `core/internal/driver/driver.go` or
   `core/internal/driver/types.go`, documenting the precondition and
   the capability-flag semantics in the doc comment.
2. Implement it in `core/internal/androiddriver/driver.go`,
   `core/internal/iosdriver/driver.go`, and
   `core/internal/iossimdriver/driver.go`.
3. Implement it in `core/internal/driver/mock/mock.go` so every
   consumer can exercise the new method under test without a real
   device.
4. Add the invariant to the shared contract suite in
   `core/internal/driver/drivertest` so all four implementers are
   checked against the same behavior.
5. If Android needs a new HTTP route, add it under
   `platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/`
   and the corresponding client call in
   `core/internal/androiddriver/driver.go`.
6. If iOS needs a new command, add it under
   `platforms/ios-agent/Tests/Commands/` following the existing
   command registry pattern, plus the client call in
   `core/internal/iosdriver/driver.go`.
7. Compose any higher-level behavior (selector interaction, retry) in
   `core/internal/orchestra/orchestra.go`, never inside the Driver
   implementers themselves.
8. Add the method to the observing decorator in
   `core/internal/session/observing_driver.go`, advancing the action
   epoch with `defer o.mutate()()` when the method changes the device or
   the app, and classify it in `driverMethodClasses` in
   `core/internal/session/observing_driver_test.go`. The classification
   test fails until every port method is listed, and the epoch test
   fails when a mutating method forgets to advance the epoch.

## Native agent build and test

### Android agent (Kotlin)

From `platforms/android-agent/`:

```bash
./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

After a reinstall, Android caches the previous accessibility-service
binding. Re-bind it:

```bash
PKG=dev.atomyx.agent
SVC=$PKG/$PKG.service.AtomyxAccessibilityService
adb shell 'settings delete secure enabled_accessibility_services'
adb shell settings put secure enabled_accessibility_services "$SVC"
adb shell am start-foreground-service -n $PKG/.control.AtomyxForegroundService
```

`core/internal/androiddriver` preflights for stale-binding state and
returns an actionable rebind hint when a tree request comes back
empty. See [`android.md`](./android.md) and
[`android-device-setup.md`](./android-device-setup.md) for the full
device-setup story.

### iOS agent (Swift)

From `platforms/ios-agent/`, against a simulator:

```bash
make setup      # xcodegen + build-for-testing
make test       # unit tests only, fast
make serve      # start the TCP server, blocks the terminal
```

Against a physical device:

```bash
cp device.env.example device.env
$EDITOR device.env        # DEVICE_UDID + DEV_TEAM
make setup-device
make serve-device         # additionally spawns the iproxy USB tunnel
```

See [`ios.md`](./ios.md) for the command surface and platform
quirks.

## End-to-end smoke (real device plus MCP)

```bash
# 1. An iOS target needs no manual step, because atomyx-mcp launches its
#    own runner for the selected device. To drive the runner by hand
#    instead, run `make serve` in platforms/ios-agent, which blocks.

# 2. Build and run the MCP server
cd core && make build
./bin/atomyx-mcp

# 3. Wire the running process into an MCP client and drive the device
```

## Running the device-lab end-to-end suite

Purpose of this section: run the device-gated Go tests that exercise a
real simulator, emulator, or phone, and keep a machine-readable report.
The suite is separate from `make check`, which only compiles and lints
the e2e files.

```bash
ATOMYX_IOS_SIM_UDID=<booted simulator UDID> \
  ATOMYX_IOS_INSTALL_APP=<path to the built demo Runner.app> \
  ATOMYX_IOS_INSTALL_APP_ID=dev.atomyx.demo make e2e PLATFORM=iossim
ATOMYX_ANDROID_SERIAL=<adb serial> make e2e PLATFORM=android
ATOMYX_IOS_DEVICE_UDID=<device UDID> \
  ATOMYX_IOS_INSTALL_APP=<path to the signed Release-iphoneos Runner.app> \
  ATOMYX_IOS_INSTALL_APP_ID=dev.atomyx.demo make e2e PLATFORM=ios
```

`PLATFORM` selects the driver packages under test. The required
environment variable per platform:

| Platform | Required variable | Extra prerequisite |
|---|---|---|
| `iossim` | `ATOMYX_IOS_SIM_UDID`, `ATOMYX_IOS_INSTALL_APP`, `ATOMYX_IOS_INSTALL_APP_ID` | The demo app built with `make demo-ios-sim`, and a runner answering on `127.0.0.1:22087` (`make serve` in `platforms/ios-agent`) |
| `android` | `ATOMYX_ANDROID_SERIAL` | The Android agent running on the device |
| `ios` | `ATOMYX_IOS_DEVICE_UDID`, `ATOMYX_IOS_INSTALL_APP`, `ATOMYX_IOS_INSTALL_APP_ID` | The signed demo app built with `make demo-ios-device`, and the runner served and tunneled to the device |

The `iossim` platform runs the `iossimdriver` package only when
`ATOMYX_SIM_HID_BIN` is set to an executable `atomyx-sim-hid` helper
binary. Without it the script runs only the `iosdriver` package, prints
a line saying the simulator HID helper leg was skipped, and writes the
same line to `summary.txt`.

Optional variables unlock extra cases, and those cases skip when the
variable is unset: `ATOMYX_ANDROID_SERIAL_A` and `ATOMYX_ANDROID_SERIAL_B`
(two attached Android devices), and `ATOMYX_E2E_IOS_SIM_UDID` with
`ATOMYX_E2E_IOS_PROJECT_DIR` (runner identity check on a simulator).

The shared driver contract cases that need a device fixture skip when
the fixture is absent. On `android`, the installed `dev.atomyx.demo`
app serves the launch and clear app data cases. The install and
uninstall roundtrip needs `ATOMYX_ANDROID_INSTALL_APK` (path to an APK)
and `ATOMYX_ANDROID_INSTALL_APP_ID` (that APK's package name). The
fixture app may already be installed on the device. The case removes it,
installs it, removes it again, and reinstalls it from the APK afterward,
so the device ends as it started. Both fixture paths (`ATOMYX_ANDROID_INSTALL_APK`
and `ATOMYX_IOS_INSTALL_APP`) may be absolute or relative to the repository
root. The test validates them before it touches the device, requiring an
existing file for the APK and an existing `.app` directory for iOS, and fails
with a message naming the variable otherwise. The demo app works as the fixture: build
it with `make demo-android` from the repo root, set the APK to
`examples/atomyx-demo/build/app/outputs/flutter-apk/app-debug.apk`, and
set the app id to `dev.atomyx.demo`. `make demo-ios-sim` and
`make demo-ios-device` build the iOS counterparts, and each target prints
its output path. `demo-ios-device` signs with `DEV_TEAM` from
`platforms/ios-agent/device.env`, and signs with a locally installed
provisioning profile when `DEMO_PROFILE=<profile name or UUID>` is passed
(see `examples/atomyx-demo/README.md`). The demo app also serves deep link
`atomyxdemo://open/<route>`, with the routes listed in
`examples/atomyx-demo/README.md`. The erase text case and the exact no
foreground assertions skip on real devices.

The fault and focus hooks are wired on the legs that can support them:

- `FocusInput` restarts the demo app on the `/focus` route and waits until
  the `focus-input` field reports focus. `android` uses `adb shell am start`
  with a `route` extra. `iossim` and `ios` launch the app through the
  driver with a `route` launch argument, so the runner keeps tracking the
  app it drives.
- `FailConnect` aims the next connect at a closed local port, on every
  leg.
- `FailPing` removes the adb forward the test's own driver created on
  `android`, and closes the test's own client on `ios` and `iossim`. No
  leg touches a forward, tunnel, or runner it did not start.

The `iossim` and `ios` legs install the app at `ATOMYX_IOS_INSTALL_APP`
before the suite and use `ATOMYX_IOS_INSTALL_APP_ID` for the launch, clear
app data, install, focus, launch argument, link, and orientation cases.
The erase text case skips on `iossim`. iOS clears app data by uninstalling
and reinstalling from that app. On the `ios` leg the app must be the signed
`Release-iphoneos` build from `make demo-ios-device`, and the rotation
cases wait for the real-device rotation animation.

The script exits 2 on an unknown platform or a missing variable, exits
1 when the iossim runner does not answer or a test fails, and otherwise
exits with the `go test` status. It never prints variable values.

Each run writes `e2e-results/<UTC timestamp>-<platform>/`, which git
ignores:

- `go-test.json` holds the `go test -json` stream.
- `summary.txt` holds the platform, the pass, fail, and skip counts for
  tests and subtests, the skip gate verdict, and the exit status.

### Skip discipline

Every skip in the shared corpus and in the driver packages calls
`drivertest.SkipBecause(t, key, detail)` with a key constant from
`core/internal/driver/drivertest/skip.go`. A raw `t.Skip`, `t.Skipf`, or
`t.SkipNow` in `internal/driver`, `internal/androiddriver`,
`internal/iosdriver`, or `internal/iossimdriver` fails
`scripts/check-e2e-skip-gate.sh`, which runs in `make check`.

After the run, `scripts/e2e.sh` calls `scripts/e2e-skip-gate.sh`, which
reads `go-test.json` and judges each skipped test by its key:

- `leg-not-selected` means the test does not belong to the leg because
  `ATOMYX_DRIVER_E2E` is unset or names another platform. The gate counts
  these in `summary.txt` and never fails on them, so a normal run is not
  flooded.
- A key listed for the leg in `scripts/e2e-skip-allowlist.txt` passes.
- Any other key, and any skip without a key, fails the leg and appears in
  `summary.txt` as `unexpected-skip: <package> <test> <key>`.

To add a skip, add a key constant to `skip.go` with a comment saying what
it means, call `SkipBecause` with it, and decide whether a leg may
tolerate it. Add a `[permanent]` allowlist entry only when the skip is a
property of the platform that no fixture can remove, such as a real
device that cannot stage a known hierarchy. A missing fixture is never a
permanent reason. Entries under `[temporary]` name fixtures that are not
wired yet, and wiring the fixture deletes the entry in the same change.
Each entry is one line, `<leg> <key> <reason>`, with the leg being
`android`, `ios`, or `iossim`.

### Soak

A soak repeats one short demo flow many times on one device to measure how
often the infrastructure fails, as opposed to the app under test.
`examples/soak/demo-soak.yml` launches `dev.atomyx.demo`, signs in, opens
Products and scrolls, opens Gestures for a long press and a swipe, and ends
on the home screen, with waits and visibility assertions between screens. The
script language has no erase, reset, or stop step, so the flow signs out through
a precondition that runs whenever Sign in is not on screen after a launch, and
ends with the home key. One run takes well under a minute.

```bash
scripts/soak.sh <android|ios|iossim> <device-id> <count> [max-infra-rate]
```

Prerequisites per platform:

- `android`: the Atomyx agent is ready on the device (`atomyx doctor`
  reports healthy) and `dev.atomyx.demo` is installed (`make demo-android`
  builds the APK).
- `ios`: the runner is served for the device and the signed demo build is
  built (`make demo-ios-device`). The leg installs it from
  `ATOMYX_IOS_INSTALL_APP`.
- `iossim`: the runner is served for the booted simulator and the demo is
  installed (`make demo-ios-sim`). The script passes the simulator UDID as the
  device id.

Before every iteration the script force-stops the demo app, using the `appId`
of the flow, so a failed iteration cannot leave the app on a screen that breaks
the next one:

- `android`: `adb -s <device-id> shell am force-stop <appId>`.
- `iossim`: `xcrun simctl terminate <device-id> <appId>`, where an app that is
  not running is not an error.
- `ios`: no reset. `devicectl` can only terminate by process id, so the flow's
  own precondition is the only protection on a physical iPhone.

A stop that fails counts as an infrastructure failure of that iteration, is
recorded in `iterations.txt` with exit code 125, skips the run, and is counted
in `resetFailures`, `infraFailures`, and `attempts` of the summary.

A failed run whose log has no `Run folder:` line failed before a run folder
existed, for example because the device or the runner could not be reached.
`atomyx runs stats` never sees such a run, so the script counts it as an
infrastructure failure and in `attempts`, and reports the count as
`unrecordedFailures`.

The script uses `core/bin/atomyx` (`ATOMYX_BIN` overrides it, `make build-core`
builds it), runs the flow `count` times, and keeps going after a failed
iteration. Results land in `e2e-results/soak/<UTC timestamp>-<platform>/`:

- `iter-NNNN.log` holds the output of each run.
- `iterations.txt` holds one line per run with the iteration number and exit
  code.
- `storage/` holds the run folders, unless `ATOMYX_STORAGE_DIR` is set, so a
  soak never writes into `~/.atomyx`.
- `summary.txt` holds the platform, the total, passed, and failed counts, and
  `infraFailures`, `attempts`, and `infraFailureRate` read from
  `atomyx runs stats --json` plus the reset failures and the unrecorded failures, and `resetFailures` and
  `unrecordedFailures`.

The exit status is 0 when the infrastructure failure rate is at or below the
limit and 1 when it is above, or when the statistics cannot be read. The limit
is the fourth argument, then `ATOMYX_SOAK_MAX_INFRA_RATE`, then 0.005. A test
failure counts as failed in the summary but not toward the rate. Ctrl-C stops
after the current run, writes a partial summary with `interrupted: yes`, and
exits 130. Use a fresh storage directory per soak, because the rate covers
every run in the storage root.

`scripts/check-soak.sh` runs in `make check`. It drives the script with a fake
`atomyx` binary that replays passes, test failures, and infrastructure
failures, and asserts the summary counts, the rate, the exit codes, and the reset calls per
platform, with fake `adb` and `xcrun` tools in front of the real ones.

### Nightly matrix

`scripts/nightly.sh` runs the e2e suite and a short soak on every device in
a matrix file, one entry at a time, and tears down only what it started.

```bash
cp scripts/matrix.example scripts/matrix.local   # then edit the ids
scripts/nightly.sh --dry-run scripts/matrix.local
scripts/nightly.sh scripts/matrix.local
```

`scripts/matrix.local` is gitignored. A matrix line is
`<kind> <id> <soak-count>`, and blank lines and lines starting with a hash are
ignored:

- `android`: the id is an adb serial, used as is.
- `android-emulator`: the id is an AVD name. The script boots it headless on a
  free console port, waits for `sys.boot_completed`, and kills that emulator at
  the end of the entry.
- `iossim`: the id is a simulator UDID. The script boots it only when it is not
  booted, and shuts it down at the end only if it booted it.
- `ios`: the id is a physical device UDID.

A soak count of 0 skips the soak. For each entry the script prepares the
device (`atomyx setup` and the demo APK on Android, the demo app on a simulator
when built, the runner served for iOS), runs `make e2e` with the
environment the platform needs, runs `scripts/soak.sh`, and stops the runner,
emulator, or simulator it started. A simulator runner is ready when port 22087
accepts a connection. A physical device runner is ready when `driver listening`
appears in the entry's `setup.log`, because the driver opens the host side
tunnel later. Both waits are bounded by 600 seconds, and
`ATOMYX_NIGHTLY_RUNNER_WAIT_S` overrides the bound. Build the demo app first with the `demo-*`
targets so the install fixtures exist. An iOS or simulator entry is skipped, and reported as
skipped, when something already listens on port 22087, because the driver
binds its host tunnel on that port. A failing entry never
stops the next one, and `SIGINT` or `SIGTERM` tears down the entry in flight.

`--dry-run` validates the matrix and prints the commands per entry without
touching any device. A malformed matrix exits 2 before any entry runs.

Results land in `e2e-results/nightly/<UTC date-time>/<index>-<kind>/` with
`setup.log`, `e2e.log`, `soak.log`, and `result.txt` (kind, id, e2e exit, e2e
pass, fail, skip, and unexpectedSkips counts, soak exit, passed, failed,
infraFailures, and infraFailureRate, and status), plus `summary.txt` with one status line per entry. Run storage is
kept under each entry directory, never in `~/.atomyx`. The exit status is 0
only when every entry is ok.

To schedule it, the owner copies `scripts/nightly.launchd.plist.template`,
replaces the placeholders, and loads it with `launchctl bootstrap`. The
repository never installs the job. `scripts/check-nightly.sh` runs in
`make check` and covers the dry run and matrix validation with no device.

#### Trend report

`scripts/e2e-trend.sh [--nights N] [--results-dir DIR]` reads the last N run
folders (default 14, the length of two weeks) and prints one table per device,
identified by kind and id. Each row is one night with the e2e pass, fail,
skip, and unexpected skip counts, the soak passed and failed counts, the
infrastructure failure rate, and the status. A summary line follows each table
with the green nights out of the nights shown, the total soak runs, and the
pooled infrastructure failure rate, which is the sum of infrastructure
failures over the sum of soak runs. A night where a device is absent adds no
row for it, and a value the night did not record prints as `n/a`. A result
written without `soak infraFailures` contributes its rate multiplied by its
soak runs. The results directory defaults to `e2e-results/nightly` and honors
`ATOMYX_NIGHTLY_RESULTS_DIR`. Exit status is 0 when the report prints and 2 on
a usage error or a missing directory. `scripts/check-e2e-trend.sh` runs in
`make check` against fixture folders. The thresholds the report is read
against live in [`docs/reliability.md`](../../docs/reliability.md).

## Testing philosophy

- **Mock the Driver, not the Orchestra.** `core/internal/driver/mock`
  implements `Driver` with a scripted hierarchy queue and a call log;
  feed it a sequence of hierarchies, drive the code under test, and
  assert the call log.
- **Test MCP tool handlers in isolation.** MCP tool tests dispatch
  through the real server construction path with a mock driver,
  never by reflecting on tool internals.
- **Use `-race` for anything concurrent.** A single non-race pass can
  hide a flake. Concurrency-sensitive packages (the sidecar dispatcher,
  process lifecycle, gesture streaming) warrant
  `go test -race -count=N ./...` locally before trusting a change.
- **Regenerate conformance goldens deliberately.** Treat every diff
  `-update` produces as a change to review, not a formality to run
  through.

## Workspace conventions

- The Go core is a single module (`dev.atomyx`) rooted at `core/`;
  every package lives under `core/internal/` except the five binary
  entry points under `core/cmd/`.
- Package names match their directory name, and imports are always the
  full `dev.atomyx/internal/<package>` path.
- Cross-package dependency direction is declared in
  `core/.go-arch-lint.yml` and enforced by the layer-import gate. See
  [`architecture.md`](./architecture.md) for the rules it encodes.
