# ATOMYX ARCHITECTURE — OPT-IN MODULAR ECOSYSTEM

## 1. CORE PHILOSOPHY: DECOUPLED ECOSYSTEM
Atomyx is NOT a single application. It is a suite of independent, opt-in
modules.

- **The User is King:** Users install only what they need.
- **Example:** A pure developer who only wants device interaction installs
  `@atomyx/cli` alone — no test-management, no studio, no cloud.
- **Persona-driven:** Every module exists because a real user persona needs
  it standalone (see §6).

## 2. MODULE INDEPENDENCE — DISTRIBUTION-LEVEL, NOT RUNTIME-LEVEL

Atomyx separates **two kinds of modularity** that are easy to confuse:

| | Distribution modularity | Runtime modularity |
|---|---|---|
| **What** | "Install only what you need" | "Modules talk over network at runtime" |
| **Mechanism** | Separate npm packages, opt-in install | HTTP / IPC between processes |
| **Cost** | Near zero | Latency, process orchestration, type-safety loss |
| **Atomyx uses** | ✅ Yes | ❌ No |

**Atomyx targets distribution modularity, NOT runtime modularity.**

Each module ships as one or more independently-installable npm packages.
Power users install all of them; pure developers install one. **At runtime,
modules link in-process via standard ES imports** — no HTTP between them,
no separate processes to orchestrate, no serialization overhead, full
TypeScript type safety preserved end-to-end.

The tradeoff we make: in-process composition trades inter-module HTTP
overhead + per-module deployment autonomy for operational simplicity and
type safety end-to-end. The sharp edge is that a process crash takes
every module down together; the benefit is that a single install yields
a working setup without orchestrating multiple services.

When the day comes that a module needs to scale to a separate team, a
separate language, or a separate deployment tier, the boundaries are
already clean (enforced by `dependency-cruiser` — see §4) and the
migration to runtime modularity is incremental, not a rewrite.

## 3. INTERFACE LAYERING

Every module follows the same internal layering:

```
┌─────────────────────────────────────────────┐
│  CORE LOGIC (TypeScript library)            │
│  — pure functions, no I/O assumptions       │
│  — the primary execution engine             │
└──────────────┬───────────────┬──────────────┘
               │               │
       ┌───────┴────┐     ┌────┴──────┐
       ▼            ▼     ▼           ▼
┌──────────┐  ┌───────────┐  ┌──────────────┐
│  CLI     │  │  Studio   │  │ MCP server   │
│  binary  │  │  (Tauri,  │  │ (stdio)      │
│          │  │  Node     │  │ — optional   │
│          │  │  sidecar) │  │   AI adapter │
└──────────┘  └───────────┘  └──────────────┘
                                     │
                          (optional) ▼
                            ┌──────────────┐
                            │ HTTP server  │
                            │ (browser /   │
                            │  non-TS /    │
                            │  remote use) │
                            └──────────────┘
```

Studio consumes the core runtime via a `StudioRuntime` port —
default adapter spawns a Node sidecar that links the core as a
library. MCP is a SECONDARY adapter that wraps the same core for
AI-agent consumers; Studio can opt into it via settings but does
not depend on it. See ADR-005 for the rationale and contract.

Rules:

1. **Core logic is the canonical implementation.** It is a pure TypeScript
   library — exports types, classes, and functions. No transport, no I/O
   beyond what the Driver port abstracts.

2. **CLI, Studio, MCP, and HTTP are PARALLEL consumers** of the core
   logic. They are siblings, not stacked layers. MCP does NOT wrap
   HTTP; both call the core directly. Studio in Tauri talks to the
   core through a Node sidecar it owns — no MCP required for humans.

3. **HTTP is OPTIONAL** within a module. Ship it when there's a real
   external consumer that cannot link the core as a library (browser,
   non-TS language, remote Studio scenario). Until that consumer exists,
   don't ship HTTP — it's surface area to maintain for nothing.

