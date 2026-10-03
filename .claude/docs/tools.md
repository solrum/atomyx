| The server stopped | `cancelled` | `cancelled` |
| A later `start_run` found the run open | `error` | `failed` |
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
`storage.RunStore`, the run session, or `storage.Storage` for run and bug records, or
`runner.ScriptRunner` for `run_script`. No handler reaches around
that layer to call a `Driver` method directly. `ToolContext` (in
`core/internal/mcp/tool_context.go`) has no `Driver` field, so the
type system enforces it.

## Tool reference

| Tool | File | Key inputs | Returns | Calls |
|---|---|---|---|---|
| **Device and app lifecycle** | | | | |
| `list_devices` | `core/internal/mcp/list_devices_tool.go` | `platform` (`ios`, `android`, `all`) | `{count, devices[], hint?}` | `device.Probe.Scan` |
| `select_device` | `core/internal/mcp/select_device_tool.go` | `platform`, `id`, `kind` (accepted and ignored), `port` | `{ok, active}` or `{ok:false, reason}` | `DeviceSession.Select`, which resolves the descriptor through `device.ResolveDescriptor`. A server started with `--platform` and `--device` carries `ServerDeps.Pinned`, `Select` fails any other device with `DEVICE_PINNED`, and `Ensure` selects the pinned device on first use |
| `disconnect_device` | `core/internal/mcp/select_device_tool.go` | none | `{ok:true, wasActive?}` | `DeviceSession.Current` and `DeviceSession.Disconnect` |
| `check_health` | `core/internal/mcp/check_health_tool.go` | none | `HealthReport` (selected, healthy, state, code, remedy), never takes or probes a released, borrowed or lost device | `DeviceSession.CheckHealth` |
| `list_apps` | `core/internal/mcp/list_apps_tool.go` | none | `{count, apps[], note?}` | `Orchestra.ListApps` |
| `launch_app` | `core/internal/mcp/launch_app_tool.go` | `appId` | `{ok, appId}` | `Orchestra.LaunchApp` |
| **Screen** | | | | |
| `get_ui_tree` | `core/internal/mcp/get_ui_tree_tool.go` | `limit`, `includeSystemOverlays` | `{total, returned, truncated, nodes[]}` | `Orchestra.Hierarchy` |
| `find_element` | `core/internal/mcp/find_element_tool.go` | a `Selector` (id, label, text, value, hint, role, enabled, clickable, focused, nth) | element details plus center, or `{found:false}` | `Orchestra.FindOne` |
| `screenshot` | `core/internal/mcp/screenshot_tool.go` | none | `{path, sizeBytes, format}`, and writes the image into the open run's `artifacts` directory, or under `<root>/screenshots` when no run is open | `Orchestra.Screenshot`, then `runSession.SaveScreenshot` |
| **Actions** | | | | |
| `tap` | `core/internal/mcp/tap_tool.go` | `selector`, or `x`/`y` | `ActionResult`, or `{ok, detail}` for the coordinate path | `Orchestra.Tap` (selector) or `Orchestra.TapAt` (coordinates) |
| `tap_and_wait_transition` | `core/internal/mcp/tap_and_wait_transition_tool.go` | `selector`, `waitForAbsent`, `waitForAppear`, `timeoutMs`, `maxTimeoutMs`, `intervalMs`, `loadingKeywords` | `ActionResult` plus transition diagnostics, classifying a timeout as still-loading, dialog-shown, overlay-unknown, partial-transition, or no-change | `Orchestra.Tap` then `Orchestra.WaitForTransition` |
| `input_text` | `core/internal/mcp/input_text_tool.go` | `selector` or `x`/`y`, `text`, `clearFirst` | `ActionResult`, or `{ok, detail}` for the coordinate path | `Orchestra.InputText` (selector), and `Orchestra.TapAt` then `Orchestra.EraseText` then `Orchestra.TypeText` (coordinates) |
| `swipe` | `core/internal/mcp/swipe_tool.go` | `direction` and `fraction`, or `fromX`/`fromY`/`toX`/`toY`, plus `durationMs` | `{ok, direction}` or `{ok, detail}` | `Orchestra.SwipeDirection` (direction) or `Orchestra.SwipeAt` (coordinates) |
| `press_key` | `core/internal/mcp/press_key_tool.go` | `key` | `KeyResult` (may report `ok:false` with a fallback hint) | `Orchestra.PressKey` |
| **Wait** | | | | |
| `wait_for_element` | `core/internal/mcp/wait_for_element_tool.go` | `selector`, `timeoutMs`, `pollIntervalMs` | element details on success, or `{found:false, reason, elapsedMs, polls}` on timeout | `Orchestra.WaitFor` |
| **Run lifecycle and reporting** | | | | |
| `start_run` | `core/internal/mcp/run_lifecycle_tool.go` | `name`, `source` | `{ok, id, name, source, startedAt, erroredPredecessor?}` | `runSession.Start`, which creates the run folder and opens the in-memory run |
| `finish_run` | `core/internal/mcp/run_lifecycle_tool.go` | `status` (`passed`, `failed`, `error`), `summary` | the finished run record | `runSession.Finish`, which finalizes the run folder |
| `report_bug` | `core/internal/mcp/report_bug_tool.go` | `title`, `description`, `captureScreenshot` | `{ok, id, runId, screenshotPath?}` | `storage.RunStore.RecordBug`, plus `Orchestra.Screenshot` when requested |
| **Run and bug queries** | | | | |
| `list_runs` | `core/internal/mcp/run_read_tool.go` | `status`, `limit`, `offset` | paginated run summaries, each with `result` and `outcome` (absent while running), plus `totalMatching` and `skipped` | `storage.RunCatalog.List` |
| `get_run` | `core/internal/mcp/run_read_tool.go` | `id` | the full run record, with `result` and `outcome` (absent while running) | `storage.RunCatalog.Get` |
| `update_run_summary` | `core/internal/mcp/run_read_tool.go` | `id`, `summary` | `{ok}`, `RUN_READ_ONLY` for a run with no summary field, or `RUN_BUSY` for a run a writer still holds open, or while a run migration holds the run store | `storage.RunCatalog.SetSummary` |
| `delete_run` | `core/internal/mcp/run_read_tool.go` | `id` | `{ok}`, or `RUN_BUSY` for a run a writer still holds open, which is left untouched, or `RUN_BUSY` while a run migration holds the run store | `storage.RunCatalog.Delete` |
| `list_bugs` | `core/internal/mcp/bug_read_tool.go` | `runId`, `limit`, `offset` | paginated bug summaries, newest first | `storage.Storage.List` |
| `get_bug` | `core/internal/mcp/bug_read_tool.go` | `id` | the full bug record | `storage.Storage.Load` |
| `delete_bug` | `core/internal/mcp/bug_read_tool.go` | `id` | `{ok}`, and removes the bug record and its screenshot sub-record | `storage.Storage.Delete` |
| **Guidance** | | | | |
| `add_case_study` | `core/internal/mcp/case_study_tool.go` | `title`, `trigger`, `solution`, `example` | `{ok}` | `storage.Storage.Save` |
| `get_case_studies` | `core/internal/mcp/case_study_tool.go` | `month`, `limit`, `offset` | paginated case-study bodies | `storage.Storage.List` then `storage.Storage.Load` |
| **Script execution** | | | | |
| `run_script` | `core/internal/mcp/run_script_tool.go` | `script` (YAML), `env` | structured per-step results: `{scriptName, ok, steps[], totalSteps, passedSteps, failedAtStep?, durationMs, deviceLost?}`, and each step carries its own `deviceLost?` | `script.ParseScript` then `runSession.RunScript`, which runs `runner.ScriptRunner.Run` inside the open run, or inside a run of its own finalized when the script ends. The handler passes the runner a `runner.NewFailureBundle` over the target's evidence writer, which writes `script-NNN-step-SSS-failure`, `-tree.json`, and `-log.jsonl` into the run's `artifacts` directory for a failed step. `NNN` is drawn from the run's shared artifact counter on the first write of each script execution |

