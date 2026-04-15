# Atomyx — Repository layout reference

Concrete repo map. Read this when you need to locate a package,
trace a layer, or find where a specific concern lives. For the
high-level architectural rules see [`architecture.md`](./architecture.md);
for tool-specific implementation details see [`tools.md`](./tools.md).

## Layers

1. **`@atomyx/core-driver`** (TypeScript, `packages/core-driver/`)
   — framework primitives. Driver port, Orchestra command layer,
   filter composition, selector priority broadening, scroll-into-view,
   obscurement detection, infra (Clock, Logger), testing kit
   (MockDriver). Single source of truth for cross-platform logic.
2. **Driver adapters** — `@atomyx/core-driver-ios` and
   `@atomyx/core-driver-android` implement the `Driver` port. Each
   is a thin transport + tree normalizer; no business logic.
3. **MCP server** (`@atomyx/core-driver-mcp`) — agent-facing tool
   surface. Composes Orchestra + driver + MCP stdio transport.
4. **CLI** (`@atomyx/core-driver-cli`) — `atomyx-driver` binary
   that wires everything for end-user install.
5. **Native drivers** — Swift XCUITest bundle (`native/ios-driver/`)
   and Kotlin APK (`native/android-agent/`). They speak the TCP /
   HTTP wire protocol the TS driver adapters consume.
6. **Legacy `src/`** — the pre-refactor TS runtime with 19 MCP
   tools. Still functional; slated for deletion once the new
   framework is validated end-to-end against real devices.

Adding a new platform = implement the `Driver` port from
`@atomyx/core-driver`. No core changes needed.

## Repo map

Atomyx is organized as an opt-in package ecosystem (see
`.claude/docs/architecture.md` for the contract). All TS packages live flat
under `packages/`. Module ownership is encoded in the package
NAME PREFIX — `core-driver-*` for the device interaction module,
`test-mgmt-*` for test management, `studio-*` for the GUI
client, `cloud-*` for scale workers. Platform-native projects
(Swift, Kotlin) live separately under `native/`.

```
atomyx/
├── CLAUDE.md                  ← AI-agent runtime instructions
├── README.md                  ← user-facing landing
├── LICENSE
├── .dependency-cruiser.cjs    ← cross-package boundary lint
├── package.json               ← root workspace
│
├── packages/                  ← all TS packages, flat
│   │
│   │── ── core-driver MODULE (Persona: Pure Developer) ──
│   ├── core-driver/           @atomyx/core-driver
│   │                            Driver port + Orchestra + filters +
│   │                            selectors + scroll + obscurement +
│   │                            finder + infra (clock, logger) +
│   │                            testing utilities (MockDriver)
│   ├── core-driver-wire/      @atomyx/core-driver-wire
│   │                            Zod wire-protocol schemas
│   ├── core-driver-android/   @atomyx/core-driver-android
│   │                            HTTP client to Kotlin APK +
│   │                            tree normalizer + adb lifecycle
│   ├── core-driver-ios/       @atomyx/core-driver-ios
│   │                            TCP client to Swift driver +
│   │                            tree normalizer + iproxy lifecycle
│   ├── core-driver-mcp/       @atomyx/core-driver-mcp
│   │                            createMcpServer factory + 9 tools
│   ├── core-driver-cli/       @atomyx/core-driver-cli
│   │                            bin: atomyx-driver — the end-user
│   │                            entry point
│   │
│   │── ── test-mgmt MODULE (Persona: QC Manager) ──
│   ├── test-mgmt/             @atomyx/test-mgmt (skeleton)
│   │
│   │── ── studio MODULE (Persona: Power User) ──
│   ├── studio/                @atomyx/studio (skeleton)
│   │
│   └── ── cloud MODULE (Persona: Scale Operator) ──
│       cloud/                 @atomyx/cloud (skeleton)
│
├── native/                    ← non-npm platform projects
│   ├── ios-driver/            Swift XCUITest runner
│   │                            project.yml + Tests/ + App/ + Makefile
│   └── android-agent/         Kotlin APK
│                                app/src/main/java/dev/atomyx/agent/
│
├── shared/                    ← cross-package type contracts (empty)
├── examples/                  ← runnable demos (not workspace members)
├── scripts/                   ← repo-level dev tools
│
├── docs/                      ← END USER documentation
│   └── README.md              roadmap of planned user content
│
├── .claude/docs/              ← CONTRIBUTOR + AI-agent documentation
│   ├── README.md              audience split rationale
│   ├── architecture.md        opt-in modular ecosystem CONTRACT
│   ├── repo-map.md            this file — repo layout reference
│   ├── tools.md               tool implementation reference
│   ├── development.md         build / test / extend workflow
│   ├── pitfalls.md            known traps for contributors
│   └── ios.md                 iOS bridge decision log + internals
│
├── src/                       ← LEGACY (19-tool server) — delete after
│                                   the new CLI is validated against real
│                                   devices in a smoke run
└── .github/workflows/
```

The legacy `src/` MCP server still exists alongside the new
packages during the strangler-fig transition.
`packages/core-driver-cli/` now ships the replacement entry
point (`atomyx-driver mcp`); once it is validated against real
devices, `src/` is deleted in one atomic commit.

**Module conceptual grouping**: a "module" in Atomyx is the
set of packages sharing a name prefix. `core-driver` module =
{`core-driver`, `core-driver-wire`, `core-driver-android`,
`core-driver-ios`, `core-driver-mcp`, `core-driver-cli`}.
The grouping is implicit in naming, not explicit in directory
structure — keeps paths shallow and matches Playwright /
Maestro / React monorepo conventions.

## Tool surfaces

Atomyx has two parallel tool surfaces during the strangler-fig
transition:

**New — `@atomyx/core-driver-mcp` (9 tools)**. The current
active surface. Every tool is a thin `defineTool` wrapper that
calls Orchestra methods. See [`tools.md`](./tools.md) for the
full contract.

| Category | Tools |
|---|---|
| App | `launch_app` |
| Screen | `get_ui_tree`, `find_element`, `screenshot` |
| Actions | `tap` (selector/coords), `input_text`, `swipe`, `press_key` |
| Wait | `wait_for_element` |

**Legacy — `src/` (19 tools)**. Still running, slated for
deletion. Adds `list_devices`, `select_device`, `list_apps`,
`tap_and_wait_transition`, `start_run`, `finish_run`,
`report_bug`, `get_playbook`, `add_case_study`,
`get_case_studies`. These will migrate into
`@atomyx/core-driver-mcp` incrementally as real usage demands
them through the new Orchestra-backed pipeline.

The tool surface was consolidated from ~40 to 19 because agents were confused
by overlapping intents. Before adding a tool, ask whether an existing tool can
absorb the new intent via a parameter.
