# Atomyx — Feature structure rules

**Scope**: this file applies to every TypeScript package and app in
the Atomyx monorepo (`packages/`, `apps/studio/`, future
`apps/cli-ui/`). Native projects (`platforms/android-agent/`,
`platforms/ios-agent/`) follow their own platform conventions.

The goal: adding a feature is *one folder and one registrar line*.
Removing a feature is *one folder delete and one line delete*. No
other file should ever need to change when the feature surface
grows. Any layout that violates this is a design flaw, not a style
preference — file it and fix it in the next refactor window.

Read this file before:

- Creating any new top-level file under `src/` in any package.
- Adding a new feature (new subsystem in the sidecar, new tool
  window in Studio, new CLI command).
- Reviewing a PR that grows a `src/services/`, `src/handlers/`,
  `src/state/`, or `src/ui/` folder by more than two files.

If a rule below conflicts with what the task requires, stop and
surface the conflict — do not quietly bend the rule. The whole
point of this document is that the structure is *predictable*.

---

## 1. Feature-first, layer-second

**Wrong** (layer-first, flat-by-responsibility):

```
src/
├── services/
│   ├── device.service.ts
│   ├── app.service.ts
│   ├── script-runner.service.ts
│   └── inspection.service.ts
├── handlers/
│   ├── device.handlers.ts
│   ├── app.handlers.ts
│   └── ...
└── types/
    └── all-types.ts
```

Problem: the "device" feature is scattered across three folders.
Adding a fifth service means editing the services/ folder — a
cross-cutting change. Removing a feature means archaeology.

**Right** (feature-first):

```
src/
├── infra/                    ← cross-cutting, framework-level
│   ├── transport/
│   ├── events/
│   └── session/
├── features/
│   ├── device/
│   │   ├── device.types.ts
│   │   ├── device-probe.ts
│   │   ├── driver-factory.ts
│   │   ├── device.service.ts
│   │   ├── device.handlers.ts
│   │   ├── device.service.test.ts
│   │   └── index.ts
│   ├── app/
│   ├── script/
│   └── inspection/
└── compose.ts                ← iterates features, nothing feature-specific
```

Removing `device/` is one `rm -rf` and one line in `compose.ts`.
Adding `recording/` is one folder + one append.

---

## 2. `infra/` vs `features/`

**`infra/`**: the code that has no business knowledge but is
required to run any feature — transport, event bus, session,
clocks, loggers, **notifications surface**, config loading.

Notifications and logging belong to `infra/` specifically because
every feature consumes them. Making them features would force
every other feature to import from another feature, which is
exactly the cross-coupling the rules exist to prevent. Cross-
cutting surfaces with no owning domain → `infra/`.

  - `infra/` must not import from `features/`.
  - If an infra file mentions a specific feature name, it is not
    infra — move it into that feature.
  - `infra/` changes are architectural and should be rare. A pull
    request that grows `infra/` by more than one file needs a
    reviewer that understands the whole system.

**`features/`**: everything else. Each feature is a vertical slice
that owns its types, logic, transport adapters (handlers), and
tests.

  - A feature MAY import from `infra/` (it needs a transport, an
    event bus, etc.).
  - A feature SHOULD NOT import from another feature's internals.
    Cross-feature imports go through the other feature's `index.ts`
    public surface.
  - A feature MUST NOT import from `compose.ts`. The composition
    root assembles features; features know nothing about the
    assembly.

---

## 3. Feature folder convention

Every folder under `features/` has the same shape:

```
features/<name>/
├── <name>.types.ts           (public types — shapes crossing the index)
├── <name>.service.ts          (the core logic; one class, constructor DI)
├── <name>.handlers.ts         (JSON-RPC / IPC adapter — optional)
├── <name>.service.test.ts     (colocated unit tests)
├── <name>.<helper>.ts         (supporting files: scanners, factories,
│                                strategies — as many as needed; filenames
│                                always prefixed with the feature name)
└── index.ts                   (public surface — see §5)
```

Rules:

1. **Filename prefix**. Every file under `features/<name>/` is
   prefixed with `<name>.` or `<name>-` so a grep, `git log
   --name-only`, or a bare filename in a stack trace unambiguously
   points to the feature.

