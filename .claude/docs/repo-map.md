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
Adding a new platform = implement the `Driver` port from
`@atomyx/core-driver`. No core changes needed.

The pre-refactor `src/` runtime was retired when the new
framework reached parity — see the "retire legacy" commit.

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
│   │                            createMcpServer factory + 21 tools
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
│   ├── ios.md                 iOS bridge decision log + internals
│   └── android-shrink.md      completed Android APK shrink record
│
└── .github/workflows/
```

**Module conceptual grouping**: a "module" in Atomyx is the
set of packages sharing a name prefix. `core-driver` module =
{`core-driver`, `core-driver-wire`, `core-driver-android`,
`core-driver-ios`, `core-driver-mcp`, `core-driver-cli`}.
The grouping is implicit in naming, not explicit in directory
structure — keeps paths shallow and matches Playwright /
Maestro / React monorepo conventions.

## Tool surface

`@atomyx/core-driver-mcp` ships 21 tools, every one a thin
`defineTool` wrapper that calls Orchestra methods. See
[`tools.md`](./tools.md) for the full contract.

| Category | Tools |
|---|---|
| Device + app lifecycle | `list_devices`, `list_apps`, `launch_app` |
| Screen | `get_ui_tree`, `find_element`, `screenshot` |
| Actions | `tap`, `tap_and_wait_transition`, `input_text`, `swipe`, `press_key` |
| Wait | `wait_for_element` |
| Run lifecycle + reporting | `start_run`, `finish_run`, `report_bug` |
| Run + bug queries | `list_runs`, `get_run`, `list_bugs`, `get_bug` |
| Guidance | `add_case_study`, `get_case_studies` |

The surface was consolidated so each tool has exactly one
intent. Before adding a tool, ask whether an existing tool
can absorb the new intent via a parameter.
