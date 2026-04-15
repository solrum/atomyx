# adet tool reference

> **Read this before touching anything under `src/tools/`.** It's the fast path to knowing which file holds which tool, what the response shape is, and which invariants must be preserved. Updated on every tool change.

## Summary

- **19 tools total.** Consolidated from ~40 to eliminate overlap-driven agent confusion.
- **100% class-based.** Every tool extends `Tool<TShape>` with constructor-injected strategies. No inline handler closures remain.
- **Source of truth**: `src/registry.ts` constructs strategy singletons, instantiates concrete tool classes, and registers them via `factory.registerTool(instance)`. `ToolFactory.register({...})` is still exposed for future inline additions but is unused today.
- **Client contract**: tools are addressable by name via the MCP protocol. Response shape is a JSON-serializable object; consumers read `ok`, `reason`, category-specific fields.

## Architecture at a glance

```
src/tools/
├── core/                         # strategies + base class + cache (no tool registration)
│   ├── tool.ts                   # abstract Tool<TShape> + ToolShape
│   ├── selector-resolution-pipeline.ts   # priority broadening (resourceId → contentDesc → text → …)
│   ├── ime-geometric-guard.ts    # coord-in-IME structural check
│   ├── fuzzy-resource-matcher.ts # 3-tier resourceId suffix match
│   ├── ambiguity-detector.ts     # duplicate-token counts for tree render
│   ├── structural-input-finder.ts # 4-strategy chain wrapping find-input.ts
│   ├── transition-classifier.ts  # wraps transition-diagnostics.ts
│   └── ui-tree-cache.ts          # 2s dedupe cache, invalidated on mutations
│
│  ── Class-based tools (one file per tool unless trivial) ──
├── devices.tool.ts               # ListDevicesTool + SelectDeviceTool
├── launch-app.tool.ts            # LaunchAppTool
├── get-ui-tree.tool.ts           # GetUiTreeTool
├── get-screenshot.tool.ts        # GetScreenshotTool
├── find-element.tool.ts          # FindElementTool
├── tap.tool.ts                   # TapTool
├── tap-and-wait-transition.tool.ts # TapAndWaitTransitionTool
├── input-text.tool.ts            # InputTextTool
├── wait-for-element.tool.ts      # WaitForElementTool
├── report-bug.tool.ts            # ReportBugTool
├── playbook-tools.ts             # GetPlaybookTool, AddCaseStudyTool, GetCaseStudiesTool
├── trivial.tools.ts              # PressKeyTool, SwipeTool, ListAppsTool, StartRunTool, FinishRunTool
│                                 # (bundled — each is ~10 LoC of pure delegation)
│
│  ── Helpers (pure functions, no tool registration) ──
├── tool-factory.ts               # ToolFactory: registerTool + register + byName
├── tree-render.ts                # renderCompactLine, filterStable, sortByStability
├── find-input.ts                 # pure functions wrapped by StructuralInputFinder
├── playbook-content.ts           # static PLAYBOOK markdown string
├── selector-quality.ts           # warns when text used but stable id exists
├── preflight.ts                  # stale a11y binding detection (platform-keyed)
└── transition-diagnostics.ts     # pure functions wrapped by TransitionClassifier
```

**All 19 tools are class-based.** Each extends `Tool<TShape>` with constructor-injected strategies. `execute()` methods are orchestration only — no business rule lives inline.

**Strategy classes in `src/tools/core/`** contain the reusable business rules. Each is constructor-injected into the tool classes that need it, and each has dedicated unit tests under `src/tools/core/*.test.ts`.

## Tool catalog

Each row lists: tool name | file | signature | what it returns | mutates.

### Device

| Tool | File | Args | Returns | Mutates |
| --- | --- | --- | --- | --- |
| `list_devices` | `devices.tools.ts` | `{}` | `{ devices: DeviceInfo[], selected: string \| null }` | no |
| `select_device` | `devices.tools.ts` | `{ deviceId }` | `{ ok, selected, platform }` or `{ ok: false, preflight }` on stale a11y | yes (connects) |

### App

