# Atomyx: MCP tool reference

## Purpose

Who reads this: a contributor or AI agent about to add, change, or
call a tool in `core/internal/mcp`, or an agent that needs to know
what a tool returns before calling it.

When to read it: before editing anything under `core/internal/mcp`,
and before choosing which tool to call for a given intent.

What this does not cover: why the tool surface is shaped as an
adapter over `Orchestra` rather than over the `Driver` port directly
(see [`architecture.md`](./architecture.md)), or how to add a new
tool step by step (see [`development.md`](./development.md)).

## TL;DR

Every tool is registered in `core/internal/mcp/server.go`'s
`NewServer`. A tool handler validates its arguments against a JSON
Schema, then calls exactly one layer beneath it: `Orchestra` for
device actions, `DeviceSession` for connection state,
`storage.RunStore` or `storage.Storage` for run and bug records, or
`runner.ScriptRunner` for `run_script`. No handler reaches around
that layer to call a `Driver` method directly. `ToolContext` (in
`core/internal/mcp/tool_context.go`) has no `Driver` field, so the
type system enforces it.

## Tool reference

| Tool | File | Key inputs | Returns | Calls |
|---|---|---|---|---|
| **Device and app lifecycle** | | | | |
| `list_devices` | `core/internal/mcp/list_devices_tool.go` | `platform` (`ios`, `android`, `all`) | `{count, devices[], hint?}` | `device.Probe.Scan` |
| `select_device` | `core/internal/mcp/select_device_tool.go` | `platform`, `id`, `kind` (accepted and ignored), `port` | `{ok, active}` or `{ok:false, reason}` | `DeviceSession.Select`, which resolves the descriptor through `device.ResolveDescriptor` |
| `disconnect_device` | `core/internal/mcp/select_device_tool.go` | none | `{ok:true, wasActive?}` | `DeviceSession.Current` and `DeviceSession.Disconnect` |
| `check_health` | `core/internal/mcp/check_health_tool.go` | none | `HealthReport` (selected, healthy, state, code, remedy) | `DeviceSession.CheckHealth` |
| `list_apps` | `core/internal/mcp/list_apps_tool.go` | none | `{count, apps[], note?}` | `Orchestra.ListApps` |
| `launch_app` | `core/internal/mcp/launch_app_tool.go` | `appId` | `{ok, appId}` | `Orchestra.LaunchApp` |
| **Screen** | | | | |
| `get_ui_tree` | `core/internal/mcp/get_ui_tree_tool.go` | `limit`, `includeSystemOverlays` | `{total, returned, truncated, nodes[]}` | `Orchestra.Hierarchy` |
| `find_element` | `core/internal/mcp/find_element_tool.go` | a `Selector` (id, label, text, value, hint, role, enabled, clickable, focused, nth) | element details plus center, or `{found:false}` | `Orchestra.FindOne` |
| `screenshot` | `core/internal/mcp/screenshot_tool.go` | none | `{path, sizeBytes, format}`, and writes the image under `.atomyx/screenshots/` | `Orchestra.Screenshot` |
| **Actions** | | | | |
| `tap` | `core/internal/mcp/tap_tool.go` | `selector`, or `x`/`y` | `ActionResult`, or `{ok, detail}` for the coordinate path | `Orchestra.Tap` (selector) or `Orchestra.TapAt` (coordinates) |
| `tap_and_wait_transition` | `core/internal/mcp/tap_and_wait_transition_tool.go` | `selector`, `waitForAbsent`, `waitForAppear`, `timeoutMs`, `maxTimeoutMs`, `intervalMs`, `loadingKeywords` | `ActionResult` plus transition diagnostics, classifying a timeout as still-loading, dialog-shown, overlay-unknown, partial-transition, or no-change | `Orchestra.Tap` then `Orchestra.WaitForTransition` |
| `input_text` | `core/internal/mcp/input_text_tool.go` | `selector` or `x`/`y`, `text`, `clearFirst` | `ActionResult`, or `{ok, detail}` for the coordinate path | `Orchestra.InputText` (selector), and `Orchestra.TapAt` then `Orchestra.EraseText` then `Orchestra.TypeText` (coordinates) |
| `swipe` | `core/internal/mcp/swipe_tool.go` | `direction` and `fraction`, or `fromX`/`fromY`/`toX`/`toY`, plus `durationMs` | `{ok, direction}` or `{ok, detail}` | `Orchestra.SwipeDirection` (direction) or `Orchestra.SwipeAt` (coordinates) |
| `press_key` | `core/internal/mcp/press_key_tool.go` | `key` | `KeyResult` (may report `ok:false` with a fallback hint) | `Orchestra.PressKey` |
| **Wait** | | | | |
| `wait_for_element` | `core/internal/mcp/wait_for_element_tool.go` | `selector`, `timeoutMs`, `pollIntervalMs` | element details on success, or `{found:false, reason, elapsedMs, polls}` on timeout | `Orchestra.WaitFor` |
| **Run lifecycle and reporting** | | | | |
| `start_run` | `core/internal/mcp/run_lifecycle_tool.go` | `name`, `source` | `{ok, id, name, source, startedAt, erroredPredecessor?}` | `storage.RunStore.Start` |
| `finish_run` | `core/internal/mcp/run_lifecycle_tool.go` | `status` (`passed`, `failed`, `error`), `summary` | the finished run record | `storage.RunStore.Finish` |
| `report_bug` | `core/internal/mcp/report_bug_tool.go` | `title`, `description`, `captureScreenshot` | `{ok, id, runId, screenshotPath?}` | `storage.RunStore.RecordBug`, plus `Orchestra.Screenshot` when requested |
| **Run and bug queries** | | | | |
| `list_runs` | `core/internal/mcp/run_read_tool.go` | `status`, `limit`, `offset` | paginated run summaries | `storage.Storage.List` |
| `get_run` | `core/internal/mcp/run_read_tool.go` | `id` | the full run record | `storage.Storage.Load` |
| `update_run_summary` | `core/internal/mcp/run_read_tool.go` | `id`, `summary` | `{ok}` | `storage.Storage.Load` then `storage.Storage.Save` |
| `delete_run` | `core/internal/mcp/run_read_tool.go` | `id` | `{ok}` | `storage.Storage.Delete` |
| `list_bugs` | `core/internal/mcp/bug_read_tool.go` | `runId`, `limit`, `offset` | paginated bug summaries, newest first | `storage.Storage.List` |
| `get_bug` | `core/internal/mcp/bug_read_tool.go` | `id` | the full bug record | `storage.Storage.Load` |
| `delete_bug` | `core/internal/mcp/bug_read_tool.go` | `id` | `{ok}`, and removes the bug record and its screenshot sub-record | `storage.Storage.Delete` |
| **Guidance** | | | | |
| `add_case_study` | `core/internal/mcp/case_study_tool.go` | `title`, `trigger`, `solution`, `example` | `{ok}` | `storage.Storage.Save` |
| `get_case_studies` | `core/internal/mcp/case_study_tool.go` | `month`, `limit`, `offset` | paginated case-study bodies | `storage.Storage.List` then `storage.Storage.Load` |
| **Script execution** | | | | |
| `run_script` | `core/internal/mcp/run_script_tool.go` | `script` (YAML), `env` | structured per-step results: `{scriptName, ok, steps[], totalSteps, passedSteps, failedAtStep?, durationMs, deviceLost?}`, and each step carries its own `deviceLost?` | `script.ParseScript` then `runner.ScriptRunner.Run` |

