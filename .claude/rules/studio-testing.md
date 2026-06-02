# Atomyx Studio — testing rules

**Scope**: this file applies to source files under `apps/studio/`
ONLY. The rest of the workspace (`packages/`, `platforms/`) tests
under its own per-package conventions and is unaffected by anything
below.

**Purpose**: answers "how is Studio tested, and what must I cover
before merging?". Read it before adding a test, adding a feature
with logic, or changing the test/coverage tooling. It does NOT
cover the layer-import rules (`studio-architecture.md`) or the
feature folder shape (`feature-structure.md`).

If a rule here conflicts with what a task requires, stop and
surface the conflict — do not quietly bend it.

---

## 1. Layer → test-type matrix

Studio is four layers (`ui → state → domain ← platform`). Each
layer is tested differently because each has a different amount of
pure logic and a different cost to exercise.

| Layer | What to test | Runner | Required? |
|---|---|---|---|
| `domain/` | Pure functions, validators, ports' pure logic | `node:test` (no DOM) | Yes — any file with branching logic |
| `state/` | Store factories (`*.zustand.ts` / `*.impl.ts`): state transitions, derived snapshots | `node:test` (no DOM) | Yes — any store with non-trivial transitions |
| `platform/` | Adapters, by mocking `@tauri-apps/api` `invoke` | `node:test` (no DOM) | Encouraged, not gated |
| `ui/` | React components, rendering, interaction | jsdom suite (future — see §6) | Deferred |

Tests are colocated with the code under test (`foo.test.ts` next
to `foo.ts`), never in a parallel `tests/` tree.

---

## 2. The DOM-free invariant

The `domain/` and `state/` suites MUST run under `node:test` with
no bundler, no `jsdom`, and no React. A test that imports `react`,
`react-dom`, `monaco-editor`, or touches `document` / `window`
blows up at load time — and that is the point. If a test needs the
DOM, the code under test belongs in `ui/`, not `domain/` or
`state/`.

The only React allowed under `state/` is the feature's own
`index.ts` hook (`useXxx` over `useSyncExternalStore`); that hook
is exercised by UI tests, not by the `node:test` suite. Unit tests
target the store factory directly (`createZustandEditor()`), not
the hook.

---

## 3. What MUST have a test before merge

A change is not done without a test when it:

- adds or edits a `domain/` function with a branch, a loop, or a
  validator;
- adds or edits a `state/` store transition, async action, or
  derived snapshot;
- fixes a bug in `domain/` or `state/` — add the regression test
  that fails before the fix.

A change does NOT need a test when it is:

- pure `ui/` rendering / styling (until the jsdom suite lands);
- a thin `platform/` `invoke` passthrough with no branching;
- type-only edits, comments, or docs.

When unsure, write the test — the floor only ratchets up.

---

## 4. Coverage policy

Coverage is measured by `c8` over `src/domain` and `src/state`
only — the two pure-logic layers. `ui/` has no runner yet and
`platform/` is mostly untestable invoke glue, so gating them would
reward writing thin wrappers, not real tests. They are excluded
from the metric, not from the codebase.

- The threshold lives in `apps/studio/.c8rc.json`.
- The threshold only ever **ratchets up**. When a batch of tests
  lands and raises measured coverage, raise the floor to match in
  the same change. Never lower it to make a red build green —
  fix the test or the code.
- A drop below the floor fails `npm run test:coverage`, which is a
  blocking CI step (§5).

---

## 5. Enforcement

Two gates, both blocking:

- **Local**: `npm run test:coverage` runs the full `node:test`
  suite under `c8 --check-coverage` against the
  `.c8rc.json` thresholds. `npm run test` runs the suite without
  the coverage check for fast iteration.
- **CI**: `.github/workflows/studio-ci.yml` runs on push / PR
  touching `apps/studio/` and its workspace dependencies. It runs
  `typecheck`, `lint`, `depcruise`, `lint:feature-api`, and
  `test:coverage`. A failure on any blocks merge.

When you raise the coverage floor, confirm CI is green at the new
floor before merging.

---

## 6. UI and end-to-end testing (deferred)

There is no jsdom / component test suite and no end-to-end suite
yet. When one is introduced:

- It runs as a **separate** suite (its own script + config) so the
  `domain/` and `state/` `node:test` suites stay DOM-free (§2). A
  jsdom global must never leak into the pure-logic runner.
- Component tests live colocated as `*.test.tsx` under `ui/`.
- Coverage from a UI suite is tracked separately; it does not mix
  into the `domain`+`state` floor until the policy is revisited.

Until that suite exists, `ui/` correctness is verified by running
the app, not by automated tests. Say so explicitly when reporting
a UI change as done — type-checking and the logic suite do not
prove a component renders.

---

## 7. Pre-commit self-check

1. Did I add or change `domain/` / `state/` logic? Is there a
   colocated test for it?
2. Does any `domain/` or `state/` test import React / DOM / a
   bundler-only module? If so, the code or the test is in the
   wrong layer.
3. Did `npm run test:coverage` pass at or above the current floor?
4. If coverage rose, did I ratchet the floor in `.c8rc.json` in
   the same change?

If any answer is wrong, fix it before committing.