| Tool | File | Args | Returns | Mutates |
| --- | --- | --- | --- | --- |
| `launch_app` | `app.tools.ts` | `{ appId, forceStop?=true }` — `appId` is Android package name / iOS bundle id | `{ ok, initialTree, inputs[], elementCount, instruction }` — `inputs[]` = `[{label, stableId, center, currentValue}]` | yes |
| `list_apps` | `app.tools.ts` | `{}` | `{ packageName, label? }[]` | no |

### Screen

| Tool | File | Args | Returns | Mutates |
| --- | --- | --- | --- | --- |
| `get_ui_tree` | `ui.tools.ts` | `{ stableOnly?=true, limit?=40 }` | `{ treeFingerprint, count, totalAvailable, truncated, tree }` | no |
| `find_element` | `ui.tools.ts` | `{ resourceId?, contentDesc?, text?, labelContains?, keyword?, role?, nth?, nthOfRole?, inputField?=false, all?=false, limit?=20 }` | `{ found, selector, label, role, center }` / `{ found: false, candidates?, suggestions? }` / `{ count, tree }` when `all=true` | no (caches 2s) |
| `get_screenshot` | `ui.tools.ts` | `{}` | `{ path, bytes, format: "png" }` — saved to `.adet/screenshots/` | no |

### Actions

| Tool | File | Args | Returns | Mutates |
| --- | --- | --- | --- | --- |
| `tap` | `actions.tools.ts` | `{ selector? } \| { x, y }` | `{ ok, reason? }` or blocked with `{ ok: false, reason, candidates? }` | yes |
| `tap_and_wait_transition` | `actions.tools.ts` | `{ selector, waitForAbsent?, waitForAppear?, timeoutMs?=10000, maxTimeoutMs?=60000, intervalMs?=300, loadingKeywords? }` | `{ ok, waitedMs }` / `{ ok: false, classification, hint, dialogLabels?, overlayKind?, targetStateChanged? }` | yes |
| `input_text` | `actions.tools.ts` | `{ selector?, x?, y?, text, clearFirst?=true }` | `{ ok, typed, total, reason?, strategy? }` | yes |
| `swipe` | `actions.tools.ts` | `{ fromX, fromY, toX, toY, durationMs?=300 }` | `{ ok }` — rejects near-zero movement (`dx<16 && dy<16`) | yes |
| `press_key` | `actions.tools.ts` | `{ key: "back" \| "home" \| "enter" }` | `{ ok }` | yes |

### Wait

| Tool | File | Args | Returns | Mutates |
| --- | --- | --- | --- | --- |
| `wait_for_element` | `assertion.tools.ts` | `{ selector, absent?=false, timeoutMs?=5000, intervalMs?=300 }` | `{ ok, found, waitedMs }` | no |

### Run

| Tool | File | Args | Returns | Mutates |
| --- | --- | --- | --- | --- |
| `start_run` | `verification.tools.ts` | `{ name, source?="interactive" }` | `{ ok, runId }` | yes (resets history) |
| `finish_run` | `verification.tools.ts` | `{ status?="passed" }` | `{ ok, run, savedTo }` | yes |
| `report_bug` | `verification.tools.ts` | `{ severity, title, description?, captureScreenshot?=true, context? }` | `{ ok, bugId, screenshotPath }` | yes |

### Guidance

| Tool | File | Args | Returns | Mutates |
| --- | --- | --- | --- | --- |
| `get_playbook` | `playbook.tools.ts` | `{}` | `{ playbook: string }` (markdown) | no |
| `add_case_study` | `playbook.tools.ts` | `{ title, trigger, solution, example? }` | `{ ok, file }` | no (writes `.adet/case-studies/YYYY-MM.md`) |
| `get_case_studies` | `playbook.tools.ts` | `{ month? }` | `{ found, month, content? }` | no |

## One tool per intent

The consolidation rule: **no two tools share an intent**. Overlap causes the agent to pick wrong; the old surface had 4 ways to type text and 3 ways to find an element, which was measurably bad.

If a new capability overlaps an existing tool, **extend the existing tool with a new parameter**. Examples of good extensions:

