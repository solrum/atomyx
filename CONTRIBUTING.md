# Contributing to Atomyx

Atomyx is an open-source mobile test orchestration framework at
`v0.1.0` (domain `atomyx.dev`). Contributions from humans and AI
agents are welcome: bug reports, driver adapters, script engine
fixes, platform coverage, documentation.

The runtime is a Go core (`core/`, module `dev.atomyx`) that builds
three binaries: the `atomyx` CLI, the `atomyx-mcp` stdio server for
AI agents, and the `atomyx-runtime` sidecar. Two native agents drive
the device side: an Android APK (`platforms/android-agent/`) and an
iOS XCUITest runner (`platforms/ios-agent/`).

## Prerequisites

- **Go 1.25** (see `core/go.mod`). Every core change builds, vets,
  lints, and tests under this toolchain.
- **Android**: the Android SDK plus `adb` on your `PATH`, for
  building and running the Android agent APK and for any change
  touching `core/internal/androiddriver`.
- **iOS**: Xcode and `xcodegen` (`brew install xcodegen`), for
  building the iOS XCUITest runner and for any change touching
  `core/internal/iosdriver` or `core/internal/iossimdriver`. A
  physical-device run additionally needs an Apple Developer team ID
  and `libimobiledevice` for `iproxy`.

You do not need both mobile toolchains to contribute to the core
runtime alone. A change confined to `core/internal/script`,
`core/internal/runner`, or `core/internal/mcp` builds and tests with
only Go installed.

## Build

Build the core binaries from the repository root:

```bash
make build-core
```

This delegates to `core/Makefile`, which builds every command under
`core/cmd/` (`atomyx`, `atomyx-mcp`, `atomyx-runtime`, and the two
schema generators) into `core/bin/`. Building the core directly
works the same way:

```bash
cd core
make build
```

Android and iOS builds run through their own platform-native
tooling. See the "Native agents" section below.

## Quality gates

There is no hosted CI. Run `make -C core check` from the repository
root before opening a pull request that touches `core/**`; it runs
every gate below in order and stops at the first failure. Each gate
also runs on its own from `core/`:

| Gate | Local command |
|---|---|
| Format | `gofmt -l .` (must print nothing) |
| Build | `go build ./...` |
| Vet | `go vet ./...` |
| Layer-import lint | `go-arch-lint check --project-path .` |
| Lint | `golangci-lint run ./...` (pinned to `v2.13.2`) |
| Comment gate | `bash scripts/check-comments.sh` |
| Schema freshness | `bash scripts/check-schema-fresh.sh` |
| Test | `go test -race ./...` |

Install `go-arch-lint` with
`go install github.com/fe3dback/go-arch-lint@latest` and
`golangci-lint` with
`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2`.
Both installed binaries land in `$(go env GOPATH)/bin`, so make sure
that directory is on your `PATH`.

Some tests bind local TCP ports (transport, iOS launcher, Android
launcher, and process-teardown tests). Running the suite on a
machine that blocks local port binding, or inside a sandbox with the
same restriction, fails those tests for a reason unrelated to your
change, so run them on a host that allows it.

`go test -race ./...` is the test gate `make -C core check` runs. A
single `-race` pass can still miss a flake in genuinely concurrent
code, so when you touch concurrency or timing-sensitive code, also
run `go test -race -count=10 ./path/to/package/...` before you
consider it solid.

## Where things live

- `core/internal/driver`: the `Driver` port every platform adapter
  implements, and the contract the rest of the runtime depends on.
- `core/internal/orchestra`: the command layer above `Driver`
  (selector resolution, scroll-into-view, obscurement detection,
  priority broadening).
- Driver adapters: `core/internal/androiddriver`,
  `core/internal/iosdriver` (physical device),
  `core/internal/iossimdriver` (simulator), and
  `core/internal/driver/mock` (the in-memory `MockDriver` used by
  tests that need no device).
