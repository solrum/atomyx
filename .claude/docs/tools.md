# Atomyx tool reference

> Read before touching anything under `packages/mcp/src/tools/`.
> Fast path to knowing which tool lives where, what the response shape
> is, and which invariants must hold. Updated on every tool change.

## Overview

Atomyx exposes its functionality through three parallel tool surfaces,
all wrapping the same in-process `Orchestra` command layer from
`@atomyx/driver`:

- **MCP tools** (`packages/mcp/src/tools/`) — JSON tool
  descriptors consumed by AI agents over stdio. The primary surface.
- **CLI commands** (`packages/cli/src/driver/commands/`) — a
  thin argv wrapper that constructs Driver + Orchestra + MCP server
  and connects to a stdio transport.
- **HTTP routes** (planned, not yet shipped) — REST endpoints that
  wrap the same Orchestra methods for consumers that cannot link
  in-process.

All three call the same `Orchestra` methods in-process. There is no
HTTP hop between MCP and Orchestra; there is no translation layer
between CLI and MCP. Each transport is a sibling.

## MCP tool surface (27 tools)

Every tool is defined via `defineTool` from
`packages/mcp/src/tool-definition.ts`. The shape:

```ts
defineTool({
  name: "tap",
  description: "Tap an element ...",
  inputSchema: ZodSchema,
  async execute(args, ctx) {
    return ctx.orchestra.tap(compileSelectorInput(args.selector));
  },
});
```

Each tool is a thin wrapper that: validates args (Zod), calls an
`Orchestra` method, returns the result. No business logic in tools.
All orchestration lives in `Orchestra` / `Finder` / `ScrollController`
in `@atomyx/driver`.

| Tool | File | Calls | Returns |
|---|---|---|---|
| **Device + app lifecycle** | | | |
| `list_devices` | `list-devices.tool.ts` | device discovery (adb, xcrun, idevice_id) | `{ devices[] }` |
| `select_device` | `select-device.tool.ts` | `DeviceSession.select` | `{ ok, deviceId, platform }` |
| `disconnect_device` | `select-device.tool.ts` | `DeviceSession.disconnect` | `{ ok }` |
| `list_apps` | `list-apps.tool.ts` | `orchestra.listApps` | `{ count, apps[] }` |
| `launch_app` | `launch-app.tool.ts` | `orchestra.launchApp` | `{ ok, appId }` |
| **Screen** | | | |
| `get_ui_tree` | `get-ui-tree.tool.ts` | `orchestra.hierarchy` + flatten | `{ total, returned, truncated, nodes[] }` |
| `find_element` | `find-element.tool.ts` | `orchestra.findOne` | element details + center, or `{ found: false }` |
| `screenshot` | `screenshot.tool.ts` | `orchestra.screenshot` | `{ base64, format, sizeBytes }` |
| **Actions** | | | |
| `tap` | `tap.tool.ts` | `orchestra.tap` (selector) or `orchestra.tapAt` (coords) | `ActionResult` |
| `tap_and_wait_transition` | `tap-and-wait-transition.tool.ts` | `orchestra.tap` + transition diagnostics | `ActionResult` + transition info |
| `input_text` | `input-text.tool.ts` | `orchestra.inputText` (selector) or coord path | `ActionResult` |
| `swipe` | `swipe.tool.ts` | `orchestra.swipeDirection` or `orchestra.swipeAt` | `{ ok, ... }` |
| `press_key` | `press-key.tool.ts` | `orchestra.pressKey` | `ActionResult` (iOS may return `ok:false`) |
| **Wait** | | | |
| `wait_for_element` | `wait-for-element.tool.ts` | `orchestra.waitFor` with timeout | element details or `{ found: false, reason }` |
| **Run lifecycle + reporting** | | | |
| `start_run` | `run-lifecycle.tool.ts` | `RunStore.startRun` | `{ runId, startedAt }` |
| `finish_run` | `run-lifecycle.tool.ts` | `RunStore.finishRun` + storage | `{ runId, summary }` |
| `report_bug` | `report-bug.tool.ts` | `RunStore.reportBug` + storage | `{ bugId }` |
| **Run + bug queries** | | | |
| `list_runs` | `run-read.tool.ts` | `RunStore` read | `{ runs[] }` |
| `get_run` | `run-read.tool.ts` | `RunStore` read | run details |
| `update_run_summary` | `run-read.tool.ts` | `RunStore.updateSummary` | `{ ok }` |
| `delete_run` | `run-read.tool.ts` | `RunStore.deleteRun` | `{ ok }` |
| `list_bugs` | `bug-read.tool.ts` | `RunStore` read | `{ bugs[] }` |
| `get_bug` | `bug-read.tool.ts` | `RunStore` read | bug details |
| `delete_bug` | `bug-read.tool.ts` | `RunStore.deleteBug` | `{ ok }` |
| **Guidance** | | | |
| `add_case_study` | `case-study.tool.ts` | storage write | `{ ok }` |
| `get_case_studies` | `case-study.tool.ts` | storage read | `{ studies[] }` |