- `input_text` accepts `{selector}` OR `{x, y}` — merged `input_text`, `fill_input_at_coordinates`, `type_via_keyboard`, `clear_focused_input`.
- `tap` accepts `{selector}` OR `{x, y}` — merged `tap` + `tap_coordinates`.
- `find_element` accepts `all: true` for lists, `inputField: true` for the structural chain, `nth` / `nthOfRole` for positional queries — merged `find_element` + `find_elements` + `find_input` + `resolve_selector`.
- `launch_app` accepts `forceStop: boolean` — merged `launch_app` + `force_stop_app`.

**Before adding a new tool**, justify why an existing tool can't absorb the intent. File the reasoning in a case study if it's non-obvious.

## Shared patterns

### Selector priority

Priority order on both sides: **resourceId > contentDesc > text > textContains > hint**.

- `resourceId`: most stable, language-independent. Use verbatim, including package prefix if present. Flutter / Compose / RN ids without a prefix (e.g. `G01-05-01/2`) work via `ResourceIdStrategy`'s walk fallback.
- `contentDesc`: Android primary content selector. Material / Compose / native widgets consistently set contentDesc; `text` is often empty. Prefer contentDesc over text when picking a selector manually.
- `text`: less reliable (localized, often empty for buttons).
- `textContains`: substring fallback.
- `hint`: fuzzy last resort.

**Priority broadening inside `tap`**: even if the agent passes `text:"OK"`, the handler tries `{contentDesc:"OK"}` first, then `{text:"OK"}`, etc. Agents don't need to know platform conventions. See `src/tools/actions.tools.ts` tap handler.

### Fuzzy fallback (resourceId)

If exact `resourceId` match fails, `tap` and `find_element` try suffix match:

```
G01-05-01/2   →  tries "endsWith('/G01-05-01/2')" then "endsWith('2')"
"2"           →  matches any id ending in /2
```

Single candidate → success. Multiple candidates → return `{ok: false, candidates: [...]}` so the agent picks by full id.

### Anti-pattern blocks

Structural, not counter-based (counters caused false positives on legit flows):

- **Coordinate inside IME** (`coordInIme`): `tap({x,y})`, `long_press_coordinates`, and internally `tap_and_wait_transition` refuse coordinates that fall inside an element with `isInIme: true`. Forces agent to use `input_text` for typing.
- **IME element tap via selector**: `tap` refuses when resolved element has `isInIme: true`. Same intent as coordinate block.
- **Input field tap by value**: `tap({text:"09044085"})` is blocked when the resolved element is editable — the text is the current value, not the identity. Agent should use `input_text` with the field's label/id.
- **Swipe no-op**: `swipe(x,y → x,y)` (movement < 16px) is blocked — agents once used it as a dummy "reset" gesture.
- **Content selector not found + agent guessed text**: `tap` returns `NOT FOUND` with an explicit hint to call `get_ui_tree` instead of retrying with another guess.

**Removed**: consecutive-tap counters (caused false positives; structural geometric blocks are enough).

### Tree render format

`renderCompactLine` in `src/tools/tree-render.ts` produces one line per element:

```
resourceId="G01-05-01/2" view @410,487
contentDesc="ログイン" button "ログイン" @540,1442
text="保存" @100,200
contentDesc="注文" @540,157 (2×)          ← duplicate marker
```

Rules:
- **Always quote the value**: `resourceId="..."` / `contentDesc="..."` / `text="..."`. Agents copy the value between quotes verbatim. Do not render as `#G01-05-01/2` — agents misparse the `/`.
- **Never strip package prefix** from resourceId. Android's native lookup requires the full qualified form.
- **No clickable indicator** (`○`). The `clickable` flag is unreliable on Flutter / Compose / RN; showing it misleads agents into skipping tappable elements.
- **Inline center coords** `@cx,cy`. First-class selector — agents can use them directly via `tap({x,y})` / `input_text({x,y,text})`.
- **Ambiguity marker** `(N×)` when multiple elements share the same selector token. Agent must disambiguate via coords or `nth`.

