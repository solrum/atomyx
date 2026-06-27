# Atomyx Studio — architecture rules

**Scope**: this file applies to source files under `apps/studio/`
ONLY. The rest of the Atomyx workspace (`packages/`, `shared/`,
`platforms/`) is governed by `CLAUDE.md` + the per-package
conventions and is unaffected by anything below.

The goal: Studio's UI layer must be replaceable without touching
the rest of the system. Design kit swaps, component rewrites, even
a framework migration (React → Vue / Svelte / Solid) must stay
contained in `ui/`. These rules exist because the alternative —
free-for-all imports between UI components and MCP wiring — is the
mechanism by which "re-skin the editor" becomes "re-engineer the
whole app".

Read this file before:

- Creating any new file under `apps/studio/src/`.
- Adding a dependency to `apps/studio/package.json`.
- Reviewing a PR that touches Studio code.

If a rule below conflicts with what the task requires, stop and
surface the conflict — do not quietly bend the rule. Ergonomic
shortcuts compound into architectural debt fast at this scale.

---

## 1. Four layers, one-way dependencies

`apps/studio/src/` contains exactly these four top-level folders:

```
apps/studio/src/
├── ui/         React components, pages, views, styling
├── state/      Zustand stores (UI state only)
├── domain/     Pure TypeScript services, ports, contracts
└── platform/   Tauri adapters, OS integration
```

Within each layer, code is feature-sliced per
`.claude/rules/feature-structure.md` (domain/features/<name>,
state/features/<name>, platform/features/<name>, ui/features/<name>
+ ui/primitives + ui/shell). The layer boundary stays — a UI
feature may not import a domain feature's internals, only its
`index.ts`.

Not every feature populates all four layers. See §12 for when a layer
may be omitted (pure UI state has no domain port; service ports have
no state slice).

Import direction is strictly one-way, top to bottom:

```
ui   ──▶  state   ──▶  domain   ◀──  platform
```

- `ui/` MAY import from `state/`, `domain/`, `platform/`.
- `state/` MAY import from `domain/`. MAY NOT import from `ui/` or
  `platform/`.
- `domain/` MAY import from `@atomyx/shared`, `@atomyx/script`, and
  other domain files. MAY NOT import from `ui/`, `state/`, or
  `platform/`.
- `platform/` MAY import from `domain/` (to implement its ports).
  MAY NOT import from `ui/` or `state/`.

Circular imports are forbidden at every boundary — not just across
layers.

---

## 2. Which layer owns which code

A decision tree for "where does this new file go":

| If the code … | It belongs in |
|---|---|
| Renders JSX, uses React hooks, imports from a component library | `ui/` |
| Is a Zustand `create()` store, defines UI-state transitions | `state/` |
| Is a TypeScript interface describing a capability | `domain/` (as a port) |
| Uses `@atomyx/shared` schemas to validate data | `domain/` |
| Is pure business logic (no I/O, no DOM, no Tauri) | `domain/` |
| Calls `invoke()` from `@tauri-apps/api` | `platform/` |
| Spawns processes, reads files, talks to the OS | `platform/` |
| Imports `monaco-editor` or other UI-framework code | `ui/` |
| Is a unit test of domain logic | next to the domain file, `*.test.ts` |
| Is a React-component test using `jsdom` | next to the component, `*.test.tsx` |

If a file would legitimately sit in two layers, it is doing two
things — split it.

---

## 3. Rule specifics per layer

### 3.1 `ui/`

- React components, pages, layouts, styling.
- May use Tailwind, shadcn, Monaco, or any UI library.
- Never calls `invoke()` directly — goes through `state/` or a
  domain service accessed via `state/`.
- Never reads or writes files directly — delegates to a domain
  port via `state/`.
- Component files end in `.tsx`; non-JSX helpers under `ui/` end
  in `.ts`.
- One exported component per file, named the same as the file
  (`editor-pane.tsx` exports `EditorPane`).

### 3.2 `state/`

- Zustand stores only. One store per concern (workspace, runs,
  settings), not one monster store.
- Holds references to domain services injected at startup (see §4).
- NEVER imports from `@tauri-apps/*`, `react-dom`, `monaco-editor`,
  or any DOM API.
- React is banned from every `state/` file EXCEPT the feature's
  own `index.ts`, which MAY import `react` to ship the public
  `useXxx()` hook (per `feature-api.md`). The hook must be a thin
  wrapper over `useSyncExternalStore` + the feature's
  `getSnapshot` / `subscribe` — no JSX, no DOM access. All other
  state files (`*.contract.ts`, `*.zustand.ts`, `*.impl.ts`,
  tests) remain pure and runnable under `node:test`.
- Testable under `node:test` with no DOM.