Tools are registered in `packages/mcp/src/tools/index.ts`
via `DEFAULT_TOOLS`. The list is **explicit**, not auto-discovered —
adding a file to `tools/` requires editing `index.ts` to ship it.
This prevents accidental surface growth.

## Shared schemas

- `packages/mcp/src/selector-schema.ts` — Zod schema for
  the `Selector` input + `compileSelectorInput` helper that turns
  string/regex patterns into runtime `Selector` objects for the
  `Orchestra` API.
- `packages/mcp/src/zod-to-json-schema.ts` — minimal
  hand-rolled converter (object/string/number/boolean/array/union/
  literal/nested) used to publish tool `inputSchema` to MCP clients.
  Kept hand-rolled to avoid the ~30 KB `zod-to-json-schema` npm dep.
- `packages/mcp/src/tool-definition.ts` — the
  `ToolDefinition<TArgs, TResult>` contract + `defineTool` helper
  with Zod type inference. `ToolContext` exposes `orchestra` and
  `logger` — nothing else. Tools cannot reach around and call a
  Driver method directly (type system blocks it).

## Composition inside the MCP server

`createMcpServer` in `packages/mcp/src/server.ts` is the
factory that wires everything:

```ts
export function createMcpServer(opts: McpServerOptions): Server {
  const tools = opts.tools ?? DEFAULT_TOOLS;
  const ctx: ToolContext = {
    orchestra: opts.orchestra,
    logger: opts.logger ?? new NoopLogger(),
  };
  const server = new Server(opts.serverInfo ?? { name: "atomyx", ... }, ...);
  server.setRequestHandler(ListToolsRequestSchema, ...);
  server.setRequestHandler(CallToolRequestSchema, ...);
  return server;
}
```

Contract guarantees:
1. **Orchestra is the only mutator in `ToolContext`.** No tool can
   call Driver methods directly — the type system enforces it.
2. **Tool surface is pluggable.** Pass `tools: [...custom]` to ship
   a different surface than `DEFAULT_TOOLS`. Studio might ship
   diagnostic/replay tools the MCP default omits.
3. **No global state.** Each `createMcpServer` call returns a fresh
   server. Parallel instances work out of the box (used by tests).
4. **Errors are structured.** Tool handler throws become
   `{isError:true, content:[{type:"text", text:msg}]}` MCP
   responses — never a server crash.

## Adding a new tool

1. Create `packages/mcp/src/tools/<name>.tool.ts`:
   ```ts
   import { z } from "zod";
   import { defineTool } from "../tool-definition.js";

   const MyArgs = z.object({ ... }).strict();

   export const myTool = defineTool({
     name: "my_tool",
     description: "One-sentence description the agent uses to pick it.",
     inputSchema: MyArgs,
     async execute(args, ctx) {
       // Only calls on ctx.orchestra allowed
       return ctx.orchestra.someMethod(args);
     },
   });
   ```
2. Register it in `packages/mcp/src/tools/index.ts`:
   ```ts
   import { myTool } from "./my-tool.tool.js";
   export const DEFAULT_TOOLS = [
     ...existing,
     myTool as unknown as AnyToolDefinition,
   ];
   ```
3. Add a test to `packages/mcp/src/server.test.ts` that
   dispatches through the MCP request handler and asserts the
   expected `MockDriver` calls.

**Before adding a tool**, verify an existing tool cannot absorb
the intent via a parameter. The 27-tool surface is deliberately
small — growing it is a last resort.

## Orchestra method surface (the underlying contract)

Tools are wrappers around `Orchestra` methods in
`packages/driver/src/orchestra/orchestra.ts`:

