# ATOMYX ARCHITECTURE — OPT-IN MODULAR ECOSYSTEM

## 1. CORE PHILOSOPHY: DECOUPLED ECOSYSTEM
Atomyx is NOT a single application. It is a suite of independent, opt-in
modules.

- **The User is King:** Users install only what they need.
- **Example:** A pure developer who only wants device interaction installs
  `@atomyx/core-driver-cli` alone — no test-management, no studio, no cloud.
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

This is the same pattern Playwright uses (`@playwright/test`,
`@playwright/browser-chromium`, `@playwright/browser-firefox` — opt-in
install, in-process composition). The opposite pattern is Appium
(everything talks HTTP, separate driver processes) — Atomyx deliberately
rejects that path because it pays high operational cost for benefits
that only matter at multi-team / multi-language scale, neither of which
applies to Atomyx today.

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
└──────────────────┬──────────────────────────┘
                   │
       ┌───────────┴───────────┐
       ▼                       ▼
┌──────────────┐      ┌──────────────────┐
│  CLI         │      │  MCP server      │
│  binary      │      │  (stdio)         │
└──────────────┘      └──────────────────┘
       │                       │
       │  (optional sibling)   │
       ▼                       ▼
┌─────────────────────────────────────┐
│  HTTP server (optional)             │
│  for browser / curl / external      │
│  consumers that can't link in-      │
│  process (e.g. a remote Studio)     │
└─────────────────────────────────────┘
```

Rules:

1. **Core logic is the canonical implementation.** It is a pure TypeScript
   library — exports types, classes, and functions. No transport, no I/O
   beyond what the Driver port abstracts.

2. **CLI, MCP, and HTTP are PARALLEL transports**, all wrapping the same
   core logic in-process. They are siblings, not stacked layers. MCP does
   NOT wrap HTTP; both call the core directly.

3. **HTTP is OPTIONAL** within a module. Ship it when there's a real
   external consumer that cannot link the core as a library (browser,
   non-TS language, remote Studio scenario). Until that consumer exists,
   don't ship HTTP — it's surface area to maintain for nothing.

4. **MCP wraps the core, not HTTP.** AI agents talking to a local MCP
   server hit the core in the same process — zero network hops, full
   speed, full type safety up to the MCP boundary.

## 4. CROSS-MODULE BOUNDARIES — ENFORCED BY LINT

Modules are independent at the **package** level: each module ships as
its own npm package(s) with its own `package.json`, version, changelog,
and publish pipeline. They can be split into separate repositories at
any point with `git subtree split` if the team scales beyond one.

**Imports across packages go through public entry points only:**

```ts
// ✅ ALLOWED — studio imports core-driver's PUBLIC API
import { Orchestra, IosDriver } from "@atomyx/core-driver";

// ❌ FORBIDDEN — reaching into core-driver internals
import { ScrollController } from "@atomyx/core-driver/src/scroll/...";
import { internalThing } from "@atomyx/core-driver/dist/internals/...";
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
Module ownership is encoded in the package NAME prefix (`core-driver-*`,
`test-mgmt-*`, `studio-*`, `cloud-*`), not in directory hierarchy. The
filesystem layout stays flat for ergonomics; the conceptual modules are
inferred from naming.

CI fails on violations. This gives the same loose-coupling guarantee as
HTTP boundaries (no module can secretly depend on another's internals)
**without** the runtime cost.

## 5. FEATURE DISCOVERY — GRACEFUL DEGRADATION

Modules detect siblings at install time via npm dependency resolution,
NOT at runtime via network probes:

```ts
// packages/studio/src/features.ts
let testRepo: TestCaseRepo | null = null;
try {
  const mod = await import("@atomyx/test-mgmt");
  testRepo = new mod.TestCaseRepo();
} catch {
  // @atomyx/test-mgmt not installed — disable test-mgmt UI section
}
```

If a user installs only `@atomyx/core-driver-cli`, the optional
`@atomyx/test-mgmt` import fails at runtime and the consumer
gracefully reports "I have driver, no test repository" — exactly the
behavior the previous ARCHITECTURE.md draft required, achieved via npm rather than HTTP
probing.

## 6. USER PERSONAS

Each persona maps to a specific install:

| Persona | Installs | Gets |
|---|---|---|
| **Pure Developer** | `@atomyx/core-driver-cli` | Device interaction via CLI + MCP. ~5-10 MB. |
| **QC Manager** | `@atomyx/test-mgmt-cli` | Standalone test-case + report manager. No driver. |
| **Power User** | `@atomyx/studio` | GUI bundling everything. Auto-detects which siblings are installed. |
| **CI Pipeline** | `@atomyx/core-driver-cli` + `@atomyx/test-mgmt-cli` | Headless run-and-report. |
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
 ║  │ @atomyx/core-driver │  │ @atomyx/test-mgmt    │                  ║
 ║  │ - Driver port       │  │ - Case manager       │                  ║
 ║  │ - Orchestra         │  │ - Report storage     │                  ║
 ║  │ - Filters / scroll  │  │ - YML parser         │                  ║
 ║  │ - iOS / Android     │  │ - Persona: QC Mgr    │                  ║
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

## 8. WHAT THIS DOCUMENT IS NOT

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