4. **MCP wraps the core, not HTTP.** AI agents talking to a local MCP
   server hit the core in the same process — zero network hops, full
   speed, full type safety up to the MCP boundary.

5. **MCP is optional**, not a middleware the human tools depend on.
   CLI and Studio call the core runtime directly; MCP is a separate
   adapter that AI-agent sessions opt into. Building Studio features
   that require MCP when the user has not opted in is a regression.

## 4. CROSS-MODULE BOUNDARIES — ENFORCED BY LINT

Modules are independent at the **package** level: each module ships as
its own npm package(s) with its own `package.json`, version, changelog,
and publish pipeline. They can be split into separate repositories at
any point with `git subtree split` if the team scales beyond one.

**Imports across packages go through public entry points only:**

```ts
// ✅ ALLOWED — studio imports driver's PUBLIC API
import { Orchestra, IosDriver } from "@atomyx/driver";

// ❌ FORBIDDEN — reaching into driver internals
import { ScrollController } from "@atomyx/driver/src/scroll/...";
import { internalThing } from "@atomyx/driver/dist/internals/...";
```

This is enforced by `dependency-cruiser` configured in the repo root:

```js
// .dependency-cruiser.cjs
forbidden: [
  {
    name: "no-cross-package-deep-imports",
    from: { path: "^packages/" },
    to: {
      path: "^packages/[^/]+/src/",
      pathNot: "/index\\.ts$",
    },
  },
];
```

The rule operates on the package directory level (`packages/<name>/`).
Module ownership is encoded in the package name convention, not in
directory hierarchy. The
filesystem layout stays flat for ergonomics; the conceptual modules are
inferred from naming.