```
Tree inspection
  hierarchy()                          → current TreeNode
  find(selector)                       → all matches, sync
  findOne(selector)                    → first or null, sync
  waitFor(selector, {timeoutMs})       → polling wait, throws on timeout

Selector actions (full pipeline: compile → scroll-into-view →
obscurement check → coordinate primitive. Return ActionResult,
never throw on action-level failure):
  tap(selector)
    — Keyboard-gate: when obscurement flags a keyboard on top of
      the target AND a prior inputText set `maybeKeyboardOpen`,
      dispatches `driver.hideKeyboard()` + `waitForKeyboard(false)`
      and retries the tap once.
  longPress(selector, durationMs?)
  inputText(selector, text, {clearFirst?})
    — `tap → (conditional erase) → inputText`. Erase is skipped
      when the resolved field is observably empty (pre-tap cursor).
      Driver adapters self-synchronize on focus + keyboard, so no
      Orchestra-level focus wait is issued.

Coordinate primitives (bypass pipeline):
  tapAt(point)
  longPressAt(point, durationMs?)
  swipeAt(from, to, durationMs?)
  swipeDirection("up"|"down"|"left"|"right", {durationMs?, fraction?})

Input + keyboard:
  pressKey(key)         → ActionResult (some platforms can't
                          honor every key; hint explains fallback)
  typeText(text)        → void, no focus tap
  eraseText(count)      → throws if driver lacks canEraseText
  hideKeyboard()        → ActionResult, ok=false when
                          canHideKeyboard=false

App lifecycle:
  launchApp / stopApp / killApp

Media + state:
  screenshot()           → Uint8Array
  waitForIdle(timeoutMs) → native driver primitive or host fallback
```

Observable state + wait primitives (free functions, used by
Orchestra and exported for callers who need fine-grained control):

```
packages/driver/src/state/
  findFocusedNode(tree)         → TreeCursor | null
  findKeyboardNode(tree)        → TreeCursor | null
  readKeyboardState(tree)       → { visible, bounds? }

packages/driver/src/waits/
  waitUntil(fetch, predicate, opts)     — generic poll
  waitForFocus(…)                       — selector becomes focused
  waitForText(…)                        — field text satisfies
  waitForInputReady(…)                  — focus OR keyboard visible
  waitForInputCommitted(…)              — role-aware "typed" check
  waitForKeyboard(…)                    — keyboard visible/hidden
  waitForTreeStable(…)                  — no tree changes for quietMs
```

Driver port capabilities relevant to the observable paths above:

- `canEraseText` — driver supports `eraseText`.
- `canHideKeyboard` — driver supports `hideKeyboard`. Orchestra
  skips the keyboard-gate when false.
- `canWaitForIdle` — native idle primitive. Host falls back to
  `waitForTreeStable` when false.

The `TreeNode.focused` field and the `ext:isIme` attribute key are
driver-adapter outputs consumed by `state/`. Adapters set them
from their platform's focus / IME signals; cross-platform code
reads them via the state helpers rather than by poking raw
attributes.

Every tool either calls one of these directly or composes a small
sequence of them. Complex behavior lives in the Orchestra pipeline
(not in tools) so all three transports (MCP, CLI, future HTTP)
inherit it for free.

## Invariants

- **Tools never reach the Driver directly.** Bypasses the Orchestra
  pipeline (scroll, obscurement, priority broadening). Type system
  blocks this: `ToolContext` has no `driver` field.
- **Tool `execute` is pure orchestration.** If you're writing logic
  inside a tool, move it to `@atomyx/driver` as a method on
  Orchestra, Finder, or a new core helper.
- **Tool descriptions are for the agent, not humans.** They must be
  crisp and action-oriented — "Tap an element matching a selector",
  not "This tool can be used to tap on elements". The LLM chooses
  between tools based on the description alone.
- **Input schema is Zod, not JSON Schema.** The
  `zodToJsonSchema` helper generates the JSON Schema the MCP
  protocol expects at server-boot time.

## Selector priority broadening

Selector resolution lives in
`packages/driver/src/selectors/priority-broadening.ts`. When a
selector has multiple content fields, `compileSelector` tries them
in this order:

```
id > label > text > value > hint
```

`role`, `enabled`, `clickable`, `focused` are AND-ed into every
candidate filter as constraints (not priority fallbacks). Behavior:

- Agent passes `{ text: "Login", role: "button" }` → compile to
  `intersect(textMatches("Login"), roleIs("button"))`. Only matches
  elements that satisfy both.
- Agent passes `{ id: "login_btn", text: "Login" }` → try
  `idMatches("login_btn")` first. If any element matches, return
  it. Only fall through to `textMatches` when id match is empty.
- Agent passes just `{ text: "Login" }` → `textMatches("Login")`.

Priority broadening is the reason agents don't need to know whether
Android exposes a label via `contentDesc` or iOS via `label` — the
core maps both to the canonical `AttrKeys.Label` at the driver
boundary, and any content field the agent supplies is tried in
priority order.

## Prompts are a separate surface

The 27 tools above are for agent-driven **actions**. Methodology
guidance (playbooks, exploratory heuristics) is exposed via the MCP
`prompts/` capability in `packages/mcp/src/prompts/`
(`atomyx/playbook`, `atomyx/exploratory`, etc), not as tools. Adding
a methodology shortcut goes into `prompts/`, not into `tools/`.
