---
name: atomyx-debug-failure
description: Recover from Atomyx tool failures, including TOOL_TIMEOUT, empty find_element results, stale UI tree, device disconnects. Auto-trigger when any mcp__atomyx__* tool returns isError=true, when find_element returns an empty match, or when a tap landed but the screen did not change.
---

# Atomyx failure recovery

When an Atomyx tool fails, classify first, retry second. Never retry the same call with the same arguments.

## Failure classes

### TOOL_TIMEOUT

Payload shape: `{ code: "TOOL_TIMEOUT", hint: "..." }`. The device call exceeded its budget and was aborted.

Steps:
1. Re-orient: call `get_ui_tree`. It will fail fast if the driver is dead.
2. If the tree call also fails: call `select_device` to reconnect.
3. If reconnect fails: surface the device problem to the user and stop.

### Empty find_element result

The selector did not match any element on the current screen.

Steps:
1. Confirm the screen is the one expected. Call `get_ui_tree` and inspect the layout.
2. Broaden the selector. If `text: "Login"` was passed as a literal, retry with a regex pattern such as `{"text": {"regex": "Login", "flags": "i"}}`. If `id` was passed, try `label` or visible `text` instead.
3. If the element is visible in the tree but no selector matches, read its `bounds` (a "left,top,right,bottom" string) from `get_ui_tree`, or call `find_element` for its `center` point, and tap by coordinates.

### Tap landed but screen did not change

Likely tapped the wrong element, or the action is asynchronous.

Steps:
1. `wait_for_element` for the expected next-screen element with a 2-3 second budget.
2. If still nothing: re-fetch the tree and check whether the tap target was actually tappable. Some elements look tappable but are not (decorative labels, container backgrounds).

## Retry budget

Per intent, retry at most twice. If both retries fail, report the failure with:

- The tool name and arguments that failed.
- The current UI tree, so a human can see what was on screen.
- A screenshot.

Do not loop. Three failures at the same step means the assumption is wrong, so escalate to the user instead of brute-forcing it.

## When the device disappears

If `select_device` succeeds but tools immediately fail with disconnect-shaped errors:

- **Android**: confirm the emulator or device is still booted. The user's `adb forward` may have dropped.
- **iOS**: confirm the app is still in foreground. The XCUITest runner stops when the app crashes; relaunch via `launch_app`.

Surface this to the user before any further retry. Do not silently relaunch the app, because the crash itself may be the bug worth reporting.
