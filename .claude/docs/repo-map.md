# Atomyx — Repository layout reference

Concrete repo map. Read this when you need to locate a package,
trace a layer, or find where a specific concern lives. For the
high-level architectural rules see [`architecture.md`](./architecture.md);
for tool-specific implementation details see [`tools.md`](./tools.md).

## Layers

1. **`@atomyx/core`** (TypeScript, `packages/core/`) — generic infra
   (Clock, Logger, Storage, Sessions).
   **`@atomyx/driver`** (TypeScript, `packages/driver/`) — device
   interaction: Driver port, Orchestra command layer, filter
   composition, selector priority broadening, scroll-into-view,
   obscurement detection, testing kit (MockDriver). `@atomyx/driver`
   re-exports `@atomyx/core` for convenience. Single source of truth
   for cross-platform logic.
2. **Driver adapters** — `@atomyx/ios-driver` and
   `@atomyx/android-driver` implement the `Driver` port. Each
   is a thin transport + tree normalizer; no business logic.
3. **MCP server** (`@atomyx/mcp`) — agent-facing tool
   surface. Composes Orchestra + driver + MCP stdio transport.
4. **CLI** (`@atomyx/cli`) — `atomyx driver` subcommand
   that wires everything for end-user install.
5. **Platform drivers** — Swift XCUITest bundle (`platforms/ios-agent/`)
   and Kotlin APK (`platforms/android-agent/`). They speak the TCP /
   HTTP wire protocol the TS driver adapters consume.
Adding a new platform = implement the `Driver` port from
`@atomyx/driver`. No core changes needed.

## Repo map

Atomyx is organized as an opt-in package ecosystem (see
`.claude/docs/architecture.md` for the contract). All TS packages live flat
under `packages/`. Module ownership is encoded in the package
name convention: plain `@atomyx/<name>` for the device-interaction
module, `@atomyx/test-mgmt-*` for test management, `@atomyx/studio-*`
for the GUI client, `@atomyx/cloud-*` for scale workers.
Platform-native projects (Swift, Kotlin) live separately under
`platforms/`.

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
│   │── ── driver MODULE (Persona: Pure Developer) ──
│   ├── core/                  @atomyx/core
│   │                            Generic infra (Clock, Logger,
│   │                            Storage, Sessions)
│   ├── driver/                @atomyx/driver
│   │                            Driver port + Orchestra + filters +
│   │                            selectors + scroll + obscurement +
│   │                            finder + transitions + state
│   │                            inspection + observation-driven wait
│   │                            primitives + testing (MockDriver).
│   │                            Re-exports @atomyx/core.
│   ├── driver-wire/           @atomyx/driver-wire
│   │                            Zod wire-protocol schemas
│   ├── android-driver/        @atomyx/android-driver
│   │                            HTTP client to Kotlin APK +
│   │                            tree normalizer + adb lifecycle
│   ├── ios-driver/            @atomyx/ios-driver
│   │                            TCP client to Swift driver +
│   │                            tree normalizer + iproxy lifecycle
│   ├── mcp/                   @atomyx/mcp
│   │                            createMcpServer factory + 27 tools
│   ├── script/                @atomyx/script
│   │                            Parser + 17 commands + runner + network
│   ├── cli/                   @atomyx/cli
│   │                            bin: atomyx driver — the end-user
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
├── platforms/                 ← non-npm platform projects
│   ├── ios-agent/             Swift XCUITest runner
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
│   ├── ios.md                 iOS driver internals
│   └── status.md              current version + per-package test counts
│
└── .github/workflows/
```

**Module conceptual grouping**: a "module" in Atomyx is the
set of packages related to a feature area. Driver module =
{`core`, `driver`, `driver-wire`, `android-driver`,
`ios-driver`, `mcp`, `script`, `cli`}.
The grouping is implicit in naming, not explicit in directory
structure — keeps paths shallow and lets `dependency-cruiser`
enforce module boundaries at CI time rather than relying on a
nested directory layout.

## Tool surface

`@atomyx/mcp` ships 27 tools, every one a thin
`defineTool` wrapper that calls Orchestra methods. See
[`tools.md`](./tools.md) for the full contract.

Each tool has exactly one intent. Before adding a tool, confirm
no existing tool can absorb the new intent via a parameter.
