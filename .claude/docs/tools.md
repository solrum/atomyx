# Atomyx tool reference

> Read before touching anything under `packages/core-driver-mcp/src/tools/`.
> Fast path to knowing which tool lives where, what the response shape
> is, and which invariants must hold. Updated on every tool change.

## Overview

Atomyx exposes its functionality through three parallel tool surfaces,
all wrapping the same in-process `Orchestra` command layer from
`@atomyx/core-driver`:

- **MCP tools** (`packages/core-driver-mcp/src/tools/`) — JSON tool
  descriptors consumed by AI agents over stdio. The primary surface.
- **CLI commands** (`packages/core-driver-cli/src/commands/`) — a
  thin argv wrapper that constructs Driver + Orchestra + MCP server
  and connects to a stdio transport.
- **HTTP routes** (planned, not yet shipped) — REST endpoints that
  wrap the same Orchestra methods for consumers that cannot link
  in-process.

All three call the same `Orchestra` methods in-process. There is no
HTTP hop between MCP and Orchestra; there is no translation layer
between CLI and MCP. Each transport is a sibling.

## MCP tool surface (9 tools)

Every tool is defined via `defineTool` from
`packages/core-driver-mcp/src/tool-definition.ts`. The shape:

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
in `@atomyx/core-driver`.

| Tool | File | Calls | Returns |
|---|---|---|---|
| `launch_app` | `launch-app.tool.ts` | `orchestra.launchApp` | `{ ok, appId }` |
| `get_ui_tree` | `get-ui-tree.tool.ts` | `orchestra.hierarchy` + flatten | `{ total, returned, truncated, nodes[] }` |
| `find_element` | `find-element.tool.ts` | `orchestra.findOne` | element details + center, or `{ found: false }` |
| `tap` | `tap.tool.ts` | `orchestra.tap` (selector) or `orchestra.tapAt` (coords) | `ActionResult` |
| `input_text` | `input-text.tool.ts` | `orchestra.inputText` (selector) or coord path | `ActionResult` |
| `swipe` | `swipe.tool.ts` | `orchestra.swipeDirection` or `orchestra.swipeAt` | `{ ok, ... }` |
| `press_key` | `press-key.tool.ts` | `orchestra.pressKey` | `ActionResult` (iOS may return `ok:false`) |
| `screenshot` | `screenshot.tool.ts` | `orchestra.screenshot` | `{ base64, format, sizeBytes }` |
| `wait_for_element` | `wait-for-element.tool.ts` | `orchestra.waitFor` with timeout | element details or `{ found: false, reason }` |

Tools are registered in `packages/core-driver-mcp/src/tools/index.ts`
via `DEFAULT_TOOLS`. The list is **explicit**, not auto-discovered —
adding a file to `tools/` requires editing `index.ts` to ship it.
This prevents accidental surface growth.

## Shared schemas

- `packages/core-driver-mcp/src/selector-schema.ts` — Zod schema for
  the `Selector` input + `compileSelectorInput` helper that turns
  string/regex patterns into runtime `Selector` objects for the
  `Orchestra` API.
- `packages/core-driver-mcp/src/zod-to-json-schema.ts` — minimal
  hand-rolled converter (object/string/number/boolean/array/union/
  literal/nested) used to publish tool `inputSchema` to MCP clients.
  Kept hand-rolled to avoid the ~30 KB `zod-to-json-schema` npm dep.
- `packages/core-driver-mcp/src/tool-definition.ts` — the
  `ToolDefinition<TArgs, TResult>` contract + `defineTool` helper
  with Zod type inference. `ToolContext` exposes `orchestra` and
  `logger` — nothing else. Tools cannot reach around and call a
  Driver method directly (type system blocks it).

## Composition inside the MCP server

`createMcpServer` in `packages/core-driver-mcp/src/server.ts` is the
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

1. Create `packages/core-driver-mcp/src/tools/<name>.tool.ts`:
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
2. Register it in `packages/core-driver-mcp/src/tools/index.ts`:
   ```ts
   import { myTool } from "./my-tool.tool.js";
   export const DEFAULT_TOOLS = [
     ...existing,
     myTool as unknown as AnyToolDefinition,
   ];
   ```
3. Add a test to `packages/core-driver-mcp/src/server.test.ts` that
   dispatches through the MCP request handler and asserts the
   expected `MockDriver` calls.

**Before adding a tool**, verify an existing tool cannot absorb
the intent via a parameter. The 9-tool surface was picked to
eliminate overlap — growing it is a last resort.

## Orchestra method surface (the underlying contract)

Tools are wrappers around `Orchestra` methods in
`packages/core-driver/src/orchestra/orchestra.ts`:

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
  longPress(selector, durationMs?)
  inputText(selector, text, {clearFirst?})

Coordinate primitives (bypass pipeline):
  tapAt(point)
  longPressAt(point, durationMs?)
  swipeAt(from, to, durationMs?)
  swipeDirection("up"|"down"|"left"|"right", {durationMs?, fraction?})

Input + keyboard:
  pressKey(key)         → ActionResult (iOS can fail honestly)
  typeText(text)        → void, no focus tap
  eraseText(count)      → throws if driver lacks canEraseText

App lifecycle:
  launchApp / stopApp / killApp

Media + state:
  screenshot()           → Uint8Array
  waitForIdle(timeoutMs) → native driver primitive or host fallback
```

Every tool either calls one of these directly or composes a small
sequence of them. Complex behavior lives in the Orchestra pipeline
(not in tools) so all three transports (MCP, CLI, future HTTP)
inherit it for free.

## Invariants

- **Tools never reach the Driver directly.** Bypasses the Orchestra
  pipeline (scroll, obscurement, priority broadening). Type system
  blocks this: `ToolContext` has no `driver` field.
- **Tool `execute` is pure orchestration.** If you're writing logic
  inside a tool, move it to `@atomyx/core-driver` as a method on
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
`packages/core-driver/src/selectors/priority-broadening.ts`. When a
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

## Legacy `src/tools/` (being phased out)

The pre-refactor runtime in `src/tools/` still exists and still ships
19 tools via the legacy `src/server.ts` entry point. It coexists with
the new `@atomyx/core-driver-mcp` during the strangler-fig
transition. Tools-in-legacy are NOT actively developed — new tool work
goes into `packages/core-driver-mcp/src/tools/`.

Deletion of `src/` is planned after `@atomyx/core-driver-cli` has been
validated against real-device sessions. Legacy tools that have no
equivalent in the new 9-tool surface yet:

- `tap_and_wait_transition` — transition verification after tap
- `report_bug` — bug reporting with screenshot
- `start_run` / `finish_run` — run lifecycle
- `add_case_study` / `get_case_studies` — playbook
- `get_playbook` — static playbook content
- `list_apps` / `list_devices` / `select_device` — device management

These will land in `packages/core-driver-mcp/src/tools/` incrementally
as real usage demands them. They are not missing on purpose; they're
deferred until the new framework proves out end-to-end with the core
9, then pulled in one by one with updated implementations that go
through Orchestra rather than the legacy DeviceController port.