### 3.3 `domain/`

- Pure TypeScript. Defines ports (interfaces) for every capability
  the app needs from the outside world (MCP, filesystem, clock,
  logger).
- Concrete implementations live in `platform/`, not here.
- MAY depend on `@atomyx/shared`, `@atomyx/script`, and npm
  packages that are themselves pure (`zod`, `uuid`, `date-fns`).
- MUST NOT depend on `react`, `react-dom`, `@tauri-apps/*`,
  `monaco-editor`, or any browser/Node-platform API that is not
  trivially mockable.
- Every port has a `Mock*` adapter colocated in the same folder,
  usable by UI tests and state tests.

### 3.4 `platform/`

- Implements domain ports against Tauri + OS APIs.
- `EmbeddedMcpClient`, `FsArtifactStore`, `TauriProcessRunner`,
  etc. live here.
- May import `@tauri-apps/api`, Node built-ins (via Tauri's
  allowlisted bindings), and domain ports (to implement them).
- Must not import from `ui/` or `state/`.
- Each adapter is one file named `<port>.<backend>.ts` — e.g.
  `mcp-client.embedded.ts`, `artifact-store.fs.ts`.

---

## 4. Wiring: where things come together

Exactly one place composes the app: `src/main.tsx`.

- Instantiates platform adapters.
- Passes them to the Zustand stores' initializers.
- Renders the root `App` component.

No other file imports from all four layers. If a second wiring
site appears, refactor — centralize in `main.tsx`.

This is the hexagonal-architecture assembly point, equivalent to
the composition root used in `@atomyx/driver`'s `Orchestra`
constructor wiring.

---

## 5. Free changes — no review gate

These changes do not require architectural review. They stay in
`ui/` and don't touch any other layer:

- Rearranging panels, tabs, menus.
- Swapping Tailwind for another styling approach.
- Replacing shadcn components with custom ones.
- Renaming / splitting / merging any React component file.
- Adding animations, themes, keyboard shortcuts, command palette.
- Replacing the Monaco integration with another editor — provided
  the editor is still fed a string and emits a string.
- Rewriting the frontend in Vue / Svelte / Solid — again, `domain/`
  and `platform/` are untouched.

---

## 6. Breaking changes — ADR gate

These changes are breaking regardless of intent. They require an
ADR under `.claude/docs/decisions/` before landing:

- Changing the method signature or removing a method from any
  domain port (e.g. `StudioRuntime`, `ArtifactStore`).
- Changing the on-disk format of the artifact store (see §7).
- Changing Tauri IPC command names or payload shapes (relevant
  once auto-update ships and a frontend bundle may run against
  an older backend).
- Bumping the minimum Node, Rust, or macOS version.
- Introducing a new layer or reshaping the existing four.
- Changing the import-direction rules in §1.

---

## 7. Public contracts kept stable

Three surfaces carry semver-like discipline even internally. A
change that removes a field, renames a method, or tightens a
validator is breaking:

1. **Domain ports** in `apps/studio/src/domain/` — most notably
   `StudioRuntime` and `ArtifactStore`. These are candidates for
   a future public API (VS Code extension, third-party tooling).
2. **Artifact store folder format** — `runs/<id>/meta.json`,
   `steps.jsonl` event shape, artifact file naming. External
   consumers zip and parse run folders.
3. **Zod schemas exported from `@atomyx/shared/script`** — these
   are not Studio-owned but Studio depends on them; changes affect
   every script the user has written.

---

## 8. Enforcement

Four layers of defence. Each catches a different class of mistake.

### 8.1 TypeScript project references (compile-time)

`apps/studio/tsconfig.json` splits the four layers into separate
`references` so the compiler rejects illegal imports before tests
even run. Configured via subfolder `tsconfig.*.json` files, one
per layer.

### 8.2 `dependency-cruiser` rule (CI)

A Studio-scoped rule set — `apps/studio/.dependency-cruiser.cjs` —
forbids:

- any import from `ui/` into `domain/`, `state/`, or `platform/`.
- any import from `state/` into `ui/` or `platform/`.
- any import from `domain/` into `ui/`, `state/`, or `platform/`.
- any import from `platform/` into `ui/` or `state/`.
- circular imports at any boundary.

This rule set is independent of the root `.dependency-cruiser.cjs`
used for `packages/`. Studio's rules do not apply to the rest of
the repo and vice versa.

### 8.3 ESLint `no-restricted-imports` (editor-time)

`apps/studio/.eslintrc.cjs` contains `no-restricted-imports`
patterns that:

- forbid `react` / `react-dom` / `@tauri-apps/*` under `domain/`.
- forbid `react` / `react-dom` under `state/`.
- forbid `@tauri-apps/*` under `state/`.
- forbid `monaco-editor` / DOM-only libraries under `state/` and
  `domain/`.

