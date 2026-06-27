---
name: atomyx-debug-failure
description: Recover from Atomyx tool failures — handle TOOL_TIMEOUT, empty find_element results, stale UI tree, device disconnects. Auto-trigger when any mcp__atomyx__* tool returns isError=true, when find_element returns an empty match, or when a tap landed but the screen did not change.
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
2. Broaden the selector. If `text: "Login"` was passed, retry with `textContains: "Login"`. If `resourceId` was passed, try `contentDesc` or visible `text`.
3. If the element is visible in the tree but no selector matches, capture coordinates from the tree's inline `@cx,cy` and tap by coords.

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

Do not loop. Three failures at the same step means the assumption is wrong — escalate to the user, do not brute-force.

## When the device disappears

If `select_device` succeeds but tools immediately fail with disconnect-shaped errors:

- **Android**: confirm the emulator or device is still booted. The user's `adb forward` may have dropped.
- **iOS**: confirm the app is still in foreground. The XCUITest runner stops when the app crashes; relaunch via `launch_app`.

Surface this to the user before any further retry. Do not silently relaunch the app — the crash itself may be the bug worth reporting.
