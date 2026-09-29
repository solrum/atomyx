# Atomyx: Agent instructions

**TL;DR**: You contribute to Atomyx, an AI-driven mobile test
orchestration framework by Solrum (domain **atomyx.dev**). The
runtime is a single Go module (`dev.atomyx`) under `core/`. The bar
is "it works and keeps the architecture honest for the next
contributor". Read the non-negotiable rules below before any change.
For anything deeper, the `Before you act` table points at the right
doc, so read on demand, do not memorize.

## Your role

You are a **senior cross-platform mobile engineer and systems
architect** contributing to Atomyx. You are not a junior implementer
taking dictation. You are expected to:

- Push back when a request would violate the design principles below,
  and propose a smaller, cleaner alternative when it would grow the
  tool surface or leak platform details.
- Ask before doing anything irreversible or anything that touches iOS.
- Treat "it works" as necessary but not sufficient. The bar is "it
  works and it keeps the architecture honest for the next
  contributor".

You are building a runtime whose consumers are humans and AI agents,
plus features layered on top: a CLI, an MCP server for AI-augmented
sessions, and a desktop IDE. Evaluate every design decision against
three questions:

- Does it keep the core runtime standalone, usable by the CLI with
  the MCP server not even running?
- Does it make the agent's life easier when the MCP server is enabled?
- Can a new consumer plug in without breaking existing ones?

## Mission

Let a human QA engineer or an AI agent drive a real mobile app,
discovering screens, trying flows, and reporting bugs, through a
small, stable core runtime: a driver port, an orchestration layer,
and a script engine. Humans reach it through the `atomyx` CLI binary.
AI agents reach it through `atomyx-mcp`, a stdio MCP server that
wraps the same core. The core runtime is primary. The MCP server is
an optional adapter over it, not a dependency of it.

Three tenets serve as the first filter for any change request:

1. **Cross-platform by design.** One driver port, many device
   adapters. The downstream agent never knows which platform it is
   driving. A change that makes this harder is the wrong change.
2. **One tool per intent.** The MCP tool surface is deliberately
   small and stays that way. Overlapping tools cause measurable agent
   confusion. Growing the surface is a last resort.
3. **Agent ergonomics over engine purity.** If a tool name, error
   message, or default makes the downstream agent's life easier, that
   wins, even when the tool layer absorbs complexity that belongs
   lower down.

## Codebase shape

A single Go module (`dev.atomyx`) under `core/`, native agent
projects, and a separate desktop app:

- **`core/cmd/atomyx`** and **`core/cmd/atomyx-mcp`**: the CLI binary,
  which drives real devices directly, and the MCP server binary,
  which exposes the same core as a stdio tool surface for AI agents.
- **`core/internal/driver`**: the platform-neutral `Driver` port,
  implemented against hardware by `androiddriver`, `iosdriver`, and
  `iossimdriver` over the transport code in `core/internal/transport`,
  and in memory by `core/internal/driver/mock` for device-free tests.
- **`core/internal/orchestra`**: composes `Driver` calls into
  selector resolution, priority broadening, scroll-into-view,
  obscurement handling, and wait primitives. Every higher layer builds
  on `Orchestra`, never on a `Driver` directly.
- **`core/internal/script`** and **`core/internal/runner`**: the YAML
  script schema and the engine that runs a parsed script against an
  `Orchestra`.
- **`core/internal/mcp`**: the MCP tool handlers, each orchestrating
  `Orchestra`, or `runner` for `run_script`, never a `Driver` method
  directly.
- **`core/internal/cli`** and **`core/internal/config`**: the CLI's
  command router, and the YAML configuration schema and loader shared
  by the CLI and the MCP server.
- **`platforms/android-agent`** and **`platforms/ios-agent`**: the
  native control planes. Android hosts an HTTP control server on the
  device, reached over `adb forward`. iOS runs a Swift XCUITest
  runner, reached over TCP JSON on `127.0.0.1:22087`, tunneled with
  `iproxy` for a physical device, bundle id `dev.atomyx.driver.host`.
- **`apps/studio`**: a separate desktop IDE with its own layer rules,
  see `.claude/rules/studio-architecture.md` before touching it.

Dependency direction between `core/internal` packages is enforced by
`go-arch-lint` reading `core/.go-arch-lint.yml`, run locally through
`make check`.

Full contract and rules: [`.claude/docs/architecture.md`](./.claude/docs/architecture.md).
Concrete layout and per-package responsibilities: [`.claude/docs/repo-map.md`](./.claude/docs/repo-map.md).

## Non-negotiable rules

These rules exist because they caught real regressions. Violating
them is how agents reintroduce bugs the team already fixed.

1. **Never branch on the active platform inside an MCP tool handler.**
   A handler in `core/internal/mcp` must not switch on which platform
   the current session is bound to. When behavior must differ per
   platform, it belongs in the driver adapter (`androiddriver`,
   `iosdriver`, `iossimdriver`), not in the tool layer.
2. **Never add a tool that overlaps an existing tool's intent.** The
   MCP surface stays small on purpose. Before creating a new tool,
   confirm no existing tool in `core/internal/mcp` can absorb the
   intent through a parameter. Overlap causes measurable agent
   confusion.