Contributors see violations in their editor before they commit.

### 8.4 `node:test` domain suite (no DOM allowed)

`apps/studio/domain/` has a test suite runnable via
`node:test` with no bundler, no `jsdom`, no React. The suite
importing anything from `ui/` or using `document` blows up at
load time — which is the point. If a domain test needs DOM, the
code under test belongs in `ui/`.

---

## 9. Dependency policy for `apps/studio/package.json`

- `dependencies`: only runtime libraries. Split by layer mentally —
  domain deps (zod, date-fns), platform deps (`@tauri-apps/api`,
  `@tauri-apps/plugin-updater`), UI deps (`react`, `react-dom`,
  `monaco-editor`, `monaco-yaml`, `zustand`, `tailwindcss` runtime
  where applicable).
- `devDependencies`: build / test tooling only.
- Adding a dep that pulls in Node/DOM for domain code is almost
  always a smell — wrap it in a platform adapter instead.

---

## 10. Pre-commit self-check

Before you commit code under `apps/studio/`:

1. Did I add a new file? Is it in the correct layer per §2?
2. Does the file import from any layer the rules forbid?
3. If I changed a domain port signature — did I write an ADR?
4. If I changed the artifact-store on-disk format — did I write
   an ADR?
5. Is every domain port still testable under `node:test` with no
   DOM?
6. Did I introduce a second wiring site outside `main.tsx`?

If any answer is "no" where it should be "yes", fix before
committing.

---

## 11. Why these rules don't leak elsewhere

This file lives in `.claude/rules/` alongside project-wide rules
(`comments.md`, `docs.md`) but its scope line makes clear it
applies only to `apps/studio/`. The enforcement mechanisms are
also scoped:

- TypeScript references live under `apps/studio/`.
- `dependency-cruiser` config lives under `apps/studio/`.
- ESLint config lives under `apps/studio/`.

Nothing here modifies the root build, the root test runner, the
root `dependency-cruiser` config, or any other package's tooling.
Contributors working on `@atomyx/driver` or `@atomyx/mcp` will
never hit these rules.

---

## 12. When a layer may be omitted

A feature is not required to populate all four layers. Skipping a
layer is acceptable when adding that layer would only add indirection
without expressing a new contract. When in doubt, prefer to skip —
adding a port later is cheap, removing a vestigial wrapper is not.

### Skipping `domain/`

Omit the `domain/` layer when the feature carries no contract that a
second platform adapter would ever need to implement. Examples in the
current tree:

- Pure UI state with no I/O: `layout` (pane sizes, visibility
  toggles), `nav-history` (in-memory navigation stack), `popups`
  (visibility set), `bookmarks` (in-memory list with persistence
  callbacks injected at composition time), `mirror-window`
  (positional / dock state), `apps` (passthrough projection of the
  runtime port), `problems` (projection of Monaco markers),
  `runtime-status` (connectivity bookkeeping over the existing
  runtime port).
- Static pure-function library: `script-actions` (YAML builder
  catalogue), `scripts` (Monaco schema + command-name list derived
  from the shared zod schema).

### Skipping `state/`

Omit the `state/` layer when the feature is a service port rather
than a reactive slice. Service ports define an interface in
`domain/` and a concrete implementation in `platform/`; their
observable projections live in purpose-built slices of other
features that consume the port. Wrapping a service port in its own
`state/features/<name>` only adds indirection.

Examples: `runtime` (service port, projected via `apps`, `devices`,
`runtime-status`, `runs`), `artifacts` (storage service, projected
via `runs`).

### Skipping `platform/`

Omit the `platform/` layer when the feature performs no I/O at all,
or when all I/O it requires is fully mediated by an existing domain
port that another feature already implements.

Examples: every pure-UI-state feature listed under "Skipping
`domain/`" — none of them touch the platform. `apps` delegates
entirely to the `runtime` port, so no `platform/features/apps` is
needed.

### Invariant that must never be broken

A file under `state/`, `domain/`, or `ui/` must never call
`invoke()` or any Tauri / OS API directly. If a state file needs
I/O, a domain port and a concrete platform adapter for that I/O
must exist; the state file receives the port through constructor
injection at the composition root. Direct platform calls in
non-`platform/` files defeat the layer boundaries; the
dep-cruiser rule plus the ESLint `no-restricted-imports` patterns
catch the most common shapes.

If the skip decision later turns out wrong (the feature grows a
second platform adapter, or the state slice starts performing I/O
that doesn't belong in `state/`), promote the missing layer in a
single ADR-gated change. Do not let the in-memory feature reach
into `@tauri-apps/*` directly as a stopgap.
