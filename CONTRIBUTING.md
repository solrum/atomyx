# Contributing to Atomyx

Atomyx is an open-source mobile test orchestration framework at `v0.1.0`.
Contributions from humans and AI agents are welcome — bug reports, driver
adapters, script engine fixes, platform coverage, documentation.

## First-time setup

Node 22 is required (see `.nvmrc`). If you use nvm:

```bash
nvm use
```

Then clone and install:

```bash
git clone https://github.com/solrum/atomyx.git
cd atomyx
npm run setup   # npm ci + husky git hooks
```

`npm run setup` installs all workspace dependencies and wires the husky
pre-commit and pre-push hooks. You should see a confirmation that hooks
were installed.

This sets up TypeScript packages only. Native toolchains (Android APK,
iOS XCUITest runner) require additional steps — see
[`.claude/docs/android.md`](./.claude/docs/android.md) and
[`.claude/docs/ios.md`](./.claude/docs/ios.md).

## Quality gates

Atomyx is local-first — there is no CI server. Correctness is the
contributor's responsibility before pushing.

### Fast gate (run often)

```bash
npm run check:fast
```

Runs lint, TypeScript typecheck, filename-prefix lint, and banned-phrase
scan. Takes a few seconds. Run this before committing.

### Full gate (run before push)

```bash
npm run check
```

Runs everything: lint, typecheck, dep-cruiser, feature-api lint,
prefix lint, phrase lint, doc-path lint, tests, coverage check, API
snapshots, wire schema snapshots, audit, and license check. The pre-push
hook runs this automatically.

**Audit note**: the `audit` step runs at `--audit-level=critical`. A set
of high-severity CVEs in `fast-uri` and `ajv` (transitive via
`@modelcontextprotocol/sdk`) are excluded because no upstream fix is
available and the affected code paths (URL parsing inside MCP's JSON
Schema validator) are not reachable from user-controlled input in Atomyx.
Re-evaluate when a fixed `@modelcontextprotocol/sdk` version ships.

### Hooks

- **Pre-commit**: runs lint-staged on changed files.
- **Pre-push**: runs `npm run check`.

Do not bypass hooks with `--no-verify` unless you have already run the
full gate locally and confirmed it is green.

## The rules

Atomyx has codified rules with enforcement gates. Read the relevant rule
before making a change — skipping this step is the most common reason
PRs are sent back.

| You are about to… | Read |
|---|---|
| Design or evaluate a module or feature boundary | [`.claude/docs/architecture.md`](./.claude/docs/architecture.md) |
| Create a new file or feature in any TS package or app | [`.claude/rules/feature-structure.md`](./.claude/rules/feature-structure.md) |
| Export anything from a feature's `index.ts` | [`.claude/rules/feature-api.md`](./.claude/rules/feature-api.md) |
| Edit anything under `apps/studio/` | [`.claude/rules/studio-architecture.md`](./.claude/rules/studio-architecture.md) |
| Write or edit any comment or docstring | [`.claude/rules/comments.md`](./.claude/rules/comments.md) |
| Write or edit any markdown file | [`.claude/rules/docs.md`](./.claude/rules/docs.md) |
| Write or edit Studio tests | [`.claude/rules/studio-testing.md`](./.claude/rules/studio-testing.md) |
| Edit Android or iOS agent code, or tool-layer code | [`.claude/docs/pitfalls.md`](./.claude/docs/pitfalls.md) |

Each rule has an associated enforcement gate (dep-cruiser, ESLint,
lint scripts, node:test coverage). Rules drift without enforcement — the
gate is the rule.

## Adding a feature

A new feature is one folder and one composition-root line. Removing it
is one folder delete and one line delete. No other file should change.

Follow the checklist in
[`.claude/rules/feature-structure.md`](./.claude/rules/feature-structure.md)
for the folder shape and the checklist in
[`.claude/rules/feature-api.md`](./.claude/rules/feature-api.md)
for the `index.ts` contract. Both checklists must be green before a
feature PR is considered complete.

## Editing a public API

Any change to a domain port, a wire schema, or a zod schema exported
from `@atomyx/shared` is a breaking change and requires a changeset:

```bash
npx changeset add
```

Select the affected packages and pick a semver bump (`patch`, `minor`,
or `major`). Commit the generated changeset file alongside your code
change. One changeset per PR.

If the change affects API snapshots or wire snapshots, regenerate them:

```bash
npm run api:check        # check — fails if snapshots are stale
node scripts/snapshot-wire-schema.mjs   # regenerate wire snapshots
```

Commit the updated snapshot files in the same PR as the change.

## Tests

Tests are colocated with the code they cover (`foo.test.ts` next to
`foo.ts`), never in a parallel `tests/` tree.

The runner is `node:test`. Run a single package:

```bash
cd packages/driver
npm test
```

Run all packages:

```bash
npm test
```

### DOM-free invariant

Tests under `domain/` and `state/` in `apps/studio/` must run under
`node:test` with no bundler, no `jsdom`, and no React. A test that
imports `react`, `react-dom`, or touches `document` or `window` fails
at load time — that is intentional. If a test needs the DOM, the code
under test belongs in `ui/`, not `domain/` or `state/`.

### Coverage ratchet

Coverage is measured by `c8` over the `domain/` and `state/` layers in
`apps/studio/`. The floor in `apps/studio/.c8rc.json` only ever goes
up. When new tests raise measured coverage, raise the floor to match
in the same PR. Never lower it to make a failing build green.

## Banned phrases

These phrases are blocked by `npm run lint:phrases` and must not appear
in source files or docs:

- **Milestone refs** — words like "Phase", "Batch", "Sprint", "Week",
  or "Milestone" followed by a number. Project-plan concepts don't
  travel with the source; describe the current state instead.
- **Past-thing refs** — phrases that name a thing that no longer exists
  (e.g. "the old X", "X was no longer used"). Describe what IS; git
  log owns what WAS.
- **ADR refs in code** — phrases like "per ADR-NNN" or "see ADR-NNN"
  in source or doc files. State the constraint inline, not by reference
  to a document the reader may not have.
- **SHIPPED status markers** — words like "SHIPPED" followed by a date.
  Docs describe present state; git keeps history.
- **Code-style comments inside template literals** (`//`, `/* */`) —
  they leak into user-facing strings.

## Commits and PRs

Follow the conventional commit format used throughout the repo:

```
feat(scope): short imperative description

Body explains WHY when the change is non-obvious. Keep the subject
under 72 characters. Reference issues with Fixes #N or Refs #N.
```

Common scopes: `android`, `ios`, `mcp`, `cli`, `driver`, `script`,
`studio`, `core`, `shared`, `skills`.

One concept per commit. A PR may contain multiple commits; each must
build and test green on its own.

- Do not use `--no-verify` or `--no-gpg-sign`.
- Co-authored-by trailers are fine for AI-assisted changes.

### PR description

Include:

1. **What** — one sentence on the user-visible change.
2. **Why** — the motivating problem, with a linked issue when one exists.
3. **How** — the design decision and any tradeoff.
4. **Test plan** — what you ran. For device-facing changes, name the
   platform, OS version, and app you tested against.

## Releases

Releases are maintainer-only and run from a local machine:

```bash
npm run release
```

This runs `changeset version` to apply pending changesets, then
`npm run check` to confirm the build is green, then `changeset publish`
to push packages to the registry. Do not run this as a contributor.

## License

Atomyx is Apache 2.0. By contributing, you agree your code ships under
the same terms. Do not submit code with incompatible licenses. Do not
copy from GPL projects.

See [`LICENSE`](./LICENSE) for the full text.