Run verdicts in `list_runs` and `get_run` depend on liveness. A run
with no completion time and a live writer has status `running` and no
`result` or `outcome`. A run with no completion time and no live writer
has status `cancelled`, result `cancelled`, and outcome `interrupted`.
`NewServer` requires `ServerDeps.RunStarter`, the run folder creator, and
returns an error when it is missing or names no root. The starter's
`Root()` is the only root value the server holds: the run catalog and
the loose screenshots directory derive from it, so they cannot disagree.
`cmd/atomyx-mcp` builds the starter over the storage root, and the
catalog reads liveness from `runfolder.LockLiveness` unless a test sets
`ServerDeps.RunLiveness`. The lock covers the writers
of this process and of every other one, so a run held by a CLI run lists
as running, and `delete_run` refuses it with `RUN_BUSY`. A
`list_runs` call also reports `skipped`, the count of stored runs that
could not be read, which `totalMatching` excludes. `update_run_summary`
edits runs that carry a summary field, which are the runs written by
`start_run` and `run_script` and runs stored as flat records. A run written
by the CLI has none, and a run whose writer is another live process is
not editable until it finishes and returns `RUN_BUSY`. A run with no
summary field returns `RUN_READ_ONLY`.

`delete_run` and `update_run_summary` hold the run migration lock in
shared form for the length of the call, covering both the flat record
and the run folder, so edits of different runs never block each other.
While `atomyx doctor --migrate-runs` holds the lock exclusively, both
tools refuse without waiting and return `RUN_BUSY` with a message that
names the migration. The refusal changes nothing, so the caller repeats
the call once the migration ends. This is what keeps a migration from
losing a summary edit or bringing back a deleted run.

