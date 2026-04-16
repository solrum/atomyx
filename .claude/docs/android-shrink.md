# Android control plane shrink plan

Executes alongside the legacy `src/` retire. Records the Android-APK
dead code inventory identified by the batch 13 parity audit and the
concrete execution steps.

**Preconditions are now met** as of the legacy-retire batch: `src/`
was deleted entirely, no caller of `/resolve` / `/actions/tap` (selector-taking
variant) / `/actions/input` / `/actions/clear_focused_input` /
`/tree?format=compact` remains in the TypeScript side. The Android
routes those adapters consumed are safe to drop.

## Why this exists

iOS batch 10 moved selector resolution, obscurement detection, and
compact tree filtering out of the Swift driver to
`@atomyx/core-driver`, leaving the Swift side as a thin XCUI wrapper.
That was safe because iOS had no legacy caller.

Android has the **same architectural opportunity** — the APK ships an
identical set of selector-side logic — but it also has a **live legacy
caller** in `src/adapters/agent-direct.adapter.ts` that consumes the
selector-taking routes. Until legacy `src/` is retired, deleting the
Android routes would break a runtime path that still exists.

This doc captures the inventory so the shrink can land the moment
legacy retirement happens, with no re-discovery work.

## Preconditions for execution

All of these must be true before applying the Android-side deletions:

1. `src/adapters/agent-direct.adapter.ts` is deleted or no longer
   imports `/resolve`, `/actions/tap`, `/actions/input`,
   `/actions/clear_focused_input`, or `/tree?format=compact`.
2. `src/adapters/device-router.ts` no longer wires
   `AgentDirectController` (grep for `AgentDirectController` returns
   zero hits under `src/`).
3. `src/cli/main.ts` and `src/tools/devices.tool.ts` no longer
   transitively depend on the deleted adapter.
4. Root `package.json` `bin` / `scripts` no longer reference the
   legacy entrypoint if it was exposed as a CLI.
5. Any e2e / smoke test that spoke the legacy MCP protocol has been
   migrated to the new `@atomyx/core-driver-mcp` surface.

Verification command (run before starting the shrink):

```bash
grep -rn '/resolve\|/actions/tap"\|/actions/input\|/actions/clear_focused_input\|format=compact' src/ packages/ 2>/dev/null
```

Expected output: empty. Any remaining hit is a blocker.

## Android APK — files to delete

Paths under
`native/android-agent/app/src/main/java/dev/atomyx/agent/`.

### Full-delete

| File | LOC | Reason |
|---|---|---|
| `control/SelectorResolver.kt` | 80 | Selector chain lives in `@atomyx/core-driver` |
| `control/strategy/ResolutionStrategy.kt` | 56 | Strategy protocol for deleted resolver |
| `control/strategy/ResourceIdStrategy.kt` | 57 | " |
| `control/strategy/TextStrategy.kt` | 64 | " |
| `control/strategy/ContentDescStrategy.kt` | 36 | " |
| `control/strategy/HintStrategy.kt` | 40 | " |
| **Subtotal** | **333** | |

Also delete the empty `control/strategy/` directory after the files
are gone.

### Full-delete (route layer)

In `control/router/CommonRoutes.kt`, delete these route classes:
- `ResolveRoute` (~35 LOC)
- `TapRoute` (selector-taking variant; **keep `TapCoordsRoute`**) (~10 LOC)
- `InputRoute` (~12 LOC)
- `ClearFocusedInputRoute` (~8 LOC)
- `parseSelector` helper function (~12 LOC) — only used by the deleted
  routes

Estimated: **~80 LOC** removed from `CommonRoutes.kt`.

### Full-delete (registration)

In `control/HttpControlServer.kt`:
- Remove imports: `ResolveRoute`, `TapRoute`, `InputRoute`,
  `ClearFocusedInputRoute`
- Remove the 4 matching entries from `buildRoutes()`

### Partial-delete (UiTreeService surgery)

`control/UiTreeService.kt` (404 LOC currently) has multiple concerns
intertwined. The shrink needs surgical editing, not a full rewrite:

Delete:
- `dumpCompact()` function (~30 LOC)
- `bestSelector()` helper (~7 LOC)
- `roleOf()` helper (~12 LOC)
- `CompactElement` data class (~10 LOC)
- `findEntry()` function (~3 LOC) — only used by the deleted selector
  chain; host-side adapter uses `dumpTree` output directly
- `findLiveNode()` function (~32 LOC) — same reason; host-side drives
  gestures via coordinates, not by re-fetching live nodes
- `registry: MutableMap<String, RegistryEntry>` field (~2 LOC) and all
  its populate/clear/lookup sites inside `walk()` and `dumpTree()`
  (~6 LOC)
- `RegistryEntry` data class (~12 LOC)
- `ElementMatchDto` data class (~5 LOC) — appears unused already; grep
  to confirm before deletion

Keep:
- `dumpTree()` — this is the canonical tree source consumed by the new
  `@atomyx/core-driver-android` adapter via `/tree`
