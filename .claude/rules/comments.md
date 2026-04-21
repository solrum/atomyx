# Atomyx — Code comment rules

Rules for inline comments, doc comments, and docstrings in production code
across all languages in this repo (TypeScript, Kotlin, Swift, YAML).

The goal: comments age as badly as the code they describe, so we keep them
sparse, structural, and self-contained. A new contributor reading a file in
isolation should not need git history, retired design docs, or a Slack
archive to understand what the comment means.

---

## 1. Default: write no comment

Only add a comment when the **WHY** is non-obvious. Things that don't need
comments:

- What the code does. Well-named identifiers do that.
- Who the caller is. `git grep`, call graph, IDE references do that.
- When the code was added, by whom, or as part of which PR. `git blame` does
  that.
- That the code is "simple", "obvious", or "straightforward". If you had to
  say so, it isn't.

If removing a comment wouldn't confuse a future reader, delete it.

---

## 2. Comments MUST be self-contained

A reader opening the file today should understand the comment without
opening anything else. Never reference:

- **Non-production milestones**: "Batch 3", "Phase 2", "Stage 4", "M1",
  "Sprint 14", "v2 rewrite". These are project-plan concepts; they don't
  travel with the source.
- **Retired files by path**: `"src/adapters/agent-direct.adapter.ts"`,
  `"legacy/driver-bridge.ts"`. The comment will outlive the file and become
  a ghost.
- **Deprecated / renamed components by name**: "replaces the old
  `waitForFocus`", "port of the legacy tool onto the new framework",
  "pre-refactor behaviour". If the old thing is gone, say what the current
  thing IS, not what it replaced.
- **Brand / product names that aren't the current one**: never leave a
  sibling product's name in a user-facing error string or service
  description. If the repo is `Atomyx`, it's `Atomyx`.
- **Historic narrative framing**: "this used to …", "originally we …",
  "in the old path …". Git log owns that. Source files describe the
  present.

Allowed cross-file references:

- Sibling file in the same module, by relative path:
  `see ../orchestra/orchestra.ts`. These survive refactors and rename
  operations catch them.
- Named concepts from the canonical docs under `.claude/docs/`.

---

## 3. Describe WHY, not WHAT

Good comments answer questions the code can't:

- **Invariants** the code relies on. `// Caller must hold the registry
  lock — we mutate `entries` in place.`
- **Hidden constraints** from the platform, protocol, or hardware.
  `// iOS XCUITest typeText blocks until the keyboard is presentable; no
  explicit wait needed here.`
- **Non-obvious tradeoffs**. `// Smallest-bounds wins so a container
  ancestor doesn't shadow the actual input field.`
- **Workarounds for a specific platform bug**. Reference the platform
  version the bug applies to, and the symptom — not a ticket number that
  will rot.

Bad comments:

- `// increment counter` next to `counter++`.
- `// set timeout to 1000` next to `timeoutMs: 1000`.
- `// this is a simple function` anywhere.

---

## 4. Avoid magic numbers in code; name constants when the number has meaning

If a number appears in a comment because it has meaning (timeouts, retry
budgets, poll intervals, caps), promote it to a named `const` at the top of
the block or file. The name carries the intent so the comment doesn't have
to.

Bad:

```ts
await ctx.clock.sleep(300); // poll interval
```

Good:

```ts
const DISAPPEAR_POLL_INTERVAL_MS = 100;
// ...
await ctx.clock.sleep(DISAPPEAR_POLL_INTERVAL_MS);
```

Numbers that ARE just implementation details (array indices, buffer sizes
chosen for no particular reason) can stay inline.

---

## 5. Structure multi-platform / multi-framework notes as sections

Cross-platform code grows platform notes over time (Flutter today, React
Native tomorrow, Jetpack Compose the week after). Never narrate them as a
single paragraph — structure them so new entries can be appended cleanly
without rewriting.

Use an explicit `Framework notes` (or `Platform notes`) section, one bullet
per framework, parallel phrasing:

```kotlin
/**
 * Try to set text directly via ACTION_SET_TEXT on the focused node.
 *
 * Contract:
 *   - Returns a successful TypeResult when the action is accepted AND
 *     verification confirms the text landed.
 *   - Returns null on rejection OR verification miss.
 *
 * Framework notes (expand this list when adding support):
 *
 *   - Native Android EditText: ACTION_SET_TEXT is the canonical
 *     programmatic-set path. Accepts atomically.
 *
 *   - Flutter Semantics text fields: accept the action, but
 *     `obscureText: true` variants silently drop the write.
 */
```

Mention only frameworks we actually observe behaving differently. Don't
list frameworks "for completeness" — that turns into stale content the
first time a reader checks one.

---

## 6. One docstring per symbol

Never leave two doc blocks on the same function, class, or field. When you
revise a docstring, delete the old one. Doc tooling (JSDoc, KDoc, Dokka,
Swift symbol graphs) typically picks the first block and silently drops
later ones — the rendered API doc is then factually wrong.

If you find yourself wanting both blocks, merge them into one structured
docstring (see §5).

---

## 7. Docstrings describe the CONTRACT, not the implementation

A function's docstring should be readable without looking at the body.
State:

- What the function does at the abstraction level of its signature.
- Preconditions the caller must meet.
- Postconditions the function guarantees.
- Error / null behavior and what it signals.

Internal implementation notes (which algorithm, which strategy is tried
first, cache hit rates) belong in BODY comments next to the relevant
lines — not in the docstring. Docstrings live longer; bodies are rewritten
more often, so implementation notes next to the affected lines stay in
sync naturally.

---

## 8. Never narrate the diff

A comment must not describe the change that introduced it. Banned phrases:

- "removed the X check",
- "added verification after typing",
- "now uses poll-based approach",
- "after the observation-driven refactor",
- "the `X` parameter is no longer used".

The diff is in git. The current source describes the present behavior.

Exception: a comment pointing at a platform-version-specific quirk MAY
name the version, because the quirk outlives the version. `// iOS 15+
exposes this via ...` is fine.

---

## 9. No commented-out code

Dead code is a distraction. Either it's used (keep it, no comment needed),
or it isn't (delete it — git has it). The one exception: sample code
inside a docstring that illustrates usage.

If you must suppress a block temporarily while iterating, use your
language's feature-flag / conditional compilation mechanism (not a comment
wrapper) so the suppression is visible to tooling.

---

## 10. User-facing strings are not comments

Error messages, log lines, and tool descriptions are part of the product
surface. They follow all the rules above PLUS:

- Never name internal implementation details (class names, private
  methods, internal protocol constants) in user-facing strings.
- Never reference platforms the user didn't ask about ("On iOS, …") in
  cross-platform surfaces — abstract the behavior (see §5 if you
  genuinely need to list platform differences in the public surface).
- Use the current product / brand name. A stale product name in a user-
  visible string is always a bug.

---

## Quick self-check before committing

Before you ship a comment, ask:

1. Could I delete this without making the code harder to understand? →
   Delete.
2. Does it answer WHY, not WHAT? → Keep.
3. Does it still make sense with no external context (no git log, no
   project-plan doc, no chat history)? → Keep.
4. Does it describe a removed piece of the diff? → Rewrite to describe
   the present.
5. Does it reference anything that could be renamed, moved, or deleted
   without the comment noticing? → Restructure to not depend on that
   reference.

If any answer is "no", fix the comment before it lands.