`start_run` creates a run folder of kind `mcp` through `runfolder.Starter`
at the storage root. The folder id is labeled with the selected
device's name, then its id, and with `mcp` when no device is selected.
`finish_run` finalizes the folder, and the status the caller gives maps
to an outcome as follows:

| `finish_run` status | Outcome | Result |
|---|---|---|
| `passed` (the default) | `passed` | `passed` |
| `failed` | `failed` | `failed` |
| `error` | `error` | `failed` |

A run the server closes itself takes these outcomes, and each
finalization releases the run folder lock:

| Cause | Outcome | Result |
|---|---|---|
| The device was lost | `deviceLost` | `cancelled` |
| The open-run idle timeout elapsed | `cancelled` | `cancelled` |
| A later `start_run` found the run open | `error` | `failed` |

The idle timeout is `cancelled` and not `interrupted` because the
server closes that run deliberately and the next call on it reports
`RUN_EXPIRED`. `interrupted` is what a run reads as when its writer
vanished without recording an end.

Every way of ending the open run (`finish_run`, a superseding `start_run`,
the idle timeout, device loss, server stop) first cancels the scripts
`run_script` is executing inside it and waits for them to return, so
`runSession` never finalizes a run under a writing script. `finish_run`
waits as long as its call allows and then returns an error with the run
still open. A server-initiated close waits at most 5 seconds. A
`finish_run` whose run folder cannot be finalized returns a tool error,
and the run reads as interrupted. When several scripts run in one run,
the meta `totalSteps` and `failedAtStep` come from the script that ended
last. A summary the agent set is kept by every server-initiated close.

Tools are registered in `core/internal/mcp/server.go`'s `NewServer`
in a fixed, explicit order grouped by concern (matching the grouping
above). Registration is never auto-discovered, so adding a file under
`core/internal/mcp/` requires also adding its `register*Tool` call in
`NewServer` to ship it.

## Shared building blocks

- **`ToolContext`** (`core/internal/mcp/tool_context.go`): the
  dependency bundle every handler closes over: `Session`
  (`*DeviceSession`), `Traits`, `Logger`, `Storage`, `Runs`, `RunSession`, `RunStore`, and `Clock`. A
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
  `list_devices` and `disconnect_device` register this way, and so does
  `select_device`, which settles the holder state itself when it
  installs the new session.
  A call that is not observe-only also enters the activity gate
  through `DeviceSession.EnterCall` for its whole length. Several calls
  may be inside at once, and a fresh capture of the held session
  (`session.Controller.FreshTree`) is refused with `HOLDER_BUSY` while
  any call is inside, so it never runs between two device operations of
  one call. A call that arrives during a capture waits for it inside its
  own tool budget. When the budget elapses first the call fails with
  `TOOL_TIMEOUT`, and when the caller cancels first the call reports
  the context cancellation from its own error, never `HOLDER_BUSY`.
  The middleware also appends the notices raised during a call
  (`core/internal/mcp/call_notices.go`) as extra text content blocks
  after the tool's own content. `DeviceSession.Ensure` raises one when a
  re-acquire relaunched the app, so no handler formats it.
  The same middleware drains `DeviceSession.Interventions` after every
  call, observe-only tools included, and appends the entries as one
  extra text block of the JSON shape
  `{"interventions":[{"kind","atMs","client"}]}`
  (`core/internal/mcp/call_interventions.go`), only when new entries
  exist. Observe-only tools carry them because draining takes no device
  and is not session activity, and the agent should learn about a viewer
  at its next call of any kind. A call that timed out leaves the entries
  pending. `DeviceSession` keeps the entries of a session it gives up
  (idle release, lost lease, select of another device, disconnect) until
  the next drain, so a re-acquire loses none.
- **Tool budget** (`core/internal/mcp/tool_budget.go`): the context a
  call runs under. It is clock-driven and can be paused.
  `pauseToolBudget(ctx)` stops the budget for the rest of a device
  acquisition and restarts it in full afterwards. Never count device
  acquisition against a tool budget: `DeviceSession.Select` and
  `DeviceSession.Ensure` pause it, then run `session.Prepare` under the
  runner build timeout (`session.Timeouts.RunnerBuildMs`, a fixed 300
  seconds that is not configurable, code `RUNNER_BUILD_TIMEOUT`), then
  `session.Open` within the acquisition timeout
  (`session.deviceAcquireTimeoutMs`, 120 seconds by default, runner
  build excluded, code `TOOL_TIMEOUT`). The worst case is the two
  phases added together, which excludes the identity lookup that runs
  first and the wait for a transition already in flight.
  `session.Prepare` takes no lease, so waiting on another process's
  build never spends the acquisition budget.
