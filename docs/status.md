# Atomyx module status

## Purpose

For a user trying Atomyx for the first time, or a contributor orienting in
the codebase, who wants to know what each module does today and how to
check that claim locally. Read this after `getting-started.md` and before
making a change.

This doc does not cover:

- Exact tool inputs and outputs. See `mcp-tools.md`.
- YAML script syntax. See `yml-script-reference.md`.
- Symptom-level workarounds for a known issue. See `known-limitations.md`.
- The list of notable changes. See `../CHANGELOG.md`.

## Summary

| Module | Location | What it does today | Checked by |
|---|---|---|---|
| `atomyx` CLI | `core/cmd/atomyx`, `core/internal/cli`, `core/internal/config` | Runs a script or suite against a device, lists devices, loads project config | `cd core && go test ./internal/cli/... ./internal/config/...` |
| `atomyx-mcp` server | `core/cmd/atomyx-mcp`, `core/internal/mcp` | Exposes 28 registered tools over stdio MCP for device, app, UI, run, and script control | `cd core && go test ./internal/mcp/...` |
| Script engine | `core/internal/script`, `core/internal/runner` | Parses and runs YAML scripts: commands, preconditions, flows, expressions, gestures | `cd core && go test ./internal/script/... ./internal/runner/...` |
| Orchestra | `core/internal/orchestra` | Selector resolution, priority broadening, scroll-into-view, obscurement, waits | `cd core && go test ./internal/orchestra/...` |
| Driver adapters | `core/internal/driver`, `androiddriver`, `iosdriver`, `iossimdriver` | One `Driver` port implemented against a mock, Android, iOS device, and iOS simulator | `cd core && go test ./internal/conformance/...` |
| Agent error boundary | `core/internal/diag` | Stable error codes and a sanitizer for agent-reported messages | `cd core && go test ./internal/diag/...` |
| Network capture | `core/internal/netcapture` | Reads or discards HTTP traffic for `capture`, `assertApi`, `extract`, and `branch` | `cd core && go test ./internal/netcapture/...` |
| Android agent | `platforms/android-agent` | Kotlin app hosting an HTTP control server, reached over `adb forward` | `cd platforms/android-agent && ./gradlew :app:testDebugUnitTest` |
| iOS agent | `platforms/ios-agent` | Swift XCUITest runner exposing a JSON command protocol over loopback or usbmux | `cd platforms/ios-agent && make test` |
| Studio | `apps/studio` | Desktop IDE for authoring and running scripts, as a developer preview with feature work paused | `cd apps/studio && npm run test` |

## atomyx CLI

- `driver run` runs one script file against a selected device, `driver
  suite` runs a named suite from project config or an ad-hoc selection of
  flows by tag or glob, `driver list-devices` (shortcut `atomyx devices`)
  lists Android, iOS simulator, and iOS physical devices, and `driver
  version` reports the build version.
- `skills init` and `skills update-skills` copy or refresh the bundled skill
  and agent files into a project. Project configuration loads via
  `--config`, selects an environment profile via `--env`, and resolves
  platform, device, proxy, and variables ahead of a run.
- Verify: `cd core && go test ./internal/cli/... ./internal/config/...`.

## atomyx-mcp server

- Registers 28 tools in `core/internal/mcp/server.go`, covering device and
  app lifecycle, UI inspection, tap and gesture actions, waits, run and bug
  reporting, case studies, and script execution. Count this from the
  registration list in that file rather than trusting a number in prose.
- Every tool handler orchestrates `Orchestra`, or the runner for
  `run_script`. No handler branches on platform or holds business logic of
  its own. A shared middleware applies argument validation and a per-call
  timeout uniformly to every registered tool.
- Verify: `cd core && go test ./internal/mcp/...`.

## Script engine

- Runs commands for interaction (`tap`, `type`, `swipe`, `pressKey`,
  `back`), assertions (`assertVisible`, `assertNotVisible`, `assertExists`,
  `assertExpr`), capture and variables (`read`, `capture`, `assertApi`,
  `extract`), flow control (`handle`, `branch`, `runFlow`), and gestures
  (`pointer`, `longPress`, `pinch`, `rotate`).
