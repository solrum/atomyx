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