Tools are registered in `core/internal/mcp/server.go`'s `NewServer`
in a fixed, explicit order grouped by concern (matching the grouping
above). Registration is never auto-discovered, so adding a file under
`core/internal/mcp/` requires also adding its `register*Tool` call in
`NewServer` to ship it.

## Shared building blocks

- **`ToolContext`** (`core/internal/mcp/tool_context.go`): the
  dependency bundle every handler closes over: `Session`
  (`*DeviceSession`), `Logger`, `Storage`, `RunStore`, and `Clock`. A
  handler reads only the fields its own logic needs. There is no
  `Driver` field, so a handler cannot bypass `Orchestra`.
- **`orchestraOrFail`** (`core/internal/mcp/tool_context.go`):
  resolves the currently selected device's `Orchestra` through
  `DeviceSession.Ensure`, which takes a device the session released
  back again, or returns an actionable "no active device" error naming
  `select_device` as the fix. Tools where "no device yet" is itself a meaningful outcome
  (`report_bug`, `disconnect_device`) read
  `tc.Session.Current()` directly instead.
- **Input schema helpers** (`core/internal/mcp/schema_helpers.go`):
  the `js*` functions (`jsObject`, `jsString`, `jsStringDesc`,
  `jsNumber`, `jsBoolean`, `jsArray`, `jsAnyOf`, `jsConst`,
  `jsEmptyObject`, `jsStringRecord`) build a draft-07 JSON Schema
  literal. `selectorInputSchema()` is the shared `Selector` shape
  every selector-accepting tool embeds.
