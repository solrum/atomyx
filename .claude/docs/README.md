# Atomyx — Implementation Documentation

This directory holds **contributor and AI-agent documentation**:
the internal architecture, build/test workflow, iOS / Android
implementation deep dives, known pitfalls, and tool
implementation reference. Tracked in git (despite living under
`.claude/`) so collaborators and future Claude sessions can read
it.

**End-user documentation lives in [`docs/`](../../docs/)** —
getting started, install guides, MCP setup, recipes. The split
exists so that the user-facing `docs/` directory stays focused on
HOW TO USE Atomyx, while this directory holds HOW ATOMYX IS
BUILT.

## Contents

- [`architecture.md`](./architecture.md) — opt-in modular
  ecosystem **contract**: persona-driven module split,
  cross-package boundary rules, interface layering (CLI / MCP
  / HTTP), feature-discovery via npm. The WHY and the RULES.
  Read this first to understand how Atomyx is supposed to be
  built.
- [`repo-map.md`](./repo-map.md) — concrete repo layout
  reference: which package lives where, the current 19-tool
  surface, design tenet implementations, decision log pointers.
  The WHAT and the WHERE. Read this when you need to find a
  module or trace a layer.
- [`development.md`](./development.md) — build, test, smoke,
  add-a-tool workflow.
- [`tools.md`](./tools.md) — tool implementation reference:
  file paths, response shapes, invariants. Read before editing
  anything under `packages/core-driver-mcp/src/tools/` or the
  legacy `src/tools/`.
- [`pitfalls.md`](./pitfalls.md) — known traps the team has hit
  once. Read before touching the corresponding subsystem.
- [`ios.md`](./ios.md) — iOS implementation deep dive: bridge
  decision log, Phase tracking, Swift driver internals, real
  device setup, CI matrix.

## Why under `.claude/`?

Two reasons:

1. **Tooling discoverability** — Claude Code (and other AI
   agents) automatically reads `.claude/` for project context.
   Putting implementation docs here means future AI sessions
   working on this codebase find them without needing the user
   to point them out.

2. **Separation of concerns** — `docs/` is for end users; the
   contributor view doesn't belong in the same tree. Mixing them
   creates a `docs/` directory full of internal jargon
   ("strangler fig", "obscurement detection", "Driver port") that
   confuses someone who just wants to install the framework.

## Why tracked in git?

Despite the `.claude/` location, this directory is exempted from
`.gitignore` so contributors who clone the repo get the docs.
The rest of `.claude/` (settings, caches, per-machine state)
remains ignored.

## When adding a new doc

- **End-user content** → `docs/`
- **Contributor / implementation content** → `.claude/docs/`
- **Decision records that affect future code** →
  `.claude/docs/<area>.md` decision-log section
- **AI-agent runtime instructions** → `CLAUDE.md` at repo root
  (those are loaded into every session, so keep them tight)
