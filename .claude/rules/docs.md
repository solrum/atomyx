# Atomyx — Documentation rules (repo overrides)

Every rule in `~/.claude/rules/docs.md` applies. Read it first.
This file adds Atomyx-specific policies, templates, and data on
top.

---

## 1. All docs files are written in English

Every markdown file in the repo — `.claude/rules/*.md`,
`.claude/docs/**/*.md`, `docs/**/*.md`, `README.md`, `CLAUDE.md`,
package-level and platform-level `README.md` — is written in
English.

Reason: Atomyx is an open-source framework with non-Vietnamese
contributors and downstream agents. English docs keep the surface
uniform and machine-readable. Chat-level conversation with the
user stays in whatever language the user prefers; persisted
artifacts (source, docs, commit messages) stay English.

This rule applies even to files under `.claude/` that are
nominally "internal" — they still ship via git, are read by future
Claude sessions and by any contributor who clones the repo.

---

## 2. File → question mapping

Each doc in `.claude/docs/` answers exactly one question (global
§2). The current mapping:

| File | Question it answers |
|---|---|
| `architecture.md` | What contract is Atomyx built on? Why? |
| `repo-map.md` | Where do I find this? |
| `tools.md` | What does tool X do, what does it return, which Orchestra method does it call? |
| `development.md` | How do I build/test/extend? |
| `pitfalls.md` | What traps does subsystem Y have? |
| `ios.md` / `android.md` | How does platform Z work, how do I set it up, what quirks? |
| `status.md` | What version/branch is the repo on, how many tests? |

Creating a new doc: declare the question it answers in its
`Purpose` header, or do not create it.

---

## 3. Decisions folder

Atomyx follows global §9 with this specific convention:

```
.claude/docs/               Reference — ships with the repo
.claude/docs/proposals/     Design sketches — gitignored, per-contributor
.claude/docs/decisions/     ADRs — gitignored, per-contributor
```

Proposals and ADRs live under `.claude/docs/` but are listed in
`.gitignore` under `# Local-only design docs`. They never ride
with the repo — if a decision's rationale matters to future
contributors, fold it into a reference doc and delete the ADR.

---

## 4. Standard structure for platform deep-dives

Applies to `ios.md`, `android.md`, and any future platform.
Target: 150–250 lines.

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
driver tracks currentApp internally." NOT: "finding #1 discovered
in week 1.")

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

## 5. Atomyx-specific banned phrases

In addition to the global banned list, reference docs in this repo
must not use any of these project-internal terms to describe past
state:

- "observation-driven refactor" / "observation-driven wait
  primitives" (use the current API name inline)
- "sidecar split" / "sidecar extraction"
- "feature-api migration" / "DI migration" / "state feature batch"
- Internal codename for any refactor batch number

If a rationale genuinely belongs in docs, state it in present-
tense contract terms.