- **Validation** (`core/internal/mcp/validation.go`):
  `compileToolSchema` compiles a tool's input schema once at server
  construction. `validateToolArguments` checks a call's raw arguments
  against the compiled schema before the handler runs. A malformed
  schema panics at startup rather than failing a later call, because
  it is a programming error, not a runtime condition.
- **Middleware** (`core/internal/mcp/tool_call_middleware.go`):
  registered once in `NewServer` via `AddReceivingMiddleware`, ahead
  of every `AddTool` call. It applies a per-call timeout (the
  `ATOMYX_TOOL_TIMEOUT_MS` environment variable, or a call's own
  `timeoutMs`/`maxTimeoutMs` plus a fixed buffer, or a built-in
  default) uniformly to every registered tool, so no handler
  implements its own top-level timeout.
  It also reports every call start and end to the `DeviceSession`,
  which drives the idle timers and refuses a call on an expired run
  with `RUN_EXPIRED`. A tool registered through `addObserveOnlyTool`
  (`core/internal/mcp/tool_traits.go`) is not reported, so it neither
  marks the device running nor restarts the idle timer. `check_health`,
  `list_devices` and `disconnect_device` register this way.
  The middleware also appends the notices raised during a call
  (`core/internal/mcp/call_notices.go`) as extra text content blocks
  after the tool's own content. `DeviceSession.Ensure` raises one when a
  re-acquire relaunched the app, so no handler formats it.
- **Tool budget** (`core/internal/mcp/tool_budget.go`): the context a
  call runs under. It is clock-driven and can be paused.
  `pauseToolBudget(ctx)` stops the budget for the rest of a device
  acquisition and restarts it in full afterwards. Never count device
  acquisition against a tool budget: `DeviceSession.Select` and
  `DeviceSession.Ensure` pause it, then run `session.Prepare` under the
  runner build timeout (300 seconds, code `RUNNER_BUILD_TIMEOUT`), then
  `session.Open` within the acquisition timeout
  (`session.deviceAcquireTimeoutMs`, 120 seconds by default, runner
  build excluded, code `TOOL_TIMEOUT`). The worst case is the two
  phases added together. `session.Prepare` takes no lease, so waiting
  on another process's build never spends the acquisition budget.
- **Acquisition progress** (`core/internal/mcp/device_progress.go`): the
  tool call middleware puts a phase reporter on the call context when
  the request carries a progress token. `DeviceSession` reports
  preparing, launching and connected through it, connected only after the session is installed, as MCP progress
  notifications, so a handler never emits progress itself. A call with
  no token reports nothing.
- **Run tracker** (`core/internal/mcp/run_tracker.go`): lets the
  `DeviceSession` see the open run and close it when the open-run
  idle timeout or a lost lease ends the session's ownership.

## Composition inside the MCP server

`NewServer` in `core/internal/mcp/server.go` wires a `DeviceSession`
from the supplied `device.Probe` and `device.Factory`, builds one
shared `ToolContext`, registers the timeout-and-validation
middleware, then calls every `register*Tool` function in the fixed
order shown in the table above. `Serve` (same file) runs the
constructed server over stdio until the context is done or the
client disconnects.

`DeviceSession` is a holder state machine over `session.Open`. The
states are unselected, idle, running, released and lost.

