# Atomyx: Implementation documentation

## Purpose

Who reads this: a contributor or AI agent orienting inside this
directory for the first time.

When to read it: before opening any other file under
`.claude/docs/`, to pick the one that answers your actual question.

What this does not cover: end-user documentation (see
[`../../docs/`](../../docs/)) or any content in
`.claude/docs/decisions/` or `.claude/docs/proposals/`, both
gitignored and kept per contributor rather than shipped with the
repository.

## Index

| File | Question it answers |
|---|---|
| [`architecture.md`](./architecture.md) | What contract is Atomyx built on, and why? |
| [`repo-map.md`](./repo-map.md) | Where do I find a package, a binary, or a workflow file? |
| [`development.md`](./development.md) | How do I build, test, and extend the runtime? |
| [`tools.md`](./tools.md) | What does each MCP tool do, and which method does it call? |
| [`pitfalls.md`](./pitfalls.md) | What traps does a given subsystem have? |
| [`android.md`](./android.md) | How does the Android driver work, and what quirks does it have? |
| [`ios.md`](./ios.md) | How does the iOS driver work, and what quirks does it have? |
| [`android-device-setup.md`](./android-device-setup.md) | How do I set up a proxy, a certificate, or screen-lock behavior on an Android device? |

`.claude/docs/decisions/` (ADRs) and `.claude/docs/proposals/`
(unshipped design sketches) are gitignored and local to each
contributor. When a decision or proposal ships, its rationale is
folded into the relevant reference file above and the local file is
deleted rather than kept as a historical record.

## Why under `.claude/`

Two reasons:

1. **Tooling discoverability**: Claude Code and other AI agents read
   `.claude/` for project context automatically, so an agent working
   on this codebase finds these docs without being pointed at them.
2. **Separation of concerns**: [`docs/`](../../docs/) at the
   repository root is for end users installing and using Atomyx.
   Mixing implementation detail (the Driver port, selector priority
   broadening, the conformance corpus) into that tree would confuse a
   reader who only wants to install the CLI.

Despite the `.claude/` location, this directory is exempted from
`.gitignore` so every contributor gets it on clone. The rest of
`.claude/` (settings, caches, per-machine state) stays ignored.

## See also

- [`../rules/docs.md`](../rules/docs.md): rules for every markdown
  file in the repository. Read before creating or editing any of
  them.
- [`../rules/comments.md`](../rules/comments.md): rules for inline
  comments and doc comments in Go, Kotlin, and Swift source.
- [`../rules/studio-architecture.md`](../rules/studio-architecture.md)
  and [`../rules/studio-testing.md`](../rules/studio-testing.md):
  layer and testing rules scoped to `apps/studio/`.

## Adding a new doc

Before writing, read [`../rules/docs.md`](../rules/docs.md) and
confirm the new file answers a question none of the files above
already answers:

- **End-user content** goes to [`docs/`](../../docs/).
- **Contributor or implementation reference** goes to
  `.claude/docs/`, with an entry added to the index above.
- **Unshipped design sketches or accepted decisions worth
  preserving locally** go to `.claude/docs/proposals/` or
  `.claude/docs/decisions/` as one short-slug-named file per
  proposal or decision. Both directories are gitignored.
- **AI-agent runtime instructions** go to the repository root
  `CLAUDE.md`, loaded into every session, so keep it tight.