### UI tree cache

`get_ui_tree` caches the last compact dump for 2 seconds. `find_element` reuses the cache. Duplicate `get_ui_tree` calls within 1.5s on an unchanged fingerprint are **blocked** with a hint to call `find_element` instead.

Mutating tools invalidate the cache via `ctx.invalidateUiCache()` (wired in `src/server.ts` dispatcher — every tool in `MUTATING_TOOLS` auto-invalidates after running).

### `find-input` strategy chain

`src/tools/find-input.ts` runs a 4-strategy chain to locate the real editable text field for a semantic query. Real apps expose inputs as siblings of a label, not children. Order of strategies tried:

1. `following_sibling_edittext` — anchor's next sibling IS the EditText (simple row).
2. `following_sibling_container_edittext` — next sibling is a wrapper (no-info View) containing the EditText + icons (prefix/suffix, password toggle).
3. `descendant_edittext` — anchor wraps the EditText directly.
4. `self_is_edittext` — rare, only when explicit Semantics.

Triggered when:
- `input_text` is called with a selector that doesn't resolve to an editable node (label/container rather than the input itself).
- `find_element({inputField: true, ...})` is called.

The strategy returns `{element, strategy, anchor, stableId}` where `stableId` is extracted from the anchor (resourceId > contentDesc > label). Agents cache `stableId` for later identification without re-dumping.

### Stale a11y detection (`preflight`)

`src/tools/preflight.ts` exports `preflight(ctl)` — called by `select_device` and `launch_app`. If `getUiSummary()` returns empty AND `currentActivity` has no package, the binding is stale (usually after APK install) and the tool returns an actionable rebind hint with the three `adb shell` commands.

## File map

### Wiring
| File | Responsibility |
| --- | --- |
| `src/registry.ts` | Tool factory wiring; constructs strategies, class-based tools, inline categories; `MUTATING_TOOLS` set |
| `src/server.ts` | MCP dispatcher; updates `ctx.lastToolName`, invalidates cache after mutations |
| `src/runtime/adet-context.ts` | DI container (shared): controller, history, results, recordedActions, invalidateUiCache, lastToolName |
| `src/tools/tool-factory.ts` | `ToolFactory` with `registerTool` (class) and `register` (inline) |

### Core strategies (`src/tools/core/`)
| File | Responsibility |
| --- | --- |
| `tool.ts` | `abstract class Tool<TShape>` base + `ToolShape` interface |
| `selector-resolution-pipeline.ts` | Priority broadening: tries resourceId → contentDesc → text → textContains → hint in order regardless of what the agent passed |
| `ime-geometric-guard.ts` | `coordInIme(x, y)` — refuses coordinate gestures inside IME region |
| `fuzzy-resource-matcher.ts` | 3-tier match (exact → suffix → substring) for Flutter / Compose / RN non-qualified ids |
| `ambiguity-detector.ts` | Computes duplicate-token counts so tree render can mark ambiguous selectors |
| `structural-input-finder.ts` | Wraps `find-input.ts` 4-strategy chain as an injectable class |
| `transition-classifier.ts` | Wraps `transition-diagnostics.ts` classify functions |
| `ui-tree-cache.ts` | 2s dedupe cache for `getUiSummary`; shared between `get_ui_tree` and `FindElementTool` |

### Class-based tools (all 19)
| File | Tool(s) | Injected strategies |
| --- | --- | --- |
| `devices.tool.ts` | `list_devices`, `select_device` | *(no strategies — uses device-router directly)* |
| `launch-app.tool.ts` | `launch_app` | `StructuralInputFinder` |
| `get-ui-tree.tool.ts` | `get_ui_tree` | `UiTreeCache`, `AmbiguityDetector` |
| `find-element.tool.ts` | `find_element` | `UiTreeCache`, `StructuralInputFinder`, `FuzzyResourceMatcher` |
| `get-screenshot.tool.ts` | `get_screenshot` | *(none)* |
| `tap.tool.ts` | `tap` | `SelectorResolutionPipeline`, `ImeGeometricGuard`, `FuzzyResourceMatcher` |
| `tap-and-wait-transition.tool.ts` | `tap_and_wait_transition` | `TransitionClassifier` |
| `input-text.tool.ts` | `input_text` | `StructuralInputFinder` |
| `wait-for-element.tool.ts` | `wait_for_element` | *(none — early-probe inline)* |
| `report-bug.tool.ts` | `report_bug` | *(none)* |
| `playbook-tools.ts` | `get_playbook`, `add_case_study`, `get_case_studies` | *(none)* |
| `trivial.tools.ts` | `press_key`, `swipe`, `list_apps`, `start_run`, `finish_run` | *(none — bundled trivial tools)* |