2. **One class per file** when the file carries behavior. Helpers
   (types, constants) may share a file if they are genuinely
   related.

3. **Tests colocated** with the code under test — never a parallel
   `tests/` tree.

4. **No subfolders** for at least the first version of a feature.
   Nest only when the feature's file count crosses 8 and a clear
   sub-domain emerges. Premature nesting hides what the feature
   actually does.

---

## 4. Shared code: `shared/` in a feature vs hoisting

When two features need the same type or helper:

- **Small, stable, truly generic**: hoist to `infra/` or (for
  Studio) `domain/shared/`. Examples: Clock, Logger, Result<T>.

- **Stable but belongs to one feature**: leave it in that feature
  and let the other import via `index.ts`. Do NOT duplicate.

- **Experimental or churning**: keep it duplicated. Abstracting
  churn-prone code is a premature optimization — wait for the
  shape to settle.

Never create a `utils/` folder. Every helper belongs to a feature
or to `infra/`.

---

## 5. `index.ts` — the feature's public API

Every feature has one `index.ts`. It defines:

1. **What consumers outside the feature may import**. Anything
   NOT exported from `index.ts` is internal — the rest of the
   codebase must not reach in.

2. **A `register<Name>Feature()` function** (or equivalent) that
   wires the feature into the composition root. This function:

   - Takes a `CompositionContext` (dispatcher, event bus, session,
     services registry, UI shell, whatever the package uses).
   - Constructs the feature's services.
   - Registers its handlers / routes / UI slots.
   - Returns a `FeatureHandle` with optional `dispose()` so the
     root can tear down cleanly.

Example (sidecar):

```ts
// features/device/index.ts
export * from "./device.types.js";
export { DeviceService } from "./device.service.js";

export function registerDeviceFeature(ctx: SidecarContext): FeatureHandle {
  const probe = new AndroidAdbProbe(); // internal
  const factory = new DriverFactory(); // internal
  const service = new DeviceService({ probe, factory, ...ctx });
  registerDeviceHandlers(ctx.dispatcher, service);
  return { dispose: async () => service.dispose() };
}
```

Composition root:

```ts
// compose.ts
const ctx = createContext({ ... });
const features = [
  registerMetaFeature(ctx),
  registerDeviceFeature(ctx),
  registerAppFeature(ctx),
  registerScriptFeature(ctx),
  registerInspectionFeature(ctx),
];
```

Adding a feature = one import + one line in the array.

---

## 6. Cross-feature rules

Allowed:

- `features/a` imports from `features/b/index.ts` (public surface).

Forbidden:

- `features/a` imports from `features/b/b.service.ts` (internal).
- `features/a` imports from `features/b/anything-but-index.ts`.
- `infra/*` imports from `features/*` (infra is always leaf).
- Circular imports at any level.

Enforcement: dep-cruiser rule set in each package (see §10).

When feature A *must* know about feature B's data, define the
contract on B's `index.ts` and have A depend on the type, not on
the runtime service directly. Often this means extracting an
interface that B exports and A receives via dependency injection
at composition time.

---

## 7. Studio: 4 layers + feature-slicing

Studio has an enforced 4-layer architecture (`ui/`, `state/`,
`domain/`, `platform/`) — see `rules/studio-architecture.md`.
Layers are PRESERVED. Feature-slicing happens *within* each layer:

