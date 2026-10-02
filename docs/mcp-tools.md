# Atomyx MCP tools

## Purpose

This doc is for people and AI agents driving a mobile app through the
`atomyx-mcp` server: connecting a client, choosing the right tool for
a step, and reading each tool's inputs and outputs.

Read it when you are about to call an Atomyx MCP tool and need to
know what it expects or returns.

It does not cover:

- Script syntax for `run_script`. See `yml-script-reference.md`.
- The `atomyx` command-line tool. See `cli.md`.
- Contributor-facing internals of the tool layer. See
  `../.claude/docs/tools.md`.

## Connect a client

Build the server binary from the repo root:

```
make build-core
```

Then point your MCP client at the built binary. For a client that
reads `.mcp.json`:

```json
{
  "mcpServers": {
    "atomyx": {
      "type": "stdio",
      "command": "/absolute/path/to/atomyx/core/bin/atomyx-mcp",
      "args": []
    }
  }
}
```

A device agent must already be running on the target device before
any tool can drive it. See `device-setup.md` to install and start one,
and `getting-started.md` for the first end-to-end walkthrough.

## Tools at a glance

| Tool | Group | What it does |
|---|---|---|
| [list_devices](#list_devices) | Session and device | Lists connected devices across supported platforms so the caller can pick a target |
| [select_device](#select_device) | Session and device | Binds the session to a device so later tools operate on it |
| [disconnect_device](#disconnect_device) | Session and device | Disconnects the active device and leaves the session idle |
| [check_health](#check_health) | Session and device | Reports whether a device is bound and answering, without changing app state |
| [list_apps](#list_apps) | Apps and inspection | Lists apps installed on the current device with identifiers and labels |
| [launch_app](#launch_app) | Apps and inspection | Launches an app by its identifier and brings it to the foreground |
| [get_ui_tree](#get_ui_tree) | Apps and inspection | Captures the current UI hierarchy as a flat list of elements with bounds and state |
| [find_element](#find_element) | Apps and inspection | Finds a single element matching a selector and returns its bounds and tap center |
| [screenshot](#screenshot) | Apps and inspection | Saves a screenshot to disk for cases the UI hierarchy cannot answer |
| [tap](#tap) | Interaction and waiting | Taps an element by selector or by raw coordinates |
| [tap_and_wait_transition](#tap_and_wait_transition) | Interaction and waiting | Taps and verifies the resulting screen transition in one call |
| [input_text](#input_text) | Interaction and waiting | Types text into a field by selector or by coordinates |
| [swipe](#swipe) | Interaction and waiting | Swipes the screen in a direction or between two points |
| [press_key](#press_key) | Interaction and waiting | Presses a system or navigation key such as back or home |
| [wait_for_element](#wait_for_element) | Interaction and waiting | Polls the UI hierarchy until a selector matches or the timeout expires |
| [run_script](#run_script) | Script execution | Executes a YAML test script step by step and returns pass or fail per step |
| [start_run](#start_run) | Run tracking | Starts a named test run so later findings attach to it |
| [finish_run](#finish_run) | Run tracking | Finishes the active run with a verdict and optional summary |
| [list_runs](#list_runs) | Run tracking | Lists persisted run summaries with filtering and pagination |
| [get_run](#get_run) | Run tracking | Fetches the full record for one persisted run |
| [update_run_summary](#update_run_summary) | Run tracking | Replaces the summary field on a persisted run |
| [delete_run](#delete_run) | Run tracking | Deletes a persisted run record |
| [report_bug](#report_bug) | Bugs and case studies | Records a bug finding in the active run, optionally with a screenshot |
| [list_bugs](#list_bugs) | Bugs and case studies | Lists persisted bug report summaries with filtering and pagination |
| [get_bug](#get_bug) | Bugs and case studies | Fetches the full record for one persisted bug report |
| [delete_bug](#delete_bug) | Bugs and case studies | Deletes a persisted bug report and its screenshot |
| [add_case_study](#add_case_study) | Bugs and case studies | Appends a learned lesson to the case-studies playbook |
| [get_case_studies](#get_case_studies) | Bugs and case studies | Lists and reads case studies from the playbook |

## Typical agent workflow

1. Call `list_devices` to see what is connected.
2. Call `select_device` to bind the session to the target device.
3. Call `launch_app` to bring the app under test to the foreground.
4. Call `get_ui_tree` to see the current screen, its elements, and
   their coordinates.
5. Act on the screen with `tap`, `input_text`, or `swipe`.
6. Verify the result with `wait_for_element` or `find_element`.
7. Record findings across the session: open a `start_run` before
   driving the device, call `report_bug` when something breaks, then
   close with `finish_run`.

## Selectors and coordinates

Every tool that targets an element (`tap`, `input_text`,
`find_element`, `wait_for_element`, `tap_and_wait_transition`) accepts
a selector object instead of a bare string. A selector combines match
fields with optional state constraints:

- Match fields: `id`, `text`, `label`, `hint`, `value`. Each accepts
  either a literal string or a `{regex, flags}` object.
- Constraint fields: `role`, `enabled`, `clickable`, `focused`, `nth`.
  These narrow a match instead of matching text on their own.

The server tries the match fields in a fixed priority order (id, then
label, then text, then value, then hint) so the caller does not need
to know which field a given platform exposes best. Pass whichever
fields you have and let the server pick the strongest one.

A literal string in a match field requires an exact match: the
element's entire attribute value must equal the string exactly, with
matching case and no allowance for surrounding or embedded
whitespace. There is no substring or trimmed match. Use a
`{regex, flags}` object instead when you need a partial match, an
anchored prefix, or a case-insensitive comparison. `flags` accepts
any combination of `i` (case-insensitive), `m` (multiline anchors),
and `s` (dot matches newline).

Flutter Semantics often merges a visible title and a subtitle into
one label, so an element showing "Products" with a subtitle below it
can carry the combined label "Products\nBrowse our catalog", newline
included. An exact match on "Products" does not match that element.
The working alternative is a regex that matches the prefix:

```json
{ "label": { "regex": "^Products" } }
```

`find_element` takes the selector fields at the top level of its
arguments, while `tap`, `input_text`, and `wait_for_element` take them
under a `selector` key. A selector takes `id`, `text`, `label`, `hint`,
`value`, `role`, `enabled`, `clickable`, `focused` and `nth`. One that
sets no content field, no role and no true state flag is rejected with an `INVALID_ARGUMENT` error
instead of matching the whole screen, because `nth` only picks among
matches. Keys a selector does not take are ignored, so a misspelled or
misplaced key leaves it empty.

When no stable selector exists for an element, pass coordinates as
`x` and `y` to `tap` or `input_text` instead of a selector. Take them
from:

- the `center` point that `find_element` or `wait_for_element` returns
- the middle of the `bounds` string (`left,top,right,bottom`) that
  `get_ui_tree` reports for each node

## Session and device

### list_devices
Enumerate devices reachable from the host across every supported platform.

**Use when:** discovering which devices are available before choosing one. Call select_device next to bind the session to one of the results.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| platform | string, one of "ios", "android", "all" | No | all platforms (no filtering) | Restrict results to one platform, or return devices from every platform |

**Behavior**
- Scans the host fresh on every call. It does not read from or write to the session, so it never changes which device, if any, is currently selected.
- A platform whose toolchain is not installed on the host is skipped silently rather than reported as an error, so an empty or partial list can mean "not installed" as well as "nothing connected."
- Devices are returned in the order the underlying scan produces them, with no guaranteed sort order.

**Returns**
- count: integer, the number of devices in the list.
- devices: array of objects, each with platform, id, name, state, and kind. State is one of "online", "offline", "unauthorized", or "unavailable". Kind is one of "simulator", "emulator", or "device".
- hint: string, present only when count is 0, suggesting that the host toolchain or a running device or simulator should be checked.

**Errors and recovery**
- Passing a platform value outside "ios", "android", or "all" fails argument validation and returns an error naming the tool. Correct the value and retry.
- An empty result is not an error. Confirm the target platform's toolchain is installed and a device or simulator is actually running, then call list_devices again.

**Example**
```json
{ "platform": "android" }
```

### select_device
Bind the session to one device so every other device-driving tool operates on it.

**Use when:** starting a session against a specific device, or switching to a different device mid-session. Call disconnect_device instead when the goal is only to release the current binding without picking a new one.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| platform | string, one of "ios", "android" | Yes | none | The platform the device belongs to |
| id | string, non-empty | Yes | none | Device identifier, exactly as reported by list_devices |
| kind | string, one of "simulator", "device" | No | none | Accepted for compatibility and ignored. The device kind is resolved from the id, so the hint cannot change which device is bound |
| port | integer, positive | No | the platform's default control connection | Overrides the device's control connection port. Only honored on platforms whose driver exposes a configurable port, and ignored otherwise |

**Behavior**
- Calling select_device again, whether or not a device is already bound, resolves, prepares and opens the new device first, and only then releases the previously-bound device. A failed selection therefore leaves the previously-bound device bound and working. A failure disconnecting the previous device is logged but does not block the new selection.
  - The exception is a selection that reaches the hardware the session already holds, for example through a second serial of the same phone. Its ownership cannot be taken twice, so the held device is released first. If taking it again fails, the device stays remembered and the next tool call that needs a device takes it again.
- Every other device-driving tool reads the currently-bound device on each call, so a mid-session select_device call takes effect immediately for the very next tool call.
- The device kind is resolved from the id, the same way the command line resolves it. On iOS the id must name a simulator or physical device that list_devices currently reports, and the match is case-insensitive. An iOS id that is not listed fails with code DEVICE_NOT_FOUND before any connection is attempted. On Android the id is used as given.
- The returned kind reports the kind of the device actually bound, taken from the driver, not an echo of the kind argument the caller passed. It is always present in the response, even when the caller omitted kind. On Android it is "emulator" when the device id starts with "emulator-" and "device" otherwise. On iOS it is "simulator" or "device". The value "emulator" can appear in the response even though the kind argument only accepts "simulator" or "device" as input.
- connectedAt is a timestamp in milliseconds, recorded at the moment the new connection was established.
- Binding takes exclusive ownership of the device. Only one Atomyx process, whether the CLI or another MCP server, can drive a device at a time, and a second one fails with DEVICE_HELD.
- Acquiring the device does not count against the 30 second tool call budget. It runs in two phases, each with its own budget. The worst case is 300 seconds for the first phase and then 120 seconds for the second. These are the bounds of the two phases. They do not include the device identity lookup that runs before them, or the wait for another selection or re-acquire already in flight in the same server.
  - First the iOS runner is built when it is missing or stale. This also covers waiting for another Atomyx process that is building it, and no device ownership is held meanwhile. The build timeout is a fixed 300 seconds and is not configurable. When it elapses the call fails with code RUNNER_BUILD_TIMEOUT.
  - Then the device is acquired. Acquisition has its own budget of 120 seconds by default, which excludes the build. When it is exhausted the call fails with code TOOL_TIMEOUT. Configure it with `session.deviceAcquireTimeoutMs` in the project config or the `ATOMYX_DEVICE_ACQUIRE_TIMEOUT_MS` environment variable. The environment variable wins. The server reads the project config from its working directory, looking in each parent directory when it finds none there.
- When the call carries a progress token, the server sends three progress notifications as the device comes up, with progress 1, 2 and 3 of a total of 3. They announce in order that the device is being prepared, that the runner is launching, and that the device is connected. The connected notification is sent only after the device is installed in the session. Without a progress token no notifications are sent.
- A bound device is released automatically when the session sits idle. See Device ownership and idle release.

**Returns**
- On a successful bind: ok true, and active with platform, id, kind (see Behavior), and connectedAt.
- On a failed bind: ok false, reason (a human-readable failure message), and code (a stable machine-readable failure code). hint is present with an actionable next step when the failure carries one, and omitted otherwise. This is a normal (not isError) response, so a caller must check the ok field rather than relying on error handling alone.

**Errors and recovery**
- Missing platform or id, an id that is empty, a non-positive port, or a platform, kind, or id outside the accepted values all fail argument validation before a connection is attempted. Correct the arguments and retry.
- ok false with code DEVICE_NOT_FOUND means an iOS id is not among the reachable devices. Other reasons typically mean the id does not currently match a reachable device, or the device toolchain rejected the connection. Call list_devices again to confirm the id is still valid and the device is online, then retry.
- ok false with code DEVICE_HELD means another Atomyx process is driving the device. The reason names that process and its state. Wait for it to finish or ask the operator to stop it, then call select_device again.
- ok false with code TOOL_TIMEOUT means the device did not become ready within the acquisition budget. Check that the device is connected and unlocked, then retry.
- ok false with code AGENT_NOT_READY on an Android device can also mean the installed agent is too old to mark secret fields. select_device first reinstalls the agent when it can find the agent APK, so this error means no current APK was found or the agent is still out of date. Build the agent, or point ATOMYX_ANDROID_APK at a current APK, then call select_device again.
- ok false with code RUNNER_BUILD_TIMEOUT means building the iOS runner, or waiting for another process that is building it, did not finish within the build timeout. This is a different failure from TOOL_TIMEOUT. Wait for any other Atomyx process that is building the runner to finish, check that Xcode is installed and responsive, then call select_device again.

**Example**
```json
{ "platform": "ios", "id": "00008030-001A2D3E1401002E", "kind": "device" }
```

### Device ownership and idle release

A session owns its device for as long as it is selected and active, and gives it up in these cases.

- With no tool call in flight and no run open, the device is released after the MCP idle timeout, 10 minutes by default. Configure it with `session.mcpIdleTimeoutMs` or the `ATOMYX_MCP_IDLE_TIMEOUT_MS` environment variable.
- With a run open and no tool call in flight, the device is released after the open-run idle timeout, 10 minutes by default and configured with `session.openRunIdleTimeoutMs`. The open run is closed with status cancelled.
- A released device is remembered. The next tool call that needs a device takes it again without a new select_device call, and the call runs outside its tool budget like select_device does.
- On iOS, taking the device again starts a new driver, and a driver can query only an app it launched itself. So the app that was in front is launched again, which discards its unsaved in-memory state. Screens and data the app persists are unaffected.
- When taking the device again relaunched the app, the result of the call that triggered it carries one extra text content block after the tool's own content. It states that the app was relaunched on a new runner and that its in-memory state was lost. The tool's own content is unchanged, so a client that reads only the first content block sees the same result as before.
- If another process took the device in the meantime, the call fails with DEVICE_RELEASED_HELD. Retry shortly, and ask the operator to release the device if it keeps failing.
- After the open-run idle timeout closed a run, finish_run and report_bug fail with RUN_EXPIRED until start_run is called. The first tool that needs a device after the expiry also fails once with RUN_EXPIRED so the agent learns the run is gone. Start a new run and repeat the lost work.
- disconnect_device releases the device and forgets it, so nothing is taken again until select_device is called.
- If the device is lost while the session holds it, the session behaves as released. The device is lost when its lease is taken away, or when the runner process of an iOS device, or the USB tunnel to a physical iPhone, exits on its own. Every script running at that moment is cancelled, and the open run is closed with result `cancelled` and outcome `deviceLost`. A run_script call that is its own run is finalized as `deviceLost`, not `cancelled`. An Android device reports no such signal, so a lost Android agent shows up as a failed step that the liveness probe classifies as deviceLost.

### disconnect_device
Release the currently-bound device and leave the session idle.

**Use when:** deliberately ending device control without immediately picking another device, such as before finishing a session. To switch straight to a different device, call select_device directly instead, since it disconnects the previous device automatically.

**Parameters**
None

**Behavior**
- Idempotent. Calling it with no device currently bound still succeeds.
- After this call, every tool that needs a bound device fails until select_device is called again.
- Calling it is not session activity. It does not restart the idle timer or move a held idle device to running.
- When a selection or a re-acquire is in flight, the call waits for it only until the call's own budget ends. If it ends first, nothing changes and the result is ok false with the reason and code of the interruption. Call it again afterward.
- A failure tearing down the underlying connection is logged internally but never changes this tool's result. The session is always left idle and the call always reports success, whether or not the teardown underneath actually succeeded cleanly.

**Returns**
- ok: boolean, true once the session is left idle. False only when the call ended while it waited for a selection or a re-acquire in flight, with reason, code and, when present, hint beside it.
- wasActive: object with platform and id describing the device that was bound before this call, or null when no device was bound.

**Errors and recovery**
- ok false means the call ended before the device could be released, and the session is unchanged. Call disconnect_device again. If a subsequent tool reports that no device is bound, call select_device with a platform and id from list_devices.

**Example**
```json
{}
```

### check_health
Report whether a device is bound to the session and, if so, whether it currently answers a quick liveness check.

**Use when:** diagnosing a stalled session or an unresponsive device before retrying an action, instead of guessing from a timed-out call. Not for confirming what is currently on screen. Use get_ui_tree instead for that.

**Parameters**
None

**Behavior**
- Never runs a flow, never touches app state, and never changes what device is bound.
- Calling it is not session activity. It does not restart the idle timer or move a held idle device to running, so state reports idle for a held device that no other call is using, and polling it never keeps a device from its idle release. list_devices and disconnect_device behave the same way.
- A device released by an idle timeout, or lost, is not taken again and not probed. The report names the state instead, so the check never acquires a device or waits for a runner build.
- The liveness probe is bounded by a fixed five-second timeout, separate from and shorter than the overall per-call time budget.
- An unresponsive device is a normal, successful check outcome, not a tool failure. This tool only reports isError:true when the check itself could not be attempted at all.

**Returns**
- selected: boolean, whether the session has a device, held or remembered. Every other field carries its zero value except state when this is false.
- state: string, one of unselected, idle, running, released or lost. Released and lost mean the device is remembered but not held, and the next tool that needs a device takes it again.
- healthy: boolean, always present. True means the bound device answered the liveness probe in time. False with state released or lost means the device was not probed. Meaningless when selected is false.
- code: string, present only when a probe of a held device failed, a stable failure code.
- message: string, present only when healthy is false, a human-readable description of the failure or of the state.
- remedy: string, present only when healthy is false and the failure carries an actionable next step.

**Errors and recovery**
- selected false: call select_device with a platform and id from list_devices before retrying whatever action prompted the health check.
- selected true and healthy false: read code and remedy. An agent that could not be reached at all typically needs disconnect_device followed by select_device to reconnect. An agent that answered but reported itself unable to serve requests, for example because a required on-device service is disabled, instead needs that service re-enabled on the device before retrying; the message explains what to fix.

**Example**
```json
{}
```

## Apps and inspection

### list_apps
Lists every app installed on the currently selected device.

**Use when:** you need to confirm the exact app identifier before calling launch_app. Use get_ui_tree instead when you already know the app is running and want to inspect its screen.

**Parameters**

None.

**Behavior**
- Returns every installed app the device reports, with no filtering.
- Requires an active device. Call select_device first.

**Returns**
- `count`: number of apps returned.
- `apps`: a list of installed apps, each with an identifier and a display label.
- `note`: present only when the list is empty, explaining that the device may have no installed apps or that enumeration is unavailable on this host.

**Errors and recovery**
- No active device: select a device with select_device, then retry.

**Example**
```json
{}
```

### launch_app
Launches an app on the connected device by its app identifier and brings it to the foreground.

**Use when:** starting a test flow against a specific app. Use find_element or get_ui_tree afterward to confirm the app actually reached the screen you expect.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| appId | string | Yes | None | Cross-platform app identifier (bundle id or package name), as reported by list_apps |

**Behavior**
- Requires an active device. Call select_device first.
- Confirms the app is installed before launching it, using the same installed-app list list_apps reports.
- Within one flow (a script run or a chain of steps), calling launch_app again for an app identifier that this tool already launched is a no-op: the app is left running as is, rather than being restarted. This tool does not expose a way to force a fresh restart, so use a stop or reset action first if a clean relaunch is required.
- Subsequent tool calls (get_ui_tree, tap, find_element, and so on) operate on the launched app's foreground UI.

**Returns**
- `ok`: true on success.
- `appId`: the identifier that was launched, echoed back.

**Errors and recovery**
- No active device: select a device with select_device, then retry.
- App not installed on the target device: install the app on the device, then retry.

**Example**
```json
{ "appId": "com.example.app" }
```

### get_ui_tree
Captures the current UI hierarchy as a flat, depth-annotated list of elements.

**Use when:** this is the primary observation tool. Call it first after launch_app, after every navigation action such as tap, back, or swipe, and whenever you need to understand the current screen. Prefer it over screenshot because it is faster, cheaper, and gives you selectors you can pass directly to tap or input_text. Use find_element instead when you only need one specific element rather than the whole screen.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| limit | number | No | None (no limit) | Maximum number of elements to return, counted in the flattened, pre-order list |
| includeSystemOverlays | boolean | No | false | When true, includes known OEM system-overlay elements (such as an edge-panel trigger) that are otherwise pruned |

**Behavior**
- Requires an active device. Call select_device first.
- Returns elements as a flat list in pre-order (parent before its children), not a nested tree. Each entry carries a `depth` field giving its nesting level, so a caller can reconstruct structure from the sequence if needed.
- By default, elements belonging to a known OEM system overlay, and everything nested under them, are excluded from the list before it is counted or truncated. Set includeSystemOverlays to true to see them.
- When limit is set and smaller than the full element count, the list is cut off after that many elements (in the same pre-order sequence) and truncation is reported.
- Each element reports role, id, text, label, hint, value, bounds, and boolean interactive state (enabled, clickable, focused, selected, checked, visible) only for the fields the underlying platform actually supplied. Fields the platform does not report are omitted rather than sent as empty or false.
- Bounds are reported as a raw rectangle string in the form "left,top,right,bottom", in device pixels. This tool does not compute a center point for you. Use find_element or wait_for_element when you need a ready-to-tap center coordinate for a specific element.
- Platform- or framework-specific extension attributes, when present, are grouped under an `ext` map on the element rather than mixed into the common fields.
- An element that holds a secret, such as a password input, reports `secure: true`. Its `text` and `value` are replaced by the fixed mask `********`, whatever the field holds, so the secret never reaches the agent. Its `label`, `hint`, `id` and `role` stay readable, so the field is still identifiable. An empty secure field keeps its empty `text` and `value`. Selectors still match the real content of the field, but an agent cannot read it back.

**Returns**
- `total`: number of elements after overlay filtering, before any limit is applied.
- `returned`: number of elements actually included in `nodes`.
- `truncated`: true when limit cut the list short.
- `nodes`: the flat list of elements described above.

**Errors and recovery**
- No active device: select a device with select_device, then retry.
- When no app has been launched in this session, get_ui_tree can fail
  with AGENT_REJECTED and a snapshot failure message. Call launch_app
  first.

**Example**
```json
{ "limit": 200 }
```

### find_element
Finds a single element matching a selector and returns enough detail to act on it directly.

**Use when:** you need to verify one specific element exists, or you need its exact tap coordinates because a selector-based tap is failing. Use get_ui_tree instead when you need to see the whole screen rather than one element.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| id | string or pattern | No | None | Matches the element's id attribute. A pattern is either a plain string or an object with `regex` and optional `flags` |
| text | string or pattern | No | None | Matches the element's text attribute |
| label | string or pattern | No | None | Matches the element's accessibility label attribute |
| hint | string or pattern | No | None | Matches the element's hint attribute |
| value | string or pattern | No | None | Matches the element's value attribute |
| role | string | No | None | Constrains matches to a normalized semantic role, for example "button" |
| enabled | boolean | No | None | When true, constrains matches to enabled elements. False or omitted places no constraint |
| clickable | boolean | No | None | When true, constrains matches to clickable elements. False or omitted places no constraint |
| focused | boolean | No | None | When true, constrains matches to focused elements. False or omitted places no constraint |
| nth | number | No | None | Zero-based index selecting one match by position from the matched list, in document order |

**Behavior**
- Requires an active device. Call select_device first.
- role, enabled, clickable, and focused are constraints applied on top of whichever content field matches. They narrow the result but never substitute for a content field on their own.
- When more than one content field is given (id, text, label, hint, value), matching tries them in priority order: id, then label, then text, then value, then hint. The first field that produces at least one match wins, and the rest are not tried. As an exception, if text is given without label, a text query also tries matching the same pattern against the label attribute as a fallback, since some elements only carry an accessibility label.
- When no content field is given at all, every element in the hierarchy is a candidate, filtered only by role, enabled, clickable, or focused if given.
- Among the winning field's matches, clickable and focused elements are ranked above otherwise-identical matches. Elements with equal ranking keep their original document order. This ranking is skipped when nth is set, since nth is a positional pick, not a ranked one.
- When nth is set, it selects that position (zero-based, in document order) from the winning field's raw match list.
- Returns only the first match after ranking or after applying nth. It never returns a list of matches.

**Returns**
- `found`: true when a match was located, false otherwise.
- On a match: `id`, `text`, `label`, `value`, `role`, `enabled`, `clickable` for the fields the platform reported, plus `secure: true` for a field that holds a secret (its `text` and `value` are then the mask `********`, as in get_ui_tree), plus `bounds` (parsed rectangle) and `center` (the computed tap point), both null when the element has no parseable bounds.
- On no match: only `found: false` is returned.

**Errors and recovery**
- No active device: select a device with select_device, then retry.
- `found: false` is not an error. Re-check the selector against a fresh get_ui_tree call, broaden or correct the pattern, or confirm the screen has finished loading before retrying.

**Example**
```json
{ "label": "Sign in", "role": "button" }
```

### screenshot
Captures the active device's screen and saves it as an image file, returning the file path.

**Use when:** get_ui_tree or find_element cannot answer your question, for example a custom-rendered canvas, a UI framework that does not expose semantics, visual layout validation, or when you suspect the UI tree is missing elements. Always try get_ui_tree or find_element first, since they are faster, cheaper, and give you actionable selectors directly.

**Parameters**

None.

**Behavior**
- Requires an active device. Call select_device first.
- When a run is open, saves the image into that run's `artifacts` folder, next to the rest of the run's evidence. With no run open, saves it into the `screenshots` folder directly under the storage root, which is `~/.atomyx` unless the storage directory environment variable overrides it, so the location never depends on the server's working directory.
- The image is JPEG when the underlying capture is already JPEG, and PNG otherwise. The tool does not return image bytes inline. The caller reads the file at the returned path to view it.

**Returns**
- `path`: absolute file path to the saved image.
- `sizeBytes`: size of the saved file, in bytes.
- `format`: the image file's format, either "jpg" or "png".

**Errors and recovery**
- No active device: select a device with select_device, then retry.
- A file-system failure while saving surfaces as a tool error. Check that the storage root is writable and retry.

**Example**
```json
{}
```

## Interaction and waiting

These tools drive the device: tapping, typing, swiping, pressing
keys, and waiting for the screen to reach an expected state. Every
selector-based tool here compiles the selector with the same
priority-broadening rule: id first, then label, then text, then
value, then hint, with role, enabled, clickable, and focused applied
as constraints, and nth selecting among the ranked matches.

### tap
Taps an element found by selector, or a raw point given as
coordinates.

**Use when:** performing a tap with no expected screen change. Use
tap_and_wait_transition instead when the tap should trigger
navigation, a submit, or a login, and the caller needs to confirm the
result.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| selector | object | conditionally | none | Element selector (id, text, label, hint, value, role, enabled, clickable, focused, nth). Provide this, or provide x and y instead. |
| x | number | conditionally | none | Screen x coordinate to tap directly. Required together with y when selector is omitted. |
| y | number | conditionally | none | Screen y coordinate to tap directly. Required together with x when selector is omitted. |

**Behavior**
- Exactly one path runs per call: provide selector, or provide both x
  and y. The call fails argument validation if neither is provided.
- The selector path scrolls the matching element into view first,
  then checks it is not visually obscured by another element on
  screen before tapping its resolved center point.
- If the resolved element is obscured specifically by the on-screen
  keyboard, and a prior input_text call in this session opened the
  keyboard, tap dismisses the keyboard and retries once
  automatically before reporting a failure.
- The coordinate path dispatches the tap directly at the given point.
  It does not scroll and does not check obscurement.

**Returns**
- Selector path: `ok`, and on success `resolvedBy` (which selector
  field matched: id, label, text, value, or hint) and `detail`. On
  rejection, `ok` is false with `reason`, and `obscurer` (role, id,
  label) is present when the rejection was caused by an obscuring
  element.
- Coordinate path: `ok: true` and `detail` describing the tapped
  point. `resolvedBy` and `obscurer` are never present on this path.

**Errors and recovery**
- `ok: false` with a "not found" or "could not scroll element into
  view" reason: widen the selector, or confirm the element exists
  with get_ui_tree or find_element first.
- `ok: false` with an `obscurer`: dismiss the named obscuring element,
  or tap it directly, then retry the original tap.
- `ok: false` with a "no bounds attribute" reason: the element
  resolved but cannot be tapped by selector. Fall back to find_element
  to get its center coordinates and tap those instead.
- Missing both selector and x and y fails argument validation before
  any device interaction happens.
- On the selector path, when no app has been launched in this
  session, tap can fail with AGENT_REJECTED and a snapshot failure
  message instead of an `ok: false` result. Call launch_app first.

**Example**
```json
{ "selector": { "text": "Login" } }
```

### tap_and_wait_transition
Taps an element and waits for the resulting screen transition,
classifying why the transition never resolved if it times out.

**Use when:** the tap causes navigation, a submit, or a login, and
the caller needs confirmation the screen actually changed. Use tap
instead when no screen transition is expected.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| selector | object | yes | none | Element selector to tap. |
| waitForAbsent | object | conditionally | none | Selector expected to stop matching after the tap (the old screen's anchor). At least one of waitForAbsent or waitForAppear is required. A selector that sets no field is rejected before the tap. |
| waitForAppear | object | conditionally | none | Selector expected to start matching after the tap (the new screen's anchor). At least one of waitForAbsent or waitForAppear is required. A selector that sets no field is rejected before the tap. |
| timeoutMs | number | no | 10000 | Soft deadline in milliseconds. When a loading indicator or element motion is observed at this deadline, the wait extends once, up to maxTimeoutMs. |
| maxTimeoutMs | number | no | 60000 | Hard deadline in milliseconds. No extension exceeds this. |
| intervalMs | number | no | 300 | Delay between hierarchy polls, in milliseconds. |
| loadingKeywords | array of strings | no | none | Extra label substrings to recognize as a loading indicator, in addition to the built-in vocabulary. |

**Behavior**
- Captures the hierarchy and the tap target's own state before
  tapping, so a timeout can be diagnosed against the pre-tap screen
  rather than a fresh, unrelated snapshot.
- Taps selector through the same pipeline as tap (scroll-into-view,
  obscurement check, keyboard-dismiss retry). If the tap itself is
  rejected, the call returns immediately with `classification:
  "tap_failed"` and never starts waiting.
- An appeared element that holds a secret reports `secure: true`, and its `selector` carries its id or label, never its text, so the selector still resolves the field. The mask `********` never appears in a selector.
- After a successful tap, polls the hierarchy until waitForAbsent no
  longer matches and waitForAppear matches, whichever of the two was
  supplied.
- If timeoutMs elapses without resolving, but the current screen
  shows a loading indicator or element motion, the deadline extends
  once, up to maxTimeoutMs. A screen with neither signal gives up at
  timeoutMs.
- On timeout, classifies the cause into exactly one of: still_loading,
  still_loading_with_cancel, dialog_or_error_shown, overlay_unknown,
  partial_transition, or no_change_detected, each paired with a hint
  written for the next action to take.

**Returns**
- `ok: true`: `waitedMs`, and `resolvedBy` (which selector field
  resolved the tap target).
- `ok: false` from a failed tap: `reason`, `classification:
  "tap_failed"`, `hint`, and `tapResult` (the underlying tap's own
  result).
- `ok: false` from a wait timeout: `waitedMs`, `classification`,
  `hint`, `dialogLabels` (labels of dialog-like elements that
  appeared), `targetStateChanged` (why the tapped element's own state
  changed, if it did), `appeared` (up to 10 newly appeared elements,
  each with label, role, selector, and whether it looks like a
  dialog), `currentSnapshot` (a compact description of the current
  screen), and `overlayKind` when an overlay was detected.

**Errors and recovery**
- Missing both waitForAbsent and waitForAppear fails argument
  validation. Supply at least one anchor.
- `classification: "tap_failed"`: fix the selector or obscurement
  issue named in `tapResult` before retrying.
- `still_loading` or `still_loading_with_cancel`: the app is still
  processing. Wait longer or raise maxTimeoutMs rather than retapping.
- `dialog_or_error_shown`: read `dialogLabels` for the message and
  handle it (dismiss it or act on it) before retrying the original
  flow.
- `overlay_unknown`, `partial_transition`, or `no_change_detected`:
  inspect `currentSnapshot`, then re-orient with get_ui_tree rather
  than guessing at a cause. Do not call screenshot to diagnose a
  timeout. The returned fields already describe what is on screen.

**Example**
```json
{ "selector": { "text": "Log in" }, "waitForAppear": { "text": "Welcome" }, "timeoutMs": 8000 }
```

### input_text
Types text into an input field, focusing it first by selector or by
coordinates.

**Use when:** entering text into a field. Use tap instead when the
field only needs to be focused, with nothing typed into it.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| selector | object | conditionally | none | Selector for the field to type into. Provide this, or provide x and y instead. |
| x | number | conditionally | none | Screen x coordinate of the field. Required together with y when selector is omitted. |
| y | number | conditionally | none | Screen y coordinate of the field. Required together with x when selector is omitted. |
| text | string | yes | none | Text to type. |
| clearFirst | boolean | no | true | true erases the field's existing content before typing. False appends instead. |

**Behavior**
- Exactly one path runs per call: provide selector, or provide both x
  and y. The call fails argument validation if neither is provided.
- The selector path scrolls the field into view, checks it is not
  obscured, taps it to focus it, then waits for the field to actually
  report input focus before typing. This covers a soft-keyboard
  reflow or a field swap that can leave focus on a different field for
  a moment after the tap.
- If the target field never reports focus in time, the call fails
  without typing anything, so text is never sent into the wrong
  field.
- clearFirst true (the default) erases the field's existing content
  before typing, but only when the current driver supports erasing
  text. On a driver without that support, the field is left uncleared
  and no error is raised. clearFirst false always appends.
- The coordinate path taps the point, then applies clearFirst the
  same way, then types. It does not scroll, does not check
  obscurement, and does not wait for the field to report focus before
  typing.

**Returns**
- Selector path: `ok`, and on success `resolvedBy` and `detail`. On
  rejection, `ok: false` with `reason`, and `obscurer` when the
  rejection was an obscurement.
- Coordinate path: `ok: true` and `detail` describing how many
  characters were typed and where.

**Errors and recovery**
- `ok: false` with a "not found", "could not scroll", or obscurement
  reason: same recovery as tap.
- A focus-timeout failure reports that the field never took input
  focus and that nothing was typed: retarget the field with a more
  stable selector, or tap its coordinates directly before calling
  input_text again.
- If the field's value does not reflect clearFirst on a driver without
  erase support, verify the resulting value and clear it manually
  (for example with press_key using delete) before relying on it.

**Example**
```json
{ "selector": { "id": "email" }, "text": "user@example.com" }
```

### swipe
Swipes the screen, either as a centered directional scroll or as a
precise drag between two points.

**Use when:** scrolling a list or paging a carousel with no specific
element to auto-scroll to. Prefer tap or input_text on their own when
the target has a selector, since both already scroll a matching
element into view before acting on it.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| direction | string, one of `up`, `down`, `left`, `right` | conditionally | none | Centered directional scroll. Provide this, or provide fromX, fromY, toX, and toY instead. |
| fraction | number | no | 0.6 | Swipe distance for the direction path, as a fraction of the screen along the swipe axis. Ignored on the coordinate path. |
| fromX | number | conditionally | none | Start x coordinate for a two-point swipe. Required together with fromY, toX, and toY when direction is omitted. |
| fromY | number | conditionally | none | Start y coordinate for a two-point swipe. |
| toX | number | conditionally | none | End x coordinate for a two-point swipe. |
| toY | number | conditionally | none | End y coordinate for a two-point swipe. |
| durationMs | number | no | 200 | Swipe animation duration in milliseconds, for either path. |

**Behavior**
- Exactly one path runs per call: provide direction, or provide all
  four of fromX, fromY, toX, and toY. The call fails argument
  validation if neither shape is provided.
- The direction path reads the current screen size, then computes a
  swipe centered on the screen sized to fraction of the screen along
  the swipe axis (60 percent by default).
- The coordinate path drags exactly from (fromX, fromY) to (toX,
  toY). Fraction has no effect on it.
- Both paths bypass selector resolution and obscurement checks
  entirely. There is no selector-aware variant of swipe.

**Returns**
- Direction path: `ok: true` and `direction`.
- Coordinate path: `ok: true` and `detail` describing the from and to
  points.
- Neither path returns an `ok: false` outcome. A low-level driver
  failure (for example failing to read screen size) surfaces as a
  tool error instead.

**Errors and recovery**
- Missing both direction and the four coordinates fails argument
  validation. Supply one of the two shapes.
- A tool error on the direction path usually means the device is not
  responding. Confirm it with check_health before retrying.

**Example**
```json
{ "direction": "up", "fraction": 0.5 }
```

### press_key
Presses a system or navigation key, such as back, home, or enter.

**Use when:** dismissing a keyboard's enter action, navigating back,
or returning to the home screen without an on-screen control. Use tap
on a visible control such as Cancel or Close when the key reports no
equivalent on the current platform.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| key | string | yes | none | Key to press, such as back, home, or enter. Which keys a platform recognizes, and how each behaves, differs by platform. See the platform support table below. |

**Behavior**
- Dispatches the named key to the current device.
- Not every key has an equivalent primitive on every platform. When
  the current platform has none, the call still completes and
  reports the rejection through the result rather than as an error.
- Any string passes argument validation at the tool boundary: `key`
  has no fixed enum here. A key name the on-device agent does not
  recognize at all is rejected by the agent itself and reported as a
  tool error with an invalid-argument code, not as `ok: false`.

**Platform support**

| Key | Android | iOS |
|---|---|---|
| back | Dispatches the system back action. Always reports success, regardless of whether the foreground app handled it. | Taps the navigation bar's back button when one is present, reporting success. Otherwise performs a left-edge swipe gesture as a best-effort fallback, reporting no confirmable effect and naming an on-screen affordance to try instead. |
| home | Dispatches the system home action. Always reports success. | Presses the device's home control. Always reports success. |
| enter | Not recognized. Rejected with a tool error listing the keys the device build accepts. | Types a newline into the focused field. Always reports success. |
| tab | Not recognized. Rejected with a tool error listing the keys the device build accepts. | Not recognized as a distinct key. Types the literal text "tab" into the focused field and reports success. |
| escape | Not recognized. Rejected with a tool error listing the keys the device build accepts. | Not recognized as a distinct key. Types the literal text "escape" into the focused field and reports success. |
| delete | Not recognized. Rejected with a tool error listing the keys the device build accepts. | Not recognized as a distinct key. Types the literal text "delete" into the focused field and reports success. |

A key iOS does not recognize as a distinct key is never rejected: it
falls back to typing its own name as literal text into the currently
focused field, which is rarely the caller's intent. Confirm a key
belongs to the dedicated column above before relying on it for
anything other than typed input.

**Returns**
- `ok: true`: `detail`, present when the platform reports something
  about the outcome, empty otherwise.
- `ok: false`: `reason`, explaining why the key had no effect, often
  naming the on-screen fallback to use instead.

**Errors and recovery**
- `ok: false`: read `reason` for the suggested on-screen fallback and
  perform that action instead of retrying the same key.
- A tool error naming the key: the current platform rejected it.
  Check the platform support table above for a key the current
  platform accepts, or use the on-screen fallback the error message
  recommends.

**Example**
```json
{ "key": "back" }
```

### wait_for_element
Polls the UI hierarchy until an element matching a selector appears,
or a timeout expires.

**Use when:** confirming an expected element eventually appears after
an action that triggers navigation or a delayed state change. Use
tap_and_wait_transition instead when the wait follows a tap whose
failure should also be classified.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| selector | object | yes | none | Element selector to poll for. |
| timeoutMs | number | no | 5000 | Maximum time to wait, in milliseconds, measured from the first poll. |
| pollIntervalMs | number | no | 250 | Delay between hierarchy polls, in milliseconds. |

**Behavior**
- Polls the hierarchy on an interval of pollIntervalMs, checking the
  selector against each poll, and returns as soon as a poll matches.
- Does not scroll to bring an off-screen element into view and does
  not check obscurement. It only reads the hierarchy as it currently
  stands.
- Gives up once timeoutMs elapses from the first poll without a
  match.

**Returns**
- On match: the same shape as find_element (`found: true`, plus
  whichever of `id`, `text`, `label`, `value`, `role`, `bounds`,
  `center`, `enabled`, and `clickable` the element carries, including `secure` and the masked `text` and `value` of a secret field).
- On timeout: `{ found: false, reason, elapsedMs, polls }`, where
  `elapsedMs` is time spent waiting and `polls` is the number of
  hierarchy reads performed.

**Errors and recovery**
- `found: false`: the element never appeared in time. Confirm the
  selector against a fresh get_ui_tree read, widen the selector, or
  raise timeoutMs.
- A selector with no content-matching field (none of id, text, label,
  hint, or value set) matches the first element encountered in the
  tree. Add at least one content field to target a specific element.

**Example**
```json
{ "selector": { "text": "Welcome" }, "timeoutMs": 8000 }
```

## Script execution

### run_script
Execute a YAML test script against the currently bound device, one step at a time, stopping at the first failure.

**Use when:** replaying a saved regression script or a recorded flow, where the sequence of steps is already known. For exploratory testing or one-off actions, use the individual device tools (tap, input_text, get_ui_tree, and so on) directly instead.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| script | string | Yes | none | YAML test script content, in the Atomyx script format |
| env | object of string to string | No | none | Variables available to the script's `${var}` placeholders. A key here overrides the same key in the script's own `env` block |
| proxy | string | No | none | Network capture target for a script whose `proxy` field is `required`, in `type:path` form, for example `file:/tmp/capture.jsonl`. Same syntax the CLI's `--proxy` flag accepts. Omit when the script declares no proxy requirement |

**Behavior**
- Requires a device already bound with select_device. With no device bound, the call fails immediately without parsing the script.
- The script is parsed and validated before anything runs. A structurally invalid script (an unrecognized command, a malformed step, a failed validation rule) fails the call before any step executes.
- Inside a run opened by start_run, the script writes into that run, so its screenshots land in the run's `artifacts` folder and no second run is created. The run stays open afterwards. The run meta then records the script's `totalSteps` and `failedAtStep`, and when several scripts run in one run the script that ended last sets them. Calling finish_run or start_run, or stopping the server, while the script runs cancels it and waits for it to return before the run is finalized.
- When a step fails, the script's failure evidence is written into the run's `artifacts` folder as `script-NNN-step-SSS-failure.png`, `script-NNN-step-SSS-tree.json`, and `script-NNN-step-SSS-log.jsonl`. `SSS` is the failed step's 1-based position in the script, and `NNN` comes from the run's shared artifact counter, so two scripts that fail in one run never share a name. The extension of the screenshot follows the image the device returns, so it is `jpg` for a JPEG. The capture is bounded at 5 seconds, and when it is cut short the files that finished are kept and the log file ends with a `truncated` note. A script that stops because the device went unreachable writes no failure evidence.
- With no run open, the call is its own run of kind `mcp`, labeled with the script's name. The run is finalized when the script ends, with outcome `passed` when every step passed, `failed` when a step failed, `deviceLost` when the device went unreachable, `cancelled` when the call was canceled, and `error` when the script could not run to a result. Read it back with list_runs and get_run.
- Preconditions and required flows declared in the script, if any, are evaluated before the script's own steps. A failed precondition or a failed required flow is reported as a single synthetic step at index 0 (with command "preconditions" or "requires") and stops the run before any of the script's own steps execute.
- If the script declares `proxy: required` and this call carries no proxy argument (and none is otherwise configured), the run is refused before any step executes: ok is false, steps is empty, totalSteps still reports the script's full declared count, and the top level abort object names the code `PROXY_REQUIRED`, an operator-facing message, and a remedy naming the `proxy` argument. See yml-script-reference.md's Proxy Setup section for how to configure capture per platform.
- Steps run strictly in order. Execution stops at the first step whose result is not ok. Steps after that point are not attempted and do not appear in the result.
- A single call to run_script is subject to the same overall time budget as any other tool call (30 seconds by default). A script with many steps or long waits can exceed this easily. Pass a larger timeoutMs argument alongside script and env to raise the budget for a specific call.
- The script format itself, including config fields, step shorthand, preconditions, and variable substitution, is documented in `yml-script-reference.md`. That reference covers the YAML syntax. This entry covers only the tool call around it.

**Returns**
- scriptName: string, the script's declared name (or a default when the script does not declare one).
- ok: boolean, true only when every step passed.
- steps: array of per-step results, one entry per step that actually ran (steps after the first failure are omitted). Each entry has:
  - stepIndex: integer, the step's position in the script's step list. A synthetic precondition or required-flow failure reports -1 here.
  - command: string, the step's command name, or the literal unrecognized command text when the command name is not known.
  - ok: boolean, whether this step passed.
  - detail: string, present when the command reports additional detail. More often present on a failure, describing why.
    A `type` step reports only the number of characters it entered, for example `Typed 12 characters`, never the text.
  - durationMs: integer, how long this step took to run.
  - deviceLost: boolean, true only on the step the run aborted on when that abort happened because the device or agent went unreachable. Absent on every other step, including an ordinary failed step.
- totalSteps: integer, the number of steps declared in the script's own step list. This stays the full declared count even when the run stopped early, for example on a failed precondition.
- passedSteps: integer, how many of the reported steps were ok.
- failedAtStep: integer, the index of the first failed step. Absent entirely when every step passed.
- durationMs: integer, total wall-clock time for the whole run.
- artifacts: object, always empty. Reserved for future artifact metadata and not yet populated regardless of how many screenshots the run captured.
- deviceLost: boolean, true when the run stopped early because the device or agent went unreachable, as opposed to running to completion or stopping on an ordinary failed step. Absent when false.
- abort: object, present only when the run stopped early for a reason beyond an ordinary step failure. Absent when the run completed, including when it completed with a failed step. Fields:
  - reason: string, a stable tag for why the run stopped, for example `deviceLost` or `proxyRequired`.
  - code: string, the stable diagnostic code, for example `PROXY_REQUIRED`.
  - message: string, an operator-facing description of the failure.
  - remedy: string, present when the failure carries an actionable next step.

**Errors and recovery**
- No device bound: call select_device with a platform and id from list_devices, then retry.
- Script fails to parse or fails validation: the error message describes the offending field or step. Fix the YAML and retry. Nothing runs in this case, so there are no partial results to inspect.
- The call itself times out: the result reports a timeout with the elapsed time and the budget instead of a script result. Reissue the call with a larger timeoutMs, or split the script into shorter scripts.
- ok false with no timeout and no abort: read failedAtStep and the matching step's detail to find the failing step, fix the underlying app or script issue, then rerun.
- abort present with code PROXY_REQUIRED: the script declares `proxy: required` and this call carried no working proxy argument. Pass a `proxy` argument in `type:path` form, or drop `proxy: required` from the script when the run genuinely needs no capture.
- deviceLost true: the device or agent went unreachable mid-run rather than an ordinary step failing. Call select_device again to confirm the device is still reachable before rerunning the script.

**Example**
```json
{
  "script": "format: atomyx/v1\nappId: com.example.app\nname: Login test\n---\n- launchApp\n- tap: \"Sign in\"\n- type:\n    into: \"Email\"\n    text: ${email}\n- tap: \"Login\"\n- waitFor: \"Dashboard\"\n",
  "env": { "email": "user@test.com" }
}
```

## Run tracking

A run is an exploratory testing session: a bounded window in which an
agent or a human drives a device, and the tools attach evidence to it,
such as screenshots, bug reports, and a closing summary. Exactly one
run can be active at a time. Starting, working, and finishing a run is
the expected loop for any testing session that should leave a
reviewable trail behind it.

Run records persist to disk so they survive process restarts and can
be read back later. The default location is `~/.atomyx`, and the
root directory can be overridden through an environment variable read
at startup. Each run started by start_run or run_script is its own
run folder under the root, named after its id, with a `meta.json` that
carries `result` and `outcome`, and an `artifacts` folder that holds
the screenshots taken during the run. Runs stored as single flat
files are listed and read too.

### start_run
Begins a new active run.

**Use when:** at the beginning of a testing session, before driving
any device. Use finish_run when the session is done, not start_run
again.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| name | string | yes | none | Human description of the run. |
| source | string | no | `"unknown"` | Where the run originated. |

**Behavior**
- If a run is already active, it is force-closed with outcome error
  before the new run starts, so its trail is not silently dropped. Its
  run folder is finalized immediately, with a summary noting it was
  closed automatically because finish_run was never called.
- The new run's id has the form `<yyyymmdd-hhmmss>-<label>-<4 hex>`.
  The label is the selected device's name, or its id when it has no
  name, and `mcp` when no device is selected yet. It is unique even
  across runs started in the same second.
- Only one run is active at a time. There is no nested or parallel
  run support.

**Returns**
- `ok`: whether the call completed.
- `id`, `name`, `source`, `startedAt`: identity and start time of the
  new run.
- `erroredPredecessor`: the id of the run that was force-closed, only
  present when that happened.

**Errors and recovery**
- Missing `name` fails argument validation. Supply a short description
  of what the session will cover.
- A non-empty `erroredPredecessor` in the response means a previous
  session forgot to call finish_run. Investigate that run with get_run
  if its findings matter.

**Example**
```json
{ "name": "Checkout flow smoke test", "source": "agent" }
```

### finish_run
Closes the active run with a verdict and finalizes its run folder.

**Use when:** the testing session is complete and a final status is
known. Use update_run_summary afterward if a summary needs to be added
or changed once more is learned.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| status | string, one of `passed`, `failed`, `error` | no | `"passed"` | Final verdict of the run. |
| summary | string | no | none | Closing summary text. |

**Behavior**
- Closes whichever run is currently active. There is no id parameter,
  because only one run can be active at a time.
- Finalizes the run folder immediately, including every bug finding
  recorded against it during the session. The status becomes the run's
  outcome: `passed` gives outcome `passed`, `failed` gives `failed`,
  and `error` gives `error`.
- Computes the run's duration from its start time to the finish time.
- A run the server closes on its own is finalized too. A lost device
  closes it with result `cancelled` and outcome `deviceLost`, and the
  open-run idle timeout closes it with result `cancelled` and outcome
  `cancelled`.
- Stopping the server finalizes the open run with result `cancelled` and
  outcome `cancelled`, with its findings written. A summary you set with
  update_run_summary is kept, and a closing summary the server composes
  fills only an empty one.
- Scripts still running inside the run are cancelled and awaited before
  the folder is finalized, so a script never writes into a finished run.

**Returns**
- `ok`: false when no run was active to finish, true otherwise.
- `id`, `status`: identity and final verdict.
- `actionCount`: number of driver actions recorded during the run.
- `findingsCount`: number of bug reports attached to the run.
- `durationMs`: elapsed time from start to finish, in milliseconds.

**Errors and recovery**
- `ok: false` with no active run means finish_run was called with
  nothing open. Call start_run first, or check whether a previous
  finish_run already closed it.
- A tool error naming the run means the run folder could not be
  finalized. The run is closed in memory but reads as interrupted, so
  check the storage directory, then read it with get_run.
- A tool error saying a script is still executing means a script in the
  run did not return before the call gave up. The run stays open, so call
  finish_run again.

**Example**
```json
{ "status": "failed", "summary": "Login succeeds but checkout crashes on payment step." }
```

### list_runs
Lists persisted runs as summaries, most recent first.

**Use when:** discovering which run ids exist before reading one in
full. Use get_run to fetch a single run's complete record including
its findings.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| status | string, one of `passed`, `failed`, `error`, `cancelled`, `running` | no | none | Filter to runs with this status. |
| limit | number | no | 20 | Maximum number of runs in this page. |
| offset | number | no | 0 | Number of matching runs to skip. |

**Behavior**
- Sorted by start time descending, so the newest run is first.
- Returns summaries only, not the findings list or summary text. Use
  get_run for the full record.
- Reads every run folder first, then every flat run record. A run id
  present in both forms is listed once, from the run folder. Runs with
  equal start times are ordered by id.
- The status filter matches the `status` field of each summary. A run
  that has not completed and still has a live writer has status
  `running`. A run that has not completed and has no live writer was
  interrupted and has status `cancelled`.
- A run whose stored record cannot be read is left out of the list and
  counted in `skipped`.
- The status filter matches persisted runs, so a currently active,
  unfinished run does not appear until finish_run persists it, unless
  a previous start_run already force-closed and persisted it.

**Returns**
- `ok`: whether the call completed.
- `count`: number of runs in this page.
- `totalMatching`: number of readable runs matching the filter across
  every page. Runs counted in `skipped` are not included.
- `skipped`: number of stored runs that could not be read, for example
  a damaged run folder or a record whose id differs from its name.
  Always present, and zero when every stored run was readable.
- `offset`: the offset this page was requested at.
- `nextOffset`: offset for the next page, or absent when this page
  reached the end.
- `runs`: array of run summaries, each with `id`, `name`, `source`,
  `status`, `result`, `outcome`, `startedAt`, `finishedAt`, `durationMs`,
  `actionCount`, and `findingsCount`.
- `result` is the verdict and is one of `passed`, `failed`, or
  `cancelled`. `outcome` is the precise reason the run ended and is one
  of `passed`, `failed`, `error`, `cancelled`, `deviceLost`, or
  `interrupted`. Every outcome belongs to exactly one result, so `error`
  belongs to `failed`, and `cancelled`, `deviceLost`, and `interrupted`
  belong to `cancelled`. A run that is still running has no `result`
  and no `outcome`, and both fields are absent. A run that never
  recorded a completion and has no live writer reports result
  `cancelled` and outcome `interrupted`. `status` is kept for
  compatibility, so prefer `result` and `outcome`.

**Errors and recovery**
- An empty `runs` array with `totalMatching: 0` means no runs match
  the filter. Widen or drop the status filter.

**Example**
```json
{ "status": "failed", "limit": 10 }
```

### get_run
Fetches one persisted run's full record by id.

**Use when:** the run's id is already known and its full detail is
needed, including findings and summary. Use list_runs first when the
id is not yet known.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| id | string | yes | none | Run id as returned by start_run. |

**Behavior**
- Reads the run folder first, then the flat run record, so a run is found
  in whichever form it is stored. A run that was started but never
  finished or force-closed is not stored yet, so it is not found here.

**Returns**
- `ok`: false when the id is not found, true otherwise.
- `run`: the full persisted run record, including `findings` (each bug
  attached during the session), `summary` when present, and the `result`
  and `outcome` fields described under list_runs.

**Errors and recovery**
- `ok: false` means the id does not exist in storage. Confirm it with
  list_runs, or check whether delete_run already deleted it.
- Error `RUN_UNREADABLE` names the id and means the run exists but its
  stored record is damaged. When a readable flat record exists for the
  same id, get_run returns that record instead. Otherwise call
  delete_run to remove the damaged record.

**Example**
```json
{ "id": "run-1732550400000-1" }
```

### update_run_summary
Sets or replaces the summary text on a persisted run.

**Use when:** the summary was not known at finish_run time, or new
context should be added after the fact. Use finish_run instead when
the run is still active and has not been closed yet.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| id | string | yes | none | Run id to update. |
| summary | string | yes | none | New summary text. Pass an empty string to clear it. |

**Behavior**
- Replaces the summary field only. Every other field on the run,
  including status, findings, and durationMs, is left untouched.
- Works on the open run and on a finished run. The summary is written
  into the run folder of a run started by start_run or run_script, or
  into the flat record of a run stored in the flat form.
- A run with no summary field, such as one written by the atomyx CLI,
  is read-only here. A run that a writer other than this session's open
  run still holds open is busy until it finishes.
- The id must be a bare run id. An id that contains a path separator is
  rejected as an invalid argument.

**Returns**
- `ok`: false when the id is not found, true otherwise.
- `id`, `summary`: the updated run id and the summary now stored.

**Errors and recovery**
- `ok: false` means the id does not exist. Confirm it with list_runs
  or get_run before retrying.
- Error `RUN_READ_ONLY` names the run and means it has no summary
  field.
- Error `RUN_BUSY` names the run and means a writer still holds it open,
  such as a run_script run in progress or a run another process is
  writing. Repeat the call after the run finishes.
- Error `RUN_BUSY` can also mean a run migration is in progress. Nothing
  was changed. Wait for the migration to finish, then repeat the call.
- Error `INVALID_ARGUMENT` means the id is not a bare run id.

**Example**
```json
{ "id": "run-1732550400000-1", "summary": "Reproduced the payment crash on the second attempt." }
```

### delete_run
Deletes a persisted run.

**Use when:** removing a stale or unwanted run from storage. Deleting
a run does not delete its bug records, so call delete_bug afterward
for each of that run's bug records that also needs deleting.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| id | string | yes | none | Run id to delete. |

**Behavior**
- Removes the run in every form it is stored, run folder and flat record
  alike, and succeeds when either one existed. Bug records created
  during that run remain in storage and stay reachable with get_bug, but
  their runId field then points at a run that no longer exists.
- Use list_bugs with a runId filter to find those bug records, then
  delete_bug to remove each one individually if a full cleanup is
  wanted.
- A run a writer still holds open is never deleted. That covers the run
  opened by start_run and a run another process is writing.

**Returns**
- `ok`: false when the id is not found, true otherwise.
- `id`: the id that was deleted.

**Errors and recovery**
- `ok: false` means the id does not exist, so there is nothing to
  delete. Confirm the id with list_runs.
- Error `RUN_BUSY` names the run and means a writer still holds it open.
  The run folder is left untouched. Call finish_run if it is the run you
  opened, or wait for the other process to finish, then call delete_run
  again.
- Error `RUN_BUSY` can also mean a run migration is in progress. The run
  is left untouched. Wait for the migration to finish, then call
  delete_run again.

**Example**
```json
{ "id": "run-1732550400000-1" }
```

## Bugs and case studies

### report_bug
Records a bug finding against the currently active run, and by default attaches a screenshot of the device as evidence.

**Use when:** you have confirmed a defect by observing device state and want to log it for the run. Use update_run_summary instead when you want to record overall run notes rather than a single finding.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| title | string | yes | none | One-line bug title. |
| description | string | yes | none | Full description: expected versus actual, repro steps, and any context gathered, such as selectors tried or tool results. |
| captureScreenshot | boolean | no | true | Whether to capture a screenshot of the active device as evidence. |

**Behavior**
- Requires an active run. If no run is active, the call returns `ok: false` with a reason telling you to call start_run first, and no bug is recorded.
- Does not require an active device. The bug is still recorded when no device is selected, so a lost device connection never causes a lost bug report.
- When captureScreenshot is true or omitted and a device is active, a screenshot is captured and saved automatically. There is no separate step to attach it.
- If screenshot capture fails, or no device is active, the bug is still recorded without a screenshot rather than failing the call.
- The bug is stored both as a finding on the active run and as its own persisted record, so it can be listed or fetched independently of the run.
- There are no severity or category fields. A bug is only a title, a description, and an optional screenshot.

**Returns**
- `ok`: true when the bug was recorded.
- `id`: the bug id, generated from the run id and the bug's position among the run's findings so far.
- `runId`: the id of the run the bug was attached to.
- `screenshotPath`: the storage key of the screenshot, present only when one was captured and saved.

**Errors and recovery**
- `ok: false` with a reason mentioning no active run: call start_run first, then retry.
- A screenshot failure never fails the whole call. Check for a missing `screenshotPath` in the result if you expected one and it is absent, and consider calling screenshot manually afterward if the evidence still matters.

**Example**
```json
{
  "title": "Login button unresponsive after network error",
  "description": "Expected: tapping Login after a failed request re-enables the button. Actual: button stays disabled. Repro: disable network, submit login, re-enable network, tap Login.",
  "captureScreenshot": true
}
```

### list_bugs
Lists persisted bug reports as summaries, newest first, with optional run filtering and pagination.

**Use when:** you want an overview of recorded bugs, or need to find a bug id before calling get_bug or delete_bug. Use get_case_studies instead when looking for past lessons rather than specific defects.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| runId | string | no | none | Restrict results to bugs recorded against this run id. |
| limit | number | no | 50 | Maximum number of summaries to return. |
| offset | number | no | 0 | Number of matching bugs to skip before the returned page. |

**Behavior**
- Bugs are sorted by timestamp descending, so the newest bug is first.
- Each entry is a summary only. Fetch the full record, including the description, with get_bug.
- Filtering by runId happens before pagination, so offset and limit apply to the filtered set, not the full bug list.

**Returns**
- `ok`: true.
- `count`: number of summaries in this page.
- `totalMatching`: total number of bugs matching the filter, across all pages.
- `offset`: the offset this page started from.
- `nextOffset`: the offset to use for the next page, or absent when this page reached the end.
- `bugs`: array of summaries, each with `id`, `runId`, `runName`, `title`, `timestamp`, and `hasScreenshot`.

**Errors and recovery**
- A storage failure surfaces as an error result. Retry the call. There is no partial or inconsistent state to recover from since list_bugs never writes.

**Example**
```json
{ "runId": "run-1732550000000-1", "limit": 20 }
```

### get_bug
Fetches one persisted bug report by id, including its full description and screenshot reference.

**Use when:** you already have a bug id, typically from report_bug or list_bugs, and need the full description or the screenshot location. Use list_bugs instead when you only have a run id or want to browse.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| id | string | yes | none | Bug id as returned by report_bug. |

**Behavior**
- Returns the full persisted record: id, run id, run name, title, description, timestamp, and screenshot path when one exists.
- The screenshot bytes themselves are not inlined in the response. The `screenshotPath` is a storage key that a consumer with storage access can load separately.

**Returns**
- `ok`: true with `bug` holding the full record, or false when the id does not exist.

**Errors and recovery**
- `ok: false` with a reason naming the missing id: the id was wrong, or the bug was deleted. Call list_bugs to find the correct id.

**Example**
```json
{ "id": "run-1732550000000-1-bug-1" }
```

### delete_bug
Deletes a persisted bug report and its screenshot, if any.

**Use when:** a reported bug turns out to be invalid or a duplicate that does not belong in the playbook. There is no neighbouring tool to soften this since deletion here is permanent.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| id | string | yes | none | Bug id to delete. |

**Behavior**
- Removes both the bug record and its sibling screenshot key, when a screenshot was captured, so no orphaned screenshot payload is left behind.
- Existence is checked before any delete happens, so the operation either fully succeeds or leaves the bug untouched. There is no partial-delete state.
- Deleting a bug removes it from list_bugs and get_bug, but does not remove it from the findings already recorded on its run's own persisted record.

**Returns**
- `ok`: true with the deleted `id`, or false when the id does not exist.

**Errors and recovery**
- `ok: false` with a reason naming the missing id: nothing was deleted. Call list_bugs to confirm the id before retrying.

**Example**
```json
{ "id": "run-1732550000000-1-bug-1" }
```

### add_case_study
Appends a learned lesson to the case-studies playbook as a markdown record.

**Use when:** you have just recovered from a non-obvious error, such as an unexpected obscurement dismissal, a scroll-search edge case, or a flaky transition, and want future sessions to benefit. Use report_bug instead when the point is a defect in the app under test rather than a lesson for the agent itself.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| title | string | yes | none | Short title for the lesson. |
| trigger | string | yes | none | What situation caused the pain. |
| solution | string | yes | none | What worked to recover. |
| example | string | no | none | An optional concrete example, rendered as a code block in the record. |

**Behavior**
- The record is stored as markdown under a key made of the current year and month plus a slug derived from the title, so multiple case studies in the same month accumulate rather than overwrite each other by date alone.
- The slug is generated automatically from the title: lowercased, with runs of characters that are not letters or digits collapsed to a single dash. You do not choose or pass a slug.
- The record body always includes the title as a heading, the trigger, and the solution, plus the example as a fenced code block when provided, plus an added timestamp.
- There is no severity, category, or app-scoping field. A case study is a general lesson, not an app-specific record.

**Returns**
- `ok`: true.
- `key`: the storage key the case study was saved under, in the form of a year-month path followed by the title slug.

**Errors and recovery**
- A storage failure surfaces as an error result. Retry the call with the same arguments.

**Example**
```json
{
  "title": "Obscurement dismissal after permission dialog",
  "trigger": "A system permission dialog stole focus mid-script, and the next tap silently missed.",
  "solution": "Dismiss the obscurement explicitly and re-resolve the selector before retrying the tap.",
  "example": "wait_for_element then dismiss overlay then retry tap"
}
```

### get_case_studies
Lists and reads case-study markdown records from the playbook, newest month first.

**Use when:** starting a session and wanting to reuse past learnings, including learnings recorded while testing a different app, before diagnosing a new failure from scratch. Use list_bugs instead when looking for specific past defects rather than general lessons.

**Parameters**
| Name | Type | Required | Default | Meaning |
|---|---|---|---|---|
| month | string | no | none | Restrict results to a single year-month, matching the format used in add_case_study's storage key. |
| limit | number | no | 20 | Maximum number of records to return. |
| offset | number | no | 0 | Number of matching records to skip before the returned page. |

**Behavior**
- Case studies are not scoped to a particular app or run. The playbook is shared across every session and every app the agent has tested, so a lesson learned on one app is available when testing another.
- Results are sorted descending by storage key, which sorts by year-month first and then by title slug, so the newest month's records come first.
- Each returned entry includes the full markdown body, not a summary, since case studies are meant to be read in full.
- Calling this with no arguments at the start of a session is the intended way to surface prior lessons before starting work.

**Returns**
- `count`: number of records in this page.
- `totalMatching`: total number of records matching the filter, across all pages.
- `offset`: the offset this page started from.
- `nextOffset`: the offset to use for the next page, or absent when this page reached the end.
- `studies`: array of records, each with `key` and `body`.

**Errors and recovery**
- A storage failure surfaces as an error result. Retry the call.
- An empty `studies` array with `totalMatching: 0` means no case studies exist yet, or none match the `month` filter. Drop the filter to check the full playbook.

**Example**
```json
{ "limit": 10 }
```