### Helpers (pure functions — not strategies)
| File | Responsibility |
| --- | --- |
| `tree-render.ts` | `renderCompactLine`, `filterStable`, `sortByStability` |
| `find-input.ts` | Pure functions wrapped by `StructuralInputFinder` |
| `transition-diagnostics.ts` | Pure functions wrapped by `TransitionClassifier` |
| `selector-quality.ts` | Warns when `text` used but a stable id exists on the resolved element |
| `preflight.ts` | Stale a11y binding detection; platform-keyed rebind hints |

`runner.tools.ts` and `test.tools.ts` exist but are **not wired into the registry**. Keep them as dormant scaffolding for future re-introduction of YAML runner / test-case recording via MCP.

## Response shape conventions

- Success: `{ ok: true, ... }` or `{ found: true, ... }` for query tools.
- Failure: `{ ok: false, reason: string, ... }` — the `reason` is the primary hint the agent reads. Keep it **actionable** (point at a specific tool or fix), not descriptive.
- Blocked: same as failure, prefix reason with `BLOCKED (...)`. Include `blockedSelector` / `consecutiveCount` / `isInIme` / etc so the agent has diagnostic context.
- Ambiguous (query tools): `{ found: false, candidates: [...] }` or `{ candidates: [{selector, center, label}, ...] }` so the agent can pick one.
- Response size matters: keep `tap_and_wait_transition` failure under 500B by omitting `currentSnapshot`, `appeared`, verbose diagnostics. Let the agent call `get_ui_tree` / `get_screenshot` if they need more.

## Extension checklist (changing a tool)

1. **Read this doc first.** Confirm you understand which existing tool owns the intent.
2. **Can an existing tool absorb the change via a new parameter?** If yes, extend it. If no, document the reason.
3. **If adding a tool**:
   - Decide: class-based (extends `Tool<TShape>`) or legacy inline category. Prefer class-based if the tool has any non-trivial orchestration (delegates to strategies) or shared business rules.
   - Class-based: create `src/tools/<name>.tool.ts`, extend `Tool<{args, result}>`, inject strategies via constructor, register via `factory.registerTool(new MyTool(...))` in `src/registry.ts`.
   - Inline: call `factory.register({ name, description, inputSchema, handler })` inside a `registerXxxTools` function.
   - Keep the description short — offload details to `get_playbook`.
   - Add the name to `MUTATING_TOOLS` in `src/registry.ts` if it changes device state.
   - Write a test using `MockController` for orchestration + per-strategy unit tests.
4. **If adding a business rule**:
   - Don't inline it. Create a class under `src/tools/core/` with a narrow contract (one public method, clear inputs, clear outputs).
   - Add unit tests under `src/tools/core/<rule>.test.ts`.
   - Inject into the tool class via constructor.
5. **If adding a device-side capability**:
   - Define the method in `src/adapters/device-controller.port.ts` with a JSDoc describing the Android / iOS semantic mapping.
   - Implement in `agent-direct.adapter.ts` (HTTP call). If the wire format is Android-specific (e.g. `packageName`), translate to the neutral name (`appId`) at the adapter boundary.
   - Stub in `ios-xctest.adapter.ts` with `this.nope(...)`.
   - Add to `mock-controller.ts`.
   - On Android side: add a `Route` in `android/.../control/router/`, register in `HttpControlServer.buildRoutes()`, implement logic in `GestureDispatcher` / `UiTreeService` / a new strategy.