```
apps/studio/src/
├── domain/
│   ├── shared/               (Clock, Logger, Result — truly generic)
│   └── features/
│       ├── workspace/
│       │   ├── workspace.types.ts
│       │   ├── workspace.port.ts
│       │   ├── workspace.mock.ts
│       │   └── index.ts
│       ├── editor/
│       ├── runtime/
│       ├── bookmarks/
│       ├── nav-history/
│       └── ...
├── state/
│   ├── core/                 (services.ts = composition root seed)
│   └── features/
│       ├── workspace/        (workspace.store, workspace-state.store, fs-events)
│       ├── editor/           (editor.store, bookmarks.store, nav-history.store)
│       ├── layout/           (layout.store, paneSizes)
│       ├── runs/             (runs.store, run-configs.store)
│       ├── notifications/
│       ├── problems/
│       ├── todos/
│       ├── projects/
│       ├── settings/
│       ├── theme/
│       └── actions/
├── platform/
│   └── features/             (concrete adapters matching each domain feature)
│       ├── workspace/        (workspace.tauri.ts)
│       ├── runtime/          (runtime.embedded.ts)
│       ├── settings/         (settings-store.fs.ts)
│       └── ...
└── ui/
    ├── primitives/           (Button, ContextMenu, ResizeHandle, Tooltip)
    ├── shell/                (app-shell, main layout composition)
    └── features/
        ├── workspace/        (file-tree with CRUD + speed search)
        ├── editor/           (editor-pane, editor-tabs, script-editor, breadcrumb)
        ├── tool-windows/     (bottom pane with problems / todos / terminal, stripes)
        ├── navigation/       (recent-locations, bookmarks-popup)
        ├── command-palette/  (find-everywhere, find-in-path, file-switcher, keymap-help)
        ├── notifications/    (notification-stack)
        ├── settings/         (settings-dialog)
        ├── theme/
        ├── run-configs/      (dropdown + dialog)
        └── welcome/
```

Rules:

- Layer boundaries (`ui` → `state` → `domain` ← `platform`) still
  enforced by dep-cruiser, ESLint, TS project refs.
- Within a layer, feature folders are the unit of growth.
- A feature may span layers: `domain/features/workspace`,
  `state/features/workspace`, `platform/features/workspace`,
  `ui/features/workspace`. Each is independent; they communicate
  via the ports declared in `domain/`.
- Names match across layers **where the concepts correspond
  one-to-one** (workspace, editor, runs, settings, theme).
- Names MAY diverge where a layer groups differently than domain.
  Example: `ui/features/command-palette/` is one UI feature that
  hosts popups spanning several state/domain features
  (nav-history, bookmarks, fuzzy file search, keymap help). This
  is acceptable when:
    - the UI grouping is dictated by shared UX (modal + fuzzy +
      keyboard), not by domain ownership; and
    - state and domain stay feature-sliced at their own grain.
  Grep ergonomics still hold: every popup file inside
  `ui/features/command-palette/` has the feature prefix of the
  popup itself (e.g., `bookmarks-popup.tsx`, `find-everywhere.tsx`)
  so `git grep bookmarks-popup` still lands on one file.

---

## 8. Extension-point patterns (add feature, no shell edit)

The composition root and the UI shell are the two places that
grow fastest when features scatter. Three registry patterns keep
them stable:

### 8.1 Feature registry (composition)

`compose.ts` (sidecar) / `main.tsx` (Studio) holds only:

```ts
const FEATURES = [
  registerMetaFeature,
  registerDeviceFeature,
  registerAppFeature,
  registerScriptFeature,
  registerInspectionFeature,
];
for (const register of FEATURES) register(ctx);
```

Adding a feature = append one line. Removing = delete one line.

The `ctx` object is **not a god context**. It is a bundle of
focused registries the feature uses to attach its behavior:

```ts
interface SidecarContext {
  readonly dispatcher: Dispatcher;         // JSON-RPC registration
  readonly events: EventBus;               // pub/sub
  readonly session: Session;               // shared state
  readonly notifications: NotificationSink; // infra/notifications
  readonly logger: Logger;                  // infra/logger
}

interface StudioContext {
  readonly services: StudioServices;
  readonly toolWindows: ToolWindowRegistry;
  readonly actions: ActionRegistry;
  readonly popups: PopupRegistry;
  readonly notifications: NotificationSink;
}
```

A feature takes what it needs and ignores the rest. New needs
add one registry field — the individual registries stay single-
purpose. Never add cross-feature services to the context; those
live in their own feature's `index.ts` and get imported
explicitly when truly needed.

### 8.2 Tool-window registry (Studio shell)

`ui/shell/tool-windows.registry.ts` holds a static array of
tool-window descriptors:

```ts
export const TOOL_WINDOWS: ToolWindowDescriptor[] = [
  { id: "projects", side: "left", icon: Files, labelKey: "Projects", ... },
  { id: "structure", side: "left", icon: ListTree, labelKey: "Structure", ... },
  { id: "run", side: "right", icon: PlaySquare, labelKey: "Run", ... },
  { id: "problems", side: "bottom", icon: AlertCircle, labelKey: "Problems", ... },
];
```