| From | Event | To |
|---|---|---|
| unselected, released, lost | `select_device` succeeds | running, then idle at the end of the call |
| idle | a tool call starts | running |
| running | the last in-flight call ends with no run open | idle, MCP idle timer starts |
| running | the last in-flight call ends with a run open | running, open-run idle timer starts |
| idle | MCP idle timeout (10 minutes by default) | released |
| running with a run open | open-run idle timeout (10 minutes by default) | released, the run is closed as cancelled |
| idle, running | the lease watcher reports the lease lost | lost, an open run is closed with status error |
| released, lost | a tool that needs a device runs | running again, or the call fails with `DEVICE_RELEASED_HELD` when another process holds the device |
| any | `disconnect_device` | unselected, the remembered device is forgotten |

Timers and the acquisition budget run on the injectable timer clock of
`ServerDeps`, so tests drive them with `clock.ManualClock`. After the
open-run idle timeout closes a run, `finish_run` and `report_bug` fail
with `RUN_EXPIRED` until `start_run` runs, and the first tool that
needs a device fails once with `RUN_EXPIRED`.

Contract guarantees this composition holds:

1. **`Orchestra` is the only device mutator reachable from a tool.**
   The type system enforces this: `ToolContext` carries no `Driver`
   field.
2. **No global state.** Each `NewServer` call returns an independent
   server with its own `DeviceSession` and `ToolContext`, so parallel
   instances (used by tests) do not interfere.
3. **Errors are structured.** A tool handler's error return becomes
   an `isError:true` MCP result, not a server crash.

## Orchestra method surface (the underlying contract)

Every tool above is a thin wrapper around one or more `Orchestra`
methods, defined in `core/internal/orchestra/orchestra.go`:

```
Tree inspection
  Hierarchy(ctx)                         current TreeNode
  FindOne(ctx, selector)                 first match or nil
  WaitFor(ctx, selector, opts)           blocks until match or timeout

Selector actions (full pipeline: compile the selector, scroll it into
view, check obscurement, then dispatch the coordinate primitive.
Returns an ActionResult; does not throw on an action-level failure):
  Tap(ctx, selector, opts)
  LongPress(ctx, selector, durationMs)
  InputText(ctx, selector, text, opts)

Coordinate primitives (bypass the selector pipeline):
  TapAt(ctx, point)
  LongPressAt(ctx, point, durationMs)
  SwipeAt(ctx, from, to, durationMs)
  SwipeDirection(ctx, direction, opts)

Input and keyboard:
  PressKey(ctx, key)      KeyResult; some platforms cannot honor
                          every key and report a fallback hint
  TypeText(ctx, text)
  EraseText(ctx, count)
  HideKeyboard(ctx)

App lifecycle:
  LaunchApp(ctx, appId, args)
  ListApps(ctx)

Media and transitions:
  Screenshot(ctx)                        raw image bytes
  WaitForTransition(ctx, params)          used by tap_and_wait_transition
```

Selector resolution itself lives in
`core/internal/orchestra/priority_broadening.go`'s `CompileSelector`;
see [`architecture.md`](./architecture.md) for the priority order it
applies.

## Invariants

- **Tools never reach the Driver directly.** Doing so would bypass
  the `Orchestra` pipeline (scroll, obscurement, priority
  broadening). The type system blocks it: `ToolContext` has no
  `Driver` field.
- **A tool's `execute` closure is pure orchestration.** Logic beyond
  argument decoding and result shaping belongs on `Orchestra`, a
  `runner` command, or a new helper in the layer beneath `mcp`, not
  inline in a tool file.
- **Tool descriptions are written for the calling agent, not for a
  human reader.** They state what the tool does and when to prefer it
  over an alternative, because an agent chooses between tools based
  on the description text alone.
- **Input schemas are JSON Schema literals**, built with the `js*`
  helpers and validated with a draft-07 JSON Schema validator
  dependency before a handler ever runs.

## Adding a new tool

See the "Adding a new MCP tool" checklist in
[`development.md`](./development.md). Before creating a file, confirm
no existing tool in the table above can absorb the new intent through
a parameter.
