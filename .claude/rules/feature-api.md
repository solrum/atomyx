# Atomyx — Feature API rules (Dependency Inversion)

**Scope**: every TypeScript package and app in the monorepo
(`packages/`, `apps/studio/`, future apps). Native projects
follow their own conventions.

**Sister rule**: `feature-structure.md` tells you *where* files
go. This file tells you *how code inside a feature exposes
itself to consumers*. Both apply. If a feature violates this one
it is not done, no matter how clean its folder layout.

The goal: **changing an implementation touches one wire site.
Everything else, including every consumer, keeps compiling.**

Read this file before:

- Creating a new feature in any TS package.
- Exporting anything from a feature's `index.ts`.
- Writing an import that starts `@…/features/<name>/` and does
  not end in `index.js`.
- Reviewing a PR that touches more than three consumer files in
  response to a single feature-internal change.

If a rule conflicts with what a task requires, stop and surface
the conflict. We already paid the cost of a hundreds-of-sites
refactor; we will not pay it again for lack of discipline.

---

## 1. Motivation (read this first)

Before these rules landed, a Studio feature looked like:

```ts
// state/features/editor/editor.store.ts
export const editorStore = createStore<EditorState>((set, get) => ({
  tabs: [],
  openFile: async (path) => { … },
  …
}));
```

and every consumer imported the concrete store:

```ts
import { editorStore } from "@state/features/editor";
editorStore.getState().openFile(path);
editorStore.subscribe(listener);
```

Change `createStore` → another state manager, split the store,
add a cache layer — every consumer site had to be rewritten.
Path aliases and feature barrels do not fix this: the consumer
depends on the *shape* of the implementation, not a stable
contract.

The fix: features export **contracts** (interfaces). One place
wires a concrete implementation to the contract. Consumers hold
only the contract type and get the instance via a registry or
hook. Changing the implementation changes exactly one file; no
caller moves.

---

## 2. File convention

Every feature has three canonical files. Additional files are
allowed but MUST be internal (see §4).

```
features/<name>/
├── <name>.contract.ts   ← interface(s) + snapshot shapes
├── <name>.<impl>.ts     ← concrete implementation
│                         (<impl> names its backing tech, e.g.
│                          .zustand.ts, .tauri.ts, .fs.ts, .node.ts)
├── index.ts             ← public surface: export type +
│                         factory function(s). NEVER re-exports
│                         the impl class or concrete value.
└── <name>.test.ts       ← tests may reach impl directly.
```

### 2.1 `<name>.contract.ts`

Pure types. No runtime, no import of anything but other types.

```ts
// editor.contract.ts
export interface EditorTab { … }
export interface EditorSnapshot {
  readonly tabs: readonly EditorTab[];
  readonly activePath: string | null;
}
export interface EditorApi {
  getSnapshot(): EditorSnapshot;
  subscribe(listener: () => void): () => void;
  openFile(path: string): Promise<void>;
  closeFile(path: string): void;
  …
}
```

Rules:

- Contract must be **narrow**: only the methods + snapshot
  consumers actually use. No "just in case" surface.
- Mix of imperative methods (actions) and observable state
  (`getSnapshot` + `subscribe`) is allowed and expected.
- Zero imports from any `*.impl.ts`, `*.zustand.ts`, etc. If a
  contract needs a shape, it defines or re-exports the type
  from another contract.

### 2.2 `<name>.<impl>.ts`

Concrete implementation. Names:

- `.zustand.ts` — zustand-backed state
- `.tauri.ts` — Tauri invoke-backed
- `.fs.ts` — Node filesystem-backed
- `.node.ts` — Node built-ins (spawn, http)
- `.mock.ts` — test double
- `.impl.ts` — generic fallback

Implements the contract. Exports a **factory function**, not the
class:

```ts
// editor.zustand.ts
import { createStore } from "zustand/vanilla";
import type { EditorApi, EditorSnapshot } from "./editor.contract.js";

export function createZustandEditor(): EditorApi {
  const store = createStore<InternalState>(() => ({ tabs: [], … }));
  return {
    getSnapshot: () => toSnapshot(store.getState()),
    subscribe: (l) => store.subscribe(l),
    openFile: async (p) => store.setState(…),
    …
  };
}
```

Rules:

- The impl module **exports factory(s) only**. No concrete
  singleton. No `export const editor = …` at module top level.
- Dependencies injected via factory parameters. Nothing reads
  `getServices()` or a global registry from module scope; that
  would be a hidden dependency.

### 2.3 `index.ts`

The feature's public API. What consumers are allowed to import.

```ts
// features/editor/index.ts
export type {
  EditorApi,
  EditorSnapshot,
  EditorTab,
} from "./editor.contract.js";
export { createZustandEditor } from "./editor.zustand.js";
```

Rules:

- Exports **types only** (contracts) and **factories** (functions
  that return instances). Never concrete instances, never
  concrete classes as named exports.
- If an alternative impl exists (mock, remote, local) export
  another named factory: `createMockEditor`, `createRemoteEditor`.
- Nothing else — no helpers, no constants, no utility
  re-exports. Those are internal.

---

## 3. Composition root

Exactly one place per app wires instances. For Studio that is
`apps/studio/src/main.tsx`. The root:

1. Constructs each feature instance via its factory.
2. Registers each instance in the `FeatureRegistry` keyed by
   contract type.
3. Triggers any startup effects.

```ts
// main.tsx excerpt
import { registerFeature } from "./state/core/registry.js";
import { createZustandEditor } from "./state/features/editor/index.js";
import { createZustandLayout } from "./state/features/layout/index.js";
…

registerFeature("editor", createZustandEditor());
registerFeature("layout", createZustandLayout());
…
```