- `getKeyboardInfo()` + `KeyboardInfo` + `KeyboardKey` + `classifyLayout` — 
  host-side IME guard uses this (symmetric to iOS `getKeyboard`)
- `walk()` — still needed by `dumpTree`, but strip the registry-populate
  side-effect
- `markDirty()`, cache invalidation, `@Synchronized` scaffolding — the
  tree dump cache is orthogonal to selector logic
- `RawElementDto`, `BoundsDto` — wire shape the new adapter consumes

Estimated: **~120 LOC** net removed from `UiTreeService.kt`.

### Partial-delete (GestureDispatcher)

`control/GestureDispatcher.kt` (494 LOC) has selector-taking methods
that only the deleted routes call. Verify with grep before deletion:

```bash
grep -n 'services.gestures.tap(\|services.gestures.inputText(' native/android-agent/
```

If only the deleted routes are the callers, delete:
- `tap(selector: SelectorResolver.Selector)` overload (~30 LOC)
- `inputText(selector: SelectorResolver.Selector, text: String)` (~50 LOC)
- `clearFocusedInput()` method (~25 LOC if only `ClearFocusedInputRoute`
  calls it)
- Any `SelectorResolver`-typed fields / constructor params

Keep:
- `tapAt(x, y)` (used by `TapCoordsRoute`)
- `longPressAt`, `swipe`, `typeViaKeyboard`, `pressKey`, `launchApp`,
  `forceStopApp`

Estimated: **~100 LOC** removed.

### Total expected Android APK shrink

~333 (SelectorResolver + strategies) + ~80 (routes) + ~120
(UiTreeService) + ~100 (GestureDispatcher) = **~630 LOC**, roughly
**28%** of the current 2268 LOC Android agent. Comparable ratio to
iOS batch 10's 27% shrink.

## Legacy src/ — files to delete (preconditions step)

These are the prerequisite deletions that unblock the Android shrink.
Execute this **first**, verify tests still pass, then proceed to the
Android APK edits above.

| File | LOC (approx) | Notes |
|---|---|---|
| `src/adapters/agent-direct.adapter.ts` | ~200 | Selector-taking legacy HTTP adapter |
| `src/adapters/device-router.ts` | ~? | Wires `AgentDirectController` into tool layer |
| Any other `src/adapters/*` orphaned by the above | — | Follow the import graph |
| `src/tools/devices.tool.ts` | ~? | If it only exists to wrap the legacy adapter |
| `src/cli/main.ts` | ~? | If the legacy CLI entrypoint is being retired wholesale |

**Do not assume these are safe to delete.** Run the full grep
verification above, plus:

```bash
grep -rn 'from "./adapters/agent-direct' src/
grep -rn 'import.*device-router' src/
npm test --workspaces  # expect green before + after
```

## Step-by-step execution order

When executing this batch in the future, do it in this order to keep
each step reversible:

1. **Legacy grep audit** — confirm preconditions. Produce a list of
   current callers for each deleted path. Any unexpected hit stops the
   batch.

2. **Delete legacy src/ adapters first**. Run `npm test --workspaces`
   after each deletion. If anything goes red, the adapter is still
   live — resolve before continuing.

3. **Delete Android routes + registration** (`CommonRoutes.kt` +
   `HttpControlServer.kt`). The APK still compiles because
   `SelectorResolver` and strategies are unused imports now — Kotlin
   will flag them with warnings.

4. **Delete `SelectorResolver.kt` + `strategy/` directory**. APK still
   compiles; `UiTreeService` may still reference `RegistryEntry` etc.

5. **Surgery on `UiTreeService.kt`** — remove registry, compact dump,
   helpers. Run `./gradlew assembleDebug` (or whatever the project's
   APK build target is) to verify compilation.

6. **Surgery on `GestureDispatcher.kt`** — remove selector-taking
   methods. Rebuild APK.

7. **Rebuild + smoke test** — build APK, sideload to emulator, run the
   `@atomyx/core-driver-android` test suite against it. All tests
   should pass because the new adapter only uses the surviving routes.

8. **Update `.claude/docs/repo-map.md`** and any architecture doc that
   mentions the deleted paths.

9. **Commit as a single batch** — everything in one commit because
   partial application leaves the APK in a half-shrunk state that's
   hard to reason about.

## Related work

- iOS batch 10 (`native/ios-driver/`): the symmetric shrink that set
  the precedent. Same rationale, no legacy blocker, already landed.
- iOS batch 11: SOLID polish post-shrink (strategy registries, thread
  safety docs). Android may need a similar polish pass after the
  shrink lands — defer that to a batch 14 or skip if Android doesn't
  have the same SRP/OCP hotspots.
- iOS batch 12: end-to-end verification that TS adapter matches the
  shrunk driver's wire contract. Apply the same audit to Android
  after the shrink — grep `packages/core-driver-android/src/*.ts` for
  route names and verify every one still exists in the APK.