Stripes iterate the descriptors. Adding a tool window = one entry
in the array + one feature folder implementing the `ToolWindow`
contract (title component + body component).

### 8.3 Action registry (already in place)

`state/features/actions/actions.store.ts` already holds every
shortcut in `ACTION_DEFINITIONS`. When adding a feature that has
actions:

- The feature exports its action definitions from its own
  `index.ts`.
- A tiny import in `actions.store.ts` includes them via spread:
  `...workspaceActions, ...editorActions, ...`. Preferred pattern
  is a single collector file that imports + re-exports.

---

## 9. Tests colocated, test data in feature

- Unit tests: `feature.service.test.ts` next to `feature.service.ts`.
- Fixtures / mocks used by only one feature: inside the feature
  folder.
- Fixtures shared across features: under `<pkg>/testing/` (sidecar)
  or `<pkg>/src/testing/` (Studio). These ARE infra-like and must
  not import from features.

---

## 10. Enforcement

Each package owns its own dep-cruiser config:

- `packages/sidecar/.dependency-cruiser.cjs` — forbids:
  - `infra/*` → `features/*`
  - `features/<A>/**/!(index.ts)` ← `features/<B>/**`
  - circular at any depth
- `apps/studio/.dependency-cruiser.cjs` — existing 4-layer rules
  PLUS the same feature-isolation rules within each layer.

ESLint `no-restricted-imports` patterns help at editor time for
the same rules.

Every commit runs `depcruise`. A PR that violates feature
isolation fails CI.

---

## 11. Before you create a file

1. Does it belong in `infra/` (generic)? If yes, proceed.
2. Which feature owns it? Put it in `features/<name>/`.
3. Does no existing feature fit? Either extend the closest one
   with a clear name, or create a new feature folder — whichever
   is smaller.
4. Never create a file directly under `src/` (or `src/services/`,
   `src/handlers/`, etc.). The flat placement is the error.

---

## 12. Adding a feature — checklist

A new feature is ready when:

- [ ] `features/<name>/` folder exists with the file conventions in §3.
- [ ] `features/<name>/index.ts` exports the public surface + a
  `register<Name>Feature(ctx)` function.
- [ ] The composition root appends the register call to its
  feature array.
- [ ] Unit tests cover the service's public behavior.
- [ ] `dep-cruiser` passes — no feature-isolation violations.
- [ ] If the feature adds actions, UI components, tool windows, or
  routes, the corresponding registry is updated (§8).
- [ ] No file outside the feature folder names the feature — grep
  for the feature name confirms all hits are in `features/<name>/`.

A failure on any item means the feature is not complete; fix
before merge. "We'll clean this up later" is how we ended up
needing this document.

---

## 13. Migration policy

These rules take effect on merge. Existing code follows one of two
tracks depending on risk:

**Full refactor (all-at-once)**: packages / apps with small,
contained code — at ratification time that means
`packages/sidecar/`. The whole tree moves in one pull request so
the state never sits in a half-migrated shape.

**Incremental refactor (touch-to-convert)**: packages / apps where
a full move would churn hundreds of files — at ratification time
that means `apps/studio/`. Rules:

- **Every new file** must follow feature structure. Exceptions
  require an ADR.
- **Every new feature** must be added under `features/` from day
  one. Flat placement is not allowed for new work.
- **Existing flat files** are grandfathered. When a contributor
  edits more than half the lines of a flat file, they move it
  under its proper `features/<name>/` folder in the same pull
  request. Small edits do not trigger a move.
- Dep-cruiser has a two-phase rule set: new `features/<name>/`
  folders are held to the full isolation rules; the old flat
  tree retains a grandfather exemption listed explicitly by path.
  The exemption list shrinks over time and must never grow.

When the incremental policy eventually finishes the migration,
remove the exemption list and the grandfather clause. Until then,
the whole team reads this section before committing so the
direction of travel stays one-way.

## 14. Removing a feature — checklist

- [ ] Remove the register call from the composition root.
- [ ] Delete `features/<name>/`.
- [ ] Remove its entries from any registries (§8).
- [ ] Run tests and dep-cruiser.

If any of these steps needs to edit files outside `features/<name>/`
that do NOT live in §8's small set of registries, that counts as a
design leak — investigate and close it.