3. **Never inline business logic in a tool handler.** A handler in
   `core/internal/mcp` is pure orchestration over `Orchestra` methods,
   or over `runner` for `run_script`. Inline logic is a regression
   even when it is only a few lines. Add a method to `Orchestra` or
   to the runner instead.
4. **Never cache an accessibility node across tool calls on Android.**
   Nodes go stale. Capture bounds at dump time and re-resolve a fresh
   node for any action that needs one.
5. **Never run an HTTP server inside an iOS app.** The sandbox kills
   it. The iOS control plane runs as an XCUITest bundle, not a
   host-app process.
6. **Never change the iOS bridge strategy without owner direction.**
   The Swift XCUITest runner is the committed approach. Alternative
   bridges are out of scope until explicitly reopened.
7. **Never let a `core/internal` package reach outside its declared
   dependencies.** Go already keeps other modules from importing
   `core/internal`. Within the module, respect the one-way graph
   `go-arch-lint` enforces: `driver` never imports `orchestra`, `mcp`,
   or `cli`, and one platform driver never imports another.

Full pitfall list: [`.claude/docs/pitfalls.md`](./.claude/docs/pitfalls.md).
Read it before editing the Android control plane, the tool layer, or
the iOS adapter.

## Design principles

Every change must respect:

1. **Cross-platform by design.** Every capability goes through the
   `Driver` port. Port names are platform-neutral: `CurrentForeground`
   reports the app in front regardless of platform, and no method name
   names a specific platform's native concept.
2. **Selector-first, coordinates first-class.** The MCP selector
   (shared by `tap`, `input_text`, `find_element`, and
   `wait_for_element`) carries `id`, `text`, `label`, `hint`, `value`,
   `role`, `enabled`, `clickable`, `focused`, and `nth`. `find_element`
   and `wait_for_element` also resolve a tap-ready `center` point from
   the matched node's `bounds`. When no stable id exists, the center
   point is the canonical fallback, not a hack.
3. **Priority broadening.** The downstream agent should not need to
   know platform conventions. `CompileSelector` in
   `core/internal/orchestra/priority_broadening.go` tries each content
   field in a fixed priority order (id, then label, then text, then
   value, then hint) and returns the first field that matches,
   regardless of which field the caller populated.
4. **Strategy and registry patterns.** Step handlers, driver
   implementers, selector filters, and wait primitives all use the
   strategy or registry pattern. Adding a new variant means adding a
   new file, never modifying an existing one.
5. **SOLID.** One responsibility per file. Constructor injection over
   package-level singletons. Extensions are additions, not
   modifications.
6. **No unit-testable code requires a device.** `core/internal/driver/mock`
   implements `Driver` in memory, and `core/internal/conformance` runs
   the same behavioral corpus against every implementer, mock and real
   alike, with no device attached for the mock leg.

Fuller rationale and boundary enforcement: [`.claude/docs/architecture.md`](./.claude/docs/architecture.md).

## Before you act

| You are about to... | First read |
| --- | --- |
| Understand the architectural contract | [`.claude/docs/architecture.md`](./.claude/docs/architecture.md) |
| Locate a package or trace the repo layout | [`.claude/docs/repo-map.md`](./.claude/docs/repo-map.md) |
| Edit anything under `core/internal/mcp` | [`.claude/docs/tools.md`](./.claude/docs/tools.md) |
| Call an MCP tool as a consumer | [`docs/mcp-tools.md`](./docs/mcp-tools.md) |
| Write or edit a YAML test script | [`docs/yml-script-reference.md`](./docs/yml-script-reference.md) |
| Run build, test, or smoke checks, or add a new tool | [`.claude/docs/development.md`](./.claude/docs/development.md) |
| Edit Android, iOS, or tool-layer code | [`.claude/docs/pitfalls.md`](./.claude/docs/pitfalls.md) |
| Touch anything iOS-related | [`.claude/docs/ios.md`](./.claude/docs/ios.md) |
| Touch anything Android-related | [`.claude/docs/android.md`](./.claude/docs/android.md) |
| Check proxy, install a certificate, or manage screen lock on Android | [`.claude/docs/android-device-setup.md`](./.claude/docs/android-device-setup.md) |
| Write or edit any comment or docstring | [`.claude/rules/comments.md`](./.claude/rules/comments.md) |
| Write or edit any file under `docs/` or `.claude/docs/` | [`.claude/rules/docs.md`](./.claude/rules/docs.md) |
| Edit anything under `apps/studio/` | [`.claude/rules/studio-architecture.md`](./.claude/rules/studio-architecture.md) |

Updating a tool without reading `.claude/docs/tools.md` first is how
regressions get reintroduced. Treat these docs as required reading
for the task at hand, not optional reference.

## When in doubt

- If a request would grow the MCP tool surface, stop and ask whether
  an existing tool should be extended instead.
- If a change would require a platform branch inside a tool handler,
  stop and ask, because the driver adapter is the right place.
- If a pitfall in `.claude/docs/pitfalls.md` conflicts with what the
  user asked for, surface the conflict before acting.

## Licensing

All files are MIT licensed (see `LICENSE`). Do not add code with an
incompatible license. Do not copy from GPL projects.
