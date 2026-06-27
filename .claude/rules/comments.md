# Atomyx — Code comment rules (repo overrides)

Every rule in `~/.claude/rules/comments.md` applies. Read it first.
This file adds Atomyx-specific constraints and examples on top.

---

## Repo languages

Applies across all production languages in this repo: TypeScript,
Kotlin, Swift, YAML.

---

## Banned references (extends global §2)

On top of the global self-contained rule, these Atomyx-specific
things also must not appear in a comment:

- **Rule / process docs by path or section**: `per
  .claude/rules/studio-architecture.md §7`, `see ADR-005`, `per
  .claude/docs/proposals/*.md`. State the rule itself inline.
- **Retired paths**: `src/adapters/agent-direct.adapter.ts`,
  `legacy/driver-bridge.ts`, any path under a `legacy/` or
  `deprecated/` directory name.
- **Retired symbol names**: `waitForFocus` (pre observation-driven
  primitives), "port of the legacy tool onto the new framework",
  "pre-refactor behaviour". Describe the current symbol, not what
  it replaced.
- **Sibling-product brand names**: user-facing strings must say
  `Atomyx` — no earlier codename, no parent-org brand.
- **Internal codenames for past refactors**: "observation-driven
  refactor", "sidecar split", "feature-api migration". These are
  project-plan concepts; the current code describes what it is,
  not what it moved from.

---

## Platform notes — which platforms to list

When a `Platform notes` block (global §5) is warranted in Atomyx
code, the relevant platforms are:

- **Native Android** (Kotlin via accessibility service)
- **Native iOS** (Swift via XCUITest)
- **Flutter Semantics** (cross-platform, commonly on both)
- **React Native / Jetpack Compose** (add when first observed)

Mention a platform only when its behavior forces caller-visible
differences. iOS-only selector fields (`predicate`, `classChain`)
silently ignored on Android is the canonical example — document it
on the selector type, not on every caller.

Version-specific iOS quirks MAY name the version (e.g. `// iOS 15+
exposes this via …`) because the quirk outlives the version — this
is the version-exception at global §8.

---

## Agent product terminology

User-facing strings produced by tools (error messages, log lines,
tool descriptions) never mention platform internals the agent does
not need to reason about:

- Never "On iOS, …" or "On Android, …" in a cross-platform tool
  surface. If the behavior genuinely differs, document it under
  the driver adapter, not in the user-visible message.
- Never name internal classes (`Orchestra`, `Finder`,
  `ScrollController`, `MockDriver`, adapter class names) in a
  message that crosses the tool boundary.
- Never name the bridge protocol (`JSON-RPC over stdio`, `HTTP on
  :8765`) — consumers of the tool surface don't care.