- `preconditions` gates setup steps ahead of the script body, with an
  `ensure`, `always`, or `skip` policy per entry.
- An expression engine resolves typed variables (string, number, boolean)
  captured by `read`, `capture`, or `extract`, and evaluates them in
  `assertExpr` and `branch`.
- A run whose liveness probe finds the device gone aborts with the reason
  `deviceLost`. A script declaring `proxy: required` with no capture
  adapter configured aborts before its first step with the code
  `PROXY_REQUIRED`, naming no route, host, or port in its message.
- Verify: `cd core && go test ./internal/script/... ./internal/runner/...`,
  and `cd core && go test ./internal/conformance/...` for the frozen
  behavioral corpus.
- Known limitations this module hits today: [a misplaced `otherwise` in a
  `handle` step is silently ignored](./known-limitations.md#a-misplaced-otherwise-in-a-handle-step-is-silently-ignored)
  and [YAML selectors cannot express a regex, so a merged accessibility
  label defeats a short match](./known-limitations.md#yaml-selectors-cannot-express-a-regex-so-a-merged-accessibility-label-defeats-a-short-match).

## Orchestra

- Resolves a selector by trying each content field in a fixed priority
  order (id, then label, then text, then value, then hint), so a caller
  does not need to know which field a target carries.
- Scrolls a target into view when it exists off-screen inside a scrollable
  container, and detects obscurement by walking the accessibility tree,
  treating the last node whose bounds contain the target's center point as
  topmost. Provides wait primitives that poll until a condition holds or a
  timeout elapses, instead of a fixed sleep.
- Verify: `cd core && go test ./internal/orchestra/...`.
- Known limitation this module hits today: [obscurement check can flag a
  dialog's backdrop as blocking the dialog's own button](./known-limitations.md#obscurement-check-can-flag-a-dialogs-backdrop-as-blocking-the-dialogs-own-button).

## Driver adapters

- `core/internal/driver` defines the platform-neutral `Driver` port every
  adapter implements, and `core/internal/driver/mock` implements it in
  memory, so a test exercises driver-consuming code with no device
  attached.
- `core/internal/androiddriver` implements the port over the Android
  agent's HTTP control server, reached through `adb forward`.
  `core/internal/iosdriver` implements it over the iOS agent's JSON
  protocol on a physical device, tunneled with `iproxy`, and
  `core/internal/iossimdriver` implements the same protocol over a
  loopback TCP connection to the iOS Simulator.
- `core/internal/conformance` runs one behavioral corpus against every
  implementer, mock and real alike, and asserts each matches a frozen
  golden fixture. Verify: `cd core && go test ./internal/conformance/...`,
  and `cd core && go test ./internal/androiddriver/...
  ./internal/iosdriver/... ./internal/iossimdriver/...` for each adapter's
  own suite.

## Agent error boundary

- `core/internal/diag` defines the stable, machine-readable codes every
  user-facing surface reports a failure through: `DEVICE_NOT_FOUND`,
  `AGENT_UNREACHABLE`, `APP_INSTALL_FAILED`, `APP_UNINSTALL_FAILED`,
  `APP_DATA_CLEAR_FAILED`, `APP_NOT_INSTALLED`, `TOOL_TIMEOUT`,
  `FOREGROUND_UNSUPPORTED`, `DEVICE_BUSY`, `INVALID_APP_ID`,
  `POINTER_CAPTURE_UNSUPPORTED`, `AGENT_REJECTED`, `INVALID_ARGUMENT`,
  `AGENT_COMMUNICATION_FAILED`, `AGENT_NOT_READY`, and `PROXY_REQUIRED`,
  with `UNKNOWN` as the fallback for an error carrying none of these.
- A sanitizer strips implementation detail (a URL or scheme, a host or
  port, a route path, a named transport, a qualified exception type, a
  stack-trace frame) out of an on-device agent's raw rejection message
  before it reaches a tool-facing caller, substituting a neutral message.
- Verify: `cd core && go test ./internal/diag/...`.

## Network capture

- `NullCapture` is the adapter in effect when a run configures no proxy, so
  `capture`, `assertApi`, `extract`, and `branch` commands fail cleanly
  with `PROXY_REQUIRED` instead of hanging on absent traffic. `FileCapture`
  reads HTTP traffic recorded by an external proxy (mitmproxy) as JSON
  lines.
- `netcapture.New` dispatches on a `Config` to construct whichever adapter
  a host's proxy configuration names, so a composition root wires one call
  instead of choosing a concrete type itself. Verify: `cd core && go test
  ./internal/netcapture/...`. Setup steps for capture-dependent script
  commands live in `yml-script-reference.md`'s Proxy Setup section.

## Android agent

- A Kotlin app hosts an HTTP control server on the device, reached from
  the host over `adb forward tcp:8765 tcp:8765`.
- `GET /health` is the readiness check. It returns 200 with
  `accessibilityConnected: true` when the accessibility service can read
  the screen, and 503 with a `reason` when the service is not running or
  cannot read the screen. The host reports that state as
  `AGENT_NOT_READY`, and `make ready-android` prints the reason.
- `GET /ping` reports liveness and gesture capabilities.
- `GET /tree`, `GET /keyboard`, `GET /screenshot`, and
  `GET /current-activity` back UI inspection. The remaining routes back
  interaction and app lifecycle.
- Verify: `cd platforms/android-agent && ./gradlew :app:testDebugUnitTest`
  runs the Kotlin unit test suite under `app/src/test/kotlin`.

## iOS agent

- A Swift XCUITest bundle runs inside the iOS Simulator or on a physical
  device and exposes a line-delimited JSON command protocol on
  `127.0.0.1:22087`, reached over a loopback TCP socket for the simulator
  or an `iproxy` usbmux tunnel for a physical device.
- Selector resolution, scroll-into-view, obscurement detection, and wait
  primitives stay host-side in `core/internal/orchestra`. The Swift runner
  only speaks coordinates.
- Verify: `cd platforms/ios-agent && make test` runs the unit test target
  against the iOS Simulator, and `cd platforms/ios-agent && make
  lint-isolation` confirms no test file outside the bridge file touches a
  private XCTest symbol.
- Known limitations this module hits today: [iOS `press_key` types an
  unsupported key as literal text instead of pressing it](./known-limitations.md#ios-press_key-types-an-unsupported-key-as-literal-text-instead-of-pressing-it)
  and [typing can fail on the iOS Simulator when a field never reports
  focus or a keyboard](./known-limitations.md#typing-can-fail-on-the-ios-simulator-when-a-field-never-reports-focus-or-a-keyboard).

## Studio

Studio is a developer preview. Feature work on it is paused, and the
active surfaces of Atomyx are the core runtime, the CLI, and the MCP
server. It is a macOS-only Tauri desktop IDE for authoring, validating,
and running Atomyx YAML scripts against a real device, with a live device
mirror. It builds and runs on demand, so a contributor can use it
locally.

- Checks run as npm scripts inside `apps/studio`: `npm run test` runs the
  domain and state suites, `npm run test:domain` runs the domain-only suite
  under `node:test` with no DOM, `npm run typecheck` and `npm run lint`
  check types and layer imports, and `npm run depcruise` enforces the
  one-way layer boundary. Current limitations are tracked in
  `apps/studio/README.md`, not duplicated here.

## Build and checks

- `make check` at the repo root runs the core module's gate, the
  style-symbol gate, the iOS launcher identifier gate, the banned-phrase
  gate, and the doc-path gate, in that order.
- `make -C core check` runs the core module's own gate: a format check, a
  build, `go vet`, the `go-arch-lint` layer-import gate, `golangci-lint`, a
  comment gate, a schema-freshness gate, and the full test suite under
  `go test -race ./...`.
- The repository carries no continuous-integration workflow and no release
  pipeline. Every module above is built and checked from source, locally,
  with the commands listed in its section.