- `core/internal/script`: the YML script schema and parser.
  `core/internal/runner`: the step executor that runs a parsed
  script against an `Orchestra`.
- `core/internal/mcp`: the MCP tool surface `atomyx-mcp` serves.
- `core/internal/conformance`: the differential conformance corpus
  that pins script and expression behavior against golden fixtures.

## Adding a script step

A step is a `CommandDefinition` implementation in its own
`<name>_command.go` file under `core/internal/runner`, appended to
the registry `DefaultCommands` returns in
`core/internal/runner/commands.go`. No other file changes to make a
new step available to the parser and the runner.

After adding or changing a step's fields, regenerate the checked-in
JSON Schema and confirm it is still fresh:

```bash
cd core
make gen-schema
bash scripts/check-schema-fresh.sh
```

The schema freshness gate fails when the committed schema no longer
matches the Go structs it describes, so run `make gen-schema` and
commit the regenerated file in the same change.

## Adding an MCP tool

A tool is a `register<Name>Tool(server, tc)` function in its own
`<name>_tool.go` file under `core/internal/mcp`, called from
`NewServer` in `core/internal/mcp/server.go`. The function declares
the tool's JSON input schema and its handler, and receives a shared
`ToolContext` carrying the device session, storage, run store, clock,
and logger.

Before adding a tool, confirm no existing tool can absorb the intent
through a parameter. Atomyx keeps a deliberately small tool surface
because overlapping tools measurably confuse the agents that consume
it. Growing the surface is a last resort, not a first instinct.

## Native agents

**Android** (`platforms/android-agent/`): a Kotlin APK. Run its unit
tests with:

```bash
cd platforms/android-agent
./gradlew :app:testDebugUnitTest
```

Build the debug APK with `./gradlew :app:assembleDebug` when a
change needs on-device verification.

**iOS** (`platforms/ios-agent/`): a Swift XCUITest bundle, driven
through its own `Makefile`:

```bash
cd platforms/ios-agent
make setup   # xcodegen + build-for-testing (simulator)
make test    # unit tests, no simulator UI needed
make serve   # runs the driver against a booted simulator, blocks
```

A physical-device run additionally needs `cp device.env.example
device.env` (edited with your device UDID and team ID), then
`make setup-device` and `make serve-device`.

## Commits

Atomyx follows conventional commits: `type(scope): imperative
summary`, for example `fix(android): fail a reset launch when the
app cannot actually be stopped`. Common types are `feat`, `fix`,
`refactor`, `test`, `docs`, `ci`, `chore`, and `perf`. Common scopes
are `core`, `android`, `ios`, `cli`, `driver`, `script`. A change
that breaks a public contract carries a `!` after the scope, for
example `refactor(core)!: unify the pointer step on a single touches
list`, with the breaking change explained in the commit body.

Keep the subject under about 72 characters. Use the body to explain
why the change is needed when that is not obvious from the subject
alone. One concept per commit. A pull request may hold several
commits, and each should build and test green on its own.

## Comments

Comments explain why, not what. A well-named function or variable
does not need a comment restating its name in prose. Do not leave
`TODO`, placeholder, or stub comments, because the production bar applies
from the first commit a piece of code lands in.

Two additional constraints are enforced by
`core/scripts/check-comments.sh`:

- No comment anchors current behavior to a prior TypeScript
  implementation. Describe what the current Go code does, not what
  it replaced.
- No comment uses an em dash or a right arrow as a stand-in for a
  connecting word ("because", "so", "then"). Write the word, or
  split into two sentences.

## Docs

Documentation is written in English, in the present tense, and
describes the current contract rather than the history of how it
was built. Do not reference a past implementation, a milestone
number, or a file path that does not exist. Every path, command, and
API symbol named in a doc must exist in the repository at the time
you write it.

## License

Atomyx is MIT. By contributing, you agree your code ships
under the same terms. Do not submit code with incompatible licenses,
and do not copy from GPL projects.

See [`LICENSE`](./LICENSE) for the full text.
