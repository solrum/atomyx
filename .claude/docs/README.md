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

- [`architecture.md`](./architecture.md) — the contract Atomyx
  is built on: persona-driven module split, cross-package
  boundary rules, interface layering (CLI / MCP / HTTP),
  feature discovery via npm. Read first.
- [`repo-map.md`](./repo-map.md) — repo layout: which package
  lives where, tool surface pointer. Read when locating a
  module.
- [`tools.md`](./tools.md) — tool implementation reference:
  file paths, response shapes, invariants, Orchestra contract,
  observable state + wait primitives. Read before editing
  anything under `packages/mcp/src/tools/` or
  `packages/driver/src/{state,waits}/`.
- [`development.md`](./development.md) — build, test, smoke,
  and extension checklists.
- [`pitfalls.md`](./pitfalls.md) — known traps per subsystem.
  Read before touching the relevant code.
- [`ios.md`](./ios.md) — iOS driver internals: architecture
  split, host-side layering, platform quirks, extension points.
- [`android.md`](./android.md) — Android driver internals:
  APK + host-side layering, command surface, quirks, latency.
- [`status.md`](./status.md) — current version, active branch,
  per-package test counts, known limitations, out-of-scope
  roadmap.
> Architecture decision records live in
> `.claude/docs/decisions/` and unshipped design sketches in
> `.claude/docs/proposals/` — both gitignored, kept per-
> contributor. Internal deliberations don't ride with the
> shipped docs tree.

See also:

- [`../rules/docs.md`](../rules/docs.md) — rules for every docs file
  (both this directory and `docs/`). Read before creating or editing
  any markdown file in the repo.
- [`../rules/comments.md`](../rules/comments.md) — rules for inline
  comments and docstrings in `packages/`, `apps/`, `platforms/`,
  `shared/`.
- [`../rules/feature-structure.md`](../rules/feature-structure.md) —
  where files go inside a package or app.
- [`../rules/feature-api.md`](../rules/feature-api.md) — how a feature
  exposes itself (contract + factory + index), so swapping an impl
  touches one wire site.
- [`../rules/studio-architecture.md`](../rules/studio-architecture.md)
  — the four-layer rule that scopes to `apps/studio/`.

## Why under `.claude/`?

Two reasons:

1. **Tooling discoverability** — Claude Code (and other AI
   agents) automatically reads `.claude/` for project context.
   Putting implementation docs here means future AI sessions
   working on this codebase find them without needing the user
   to point them out.

2. **Separation of concerns** — `docs/` is for end users; the
   contributor view does not belong in the same tree. Mixing them
   creates a `docs/` directory full of internal jargon
   (obscurement detection, Driver port, snapshot walk) that
   confuses someone who just wants to install the framework.

## Why tracked in git?

Despite the `.claude/` location, this directory is exempted from
`.gitignore` so contributors who clone the repo get the docs.
The rest of `.claude/` (settings, caches, per-machine state)
remains ignored.

## When adding a new doc

Before writing, read [`../rules/docs.md`](../rules/docs.md) and
confirm the new file answers a question none of the existing files
answer. Then:

- **End-user content** → `docs/`
- **Contributor / implementation reference** → `.claude/docs/`
- **Unshipped design / accepted decision worth preserving locally** →
  `.claude/docs/proposals/<slug>.md` or
  `.claude/docs/decisions/NNN-short-slug.md`. Both directories
  are gitignored (kept per-contributor, not pushed).
- **AI-agent runtime instructions** → `CLAUDE.md` at repo root
  (loaded into every session — keep tight).
