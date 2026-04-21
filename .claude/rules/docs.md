# Atomyx — Documentation rules

Rules for every docs file in the repo (both `.claude/docs/` and `docs/`).

Single goal: a contributor or AI agent who knows nothing about the repo
must be able to read docs + code and contribute safely. Every rule
below serves that goal.

Atomyx docs have been audited and found to drift heavily — historical
narrative replacing present state, deleted paths still referenced,
changelog text interleaved with reference, stream-of-consciousness
decision logs. This file codifies the principles so future sessions do
not repeat the drift. See `.claude/rules/comments.md` for the sibling
rules that apply to inline comments and docstrings — docs are comments
at file scale, built on the same foundations.

---

## 1. All docs files are written in English

Every markdown file in the repo — `.claude/rules/*.md`,
`.claude/docs/**/*.md`, `docs/**/*.md`, `README.md`, `CLAUDE.md`,
package-level and platform-level `README.md` — is written in English.

Reason: Atomyx is an open-source framework with non-Vietnamese
contributors and downstream agents. English docs keep the surface
uniform and machine-readable. Chat-level conversation with the user
stays in whatever language the user prefers; persisted artifacts
(source, docs, commit messages) stay English.

This rule applies even to files that live under `.claude/` and are
nominally "internal" — they are still shipped via git, still read by
future Claude sessions, still read by any contributor who clones the
repo.

---

## 2. Docs describe the present. Git keeps the past.

A doc is a snapshot of the system at read time. Do not narrate
history: no "used to", "was retired", "after the X refactor", "legacy
path removed". Anyone who needs to know what changed reads `git log`.

**Banned phrases (do not appear in any docs file):**

- "Phase 1/2/3…", "Batch N", "Week N", "Sprint N", "M1"
- "✅ PASS", "SHIPPED (date)", "Exit criteria"
- "legacy X was retired", "pre-refactor Y", "old path"
- "observation-driven refactor", "strangler fig transition"
- "Finding #N", "Finding #N revision"
- A `Decision log` section with many entries on the same date

This rule is §8 of `comments.md` scaled up to file level.

---

## 3. One file — one question

Each doc answers exactly one question. If the same information
appears in two files, one is wrong — duplication guarantees that one
copy drifts first. Pick the authoritative file; the other only links
to it via relative path.

Current mapping (each file should declare its question in its
`Purpose` header):

| File | Question it answers |
|---|---|
| `architecture.md` | What contract is Atomyx built on? Why? |
| `repo-map.md` | Where do I find this? |
| `tools.md` | What does tool X do, what does it return, which Orchestra method does it call? |
| `development.md` | How do I build/test/extend? |
| `pitfalls.md` | What traps does subsystem Y have? |
| `ios.md` / `android.md` | How does platform Z work, how do I set it up, what quirks? |
| `status.md` | What version/branch is the repo on, how many tests? |

Creating a new doc: declare the question it answers, or do not
create it.

---

## 4. Diátaxis — classify by reader intent

Each doc belongs to exactly ONE of these types. Do not mix:

| Type | For | Atomyx mapping |
|---|---|---|
| **Explanation** (WHY) | Understand the design | `architecture.md`, "Platform quirks" in platform docs |
| **Reference** (WHAT) | Look up exact facts | `tools.md`, `repo-map.md`, command tables |
| **How-to** (HOW) | Finish a specific task | `development.md` checklists, setup sections |
| **Tutorial** (LEARN) | Go from zero to running | `docs/` (end-user) |

Type mixing is the root cause of doc bloat. If a file legitimately
serves two types, either split it, or use explicit section dividers
and treat the sections as two files packaged together.

---

## 5. Audience-first header

Each doc opens with 2–3 sentences stating:

- **Who reads this**: a contributor editing Swift? an AI agent picking
  a tool? a new onboarder?
- **When they read it**: before changing what? before running what?
- **What this doc does NOT cover**: with a pointer to the right file.

The header is a filter so readers can decide whether to keep reading
or jump to a better file. `.claude/docs/README.md` is the current
reference pattern — copy the way it splits audiences.

---

## 6. Every path / command / API must be verifiable

Every file path, shell command, and API symbol mentioned in docs
must exist in the repo at commit time.

- Before merging a PR that touches docs: grep every path mentioned.
  If it does not exist, fix or delete.
- When a refactor renames paths: update docs in the same PR. Do not
  defer to a "cleanup batch".
- A ghost reference (a written path that was deleted) is the worst
  docs bug because it creates false confidence — strictly worse than
  having no docs.

A CI grep step that validates paths in docs is a legitimate future
improvement.

---

## 7. Contract, not implementation

Docs describe the **contract**: port interface, tool I/O, wire
protocol shape, invariants callers can rely on. Docs do NOT describe
how it is implemented: internal classes, current strategy, cache
tuning. Implementation changes often; contracts are stable.

Banned (belongs in a commit message, not docs):