Rules:

- Only `main.tsx` (or a small `wire.ts` it imports) constructs
  feature instances.
- Any other `createXxx()` call outside the composition root is
  a violation, unless the call site is a test.
- The composition root must not contain business logic; it only
  wires.

---

## 4. Consumer rules

A consumer is any file that needs a feature's capability.

### 4.1 Imports

Consumers import **types** from the feature's `index.ts`:

```ts
import type { EditorApi } from "@studio/state/features/editor";
```

They obtain the concrete instance via the registry OR a React
hook:

```ts
import { getFeature } from "@studio/state/core/registry";
const editor = getFeature<EditorApi>("editor");
await editor.openFile(path);
```

```ts
// inside a React component
const editor = useEditor();          // tiny wrapper that picks
await editor.openFile(path);          // useSyncExternalStore
```

Rules:

- **Never** import any `*.zustand.ts`, `*.impl.ts`, `*.fs.ts`,
  `*.tauri.ts`, or `*.node.ts` from outside its own feature
  folder. Violations are dep-cruiser errors.
- Never import a factory outside the composition root or tests.
- Never assume the underlying state library (no
  `editor.setState(…)`, no `store.getState()` — those are
  impl-specific).

### 4.2 React hooks

Every observable feature ships a small React helper in the
feature's public surface:

```ts
// features/editor/index.ts
export function useEditor(): EditorSnapshot & Pick<EditorApi, "openFile" | "closeFile"> {
  const editor = useFeature<EditorApi>("editor");
  const snapshot = useSyncExternalStore(
    editor.subscribe,
    editor.getSnapshot,
    editor.getSnapshot,
  );
  return {
    ...snapshot,
    openFile: editor.openFile,
    closeFile: editor.closeFile,
  };
}
```

The hook is the idiomatic path for React consumers; the
registry accessor is for non-React code (keymap handlers,
startup initializers, tests).

---

## 5. Tests

Tests are the ONLY place allowed to reach inside a feature:

- Test files (`*.test.ts`, `*.test.tsx`) may import `*.impl.ts`,
  `*.zustand.ts` directly when they need to inspect internals.
- Tests construct via factory just like production: prefer
  `createZustandEditor()` over reaching into the module's inner
  state.
- For unit testing consumers, inject a mock via the factory or
  the registry: `registerFeature("editor", createMockEditor())`.

---

## 6. Enforcement

Three gates, all blocking:

### 6.1 `index.ts` discipline

The repo-wide CI runs a linter that checks:

- No `index.ts` in `features/<name>/` exports a concrete class
  whose type does not appear in `<name>.contract.ts`. If it does
  the lint fails.
- No file outside `features/<name>/` imports any file in
  `features/<name>/` whose name matches the impl patterns.

### 6.2 dep-cruiser rules

Per-package `.dependency-cruiser.cjs` adds:

```js
{
  name: "feature-impl-is-private",
  severity: "error",
  from: { path: "^src/", pathNot: "\\.test\\.(ts|tsx)$|^src/main\\.tsx$" },
  to: { path: "^src/(...)/features/([^/]+)/.+\\.(zustand|tauri|fs|node|impl)\\.(ts|js)$" },
}
```

Only `main.tsx` and test files can see impl files.

### 6.3 ESLint `no-restricted-imports`

Editor-time feedback with the same patterns:

```js
"no-restricted-imports": ["error", {
  patterns: [
    "**/features/*/*.zustand.*",
    "**/features/*/*.tauri.*",
    "**/features/*/*.fs.*",
    "**/features/*/*.impl.*",
  ],
}]
```

Exceptions are `main.tsx` (composition root) and any `*.test.ts`.

---

## 7. Adding a new feature — checklist

- [ ] Folder `features/<name>/` exists (per `feature-structure.md`).
- [ ] `<name>.contract.ts` defines the interface, re-exports no
      runtime values, depends only on other contracts.
- [ ] Exactly one `<name>.<impl>.ts` file implements the contract
      and exports a factory function.
- [ ] `index.ts` exports `type` imports + factories; nothing
      else. No concrete class, no singleton.
- [ ] If the feature has observable state, `index.ts` exports a
      React hook using `useSyncExternalStore` over the instance's
      `subscribe` / `getSnapshot`.
- [ ] `main.tsx` constructs an instance via the factory and
      registers it.
- [ ] All consumers import only the type from `index.ts` and the
      instance via the registry / hook.
- [ ] dep-cruiser + ESLint pass.
- [ ] `grep` for the impl filename finds hits only in: the
      impl file itself, its tests, `index.ts`, and `main.tsx`.

If any line is red, the feature is not merged.

---

## 8. Changing an implementation — what you MUST touch

Given a request "switch the editor from zustand to Redux":

1. Write `editor.redux.ts` implementing `EditorApi`.
2. Export its factory from `index.ts` (keep both temporarily or
   replace outright).
3. Change `main.tsx` to call the new factory.
4. Delete `editor.zustand.ts`.

If any other file changed as a result, the contract was not
narrow enough — fix the contract (separate ticket) instead of
spreading impl knowledge back out.

Stop; reassess; do not paper over leakage.

---

## 9. Why this isn't cosmetic

The previous rule (`feature-structure.md`) stopped folder
scatter but did not stop implementation scatter. The pain from
the last refactor was **consumers knew zustand APIs**. That was
a design error, not a naming error.

This file fixes the design. Path aliases, barrel exports, dep
rules — all helpful, none sufficient without this.
