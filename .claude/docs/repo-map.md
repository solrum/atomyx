# Atomyx: Repository layout reference

## Purpose

Who reads this: a contributor or AI agent that knows what it wants to
change but not which file owns it.

When to read it: before searching the tree by hand for a package, a
binary entry point, or a workflow file.

What this does not cover: why the dependency graph is shaped this
way (see [`architecture.md`](./architecture.md)) or how to build and
test what you find (see [`development.md`](./development.md)).

## TL;DR

The runtime is a single Go module (`dev.atomyx`) under `core/`, with
six binaries in `core/cmd/` and one package per concern under
`core/internal/`. Native platform projects live under `platforms/`.
`apps/studio` is a separate desktop IDE with its own rules and is out
of scope for this file.

## Top-level tree

```
atomyx/
├── CLAUDE.md                  AI-agent runtime instructions
├── README.md                  user-facing landing page
├── LICENSE
├── Makefile                   thin root orchestrator; delegates to core/Makefile
│
├── core/                      the Go core runtime (module dev.atomyx)
│   ├── cmd/                   binary entry points, one directory per binary
│   ├── internal/              one package per concern (see below)
│   ├── scripts/                dev tooling: comment gate, schema-freshness gate
│   ├── bin/                    local build output (gitignored)
│   ├── go.mod / go.sum
│   ├── Makefile                build and schema-generation targets
│   ├── .go-arch-lint.yml       layer-import gate config
│   └── .golangci.yml           lint gate config
│
├── platforms/                  non-Go native platform projects
│   ├── android-agent/          Kotlin APK: the on-device control server
│   └── ios-agent/               Swift XCUITest runner: the on-device control server
│
├── apps/studio/                desktop IDE, separate rules (see apps/studio/README.md)
│
├── examples/                    runnable example scripts and flows
├── docs/                        end-user documentation
├── scripts/                     repo-level dev tools (lint, codemods, style checks)
│
└── .claude/docs/                 contributor and AI-agent documentation (this directory)
```

## `core/cmd/`: binary entry points

| Directory | Binary | Responsibility |
|---|---|---|
| `core/cmd/atomyx` | `atomyx` | The CLI: argv parsing, command routing, driver and skills module commands. |
| `core/cmd/atomyx-mcp` | `atomyx-mcp` | The MCP stdio server over the tool surface in `core/internal/mcp`. |
| `core/cmd/atomyx-runtime` | `atomyx-runtime` | The sidecar: a newline-delimited JSON-RPC bridge over stdin and stdout that drives a real device on a parent process's behalf. |
| `core/cmd/gen-config-schema` | `gen-config-schema` | Regenerates the checked-in JSON Schema for the project config type from `core/internal/config`'s Go types. |
| `core/cmd/gen-run-schema` | `gen-run-schema` | Regenerates the checked-in JSON Schema for the run folder `meta.json` from `core/internal/runfolder`'s Go types. |
| `core/cmd/gen-script-schema` | `gen-script-schema` | Regenerates the checked-in JSON Schema for the script definition type from `core/internal/script`'s Go types. |
| `core/cmd/internal/runnerprovision` | none | Builds the iOS runner provisioner that `atomyx` and `atomyx-mcp` wire into their driver factory, including where runner records live. |
| `core/cmd/internal/sessionwire` | none | Builds the lease manager, identity resolver, orphaned runner reaper and session timeouts that `atomyx` and `atomyx-mcp` hand to `session.Open`, so both arbitrate devices the same way. |

## `core/internal/`: one package per concern