- "X rewritten to use Y instead of Z"
- "performance: 10s → 175ms after switching to W"
- "class A delegates to class B which composes C"

Allowed (affects the caller, is part of the contract):

- "Tool `tap` returns `ActionResult`; iOS may return `ok:false` when
  no verifiable affordance was used."
- "`get_ui_tree` is cached for 2s; mutating tools invalidate the
  cache via `ctx.invalidateUiCache()`."

---

## 8. Depth gradient

Every file is shaped as:

```
1. Purpose (one short paragraph)    ← audience + when to read
2. TL;DR or summary                  ← 80% of readers stop here
3. Detailed sections                 ← 20% who need depth
4. References / cross-links          ← pointers to other docs
```

A reader who stops at any tier must still get value. No reader
should have to finish 900 lines to learn what a platform does.

---

## 9. Update-in-place, never append

When the system changes: **rewrite the existing section to reflect
the new state**. Do NOT append `### Update YYYY-MM-DD` or `### Revision
to Section X` at the bottom. Appending is the mechanism by which docs
grow without getting more informative.

If a decision genuinely deserves historical preservation → write an
ADR (see rule 10). Do not stuff it into a reference file.

---

## 10. Proposals and decisions live apart from reference

Three kinds of content, three directories. Proposals and ADRs
are gitignored (kept per-contributor, not pushed — see
`.gitignore` §"Local-only design docs"); reference docs ride
with the repo.

```
.claude/docs/               Reference — describes present state
.claude/docs/proposals/     Design sketches — describes unshipped plans
.claude/docs/decisions/     ADRs — accepted decisions (1 file per decision)
```

**When a proposal ships:**

- Rewrite it as a reference section in the appropriate file.
- Delete the proposal file.
- If the decision is worth preserving → create an ADR.

**When a proposal is cancelled:**

- Delete the proposal file. Do not keep it as "historical".

**ADR format** (`.claude/docs/decisions/NNN-short-slug.md`):

```markdown
# ADR-NNN: Short title

**Date**: YYYY-MM-DD
**Status**: Accepted | Superseded by ADR-MMM

## Context
(The problem, the constraints.)

## Decision
(What was chosen, 1–3 paragraphs.)

## Consequences
(Positive effects + negative effects + accepted trade-offs.)
```

Do NOT cram 40 decisions into a single table on the same date.

---

## 11. Respect `.claude/rules/comments.md`

Docs are comments at file scale. All 10 rules in `comments.md` apply
to docs files. In particular:

- §2 (self-contained, no milestone references)
- §7 (contract > implementation)
- §8 (never narrate the diff)

If an inline comment may not say "Phase 3 refactor", a docs file
also may not.

---

## Standard structure for platform deep-dives

Applies to `ios.md`, `android.md`, and any future platform. Target:
150–250 lines.

```markdown
# <Platform> driver internals

## Purpose
<Who reads this, when to read, what this does NOT cover.>

## Architecture
- Components: host TS adapter, platform runner, transport.
- Data flow: tool → Orchestra → Driver → runner.
- File pointers (CURRENT, grep-verified).

## Command surface
| Driver port method | Wire command | Runner handler | Notes |
(The contract between the TS adapter and the runner.)

## Setup & run
- Dev default (simulator/emulator).
- Physical device.
- Prerequisites (SDK, trust, signing, ...).

## Platform-specific quirks
(Present tense: "iOS has no system-wide foreground query, so the
driver tracks currentApp internally." NOT: "finding #1 discovered in
week 1.")

## Limitations
(Current state. No roadmap promises.)

## Extension points
- Adding a new command / route.
- Adding a new bridge strategy.

## References
- Platform-native README (`platforms/<name>/README.md`).
- Corresponding section in `pitfalls.md`.
```

---

## Maintenance workflow

1. **PR that changes a contract / API / path → update docs in the same
   PR.** Do not defer to a "cleanup batch" — that is exactly how debt
   accumulates.
2. **PR that deletes a file → grep docs, fix immediately.** No ghost
   references.
3. **Finishing a feature or phase → rewrite the relevant sections to
   reflect the new state.** Strip out roadmap / exit criteria / status
   markers. Decisions worth preserving → create an ADR.
4. **Periodic docs review** (every release): open each file and ask
   "if someone new read this today, would they understand?" Anything
   that does not serve that answer — cut it.

---

## Pre-commit self-check

1. Is the file written in English? → if not, translate before commit.
2. Does every path in the file grep out to something real? → if not,
   fix or delete.
3. Any banned word (Phase/Batch/Week/legacy/retire/SHIPPED/etc.)? →
   remove.
4. Implementation details instead of contract? → move to the commit
   message.
5. Appending a new section under an old file? → rewrite the old
   section instead.
6. Could a reader understand the file without git log, chat history,
   or another ADR? → if not, make it self-contained.

If any answer is "no", fix it before committing.