6. **Update the playbook**: `src/tools/playbook.tools.ts` has the static markdown agents read at session start. Reflect the new tool map, decision tree, or anti-pattern immediately.
7. **Update this doc**: add/modify the entry in the tool catalog. Keep file map in sync.
8. **Do not render resourceId in path-like short form**. Keep quoted key=value format.
9. **Do not filter by `clickable`**. The flag is unreliable on Flutter / Compose / RN.
10. **Do not add consecutive-call counters**. Use structural geometric blocks (`coordInIme`, role checks) instead.
11. `npm run build && npm test` — all tests must pass (39+ currently).

## Selector semantic mapping (cross-platform)

The `Selector` interface fields have identical names on all platforms but are mapped to different native query attributes by each adapter. When picking a field for a new test or tool, use the semantic name — the adapter handles the rest.

| Semantic field | Android native                   | iOS native (planned)                          |
| -------------- | -------------------------------- | --------------------------------------------- |
| `resourceId`   | `viewIdResourceName`             | `accessibilityIdentifier`                     |
| `contentDesc`  | `contentDescription`             | `accessibilityLabel`                          |
| `text`         | `text`                           | `value` (StaticText) / `label` (Button)       |
| `textContains` | text substring                   | `label CONTAINS[cd] "X"` (NSPredicate)        |
| `hint`         | fuzzy any-field                  | fuzzy any-field                               |
| `predicate`    | *(ignored)*                      | raw NSPredicate string                        |
| `classChain`   | *(ignored)*                      | raw XCUITest class chain                      |
| `nth`          | 0-based index when multiple      | 0-based index when multiple                   |

`predicate` and `classChain` are iOS-only escape hatches. Android adapter silently ignores them. The `preflight()` helper returns platform-keyed rebind hints so the agent gets the right recovery path regardless of which platform they're on.

## Invariants (do not violate)

- **Tool count is 19**. If you add one, justify and update this doc and the playbook. If you remove one, confirm no other tool broke and update case studies.
- **Selector priority is resourceId > contentDesc > text > textContains > hint**. Change this only if you have cross-platform data showing a different order works better.
- **Tree render uses quoted key=value form with inline `@cx,cy`**. Short forms like `#foo` / `@foo` are forbidden because agents misparse delimiters.
- **`input_text` and `tap` accept both selector and `{x,y}`**. Do not split into separate tools.
- **Business rules live in `src/tools/core/` strategy classes**, not inline in tool handlers. When adding logic, extract to a strategy class even if it means one extra file.
- **Class-based tools extend `Tool<TShape>`** with constructor-injected strategies. Do not instantiate strategies inside `execute()`.
- **`MUTATING_TOOLS` must be kept in sync** with actual mutating tools — `server.ts` uses it to record actions and invalidate the UI cache.
- **`clickable` flag is not a reliability signal on Flutter / Compose / RN**. Do not reintroduce clickable filters in `dumpCompact`, `renderCompactLine`, or resolver strategies.
- **`ResourceIdStrategy` keeps its walk fallback**. Removing it re-breaks Flutter non-qualified ids.
- **`UiTreeService.dumpCompact` keeps elements with any stable signal** (resourceId / contentDesc / text / clickable). Narrowing the filter re-breaks Flutter-only elements.
- **`DeviceController` port uses platform-neutral names**: `appId` not `packageName`, `currentForeground()` not `currentActivity()`. Adapters translate at the boundary.
- **iOS-specific selector fields (`predicate`, `classChain`) are additive**. Android adapter ignores them. Do not remove them because "Android doesn't use them".

## Background: why the surface was consolidated

Before: ~40 tools with overlapping intents (`input_text` + `fill_input_at_coordinates` + `type_via_keyboard` + `clear_focused_input` — four ways to type). Agents picked the wrong one based on test-case wording, cascaded into manual tap-per-key loops, burned ~90k tokens for a 3-step login test.

After: 19 tools, one per intent. Same test case runs in ~5 tool calls. See case studies `.adet/case-studies/2026-04.md` for the full journey.
