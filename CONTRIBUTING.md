# Contributing to Atomyx

Thanks for your interest. Atomyx is an open-source framework at
`v0.1.0` (pre-release), and contributions from users, downstream
agent authors, and mobile platform experts are how we get it to
`v1.0`.

This document covers how to propose a change, set up a working
environment, and merge code that stays consistent with the
framework's design contract.

## Contents

- [Code of conduct](#code-of-conduct)
- [Ways to contribute](#ways-to-contribute)
- [Before you open a PR](#before-you-open-a-pr)
- [Development setup](#development-setup)
- [Coding standards](#coding-standards)
- [Commit and PR conventions](#commit-and-pr-conventions)
- [Review and merge](#review-and-merge)
- [License](#license)

## Code of conduct

Be respectful, direct, and patient. We welcome first-time
contributors and expect the same baseline of professional behavior
from everyone — maintainers and contributors alike. If something
feels off, open a GitHub issue tagged `coc` or email
security@atomyx.dev.

## Ways to contribute

All of the following are in-scope:

- **Bug reports.** Reproducible failures on a specific device + app.
  Attach the command you ran, the agent log, and (for UI issues) a
  screenshot or UI tree dump.
- **Feature proposals.** Open a GitHub discussion or issue describing
  the user problem first, before writing code. Atomyx is deliberately
  small-surface (27 tools, 17 YAML commands) and new surface area
  needs a design pass.
- **Platform support.** New `Driver` adapter (e.g. Web, a custom
  device fleet). Start from
  [`.claude/docs/architecture.md`](./.claude/docs/architecture.md)
  and the `Driver` port in
  `packages/driver/src/driver/driver.port.ts`.
- **App UI framework coverage.** If Atomyx misbehaves against a
  framework we already support (Flutter, Compose, RN), a reduced
  repro + the fix is the highest-value contribution type.
- **Documentation.** Corrections, clarifications, missing setup
  steps. Docs follow [`.claude/rules/docs.md`](./.claude/rules/docs.md) —
  read before editing.
- **Test cases.** Scripts or fixtures that exercise a gap in the
  current test matrix.

Out of scope for now:

- **Changes to the iOS bridge strategy.** The Swift XCUITest runner
  is the committed approach for v1.0 — see rule 7 in
  [`CLAUDE.md`](./CLAUDE.md).
- **New top-level modules** (test management, studio, cloud).
  Skeletons exist under `packages/test-mgmt/`, `packages/studio/`,
  `packages/cloud/` but are not yet ready for external contributions.

## Before you open a PR

1. **Open an issue first** for anything non-trivial. A 10-line bug
   fix does not need one; a new tool or a new driver adapter does.
2. **Search existing issues** for duplicates.
3. **Read the relevant docs**. The table in
   [`CLAUDE.md`](./CLAUDE.md#before-you-act) tells you which file to
   read before which kind of change. Skipping this step is the #1
   reason PRs get sent back.
4. **Confirm the non-negotiable rules** in `CLAUDE.md` and
   [`.claude/docs/pitfalls.md`](./.claude/docs/pitfalls.md). These
   exist because they caught real regressions.

## Development setup

```bash
git clone https://github.com/solrum/atomyx.git
cd atomyx
npm install
```

Build all TypeScript packages in dependency order:

```bash
for d in core driver driver-wire android-driver ios-driver script mcp cli; do
  (cd packages/$d && npx tsc)
done
```

Run tests for one package:

```bash
cd packages/driver
node --import tsx --test $(find src -name '*.test.ts')
```

Run tests across all packages:

```bash
for d in packages/*/; do
  (cd "$d" && [ -f tsconfig.json ] && npm test)
done
```

Platform-specific agent setup (Android APK, iOS XCUITest runner) is
covered in [`.claude/docs/development.md`](./.claude/docs/development.md).
Device-level prerequisites are in
[`docs/device-setup.md`](./docs/device-setup.md).

## Coding standards

Atomyx has codified rules that apply to every change:

- [`.claude/rules/comments.md`](./.claude/rules/comments.md) —
  inline comments and docstrings. Default: write no comment; comment
  WHY, not WHAT; never narrate the diff.
- [`.claude/rules/docs.md`](./.claude/rules/docs.md) — every
  markdown file in the repo. Present-tense only; paths must be
  verifiable; one file, one question.
- [`.claude/docs/architecture.md`](./.claude/docs/architecture.md) —
  module layering, cross-package boundary rules (enforced by
  `dependency-cruiser` at CI time).
- [`.claude/docs/tools.md`](./.claude/docs/tools.md) — contract for
  MCP tool authors: `defineTool`, pure orchestration, delegate to
  `Orchestra`, never reach the `Driver` directly.

General engineering expectations:

- **One responsibility per file.** Constructor injection over
  singletons.
- **No platform branching in tool handlers.** If behavior must
  differ per platform, fix it in the driver adapter.
- **Unit tests do not require a device.** Use `MockDriver` from
  `packages/driver/src/testing/`.
- **No commented-out code.** Git history has it.
- **English only** for all source, docs, and commit messages.

## Commit and PR conventions

- **Atomic commits.** One commit per logical change. A PR can have
  multiple commits but each should build and test green.
- **Commit message**: imperative mood, ≤ 72 chars subject, body
  explaining WHY when the change is non-obvious. Example:

  ```
  ios: reuse existing iproxy tunnel when port 22087 is occupied

  Second-session device reconnect was refusing to start because the
  port was still held by the prior `make serve-device`. Probe with a
  ping handshake; if an Atomyx driver answers, reuse it.
  ```

- **No `--no-verify`, no `--no-gpg-sign`**. Pre-commit hooks and
  signing exist for a reason.
- **Reference issues** with `Fixes #123` / `Refs #45` in the body.
- **Signed-off-by** (DCO) is encouraged for larger contributions;
  add `-s` to your `git commit`.

### Pull request template

When opening a PR, include in the description:

1. **What**: one sentence on the user-visible change.
2. **Why**: the motivating problem, ideally with a linked issue.
3. **How**: the design decision, especially any tradeoff.
4. **Test plan**: what you ran. For device-facing changes, name the
   platform + OS version + app you exercised against.

A PR template in `.github/PULL_REQUEST_TEMPLATE.md` will land
alongside issue templates in a future iteration.

## Review and merge

- Reviews focus on: correctness, design fit (hexagonal + non-
  negotiable rules), agent ergonomics, test coverage.
- At least one maintainer approval is required before merge.
- CI must be green — TypeScript type-check + unit tests across
  every workspace package.
- `dependency-cruiser` enforces cross-package boundaries at CI
  time. A violation blocks merge; the fix is almost always to
  route the import through a package's public entry point.
- Merge method: **squash** for single-author PRs, **merge commit**
  for multi-contributor PRs where authorship matters.

## License

Atomyx is Apache 2.0. By contributing, you agree your contribution
is licensed under the same terms. Do not submit code with
incompatible licenses; in particular, **do not copy from GPL
projects**.

See [`LICENSE`](./LICENSE) for the full text.