| Package | Responsibility |
|---|---|
| `androiddriver` | Implements the Driver port over the Android agent's HTTP control server. |
| `cli` | Implements the `atomyx` command-line binary: argument parsing, command routing, driver and skills commands. |
| `clock` | Time-source abstraction every polling loop, timeout, and retry uses instead of calling the standard library directly. |
| `config` | Owns the project config schema, including the `session` timeouts block, its file-discovery and YAML loader, schema validation, and environment reference resolution. |
| `conformance` | Drives the Go runtime's implementation of an operation and asserts its output against a frozen golden fixture. |
| `device` | Discovers reachable Android and iOS devices and builds a live Driver plus Orchestra pair for whichever one a caller selects. For an iOS target it launches the runner through a provisioner and ties the runner's lifetime to the driver, and it ends a runner a crashed process left behind. |
| `diag` | Defines the stable error-code taxonomy every user-facing surface reports a failure through. |
| `driver` | Defines the platform-neutral Driver contract every concrete adapter implements. |
| `core/internal/driver/drivertest` | The Driver contract suite: table-driven invariant tests any implementer must satisfy. |
| `core/internal/driver/mock` | An in-memory Driver implementation for tests. |
| `iosdriver` | Implements the Driver port over the Swift XCUITest runner's line-delimited JSON TCP protocol. |
| `iossimdriver` | Implements the Driver port over the iOS Simulator's direct-HID input path. |
| `flock` | The advisory file lock primitive: a non-blocking exclusive flock on an open file, unlock, a contention check, and a descriptor-versus-path identity check. Depends on the standard library only. |
| `lease` | Gives each physical device at most one driving process through a per-device flock, and probes it without writing. |
| `logging` | The structured logging port every layer writes through, so the host chooses the destination. |
| `mcp` | Builds the Atomyx MCP server: server construction, tool registration, the tool handlers themselves, and the `DeviceSession` holder state machine with its idle timers and clock-driven tool budget. |
| `netcapture` | The concrete network-capture adapters a script run observes HTTP traffic through. |
| `orchestra` | Composes Driver calls into selector resolution, priority broadening, scroll-into-view, obscurement, and retry. |
| `process` | Abstracts external command execution (adb, xcrun, and similar) behind Runner and BackgroundRunner interfaces, carries extra child environment on the context, and reads process and host start times. |
| `report` | Renders a run's result into HTML and versioned JSON, and writes its screenshots, into the report and artifacts directories the caller names. |
| `runfolder` | Defines the run folder contract: the layout names, the `meta.json` record with its result and outcome enums, the legacy status mapping, and the generated JSON Schema. It also writes run folders with an exclusive create and a live lock. Depends on the standard library and `flock`. |
| `runner` | Defines the command execution contract and the ScriptRunner that dispatches a parsed script's steps. |
| `scenario` | Runs ordered groups of scripts and ordered sets of scripts or scenarios against a shared Orchestra. |
| `script` | Owns the test-script schema, parser, and expression engine. |
| `core/internal/script/expression` | Implements the test-script expression language: tokenizing, parsing, and evaluating. |
| `session` | Opens a device for one process: a lease-free prepare phase that builds the runner, then identity, lease, orphaned runner and tunnel reaping, then the driver, and the owner's lifecycle of the result. |
| `core/internal/session/sessiontest` | Test-only helpers for a session host, such as a lease manager over a throwaway directory. Only `_test.go` files import it. |
| `sidecar` | Implements the host-side control process: a JSON-RPC bridge, method dispatcher, session, and event bus. |
| `skills` | Embeds and copies the Atomyx skill and agent markdown files a consumer project installs. |
| `storage` | Persistence primitives the MCP tool layer builds on: a JSON and markdown record store plus the in-flight run tracker. |
| `storageroot` | Resolves the storage root shared by the CLI and the MCP server: an explicit path, then `ATOMYX_STORAGE_DIR`, then `~/.atomyx`. |
| `suite` | Discovers the ordered set of script and scenario files a suite selector resolves to. |
| `transport` | Implements the device-facing clients: HTTP for the Android agent, TCP JSON for the iOS agent. |
| `version` | Reports one binary's build identity: release version, VCS commit, and build date. |
| `wire` | Shared types across device platforms: geometry primitives and the normalized UI tree shape. |

## `core/scripts/`

Dev tooling `make -C core check` runs (see
[`development.md`](./development.md) for the exact commands):
`core/scripts/check-comments.sh` (the comment-style gate),
`core/scripts/check-no-raw-syscall-open.sh` (rejects a bare
`syscall.Open(` or `unix.Open(` outside `core/internal/lease`),
`core/scripts/check-sessiontest-imports.sh` (rejects a non-test file
that imports `core/internal/session/sessiontest`),
`core/scripts/check-runfolder-layout/` (a Go program that rejects a
run folder layout name spelled as a string literal outside
`core/internal/runfolder`, with an allowlist of files that still
spell one), and `core/scripts/check-schema-fresh.sh` (the schema-freshness gate), plus
their allowlist files.

## `platforms/`

- `platforms/android-agent/`: the Kotlin APK that runs on-device and
  exposes the HTTP control server `androiddriver` talks to.
- `platforms/ios-agent/`: the Swift XCUITest runner that exposes the
  TCP control channel `iosdriver` talks to, plus the direct-HID helper
  `iossimdriver` talks to on the simulator.

Each has its own build and test workflow. See
[`android.md`](./android.md) and [`ios.md`](./ios.md).

## Local checks

There is no hosted CI. `make check` from the repository root runs the
core module's gates (delegated to `core/Makefile`) plus the repo-wide
prose and doc-path lints under `scripts/`. See
[`development.md`](./development.md) for the full gate list and the
command to run each one directly.

## `apps/studio/`

Desktop IDE. Separate rules apply. See `apps/studio/README.md` and
`.claude/rules/studio-architecture.md`. Out of scope for the rest of
this document.

## `examples/` and `docs/`

- `examples/`: runnable example YAML scripts and flows a contributor
  can run against a real device to see a feature working.
- `docs/`: end-user documentation (getting started, CLI reference,
  device setup, the YAML script reference), distinct from this
  directory's contributor-facing reference set.