CI fails on violations. This gives the same loose-coupling guarantee as
HTTP boundaries (no module can secretly depend on another's internals)
**without** the runtime cost.

## 5. FEATURE DISCOVERY — GRACEFUL DEGRADATION

Modules detect siblings at install time via npm dependency resolution,
NOT at runtime via network probes:

```ts
// inside any consumer that wants test-management as an optional sibling
let testRepo: TestCaseRepo | null = null;
try {
  const mod = await import("@atomyx/test-mgmt");
  testRepo = new mod.TestCaseRepo();
} catch {
  // @atomyx/test-mgmt not installed — disable test-mgmt UI section
}
```

If a user installs only `@atomyx/cli`, the optional
`@atomyx/test-mgmt` import fails at runtime and the consumer
gracefully reports "I have driver, no test repository" — exactly the
behavior the previous ARCHITECTURE.md draft required, achieved via npm rather than HTTP
probing.

## 6. USER PERSONAS

Each persona maps to a specific install:

| Persona | Installs | Gets |
|---|---|---|
| **Pure Developer** | `@atomyx/cli` | Device interaction via CLI + MCP. ~5-10 MB. |
| **QC Manager** | `@atomyx/test-mgmt` | Standalone test-case + report manager. No driver. |
| **Power User** | `@atomyx/studio` | GUI bundling everything. Auto-detects which siblings are installed. |
| **CI Pipeline** | `@atomyx/cli` + `@atomyx/test-mgmt` | Headless run-and-report. |
| **Cloud / Scale Operator** | `@atomyx/cloud` (future) | Remote device farm orchestration. |

Each module has its own `bin` and its own MCP server entry point. The
power user case is composed at install time (npm pulls everything) and
linked at runtime (Studio imports the others as libraries).

## 7. ECOSYSTEM DIAGRAM

```text
                                ┌──────────────┐
                                │  HUMAN USER  │
                                └──────┬───────┘
                                       │
                ┌──────────────────────┴──────────────────────┐
                ▼                                             ▼
       ┌─────────────────┐                           ┌─────────────────┐
       │  ATOMYX STUDIO  │                           │   ATOMYX CLI    │
       │      (GUI)      │                           │  (per module)   │
       └────────┬────────┘                           └────────┬────────┘
                │                                             │
                │  in-process imports                         │  in-process
                │  (NOT HTTP)                                 │  imports
                │                                             │
                ▼                                             ▼
 ╔═════════════════════════════════════════════════════════════════════╗
 ║         INDEPENDENT NPM PACKAGES (the Ecosystem)                    ║
 ║                                                                     ║
 ║  ┌─────────────────────┐  ┌──────────────────────┐                  ║
 ║  │ @atomyx/core +      │  │ @atomyx/test-mgmt    │                  ║
 ║  │ @atomyx/driver      │  │ - Case manager       │                  ║
 ║  │ - Driver port       │  │ - Report storage     │                  ║
 ║  │ - Orchestra         │  │ - YML parser         │                  ║
 ║  │ - Filters / scroll  │  │ - Persona: QC Mgr    │                  ║
 ║  │ - iOS / Android     │  │                      │                  ║
 ║  │   driver impls      │  │                      │                  ║
 ║  └─────────────────────┘  └──────────────────────┘                  ║
 ║                                                                     ║
 ║  ┌─────────────────────┐                                            ║
 ║  │ @atomyx/cloud       │  (future)                                  ║
 ║  │ - Remote devices    │                                            ║
 ║  │ - Worker pool       │                                            ║
 ║  └─────────────────────┘                                            ║
 ╚═════════════════════════════════════════════════════════════════════╝

       AGENT (MCP) ─────► each module's MCP server (in-process)
       SCRIPT  ─────────► each module's CLI binary
       BROWSER ─────────► each module's HTTP server (optional, only when needed)

   Cross-module imports allowed AT PUBLIC ENTRY POINTS ONLY.
   Deep imports into another module's internals → CI fail (dep-cruiser).
```

## 8. Wire protocols

Atomyx has two distinct wire protocols — one per platform. There is no
shared cross-platform wire schema. The `Driver` port in `@atomyx/driver`
abstracts both; adapter authors implement that port, not a wire schema.

### Android HTTP

- **Transport**: HTTP over `127.0.0.1:8765` via `adb forward`.
- **Path namespace**: `/actions/*` for mutations, `/tree` for the UI
  hierarchy, `/current-activity`, `/health`, `/ping` for meta.
- **Response envelope**: write actions return
  `DispatchResult { ok: boolean, reason?: string, code?: string }`.
- **Runner spec**: `platforms/android-agent/…/router/CommonRoutes.kt`.
- **Host adapter**: `packages/android-driver/src/android.driver.ts`.

### iOS TCP+JSON

- **Transport**: TCP JSON stream on `127.0.0.1:22087`; `iproxy` tunnels
  physical devices. Bundle id: `dev.atomyx.driver.host`.
- **Message envelope**: `{ id: string, type: string, args: object }` for
  requests; per-command response shapes vary by command type.
- **Runner spec**: `platforms/ios-agent/Sources/…/*Command.swift` files.
- **Host adapter**: `packages/ios-driver/src/ios.driver.ts`.

### Shared types only

`@atomyx/driver-wire` provides `TreeNodeWire` and primitive types
(`PointWire`, `BoundsWire`, etc.) that normalizers in each adapter
produce. These are TS type declarations — not a wire schema, not a
validation layer, and not a constraint on what bytes cross the wire.

---

## 9. WHAT THIS DOCUMENT IS NOT

- It is **not** a microservices manifesto. Microservices solve cross-team
  / cross-language deployment problems Atomyx does not have.
- It is **not** a license to import everywhere. Public entry points are
  the contract; deep imports break it.
- It is **not** anti-HTTP. HTTP is one of three transports a module can
  ship (CLI, MCP, HTTP) — added when a real consumer needs it.

The goal is the same as v1 of this document: **install only what you
need, modules independently developable, gracefully degrade**. The
mechanism is different: **npm-level distribution + in-process composition
+ lint-enforced boundaries**, not HTTP-between-processes.