- **Acquisition progress** (`core/internal/mcp/device_progress.go`): the
  tool call middleware puts a phase reporter on the call context when
  the request carries a progress token. `DeviceSession` reports
  preparing, launching and connected through it, connected only after
  the session is installed, as MCP progress notifications, so a handler never emits progress itself. A call with
  no token reports nothing.
- **Run session** (`core/internal/mcp/run_session.go`): owns the open
  run and its run folder. The run tools call it to start, finish, and
  edit runs, and the `DeviceSession` uses it to see the open run and to
  close it when the open-run idle timeout or a lost lease ends the
  session's ownership.

## Composition inside the MCP server

`NewServer` in `core/internal/mcp/server.go` wires a `DeviceSession`
from the supplied `device.Probe` and `device.Factory`, builds one
shared `ToolContext`, registers the timeout-and-validation
middleware, then calls every `register*Tool` function in the fixed
order shown in the table above. `Serve` (same file) runs the
constructed server over stdio until the context is done or the
client disconnects.

`DeviceSession` is a holder state machine over `session.Prepare` and
`session.Open`. Every transition of the held session happens under one
lock, and `select_device` opens the new device before it releases the
current one, so a failed selection keeps the working device. The states
are unselected, idle, running, released, borrowed and lost.

| From | Event | To |
|---|---|---|
| unselected, released, lost | `select_device` succeeds | running, then idle at the end of the call |
| idle | a tool call starts | running |
| running | the last in-flight call ends with no run open | idle, MCP idle timer starts |
| running | the last in-flight call ends with a run open | running, open-run idle timer starts |
| idle | MCP idle timeout (10 minutes by default) | released |
| running with a run open | open-run idle timeout (10 minutes by default) | released, the run is closed as cancelled |
| idle | another process sends `control.yield` and the session accepts, which needs no call or capture in flight and no run open | borrowed, the session records `controlBorrowed`, closes without stopping the runner, and watches the new holder |
| borrowed | the new holder ends, or none took the device within `session.FollowBudgetMs` | released |
| idle, running | the lease is lost, or the iOS runner or its tunnel exits on its own | lost, running scripts are cancelled with the loss as cause, and an open run is closed with outcome `deviceLost` |
| released, borrowed, lost | a tool that needs a device runs | running again, or the call fails with `DEVICE_RELEASED_HELD` when another process holds the device |
| unselected, released, borrowed, lost | `select_device` names a device held by a process that is idle and can yield | running, then idle at the end of the call, after the holder yielded through `session.Borrow` with the borrower `atomyx-mcp select_device`. A busy holder keeps the device and the call fails with `DEVICE_HELD` |
| any | `disconnect_device` | unselected, the remembered device is forgotten |

Notes on the shared-device states:

- `check_health` reports `borrowed` while another process holds the
  device the session gave up, and `released` after it. The `released`
  message names its cause, an idle timeout or a hand-over to another
  process that no longer holds the device. Neither state is probed.
- `DEVICE_RELEASED_HELD` names the holder, for example `atomyx run,
  running`, and appears in two cases. A call that needs the device while
  the state is `borrowed` fails with it without trying. A call that takes
  a released device again fails with it when another process holds the
  device, and that re-acquire never asks the holder to yield.
- `select_device` is the only tool that borrows. It names the borrower
  `atomyx-mcp select_device` to the holder.
- A yield that the session accepts adds one `controlBorrowed` intervention,
  with the borrower's command as the client, to the next result of any tool.
  The borrower records nothing.

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
- **Every tool result passes through one masking pass.** The
  tool-call middleware (`core/internal/mcp/tool_call_middleware.go`)
  runs `maskSecureResult` (`core/internal/mcp/secure_result.go`) on
  each result, and it is the only mechanism that masks tool output. An
  element object that reports `secure: true` has its `text` and `value`
  replaced by `orchestra.SecureMask`, and its label, hint, id and role
  stay. The pass keeps the layout of the document and never escapes
  markup. A tool that returns element or tree data therefore sets
  `secure` through `secureFlag`, and never masks in its own handler.
  `TestEverySecureElementIsMaskedInEveryToolResult` calls every
  registered tool against a secure fixture, so a new tool must be added
  to its table. A projection built inside `orchestra` for output, such
  as `TreeNodeToCompactElements`, carries `Secure` and leaves the secret
  field out of any selector it builds, and selector resolution always
  reads the real tree.
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
